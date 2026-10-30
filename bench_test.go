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
	for b.Loop() {
		d := NewDecoder(NewReader(strings.NewReader(benchStream)))
		e := &Event{}
		for d.Decode(e) == nil {
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	ev := &Event{
		Event:  []byte("update"),
		ID:     ID("12345"),
		Data:   []byte("{\"price\":\"1234.5678\",\"size\":\"0.25\"}\nsecond line"),
		Fields: map[string][]byte{"retry": []byte("3000")},
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

func BenchmarkWriterLargeData(b *testing.B) {
	data := bytes.Repeat([]byte("0123456789abcdef\n"), 4096)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	w := NewWriter(io.Discard)
	for b.Loop() {
		if _, err := w.Write(data); err != nil {
			b.Fatal(err)
		}
		if err := w.Next(); err != nil {
			b.Fatal(err)
		}
	}
}
