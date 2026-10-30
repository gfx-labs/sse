package sse

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Exact wire format, including field order: comments, event, fields, retry, data, id.
func TestEncoderWireFormat(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	require.NoError(t, enc.Encode(&Event{
		Comments: [][]byte{[]byte("note"), {}},
		Event:    "hello",
		Fields:   map[string][]byte{"x": []byte("1")},
		Retry:    1500 * time.Millisecond,
		Data:     []byte("a\r\nb"),
		ID:       []byte{},
	}))
	require.NoError(t, enc.Comment(""))
	require.NoError(t, enc.Encode(&Event{Data: []byte("c"), ID: []byte("7")}))
	assert.Equal(t, ": note\n:\nevent: hello\nx: 1\nretry: 1500\ndata: a\ndata: b\nid\n\n:\ndata: c\nid: 7\n\n", buf.String())
}
