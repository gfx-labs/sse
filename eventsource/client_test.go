package eventsource_test

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
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

	require.NoError(t, es.Publish(&sse.Event{Data: []byte("0")}))
	c := eventsource.Client{Reconnect: func(eventsource.Attempt) (time.Duration, bool) {
		// published while disconnected
		assert.NoError(t, es.Publish(&sse.Event{Data: []byte("1")}))
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

// Last-Event-ID follows the stream: the caller's header is the starting point,
// an empty id clears it, and an id net/http cannot send is ignored.
func TestClientLastEventID(t *testing.T) {
	for name, tc := range map[string]struct {
		initial string
		ids     []string // id sent by each connection in turn
		want    []string // Last-Event-ID header received by each connection
	}{
		"callerHeaderIsKept":     {initial: "42", ids: []string{"", ""}, want: []string{"42", "42", "42"}},
		"resetClearsCallerValue": {initial: "42", ids: []string{"reset", "7"}, want: []string{"42", "", "7"}},
		"controlCharIgnored":     {ids: []string{"5", "a\x01b"}, want: []string{"", "5", "5"}},
	} {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var got []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				n := len(got)
				got = append(got, r.Header.Get("Last-Event-ID"))
				mu.Unlock()
				stream, _ := sse.Upgrade(w, r)
				ev := &sse.Event{Data: []byte("x")}
				if n < len(tc.ids) {
					switch id := tc.ids[n]; id {
					case "":
					case "reset":
						ev.ID = []byte{}
					default:
						ev.ID = []byte(id)
					}
				}
				_ = stream.Send(ev)
			}))
			defer srv.Close()

			req := request(t, srv.URL)
			if tc.initial != "" {
				req.Header.Set("Last-Event-ID", tc.initial)
			}
			var seen []string
			c := eventsource.Client{Reconnect: func(a eventsource.Attempt) (time.Duration, bool) {
				seen = append(seen, a.LastEventID)
				return 0, len(seen) < len(tc.want)
			}}
			for _, err := range c.Events(t.Context(), req) {
				require.NoError(t, err)
			}
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, tc.want, got)
			// the policy sees the id each following request sends
			assert.Equal(t, tc.want[1:], seen[:len(tc.want)-1])
		})
	}
}

// A stream that carried only keep-alives was still a working connection, so
// its end does not count toward the backoff.
func TestClientEstablishedStreamResetsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stream, _ := sse.Upgrade(w, r)
		_ = stream.Comment("")
	}))
	defer srv.Close()

	var failures []int
	c := eventsource.Client{Reconnect: func(a eventsource.Attempt) (time.Duration, bool) {
		failures = append(failures, a.Failures)
		return 0, len(failures) < 3
	}}
	for _, err := range c.Events(t.Context(), request(t, srv.URL)) {
		require.NoError(t, err)
	}
	assert.Equal(t, []int{1, 1, 1}, failures)
}

// A request net/http refuses to send fails the same way every time, so the
// default policy reports it instead of retrying forever.
func TestClientUnsendableRequestStops(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var errs []error
	var c eventsource.Client
	// no scheme: "localhost" parses as the scheme
	for _, err := range c.Events(ctx, request(t, "localhost:8080/sse")) {
		errs = append(errs, err)
	}
	require.Len(t, errs, 1)
	require.Error(t, errs[0])
}

// Backoff stays within its bounds for inputs that used to overflow or panic.
func TestBackoffBounds(t *testing.T) {
	a := eventsource.Attempt{Err: io.EOF, Failures: 100}
	for name, tc := range map[string]struct {
		initial, maxDelay, retry time.Duration
		lo, hi                   time.Duration
	}{
		"doublingStopsAtMax": {initial: time.Hour, maxDelay: math.MaxInt64, lo: math.MaxInt64 / 5 * 4, hi: math.MaxInt64},
		"negativeInitial":    {initial: -time.Second, maxDelay: time.Minute, lo: 0, hi: 0},
		// a server may ask for a longer wait than maxDelay
		"serverRetryAboveMax": {initial: time.Second, maxDelay: time.Minute, retry: time.Hour, lo: 48 * time.Minute, hi: time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			a.Retry = tc.retry
			d, ok := eventsource.Backoff(tc.initial, tc.maxDelay)(a)
			require.True(t, ok)
			assert.GreaterOrEqual(t, d, tc.lo)
			assert.LessOrEqual(t, d, tc.hi)
		})
	}
}
