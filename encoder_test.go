package sse

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoderSimple(t *testing.T) {
	type testCase struct {
		xs []*Event
		w  string
	}
	cases := []testCase{{
		[]*Event{
			{Event: []byte("hello"), Data: []byte("some data")},
			{Data: []byte("some other data with no event header")},
		},
		"event: hello\ndata: some data\n\ndata: some other data with no event header\n\n",
	},
		{
			[]*Event{
				{Event: []byte("hello"), Data: []byte("some \n funky\r\n data\r")},
				{Data: []byte("some other data with an id"), ID: ID("dogs")},
			}, "event: hello\ndata: some \ndata:  funky\r\ndata:  data\r\n\ndata: some other data with an id\nid: dogs\n\n",
		},
	}
	for _, v := range cases {
		buf := &bytes.Buffer{}
		enc := NewEncoder(buf)
		for _, p := range v.xs {
			require.NoError(t, enc.Encode(p))
		}
		assert.EqualValues(t, v.w, buf.String())
	}
}
