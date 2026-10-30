package eventsource

import (
	"sync"

	"github.com/gfx-labs/sse"
	"github.com/tidwall/btree"
)

type savedMessage struct {
	ev *sse.Event
	id int64
}

func byID(a, b *savedMessage) bool {
	return a.id < b.id
}

// eventLog is safe for concurrent use. A limit above zero keeps only the newest events.
type eventLog struct {
	mu     sync.RWMutex
	events *btree.BTreeG[*savedMessage]
	limit  int
}

func newEventLog(limit int) *eventLog {
	return &eventLog{events: btree.NewBTreeG(byID), limit: limit}
}

func (l *eventLog) add(m *savedMessage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events.Set(m)
	for l.limit > 0 && l.events.Len() > l.limit {
		l.events.PopMin()
	}
}

func (l *eventLog) latest() (*savedMessage, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.events.Max()
}

// after calls fn for each message with an id greater than id, in order, until fn returns false.
func (l *eventLog) after(id int64, fn func(*savedMessage) bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.events.Ascend(&savedMessage{id: id + 1}, fn)
}
