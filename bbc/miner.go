package bbc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/SharzyL/bbc/bbc/pb"
	"github.com/SharzyL/bbc/bbc/trie"
)

type minerRpcHandler struct {
	pb.UnimplementedMinerServer
	l *Miner
}

const memPoolLimit = 255

func (s *minerRpcHandler) PeekChain(ctx context.Context, req *pb.PeekChainReq) (ans *pb.PeekChainAns, err error) {
	l := s.l
	headers := make([]*pb.BlockHeader, 0, 10)

	l.chainMtx.RLock()
	defer l.chainMtx.RUnlock()

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

func (s *minerRpcHandler) AdverticeBlock(ctx context.Context, req *pb.AdverticeBlockReq) (ans *pb.AdverticeBlockAns, err error) {
	l := s.l
	header := req.Header
	l.chainMtx.RLock()
	mainChainLen := int64(len(l.mainChain))
	l.chainMtx.RUnlock()

	if header.Height >= mainChainLen { // TODO: prevent selfish mining attack
		go l.syncBlock(req.Addr, header)
	}
	return &pb.AdverticeBlockAns{}, nil
}

func (s *minerRpcHandler) GetFullBlock(ctx context.Context, req *pb.HashVal) (*pb.FullBlock, error) {
	l := s.l
	b := l.findBlockByHash(req.Bytes)
	if b == nil {
		return nil, fmt.Errorf("cannot find block of given hash")
	} else {
		return b, nil
	}
}

func (s *minerRpcHandler) UploadTx(ctx context.Context, tx *pb.Tx) (*pb.UploadTxAns, error) {
	// TODO: send notif when Tx is full
	l := s.l
	if !tx.Valid {
		return nil, fmt.Errorf("why send me an invalid tx")
	}

	l.memPoolMtx.Lock()
	defer l.memPoolMtx.Unlock()

	txFee, err := l.verifyTx(tx)
	if err != nil {
		return nil, err
	}
	l.memPool = append(l.memPool, tx)
	l.memPoolTotalFee += txFee
	return &pb.UploadTxAns{}, nil
}

// Miner should be not be initialized manually
type Miner struct {
	SelfAddr     string // network addr of self
	PeerAddrList []string

	minerPubKey  []byte
	minerPrivKey []byte

	mainChain []*fullBlockWithHash

	hashToTx    *trie.Trie // stores *txWithConsumer
	hashToBlock *trie.Trie // stores *pb.FullBlock

	memPool         []*pb.Tx // transactions waiting to be packed into a block
	memPoolTotalFee uint64

	rpcHandler  minerRpcHandler
	peerClients []pb.MinerClient

	notifChan chan struct{}

	memPoolMtx *sync.RWMutex
	chainMtx   *sync.RWMutex

	logger *zap.SugaredLogger
}

func NewMiner(selfAddr string, peerAddrList []string) *Miner {
	logger, _ := zap.NewDevelopment()
	sugarLogger := logger.Sugar()
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	memPoolMtx := &sync.RWMutex{}
	chainMtx := &sync.RWMutex{}
	if err != nil {
		sugarLogger.Panicw("cannot gen key", zap.Error(err))
	}
	miner := Miner{
		SelfAddr:     selfAddr,
		PeerAddrList: peerAddrList,

		minerPubKey:  pubKey,
		minerPrivKey: privKey,

		mainChain: make([]*fullBlockWithHash, 0, 1), // reserve space for genesis block

		hashToTx:    trie.NewTrie(),
		hashToBlock: trie.NewTrie(),

		memPool:         make([]*pb.Tx, 0, 1000),
		memPoolTotalFee: 0,

		rpcHandler:  minerRpcHandler{}, // init self pointer later
		peerClients: make([]pb.MinerClient, 0, len(peerAddrList)),

		notifChan: make(chan struct{}),

		memPoolMtx: memPoolMtx,
		chainMtx:   chainMtx,

		logger: sugarLogger,
	}
	miner.rpcHandler.l = &miner
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
		l.peerClients = append(l.peerClients, pb.NewMinerClient(conn))
	}

	for {
		// wait for new transactions fill the mempool
		<-l.notifChan

		// create new block
		newBlock := l.createBlock()
		if newBlock == nil {
			continue
		}

		// advertice block
		for _, client := range l.peerClients {
			ctx := context.Background()
			_, err := client.AdverticeBlock(ctx, &pb.AdverticeBlockReq{
				Header: newBlock.Header,
				Addr:   l.SelfAddr,
			})
			if err != nil {
				l.logger.Errorw("err on advertice block", zap.Error(err))
			}
		}
	}
}

