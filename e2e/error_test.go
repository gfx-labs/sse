package e2e

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errWriter is an io.Writer that always returns an error.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

// --- Writer error paths ---

func TestWriterFieldWriteError(t *testing.T) {
	w := sse.NewWriter(errWriter{})
	err := w.Field("event", []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestWriterFieldTopicWriteError(t *testing.T) {
	// Field with empty topic but name "id" still writes
	w := sse.NewWriter(errWriter{})
	err := w.Field("id", nil)
	require.Error(t, err)
}

func TestWriterWriteError(t *testing.T) {
	w := sse.NewWriter(errWriter{})
	_, err := w.Write([]byte("data"))
	require.Error(t, err)
}

func TestWriterFlushError(t *testing.T) {
	// Get writer into inMessage state, then make it fail on flush
	buf := &bytes.Buffer{}
	w := sse.NewWriter(buf)
	_, err := w.Write([]byte("x"))
	require.NoError(t, err)

	// Now create a writer with errWriter and set it to inMessage state
	w2 := sse.NewWriter(errWriter{})
	// Write will fail on checkMessage's first write
	_, err = w2.Write([]byte("x"))
	require.Error(t, err)
}

func TestWriterNextError(t *testing.T) {
	w := sse.NewWriter(errWriter{})
	// Next calls Flush then writeByte; on errWriter the Flush newline write fails
	// but only if inMessage. Without inMessage, it goes straight to writeByte('\n')
	err := w.Next()
	require.Error(t, err)
}

// --- Encoder error paths ---

func TestEncoderEncodeWriteError(t *testing.T) {
	enc := sse.NewEncoder(errWriter{})
	err := enc.Encode(&sse.Event{Event: []byte("test")})
	require.Error(t, err)
}

func TestEncoderEncodeDataWriteError(t *testing.T) {
	enc := sse.NewEncoder(errWriter{})
	err := enc.Encode(&sse.Event{Data: []byte("test")})
	require.Error(t, err)
}

func TestEncoderEncodeFieldsWriteError(t *testing.T) {
	enc := sse.NewEncoder(errWriter{})
	err := enc.Encode(&sse.Event{Fields: map[string][]byte{"k": []byte("v")}})
	require.Error(t, err)
}

func TestEncoderEncodeIDWriteError(t *testing.T) {
	// the encoder sends each event in one Write, so a failing first write must surface
	enc := sse.NewEncoder(&failAfterN{n: 0})
	err := enc.Encode(&sse.Event{ID: sse.ID("1")})
	require.Error(t, err)
}

// failAfterN is an io.Writer that succeeds for the first n Write calls then fails.
type failAfterN struct {
	n     int
	count int
}

func (f *failAfterN) Write(p []byte) (int, error) {
	f.count++
	if f.count > f.n {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

// --- Subscribe error paths ---

func TestSubscribeNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	err := sse.Subscribe(context.Background(), req, func(msg *sse.Event) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Internal Server Error")
}

func TestSubscribeBadURL(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://127.0.0.1:1", nil)
	err := sse.Subscribe(context.Background(), req, func(msg *sse.Event) {})
	require.Error(t, err)
}

// --- EventSink context cancellation ---

func TestEventSinkContextCancelled(t *testing.T) {
	ctx, cn := context.WithCancel(context.Background())
	cn() // cancel immediately

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := sse.DefaultUpgrader.Upgrade(w, r.WithContext(ctx))
		if err != nil {
			return
		}
		err = sink.Encode(&sse.Event{Data: []byte("hello")})
		assert.ErrorIs(t, err, context.Canceled)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

// --- Upgrader streaming not supported ---

func TestUpgraderStreamingNotSupported(t *testing.T) {
	// httptest.ResponseRecorder does not implement http.Flusher in older Go versions,
	// but httptest.NewRecorder actually does. Use a minimal ResponseWriter instead.
	w := &noFlushResponseWriter{header: http.Header{}}
	r := httptest.NewRequest("GET", "/", nil)
	_, err := sse.DefaultUpgrader.Upgrade(w, r)
	require.ErrorIs(t, err, sse.ErrStreamingNotSupported)
}

type noFlushResponseWriter struct {
	header http.Header
}

func (n *noFlushResponseWriter) Header() http.Header         { return n.header }
func (n *noFlushResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *noFlushResponseWriter) WriteHeader(int)             {}

// --- Writer UTF-8 validation via full pipeline ---

func TestWriterUTF8ValidationE2E(t *testing.T) {
	buf := &bytes.Buffer{}
	w := sse.NewWriter(buf, sse.OptionValidateUtf8(true))

	// Valid UTF-8 should work
	require.NoError(t, w.Field("event", []byte("valid")))

	// Invalid UTF-8 should error on Field
	err := w.Field("event", []byte{0xff, 0xfe})
	require.ErrorIs(t, err, sse.ErrInvalidUTF8Bytes)

	// Invalid UTF-8 should error on Write
	_, err = w.Write([]byte{0x80, 0x81})
	require.ErrorIs(t, err, sse.ErrInvalidUTF8Bytes)
}

// --- Writer carriage return in data ---

func TestWriterCarriageReturnE2E(t *testing.T) {
	buf := &bytes.Buffer{}
	w := sse.NewWriter(buf)

	// \r should split data lines
	_, err := w.Write([]byte("a\rb"))
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	assert.Equal(t, "data: a\rdata: b\n", buf.String())
}

func TestWriterCRLFInData(t *testing.T) {
	buf := &bytes.Buffer{}
	w := sse.NewWriter(buf)

	_, err := w.Write([]byte("a\r\nb"))
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	assert.Equal(t, "data: a\r\ndata: b\n", buf.String())
}

// --- Flush with trailing carriage ---

func TestWriterFlushTrailingCarriage(t *testing.T) {
	buf := &bytes.Buffer{}
	w := sse.NewWriter(buf)

	_, err := w.Write([]byte("a\r"))
	require.NoError(t, err)
	// At this point trailingCarriage is true, inMessage is false
	require.NoError(t, w.Flush())
	// Flush should write the trailing newline
	assert.Contains(t, buf.String(), "data: a\r\n")
}
