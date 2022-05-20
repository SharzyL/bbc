package bbc

import (
	"fmt"
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

func minUInt64(a, b uint64) uint64 {
	if a >= b {
		return b
	} else {
		return a
	}
}

func maxUInt64(a, b uint64) uint64 {
	if a <= b {
		return b
	} else {
		return a
	}
}

func minInt64(a, b int64) int64 {
	if a >= b {
		return b
	} else {
		return a
	}
}

func maxInt64(a, b int64) int64 {
	if a <= b {
		return b
	} else {
		return a
	}
}

func minInt(a, b int) int {
	if a >= b {
		return b
	} else {
		return a
	}
}

func maxInt(a, b int) int {
	if a <= b {
		return b
	} else {
		return a
	}
}

func b2str(b []byte) string {
	if b != nil {
		return fmt.Sprintf("%x", b)[:16]
	} else {
		return "<nil>"
	}
}

func hasLeadingZeros(b []byte, len int) bool {
	byteNum := len / 8
	remainingBits := len % 8
	for i := 0; i < byteNum; i++ {
		if b[i] != 0 {
			return false
		}
	}
	if remainingBits > 0 && bits.LeadingZeros8(b[byteNum]) < remainingBits {
		return false
	}
	return true
}
