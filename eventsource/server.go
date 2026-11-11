// Package eventsource is a reference EventSource server built on package sse.
// It keeps an in-memory log of events and replays missed events to clients that
// reconnect with Last-Event-ID.
package eventsource

import (
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gfx-labs/sse"
)

// Server publishes events to every connected client. It implements http.Handler.
// Create one with NewServer. Publish, Close and ServeHTTP may be called from
// multiple goroutines. Set the exported fields before serving.
//
// A client that falls more than the log size behind skips the events it missed.
type Server struct {
	// Heartbeat is the interval between keep-alive comments. Zero disables them.
	Heartbeat time.Duration
	// WriteTimeout ends a stream whose client does not accept an event or
	// keep-alive within this time. Zero means a stalled client holds its handler
	// until the connection fails.
	WriteTimeout time.Duration

	mu sync.Mutex
	// each subscriber's wake channel has capacity 1 so a broadcast never blocks and never gets lost
	subs map[chan struct{}]struct{}
	// check encodes published events to reject those a stream could not send
	check *sse.Encoder

	elog      *eventLog
	done      chan struct{}
	closeOnce sync.Once
}

// DefaultLogSize is the log size NewServer uses when given zero or less.
const DefaultLogSize = 1024

// NewServer returns a Server that keeps the newest logSize events for replay.
// A logSize of zero or less means DefaultLogSize. Heartbeat and WriteTimeout
// start at 30 seconds.
func NewServer(logSize int) *Server {
	if logSize <= 0 {
		logSize = DefaultLogSize
	}
	// ids start at the current time so they keep increasing across restarts
	return &Server{
		Heartbeat:    30 * time.Second,
		WriteTimeout: 30 * time.Second,
		subs:         map[chan struct{}]struct{}{},
		check:        sse.NewEncoder(io.Discard),
		elog:         newEventLog(logSize, time.Now().UnixMilli()+1),
		done:         make(chan struct{}),
	}
}

// ServeHTTP streams events until the client disconnects or Close is called. A
// client whose Last-Event-ID is an id this server has reached gets every later
// event still in the log. Any other client, including one with an id from
// another server, gets the latest event, then every event published after the
// response headers were sent.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case <-s.done:
		http.Error(w, "event source closed", http.StatusServiceUnavailable)
		return
	default:
	}
	// taken before the headers are sent, so nothing published after the client sees them is missed
	cursor := s.elog.cursor(r.Header.Get("Last-Event-ID"))
	stream, err := sse.Upgrade(w, r)
	if err != nil {
		// after any other error the response has already started
		if errors.Is(err, sse.ErrStreamingNotSupported) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	wake := make(chan struct{}, 1)
	// the first pass sends what the log already holds
	wake <- struct{}{}
	s.mu.Lock()
	s.subs[wake] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, wake)
		s.mu.Unlock()
	}()

	var heartbeat <-chan time.Time
	if s.Heartbeat > 0 {
		t := time.NewTicker(s.Heartbeat)
		defer t.Stop()
		heartbeat = t.C
	}
	var pending []savedMessage
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.done:
			return
		case <-heartbeat:
			s.setWriteDeadline(stream)
			if err := stream.Comment(""); err != nil {
				return
			}
		case <-wake:
			pending = s.elog.after(cursor, pending[:0])
			for _, m := range pending {
				s.setWriteDeadline(stream)
				if err := stream.Send(m.ev); err != nil {
					return
				}
				cursor = m.id
			}
			// drop event references so evicted events can be collected
			clear(pending)
		}
		if s.WriteTimeout > 0 {
			// HTTP/2 resets the stream when a deadline passes, even an idle one
			_ = stream.SetWriteDeadline(time.Time{})
		}
	}
}

func (s *Server) setWriteDeadline(stream *sse.Stream) {
	if s.WriteTimeout > 0 {
		// a ResponseWriter without deadlines keeps streaming without the timeout
		_ = stream.SetWriteDeadline(time.Now().Add(s.WriteTimeout))
	}
}

// Publish stores a copy of ev in the event log and sends it to every client.
// The server assigns the event id, replacing ev.ID. It returns the encoder's
// error, such as sse.ErrInvalidField, for an event that cannot be sent, and
// does not store it.
func (s *Server) Publish(ev *sse.Event) error {
	c := ev.Clone()
	c.ID = nil
	s.mu.Lock()
	defer s.mu.Unlock()
	// a stored event that cannot be encoded would end every stream that reaches it
	if err := s.check.Encode(c); err != nil {
		return err
	}
	s.elog.add(c)
	for wake := range s.subs {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return nil
}

// Close ends every stream and makes ServeHTTP answer 503 Service Unavailable,
// so clients reconnect elsewhere. http.Server.Shutdown waits for streams to
// end, so register it first:
//
//	httpServer.RegisterOnShutdown(es.Close)
func (s *Server) Close() {
	s.closeOnce.Do(func() { close(s.done) })
}
