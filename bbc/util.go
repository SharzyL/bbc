package bbc

import (
	"math/bits"
)

func log2Floor(n uint64) int {
	// 0    -> -1
	// 1    -> 0
	// 2, 3 -> 1
	return 64 - 1 - bits.LeadingZeros64(n)
}

func power2Ceil(n uint64) uint64 {
	// 0    -> 0
	// 1    -> 1
	// 2    -> 2
	// 3, 4 -> 4
	return 1 << (log2Floor(n-1) + 1)
}