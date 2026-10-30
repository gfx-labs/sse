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

// --- Encoder error paths ---

func TestEncoderWriteError(t *testing.T) {
	for name, ev := range map[string]*sse.Event{
		"event":  {Event: "test"},
		"data":   {Data: []byte("test")},
		"fields": {Fields: map[string][]byte{"k": []byte("v")}},
		"id":     {ID: []byte("1")},
		"bareID": {ID: []byte{}},
	} {
		t.Run(name, func(t *testing.T) {
			err := sse.NewEncoder(errWriter{}).Encode(ev)
			require.ErrorContains(t, err, "write failed")
		})
	}
}

func TestEncoderCommentWriteError(t *testing.T) {
	require.Error(t, sse.NewEncoder(errWriter{}).Comment("x"))
}

// --- Client error paths ---

func TestClientNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	err := firstErr(t, sse.Client{}, srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestClientBadURLDisableReconnect(t *testing.T) {
	err := firstErr(t, sse.Client{DisableReconnect: true}, "http://127.0.0.1:1")
	require.Error(t, err)
}

func firstErr(t *testing.T, c sse.Client, url string) error {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	for _, err := range c.Events(t.Context(), req) {
		if err != nil {
			return err
		}
	}
	return nil
}

// --- Stream context cancellation ---

func TestStreamContextCancelled(t *testing.T) {
	ctx, cn := context.WithCancel(context.Background())
	cn() // cancel immediately

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := sse.Upgrade(w, r.WithContext(ctx))
		if !assert.NoError(t, err) {
			return
		}
		assert.ErrorIs(t, stream.Send(&sse.Event{Data: []byte("hello")}), context.Canceled)
		assert.ErrorIs(t, stream.Comment("x"), context.Canceled)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

// --- Upgrade streaming not supported ---

func TestUpgradeStreamingNotSupported(t *testing.T) {
	w := &noFlushResponseWriter{header: http.Header{}}
	r := httptest.NewRequest("GET", "/", nil)
	_, err := sse.Upgrade(w, r)
	require.ErrorIs(t, err, sse.ErrStreamingNotSupported)
}

// Middleware that wraps the ResponseWriter but exposes Unwrap must still stream.
func TestUpgradeWrappedWriter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := sse.Upgrade(&unwrapWriter{w}, r)
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, stream.Send(&sse.Event{Data: []byte("hi")}))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	for ev, err := range sse.Events(resp.Body) {
		require.NoError(t, err)
		assert.Equal(t, "hi", string(ev.Data))
	}
}

type unwrapWriter struct{ http.ResponseWriter }

func (u *unwrapWriter) Unwrap() http.ResponseWriter { return u.ResponseWriter }

type noFlushResponseWriter struct {
	header http.Header
}

func (n *noFlushResponseWriter) Header() http.Header         { return n.header }
func (n *noFlushResponseWriter) Write(b []byte) (int, error) { return len(b), nil }
func (n *noFlushResponseWriter) WriteHeader(int)             {}

// --- UTF-8 validation ---

func TestEncoderUTF8Validation(t *testing.T) {
	enc := sse.NewEncoder(&bytes.Buffer{}, sse.WithUTF8Validation())
	require.NoError(t, enc.Encode(&sse.Event{Event: "valid", Data: []byte("ok")}))
	require.ErrorIs(t, enc.Encode(&sse.Event{Event: "\xff\xfe"}), sse.ErrInvalidUTF8)
	require.ErrorIs(t, enc.Encode(&sse.Event{Data: []byte{0x80, 0x81}}), sse.ErrInvalidUTF8)
	require.ErrorIs(t, enc.Encode(&sse.Event{ID: []byte{0x80}}), sse.ErrInvalidUTF8)
}

// --- Line breaks in data ---

func TestEncoderDataLineBreaks(t *testing.T) {
	for in, want := range map[string]string{
		"a\rb":   "data: a\ndata: b\n\n",
		"a\r\nb": "data: a\ndata: b\n\n",
		"a\nb":   "data: a\ndata: b\n\n",
		"a\r":    "data: a\ndata: \n\n",
		"":       "data: \n\n",
	} {
		var buf bytes.Buffer
		require.NoError(t, sse.NewEncoder(&buf).Encode(&sse.Event{Data: []byte(in)}))
		assert.Equal(t, want, buf.String(), "input %q", in)
	}
}

// --- Injection through fields ---

func TestEncoderRejectsLineBreaks(t *testing.T) {
	for name, ev := range map[string]*sse.Event{
		"event":      {Event: "a\ndata: injected"},
		"id":         {ID: []byte("1\r")},
		"fieldValue": {Fields: map[string][]byte{"k": []byte("a\nb")}},
		"fieldName":  {Fields: map[string][]byte{"a:b": []byte("v")}},
		"emptyName":  {Fields: map[string][]byte{"": []byte("v")}},
		"comment":    {Comments: [][]byte{[]byte("a\nb")}},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			require.ErrorIs(t, sse.NewEncoder(&buf).Encode(ev), sse.ErrInvalidField)
			assert.Empty(t, buf.String())
		})
	}
}
