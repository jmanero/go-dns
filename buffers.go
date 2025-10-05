package dns

import (
	"sync"
)

var buffers = sync.Pool{
	New: func() any { return make([]byte, 4096) },
}

// GetBuffer gets a byte buffer from an underlying pool
func GetBuffer(capacity, length int) []byte {
	buf := buffers.Get().([]byte)

	// Ensure that the buffer has the requested capacity and length
	return GrowBuffer(buf, capacity, length)
}

// FreeBuffer returns a byte buffer to a pool for reuse
func FreeBuffer(buf []byte) {
	buffers.Put(buf)
}

// GrowBuffer expands a buffer slice to the requested capacity and length
func GrowBuffer(buf []byte, capacity, length int) []byte {
	if length > capacity {
		panic("length is greater than capacity")
	}

	// Grow the slice to the requested capacity. The implementation in `slices.Grow`
	// claims that this compiles to a single allocation
	if n := capacity - cap(buf); n > 0 {
		buf = append(buf[:cap(buf)], make([]byte, n)...)
	}

	// Return a subslice at the requested length
	return buf[:length]
}
