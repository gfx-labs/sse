package sse_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connect(t *testing.T, h http.HandlerFunc) (*http.Response, error) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(t.Context(), "GET", srv.URL, nil)
	require.NoError(t, err)
	return sse.Connect(nil, req)
}

func TestConnectSetsHeadersAndStreams(t *testing.T) {
	resp, err := connect(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		stream, _ := sse.Upgrade(w, r)
		_ = stream.Send(&sse.Event{Data: []byte("x")})
	})
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var got []string
	for ev, err := range sse.Events(resp.Body) {
		require.NoError(t, err)
		got = append(got, string(ev.Data))
	}
	assert.Equal(t, []string{"x"}, got)
}

func connectErr(t *testing.T, h http.HandlerFunc) error {
	t.Helper()
	resp, err := connect(t, h)
	if err == nil {
		_ = resp.Body.Close()
	}
	return err
}

func TestConnectErrors(t *testing.T) {
	err := connectErr(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
	})
	require.ErrorIs(t, err, sse.ErrInvalidContentType)

	err = connectErr(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	require.ErrorIs(t, err, sse.ErrNoContent)

	err = connectErr(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", http.StatusForbidden) })
	se, ok := errors.AsType[*sse.StatusError](err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, http.StatusForbidden, se.Code)
	assert.Contains(t, string(se.Body), "nope")
}
