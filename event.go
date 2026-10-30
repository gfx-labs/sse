package sse

import (
	"maps"
	"slices"
	"time"
)

// Event is a single server-sent event.
//
// When decoding, a nil Data means the event had no data field, which browsers do
// not dispatch. A nil ID means the event had no id field, and an empty non-nil ID
// means the event reset the last event id. Events returned by Decoder, Events and
// Client.Events are reused for the next event; use Clone to keep one.
type Event struct {
	ID    []byte
	Event string
	Data  []byte
	// Retry is the reconnection delay requested by the server. Zero means unset.
	Retry time.Duration
	// Fields holds fields not defined by the spec.
	Fields map[string][]byte
	// Comments holds comment lines without the leading colon and optional space.
	Comments [][]byte

	hasData bool
	dataBuf []byte
	idBuf   []byte
}

// Clone returns a deep copy of e.
func (e *Event) Clone() *Event {
	c := &Event{
		Event:   e.Event,
		Data:    slices.Clone(e.Data),
		Retry:   e.Retry,
		hasData: e.hasData,
	}
	if e.ID != nil {
		c.ID = append([]byte{}, e.ID...)
	}
	if e.Fields != nil {
		c.Fields = maps.Clone(e.Fields)
		for k, v := range c.Fields {
			c.Fields[k] = slices.Clone(v)
		}
	}
	for _, x := range e.Comments {
		c.Comments = append(c.Comments, slices.Clone(x))
	}
	return c
}

func (e *Event) hasContent() bool {
	return e.hasData || e.ID != nil || e.Event != "" || e.Retry > 0 || len(e.Fields) > 0
}

// reset clears e while keeping its buffers for reuse.
func (e *Event) reset() {
	if e.ID != nil {
		e.idBuf = e.ID
	}
	e.ID = nil
	e.Event = ""
	if e.Data != nil {
		e.dataBuf = e.Data
	}
	e.Data = nil
	e.Retry = 0
	clear(e.Fields)
	e.Comments = e.Comments[:0]
	e.hasData = false
}
