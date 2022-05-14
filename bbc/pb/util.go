package pb

import (
	"bytes"
	"encoding/binary"
	"github.com/SharzyL/bbc/bbc"
)

func GenesisBlock() *FullBlock {
	zeroHash := [bbc.HashLen]byte{}
	zeroNounce := [bbc.NounceLen]byte{}
	txList := make([]*Tx, 0)
	merkleTree := make([]*TxMerkleNode, 0)
	merkleHash := &HashVal{Bytes: bbc.Hash(&Tx{}, &Tx{})}
	return &FullBlock{
		Header: &BlockHeader{
			PrevHash:    &HashVal{Bytes: zeroHash[:]},
			MerkleRoot:  merkleHash,
			Timestamp:   0,
			Height:      0,
			BlockNounce: zeroNounce[:], // TODO: compute a correct nounce
		},
		TxList:     txList,
		MerkleTree: merkleTree,
	}
}

func CoinBaseTx(minerPubKey []byte, val uint64) *Tx {
	txOut := &TxOut{
		Value:          val,
		ReceiverPubKey: &PubKey{Bytes: minerPubKey},
	}
	return &Tx{
		Valid:     true,
		TxInList:  []*TxIn{},
		TxOutList: []*TxOut{txOut},
	}
}

// NewHashVal is a utility function to create pb.HashVal quickly
func NewHashVal(bytes []byte) *HashVal {
	return &HashVal{Bytes: bytes}
}

func (x *TxIn) ToSigMsgBytes() []byte {
	buf := bytes.Buffer{}
	buf.Write(x.PrevTx.Bytes)
	int32Buf := [4]byte{}
	binary.BigEndian.PutUint32(int32Buf[:], x.PrevOutIdx)
	return buf.Bytes()
}

func (x *Tx) ToBytes() []byte {
	buf := bytes.Buffer{}
	intBuf := [8]byte{}
	if !x.Valid {
		return []byte{}
	} else {
		binary.BigEndian.PutUint32(intBuf[:4], uint32(len(x.TxInList)))
		buf.Write(intBuf[:4])
		for _, tx := range x.TxInList {
			buf.Write(tx.PrevTx.Bytes)
			binary.BigEndian.PutUint32(intBuf[:4], tx.PrevOutIdx)
			buf.Write(intBuf[:4])
			buf.Write(tx.Sig.Bytes)
		}
		binary.BigEndian.PutUint32(intBuf[:4], uint32(len(x.TxOutList)))
		buf.Write(intBuf[:4])
		for _, tx := range x.TxOutList {
			binary.BigEndian.PutUint64(intBuf[:8], tx.Value)
			buf.Write(intBuf[:8])
			buf.Write(tx.ReceiverPubKey.Bytes)
		}
	}
	return buf.Bytes()
}

func (x *BlockHeader) ToBytes() []byte {
	buf := bytes.Buffer{}
	buf.Write(x.PrevHash.Bytes)
	intBuf := [8]byte{}
	binary.BigEndian.PutUint64(intBuf[:], uint64(x.Timestamp))
	buf.Write(intBuf[:])
	binary.BigEndian.PutUint64(intBuf[:], uint64(x.Height))
	buf.Write(intBuf[:])
	buf.Write(x.BlockNounce[:])
	return buf.Bytes()
}

func (x *TxMerkleNode) ToBytes() []byte {
	return x.Hash.Bytes
}
