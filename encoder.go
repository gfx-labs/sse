package sse

import (
	"bytes"
	"io"
)

func ID(x string) *[]byte {
	xb := []byte(x)
	return &xb
}

// Encoder writes whole events. Each event is built in memory and sent to the
// underlying writer in a single Write, so a stream never holds a partial event.
// It is not thread safe.
type Encoder struct {
	out io.Writer
	buf bytes.Buffer
	wr  *Writer
}

func NewEncoder(w io.Writer, opts ...Option) *Encoder {
	e := &Encoder{out: w}
	e.wr = NewWriter(&e.buf, opts...)
	return e
}

func (e *Encoder) Encode(p *Event) error {
	e.buf.Reset()
	e.wr.es = writeState{}
	if len(p.Event) > 0 {
		if err := e.wr.Field("event", p.Event); err != nil {
			return err
		}
	}
	for k, v := range p.Fields {
		if err := e.wr.Field(k, v); err != nil {
			return err
		}
	}
	if p.Data != nil {
		if _, err := e.wr.Write(p.Data); err != nil {
			return err
		}
	}
	// end the data line so the id goes on its own line
	if err := e.wr.Flush(); err != nil {
		return err
	}
	if p.ID != nil {
		if err := e.wr.Field("id", *p.ID); err != nil {
			return err
		}
	}
	if err := e.wr.Next(); err != nil {
		return err
	}
	_, err := e.out.Write(e.buf.Bytes())
	return err
}
