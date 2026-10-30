package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math/rand/v2"
	"mime"
	"net/http"
	"time"
)

const (
	defaultRetry    = 3 * time.Second
	defaultMaxRetry = 30 * time.Second
)

// Client consumes event streams and reconnects as the spec describes.
// The zero value is ready to use.
type Client struct {
	// HTTPClient sends requests. nil means http.DefaultClient.
	// Its Timeout must be zero or it will cut long-lived streams.
	HTTPClient *http.Client
	// Retry is the initial reconnect delay when the server has not sent one. Defaults to 3s.
	Retry time.Duration
	// MaxRetry caps the delay between consecutive failed attempts, which doubles
	// after each failure. Defaults to 30s.
	MaxRetry time.Duration
	// DisableReconnect stops the stream at the first disconnect.
	DisableReconnect bool
	// OnReconnect, if set, is called before waiting to reconnect, with the error
	// that ended the previous attempt (io.EOF if the server closed the stream).
	OnReconnect func(err error, delay time.Duration)
	// Options apply to the decoder of each connection.
	Options []Option
}

// Events sends req and yields events from the response, reconnecting with
// Last-Event-ID after disconnects until ctx is done.
//
// The sequence ends without an error when ctx is cancelled, when the server
// responds 204 No Content, or, with DisableReconnect, when the server closes
// the stream. It yields a final error for responses that are not retried: any
// status other than 200, 408, 429 and 5xx, and a Content-Type other than
// text/event-stream. The yielded Event is reused for the next event.
//
// The request is cloned for every attempt, so it must not have a body.
func (c *Client) Events(ctx context.Context, req *http.Request) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		var (
			lastID   string
			retry    = c.Retry
			failures int
		)
		if retry <= 0 {
			retry = defaultRetry
		}
		for {
			r := req.Clone(ctx)
			r.Header.Set("Accept", "text/event-stream")
			r.Header.Set("Cache-Control", "no-cache")
			if lastID != "" {
				r.Header.Set("Last-Event-ID", lastID)
			}
			got, stop, err := c.attempt(r, func(ev *Event) bool {
				if ev.ID != nil {
					lastID = string(ev.ID)
				}
				if ev.Retry > 0 {
					retry = ev.Retry
				}
				return yield(ev, nil)
			})
			switch {
			case stop || ctx.Err() != nil:
				return
			case errors.Is(err, errPermanent):
				yield(nil, errors.Unwrap(err))
				return
			case c.DisableReconnect:
				if !errors.Is(err, io.EOF) {
					yield(nil, err)
				}
				return
			}
			if got {
				failures = 0
			} else {
				failures++
			}
			delay := c.backoff(retry, failures)
			if c.OnReconnect != nil {
				c.OnReconnect(err, delay)
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

var errPermanent = errors.New("permanent")

type permanentError struct{ err error }

func (p permanentError) Error() string        { return p.err.Error() }
func (p permanentError) Unwrap() error        { return p.err }
func (p permanentError) Is(target error) bool { return target == errPermanent }

// attempt runs one connection. got reports whether any event was received and
// stop whether the consumer or server ended the stream.
func (c *Client) attempt(r *http.Request, yield func(*Event) bool) (got, stop bool, err error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(r)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch s := resp.StatusCode; {
	case s == http.StatusNoContent:
		// the spec tells clients to stop reconnecting
		return false, true, nil
	case s != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		err := fmt.Errorf("sse: %s: %s", resp.Status, body)
		if s == http.StatusRequestTimeout || s == http.StatusTooManyRequests || s >= 500 {
			return false, false, err
		}
		return false, false, permanentError{err}
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "text/event-stream" {
		return false, false, permanentError{fmt.Errorf("%w: %q", ErrInvalidContentType, resp.Header.Get("Content-Type"))}
	}

	d := NewDecoder(resp.Body, c.Options...)
	ev := &Event{}
	for {
		if err := d.Decode(ev); err != nil {
			if errors.Is(err, ErrLineTooLong) {
				return got, false, permanentError{err}
			}
			return got, false, err
		}
		got = true
		if !yield(ev) {
			return got, true, nil
		}
	}
}

func (c *Client) backoff(retry time.Duration, failures int) time.Duration {
	limit := c.MaxRetry
	if limit <= 0 {
		limit = defaultMaxRetry
	}
	d := retry
	for i := 1; i < failures && d < limit; i++ {
		d *= 2
	}
	d = min(d, max(limit, retry))
	// jitter of up to 20% spreads reconnects after a server restart
	return d - time.Duration(rand.Int64N(int64(d)/5+1))
}
