package bbc

import (
	"crypto/ed25519"
	"crypto/sha256"
	"hash"
	"math/rand"

	"go.uber.org/atomic"

	"github.com/SharzyL/bbc/bbc/pb"
)

const HashLen = sha256.Size               // 64
const PubKeyLen = ed25519.PublicKeySize   // 32
const PrivKeyLen = ed25519.PrivateKeySize // 64
const SigLen = ed25519.SignatureSize      // 64
const NounceLen = 32

const MinerReward = uint64(10000)

type HashState = hash.Hash

func NewHashState() HashState {
	return sha256.New()
}

type Hashable interface {
	ToBytes() []byte
}

type BytesWrapper struct {
	B []byte
}

func (b BytesWrapper) ToBytes() []byte {
	return b.B
}

func Hash(vs ...Hashable) []byte {
	h := NewHashState()
	for _, v := range vs {
		_, _ = h.Write(v.ToBytes())
	}
	return h.Sum(nil)
}

type Signable interface {
	ToSigMsgBytes() []byte
}

func Sign(s Signable, sk []byte) []byte {
	return ed25519.Sign(sk, s.ToSigMsgBytes())
}

func Verify(s Signable, pk []byte, sig []byte) bool {
	return ed25519.Verify(pk, s.ToSigMsgBytes(), sig)
}

func GenKey() ([]byte, []byte) {
	pubKey, privKey, _ := ed25519.GenerateKey(nil)
	return pubKey, privKey
}

func Mine(h *pb.BlockHeader, interrupter *atomic.Bool, prefixLen int) (success bool) {
	headerBytes := h.ToBytes()
	hasher := NewHashState()
	nounceStartIdx := len(headerBytes) - NounceLen

	for {
		for i := 0; i < 100000; i++ {
			hasher.Reset()
			rand.Read(headerBytes[nounceStartIdx:])
			_, _ = hasher.Write(headerBytes)
			hashVal := hasher.Sum(nil)

			if hasLeadingZeros(hashVal, prefixLen) {
				h.BlockNounce = headerBytes[nounceStartIdx:]
				return true
			}
		}
		if interrupter.Load() {
			interrupter.Store(false)
			return false
		}
	}
}
