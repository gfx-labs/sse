package sse

import (
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Encoder writes events to a stream. Each event is sent in a single Write.
// It is not safe for concurrent use.
type Encoder struct {
	w   io.Writer
	buf []byte
	o   options
}

func NewEncoder(w io.Writer, opts ...Option) *Encoder {
	return &Encoder{w: w, o: newOptions(opts)}
}

// Encode writes ev. Data is split into one data line per line of input, and a
// nil Data writes no data line. A non-nil empty ID writes a bare id field, which
// resets the client's last event id.
func (e *Encoder) Encode(ev *Event) error {
	b := e.buf[:0]
	var err error
	for _, c := range ev.Comments {
		if b, err = appendComment(b, c); err != nil {
			return err
		}
	}
	if ev.Event != "" {
		if b, err = e.appendField(b, "event", ev.Event); err != nil {
			return err
		}
	}
	for k, v := range ev.Fields {
		if b, err = appendFieldTo(e, b, k, v); err != nil {
			return err
		}
	}
	if ev.Retry > 0 {
		b = append(b, "retry: "...)
		b = strconv.AppendInt(b, ev.Retry.Milliseconds(), 10)
		b = append(b, '\n')
	}
	if ev.Data != nil {
		if e.o.validateUTF8 && !utf8.Valid(ev.Data) {
			return ErrInvalidUTF8
		}
		b = appendData(b, ev.Data)
	}
	if ev.ID != nil {
		if b, err = appendFieldTo(e, b, "id", ev.ID); err != nil {
			return err
		}
	}
	b = append(b, '\n')
	e.buf = b
	_, err = e.w.Write(b)
	return err
}

// Comment writes a comment line, which clients ignore. Use it as a keep-alive.
func (e *Encoder) Comment(text string) error {
	b, err := appendComment(e.buf[:0], text)
	if err != nil {
		return err
	}
	e.buf = b
	_, err = e.w.Write(b)
	return err
}

func (e *Encoder) appendField(b []byte, name, value string) ([]byte, error) {
	return appendFieldTo(e, b, name, value)
}

// appendFieldTo appends a "name: value" line. Empty values are written as a bare name.
func appendFieldTo[V string | []byte](e *Encoder, b []byte, name string, value V) ([]byte, error) {
	// a line break would end the field early and let the value inject extra fields
	if name == "" || strings.ContainsAny(name, "\r\n:") || hasEOL(value) {
		return b, ErrInvalidField
	}
	if e.o.validateUTF8 && !validUTF8(value) {
		return b, ErrInvalidUTF8
	}
	b = append(b, name...)
	if len(value) > 0 {
		b = append(b, ": "...)
		b = append(b, value...)
	}
	return append(b, '\n'), nil
}

func appendComment[V string | []byte](b []byte, text V) ([]byte, error) {
	if hasEOL(text) {
		return b, ErrInvalidField
	}
	b = append(b, ':')
	if len(text) > 0 {
		b = append(b, ' ')
		b = append(b, text...)
	}
	return append(b, '\n'), nil
}

// appendData writes one data line per line of data, treating "\r\n", "\r" and "\n" as line breaks.
func appendData(b, data []byte) []byte {
	for {
		i := indexEOL(data)
		b = append(b, "data: "...)
		if i < 0 {
			b = append(b, data...)
			return append(b, '\n')
		}
		b = append(b, data[:i]...)
		b = append(b, '\n')
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			i++
		}
		data = data[i+1:]
	}
}

func hasEOL[V string | []byte](v V) bool {
	for i := 0; i < len(v); i++ {
		if v[i] == '\n' || v[i] == '\r' {
			return true
		}
	}
	return false
}

func validUTF8[V string | []byte](v V) bool {
	switch x := any(v).(type) {
	case string:
		return utf8.ValidString(x)
	case []byte:
		return utf8.Valid(x)
	}
	return false
}
