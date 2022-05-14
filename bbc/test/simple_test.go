package test

import (
	"testing"
)

func TestTest(t *testing.T) {
	x := make([]int, 8)
	p := x[12:]
	t.Logf("l = %d", len(p))
}
