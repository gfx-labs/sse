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

// Server is an example of how a server could be built around the sse package.
// It is purposefully not very customizable. The event log is kept in memory and is unbounded.
type Server struct {
	upgrader *sse.Upgrader

	subs  map[int]*sub
	subId int
	mu    sync.Mutex

	elog *eventLog

	currentId atomic.Int64
}

func NewServer(u *sse.Upgrader) *Server {
	s := &Server{
		upgrader: u,
		subs:     map[int]*sub{},
		elog:     newEventLog(),
	}
	s.currentId.Store(time.Now().UnixMilli())
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	newSub := &sub{wake: make(chan struct{}, 1)}
	// resume after the client's last seen id if it sent a valid one
	if id, err := strconv.ParseInt(conn.LastEventId, 10, 64); err == nil {
		newSub.lastid = id
	}
	newSub.notify()

	s.mu.Lock()
	id := s.subId
	s.subId++
	s.subs[id] = newSub
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-newSub.wake:
			if err := s.flush(conn, newSub); err != nil {
				return
			}
		}
	}
}

// flush writes every event the subscriber has not seen yet.
// A new subscriber with no Last-Event-ID only gets the latest event.
func (s *Server) flush(conn *sse.EventSink, v *sub) error {
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
		msg := m.shell
		if err := conn.Encode(&msg); err != nil {
			return err
		}
		v.lastid = m.id
	}
	return nil
}

// Encode stores p in the event log and sends it to every subscriber.
// The event id is assigned by the server and overrides p.ID.
func (s *Server) Encode(p *sse.Event) error {
	id := s.currentId.Add(1)
	sm := &savedMessage{
		shell: *p,
		id:    id,
	}
	sm.shell.ID = sse.ID(strconv.FormatInt(id, 10))
	s.elog.add(sm)
	s.broadcast()
	return nil
}

func (s *Server) broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.subs {
		v.notify()
	}
}
