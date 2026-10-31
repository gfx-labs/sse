package eventsource

import (
	"strconv"
	"sync"

	"github.com/gfx-labs/sse"
)

type savedMessage struct {
	ev *sse.Event
	id int64
}

// eventLog is a fixed-size ring of the newest events. It assigns ids itself so
// they are consecutive and stored in order. It is safe for concurrent use.
type eventLog struct {
	mu   sync.RWMutex
	buf  []savedMessage
	head int   // index of the oldest event
	n    int   // number of stored events
	next int64 // id of the next event
}

func newEventLog(size int, firstID int64) *eventLog {
	return &eventLog{buf: make([]savedMessage, size), next: firstID}
}

// add stores ev with the next id, which it also writes to ev.ID, evicting the
// oldest event when full. ev must not be shared yet.
func (l *eventLog) add(ev *sse.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := l.next
	l.next++
	ev.ID = strconv.AppendInt(nil, id, 10)
	if l.n < len(l.buf) {
		l.buf[(l.head+l.n)%len(l.buf)] = savedMessage{ev: ev, id: id}
		l.n++
	} else {
		l.buf[l.head] = savedMessage{ev: ev, id: id}
		l.head = (l.head + 1) % len(l.buf)
	}
}

func (l *eventLog) latest() (savedMessage, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.n == 0 {
		return savedMessage{}, false
	}
	return l.buf[(l.head+l.n-1)%len(l.buf)], true
}

// after appends each stored message with an id greater than id to dst, in order.
// If id is older than the log, it starts from the oldest stored event.
func (l *eventLog) after(id int64, dst []savedMessage) []savedMessage {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.n == 0 {
		return dst
	}
	oldest := l.buf[l.head].id
	skip := max(id+1-oldest, 0)
	for i := int(min(skip, int64(l.n))); i < l.n; i++ {
		dst = append(dst, l.buf[(l.head+i)%len(l.buf)])
	}
	return dst
}
