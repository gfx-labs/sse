// Package eventsource is a reference EventSource server built on package sse.
// It keeps an in-memory log of events and replays missed events to clients that
// reconnect with Last-Event-ID.
package eventsource

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gfx-labs/sse"
)

type sub struct {
	mu     sync.Mutex
	lastid int64
	// wake has capacity 1 so a broadcast never blocks and never gets lost
	wake chan struct{}
}

func (s *sub) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Server publishes events to every connected client. It implements http.Handler.
type Server struct {
	// Heartbeat is the interval between keep-alive comments on idle streams. Zero disables them.
	Heartbeat time.Duration

	subs  map[int]*sub
	subID int
	mu    sync.Mutex

	elog      *eventLog
	currentID atomic.Int64
}

// NewServer returns a Server that keeps the newest logSize events for replay.
// A logSize of zero or less keeps every event.
func NewServer(logSize int) *Server {
	s := &Server{
		Heartbeat: 30 * time.Second,
		subs:      map[int]*sub{},
		elog:      newEventLog(logSize),
	}
	s.currentID.Store(time.Now().UnixMilli())
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	stream, err := sse.Upgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	newSub := &sub{wake: make(chan struct{}, 1)}
	// resume after the client's last seen id if it sent a valid one
	if id, err := strconv.ParseInt(stream.LastEventID, 10, 64); err == nil {
		newSub.lastid = id
	}
	newSub.notify()

	s.mu.Lock()
	id := s.subID
	s.subID++
	s.subs[id] = newSub
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}()

	var heartbeat <-chan time.Time
	if s.Heartbeat > 0 {
		t := time.NewTicker(s.Heartbeat)
		defer t.Stop()
		heartbeat = t.C
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat:
			if err := stream.Comment(""); err != nil {
				return
			}
		case <-newSub.wake:
			if err := s.flush(stream, newSub); err != nil {
				return
			}
		}
	}
}

// flush writes every event the subscriber has not seen yet.
// A new subscriber with no Last-Event-ID only gets the latest event.
func (s *Server) flush(stream *sse.Stream, v *sub) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	var pending []*savedMessage
	if v.lastid == 0 {
		if m, ok := s.elog.latest(); ok {
			pending = append(pending, m)
		}
	} else {
		s.elog.after(v.lastid, func(m *savedMessage) bool {
			pending = append(pending, m)
			return true
		})
	}
	for _, m := range pending {
		if err := stream.Send(m.ev); err != nil {
			return err
		}
		v.lastid = m.id
	}
	return nil
}

// Publish stores a copy of ev in the event log and sends it to every client.
// The server assigns the event id, replacing ev.ID.
func (s *Server) Publish(ev *sse.Event) {
	id := s.currentID.Add(1)
	c := ev.Clone()
	c.ID = strconv.AppendInt(nil, id, 10)
	s.elog.add(&savedMessage{ev: c, id: id})

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.subs {
		v.notify()
	}
}
