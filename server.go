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
func Upgrade(w http.ResponseWriter, r *http.Request, opts ...Option) (*Stream, error) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	if r.ProtoMajor == 1 {
		h.Set("Connection", "keep-alive")
	}
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return nil, ErrStreamingNotSupported
	}
	return &Stream{
		rc:          rc,
		enc:         NewEncoder(w, opts...),
		r:           r,
		LastEventID: r.Header.Get("Last-Event-ID"),
	}, nil
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
func (s *Stream) SetWriteDeadline(t time.Time) error {
	return s.rc.SetWriteDeadline(t)
}
