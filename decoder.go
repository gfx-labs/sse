package sse

import (
	"bytes"
	"fmt"
)

type Decoder struct {
	r *Reader
}

func NewDecoder(r *Reader) *Decoder {
	return &Decoder{
		r: r,
	}
}

// Decode reads the next event into e, replacing its previous contents.
func (d *Decoder) Decode(e *Event) error {
	if e == nil {
		panic("cannot pass nil event into (*sse.Decoder).Decode")
	}
	*e = Event{}
	var buf bytes.Buffer
	defer func() {
		e.Data = buf.Bytes()
	}()
	for {
		err := d.r.Next()
		if err != nil {
			return err
		}
		tok := d.r.Token()
		switch tok.Type {
		case TokenInvalid:
			return ErrInvalidToken
		case TokenBom, TokenSkip:
			continue
		case TokenComment:
			e.Comments = append(e.Comments, copyBytes(tok.Value))
			continue
		case TokenDispatch:
			// Trim the last "\n" per the spec.
			if buf.Len() > 0 && buf.Bytes()[buf.Len()-1] == '\n' {
				buf.Truncate(buf.Len() - 1)
			}
			return nil
		case TokenField:
		default:
			return fmt.Errorf("%w: %d", ErrUnknownTokenType, tok.Type)
		}

		name, value := splitField(tok.Value)
		switch string(name) {
		case "id":
			// the spec ignores ids containing NULL
			if bytes.IndexByte(value, 0) >= 0 {
				continue
			}
			id := copyBytes(value)
			e.ID = &id
		case "data":
			buf.Write(value)
			buf.WriteByte('\n')
		case "event":
			e.Event = copyBytes(value)
		default:
			if e.Fields == nil {
				e.Fields = make(map[string][]byte)
			}
			e.Fields[string(name)] = copyBytes(value)
		}
	}
}

// splitField splits a line into name and value at the first colon,
// removing a single leading space from the value as the spec requires.
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

func copyBytes(xs []byte) []byte {
	n := make([]byte, len(xs))
	copy(n, xs)
	return n
}
