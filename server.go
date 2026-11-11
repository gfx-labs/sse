package sse

import (
	"net/http"
	"time"
)

// Stream is a server-side event stream on an HTTP response. It is not safe for concurrent use.
type Stream struct {
	rc  *http.ResponseController
	enc *Encoder
	r   *http.Request

	// LastEventID is the Last-Event-ID header sent by a reconnecting client.
	LastEventID string
}

// Upgrade starts an event stream on w. It writes the response headers and flushes
// them, so call it only after any request validation that may need a different status.
//
// It clears the write deadline that http.Server.WriteTimeout sets, which would
// otherwise end the stream. Use SetWriteDeadline to bound individual writes.
//
// ErrStreamingNotSupported means w cannot flush and nothing was written, so the
// caller can still send an error response. After any other error the response
// has already started.
func Upgrade(w http.ResponseWriter, r *http.Request, opts ...Option) (*Stream, error) {
	if !canFlush(w) {
		return nil, ErrStreamingNotSupported
	}
	rc := http.NewResponseController(w)
	// not every ResponseWriter has deadlines, and those that lack them have none to clear
	_ = rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	if r.ProtoMajor == 1 {
		h.Set("Connection", "keep-alive")
	}
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return nil, err
	}
	return &Stream{
		rc:          rc,
		enc:         NewEncoder(w, opts...),
		r:           r,
		LastEventID: r.Header.Get("Last-Event-ID"),
	}, nil
}

// canFlush reports whether ResponseController.Flush can reach a flusher. It
// follows the same chain, without the side effect of sending the headers.
func canFlush(w http.ResponseWriter) bool {
	for {
		switch t := w.(type) {
		case interface{ FlushError() error }, http.Flusher:
			return true
		case interface{ Unwrap() http.ResponseWriter }:
			w = t.Unwrap()
		default:
			return false
		}
	}
}

// Send writes ev and flushes it to the client.
func (s *Stream) Send(ev *Event) error {
	if err := s.r.Context().Err(); err != nil {
		return err
	}
	if err := s.enc.Encode(ev); err != nil {
		return err
	}
	return s.rc.Flush()
}

// Comment writes a comment line and flushes it. Clients ignore comments, so this
// keeps idle connections open through proxies.
func (s *Stream) Comment(text string) error {
	if err := s.r.Context().Err(); err != nil {
		return err
	}
	if err := s.enc.Comment(text); err != nil {
		return err
	}
	return s.rc.Flush()
}

// SetWriteDeadline sets the deadline for writes to the client. A zero time means no deadline.
// On HTTP/2 the stream is reset when the deadline passes, even while it is idle.
func (s *Stream) SetWriteDeadline(t time.Time) error {
	return s.rc.SetWriteDeadline(t)
}
