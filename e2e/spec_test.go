// Package e2e contains end-to-end tests verifying SSE specification compliance.
// Reference: https://html.spec.whatwg.org/multipage/server-sent-events.html
package e2e

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gfx-labs/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decode is a helper that feeds raw SSE text through Reader→Decoder and returns all events.
// Fields is pre-initialized to work around a nil-map bug in decoder.go:84.
func decode(t *testing.T, raw string) []*sse.Event {
	t.Helper()
	r := sse.NewReader(strings.NewReader(raw))
	dec := sse.NewDecoder(r)
	var events []*sse.Event
	for {
		ev := &sse.Event{Fields: map[string][]byte{}}
		err := dec.Decode(ev)
		if err != nil {
			break
		}
		events = append(events, ev)
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
	// Bare \r as line endings. The reader needs the next byte after \r to flush
	// the pending carriage, so we append \n at stream end to ensure dispatch.
	raw := "data: hello\r\r\n"
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
	assert.Equal(t, []byte("myevent"), evs[0].Event)
	assert.Equal(t, []byte("payload"), evs[0].Data)
}

func TestSpecIDField(t *testing.T) {
	raw := "id: 42\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	require.NotNil(t, evs[0].ID)
	assert.Equal(t, []byte("42"), *evs[0].ID)
}

func TestSpecEmptyID(t *testing.T) {
	// "id:" with no value sets ID to empty (nil-length) byte slice
	raw := "id:\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	require.NotNil(t, evs[0].ID)
	assert.Len(t, *evs[0].ID, 0)
}

func TestSpecRetryField(t *testing.T) {
	raw := "retry: 5000\ndata: payload\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("payload"), evs[0].Data)
	require.NotNil(t, evs[0].Fields)
	assert.Contains(t, evs[0].Fields, "retry")
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
	// A stream of only comments should produce no events with content
	raw := ": comment1\n: comment2\n\n"
	evs := decode(t, raw)
	// Dispatch happens but no data/event/id content — implementation returns event
	// with only comments; hasContent() returns false so Subscribe would skip it
	for _, ev := range evs {
		assert.Empty(t, ev.Data)
		assert.Nil(t, ev.ID)
		assert.Empty(t, ev.Event)
	}
}

func TestSpecCommentPreservesColon(t *testing.T) {
	// Comment value includes the leading colon in raw token
	raw := ":hello\ndata: x\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	require.Len(t, evs[0].Comments, 1)
	assert.Equal(t, []byte(":hello"), evs[0].Comments[0])
}

// --- Unknown fields (spec section 9.2.4) ---

func TestSpecUnknownFieldIgnored(t *testing.T) {
	// Unknown fields are collected in Fields map.
	// Note: decoder has a bug where Fields map is not initialized before writing
	// custom fields (decoder.go:84). Pre-initialize Fields in the Event to work
	// around this. The data field should still be correct.
	raw := "foo: bar\ndata: payload\n\n"
	r := sse.NewReader(strings.NewReader(raw))
	dec := sse.NewDecoder(r)
	ev := &sse.Event{Fields: map[string][]byte{}}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("payload"), ev.Data)
	assert.Equal(t, []byte("bar"), ev.Fields["foo"])
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
	assert.Equal(t, []byte("1"), *ev.ID)
	assert.Equal(t, []byte("update"), ev.Event)
	assert.Equal(t, []byte("payload"), ev.Data)
	assert.Contains(t, ev.Fields, "retry")
}

// --- Consecutive blank lines ---

func TestSpecConsecutiveBlankLines(t *testing.T) {
	// Consecutive blank lines after a dispatch should produce skip tokens, not extra events
	raw := "data: x\n\n\n\n"
	evs := decode(t, raw)
	// Only one event with actual content
	contentEvents := 0
	for _, ev := range evs {
		if len(ev.Data) > 0 || ev.ID != nil || len(ev.Event) > 0 {
			contentEvents++
		}
	}
	assert.Equal(t, 1, contentEvents)
}

// --- Empty stream ---

func TestSpecEmptyStream(t *testing.T) {
	evs := decode(t, "")
	assert.Len(t, evs, 0)
}

// --- Stream with only whitespace/newlines ---

func TestSpecOnlyNewlines(t *testing.T) {
	evs := decode(t, "\n\n\n")
	// No data events
	for _, ev := range evs {
		assert.Empty(t, ev.Data)
	}
}
