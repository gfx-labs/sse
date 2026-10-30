package eventsource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/gfx-labs/sse/eventsource"
	"github.com/stretchr/testify/require"
)

// A client resuming with Last-Event-ID gets every later event in order, including
// more events than fit in one write burst.
func TestServerResume(t *testing.T) {
	s := eventsource.NewServer(0)
	srv := httptest.NewServer(s)
	defer srv.Close()

	s.Publish(&sse.Event{Data: []byte("first")})
	first := readEvents(t, srv.URL, "", 1)[0]
	require.Equal(t, "first", string(first.Data))

	const n = 300
	for range n {
		s.Publish(&sse.Event{Data: []byte("next")})
	}
	got := readEvents(t, srv.URL, string(first.ID), n)
	require.Len(t, got, n)
	prev := string(first.ID)
	for _, e := range got {
		require.Greater(t, string(e.ID), prev)
		prev = string(e.ID)
	}
}

// With a log size limit, a client resuming from an evicted id gets only the retained events.
func TestServerLogLimit(t *testing.T) {
	s := eventsource.NewServer(5)
	srv := httptest.NewServer(s)
	defer srv.Close()

	s.Publish(&sse.Event{Data: []byte("0")})
	first := readEvents(t, srv.URL, "", 1)[0]
	for range 20 {
		s.Publish(&sse.Event{Data: []byte("x")})
	}
	s.Publish(&sse.Event{Data: []byte("last")})
	got := readEvents(t, srv.URL, string(first.ID), 5)
	require.Equal(t, "last", string(got[4].Data))
}

// Idle streams get heartbeat comments, which the decoder skips.
func TestServerHeartbeat(t *testing.T) {
	s := eventsource.NewServer(0)
	s.Heartbeat = 10 * time.Millisecond
	srv := httptest.NewServer(s)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	buf := make([]byte, 3)
	_, err = resp.Body.Read(buf)
	require.NoError(t, err)
	require.Equal(t, ":\n", string(buf[:2]))
}

// Concurrent publishing and subscribing must be race free (run with -race).
func TestServerConcurrent(t *testing.T) {
	s := eventsource.NewServer(100)
	srv := httptest.NewServer(s)
	defer srv.Close()
	s.Publish(&sse.Event{Data: []byte("x")})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			s.Publish(&sse.Event{Data: []byte("x")})
		}
	}()
	for range 4 {
		readEvents(t, srv.URL, "", 1)
	}
	<-done
}

func readEvents(t *testing.T, url, lastID string, n int) []*sse.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	require.NoError(t, err)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out []*sse.Event
	for ev, err := range sse.Events(resp.Body) {
		require.NoError(t, err)
		out = append(out, ev.Clone())
		if len(out) == n {
			break
		}
	}
	return out
}
