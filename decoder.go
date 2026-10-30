package sse

import (
	"bytes"
	"errors"
	"io"
	"iter"
	"time"
)

// Decoder reads events from a stream. It is not safe for concurrent use.
type Decoder struct {
	lr        *lineReader
	lastEvent string
}

func NewDecoder(r io.Reader, opts ...Option) *Decoder {
	o := newOptions(opts)
	return &Decoder{lr: newLineReader(r, o.maxLineSize)}
}

// Decode reads the next event into e. Blank lines that would dispatch an empty
// event are skipped. e's slices are reused, so values from a previous call are
// overwritten. It returns io.EOF at the end of the stream.
func (d *Decoder) Decode(e *Event) error {
	if e == nil {
		panic("sse: Decode called with nil *Event")
	}
	e.reset()
	for {
		line, err := d.lr.next()
		if err != nil {
			// an event without its terminating blank line is discarded
			e.reset()
			return err
		}
		if len(line) == 0 {
			if !e.hasContent() {
				// comments alone, such as keep-alives, do not form an event
				e.Comments = e.Comments[:0]
				continue
			}
			if e.hasData {
				// drop the newline after the last data line
				e.Data = e.dataBuf[:len(e.dataBuf)-1]
			}
			return nil
		}
		name, value := splitField(line)
		if len(name) == 0 {
			e.Comments = appendReuse(e.Comments, value)
			continue
		}
		switch string(name) {
		case "data":
			if !e.hasData {
				e.dataBuf = e.dataBuf[:0]
				e.hasData = true
			}
			e.dataBuf = append(append(e.dataBuf, value...), '\n')
		case "event":
			if string(value) != d.lastEvent {
				d.lastEvent = string(value)
			}
			e.Event = d.lastEvent
		case "id":
			// the spec ignores ids containing NULL
			if bytes.IndexByte(value, 0) < 0 {
				if e.idBuf == nil {
					// a non-nil empty ID means "reset", so never leave it nil
					e.idBuf = make([]byte, 0, 16)
				}
				e.ID = append(e.idBuf[:0], value...)
			}
		case "retry":
			if ms, ok := parseDigits(value); ok {
				e.Retry = time.Duration(ms) * time.Millisecond
			}
		default:
			if e.Fields == nil {
				e.Fields = make(map[string][]byte)
			}
			e.Fields[string(name)] = append([]byte(nil), value...)
		}
	}
}

// Events returns an iterator over the events in r. The yielded Event is reused
// for the next event. Iteration stops at the end of the stream or after
// yielding the first error.
func Events(r io.Reader, opts ...Option) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {
		d := NewDecoder(r, opts...)
		e := &Event{}
		for {
			err := d.Decode(e)
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(e, nil) {
				return
			}
		}
	}
}

// splitField splits a line at the first colon and removes one leading space from the value.
func splitField(line []byte) (name, value []byte) {
	name, value, found := bytes.Cut(line, []byte(":"))
	if !found {
		return line, nil
	}
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return name, value
}

// appendReuse appends a copy of v to xs, reusing the backing array of a previous element.
func appendReuse(xs [][]byte, v []byte) [][]byte {
	if len(xs) < cap(xs) {
		xs = xs[:len(xs)+1]
		xs[len(xs)-1] = append(xs[len(xs)-1][:0], v...)
		return xs
	}
	return append(xs, append([]byte(nil), v...))
}

func parseDigits(b []byte) (int64, bool) {
	if len(b) == 0 || len(b) > 15 {
		return 0, false
	}
	var n int64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}
