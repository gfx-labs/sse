package sse

import "sync/atomic"

type atomicByteSlice atomic.Value

func (a *atomicByteSlice) Store(xs []byte) {
	val := atomic.Value(*a)
	val.Store(xs)
}

func (a *atomicByteSlice) Load() ([]byte, bool) {
	val := atomic.Value(*a)
	ans, ok := val.Load().([]byte)
	if !ok {
		return nil, false
	}
	return ans, true
}
