package sse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
		enc := NewReader(buf)
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

func TestReaderBOM(t *testing.T) {
	// covers: BOM detection on first read
	input := "\xef\xbb\xbfdata: hello\n\n"
	r := NewReader(strings.NewReader(input))
	require.NoError(t, r.Next())
	assert.Equal(t, TokenBom, r.Token().Type)
	require.NoError(t, r.Next())
	assert.Equal(t, TokenField, r.Token().Type)
}

func TestReaderCarriageReturn(t *testing.T) {
	// covers: \r\n line ending in the state machine
	// The reader processes \r\n: the \r triggers pending carriage, \n completes the line.
	// When pending carriage sees a non-\n byte, it also completes the line.
	input := "data: a\r\ndata: b\r\n\r\n"
	r := NewReader(strings.NewReader(input))
	require.NoError(t, r.Next())
	assert.Equal(t, TokenField, r.Token().Type)
	assert.Equal(t, "data: a", string(r.Token().Value))
	// Just drain remaining tokens to exercise the carriage return paths
	for r.Next() == nil {
	}
}

func TestReaderBareCarriageReturn(t *testing.T) {
	// covers: bare \r line ending (not followed by \n)
	input := "data: x\r\r"
	r := NewReader(strings.NewReader(input))
	require.NoError(t, r.Next())
	assert.Equal(t, TokenField, r.Token().Type)
	assert.Equal(t, "data: x", string(r.Token().Value))
}

func TestReaderComment(t *testing.T) {
	// covers: comment token
	input := ":comment\ndata: x\n\n"
	r := NewReader(strings.NewReader(input))
	require.NoError(t, r.Next())
	assert.Equal(t, TokenComment, r.Token().Type)
}

func TestReaderSkipDoubleDispatch(t *testing.T) {
	// covers: consecutive dispatches produce TokenSkip
	input := "data: x\n\n\n"
	r := NewReader(strings.NewReader(input))
	types := []TokenType{}
	for r.Next() == nil {
		types = append(types, r.Token().Type)
	}
	assert.Equal(t, []TokenType{TokenField, TokenDispatch, TokenSkip}, types)
}

func TestReaderMaxBufferSize(t *testing.T) {
	// covers: MaxBufferSize option limiting reads
	input := "data: this is a long line\n\n"
	r := NewReader(strings.NewReader(input), OptionMaxBufferSize(10))
	err := r.Next()
	// should hit EOF due to limited buffer
	assert.Error(t, err)
}

func TestTokenTypeString(t *testing.T) {
	assert.Equal(t, "invalid", TokenInvalid.String())
	assert.Equal(t, "skip", TokenSkip.String())
	assert.Equal(t, "bom", TokenBom.String())
	assert.Equal(t, "comment", TokenComment.String())
	assert.Equal(t, "field", TokenField.String())
	assert.Equal(t, "dispatch", TokenDispatch.String())
	assert.Equal(t, fmt.Sprintf("unknown (%d)", 99), TokenType(99).String())
}

func TestTokenString(t *testing.T) {
	var nilTok *Token
	assert.Equal(t, "<nil>", nilTok.String())

	tok := &Token{Type: TokenField, Value: []byte("data: hi")}
	assert.Equal(t, "field: data: hi", tok.String())
}
