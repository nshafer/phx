package phx

import "sync/atomic"

type Ref uint64

type atomicRef struct {
	ref *Ref
}

func newAtomicRef() *atomicRef {
	return &atomicRef{
		ref: new(Ref),
	}
}

func (ic *atomicRef) nextRef() Ref {
	ref := (*uint64)(ic.ref)
	val := atomic.AddUint64(ref, 1)
	return Ref(val)
}
