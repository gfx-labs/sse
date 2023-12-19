package sse

import (
	"bytes"
	"io"
	"time"
)

// Event holds all of the event source fields
type Event struct {
	timestamp time.Time
	Event     []byte
	ID        *[]byte
	Data      io.Reader

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
	if val, ok := e.Data.(*bytes.Buffer); ok {
		val.Reset()
	} else {
		val = nil
	}
	for k := range e.Fields {
		delete(e.Fields, k)
	}
	e.Comments = e.Comments[:0]
}
