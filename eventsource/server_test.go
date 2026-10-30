package eventsource_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/gfx-labs/sse/eventsource"
	"github.com/stretchr/testify/require"
)

// A client resuming with Last-Event-ID gets every later event in order, including
// more events than fit in one write burst.
func TestServerResume(t *testing.T) {
	s := eventsource.NewServer(sse.DefaultUpgrader)
	srv := httptest.NewServer(s)
	defer srv.Close()

	require.NoError(t, s.Encode(&sse.Event{Data: []byte("first")}))

	// learn the first id from a fresh subscriber
	first := readEvents(t, srv.URL, "", 1)[0]
	require.Equal(t, "first", string(first.Data))

	const n = 300
	for range n {
		require.NoError(t, s.Encode(&sse.Event{Data: []byte("next")}))
	}
	got := readEvents(t, srv.URL, string(*first.ID), n)
	require.Len(t, got, n)
	prev := string(*first.ID)
	for _, e := range got {
		require.Greater(t, string(*e.ID), prev)
		prev = string(*e.ID)
	}
}

// Concurrent publishing and subscribing must be race free (run with -race).
func TestServerConcurrent(t *testing.T) {
	s := eventsource.NewServer(sse.DefaultUpgrader)
	srv := httptest.NewServer(s)
	defer srv.Close()
	require.NoError(t, s.Encode(&sse.Event{Data: []byte("x")}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			_ = s.Encode(&sse.Event{Data: []byte("x")})
		}
	}()
	for range 4 {
		readEvents(t, srv.URL, "", 1)
	}
	<-done
}

func readEvents(t *testing.T, url, lastID string, n int) []sse.Event {
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
	dec := sse.NewDecoder(sse.NewReader(bufio.NewReader(resp.Body)))
	var out []sse.Event
	for len(out) < n {
		e := &sse.Event{}
		require.NoError(t, dec.Decode(e))
		if strings.TrimSpace(string(e.Data)) != "" {
			out = append(out, *e)
		}
	}
	return out
}
