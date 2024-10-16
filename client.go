package sse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cenkalti/backoff/v4"
)

var (
	headerID    = []byte("id:")
	headerData  = []byte("data:")
	headerEvent = []byte("event:")
	headerRetry = []byte("retry:")
)

// Subscribe to a data stream with context. the handler is called on every event. it exits on any error
func Subscribe(ctx context.Context, r *http.Request, handler func(msg *Event), opts ...Option) error {
	req := AddSSEHeaders(ctx, r, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		bts, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", http.StatusText(resp.StatusCode), bts)
	}
	reader := NewReader(resp.Body, opts...)
	dec := NewDecoder(reader)
	msg := &Event{}
	for {
		// on any sort of error while decoding, we close the connection.
		err := dec.Decode(msg)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		// Send downstream if the event has something useful
		if msg.hasContent() {
			handler(msg)
		}
	}
}

// SubscribeWIthRetry uses user specified reconnection strategy or default to standard NewExponentialBackOff() reconnection method
// it keeps track of the last id
func SubscribeWithRetry(
	ctx context.Context,
	factory func() *http.Request,
	handler func(msg *Event),
	strategy backoff.BackOff,
	opts ...Option,
) error {
	var lastID *string
	wrapHandler := func(msg *Event) {
		// keep track of the last id for resubscription
		if msg.ID != nil {
			idString := string(*msg.ID)
			lastID = &idString
		}
		handler(msg)
	}
	operation := func() error {
		req := AddSSEHeaders(ctx, factory(), lastID)
		return Subscribe(ctx, req, wrapHandler, opts...)
	}
	var err error
	var ReconnectNotify backoff.Notify
	if strategy != nil {
		err = backoff.RetryNotify(operation, strategy, ReconnectNotify)
	} else {
		err = backoff.RetryNotify(operation, backoff.NewExponentialBackOff(), ReconnectNotify)
	}
	return err
}

func AddSSEHeaders(ctx context.Context, r *http.Request, lastID *string) *http.Request {
	req := r.WithContext(ctx)

	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Connection", "keep-alive")

	if lastID != nil && len(*lastID) > 0 && req.Header.Get("Last-Event-ID") == "" {
		req.Header.Set("Last-Event-ID", *lastID)
	}

	return r
}
