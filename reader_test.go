package sse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReaderSimple(t *testing.T) {
	type testCase struct {
		dat    string
		tokens []TokenType
		lines  []string
	}
	cases := []testCase{
		{
			`data: foo bar
data: bar foo
data: wowwwwza

data: hi there
event: woah
			`,
			[]TokenType{
				TokenField, TokenField, TokenField, TokenDispatch, TokenField, TokenField,
			}, []string{
				"data: foo bar", "data: bar foo", "data: wowwwwza", "", "data: hi there", "event: woah",
			},
		},
		{
			`event: hello
data: there
data: friend
			`,
			[]TokenType{
				TokenField, TokenField, TokenField,
			}, []string{
				"event: hello", "data: there", "data: friend",
			},
		},
	}

	for d, v := range cases {
		require.Equalf(t, len(v.tokens), len(v.lines), "bad test case: %d", d)
		buf := strings.NewReader(v.dat)
		enc := NewReader(buf, -1)
		idx := 0
		for enc.Next() == nil {
			tok := enc.Token()
			if idx < len(v.lines) {
				require.Equalf(t, v.tokens[idx], tok.Type, "idx: %d", idx)
				require.EqualValuesf(t, v.lines[idx], string(tok.Value), "idx: %d", idx)
			}
			idx++
		}
		require.Equal(t, len(v.lines), idx)
	}
}
