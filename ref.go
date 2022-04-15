package phx

import "sync/atomic"

type atomicRef struct {
	ref *uint64
}

func newAtomicRef() *atomicRef {
	return &atomicRef{
		new(uint64),
	}
}

func (ic *atomicRef) nextRef() uint64 {
	return atomic.AddUint64(ic.ref, 1)
}
