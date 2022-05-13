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
	pb.UnimplementedMinerServer
	l *Miner
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

func (s *minerRpcServer) PeekChain(ctx context.Context, req *pb.PeekChainReq) (ans *pb.PeekChainAns, err error) {
	l := s.l
	headers := make([]*pb.BlockHeader, 0, 10)

	topHash := req.TopHash.Bytes
	for i := 0; i < 10; i++ {
		fullBlock := l.findBlockByHash(topHash)
		if fullBlock == nil {
			err = fmt.Errorf("cannot find block with hash %x when peekChain with depth %d", req.TopHash.Bytes, i)
			return
		}
		headers = append(headers, fullBlock.Header)
		topHash = fullBlock.Header.PrevHash.Bytes
	}

	return &pb.PeekChainAns{Headers: headers}, nil
}

func (s *minerRpcServer) AdverticeBlock(ctx context.Context, req *pb.AdverticeBlockReq) (ans *pb.AdverticeBlockAns, err error) {
	l := s.l
	header := req.Header
	if header.Height <= int64(len(l.mainChain)) { // TODO: prevent selfish mining attack
		return
	}
	go l.syncBlock(req.Addr, header)
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
	// TODO: send notif when Tx is full
	l := s.l
	l.memPool = append(l.memPool, req)
	return &pb.UploadTxAns{}, nil
}

type Miner struct {
	SelfAddr     string // network addr of self
	PeerAddrList []string

	mainChain []*fullBlockWithHash

	hashToTx    *trie.Trie
	hashToBlock *trie.Trie

	memPool []*pb.Tx // transactions waiting to be packed into a block

	rpcServer         minerRpcServer
	clientConnections []pb.MinerClient

	notifChan chan struct{}

	logger *zap.SugaredLogger
}

func NewMiner(selfAddr string, peerAddrList []string) *Miner {
	logger, _ := zap.NewDevelopment()
	sugarLogger := logger.Sugar()
	miner := Miner{
		SelfAddr:     selfAddr,
		PeerAddrList: peerAddrList,
		mainChain:    make([]*fullBlockWithHash, 0, 1), // reserve space for genesis block
		hashToTx:     trie.NewTrie(),
		hashToBlock:  trie.NewTrie(),
		memPool:      make([]*pb.Tx, 0, 1000),
		rpcServer:    minerRpcServer{}, // init self pointer later

		clientConnections: make([]pb.MinerClient, 0, len(peerAddrList)),

		notifChan: make(chan struct{}),

		logger: sugarLogger,
	}
	miner.rpcServer.l = &miner
	miner.mainChain = append(miner.mainChain, makeFullBlockWithHash(pb.GenesisBlock()))
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
			l.logger.Fatalw("failed to dial", zap.Error(err))
		}
		l.clientConnections = append(l.clientConnections, pb.NewMinerClient(conn))
	}

	for {
		// wait for new transactions fill the mempool
		<-l.notifChan

		// create new block
		newBlock := l.createBlock()
		l.hashToBlock.Insert(Hash(newBlock.Header), newBlock)
		l.mainChain = append(l.mainChain, makeFullBlockWithHash(newBlock))

		// advertice block
		for _, client := range l.clientConnections {
			ctx := context.Background()
			_, err := client.AdverticeBlock(ctx, &pb.AdverticeBlockReq{
				Header: newBlock.Header,
				Addr:   l.SelfAddr,
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
		l.logger.Fatalw("failed to listen", zap.Error(err))
	}
	s := grpc.NewServer()
	pb.RegisterMinerServer(s, &minerRpcServer{l: l})
	if err := s.Serve(lis); err != nil {
		l.logger.Fatalw("failed to serve %v", zap.Error(err))
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

func (l *Miner) syncBlock(addr string, topHeader *pb.BlockHeader) {
	// prerequisite: topHeader.length >= mainCHain.length, topHeader.top != mainChain.top

	conn, err := grpc.Dial(addr)
	if err != nil {
		l.logger.Errorw("fail to dial when syncing block", zap.Error(err))
		return
	}
	client := pb.NewMinerClient(conn)
	ctx := context.Background()

	prevHash := topHeader.PrevHash.Bytes
	hashesToSync := make([][]byte, 0, 1)
	hashesToSync = append(hashesToSync, Hash(topHeader))

	if topHeader.Height > 1 && !bytes.Equal(prevHash, l.mainChain[topHeader.Height-1].Hash) {
		hashesToSync = append(hashesToSync, prevHash)
	} else {
		// requesting older blocks to find out all blocks to sync
	findMaxSynced:
		for {
			l.logger.Infow("start send peekChainReq", zap.String("addr", addr), zap.Binary("hash", prevHash))
			ans, err := client.PeekChain(ctx, &pb.PeekChainReq{TopHash: pb.NewHashVal(prevHash)})
			l.logger.Infow("peekChainReq responds", zap.String("addr", addr),
				zap.Int("numHeaders", len(ans.Headers)))
			if err != nil {
				l.logger.Errorw("fail to peek chain when syncing block, quit syncing", zap.Error(err))
				return
			}
			for _, hdr := range ans.Headers {
				curHash := Hash(hdr)
				if !bytes.Equal(curHash, prevHash) {
					l.logger.Errorw("block hash inconsistent with prevHash",
						zap.Int64("height", hdr.Height),
						zap.Binary("curHash", curHash),
						zap.Binary("prevHash", prevHash))
					return
				}
				prevHash = hdr.PrevHash.Bytes
				if hdr.Height > 1 || !bytes.Equal(prevHash, l.mainChain[hdr.Height-1].Hash) {
					hashesToSync = append(hashesToSync, prevHash)
				} else {
					break findMaxSynced
				}
			}
		}
	}

	newBlocks := make([]*fullBlockWithHash, 0, len(hashesToSync))
	for i := len(hashesToSync) - 1; i >= 0; i++ {
		hash := hashesToSync[i]
		fullBlock, err := client.GetFullBlock(ctx, pb.NewHashVal(hash))
		if err != nil {
			l.logger.Errorw("fail to get full block",
				zap.String("addr", addr),
				zap.Binary("hash", hash))
		}
		newBlocks = append(newBlocks, makeFullBlockWithHash(fullBlock))
	}
	minUnsynced := newBlocks[0].Block.Header.Height
	l.mainChain = append(l.mainChain[:minUnsynced], newBlocks...)
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

	prevHash := l.mainChain[len(l.mainChain)-1].Hash
	timestamp := time.Now().Unix()
	height := int64(len(l.mainChain))
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

func (l *Miner) verifyBlock(b *pb.FullBlock, h uint64) error {
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
