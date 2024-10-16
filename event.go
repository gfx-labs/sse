package sse

import (
	"time"
)

// Event holds all of the event source fields
type Event struct {
	timestamp time.Time
	Event     []byte
	ID        *[]byte
	Data      []byte

	Fields   map[string][]byte
	Comments [][]byte
}

func (e *Event) hasContent() bool {
	return e.ID != nil || e.Data != nil || len(e.Event) > 0 || len(e.Fields) > 0
}

func (e *Event) reset() {
	e.timestamp = time.Time{}
	e.Event = e.Event[:0]
	e.ID = nil
	e.Data = e.Data[:0]
	for k := range e.Fields {
		delete(e.Fields, k)
	}
	e.Comments = e.Comments[:0]
}
