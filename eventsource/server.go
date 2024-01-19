package eventsource

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gfx-labs/sse"
)

type sub struct {
	lastid int
	msgs   chan *sse.Event
	mu     sync.Mutex
}

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

	newSub := &sub{
		msgs: make(chan *sse.Event, 128),
	}
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
		default:
		}
		select {
		case m := <-newSub.msgs:
			err := conn.Encode(m)
			if err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) Encode(p *sse.Event) error {
	data, err := io.ReadAll(p.Data)
	if err != nil {
		return err
	}
	// now take the payload and put it with the shell
	p.Data = nil
	sm := &savedMessage{
		shell:   *p,
		payload: data,
		id:      int(s.currentId.Add(1)),
	}
	s.elog.events.Set(sm)
	return nil
}

func (s *Server) broadcast() {
	if s.elog.events.Len() == 0 {
		return
	}
	s.mu.Lock()
	subs := make([]*sub, 0, len(s.subs))
	for _, v := range s.subs {
		subs = append(subs, v)
	}
	s.mu.Unlock()
	for _, v := range subs {
		func() {
			v.mu.Lock()
			defer v.mu.Unlock()
			curId := v.lastid
			// if no id, just send the most recent value
			if curId == 0 {
				if currentValue, ok := s.elog.events.Max(); ok {
					msg := currentValue.shell
					msg.Data = bytes.NewBuffer(currentValue.payload)
					select {
					case v.msgs <- &msg:
						v.lastid = currentValue.id
					default:
						return
					}
				}
				return
			}
			// otherwise, send anything missing to the head to each subscriber
			// if the listeners buffer becomes full, they get skipped, so this wont block
			s.elog.events.Ascend(&savedMessage{
				id: curId,
			}, func(currentValue *savedMessage) bool {
				msg := currentValue.shell
				msg.Data = bytes.NewBuffer(currentValue.payload)
				select {
				case v.msgs <- &msg:
					v.lastid = currentValue.id
					return true
				default:
					return false
				}
			})
		}()
	}
}
