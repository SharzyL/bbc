package bbc

import (
	"crypto/ed25519"
	"crypto/sha512"
	"hash"
)

const HashLen = sha512.Size               // 64
const PubKeyLen = ed25519.PublicKeySize   // 32
const PrivKeyLen = ed25519.PrivateKeySize // 64
const SigLen = ed25519.SignatureSize
const NounceLen = 64

const MinerReward = uint64(100)

type HashState = hash.Hash

func NewHashState() HashState {
	return sha512.New()
}

type Hashable interface {
	ToBytes() []byte
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
