package eventsource

import (
	"github.com/gfx-labs/sse"
	"github.com/tidwall/btree"
)

type savedMessage struct {
	shell   sse.Event
	payload []byte
	id      int
}

func byId(a, b *savedMessage) bool {
	return a.id < b.id
}

type eventLog struct {
	events *btree.BTreeG[*savedMessage]
}

func newEventLog() *eventLog {
	return &eventLog{
		events: btree.NewBTreeG(byId),
	}
}
