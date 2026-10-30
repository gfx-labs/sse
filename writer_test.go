package sse

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoderWrite(t *testing.T) {
	type testCase struct {
		e string
		i string
		w string
	}
	cases := []testCase{{
		"",
		"foo bar\nbar foo\nwowwwwza\n",
		`data: foo bar
data: bar foo
data: wowwwwza
`}, {
		"hello",
		"there\nfriend",
		`event: hello
data: there
data: friend
`},
	}

	for _, v := range cases {
		buf := &bytes.Buffer{}
		enc := NewWriter(buf)
		err := enc.Field("event", []byte(v.e))
		require.NoError(t, err)
		_, err = enc.Write([]byte(v.i))
		require.NoError(t, err)
		require.NoError(t, enc.Flush())
		assert.EqualValues(t, buf.String(), v.w)
	}
}

func TestWriterUTF8Validation(t *testing.T) {
	buf := &bytes.Buffer{}
	w := NewWriter(buf, OptionValidateUtf8(true))

	// invalid UTF-8 in Field
	err := w.Field("event", []byte{0xff, 0xfe})
	require.ErrorIs(t, err, ErrInvalidUTF8Bytes)

	// invalid UTF-8 in Write
	_, err = w.Write([]byte{0xff, 0xfe})
	require.ErrorIs(t, err, ErrInvalidUTF8Bytes)
}

func TestWriterFieldEmptyID(t *testing.T) {
	// covers: Field with name "id" and empty topic writes bare "id\n"
	buf := &bytes.Buffer{}
	w := NewWriter(buf)
	require.NoError(t, w.Field("id", nil))
	assert.Equal(t, "id\n", buf.String())
}

func TestWriterFieldEmptyNonID(t *testing.T) {
	// covers: Field with non-id name and empty topic is a no-op
	buf := &bytes.Buffer{}
	w := NewWriter(buf)
	require.NoError(t, w.Field("event", nil))
	assert.Equal(t, "", buf.String())
}

func TestWriterCarriageReturn(t *testing.T) {
	// covers: trailing carriage return handling in Write
	buf := &bytes.Buffer{}
	w := NewWriter(buf)
	_, err := w.Write([]byte("line1\rline2"))
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	assert.Equal(t, "data: line1\rdata: line2\n", buf.String())
}

func TestWriterMaxBufferSize(t *testing.T) {
	// covers: OptionMaxBufferSize path in NewWriter/NewReader
	buf := &bytes.Buffer{}
	w := NewWriter(buf, OptionMaxBufferSize(1024))
	require.NoError(t, w.Field("event", []byte("test")))
}

func TestEncoderReadFrom(t *testing.T) {
	type testCase struct {
		e string
		i string
		w string
	}
	cases := []testCase{{
		"",
		"foo bar\nbar foo\nwowwwwza\n",
		`data: foo bar
data: bar foo
data: wowwwwza
`}, {
		"hello",
		"there\nfriend",
		`event: hello
data: there
data: friend
`},
	}

	for _, v := range cases {
		buf := &bytes.Buffer{}
		enc := NewWriter(buf)
		err := enc.Field("event", []byte(v.e))
		require.NoError(t, err)
		_, err = enc.ReadFrom(strings.NewReader(v.i))
		require.NoError(t, err)
		require.NoError(t, enc.Flush())
		assert.EqualValues(t, v.w, buf.String())
	}
}
