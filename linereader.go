package sse

import (
	"bytes"
	"io"
)

var bom = []byte{0xEF, 0xBB, 0xBF}

// lineReader splits a stream into lines ending in "\r\n", "\n" or "\r".
type lineReader struct {
	r          io.Reader
	buf        []byte
	start, end int
	err        error
	max        int
	skipLF     bool
	bomChecked bool
}

func newLineReader(r io.Reader, max int) *lineReader {
	return &lineReader{r: r, buf: make([]byte, 4096), max: max}
}

// next returns the next line without its terminator. The line is valid until the next call.
// An unterminated final line is discarded, as the spec requires.
func (l *lineReader) next() ([]byte, error) {
	scanned := 0
	for {
		win := l.buf[l.start:l.end]
		if !l.bomChecked {
			if len(win) < len(bom) && l.err == nil {
				l.fill()
				continue
			}
			l.bomChecked = true
			if bytes.HasPrefix(win, bom) {
				l.start += len(bom)
				continue
			}
		}
		if l.skipLF && len(win) > 0 {
			// second half of a "\r\n" line break
			l.skipLF = false
			if win[0] == '\n' {
				l.start++
				continue
			}
		}
		if i := indexEOL(win[scanned:]); i >= 0 {
			i += scanned
			line := win[:i]
			l.skipLF = win[i] == '\r'
			l.start += i + 1
			if l.max > 0 && len(line) > l.max {
				return nil, ErrLineTooLong
			}
			return line, nil
		}
		scanned = len(win)
		if l.max > 0 && len(win) > l.max {
			return nil, ErrLineTooLong
		}
		if l.err != nil {
			return nil, l.err
		}
		l.fill()
	}
}

func indexEOL(b []byte) int {
	i := bytes.IndexByte(b, '\n')
	lim := i
	if lim < 0 {
		lim = len(b)
	}
	if j := bytes.IndexByte(b[:lim], '\r'); j >= 0 {
		return j
	}
	return i
}

func (l *lineReader) fill() {
	if l.start > 0 {
		l.end = copy(l.buf, l.buf[l.start:l.end])
		l.start = 0
	}
	if l.end == len(l.buf) {
		l.buf = append(l.buf, make([]byte, len(l.buf))...)
	}
	n, err := l.r.Read(l.buf[l.end:])
	l.end += n
	if err != nil {
		l.err = err
	}
}
