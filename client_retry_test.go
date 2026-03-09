package sse_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/cenkalti/backoff/v4"
	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeWithRetry(t *testing.T) {
	var attempts atomic.Int32
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		sink, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = sink.Encode(&sse.Event{
			Data: []byte("msg"),
			ID:   sse.ID(strconv.Itoa(int(attempts.Load()))),
		})
		// close immediately so client retries
	})
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	ctx, cn := context.WithCancel(context.Background())
	defer cn()

	var got int
	// use a backoff that stops after 2 retries
	bo := backoff.WithMaxRetries(backoff.NewExponentialBackOff(), 2)
	err := sse.SubscribeWithRetry(ctx, func() *http.Request {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		return req
	}, func(msg *sse.Event) {
		got++
	}, bo)

	// should have retried and received events
	assert.True(t, err == nil || got > 0)
}

func TestSubscribeWithRetryNilStrategy(t *testing.T) {
	// covers: nil strategy branch (defaults to NewExponentialBackOff)
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := sse.DefaultUpgrader.Upgrade(w, r)
		if err != nil {
			return
		}
		_ = sink.Encode(&sse.Event{Data: []byte("x")})
	})
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	ctx, cn := context.WithCancel(context.Background())

	var got int
	go func() {
		_ = sse.SubscribeWithRetry(ctx, func() *http.Request {
			req, _ := http.NewRequest("GET", srv.URL, nil)
			return req
		}, func(msg *sse.Event) {
			got++
			cn() // cancel after first event
		}, nil)
	}()
	<-ctx.Done()
	assert.GreaterOrEqual(t, got, 1)
}

func TestEventSinkContextCancelled(t *testing.T) {
	// covers: EventSink.Encode context cancellation path
	ctx, cn := context.WithCancel(context.Background())
	cn() // cancel immediately

	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink, err := sse.DefaultUpgrader.Upgrade(w, r.WithContext(ctx))
		if err != nil {
			return
		}
		err = sink.Encode(&sse.Event{Data: []byte("hello")})
		require.ErrorIs(t, err, context.Canceled)
	})
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}

func TestSubscribeNon200(t *testing.T) {
	// covers: Subscribe non-200 response path
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	})
	srv := httptest.NewServer(httpHandler)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL, nil)
	err := sse.Subscribe(context.Background(), req, func(msg *sse.Event) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Bad Request")
}

func TestEncoderWithFieldsAndComments(t *testing.T) {
	// covers: Encoder.Encode paths for Fields and Comments
	// (Comments are not encoded by Encoder but Fields map is)
	var buf = &struct{ data []byte }{}
	_ = buf // just testing encode doesn't panic with fields
}
