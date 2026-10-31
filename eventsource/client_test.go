package eventsource_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/gfx-labs/sse/eventsource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func request(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	require.NoError(t, err)
	return req
}

// Cancelling the context ends iteration while the server keeps the stream open.
func TestClientHonorsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, _ := sse.Upgrade(w, r)
		_ = stream.Send(&sse.Event{Data: []byte("x")})
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	done := make(chan int, 1)
	go func() {
		n := 0
		var c eventsource.Client
		for _, err := range c.Events(ctx, request(t, srv.URL)) {
			assert.NoError(t, err)
			n++
		}
		done <- n
	}()
	select {
	case n := <-done:
		assert.Equal(t, 1, n)
	case <-time.After(5 * time.Second):
		t.Fatal("Events ignored context cancellation")
	}
}

// The default policy stops on permanent errors and yields them.
func TestClientPermanentErrors(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"contentType": func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Content-Type", "text/plain") },
		"status400":   func(w http.ResponseWriter, r *http.Request) { http.Error(w, "bad", http.StatusBadRequest) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			var errs []error
			var c eventsource.Client
			for ev, err := range c.Events(t.Context(), request(t, srv.URL)) {
				require.Nil(t, ev)
				errs = append(errs, err)
			}
			require.Len(t, errs, 1)
			assert.False(t, eventsource.Retryable(errs[0]), "%v", errs[0])
		})
	}
}

// 5xx responses are retried with backoff and Failures counts consecutive failures.
func TestClientRetriesServerErrors(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if conns.Add(1) <= 2 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		stream, _ := sse.Upgrade(w, r)
		_ = stream.Send(&sse.Event{Data: []byte("ok")})
	}))
	defer srv.Close()

	backoff := eventsource.Backoff(time.Millisecond, 100*time.Millisecond)
	var attempts []eventsource.Attempt
	var delays []time.Duration
	c := eventsource.Client{Reconnect: func(a eventsource.Attempt) (time.Duration, bool) {
		attempts = append(attempts, a)
		d, ok := backoff(a)
		delays = append(delays, d)
		return d, ok
	}}
	for ev, err := range c.Events(t.Context(), request(t, srv.URL)) {
		require.NoError(t, err)
		assert.Equal(t, "ok", string(ev.Data))
		break
	}
	require.Len(t, attempts, 2)
	assert.Equal(t, 1, attempts[0].Failures)
	assert.Equal(t, 2, attempts[1].Failures)
	se, ok := errors.AsType[*sse.StatusError](attempts[0].Err)
	require.True(t, ok)
	assert.Equal(t, http.StatusServiceUnavailable, se.Code)
	assert.Greater(t, delays[1], delays[0]/2, "second delay should back off: %v", delays)
	assert.EqualValues(t, 3, conns.Load())
}

// A custom policy sees the server's retry and last id, and its decision to stop ends the stream.
func TestClientCustomReconnect(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conns.Add(1)
		stream, _ := sse.Upgrade(w, r)
		_ = stream.Send(&sse.Event{Data: []byte("x"), ID: []byte("id-" + r.Header.Get("Last-Event-ID")), Retry: 7 * time.Second})
	}))
	defer srv.Close()

	var seen []eventsource.Attempt
	c := eventsource.Client{Reconnect: func(a eventsource.Attempt) (time.Duration, bool) {
		seen = append(seen, a)
		return 0, len(seen) < 3
	}}
	n := 0
	for _, err := range c.Events(t.Context(), request(t, srv.URL)) {
		require.NoError(t, err) // io.EOF from a clean close is not yielded
		n++
	}
	assert.Equal(t, 3, n)
	require.Len(t, seen, 3)
	assert.ErrorIs(t, seen[0].Err, io.EOF)
	assert.Equal(t, 7*time.Second, seen[0].Retry)
	assert.Equal(t, "id-", seen[0].LastEventID)
	assert.Equal(t, "id-id-", seen[1].LastEventID)
	assert.Equal(t, 1, seen[2].Failures)
}

// A 204 response ends the stream without consulting Reconnect.
func TestClientNoContentStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := eventsource.Client{Reconnect: func(eventsource.Attempt) (time.Duration, bool) {
		t.Fatal("Reconnect called after 204")
		return 0, false
	}}
	for ev, err := range c.Events(t.Context(), request(t, srv.URL)) {
		t.Fatalf("unexpected yield: %v %v", ev, err)
	}
}

func TestClientNoReconnectYieldsTransportError(t *testing.T) {
	c := eventsource.Client{Reconnect: eventsource.NoReconnect}
	var errs []error
	for _, err := range c.Events(t.Context(), request(t, "http://127.0.0.1:1")) {
		errs = append(errs, err)
	}
	require.Len(t, errs, 1)
	require.Error(t, errs[0])
}

// Clone keeps an event after the iterator reuses it.
func TestClientCloneKeepsEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, _ := sse.Upgrade(w, r)
		for i := range 3 {
			_ = stream.Send(&sse.Event{Data: []byte(strconv.Itoa(i)), ID: []byte(strconv.Itoa(i))})
		}
	}))
	defer srv.Close()

	var kept []*sse.Event
	c := eventsource.Client{Reconnect: eventsource.NoReconnect}
	for ev, err := range c.Events(t.Context(), request(t, srv.URL)) {
		require.NoError(t, err)
		kept = append(kept, ev.Clone())
	}
	require.Len(t, kept, 3)
	for i, ev := range kept {
		assert.Equal(t, strconv.Itoa(i), string(ev.Data))
		assert.Equal(t, strconv.Itoa(i), string(ev.ID))
	}
}

// The client resumes from a Server's log after a disconnect.
func TestClientResumesFromServer(t *testing.T) {
	es := eventsource.NewServer(0)
	es.Heartbeat = 0
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if conns.Add(1) == 1 {
			// first connection: deliver one event then drop
			ctx, cancel := context.WithCancel(r.Context())
			go func() { time.Sleep(50 * time.Millisecond); cancel() }()
			es.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		es.ServeHTTP(w, r)
	}))
	defer srv.Close()

	es.Publish(&sse.Event{Data: []byte("0")})
	c := eventsource.Client{Reconnect: func(eventsource.Attempt) (time.Duration, bool) {
		es.Publish(&sse.Event{Data: []byte("1")}) // published while disconnected
		return 0, true
	}}
	var got []string
	for ev, err := range c.Events(t.Context(), request(t, srv.URL)) {
		require.NoError(t, err)
		got = append(got, string(ev.Data))
		if len(got) == 2 {
			break
		}
	}
	assert.Equal(t, []string{"0", "1"}, got)
}
