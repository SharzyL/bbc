package bbc

import (
	"crypto/ed25519"
)

type User struct {
	privKey ed25519.PrivateKey
	pubKey  ed25519.PublicKey
}

// TODO: add actions for user
