// Package e2e contains end-to-end tests verifying SSE specification compliance.
// This file ports the Web Platform Tests (WPT) for EventSource to Go.
// Reference: https://github.com/web-platform-tests/wpt/tree/master/eventsource
// These are the exact test vectors that browsers use for interop testing.
package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- WPT: format-field-data ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-data.any.js
// Stream: "data:\n\ndata\ndata\n\ndata:test"
// Expects 3 events: data="", data="\n", data="test"
func TestWPTFormatFieldData(t *testing.T) {
	raw := "data:\n\ndata\ndata\n\ndata:test\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 3, "expected 3 events")
	assert.Equal(t, []byte(""), evs[0].Data, "event 0: empty data from 'data:\\n'")
	assert.Equal(t, []byte("\n"), evs[1].Data, "event 1: newline from 'data\\ndata\\n'")
	assert.Equal(t, []byte("test"), evs[2].Data, "event 2: 'test' from 'data:test'")
}

// --- WPT: format-bom ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-bom.any.js
// Stream: BOM + "data:1\n\n" + BOM + "data:2\n\ndata:3"
// BOM at start is stripped. Mid-stream BOM becomes part of field name
// "\xef\xbb\xbfdata" (unknown). Event "2" is NOT dispatched as data.
func TestWPTFormatBOM(t *testing.T) {
	raw := "\xef\xbb\xbfdata:1\n\n\xef\xbb\xbfdata:2\n\ndata:3\n\n"
	evs := decode(t, raw)

	var dataEvents []string
	for _, ev := range evs {
		if len(ev.Data) > 0 {
			dataEvents = append(dataEvents, string(ev.Data))
		}
	}

	require.Contains(t, dataEvents, "1", "BOM at start stripped, event 1 dispatched")
	require.Contains(t, dataEvents, "3", "event 3 dispatched")

	for _, ev := range evs {
		assert.NotEqual(t, []byte("2"), ev.Data,
			"mid-stream BOM taints field name; event 2 should not be data")
	}
}

// --- WPT: format-bom-2 ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-bom-2.any.js
// Stream: BOM + BOM + "data:1\n\ndata:2\n\ndata:3"
// Only first BOM stripped. Second BOM taints "data:1" → not dispatched as data.
func TestWPTFormatBOM2(t *testing.T) {
	raw := "\xef\xbb\xbf\xef\xbb\xbfdata:1\n\ndata:2\n\ndata:3\n\n"
	evs := decode(t, raw)

	var dataEvents []string
	for _, ev := range evs {
		if len(ev.Data) > 0 {
			dataEvents = append(dataEvents, string(ev.Data))
		}
	}

	for _, ev := range evs {
		assert.NotEqual(t, []byte("1"), ev.Data,
			"double BOM: only first stripped, event 1 should not have data")
	}
	require.Contains(t, dataEvents, "2", "event 2 dispatched")
	require.Contains(t, dataEvents, "3", "event 3 dispatched")
}

// --- WPT: format-comments ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-comments.any.js
// Uses mixed \r/\n/\r\n line endings with comments containing null and long strings.
// Expected: single event with data "1\n2\n3\n4".
func TestWPTFormatComments(t *testing.T) {
	longstring := make([]byte, 2*1024)
	for i := range longstring {
		longstring[i] = 'x'
	}
	// Exact WPT stream with mixed line endings.
	raw := "data:1\r:\x00\n:\r\ndata:2\n:" + string(longstring) + "\rdata:3\n:data:fail\r:" + string(longstring) + "\ndata:4\n\n"
	evs := decode(t, raw)

	var found bool
	for _, ev := range evs {
		if string(ev.Data) == "1\n2\n3\n4" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected event with data '1\\n2\\n3\\n4'")
}

// TestWPTFormatCommentsNullInComment verifies null character in comments is fine.
func TestWPTFormatCommentsNullInComment(t *testing.T) {
	raw := ":\x00\ndata:ok\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("ok"), evs[0].Data)
	require.Len(t, evs[0].Comments, 1)
}

// TestWPTFormatCommentsLongComment verifies long comment lines are handled.
func TestWPTFormatCommentsLongComment(t *testing.T) {
	longstring := make([]byte, 2*1024)
	for i := range longstring {
		longstring[i] = 'x'
	}
	raw := ":" + string(longstring) + "\ndata:ok\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("ok"), evs[0].Data)
}

// TestWPTFormatCommentsDataFail verifies ":data:fail" is a comment, not a field.
func TestWPTFormatCommentsDataFail(t *testing.T) {
	raw := ":data:fail\ndata:ok\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("ok"), evs[0].Data,
		"':data:fail' is a comment, not a data field")
}

