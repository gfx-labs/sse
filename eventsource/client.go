package eventsource

import (
	"context"
	"errors"
	"io"
	"iter"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gfx-labs/sse"
)

// Attempt describes the connection that just ended, for a ReconnectFunc.
type Attempt struct {
	// Err is why the connection ended: io.EOF if the server closed the stream,
	// otherwise a transport, *sse.StatusError, content type or decode error.
	Err error
	// Failures counts consecutive connections that ended before the server
	// answered with an event stream. It is 1 after a stream that was established,
	// even one that carried no events.
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
// to maxDelay, and subtracts up to 20% jitter. A starting delay above maxDelay
// is used as is, so a server may ask for a longer wait than maxDelay. Negative
// durations count as zero. It stops on errors that Retryable reports as
// permanent.
func Backoff(initial, maxDelay time.Duration) ReconnectFunc {
	initial = max(initial, 0)
	return func(a Attempt) (time.Duration, bool) {
		if !Retryable(a.Err) {
			return 0, false
		}
		d := initial
		if a.Retry > 0 {
			d = a.Retry
		}
		for i := 1; i < a.Failures && d > 0 && d < maxDelay; i++ {
			// comparing against half the limit keeps the doubling from overflowing
			if d > maxDelay/2 {
				d = maxDelay
			} else {
				d *= 2
			}
		}
		// jitter spreads reconnects after a server restart
		return d - time.Duration(rand.Int64N(int64(d)/5+1)), true
	}
}

// NoReconnect stops the stream after the first connection ends.
func NoReconnect(Attempt) (time.Duration, bool) { return 0, false }

// Retryable reports whether a connection that ended with err may be retried.
// Statuses other than 408, 429 and 5xx, a wrong Content-Type, an oversized
// line and a request that net/http refuses to send are permanent. Other
// errors, including io.EOF, are retryable.
func Retryable(err error) bool {
	if se, ok := errors.AsType[*sse.StatusError](err); ok {
		c := se.Code
		return c == http.StatusRequestTimeout || c == http.StatusTooManyRequests || c >= 500
	}
	if ue, ok := errors.AsType[*url.Error](err); ok && unsendable(ue.Err) {
		return false
	}
	return !errors.Is(err, sse.ErrInvalidContentType) && !errors.Is(err, sse.ErrLineTooLong)
}

// unsendable reports whether net/http rejected the request before using the
// network, which no retry can fix. It has no error type for these, so they are
// matched by message. An unmatched message is retried like any network error.
func unsendable(err error) bool {
	msg := err.Error()
	for _, s := range []string{
		"unsupported protocol scheme",
		"http: no Host in request URL",
		"http: nil Request.URL",
		"net/http: invalid header field",
		"net/http: invalid method",
	} {
		if strings.HasPrefix(msg, s) {
			return true
		}
	}
	return false
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
// Last-Event-ID after each disconnect while Reconnect allows it. A
// Last-Event-ID header on req is the starting point.
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
		lastID := []byte(req.Header.Get("Last-Event-ID"))
		ev := &sse.Event{}
		for {
			r := req.Clone(ctx)
			if r.Header == nil {
				r.Header = http.Header{}
			}
			if len(lastID) > 0 {
				r.Header.Set("Last-Event-ID", string(lastID))
			} else {
				// the server reset the id, so the caller's header must not come back
				r.Header.Del("Last-Event-ID")
			}
			established, stopped, err := c.connect(r, ev, func(ev *sse.Event) bool {
				// net/http refuses to send control characters, so such an id is
				// ignored, as the spec ignores one containing NUL
				if ev.ID != nil && validHeaderValue(ev.ID) {
					lastID = append(lastID[:0], ev.ID...)
				}
				if ev.Retry > 0 {
					a.Retry = ev.Retry
				}
				return yield(ev, nil)
			})
			if stopped || ctx.Err() != nil || errors.Is(err, sse.ErrNoContent) {
				return
			}
			if established {
				a.Failures = 0
			}
			a.Failures++
			a.Err = err
			a.LastEventID = string(lastID)
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

// connect runs one connection. established reports whether the server answered
// with an event stream and stopped whether the consumer ended iteration.
func (c *Client) connect(r *http.Request, ev *sse.Event, yield func(*sse.Event) bool) (established, stopped bool, err error) {
	resp, err := sse.Connect(c.HTTPClient, r)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	d := sse.NewDecoder(resp.Body, c.Options...)
	for {
		if err := d.Decode(ev); err != nil {
			return true, false, err
		}
		if !yield(ev) {
			return true, true, nil
		}
	}
}

// validHeaderValue reports whether net/http accepts b as a header value.
func validHeaderValue(b []byte) bool {
	for _, c := range b {
		if c < ' ' && c != '\t' || c == 0x7f {
			return false
		}
	}
	return true
}
