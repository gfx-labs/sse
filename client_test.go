package sse_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)

	done := make(chan int, 1)
	go func() {
		n := 0
		var c sse.Client
		for _, err := range c.Events(ctx, req) {
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

func TestClientRejectsWrongContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("data: x\n\n"))
	}))
	defer srv.Close()
	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)
	var c sse.Client
	for ev, err := range c.Events(t.Context(), req) {
		require.Nil(t, ev)
		require.ErrorIs(t, err, sse.ErrInvalidContentType)
	}
}

// 5xx responses are retried with backoff; the failure count resets after a successful connection.
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

	var delays []time.Duration
	c := sse.Client{
		Retry:       time.Millisecond,
		MaxRetry:    100 * time.Millisecond,
		OnReconnect: func(_ error, d time.Duration) { delays = append(delays, d) },
	}
	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)
	for ev, err := range c.Events(t.Context(), req) {
		require.NoError(t, err)
		assert.Equal(t, "ok", string(ev.Data))
		break
	}
	require.Len(t, delays, 2)
	assert.Greater(t, delays[1], delays[0]/2, "second delay should back off: %v", delays)
	assert.EqualValues(t, 3, conns.Load())
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

	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)
	var kept []*sse.Event
	c := sse.Client{DisableReconnect: true}
	for ev, err := range c.Events(t.Context(), req) {
		require.NoError(t, err)
		kept = append(kept, ev.Clone())
	}
	require.Len(t, kept, 3)
	for i, ev := range kept {
		assert.Equal(t, strconv.Itoa(i), string(ev.Data))
		assert.Equal(t, strconv.Itoa(i), string(ev.ID))
	}
}