// --- WPT: format-field-event ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-event.any.js
// Stream: "event:test\ndata:x\n\ndata:x"
func TestWPTFormatFieldEvent(t *testing.T) {
	raw := "event:test\ndata:x\n\ndata:x\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 2)
	assert.Equal(t, []byte("test"), evs[0].Event)
	assert.Equal(t, []byte("x"), evs[0].Data)
	assert.Empty(t, evs[1].Event, "second event has default (empty) event type")
	assert.Equal(t, []byte("x"), evs[1].Data)
}

// --- WPT: format-field-event-empty ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-event-empty.any.js
// "event: " → empty event type (space stripped).
func TestWPTFormatFieldEventEmpty(t *testing.T) {
	raw := "event: \ndata:data\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("data"), evs[0].Data)
	assert.Equal(t, []byte(""), evs[0].Event)
}

// --- WPT: format-field-parsing ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-parsing.any.js
// Exact WPT stream with mixed \r and \n. Tests case sensitivity, null in field
// names, leading space in field names, and other parsing rules.
// Expected: single event with data "\0\n 2\n1\n3\n\n4"
func TestWPTFormatFieldParsing(t *testing.T) {
	raw := "data:\x00\ndata:  2\rData:1\ndata\x00:2\ndata:1\r\x00data:4\nda-ta:3\rdata_5\ndata:3\rdata:\r\n data:32\ndata:4\n\n"
	evs := decode(t, raw)
	require.GreaterOrEqual(t, len(evs), 1)

	expected := "\x00\n 2\n1\n3\n\n4"
	var found bool
	for _, ev := range evs {
		if string(ev.Data) == expected {
			found = true
			break
		}
	}
	assert.True(t, found, "expected event with data %q", expected)
}

// TestWPTFormatFieldParsingCaseSensitive verifies "Data" != "data".
func TestWPTFormatFieldParsingCaseSensitive(t *testing.T) {
	raw := "Data:1\ndata:hello\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("hello"), evs[0].Data)
	assert.Equal(t, []byte("1"), evs[0].Fields["Data"])
}

// TestWPTFormatFieldParsingNullInFieldName verifies "data\x00" != "data".
func TestWPTFormatFieldParsingNullInFieldName(t *testing.T) {
	raw := "data\x00:2\ndata:real\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("real"), evs[0].Data)
}

// TestWPTFormatFieldParsingSpaceInFieldName verifies " data" != "data".
func TestWPTFormatFieldParsingSpaceInFieldName(t *testing.T) {
	raw := " data:32\ndata:real\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("real"), evs[0].Data)
}

// TestWPTFormatFieldParsingNullStartLine verifies "\x00data" != "data".
func TestWPTFormatFieldParsingNullStartLine(t *testing.T) {
	raw := "\x00data:4\ndata:real\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("real"), evs[0].Data)
}

// TestWPTFormatFieldParsingBareFieldName verifies "data_5" is unknown.
func TestWPTFormatFieldParsingBareFieldName(t *testing.T) {
	raw := "data_5\ndata:real\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("real"), evs[0].Data)
}

// --- WPT: format-null-character ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-null-character.any.js
func TestWPTFormatNullCharacter(t *testing.T) {
	raw := "data:\x00\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("\x00"), evs[0].Data, "null character preserved in data")
}

