package bbc

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/SharzyL/bbc/bbc/pb"
	"github.com/SharzyL/bbc/bbc/trie"
)

type minerRpcServer struct {
	pb.UnimplementedLedgerServer
	l *Miner
}

func (s *minerRpcServer) PeekChain(ctx context.Context, req *pb.PeekChainReq) (*pb.PeekChainAns, error) {
	l := s.l
	hashes := make([]*pb.HashVal, 0, 10)
	for j := len(l.mainChain) - 1; j > len(l.mainChain)-10 && j >= 0; j-- {
		hashes = append(hashes, &pb.HashVal{Bytes: Hash(l.mainChain[j].Header)})
	}
	return &pb.PeekChainAns{Hashes: hashes}, nil
}

func (s *minerRpcServer) AdverticeBlock(ctx context.Context, req *pb.AdverticeBlockReq) (*pb.AdverticeBlockAns, error) {
	// TODO: respond to this
	return &pb.AdverticeBlockAns{}, nil
}

func (s *minerRpcServer) GetFullBlock(ctx context.Context, req *pb.HashVal) (*pb.FullBlock, error) {
	l := s.l
	b := l.findBlockByHash(req.Bytes)
	if b == nil {
		return nil, fmt.Errorf("cannot find block of given hash")
	} else {
		return b, nil
	}
}

func (s *minerRpcServer) UploadTx(ctx context.Context, req *pb.Tx) (*pb.UploadTxAns, error) {
	l := s.l
	l.memPool = append(l.memPool, req)
	return &pb.UploadTxAns{}, nil
}

type Miner struct {
	SelfAddr     string // network addr of self
	PeerAddrList []string

	mainChain []*pb.FullBlock
	height    uint64

	hashToTx    *trie.Trie
	hashToBlock *trie.Trie

	memPool []*pb.Tx // transactions waiting to be packed into a block

	rpcServer         minerRpcServer
	clientConnections []pb.LedgerClient

	notifChan chan struct{}

	logger *zap.SugaredLogger
}

func NewMiner(selfAddr string, peerAddrList []string) *Miner {
	logger, _ := zap.NewDevelopment()
	sugarLogger := logger.Sugar()
	miner := Miner{
		SelfAddr:     selfAddr,
		PeerAddrList: peerAddrList,
		mainChain:    make([]*pb.FullBlock, 0, 1), // reserve space for genesis block
		height:       1,                           // only a genesis block
		hashToTx:     trie.NewTrie(),
		hashToBlock:  trie.NewTrie(),
		memPool:      make([]*pb.Tx, 0, 1000),
		rpcServer:    minerRpcServer{}, // init self pointer later

		clientConnections: make([]pb.LedgerClient, 0, len(peerAddrList)),

		notifChan: make(chan struct{}),

		logger: sugarLogger,
	}
	miner.rpcServer.l = &miner
	miner.mainChain = append(miner.mainChain, pb.GenesisBlock())
	return &miner
}

func (l *Miner) MainLoop() {
	l.logger.Infow("starting mainloop",
		zap.String("addr", l.SelfAddr),
		zap.Strings("peerAddr", l.PeerAddrList))

	go l.serveLoop()

	// create client connections
	for _, addr := range l.PeerAddrList {
		if addr == l.SelfAddr {
			continue
		}
		conn, err := grpc.Dial(addr)
		if err != nil {
			l.logger.Fatalw("failed to dial: %v", zap.Error(err))
		}
		l.clientConnections = append(l.clientConnections, pb.NewLedgerClient(conn))
	}

	for {
		// wait for new transactions fill the mempool
		<-l.notifChan

		// create new block
		newBlock := l.createBlock()
		l.hashToBlock.Insert(Hash(newBlock.Header), newBlock)

		// advertice block
		for _, client := range l.clientConnections {
			ctx := context.Background()
			_, err := client.AdverticeBlock(ctx, &pb.AdverticeBlockReq{
				Headers: []*pb.BlockHeader{newBlock.Header},
			})
			if err != nil {
				l.logger.Errorw("err on advertice block", zap.Error(err))
			}
		}

		// TODO: actively fetch neighbor block
	}
}

