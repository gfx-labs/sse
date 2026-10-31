package eventsource

import (
	"context"
	"errors"
	"io"
	"iter"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/gfx-labs/sse"
)

// Attempt describes the connection that just ended, for a ReconnectFunc.
type Attempt struct {
	// Err is why the connection ended: io.EOF if the server closed the stream,
	// otherwise a transport, *sse.StatusError, content type or decode error.
	Err error
	// Failures is 1 after a connection that delivered events, and grows by one
	// for each following connection that delivered none.
	Failures int
	// Retry is the latest delay sent by the server in a retry field, or zero.
	Retry time.Duration
	// LastEventID is the id the next request will send in Last-Event-ID.
	LastEventID string
}

// ReconnectFunc decides whether to reconnect after a connection ends and how long to wait.
// Returning false stops the stream. Attempt.Err is then yielded unless it is io.EOF.
type ReconnectFunc func(Attempt) (delay time.Duration, ok bool)

// Backoff returns a ReconnectFunc that waits the server's retry delay, or
// initial if the server sent none, doubles it after each consecutive failure up
// to maxDelay, and subtracts up to 20% jitter. It stops on errors that Retryable
// reports as permanent.
func Backoff(initial, maxDelay time.Duration) ReconnectFunc {
	return func(a Attempt) (time.Duration, bool) {
		if !Retryable(a.Err) {
			return 0, false
		}
		d := initial
		if a.Retry > 0 {
			d = a.Retry
		}
		for i := 1; i < a.Failures && d < maxDelay; i++ {
			d *= 2
		}
		d = min(d, max(maxDelay, initial, a.Retry))
		// jitter spreads reconnects after a server restart
		return d - time.Duration(rand.Int64N(int64(d)/5+1)), true
	}
}

// NoReconnect stops the stream after the first connection ends.
func NoReconnect(Attempt) (time.Duration, bool) { return 0, false }

// Retryable reports whether a connection that ended with err may be retried.
// Statuses other than 408, 429 and 5xx, a wrong Content-Type and an
// oversized line are permanent. Other errors, including io.EOF, are retryable.
func Retryable(err error) bool {
	if se, ok := errors.AsType[*sse.StatusError](err); ok {
		c := se.Code
		return c == http.StatusRequestTimeout || c == http.StatusTooManyRequests || c >= 500
	}
	return !errors.Is(err, sse.ErrInvalidContentType) && !errors.Is(err, sse.ErrLineTooLong)
}

// Client consumes an event stream and reconnects as the EventSource spec describes.
// The zero value is ready to use. A Client may be used by multiple goroutines.
type Client struct {
	// HTTPClient sends requests. nil means http.DefaultClient.
	// Its Timeout must be zero or it will cut long-lived streams.
	HTTPClient *http.Client
	// Reconnect decides whether and when to reconnect. nil means Backoff(3s, 30s).
	Reconnect ReconnectFunc
	// Options apply to the decoder of each connection.
	Options []sse.Option
}

var defaultReconnect = Backoff(3*time.Second, 30*time.Second)

// Events sends req and yields events from the response, reconnecting with
// Last-Event-ID after each disconnect while Reconnect allows it.
//
// The sequence ends without an error when ctx is done, when the consumer stops
// iterating, or when the server responds 204 No Content. When Reconnect
// declines, it yields the error that ended the last connection, unless the
// server closed the stream cleanly. The yielded Event is reused for the next
// event; call Clone to keep it.
//
// The request is cloned for every attempt, so it must not have a body.
func (c *Client) Events(ctx context.Context, req *http.Request) iter.Seq2[*sse.Event, error] {
	reconnect := c.Reconnect
	if reconnect == nil {
		reconnect = defaultReconnect
	}
	return func(yield func(*sse.Event, error) bool) {
		var a Attempt
		ev := &sse.Event{}
		for {
			r := req.Clone(ctx)
			if a.LastEventID != "" {
				r.Header.Set("Last-Event-ID", a.LastEventID)
			}
			got, stopped, err := c.connect(r, ev, func(ev *sse.Event) bool {
				if ev.ID != nil {
					a.LastEventID = string(ev.ID)
				}
				if ev.Retry > 0 {
					a.Retry = ev.Retry
				}
				return yield(ev, nil)
			})
			if stopped || ctx.Err() != nil || errors.Is(err, sse.ErrNoContent) {
				return
			}
			if got {
				a.Failures = 0
			}
			a.Failures++
			a.Err = err
			delay, ok := reconnect(a)
			if !ok {
				if !errors.Is(err, io.EOF) {
					yield(nil, err)
				}
				return
			}
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}
}

// connect runs one connection. got reports whether any event arrived and
// stopped whether the consumer ended iteration.
func (c *Client) connect(r *http.Request, ev *sse.Event, yield func(*sse.Event) bool) (got, stopped bool, err error) {
	resp, err := sse.Connect(c.HTTPClient, r)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	d := sse.NewDecoder(resp.Body, c.Options...)
	for {
		if err := d.Decode(ev); err != nil {
			return got, false, err
		}
		got = true
		if !yield(ev) {
			return got, true, nil
		}
	}
}
