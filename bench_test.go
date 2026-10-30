package sse

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

var benchStream = strings.Repeat("event: update\nid: 12345\ndata: {\"price\":\"1234.5678\",\"size\":\"0.25\"}\ndata: second line\n\n", 1000)

func BenchmarkDecode(b *testing.B) {
	b.SetBytes(int64(len(benchStream)))
	b.ReportAllocs()
	r := strings.NewReader(benchStream)
	for b.Loop() {
		r.Reset(benchStream)
		d := NewDecoder(r)
		e := &Event{}
		for d.Decode(e) == nil {
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	ev := &Event{
		Event:  "update",
		ID:     []byte("12345"),
		Data:   []byte("{\"price\":\"1234.5678\",\"size\":\"0.25\"}\nsecond line"),
		Fields: map[string][]byte{"x-custom": []byte("3000")},
	}
	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(ev); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(buf.Len()))
	b.ReportAllocs()
	enc := NewEncoder(io.Discard)
	for b.Loop() {
		if err := enc.Encode(ev); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeLargeData(b *testing.B) {
	ev := &Event{Data: bytes.Repeat([]byte("0123456789abcdef\n"), 4096)}
	b.SetBytes(int64(len(ev.Data)))
	b.ReportAllocs()
	enc := NewEncoder(io.Discard)
	for b.Loop() {
		if err := enc.Encode(ev); err != nil {
			b.Fatal(err)
		}
	}
}
