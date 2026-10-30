// Package e2e contains end-to-end tests verifying SSE specification compliance.
// Reference: https://html.spec.whatwg.org/multipage/server-sent-events.html
package e2e

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decode feeds raw SSE text through the decoder and returns copies of all events.
func decode(t *testing.T, raw string) []*sse.Event {
	t.Helper()
	var events []*sse.Event
	for ev, err := range sse.Events(bytes.NewReader([]byte(raw))) {
		require.NoError(t, err)
		events = append(events, ev.Clone())
	}
	return events
}

// --- Line ending handling (spec section 9.2.4) ---

func TestSpecLineEndingLF(t *testing.T) {
	raw := "data: hello\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("hello"), evs[0].Data)
}

func TestSpecLineEndingCR(t *testing.T) {
	raw := "data: hello\r\r"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("hello"), evs[0].Data)
}

func TestSpecLineEndingCRLF(t *testing.T) {
	raw := "data: hello\r\n\r\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("hello"), evs[0].Data)
}

func TestSpecMixedLineEndings(t *testing.T) {
	// Mix of \n, \r, and \r\n within a single event stream
	raw := "data: a\ndata: b\rdata: c\r\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("a\nb\nc"), evs[0].Data)
}

// --- BOM handling (spec section 9.2.4) ---

func TestSpecBOMStripped(t *testing.T) {
	// UTF-8 BOM (0xEF 0xBB 0xBF) must be ignored at stream start
	raw := "\xef\xbb\xbfdata: after bom\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("after bom"), evs[0].Data)
}

func TestSpecNoBOM(t *testing.T) {
	// Streams without BOM should work identically
	raw := "data: no bom\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("no bom"), evs[0].Data)
}

// --- Field parsing (spec section 9.2.4) ---

func TestSpecDataField(t *testing.T) {
	raw := "data: simple\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("simple"), evs[0].Data)
}

func TestSpecEventField(t *testing.T) {
	raw := "event: myevent\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, "myevent", evs[0].Event)
	assert.Equal(t, []byte("payload"), evs[0].Data)
}

func TestSpecIDField(t *testing.T) {
	raw := "id: 42\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	require.NotNil(t, evs[0].ID)
	assert.Equal(t, []byte("42"), evs[0].ID)
}

func TestSpecEmptyID(t *testing.T) {
	// "id:" with no value resets the last event id: ID is empty but not nil
	raw := "id:\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	require.NotNil(t, evs[0].ID)
	assert.Empty(t, evs[0].ID)
}

func TestSpecRetryField(t *testing.T) {
	raw := "retry: 5000\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("payload"), evs[0].Data)
	assert.Equal(t, 5*time.Second, evs[0].Retry)
	assert.Empty(t, evs[0].Fields)
}

// --- Multi-line data (spec section 9.2.4) ---

func TestSpecMultiLineData(t *testing.T) {
	// Multiple data lines are concatenated with \n
	raw := "data: line1\ndata: line2\ndata: line3\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("line1\nline2\nline3"), evs[0].Data)
}

func TestSpecDataTrailingNewlineStripped(t *testing.T) {
	// Per spec, the trailing \n from concatenation is stripped on dispatch
	raw := "data: only\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("only"), evs[0].Data)
	// Should not end with \n
	assert.False(t, bytes.HasSuffix(evs[0].Data, []byte("\n")))
}

// --- Bare "data" line (spec section 9.2.4) ---

func TestSpecBareDataLine(t *testing.T) {
	// A line with just "data" (no colon) should produce an empty data field
	raw := "data\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte(""), evs[0].Data)
}

func TestSpecBareDataWithOtherData(t *testing.T) {
	// "data" line followed by "data: text" line
	raw := "data\ndata: text\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("\ntext"), evs[0].Data)
}

// --- Comments (spec section 9.2.4) ---

func TestSpecCommentIgnored(t *testing.T) {
	// Lines starting with ":" are comments and must not affect data
	raw := ": this is a comment\ndata: real data\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("real data"), evs[0].Data)
}

func TestSpecCommentOnlyStream(t *testing.T) {
	// Comments alone, such as keep-alives, do not dispatch an event
	raw := ": comment1\n: comment2\n\n"
	assert.Empty(t, decode(t, raw))
}

func TestSpecCommentText(t *testing.T) {
	// Comment text excludes the colon and one optional leading space
	raw := ":hello\n:  two\ndata: x\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	require.Len(t, evs[0].Comments, 2)
	assert.Equal(t, "hello", string(evs[0].Comments[0]))
	assert.Equal(t, " two", string(evs[0].Comments[1]))
}

