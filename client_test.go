package sse_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/require"
)

func TestClientBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for i := range 5 {
			require.NoError(t, sink.Encode(&sse.Event{Data: []byte(strconv.Itoa(i))}))
		}
	}))
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)

	idx := 0
	err = sse.Subscribe(t.Context(), req, func(msg *sse.Event) {
		val, _ := strconv.Atoi(string(msg.Data))
		require.Equal(t, idx, val)
		idx++
	})
	require.NoError(t, err)
	require.Equal(t, 5, idx)
}

// Subscribe must return when its context is cancelled while the server keeps the stream open.
func TestSubscribeHonorsContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, _ := sse.DefaultUpgrader.Upgrade(w, r)
		_ = sink.Encode(&sse.Event{Data: []byte("x")})
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- sse.Subscribe(ctx, req, func(*sse.Event) {}) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("Subscribe ignored context cancellation")
	}
}

func TestSubscribeRejectsWrongContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("data: x\n\n"))
	}))
	defer srv.Close()
	req, err := http.NewRequest("GET", srv.URL, nil)
	require.NoError(t, err)
	err = sse.Subscribe(t.Context(), req, func(*sse.Event) {})
	require.ErrorIs(t, err, sse.ErrInvalidContentType)
}

// SubscribeWithRetry reconnects with the last seen id and stops when the context ends.
func TestSubscribeWithRetryResumes(t *testing.T) {
	var conns atomic.Int32
	lastIDs := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastIDs <- r.Header.Get("Last-Event-ID")
		n := conns.Add(1)
		sink, _ := sse.DefaultUpgrader.Upgrade(w, r)
		_ = sink.Encode(&sse.Event{Data: []byte("x"), ID: sse.ID(strconv.Itoa(int(n)))})
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	got := 0
	err := sse.SubscribeWithRetry(ctx, func() *http.Request {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		return req
	}, func(*sse.Event) {
		got++
		if got == 2 {
			cancel()
		}
	}, backoff.NewConstantBackOff(time.Millisecond))

	require.True(t, errors.Is(err, context.Canceled), "err: %v", err)
	require.Equal(t, "", <-lastIDs)
	require.Equal(t, "1", <-lastIDs)
}

func TestEncoderRejectsLineBreakInField(t *testing.T) {
	var b strings.Builder
	err := sse.NewEncoder(&b).Encode(&sse.Event{Event: []byte("a\ndata: injected"), Data: []byte("x")})
	require.ErrorIs(t, err, sse.ErrInvalidField)
	require.NotContains(t, b.String(), "injected")
}
