package eventsource

import (
	"strconv"
	"testing"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer(t *testing.T) {
	s := NewServer(sse.DefaultUpgrader)
	require.NotNil(t, s)
	require.NotNil(t, s.elog)
	require.NotNil(t, s.subs)
	assert.Equal(t, 0, s.elog.events.Len())
}

func TestServerEncode(t *testing.T) {
	// covers: Encode, newEventLog, byId, savedMessage
	s := NewServer(sse.DefaultUpgrader)
	for i := 0; i < 5; i++ {
		require.NoError(t, s.Encode(&sse.Event{
			Data: []byte(strconv.Itoa(i)),
		}))
	}
	assert.Equal(t, 5, s.elog.events.Len())
}

func TestServerBroadcastEmpty(t *testing.T) {
	// covers: broadcast early return when event log is empty
	s := NewServer(sse.DefaultUpgrader)
	s.broadcast() // should not panic
}

func TestServerBroadcastToSub(t *testing.T) {
	// covers: broadcast with a subscriber, non-blocking channel send
	s := NewServer(sse.DefaultUpgrader)

	// manually register a subscriber
	newSub := &sub{
		msgs: make(chan *sse.Event, 128),
	}
	s.mu.Lock()
	s.subs[0] = newSub
	s.mu.Unlock()

	// encode sends to event log and broadcasts
	require.NoError(t, s.Encode(&sse.Event{Data: []byte("hello")}))

	// subscriber should have received the event
	select {
	case msg := <-newSub.msgs:
		assert.Equal(t, []byte("hello"), msg.Data)
	default:
		t.Fatal("expected message on subscriber channel")
	}

	// encode another event — now subscriber has a lastid, exercises Ascend path
	require.NoError(t, s.Encode(&sse.Event{Data: []byte("world")}))
	select {
	case msg := <-newSub.msgs:
		assert.Equal(t, []byte("world"), msg.Data)
	default:
		t.Fatal("expected second message on subscriber channel")
	}
}

func TestEventLogByID(t *testing.T) {
	// covers: byId comparator
	a := &savedMessage{id: 1}
	b := &savedMessage{id: 2}
	assert.True(t, byId(a, b))
	assert.False(t, byId(b, a))
	assert.False(t, byId(a, a))
}