// --- WPT: format-leading-space ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-leading-space.any.js
// Exact WPT stream: "data:\ttest\rdata: \ndata:test"
// Tab is NOT a space — not stripped. Only one U+0020 is stripped.
// Uses bare \r between first and second data lines.
// Expected: data "\ttest\n\ntest"
func TestWPTFormatLeadingSpace(t *testing.T) {
	raw := "data:\ttest\rdata: \ndata:test\n\n"
	evs := decode(t, raw)
	require.GreaterOrEqual(t, len(evs), 1)

	expected := "\ttest\n\ntest"
	var found bool
	for _, ev := range evs {
		if string(ev.Data) == expected {
			found = true
			break
		}
	}
	assert.True(t, found, "expected data %q; tab should not be stripped", expected)
}

// TestWPTFormatLeadingSpaceTabPreserved verifies tab after colon is kept.
func TestWPTFormatLeadingSpaceTabPreserved(t *testing.T) {
	raw := "data:\tvalue\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("\tvalue"), evs[0].Data)
}

// TestWPTFormatLeadingSpaceOnlyOneStripped verifies only first space is stripped.
func TestWPTFormatLeadingSpaceOnlyOneStripped(t *testing.T) {
	raw := "data:  two\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte(" two"), evs[0].Data)
}

// --- WPT: format-field-unknown ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-unknown.any.js
// Stream: "data:test\n data\ndata\nfoobar:xxx\njustsometext\n:thisisacommentyay\ndata:test"
// Expected: data "test\n\ntest"
func TestWPTFormatFieldUnknown(t *testing.T) {
	raw := "data:test\n data\ndata\nfoobar:xxx\njustsometext\n:thisisacommentyay\ndata:test\n\n"
	evs := decode(t, raw)
	require.GreaterOrEqual(t, len(evs), 1)

	expected := "test\n\ntest"
	var found bool
	for _, ev := range evs {
		if string(ev.Data) == expected {
			found = true
			break
		}
	}
	assert.True(t, found, "expected data %q", expected)
}

// --- WPT: format-data-before-final-empty-line ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-data-before-final-empty-line.any.js
// Incomplete event at EOF is discarded.
func TestWPTFormatDataBeforeFinalEmptyLine(t *testing.T) {
	raw := "retry:1000\ndata:test1\n\nid:test\ndata:test2"
	evs := decode(t, raw)

	var dataValues []string
	for _, ev := range evs {
		if len(ev.Data) > 0 {
			dataValues = append(dataValues, string(ev.Data))
		}
	}

	require.Contains(t, dataValues, "test1")
	assert.NotContains(t, dataValues, "test2",
		"incomplete event at EOF should NOT be dispatched")
}

// --- WPT: event-data (message2.py) ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/event-data.any.js
// WPT expects 3 onmessage events: "msg\nmsg", "", "end"
//
// The stream produces 6 decoder dispatches total. Only 3 have the data field
// set (Data non-nil):
//
//	event 0: data="msg\nmsg"  (from "data:msg\ndata: msg")
//	event 4: data=""          (from bare "data" line — Data is non-nil but empty)
//	event 5: data="end"       (from "data:end")
//
// The other dispatches have nil Data (comments-only, unknown-fields-only).
func TestWPTEventData(t *testing.T) {
	raw := "data:msg\ndata: msg\n\n:\n\nfalsefield:msg\n\nfalsefield:msg\nData:data\n\ndata\n\ndata:end\n\n"
	evs := decode(t, raw)

	// Filter to events where Data was set (non-nil).
	var messageEvents []string
	for _, ev := range evs {
		if ev.Data != nil {
			messageEvents = append(messageEvents, string(ev.Data))
		}
	}

	require.Len(t, messageEvents, 3, "expected 3 events with Data set")
	assert.Equal(t, "msg\nmsg", messageEvents[0], "multi-line data")
	assert.Equal(t, "", messageEvents[1], "bare 'data' line = empty data")
	assert.Equal(t, "end", messageEvents[2], "final event")
}

// TestWPTEventDataCaseSensitivity verifies "Data" (capital D) is unknown.
func TestWPTEventDataCaseSensitivity(t *testing.T) {
	raw := "Data:data\ndata:real\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("real"), evs[0].Data)
}

// TestWPTEventDataBareDataLine verifies "data" without colon = empty data.
func TestWPTEventDataBareDataLine(t *testing.T) {
	raw := "data\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte(""), evs[0].Data)
}

