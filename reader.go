package sse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

type TokenType rune

func (t TokenType) String() string {
	switch rune(t) {
	case 0:
		return "invalid"
	case 1:
		return "skip"
	case 2:
		return "bom"
	case 3:
		return "comment"
	case 4:
		return "field"
	case 5:
		return "dispatch"
	case 6:
		return "closed"
	default:
		return fmt.Sprintf("unknown (%d)", rune(t))
	}
}

const (
	TokenInvalid TokenType = iota
	TokenSkip
	TokenBom
	TokenComment
	TokenField
	TokenDispatch
)

type Token struct {
	Type  TokenType
	Value []byte
}

func (t *Token) String() string {
	if t == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: %s", t.Type, string(t.Value))
}

type readerState struct {
	triedBom        bool
	lastTokenType   TokenType
	pendingCarriage bool
	tok             *Token
}

// The reader is more like a lexer :p
// it is not thread safe.
type Reader struct {
	r            *bufio.Reader
	maxTokenSize int
	rs           readerState
	readBuf      []byte

	cur      bytes.Buffer
	leftover bytes.Buffer
}

func NewReader(r io.Reader, maxBufferSize int) *Reader {
	return &Reader{
		r:            bufio.NewReader(r),
		maxTokenSize: maxBufferSize,
		readBuf:      make([]byte, 4096),
	}
}

// returns the current token
func (r *Reader) Token() *Token {

	return r.rs.tok
}

// Token is only valid until the next call to Next()
func (r *Reader) Next() error {
	r.rs.tok = nil
	rd := r.r
	if r.maxTokenSize > 0 {
		allowedToRead := r.maxTokenSize - r.cur.Len()
		if allowedToRead <= 0 {
			return io.EOF
		}
		rd = bufio.NewReader(io.LimitReader(r.r, int64(allowedToRead)))
	}

	// this can only happen on the first write
	if !r.rs.triedBom {
		r.rs.triedBom = true
		//  read the first byte and determine if its bom
		maybeBom, _, err := rd.ReadRune()
		if err != nil {
			return err
		}
		_, err = r.cur.WriteRune(maybeBom)
		if err != nil {
			return err
		}
		if maybeBom == '\uFEFF' {
			r.cur.Reset() // reset cur here since this shouldnt be sent with the next payload, and the bom is always feff
			r.rs.lastTokenType = TokenBom
			r.rs.tok = &Token{
				Type: TokenBom,
			}
			return nil
		}
		// if it's not BOM, then just keep going, assuming the first read bytes was just data.
	}
	for {
		// read up to buf bytes
		var thisRead []byte
		// if we have leftover bytes, read those instead
		if r.leftover.Len() > 0 {
			thisRead = r.leftover.Bytes()
			r.leftover.Reset()
		} else {
			n, err := rd.Read(r.readBuf)
			if n == 0 && err != nil {
				return err
			}
			thisRead = r.readBuf[:n]
		}
		for _, x := range thisRead {
			if r.rs.tok != nil {
				r.leftover.WriteByte(x)
				continue
			}
			if r.rs.pendingCarriage {
				// we are pending a carriage, so we need to check if the next byte is a newline
				if x == '\n' {
					// if it is a newline, it means that this is an \r\n line break
					r.rs.tok = &Token{
						Value: r.cur.Bytes(),
					}
					continue
				}
				// otherwise, this is an \r line break
				r.rs.tok = &Token{
					Value: r.cur.Bytes(),
				}
				continue
			}
			// if there is no pending carriage (we got here), then just check first for '\n'
			if x == '\n' {
				// if it is a newline here, it is an \lf break
				r.rs.tok = &Token{
					Value: r.cur.Bytes(),
				}
				continue
			}
			if x == '\r' {
				// if it is \r here, then mark pendingCarriage and continue
				r.rs.pendingCarriage = true
				continue
			}
			// otherwise, just write the byte to cur
			r.cur.WriteByte(x)
		}
		if r.rs.tok != nil {
			// at this point, reset the buffer, but do not clear it so that the value can still be read
			r.cur.Reset()
			if len(r.rs.tok.Value) == 0 {
				r.rs.tok.Type = TokenDispatch
			} else if r.rs.tok.Value[0] == ':' {
				r.rs.tok.Type = TokenComment
			} else {
				r.rs.tok.Type = TokenField
			}
			// if two dispatches in a row, we start sending skip tokens instead
			if r.rs.lastTokenType == TokenDispatch {
				if r.rs.tok.Type == TokenDispatch {
					r.rs.tok.Type = TokenSkip
				}
			}
			r.rs.lastTokenType = r.rs.tok.Type
			return nil
		}
	}

}
