package trie

import "testing"

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
