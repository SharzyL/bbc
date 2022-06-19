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
		n = n.children[b] // n.children must not be nil, because n.key == nil or n is root
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
		newNode := n.children[b] // again, n.children must not be nil
		if newNode == nil {      // take a new path from n
			n.children[b] = &trieNode{
				data:     data,
				children: nil,
				key:      val,
			}
			return
		}
		n = newNode
		if n.key != nil { // if reaching a leaf node (notice that all key is of same length)
			nKey := n.key

			// the key already exists
			if bytes.Equal(nKey, val) {
				n.data = data
				return
			}
			nData := n.data
			n.key = nil
			n.data = nil
			n.children = make(map[byte]*trieNode)

			// try growth the path, until a fork appears
			for j := i + 1; j < len(val); j++ {
				if nKey[j] == val[j] {
					n.children[val[j]] = &trieNode{
						data:     nil,
						children: make(map[byte]*trieNode),
						key:      nil,
					}
					n = n.children[val[j]]
				} else {
					// reach the fork point
					n.children[val[j]] = &trieNode{
						data:     data,
						children: nil,
						key:      val,
					}
					n.children[nKey[j]] = &trieNode{
						data:     nData,
						children: nil,
						key:      nKey,
					}
					return
				}
			}
			panic("unreachable")
		}
	}
}

func (t *Trie) Delete(val []byte) {
	t.mtx.Lock()
	defer t.mtx.Unlock()

	n := t.root
	for _, b := range val {
		c := n.children[b] // n.children must not be nil, because n.key == nil or n is root
		if c == nil {
			return
		}
		if c.key != nil {
			if bytes.Equal(c.key, val) {
				delete(n.children, b)
				return
			} else {
				return
			}
		}
		n = c
	}
}
