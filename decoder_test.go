package sse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func decodeAll(t *testing.T, s string, opts ...Option) ([]Event, error) {
	t.Helper()
	d := NewDecoder(NewReader(strings.NewReader(s), opts...))
	var out []Event
	for {
		e := &Event{}
		if err := d.Decode(e); err != nil {
			return out, err
		}
		out = append(out, *e)
	}
}

func TestDecoderLineEndings(t *testing.T) {
	for name, in := range map[string]string{
		"lf":   "data: a\ndata: bcd\n\n",
		"crlf": "data: a\r\ndata: bcd\r\n\r\n",
		"cr":   "data: a\rdata: bcd\r\r",
		"bom":  "\uFEFFdata: a\ndata: bcd\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			evs, _ := decodeAll(t, in)
			require.Len(t, evs, 1)
			require.Equal(t, "a\nbcd", string(evs[0].Data))
		})
	}
}

func TestDecoderFields(t *testing.T) {
	evs, _ := decodeAll(t, "retry: 3000\nfoo: bar\nbaz\nid: 7\nevent: e\n\n")
	require.Len(t, evs, 1)
	e := evs[0]
	require.Equal(t, "3000", string(e.Fields["retry"]))
	require.Equal(t, "bar", string(e.Fields["foo"]))
	require.Contains(t, e.Fields, "baz")
	require.Equal(t, "7", string(*e.ID))
	require.Equal(t, "e", string(e.Event))
}

// Decoding into a reused Event must not keep fields from the previous event.
func TestDecoderReuse(t *testing.T) {
	d := NewDecoder(NewReader(strings.NewReader("event: x\nid: 1\nfoo: y\n\ndata: 2\n\n")))
	e := &Event{}
	require.NoError(t, d.Decode(e))
	require.NoError(t, d.Decode(e))
	require.Empty(t, e.Event)
	require.Nil(t, e.ID)
	require.Empty(t, e.Fields)
	require.Equal(t, "2", string(e.Data))
}

func TestReaderMaxBufferSize(t *testing.T) {
	evs, err := decodeAll(t, "data: short\n\ndata: "+strings.Repeat("x", 100)+"\n\n", OptionMaxBufferSize(32))
	require.ErrorIs(t, err, ErrLineTooLong)
	require.Len(t, evs, 1)
	require.Equal(t, "short", string(evs[0].Data))
}

func TestDecoderBareData(t *testing.T) {
	// covers: bare "data" line (no colon) => empty newline in data
	input := "data\n\n"
	r := NewReader(strings.NewReader(input))
	dec := NewDecoder(r)
	ev := &Event{}
	require.NoError(t, dec.Decode(ev))
	require.Equal(t, []byte(""), ev.Data)
}

func TestDecoderNilPanic(t *testing.T) {
	r := NewReader(strings.NewReader("data: x\n\n"))
	dec := NewDecoder(r)
	require.Panics(t, func() { _ = dec.Decode(nil) })
}
