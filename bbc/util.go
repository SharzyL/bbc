package bbc

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"math/bits"
)

func getLogger() *zap.SugaredLogger {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger, _ := config.Build()
	return logger.Sugar()
}

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
	return fmt.Sprintf("%x", b)[:16]
}