func (l *Miner) serveLoop() {
	lis, err := net.Listen("tcp", l.SelfAddr)
	if err != nil {
		l.logger.Fatalw("failed to listen", zap.Error(err))
	}
	s := grpc.NewServer()
	defer s.GracefulStop()
	pb.RegisterMinerServer(s, &minerRpcHandler{l: l})
	if err := s.Serve(lis); err != nil {
		l.logger.Fatalw("failed to serve %v", zap.Error(err))
	}
}

func (l *Miner) findTxByHash(hash []byte) *txWithConsumer {
	v, ok := l.hashToTx.Search(hash).(*txWithConsumer)
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
	hashesToSync := make([][]byte, 0, 1) // a list of unsynced block hashes from newest to oldest
	hashesToSync = append(hashesToSync, Hash(topHeader))

	l.chainMtx.RLock()
	originalChain := l.mainChain
	l.chainMtx.RUnlock()

	// requesting headers of blocks to sync
	if topHeader.Height > 1 && !bytes.Equal(prevHash, l.mainChain[topHeader.Height-1].Hash) {
		hashesToSync = append(hashesToSync, prevHash)
	} else {
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
				} else if hdr.Height == 1 || bytes.Equal(prevHash, l.mainChain[0].Hash) {
					l.logger.Errorw("hash of genesis hash not consistent")
					return
				} else {
					break findMaxSynced
				}
			}
		}
	}

	// fetching fullblocks
	newBlocks := make([]*fullBlockWithHash, 0, len(hashesToSync))
	for i := len(hashesToSync) - 1; i >= 0; i-- {
		hash := hashesToSync[i]
		fullBlock, err := client.GetFullBlock(ctx, pb.NewHashVal(hash))
		if err != nil {
			l.logger.Errorw("fail to get full block",
				zap.String("addr", addr),
				zap.Binary("hash", hash))
		}
		newBlocks = append(newBlocks, makeFullBlockWithHash(fullBlock))
	}

	// verify and update new chain, if failed, recover to the original chain and restore TxInfo
	success := false

	//----------------------
	// chain locked
	//----------------------
	l.chainMtx.Lock()
	defer l.chainMtx.Unlock()

	minUnsynced := newBlocks[0].Block.Header.Height
	defer func() {
		if !success {
			// recovery from failed append chain
			l.mainChain = originalChain
			for _, b := range originalChain[minUnsynced:] {
				l.updateTxInfo(b)
			}
		}
	}()
	for _, b := range newBlocks {
		err := l.verifyBlock(b.Block)
		if err != nil {
			l.logger.Errorw("failed to verify synced block", zap.Error(err))
			return
		}
		l.mainChain = append(l.mainChain[:minUnsynced], b)
		l.updateTxInfo(b)
	}
	success = true
	//----------------------
	// chain unlocked
	//----------------------
}

func (l *Miner) updateTxInfo(fullBlock *fullBlockWithHash) {
	// insert all tx to hashToTx
	// update consumers of previous tx
	for _, tx := range fullBlock.Block.TxList {
		if tx.Valid {
			l.hashToTx.Insert(Hash(tx), makeTxWithConsumer(tx, fullBlock))
			for _, txIn := range tx.TxInList {
				prevTx := l.findTxByHash(txIn.PrevTx.Bytes)
				if txIn.PrevOutIdx >= uint32(len(prevTx.Tx.TxOutList)) {
					l.logger.Errorw("PrevOutIdx too large")
					return
				}
				prevTx.Consumers[txIn.PrevOutIdx] = fullBlock
			}
		}
	}
}

func (l *Miner) createBlock() *pb.FullBlock {
	success := false

	//-----------------------
	// mempool locked
	//-----------------------
	// clear current memPool
	l.memPoolMtx.Lock()
	memPool := l.memPool
	memPoolTotalFee := l.memPoolTotalFee
	if len(memPool) <= 1 {
		l.logger.Errorw("do not create an empty block")
		return nil
	}
	l.memPool = []*pb.Tx{}
	l.memPoolTotalFee = 0

	// recover all memPool if creating new block failed
	defer func() {
		if !success {
			l.memPoolMtx.Lock()
			defer l.memPoolMtx.Unlock()
			l.memPool = append(l.memPool, memPool...)
		}
	}()
	l.memPoolMtx.Unlock()
	//-----------------------
	// mempool unlocked
	//-----------------------

	// compute coinBase and collect txList
	var txList []*pb.Tx
	txList = append(txList, pb.CoinBaseTx(l.minerPubKey, memPoolTotalFee+MinerReward))
	txList = append(txList, memPool...)
	n := uint64(len(txList))
	numTx := power2Ceil(n)
	for i := n; i < numTx; i++ {
		txList = append(txList, &pb.Tx{})
	}

	// compute merkleTree
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

	// read chain mutex
	l.chainMtx.RLock()
	prevBlock := l.mainChain[len(l.mainChain)-1]
	l.chainMtx.RUnlock()

	timestamp := time.Now().Unix()
	b := &pb.FullBlock{
		Header: &pb.BlockHeader{
			PrevHash:    &pb.HashVal{Bytes: prevBlock.Hash},
			MerkleRoot:  &pb.HashVal{Bytes: merkleRootHash},
			Timestamp:   timestamp,
			Height:      prevBlock.Block.Header.Height + 1,
			BlockNounce: nil,
		},
		TxList:     txList,
		MerkleTree: merkleTree,
	}
	l.mine(b) // it takes a long time!

	//-----------------------
	// chain locked
	//-----------------------
	l.chainMtx.Lock()
	defer l.chainMtx.Unlock()

	// add new block to mainChain if it is fresh
	if bytes.Equal(l.mainChain[len(l.mainChain)-1].Hash, prevBlock.Hash) {
		l.mainChain = append(l.mainChain, makeFullBlockWithHash(b))
		l.hashToBlock.Insert(Hash(b.Header), b)
		success = true
		return b
	} else {
		return nil
	}

	//-----------------------
	// chain unlocked
	//-----------------------
}

