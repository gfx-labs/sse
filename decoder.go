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

func (d *Decoder) Decode(e *Event) error {
	if e == nil {
		panic("cannot pass nil event into (*sse.Decoder).Decode")
	}
	buf := &bytes.Buffer{}
	e.Data = buf
	for {
		err := d.r.Next()
		if err != nil {
			return err
		}
		tok := d.r.Token()
		switch tok.Type {
		case TokenInvalid:
			return ErrInvalidToken
		case TokenBom:
			// ignore bom
			continue
		case TokenComment:
			e.Comments = append(e.Comments, copyBytes(tok.Value))
			continue
		case TokenDispatch:
			// Trim the last "\n" per the spec.
			if buf.Len() > 0 {
				if buf.Bytes()[buf.Len()-1] == '\n' {
					buf.Truncate(buf.Len() - 1)
				}
			}
			return nil
		case TokenSkip:
			continue
		default:
			return fmt.Errorf("%w: %d", ErrUnknownTokenType, tok.Type)
		case TokenField:
		}
		// at this point it's a field,

		line := tok.Value
		switch {
		case bytes.HasPrefix(line, headerID):
			idBytes := append([]byte(nil), trimField(len(headerID), line)...)
			e.ID = &idBytes
		case bytes.HasPrefix(line, headerData):
			// The spec allows for multiple data fields per event, concatenated them with "\n".
			buf.Write(append(trimField(len(headerData), line), byte('\n')))
		// The spec says that a line that simply contains the string "data" should be treated as a data field with an empty body.
		case bytes.Equal(line, bytes.TrimSuffix(headerData, []byte(":"))):
			buf.WriteRune('\n')
		case bytes.HasPrefix(line, headerEvent):
			e.Event = copyBytes(trimField(len(headerEvent), line))
		case bytes.HasPrefix(line, headerRetry):
			if e.Fields == nil {
				e.Fields = make(map[string][]byte)
			}
			e.Fields["retry"] = copyBytes(trimField(len(headerEvent), line))
		default:
			// this is a custom header. extract it from the stream
			splt := bytes.SplitN(line, []byte(":"), 2)
			var header []byte
			var topic []byte
			header = splt[0]
			if len(splt) == 2 {
				topic = bytes.TrimSpace(splt[1])
			}
			e.Fields[string(header)] = copyBytes(topic)
		}
	}
}

func trimField(size int, data []byte) []byte {
	if data == nil || len(data) < size {
		return data
	}

	data = data[size:]
	// Remove optional leading whitespace
	if len(data) > 0 && data[0] == 32 {
		data = data[1:]
	}
	// Remove trailing new line
	if len(data) > 0 && data[len(data)-1] == 10 {
		data = data[:len(data)-1]
	}
	return data
}

func copyBytes(xs []byte) []byte {
	n := make([]byte, len(xs))
	copy(n, xs)
	return n
}
