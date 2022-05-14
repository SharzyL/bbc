// Package trie
// all keys must be of the same length

package trie

import (
	"bytes"
	"sync"
)

type trieNode struct {
	data     interface{}
	children map[byte]*trieNode
	key      []byte
}

type Trie struct {
	root *trieNode
	mtx  *sync.RWMutex
}

func NewTrie() *Trie {
	return &Trie{
		root: &trieNode{
			data:     nil,
			children: make(map[byte]*trieNode),
			key:      nil,
		},
		mtx: &sync.RWMutex{},
	}
}

func (t *Trie) Search(val []byte) interface{} {
	t.mtx.RLock()
	defer t.mtx.RUnlock()

	n := t.root
	for _, b := range val {
		n = n.children[b]
		if n == nil {
			return nil
		}
		if n.key != nil {
			if bytes.Equal(n.key, val) {
				return n.data
			} else {
				return nil
			}
		}
	}
	return nil
}

func (t *Trie) Insert(val []byte, data interface{}) {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	n := t.root
	for i, b := range val {
		newNode := n.children[b]
		if newNode == nil {
			n.children[b] = &trieNode{
				data:     data,
				children: make(map[byte]*trieNode),
				key:      val,
			}
			return
		}
		n = newNode
		if n.key != nil {
			nKey := n.key
			if bytes.Equal(nKey, val) {
				n.data = data
				return
			}
			nData := n.data
			n.key = nil
			n.data = nil
			for j := i + 1; j < len(val); j++ {
				if nKey[j] == val[j] {
					n.children[val[j]] = &trieNode{
						data:     nil,
						children: make(map[byte]*trieNode),
						key:      nil,
					}
					n = n.children[val[j]]
				} else {
					n.children[val[j]] = &trieNode{
						data:     data,
						children: make(map[byte]*trieNode),
						key:      val,
					}
					n.children[nKey[j]] = &trieNode{
						data:     nData,
						children: make(map[byte]*trieNode),
						key:      nKey,
					}
					return
				}
			}
			panic("unreachable")
		}
	}
}