func (l *Miner) mine(b *pb.FullBlock) {
	nounce := make([]byte, NounceLen) // TODO: mine this
	time.Sleep(10 * time.Second)
	l.logger.Infow("mine out a block", zap.Int64("height", b.Header.Height))
	b.Header.BlockNounce = nounce
}

func (l *Miner) verifyBlock(b *pb.FullBlock) error {
	// verify size
	if len(b.TxList) < 2 {
		return fmt.Errorf("block should contain at least two transactions")
	}
	d := log2Floor(uint64(len(b.TxList)))
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
	coinBaseVal, err := l.verifyCoinBase(b.TxList[0])
	if err != nil {
		return err
	}

	totalFee := uint64(0)
	for _, t := range b.TxList[1:] {
		if t.Valid {
			txFee, err := l.verifyTx(t)
			if err != nil {
				return err
			}
			totalFee += txFee
		}
	}
	if coinBaseVal != totalFee+MinerReward {
		return fmt.Errorf("incorrect coinbase val: %d + %d != %d", coinBaseVal, totalFee, MinerReward)
	}

	return nil
}

func (l *Miner) verifyCoinBase(t *pb.Tx) (coinBaseVal uint64, err error) {
	if !t.Valid {
		return 0, fmt.Errorf("invalid coinbase tx")
	} else if len(t.TxInList) > 0 {
		return 0, fmt.Errorf("coinbase input nonempty")
	} else if len(t.TxOutList) != 1 {
		return 0, fmt.Errorf("coinbase output len != 1")
	} else {
		return t.TxOutList[0].Value, nil
	}
}

func (l *Miner) isTxOutSpent(t *txWithConsumer, txOutIdx uint32) error {
	if txOutIdx >= uint32(len(t.Consumers)) {
		return fmt.Errorf("txOutIdx too large (%d >= %d)", txOutIdx, len(t.Consumers))
	} else {
		consumer := t.Consumers[txOutIdx]
		if consumer == nil {
			return nil
		} else if consumer.Block.Header.Height >= int64(len(l.mainChain)) {
			return nil
		} else if !bytes.Equal(l.mainChain[consumer.Block.Header.Height].Hash, consumer.Hash) {
			return nil
		} else {
			return fmt.Errorf("txOut is already spent on block [%x] on height %d",
				consumer.Hash, consumer.Block.Header.Height)
		}
	}
}

func (l *Miner) verifyTx(t *pb.Tx) (minerFee uint64, err error) {
	totalInput := uint64(0)
	for i, txin := range t.TxInList {
		if prevTxW := l.findTxByHash(txin.PrevTx.Bytes); prevTxW != nil {
			return 0, fmt.Errorf("cannot find tx of hash [%x]", txin.PrevTx.Bytes)
		} else {
			err := l.isTxOutSpent(prevTxW, txin.PrevOutIdx)
			if err != nil {
				return 0, err
			}

			prevTxOut := prevTxW.Tx.TxOutList[txin.PrevOutIdx]
			if !Verify(txin, prevTxOut.ReceiverPubKey.Bytes, txin.Sig.Bytes) {
				return 0, fmt.Errorf("verify txin (%d) sig failed", i)
			}
			totalInput += prevTxOut.Value
		}
	}

	totalOutput := uint64(0)
	for _, txout := range t.TxOutList {
		totalOutput += txout.Value
	}

	if totalInput < totalOutput {
		return 0, fmt.Errorf("tx input less than output")
	}

	return totalInput - totalOutput, nil
}
