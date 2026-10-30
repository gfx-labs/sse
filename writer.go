package sse

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"
)

type writeState struct {
	inMessage        bool
	trailingCarriage bool
}

var (
	newline    = []byte{'\n'}
	dataPrefix = []byte("data: ")
	fieldSep   = []byte(": ")
)

// Writer writes the event stream wire format directly to the underlying writer.
// Wrap the destination in a buffer to batch small writes. It is not thread safe.
type Writer struct {
	es writeState
	w  io.Writer
	o  Options
}

func NewWriter(w io.Writer, opts ...Option) *Writer {
	return &Writer{
		w: w,
		o: *newOptions(opts),
	}
}

func (e *Writer) write(xs []byte) error {
	_, err := e.w.Write(xs)
	return err
}

// Flush terminates an unfinished data line.
func (e *Writer) Flush() error {
	if e.es.inMessage || e.es.trailingCarriage {
		e.es.inMessage = false
		e.es.trailingCarriage = false
		return e.write(newline)
	}
	return nil
}

// Next ends the current event with a blank line.
func (e *Writer) Next() error {
	if err := e.Flush(); err != nil {
		return err
	}
	return e.write(newline)
}

// Field writes a `name: value` line. Empty values are skipped, except for "id",
// where an empty value resets the client's last event id.
func (e *Writer) Field(name []byte, value []byte) error {
	if len(name) == 0 || bytes.ContainsAny(name, "\r\n:") {
		return ErrInvalidField
	}
	if skip, err := e.checkValue(len(value) == 0 && string(name) == "id", value); skip || err != nil {
		return err
	}
	if err := e.write(name); err != nil {
		return err
	}
	return e.fieldValue(value)
}

// FieldString is Field with a string name. The name is not copied when the
// underlying writer implements io.StringWriter.
func (e *Writer) FieldString(name string, value []byte) error {
	if len(name) == 0 || strings.ContainsAny(name, "\r\n:") {
		return ErrInvalidField
	}
	if skip, err := e.checkValue(len(value) == 0 && name == "id", value); skip || err != nil {
		return err
	}
	if _, err := io.WriteString(e.w, name); err != nil {
		return err
	}
	return e.fieldValue(value)
}

// checkValue validates a field value and reports whether the field should be skipped.
// Empty values are skipped unless keepEmpty is set.
func (e *Writer) checkValue(keepEmpty bool, value []byte) (skip bool, err error) {
	if e.o.ValidateUTF8() && !utf8.Valid(value) {
		return true, ErrInvalidUTF8Bytes
	}
	// a line break would end the field early and let the value inject extra fields
	if bytes.ContainsAny(value, "\r\n") {
		return true, ErrInvalidField
	}
	return len(value) == 0 && !keepEmpty, nil
}

func (e *Writer) fieldValue(value []byte) error {
	if len(value) > 0 {
		if err := e.write(fieldSep); err != nil {
			return err
		}
		if err := e.write(value); err != nil {
			return err
		}
	}
	return e.write(newline)
}

func (e *Writer) ReadFrom(r io.Reader) (n int64, err error) {
	return io.Copy(e, r)
}

// Write writes xs as data lines, starting a new "data: " line after every line break.
// Data may be split across calls.
func (e *Writer) Write(xs []byte) (int, error) {
	if e.o.ValidateUTF8() && !utf8.Valid(xs) {
		return 0, ErrInvalidUTF8Bytes
	}
	n := 0
	for n < len(xs) {
		if e.es.trailingCarriage {
			e.es.trailingCarriage = false
			if xs[n] == '\n' {
				// second half of a "\r\n" line break
				if err := e.write(newline); err != nil {
					return n, err
				}
				n++
				continue
			}
		}
		if !e.es.inMessage {
			if err := e.write(dataPrefix); err != nil {
				return n, err
			}
			e.es.inMessage = true
		}
		// write up to and including the next line break in one call
		rest := xs[n:]
		i := bytes.IndexAny(rest, "\r\n")
		if i < 0 {
			if err := e.write(rest); err != nil {
				return n, err
			}
			return len(xs), nil
		}
		if err := e.write(rest[:i+1]); err != nil {
			return n, err
		}
		n += i + 1
		e.es.inMessage = false
		e.es.trailingCarriage = rest[i] == '\r'
	}
	return n, nil
}