func (l *Miner) serveLoop() {
	lis, err := net.Listen("tcp", l.SelfAddr)
	if err != nil {
		l.logger.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterLedgerServer(s, &minerRpcServer{l: l})
	if err := s.Serve(lis); err != nil {
		l.logger.Fatalf("failed to serve %v", err)
	}
}

func (l *Miner) findTxByHash(hash []byte) *pb.Tx {
	v, ok := l.hashToTx.Search(hash).(*pb.Tx)
	if ok {
		return v
	} else {
		return nil
	}
}

func (l *Miner) findBlockByHash(hash []byte) *pb.FullBlock {
	v, ok := l.hashToBlock.Search(hash).(*pb.FullBlock)
	if ok {
		return v
	} else {
		return nil
	}
}

func (l *Miner) createBlock() *pb.FullBlock {
	txList := l.memPool

	n := uint64(len(txList))
	numTx := power2Ceil(n)
	for i := n; i < numTx; i++ {
		txList = append(txList, &pb.Tx{})
	}
	merkleTree := make([]*pb.TxMerkleNode, 0, 2*numTx-2)
	// generate hash of transaction
	for i := uint64(0); i < numTx; i++ {
		merkleTree[numTx-2+i].Hash.Bytes = Hash(txList[i])
	}
	// verify merkle nodes
	for i := numTx - 3; i >= 0; i-- {
		merkleTree[i].Hash.Bytes = Hash(merkleTree[2*i+2], merkleTree[2*i+3])
	}
	// generate merkle root
	var firstTx, secondTx *pb.Tx
	if len(txList) > 0 {
		firstTx = txList[0]
	}
	if len(txList) > 1 {
		secondTx = txList[1]
	}
	merkleRootHash := Hash(firstTx, secondTx)

	prevHash := Hash(l.mainChain[len(l.mainChain)-1].Header)
	timestamp := time.Now().Unix()
	height := uint64(len(l.mainChain))
	return &pb.FullBlock{
		Header: &pb.BlockHeader{
			PrevHash:    &pb.HashVal{Bytes: prevHash},
			MerkleRoot:  &pb.HashVal{Bytes: merkleRootHash},
			Timestamp:   timestamp,
			Height:      height,
			BlockNounce: nil, // TODO: mine this
		},
		TxList:     txList,
		MerkleTree: merkleTree,
	}
}

func (l *Miner) receiveBlock(b *pb.FullBlock, h uint64) error {
	// TODO: verify prev pointer

	// verify merkle tree
	d := log2Floor(uint64(len(b.TxList)))
	// verify size
	if d <= 0 {
		return fmt.Errorf("block should contain at least two transactions")
	}
	numTx := 1 << d
	if len(b.TxList) != numTx {
		return fmt.Errorf("num of transactions not a power of 2")
	}
	if len(b.MerkleTree) != 2*numTx-2 {
		return fmt.Errorf("len of merkle tree not consistent with num of tx")
	}
	// verify hash of transaction
	for i := 0; i < numTx; i++ {
		if !bytes.Equal(b.MerkleTree[numTx-2+i].Hash.Bytes, Hash(b.TxList[i])) {
			return fmt.Errorf("hash of tx %d is wrong", i)
		}
	}
	// verify merkle root
	if !bytes.Equal(b.Header.MerkleRoot.Bytes, Hash(b.TxList[0], b.TxList[1])) {
		return fmt.Errorf("merkle root not consistent")
	}
	// verify merkle nodes
	for i := 0; i < numTx-2; i++ {
		if !bytes.Equal(b.MerkleTree[i].Hash.Bytes, Hash(b.MerkleTree[2*i+2], b.MerkleTree[2*i+3])) {
			return fmt.Errorf("merkle node %d not consistent", i)
		}
	}

	// verify BlockNounce (crypto puzzle)
	// TODO:

	// verify transactions
	err := l.verifyCoinBase(b.TxList[0])
	if err != nil {
		return err
	}
	for _, t := range b.TxList[1:] {
		err := l.verifyTx(t)
		if err != nil {
			return err
		}
	}

	return nil
}

func (l *Miner) verifyCoinBase(t *pb.Tx) error {
	// TODO: verify coinbase
	return nil
}

func (l *Miner) verifyTx(t *pb.Tx) error {
	totalInput := uint64(0)
	for i, txin := range t.TxInList {
		if prevTx := l.findTxByHash(txin.PrevTx.Bytes); prevTx != nil {
			return fmt.Errorf("cannot find block")
		} else {
			if len(prevTx.TxOutList) < int(txin.PrevOutIdx)+1 {
				return fmt.Errorf("cannot find txout")
			}
			prevTxOut := prevTx.TxOutList[txin.PrevOutIdx]
			// TODO: check if txout is spent
			if !Verify(txin, prevTxOut.ReceiverPubKey.Bytes, txin.Sig.Bytes) {
				return fmt.Errorf("verify txin (%d) sig failed", i)
			}
			totalInput += prevTxOut.Value
		}
	}

	totalOutput := uint64(0)
	for _, txout := range t.TxOutList {
		totalOutput += txout.Value
	}

	if totalInput < totalOutput {
		return fmt.Errorf("tx input less than output")
	}

	return nil
}
