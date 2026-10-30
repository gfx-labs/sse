package e2e

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Encoder → Decoder roundtrip ---

func roundtrip(t *testing.T, in ...*sse.Event) []*sse.Event {
	t.Helper()
	var buf bytes.Buffer
	enc := sse.NewEncoder(&buf)
	for _, ev := range in {
		require.NoError(t, enc.Encode(ev))
	}
	var out []*sse.Event
	for ev, err := range sse.Events(&buf) {
		require.NoError(t, err)
		out = append(out, ev.Clone())
	}
	return out
}

func TestRoundtrip(t *testing.T) {
	for name, ev := range map[string]*sse.Event{
		"data":      {Data: []byte("hello world")},
		"event":     {Event: "notification", Data: []byte("you have mail")},
		"id":        {Data: []byte("msg"), ID: []byte("99")},
		"emptyID":   {Data: []byte("msg"), ID: []byte{}},
		"retry":     {Data: []byte("payload"), Retry: 3 * time.Second},
		"fields":    {Data: []byte("payload"), Fields: map[string][]byte{"custom": []byte("v")}},
		"multiLine": {Data: []byte("line1\nline2\nline3")},
		"emptyData": {Data: []byte{}},
		"noData":    {Event: "ping"},
		"comments":  {Data: []byte("x"), Comments: [][]byte{[]byte("note")}},
	} {
		t.Run(name, func(t *testing.T) {
			got := roundtrip(t, ev)
			require.Len(t, got, 1)
			assert.Equal(t, ev.Event, got[0].Event)
			assert.Equal(t, string(ev.Data), string(got[0].Data))
			assert.Equal(t, ev.ID, got[0].ID)
			assert.Equal(t, ev.Retry, got[0].Retry)
			for k, v := range ev.Fields {
				assert.Equal(t, v, got[0].Fields[k])
			}
			assert.Len(t, got[0].Comments, len(ev.Comments))
		})
	}
}

func TestRoundtripMultipleEvents(t *testing.T) {
	var in []*sse.Event
	for i := range 10 {
		in = append(in, &sse.Event{Data: []byte(strconv.Itoa(i)), ID: []byte(strconv.Itoa(i))})
	}
	got := roundtrip(t, in...)
	require.Len(t, got, 10)
	for i, ev := range got {
		assert.Equal(t, strconv.Itoa(i), string(ev.Data))
		assert.Equal(t, strconv.Itoa(i), string(ev.ID))
	}
}

// --- Full HTTP roundtrip: Upgrade → Client ---

func collect(t *testing.T, c sse.Client, url string, n int) []*sse.Event {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	var out []*sse.Event
	for ev, err := range c.Events(t.Context(), req) {
		require.NoError(t, err)
		out = append(out, ev.Clone())
		if len(out) == n {
			break
		}
	}
	return out
}

func TestHTTPRoundtrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := sse.Upgrade(w, r)
		if err != nil {
			return
		}
		for i := range 5 {
			if stream.Send(&sse.Event{Event: "count", Data: []byte(strconv.Itoa(i)), ID: []byte(strconv.Itoa(i))}) != nil {
				return
			}
		}
	}))
	defer srv.Close()

	got := collect(t, sse.Client{DisableReconnect: true}, srv.URL, -1)
	require.Len(t, got, 5)
	for i, ev := range got {
		assert.Equal(t, "count", ev.Event)
		assert.Equal(t, strconv.Itoa(i), string(ev.Data))
		assert.Equal(t, strconv.Itoa(i), string(ev.ID))
	}
}

// The client reconnects after the server closes the stream, sends Last-Event-ID,
// and uses the server's retry delay.
func TestHTTPReconnectLastEventID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, err := sse.Upgrade(w, r)
		if err != nil {
			return
		}
		if stream.LastEventID != "" {
			_ = stream.Send(&sse.Event{Data: []byte("reconnected:" + stream.LastEventID)})
			return
		}
		_ = stream.Send(&sse.Event{Data: []byte("first"), ID: []byte("myid"), Retry: time.Millisecond})
	}))
	defer srv.Close()

	var reconnects int
	c := sse.Client{OnReconnect: func(error, time.Duration) { reconnects++ }}
	got := collect(t, c, srv.URL, 2)
	require.Len(t, got, 2)
	assert.Equal(t, "first", string(got[0].Data))
	assert.Equal(t, "reconnected:myid", string(got[1].Data))
	assert.Equal(t, 1, reconnects)
}

// A 204 response tells the client to stop without an error.
func TestHTTPNoContentStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	assert.Empty(t, collect(t, sse.Client{}, srv.URL, -1))
}

func TestUpgradeSetsHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = sse.Upgrade(w, r)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "keep-alive", resp.Header.Get("Connection"))
}
