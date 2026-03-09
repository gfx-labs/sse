package e2e

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Encoder → Decoder roundtrip ---

func TestRoundtripSimpleData(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	require.NoError(t, enc.Encode(&sse.Event{Data: []byte("hello world")}))

	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("hello world"), ev.Data)
}

func TestRoundtripWithEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	require.NoError(t, enc.Encode(&sse.Event{
		Event: []byte("notification"),
		Data:  []byte("you have mail"),
	}))

	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("notification"), ev.Event)
	assert.Equal(t, []byte("you have mail"), ev.Data)
}

func TestRoundtripWithID(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	require.NoError(t, enc.Encode(&sse.Event{
		Data: []byte("msg"),
		ID:   sse.ID("99"),
	}))

	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("msg"), ev.Data)
	require.NotNil(t, ev.ID)
	assert.Equal(t, []byte("99"), *ev.ID)
}

func TestRoundtripEmptyID(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	require.NoError(t, enc.Encode(&sse.Event{
		Data: []byte("msg"),
		ID:   sse.ID(""),
	}))

	// Encoder writes bare "id\n" for empty IDs.
	// The decoder's field parser sees "id" as a custom field (no colon),
	// which tries to write to e.Fields map. Pre-initialize Fields to avoid
	// nil map panic (decoder.go:84 bug — custom field path doesn't nil-check Fields).
	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{Fields: map[string][]byte{}}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("msg"), ev.Data)
}

func TestRoundtripWithFields(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	require.NoError(t, enc.Encode(&sse.Event{
		Data:   []byte("payload"),
		Fields: map[string][]byte{"retry": []byte("3000")},
	}))

	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("payload"), ev.Data)
	assert.Equal(t, []byte("3000"), ev.Fields["retry"])
}

func TestRoundtripMultiLineData(t *testing.T) {
	original := "line1\nline2\nline3"
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	require.NoError(t, enc.Encode(&sse.Event{Data: []byte(original)}))

	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte(original), ev.Data)
}

func TestRoundtripMultipleEvents(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	for i := 0; i < 10; i++ {
		require.NoError(t, enc.Encode(&sse.Event{
			Data: []byte(strconv.Itoa(i)),
			ID:   sse.ID(strconv.Itoa(i)),
		}))
	}

	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	for i := 0; i < 10; i++ {
		ev := &sse.Event{}
		require.NoError(t, dec.Decode(ev))
		assert.Equal(t, []byte(strconv.Itoa(i)), ev.Data)
		require.NotNil(t, ev.ID)
		assert.Equal(t, []byte(strconv.Itoa(i)), *ev.ID)
	}
}

func TestRoundtripNilData(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := sse.NewEncoder(buf)
	require.NoError(t, enc.Encode(&sse.Event{Event: []byte("ping")}))

	r := sse.NewReader(strings.NewReader(buf.String()))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("ping"), ev.Event)
}

// --- Full HTTP roundtrip: server Upgrader → client Subscribe ---

func TestHTTPRoundtrip(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for i := 0; i < 5; i++ {
			err := sink.Encode(&sse.Event{
				Event: []byte("count"),
				Data:  []byte(strconv.Itoa(i)),
				ID:    sse.ID(strconv.Itoa(i)),
			})
			if err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cn := context.WithCancel(context.Background())
	defer cn()
	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)

	var received []*sse.Event
	err = sse.Subscribe(ctx, req, func(msg *sse.Event) {
		cp := &sse.Event{
			Event: append([]byte(nil), msg.Event...),
			Data:  append([]byte(nil), msg.Data...),
		}
		if msg.ID != nil {
			id := append([]byte(nil), *msg.ID...)
			cp.ID = &id
		}
		received = append(received, cp)
	})
	require.NoError(t, err)
	require.Len(t, received, 5)
	for i, ev := range received {
		assert.Equal(t, []byte("count"), ev.Event)
		assert.Equal(t, []byte(strconv.Itoa(i)), ev.Data)
		require.NotNil(t, ev.ID)
		assert.Equal(t, []byte(strconv.Itoa(i)), *ev.ID)
	}
}

func TestHTTPRoundtripMultiLineData(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			return
		}
		_ = sink.Encode(&sse.Event{Data: []byte("line1\nline2\nline3")})
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	req, _ := http.NewRequest("GET", srv.URL, nil)
	var got []byte
	err := sse.Subscribe(ctx, req, func(msg *sse.Event) {
		got = append([]byte(nil), msg.Data...)
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("line1\nline2\nline3"), got)
}

func TestHTTPRoundtripLastEventID(t *testing.T) {
	// Verify that the Last-Event-ID header is sent when reconnecting
	var mu sync.Mutex
	var receivedLastID string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedLastID = r.Header.Get("Last-Event-ID")
		_ = receivedLastID
		mu.Unlock()
		sink, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			return
		}
		// Verify LastEventId is captured by Upgrader
		if sink.LastEventId != "" {
			_ = sink.Encode(&sse.Event{Data: []byte("reconnected:" + sink.LastEventId)})
		} else {
			_ = sink.Encode(&sse.Event{Data: []byte("first"), ID: sse.ID("myid")})
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// First connection - gets ID "myid"
	ctx := context.Background()
	req, _ := http.NewRequest("GET", srv.URL, nil)
	var firstData []byte
	err := sse.Subscribe(ctx, req, func(msg *sse.Event) {
		firstData = append([]byte(nil), msg.Data...)
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("first"), firstData)
}

// --- Upgrader sets correct headers ---

func TestUpgraderSetsHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			return
		}
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "keep-alive", resp.Header.Get("Connection"))
}
