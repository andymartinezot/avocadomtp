// Package bufpool recycles large transfer buffers so long copies don't churn
// the garbage collector. Buffers come in power-of-two size classes from
// 64 KiB to 16 MiB.
package bufpool

import (
	"math/bits"
	"sync"
)

const (
	minShift = 16 // 64 KiB
	maxShift = 24 // 16 MiB
)

var pools [maxShift - minShift + 1]sync.Pool

func class(n int) int {
	if n <= 1<<minShift {
		return 0
	}
	return bits.Len(uint(n-1)) - minShift
}

// Get returns a buffer of length n. Sizes above 16 MiB are not pooled.
func Get(n int) []byte {
	c := class(n)
	if c >= len(pools) {
		return make([]byte, n)
	}
	if b, ok := pools[c].Get().(*[]byte); ok {
		return (*b)[:n]
	}
	return make([]byte, n, 1<<(c+minShift))
}

// Put returns a buffer obtained from Get. The caller must not use it afterwards.
func Put(b []byte) {
	c := class(cap(b))
	if c >= len(pools) || cap(b) != 1<<(c+minShift) {
		return // not one of ours
	}
	b = b[:cap(b)]
	pools[c].Put(&b)
}