// --- WPT: format-newlines ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-newlines.any.js
// Exact WPT stream: "data:test\r\ndata\ndata:test\r\n\r"
// Uses CRLF and bare \r. Expected: data "test\n\ntest".
func TestWPTFormatNewlines(t *testing.T) {
	// Exact WPT stream with mixed CRLF and bare \r.
	// Append \n to flush the final bare \r's pending carriage.
	raw := "data:test\r\ndata\ndata:test\r\n\r\n"
	evs := decode(t, raw)
	require.GreaterOrEqual(t, len(evs), 1)

	expected := "test\n\ntest"
	var found bool
	for _, ev := range evs {
		if string(ev.Data) == expected {
			found = true
			break
		}
	}
	assert.True(t, found, "expected data %q with mixed CRLF/LF line endings", expected)
}

// TestWPTFormatNewlinesCRLFMultiEvent tests CRLF with multiple events.
func TestWPTFormatNewlinesCRLFMultiEvent(t *testing.T) {
	raw := "data:first\r\n\r\ndata:second\r\n\r\n"
	evs := decode(t, raw)

	var dataEvents []string
	for _, ev := range evs {
		if len(ev.Data) > 0 {
			dataEvents = append(dataEvents, string(ev.Data))
		}
	}
	require.Len(t, dataEvents, 2)
	assert.Equal(t, "first", dataEvents[0])
	assert.Equal(t, "second", dataEvents[1])
}

// TestWPTFormatNewlinesCRLFMultiLine tests CRLF with multi-line data.
func TestWPTFormatNewlinesCRLFMultiLine(t *testing.T) {
	raw := "data:test\r\ndata\r\ndata:test\r\n\r\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, "test\n\ntest", string(evs[0].Data))
}

// --- WPT: format-field-retry-empty ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-retry-empty.any.js
func TestWPTFormatFieldRetryEmpty(t *testing.T) {
	raw := "retry\ndata:test\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("test"), evs[0].Data)
}

// --- WPT: format-field-retry-bogus ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-retry-bogus.any.js
// "retry:1000x" is not all digits — should be ignored per spec.
func TestWPTFormatFieldRetryBogus(t *testing.T) {
	raw := "retry:3000\nretry:1000x\ndata:x\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("x"), evs[0].Data)
}

// --- WPT: format-utf-8 ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-utf-8.any.js
// SSE is always UTF-8 regardless of charset parameter.
func TestWPTFormatUTF8(t *testing.T) {
	raw := "data:ok\xe2\x80\xa6\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	assert.Equal(t, []byte("ok\xe2\x80\xa6"), evs[0].Data)
	assert.Equal(t, "ok…", string(evs[0].Data))
}

// --- WPT: format-field-id ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-id.any.js
func TestWPTFormatFieldID(t *testing.T) {
	raw := "id:\xe2\x80\xa6\ndata:hello\n\n"
	evs := decode(t, raw)
	require.Len(t, evs, 1)
	require.NotNil(t, evs[0].ID)
	assert.Equal(t, []byte("\xe2\x80\xa6"), *evs[0].ID)
	assert.Equal(t, []byte("hello"), evs[0].Data)
}

// --- WPT: format-field-id-null ---
// Source: https://github.com/web-platform-tests/wpt/blob/master/eventsource/format-field-id-null.window.js
// Per spec, id field values containing U+0000 must be ignored.
// The library stores the id regardless (it doesn't enforce the null check).
// We verify parsing succeeds and data is correct.
func TestWPTFormatFieldIDNull(t *testing.T) {
	type testCase struct {
		idValue string
		desc    string
	}
	cases := []testCase{
		{"\x00\x00", "double null"},
		{"x\x00", "trailing null"},
		{"\x00x", "leading null"},
		{"x\x00x", "embedded null"},
		{" \x00", "space and null"},
	}
	for idx, tc := range cases {
		raw := "id: " + tc.idValue + "\ndata:hello\n\n"
		evs := decode(t, raw)
		require.Lenf(t, evs, 1, "idx %d (%s): expected 1 event", idx, tc.desc)
		assert.Equalf(t, []byte("hello"), evs[0].Data,
			"idx %d (%s): data should be 'hello'", idx, tc.desc)
	}
}
