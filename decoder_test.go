package sse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecoderAllFields(t *testing.T) {
	// covers: ID, event, retry, comment, custom field, data, copyBytes, trimField
	input := ":this is a comment\nevent: greeting\nid: 42\nretry: 3000\ncustom: value\ndata: hello\ndata: world\n\n"
	r := NewReader(strings.NewReader(input))
	dec := NewDecoder(r)
	ev := &Event{}
	require.NoError(t, dec.Decode(ev))

	assert.Equal(t, []byte("greeting"), ev.Event)
	require.NotNil(t, ev.ID)
	assert.Equal(t, []byte("42"), *ev.ID)
	assert.Equal(t, []byte("hello\nworld"), ev.Data)
	assert.Equal(t, [][]byte{[]byte(":this is a comment")}, ev.Comments)
	assert.Equal(t, []byte("3000"), ev.Fields["retry"])
	assert.Equal(t, []byte("value"), ev.Fields["custom"])
}

func TestDecoderBareData(t *testing.T) {
	// covers: bare "data" line (no colon) => empty newline in data
	input := "data\n\n"
	r := NewReader(strings.NewReader(input))
	dec := NewDecoder(r)
	ev := &Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte(""), ev.Data)
}

func TestDecoderBOM(t *testing.T) {
	// covers: TokenBom path in Decode
	input := "\xef\xbb\xbfdata: after bom\n\n"
	r := NewReader(strings.NewReader(input))
	dec := NewDecoder(r)
	ev := &Event{}
	require.NoError(t, dec.Decode(ev))
	assert.Equal(t, []byte("after bom"), ev.Data)
}

func TestDecoderNilPanic(t *testing.T) {
	r := NewReader(strings.NewReader("data: x\n\n"))
	dec := NewDecoder(r)
	assert.Panics(t, func() { _ = dec.Decode(nil) })
}

func TestEventReset(t *testing.T) {
	ev := &Event{
		Event:    []byte("test"),
		ID:       ID("1"),
		Data:     []byte("data"),
		Fields:   map[string][]byte{"k": []byte("v")},
		Comments: [][]byte{[]byte("c")},
	}
	ev.reset()
	assert.Nil(t, ev.ID)
	assert.Empty(t, ev.Event)
	assert.Empty(t, ev.Data)
	assert.Empty(t, ev.Fields)
	assert.Empty(t, ev.Comments)
}
