package trie

import (
	"crypto/ed25519"
	"math/rand"
	"testing"
)

func TestTrie_Simple(t *testing.T) {
	trie := NewTrie()
	trie.Insert([]byte{1, 2, 3}, 1)
	trie.Insert([]byte{1, 2, 4}, 2)
	trie.Insert([]byte{3, 2, 4}, 3)
	trie.Insert([]byte{3, 2, 4}, 4)
	trie.Insert([]byte{1, 5, 9}, 5)

	if val, ok := trie.Search([]byte{1, 2, 3}).(int); !ok || val != 1 {
		t.Errorf("find 123 failed: %x", val)
	}
	if val, ok := trie.Search([]byte{1, 2, 4}).(int); !ok || val != 2 {
		t.Errorf("find 124 failed: %x", val)
	}
	if val, ok := trie.Search([]byte{3, 2, 4}).(int); !ok || val != 4 {
		t.Errorf("find 324 failed: %x", val)
	}
	if val, ok := trie.Search([]byte{1, 5, 9}).(int); !ok || val != 5 {
		t.Errorf("find 159 failed: %x", val)
	}
}

func TestTrie_LargeRandom(t *testing.T) {
	trie := NewTrie()
	n := 100000
	keyList := make([][]byte, 0, 10000)

	// insert random keys
	for i := 0; i < n; i++ {
		var key [64]byte
		rand.Read(key[:])
		trie.Insert(key[:], i)
		keyList = append(keyList, key[:])
	}

	// check Search these keys
	for i, key := range keyList {
		valUnconverted := trie.Search(key)
		if valUnconverted == nil {
			t.Errorf("failed to find the %dth key %x", i, key)
		}
		val := valUnconverted.(int)
		if i != val {
			t.Errorf("val of key %x incorrect, expected %d, actual %d", key, i, val)
		}
	}

	// check search random keys
	for i := 0; i < n; i++ {
		var key [64]byte
		rand.Read(key[:])
		if trie.Search(key[:]) != nil {
			t.Errorf("it should be found with key %x", key)
		}
	}

	// delete half of the keys
	for i := 0; i < n; i += 2 {
		key := keyList[i]
		trie.Delete(key)
	}

	// check search keys
	for i, key := range keyList {
		valUnconverted := trie.Search(key)
		if i%2 == 0 { // it should be deleted
			if valUnconverted != nil {
				t.Errorf("it should have been deleted i=%d, key=%x", i, key)
			}
		} else {
			if valUnconverted == nil {
				t.Errorf("failed to find the %dth key %x", i, key)
			}
			val := valUnconverted.(int)
			if i != val {
				t.Errorf("val of key %x incorrect, expected %d, actual %d", key, i, val)
			}
		}
	}
}

func BenchmarkVerify(b *testing.B) {
	pubKey, privKey, _ := ed25519.GenerateKey(nil)
	msg := make([]byte, 1024)
	_, _ = rand.Read(msg)
	sig := ed25519.Sign(privKey, msg)
	for i := 0; i < b.N; i++ {
		ed25519.Verify(pubKey, msg, sig)
	}
}

func BenchmarkMapTrie(b *testing.B) {
	n := 1000000
	for i := 0; i < b.N; i++ {
		t := NewTrie()
		key := make([]byte, 64)
		for j := 0; j < n; j++ {
			rand.Read(key)
			t.Insert(key, 0)
		}
		for j := 0; j < n; j++ {
			rand.Read(key)
			t.Search(key)
		}
	}
}

func BenchmarkMapNative(b *testing.B) {
	n := 1000000
	for i := 0; i < b.N; i++ {
		t := make(map[string]int)
		key := make([]byte, 64)
		for j := 0; j < n; j++ {
			rand.Read(key)
			t[string(key)] = 0
		}
		for j := 0; j < n; j++ {
			rand.Read(key)
			_ = t[string(key)]
		}
	}
}

func BenchmarkByteMap(b *testing.B) {
	t := make(map[byte]int)
	for i := 0; i < b.N; i++ {
		t[byte(i)] = 0
	}
}
