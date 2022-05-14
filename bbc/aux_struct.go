package bbc

import "github.com/SharzyL/bbc/bbc/pb"

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

type txWithConsumer = struct {
	Tx        *pb.Tx
	Block     *fullBlockWithHash
	Consumers []*fullBlockWithHash
}

func makeTxWithConsumer(tx *pb.Tx, block *fullBlockWithHash) *txWithConsumer {
	return &txWithConsumer{
		Tx:        tx,
		Block:     block,
		Consumers: make([]*fullBlockWithHash, len(tx.TxOutList)),
	}
}
