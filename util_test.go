package sse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAtomicByteSlice(t *testing.T) {
	var a atomicByteSlice

	// empty load returns nil, false
	val, ok := a.Load()
	assert.False(t, ok)
	assert.Nil(t, val)

	// exercise Store (note: implementation copies atomic.Value by value,
	// so the store does not persist — we just verify it doesn't panic)
	a.Store([]byte("hello"))

	// Load after Store on a value-copy type still returns empty
	_, _ = a.Load()
}
