package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"

	"github.com/cenkalti/backoff/v4"
)

// Subscribe to a data stream with context. The handler is called on every event.
// It returns nil when the server closes the stream and an error on any failure.
// The Event passed to handler is reused, so copy it to keep it past the call.
// Pass WithHTTPClient to use a client other than http.DefaultClient.
func Subscribe(ctx context.Context, r *http.Request, handler func(msg *Event), opts ...Option) error {
	o := newOptions(opts)
	req := AddSSEHeaders(ctx, r, nil)
	resp, err := o.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		bts, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return backoffError(resp.StatusCode, fmt.Errorf("%s: %s", http.StatusText(resp.StatusCode), bts))
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "text/event-stream" {
		return backoff.Permanent(fmt.Errorf("%w: %q", ErrInvalidContentType, resp.Header.Get("Content-Type")))
	}
	dec := NewDecoder(NewReader(resp.Body, opts...))
	msg := &Event{}
	for {
		// on any sort of error while decoding, we close the connection.
		err := dec.Decode(msg)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if msg.hasContent() {
			handler(msg)
		}
	}
}

// backoffError marks client errors other than 408 and 429 as permanent so retries stop.
func backoffError(status int, err error) error {
	if status >= 400 && status < 500 && status != http.StatusRequestTimeout && status != http.StatusTooManyRequests {
		return backoff.Permanent(err)
	}
	return err
}

// SubscribeWithRetry resubscribes using strategy, or an exponential backoff when strategy is nil.
// It sends the last seen event id on reconnect and stops when ctx is done.
func SubscribeWithRetry(
	ctx context.Context,
	factory func() *http.Request,
	handler func(msg *Event),
	strategy backoff.BackOff,
	opts ...Option,
) error {
	var mu sync.Mutex
	var lastID *string
	wrapHandler := func(msg *Event) {
		if msg.ID != nil {
			idString := string(*msg.ID)
			mu.Lock()
			lastID = &idString
			mu.Unlock()
		}
		handler(msg)
	}
	operation := func() error {
		mu.Lock()
		id := lastID
		mu.Unlock()
		err := Subscribe(ctx, AddSSEHeaders(ctx, factory(), id), wrapHandler, opts...)
		if ctx.Err() != nil {
			return backoff.Permanent(ctx.Err())
		}
		if err == nil {
			// the server closed the stream; reconnect per the spec
			return io.ErrUnexpectedEOF
		}
		return err
	}
	if strategy == nil {
		strategy = backoff.NewExponentialBackOff()
	}
	return backoff.Retry(operation, backoff.WithContext(strategy, ctx))
}

// AddSSEHeaders returns a copy of r bound to ctx with the event stream request headers set.
func AddSSEHeaders(ctx context.Context, r *http.Request, lastID *string) *http.Request {
	req := r.Clone(ctx)

	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "text/event-stream")

	if lastID != nil && len(*lastID) > 0 && req.Header.Get("Last-Event-ID") == "" {
		req.Header.Set("Last-Event-ID", *lastID)
	}

	return req
}
