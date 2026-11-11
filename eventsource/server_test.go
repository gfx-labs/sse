package eventsource_test

import (
	"context"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/gfx-labs/sse/eventsource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A client resuming with Last-Event-ID gets every later event in order, including
// more events than fit in one write burst.
func TestServerResume(t *testing.T) {
	s := eventsource.NewServer(0)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	require.NoError(t, s.Publish(&sse.Event{Data: []byte("first")}))
	first := readEvents(t, srv.URL, "", 1)[0]
	require.Equal(t, "first", string(first.Data))

	const n = 300
	for range n {
		require.NoError(t, s.Publish(&sse.Event{Data: []byte("next")}))
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
	t.Cleanup(srv.Close)

	require.NoError(t, s.Publish(&sse.Event{Data: []byte("0")}))
	first := readEvents(t, srv.URL, "", 1)[0]
	for i := range 21 {
		require.NoError(t, s.Publish(&sse.Event{Data: []byte(strconv.Itoa(i + 1))}))
	}
	got := readEvents(t, srv.URL, string(first.ID), 5)
	require.Equal(t, []string{"17", "18", "19", "20", "21"}, data(got))
}

// A client without an id from this server gets the latest event and then every
// event published after its response headers arrived, however fast they come.
func TestServerNewSubscriber(t *testing.T) {
	for name, lastID := range map[string]string{
		"none":      "",
		"notNumber": "abc",
		// an id beyond the newest event, such as one issued by another server
		"future": "9000000000000000000",
		"max":    strconv.Itoa(math.MaxInt64),
	} {
		for _, stored := range []int{0, 3} {
			t.Run(name+"/stored"+strconv.Itoa(stored), func(t *testing.T) {
				s := eventsource.NewServer(0)
				srv := httptest.NewServer(s)
				t.Cleanup(srv.Close)

				var want []string
				for i := range stored {
					require.NoError(t, s.Publish(&sse.Event{Data: []byte("old" + strconv.Itoa(i))}))
					want = []string{"old" + strconv.Itoa(i)}
				}
				c := dial(t, http.DefaultClient, srv.URL, lastID)
				for i := range 10 {
					require.NoError(t, s.Publish(&sse.Event{Data: []byte(strconv.Itoa(i))}))
					want = append(want, strconv.Itoa(i))
				}
				assert.Equal(t, want, data(c.events(len(want))))
			})
		}
	}
}

// An event that cannot be encoded is rejected instead of ending every stream that reaches it.
func TestServerPublishRejectsUnencodable(t *testing.T) {
	s := eventsource.NewServer(0)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	require.NoError(t, s.Publish(&sse.Event{Data: []byte("a")}))
	c := dial(t, http.DefaultClient, srv.URL, "")
	require.ErrorIs(t, s.Publish(&sse.Event{Event: "bad\nname", Data: []byte("b")}), sse.ErrInvalidField)
	// the server assigns the id, so the caller's is never sent
	require.NoError(t, s.Publish(&sse.Event{ID: []byte("x\ny"), Data: []byte("c")}))
	assert.Equal(t, []string{"a", "c"}, data(c.events(2)))
}

// Idle streams get heartbeat comments, which the decoder skips.
func TestServerHeartbeat(t *testing.T) {
	s := eventsource.NewServer(0)
	s.Heartbeat = 10 * time.Millisecond
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

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

// Close ends open streams, so http.Server.Shutdown can finish, and turns away new ones.
func TestServerClose(t *testing.T) {
	s := eventsource.NewServer(0)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	srv.Config.RegisterOnShutdown(s.Close)

	require.NoError(t, s.Publish(&sse.Event{Data: []byte("a")}))
	c := dial(t, http.DefaultClient, srv.URL, "")
	require.Len(t, c.events(1), 1)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Config.Shutdown(ctx))

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// A client that stops reading does not hold its handler past WriteTimeout.
func TestServerWriteTimeoutStalledClient(t *testing.T) {
	s := eventsource.NewServer(32)
	s.Heartbeat = 0
	s.WriteTimeout = 100 * time.Millisecond
	returned := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(returned)
		s.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	require.NoError(t, err)

	// more than the socket buffers hold, so a write blocks
	big := make([]byte, 1<<20)
	for range 32 {
		require.NoError(t, s.Publish(&sse.Event{Data: big}))
	}
	select {
	case <-returned:
	case <-time.After(10 * time.Second):
		t.Fatal("handler still blocked on a client that is not reading")
	}
}

// WriteTimeout bounds each write, not the stream: HTTP/2 resets a stream whose
// deadline passes, so a deadline left over from the last write would end an idle one.
func TestServerWriteTimeoutIdleStream(t *testing.T) {
	s := eventsource.NewServer(0)
	s.Heartbeat = 0
	s.WriteTimeout = 100 * time.Millisecond
	srv := httptest.NewUnstartedServer(s)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	require.NoError(t, s.Publish(&sse.Event{Data: []byte("a")}))
	c := dial(t, srv.Client(), srv.URL, "")
	require.Equal(t, 2, c.resp.ProtoMajor)
	require.Equal(t, []string{"a"}, data(c.events(1)))
	time.Sleep(300 * time.Millisecond)
	require.NoError(t, s.Publish(&sse.Event{Data: []byte("b")}))
	assert.Equal(t, []string{"b"}, data(c.events(1)))
}

// Concurrent publishing and subscribing must be race free (run with -race).
func TestServerConcurrent(t *testing.T) {
	s := eventsource.NewServer(100)
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	require.NoError(t, s.Publish(&sse.Event{Data: []byte("x")}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			assert.NoError(t, s.Publish(&sse.Event{Data: []byte("x")}))
		}
	}()
	for range 4 {
		readEvents(t, srv.URL, "", 1)
	}
	<-done
}

// conn is one open stream.
type conn struct {
	t    *testing.T
	resp *http.Response
	dec  *sse.Decoder
}

// dial opens a stream and returns once the response headers have arrived. The
// stream is closed in a cleanup, which runs before a server cleanup registered
// earlier, so httptest.Server.Close does not wait on it.
func dial(t *testing.T, hc *http.Client, url, lastID string) *conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	require.NoError(t, err)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := hc.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return &conn{t: t, resp: resp, dec: sse.NewDecoder(resp.Body)}
}

// events reads the next n events.
func (c *conn) events(n int) []*sse.Event {
	c.t.Helper()
	var out []*sse.Event
	var ev sse.Event
	for len(out) < n {
		require.NoError(c.t, c.dec.Decode(&ev))
		out = append(out, ev.Clone())
	}
	return out
}

func data(evs []*sse.Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, string(e.Data))
	}
	return out
}

func readEvents(t *testing.T, url, lastID string, n int) []*sse.Event {
	t.Helper()
	c := dial(t, http.DefaultClient, url, lastID)
	defer func() { _ = c.resp.Body.Close() }()
	return c.events(n)
}
