package sse

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

type TokenType rune

func (t TokenType) String() string {
	switch t {
	case TokenInvalid:
		return "invalid"
	case TokenSkip:
		return "skip"
	case TokenBom:
		return "bom"
	case TokenComment:
		return "comment"
	case TokenField:
		return "field"
	case TokenDispatch:
		return "dispatch"
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

var bom = []byte{0xEF, 0xBB, 0xBF}

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

// Reader splits an event stream into line tokens. It is not thread safe.
type Reader struct {
	r   *bufio.Reader
	rs  readerState
	cur bytes.Buffer
	o   Options
}

func NewReader(r io.Reader, opts ...Option) *Reader {
	o := &Options{}
	for _, v := range opts {
		v(o)
	}
	return &Reader{
		r: bufio.NewReader(r),
		o: *o,
	}
}

// Token returns the current token. It is only valid until the next call to Next.
func (r *Reader) Token() *Token {
	return r.rs.tok
}

// Next advances to the next token. Lines may end in "\r\n", "\n" or "\r".
// If a max buffer size is set, a line longer than it returns ErrLineTooLong.
func (r *Reader) Next() error {
	r.rs.tok = nil
	r.cur.Reset()

	if !r.rs.triedBom {
		r.rs.triedBom = true
		if b, err := r.r.Peek(len(bom)); err == nil && bytes.Equal(b, bom) {
			if _, err := r.r.Discard(len(bom)); err != nil {
				return err
			}
			r.rs.lastTokenType = TokenBom
			r.rs.tok = &Token{Type: TokenBom}
			return nil
		}
	}

	limit := r.o.MaxBufferSize()
	for {
		x, err := r.r.ReadByte()
		if err != nil {
			return err
		}
		if r.rs.pendingCarriage {
			r.rs.pendingCarriage = false
			if x == '\n' {
				// second half of a "\r\n" line break
				continue
			}
		}
		switch x {
		case '\r':
			r.rs.pendingCarriage = true
			r.emit()
			return nil
		case '\n':
			r.emit()
			return nil
		}
		if limit > 0 && r.cur.Len() >= limit {
			return ErrLineTooLong
		}
		r.cur.WriteByte(x)
	}
}

func (r *Reader) emit() {
	tok := &Token{Value: r.cur.Bytes()}
	switch {
	case len(tok.Value) == 0:
		tok.Type = TokenDispatch
	case tok.Value[0] == ':':
		tok.Type = TokenComment
	default:
		tok.Type = TokenField
	}
	// collapse consecutive blank lines into one dispatch followed by skips
	if tok.Type == TokenDispatch && r.rs.lastTokenType == TokenDispatch {
		tok.Type = TokenSkip
	}
	r.rs.lastTokenType = tok.Type
	r.rs.tok = tok
}
