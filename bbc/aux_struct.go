package bbc

import (
	"github.com/SharzyL/bbc/bbc/pb"
	"time"
)

func GenesisBlock() *pb.FullBlock {
	zeroHash := [HashLen]byte{}
	zeroNounce := [NounceLen]byte{}
	txList := make([]*pb.Tx, 0)
	merkleTree := make([]*pb.TxMerkleNode, 0)
	merkleHash := &pb.HashVal{Bytes: Hash(&pb.Tx{}, &pb.Tx{})}
	return &pb.FullBlock{
		Header: &pb.BlockHeader{
			PrevHash:    &pb.HashVal{Bytes: zeroHash[:]},
			MerkleRoot:  merkleHash,
			Timestamp:   0,
			Height:      0,
			BlockNounce: zeroNounce[:], // TODO: compute a correct nounce
		},
		TxList:     txList,
		MerkleTree: merkleTree,
	}
}

func CoinBaseTx(minerPubKey []byte, val uint64) *pb.Tx {
	txOut := &pb.TxOut{
		Value:          val,
		ReceiverPubKey: &pb.PubKey{Bytes: minerPubKey},
	}
	return &pb.Tx{
		Valid:     true,
		Timestamp: time.Now().UnixMilli(),
		TxInList:  []*pb.TxIn{},
		TxOutList: []*pb.TxOut{txOut},
	}
}

type fullBlockWithHash struct {
	Block *pb.FullBlock
	Hash  []byte
}

func makeFullBlockWithHash(b *pb.FullBlock) *fullBlockWithHash {
	hash := Hash(b.Header)
	return &fullBlockWithHash{
		Block: b,
		Hash:  hash,
	}
}

type txWithFee = struct {
	Tx  *pb.Tx
	Fee uint64
}

type txWithConsumer = struct {
	Tx        *pb.Tx
	Block     *fullBlockWithHash // might be nil, thus not packed to a block
	Consumers []*fullBlockWithHash
}

func makeTxWithConsumer(tx *pb.Tx, block *fullBlockWithHash) *txWithConsumer {
	return &txWithConsumer{
		Tx:        tx,
		Block:     block,
		Consumers: make([]*fullBlockWithHash, len(tx.TxOutList)),
	}
}
