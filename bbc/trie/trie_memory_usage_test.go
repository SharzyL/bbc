// the following code is modified from the stackoverflow answer by Rick-777
// https://stackoverflow.com/a/15314845

package trie

import (
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"unsafe"
)

func TestTrieMemory(t *testing.T) {
	Alloc := func(hs []*Trie) uint64 {
		var stats runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&stats)
		return stats.Alloc - uint64(unsafe.Sizeof(hs[0]))*uint64(cap(hs))
	}

	var hs []*Trie
	n := 1000

	k := 1
	for p := 1; p < 16; p++ {
		before := Alloc(hs)

		for i := 0; i < n; i++ {
			h := NewTrie()
			for j := 0; j < k; j++ {
				key := [64]byte{}
				rand.Read(key[:])
				h.Insert(key[:], 0)
			}
			hs = append(hs, h)
		}

		after := Alloc(hs)
		fullPerMap := float64(after-before) / float64(n)
		fmt.Printf("%d & %.1f & %.1f\n", k, fullPerMap, (fullPerMap)/float64(k))
		k *= 2
	}
}

func TestMapMemory(t *testing.T) {
	Alloc := func(hs []map[string]int) uint64 {
		var stats runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&stats)
		return stats.Alloc - uint64(unsafe.Sizeof(hs[0]))*uint64(cap(hs))
	}

	var hs []map[string]int
	n := 10000

	k := 1
	for p := 1; p < 16; p++ {
		key := [64]byte{}
		before := Alloc(hs)

		for i := 0; i < n; i++ {
			h := map[string]int{}
			for j := 0; j < k; j++ {
				rand.Read(key[:])
				h[string(key[:])] = 0
			}
			hs = append(hs, h)
		}

		after := Alloc(hs)
		fullPerMap := float64(after-before) / float64(n)
		fmt.Printf("%d & %.1f & %.1f\n", k, fullPerMap, (fullPerMap)/float64(k))
		k *= 2
	}
}
