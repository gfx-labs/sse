package sse

import (
	"io"
)

func ID(x string) *[]byte {
	xb := []byte(x)
	return &xb
}

// Encoder works at a higher level than the encoder.
// it works on the packet level.
type Encoder struct {
	wr *Writer
}

func NewEncoder(w io.Writer) *Encoder {
	wr := NewWriter(w)
	return &Encoder{
		wr: wr,
	}
}

func (e *Encoder) Encode(p *Event) error {
	if len(p.Event) > 0 {
		if err := e.wr.Field([]byte("event"), p.Event); err != nil {
			return err
		}
	}
	if p.Fields != nil {
		for k, v := range p.Fields {
			if err := e.wr.Field([]byte(k), v); err != nil {
				return err
			}
		}
	}
	if p.Data != nil {
		if _, err := e.wr.Write(p.Data); err != nil {
			return err
		}
	}
	// flush the end of data to make sure we are safe to write an id
	err := e.wr.Flush()
	if err != nil {
		return err
	}
	if p.ID != nil {
		if err := e.wr.Field([]byte("id"), *p.ID); err != nil {
			return err
		}
	}
	if err := e.wr.Next(); err != nil {
		return err
	}

	return nil
}