// --- Unknown fields (spec section 9.2.4) ---

func TestSpecUnknownFieldIgnored(t *testing.T) {
	// Unknown fields are collected in the Fields map
	raw := "foo: bar\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("payload"), evs[0].Data)
	assert.Equal(t, []byte("bar"), evs[0].Fields["foo"])
}

// --- Leading space stripping (spec section 9.2.4) ---

func TestSpecLeadingSpaceStripped(t *testing.T) {
	// First space after colon should be stripped per spec
	raw := "data: hello\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("hello"), evs[0].Data)
}

func TestSpecNoSpaceAfterColon(t *testing.T) {
	raw := "data:hello\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("hello"), evs[0].Data)
}

func TestSpecMultipleSpacesAfterColon(t *testing.T) {
	// Only the first space is stripped; additional spaces are part of the value
	raw := "data:  two spaces\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte(" two spaces"), evs[0].Data)
}

// --- Multiple events in stream ---

func TestSpecMultipleEvents(t *testing.T) {
	raw := "data: first\n\ndata: second\n\ndata: third\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 3)
	assert.Equal(t, []byte("first"), evs[0].Data)
	assert.Equal(t, []byte("second"), evs[1].Data)
	assert.Equal(t, []byte("third"), evs[2].Data)
}

func TestSpecEventWithAllFields(t *testing.T) {
	raw := "id: 1\nevent: update\nretry: 1000\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	ev := evs[0]
	require.NotNil(t, ev.ID)
	assert.Equal(t, []byte("1"), ev.ID)
	assert.Equal(t, "update", ev.Event)
	assert.Equal(t, []byte("payload"), ev.Data)
	assert.Equal(t, time.Second, ev.Retry)
}

// --- Consecutive blank lines ---

func TestSpecConsecutiveBlankLines(t *testing.T) {
	// Extra blank lines do not dispatch empty events
	raw := "data: x\n\n\n\n"
	assert.Len(t, decode(t, raw), 1)
}

// --- Empty stream ---

func TestSpecEmptyStream(t *testing.T) {
	evs := decode(t, "")
	assert.Len(t, evs, 0)
}

// --- Stream with only whitespace/newlines ---

func TestSpecOnlyNewlines(t *testing.T) {
	assert.Empty(t, decode(t, "\n\n\n"))
}

// The line limit applies per line, so long streams of short lines are fine.
func TestSpecMaxLineSize(t *testing.T) {
	raw := "data: short\n\ndata: " + string(bytes.Repeat([]byte("x"), 100)) + "\n\n"
	var got []string
	var err error
	for ev, e := range sse.Events(bytes.NewReader([]byte(raw)), sse.WithMaxLineSize(32)) {
		if e != nil {
			err = e
			break
		}
		got = append(got, string(ev.Data))
	}
	require.ErrorIs(t, err, sse.ErrLineTooLong)
	assert.Equal(t, []string{"short"}, got)
}

// Lines split across reads, including a "\r\n" split between reads, decode correctly.
func TestSpecSplitReads(t *testing.T) {
	raw := "data: a\r\ndata: bcd\r\n\r\nevent: e\rdata: f\r\r"
	var got []string
	for ev, err := range sse.Events(&oneByteReader{s: raw}) {
		require.NoError(t, err)
		got = append(got, ev.Event+"|"+string(ev.Data))
	}
	assert.Equal(t, []string{"|a\nbcd", "e|f"}, got)
}

type oneByteReader struct{ s string }

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	p[0] = r.s[0]
	r.s = r.s[1:]
	return 1, nil
}

// A reused Event must not keep fields from the previous event.
func TestSpecDecoderReuse(t *testing.T) {
	d := sse.NewDecoder(bytes.NewReader([]byte("event: x\nid: 1\nfoo: y\nretry: 5\n:c\ndata: 1\n\ndata: 2\n\n")))
	var ev sse.Event
	require.NoError(t, d.Decode(&ev))
	require.NoError(t, d.Decode(&ev))
	assert.Empty(t, ev.Event)
	assert.Nil(t, ev.ID)
	assert.Empty(t, ev.Fields)
	assert.Empty(t, ev.Comments)
	assert.Zero(t, ev.Retry)
	assert.Equal(t, "2", string(ev.Data))
	require.ErrorIs(t, d.Decode(&ev), io.EOF)
}
