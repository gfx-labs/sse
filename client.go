package sse

import (
	"fmt"
	"io"
	"mime"
	"net/http"
)

// StatusError reports a response status other than 200 OK.
type StatusError struct {
	Code   int
	Status string
	// Body holds up to 512 bytes of the response body.
	Body []byte
}

func (e *StatusError) Error() string { return fmt.Sprintf("sse: %s: %s", e.Status, e.Body) }

// Connect sends req with event-stream request headers and checks that the
// response is an event stream. On success the caller must close resp.Body,
// typically after reading it with NewDecoder or Events. A nil hc means
// http.DefaultClient, whose Timeout must stay zero for long-lived streams.
//
// It returns ErrNoContent for 204 No Content, which tells clients to stop
// reconnecting, *StatusError for other statuses, and ErrInvalidContentType
// for a Content-Type other than text/event-stream.
//
// For a single connection without reconnects:
//
//	resp, err := sse.Connect(nil, req)
//	if err != nil { ... }
//	defer resp.Body.Close()
//	for ev, err := range sse.Events(resp.Body) { ... }
func Connect(hc *http.Client, req *http.Request) (*http.Response, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	req = req.Clone(req.Context())
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNoContent:
		_ = resp.Body.Close()
		return nil, ErrNoContent
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, &StatusError{Code: resp.StatusCode, Status: resp.Status, Body: body}
	}
	ct := resp.Header.Get("Content-Type")
	if mt, _, err := mime.ParseMediaType(ct); err != nil || mt != "text/event-stream" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: %q", ErrInvalidContentType, ct)
	}
	return resp, nil
}
