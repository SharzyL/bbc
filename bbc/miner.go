package bbc

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
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

	memPoolFullChan chan<- struct{}
}

const avgMiningTime = 5 * time.Second
const newBlockTime = 5 * time.Second

const blockLimit = 16

const adverticeTimeout = 5 * time.Second
const rpcTimeout = 1 * time.Second
const peekChainDefaultLimit = int64(10)

func (s *minerRpcHandler) PeekChain(ctx context.Context, req *pb.PeekChainReq) (ans *pb.PeekChainAns, err error) {
	l := s.l
	l.logger.Infow("receive PeekChain request",
		zap.String("hash", b2str(req.TopHash.Bytes)),
		zap.Int64("limit", req.GetLimit()))
	headers := make([]*pb.BlockHeader, 0, 10)

	l.chainMtx.RLock()
	defer l.chainMtx.RUnlock()

	var topHash []byte
	if req.TopHash != nil {
		topHash = req.TopHash.Bytes
	} else {
		topHash = l.mainChain[len(l.mainChain)-1].Hash
	}

	limit := req.GetLimit()
	if limit <= 0 {
		limit = peekChainDefaultLimit
	}

	for i := int64(0); i < limit; i++ {
		fullBlock := l.findBlockByHash(topHash)
		if fullBlock == nil {
			err = fmt.Errorf("cannot find block with hash %x when peekChain with depth %d", req.TopHash.Bytes, i)
			return
		}
		if fullBlock.Header.Height <= 0 {
			break
		} else {
			headers = append(headers, fullBlock.Header)
			topHash = fullBlock.Header.PrevHash.Bytes
		}
	}

	return &pb.PeekChainAns{Headers: headers}, nil
}

func (s *minerRpcHandler) PeekChainByHeight(ctx context.Context, req *pb.PeekChainByHeightReq) (ans *pb.PeekChainByHeightAns, err error) {
	l := s.l
	l.logger.Debugw("receive PeekChainByHeight request",
		zap.Int64("height", req.Height))

	l.chainMtx.RLock()
	defer l.chainMtx.RUnlock()
	if req.Height >= int64(len(l.mainChain)) || req.Height < 0 {
		return &pb.PeekChainByHeightAns{Header: nil}, nil
	} else {
		header := l.mainChain[req.Height].Block.Header
		return &pb.PeekChainByHeightAns{Header: header}, nil
	}
}

func (s *minerRpcHandler) AdvertiseBlock(ctx context.Context, req *pb.AdvertiseBlockReq) (ans *pb.AdvertiseBlockAns, err error) {
	l := s.l
	header := req.Header
	l.chainMtx.RLock()
	mainChainHeight := int64(len(l.mainChain)) - 1
	topBlockHash := l.mainChain[mainChainHeight].Hash
	l.chainMtx.RUnlock()

	l.logger.Debugw("receive AdvertiseBlock request",
		zap.Int64("h", header.Height),
		zap.Int64("selfH", mainChainHeight),
		zap.String("hashH", b2str(Hash(header))))

	if header.Height > mainChainHeight { // TODO: prevent selfish mining attack
		go l.syncBlock(req.Addr, header)
	} else if header.Height == mainChainHeight {
		// check prf(padding, header) == 0 to determine whether to sync the block
		if !bytes.Equal(topBlockHash, Hash(header)) && Hash(BytesWrapper{l.prfPadding}, header)[0]%2 == 0 {
			go l.syncBlock(req.Addr, header)
		}
	}
	return &pb.AdvertiseBlockAns{}, nil
}

func (s *minerRpcHandler) GetFullBlock(ctx context.Context, req *pb.HashVal) (*pb.FullBlock, error) {
	l := s.l
	l.logger.Debugw("receive getFullBlock request",
		zap.String("hash", b2str(req.Bytes)))
	b := l.findBlockByHash(req.Bytes)
	if b == nil {
		return nil, fmt.Errorf("cannot find block of given hash")
	} else {
		return b, nil
	}
}

func (s *minerRpcHandler) FindTx(ctx context.Context, hash *pb.HashVal) (*pb.TxInfo, error) {
	l := s.l
	l.logger.Debugw("receive FindTx request", zap.String("hash", b2str(hash.Bytes)))
	tx := l.findTxByHash(hash.Bytes)
	if tx != nil {
		l.chainMtx.RLock()
		defer l.chainMtx.RUnlock()
		if l.isBlockOnChain(tx.Block) {
			return &pb.TxInfo{BlockHeader: tx.Block.Block.Header}, nil
		}
	}
	return &pb.TxInfo{BlockHeader: nil}, nil
}

func (s *minerRpcHandler) UploadTx(ctx context.Context, tx *pb.Tx) (*pb.UploadTxAns, error) {
	l := s.l
	l.logger.Debugw("receive UploadTx request", zap.Int64("t", tx.Timestamp))
	if !tx.Valid {
		return nil, fmt.Errorf("why send me an invalid tx")
	}

	l.memPoolMtx.Lock()
	defer l.memPoolMtx.Unlock()

	fee, err := l.verifyTx(tx)
	if err != nil {
		return nil, fmt.Errorf("fail to verify tx: %v", err)
	}
	for _, txIn := range tx.TxInList {
		for _, memPoolTx := range l.memPool {
			for _, txIn2 := range memPoolTx.Tx.TxInList {
				if bytes.Equal(txIn.PrevTx.Bytes, txIn2.PrevTx.Bytes) && txIn.PrevOutIdx == txIn2.PrevOutIdx {
					return nil, fmt.Errorf("txIn already been used")
				}
			}
		}
	}
	txf := &txWithFee{Tx: tx, Fee: fee}

	l.memPool = append(l.memPool, txf)
	if len(l.memPool) >= blockLimit-1 { // reserve one space for coinbase
		select {
		case s.memPoolFullChan <- struct{}{}:
		case <-time.After(100 * time.Millisecond):
		}
	}
	//l.logger.Infow("finish receiving tx",
	//	zap.Int64("t", tx.Timestamp),
	//	zap.Uint64("fee", fee),
	//	zap.Int("l", len(l.memPool)))
	return &pb.UploadTxAns{}, nil
}

// Miner should be not be initialized manually
type Miner struct {
	SelfAddr     string // network addr of self
	PeerAddrList []string

	minerPubKey  []byte
	minerPrivKey []byte
	prfPadding   []byte // we will use y = Hash(prfPadding, x) to construct a pseudo-random function

	mainChain []*fullBlockWithHash

	hashToTx    *trie.Trie // stores *txWithConsumer
	hashToBlock *trie.Trie // stores *pb.FullBlock

	memPool []*txWithFee // transactions waiting to be packed into a block

	rpcHandler  minerRpcHandler
	peerClients []*pb.MinerClient

	memPoolMtx *sync.RWMutex
	chainMtx   *sync.RWMutex

	logger *zap.SugaredLogger
}

func NewMiner(pubKey []byte, privKey []byte, selfAddr string, peerAddrList []string) *Miner {
	logger := getLogger()

	memPoolMtx := &sync.RWMutex{}
	chainMtx := &sync.RWMutex{}

	miner := Miner{
		SelfAddr:     selfAddr,
		PeerAddrList: peerAddrList,

		minerPubKey:  pubKey,
		minerPrivKey: privKey,

		mainChain: make([]*fullBlockWithHash, 0, 1), // reserve space for genesis block

		hashToTx:    trie.NewTrie(),
		hashToBlock: trie.NewTrie(),

		memPool: make([]*txWithFee, 0, 1000),

		rpcHandler: minerRpcHandler{}, // init self pointer later

		memPoolMtx: memPoolMtx,
		chainMtx:   chainMtx,

		logger: logger,
	}
	miner.rpcHandler.l = &miner

	genesisBlock := makeFullBlockWithHash(GenesisBlock())
	miner.hashToBlock.Insert(genesisBlock.Hash, genesisBlock.Block)
	miner.mainChain = append(miner.mainChain, genesisBlock)

	logger.Infow("add genesis block", zap.String("hash", b2str(genesisBlock.Hash)))
	return &miner
}

func (l *Miner) MainLoop() {
	newBlockChan := make(chan *fullBlockWithHash, 3)
	memPoolFullChan := make(chan struct{})

	go l.serveLoop(memPoolFullChan)
	go l.advertiseLoop(newBlockChan)

	for {
		// wait for new transactions fill the mempool
		select {
		case <-memPoolFullChan:
		case <-time.After(newBlockTime):
		}

		// create new block
		newBlock := l.createBlock()
		if newBlock != nil {
			newBlockChan <- newBlock
		}
	}
}

func (l *Miner) serveLoop(memPoolFullChan chan<- struct{}) {
	lis, err := net.Listen("tcp", l.SelfAddr)
	if err != nil {
		l.logger.Fatalw("failed to listen", zap.Error(err))
	}
	s := grpc.NewServer()
	defer s.GracefulStop()
	pb.RegisterMinerServer(s, &minerRpcHandler{
		l:               l,
		memPoolFullChan: memPoolFullChan,
	})
	l.logger.Infow("starting mainloop",
		zap.String("addr", l.SelfAddr),
		zap.Strings("peerAddr", l.PeerAddrList))
	if err := s.Serve(lis); err != nil {
		l.logger.Fatalw("failed to serve %v", zap.Error(err))
	}
}

func (l *Miner) advertiseLoop(newBlockChan <-chan *fullBlockWithHash) {
	for {
		var blockToAdvertise *fullBlockWithHash
		select {
		case blockToAdvertise = <-newBlockChan:
		case <-time.After(adverticeTimeout):
			l.chainMtx.RLock()
			blockToAdvertise = l.mainChain[len(l.mainChain)-1]
			l.chainMtx.RUnlock()
		}

		// advertise new block to peers
		wg := sync.WaitGroup{}
		wg.Add(len(l.PeerAddrList))
		for i, addr := range l.PeerAddrList {
			go func(i int, addr string) {
				defer wg.Done()
				if addr == l.SelfAddr {
					return
				}

				ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
				defer cancel()
				conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock())
				if err != nil {
					l.logger.Warnw("failed to dial peer", zap.Error(err))
					return
				}
				defer conn.Close()

				client := pb.NewMinerClient(conn)

				ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
				defer cancel()
				l.logger.Debugw("advertice block to peer",
					zap.Int64("height", blockToAdvertise.Block.Header.Height),
					zap.String("hash", b2str(blockToAdvertise.Hash)),
					zap.String("peer", addr))
				_, err = client.AdvertiseBlock(ctx, &pb.AdvertiseBlockReq{
					Header: blockToAdvertise.Block.Header,
					Addr:   l.SelfAddr,
				})
				if err != nil {
					l.logger.Errorw("fail to advertise block to peer", zap.Error(err))
					return
				}
			}(i, addr)
		}
		wg.Wait()
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
	// prerequisite: topHeader.length > mainCHain.length, topHeader.top != mainChain.top

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		l.logger.Errorw("fail to dial when syncing block", zap.Error(err))
		return
	}
	defer conn.Close()

	client := pb.NewMinerClient(conn)

	hashesToSync := make([][]byte, 0, 1) // a list of unsynced block hashes from newest to oldest, hightest first

	l.chainMtx.RLock()
	originalChain := l.mainChain
	l.chainMtx.RUnlock()

	headersPending := []*pb.BlockHeader{topHeader} // a list of headers, will later be checked
	height := topHeader.Height                     // expected height of the last checked header

findMaxSynced:
	for {
		for _, header := range headersPending {
			if header.Height != height {
				l.logger.Errorw("unexpected height", zap.Int64("exp", height),
					zap.Int64("act", header.Height))
				return
			} else if header.Height < int64(len(originalChain)) && bytes.Equal(Hash(header), originalChain[height].Hash) {
				// already on the chain, no need to sync
				break findMaxSynced
			} else {
				hashesToSync = append(hashesToSync, Hash(header))
				height--
			}
		}

		if height == 0 { // no need to sync genesis block
			break
		}

		prevHash := headersPending[len(headersPending)-1].PrevHash.Bytes
		l.logger.Debugw("send peekChainReq", zap.String("addr", addr), zap.String("hash", b2str(prevHash)))

		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		ans, err := client.PeekChain(ctx, &pb.PeekChainReq{TopHash: pb.NewHashVal(prevHash)})
		cancel()

		if err != nil {
			l.logger.Errorw("fail peekChainReq", zap.Error(err))
			return
		} else if len(ans.Headers) == 0 {
			l.logger.Errorw("unexpected empty peek result")
			return
		}

		l.logger.Debugw("peekChainReq responds", zap.String("addr", addr),
			zap.Int("numHeaders", len(ans.Headers)))
		headersPending = ans.Headers
	}

	// fetching fullblocks
	newBlocks := make([]*fullBlockWithHash, 0, len(hashesToSync)) // from oldest to newest
	for i := len(hashesToSync) - 1; i >= 0; i-- {
		hash := hashesToSync[i]
		fullBlock := l.findBlockByHash(hash)
		if fullBlock == nil {
			fullBlock, err = client.GetFullBlock(ctx, pb.NewHashVal(hash))
			if err != nil {
				l.logger.Errorw("fail to get full block",
					zap.String("addr", addr),
					zap.String("hash", b2str(hash)))
				return
			}
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
			l.logger.Infow("failed to append chain, roll back")
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
		l.mainChain = append(l.mainChain, b)
		l.hashToBlock.Insert(b.Hash, b.Block)
		l.updateTxInfo(b)
	}
	success = true
	l.logger.Infow("success to sync block",
		zap.String("addr", addr), zap.Int("height", len(l.mainChain)), zap.Int64("from", minUnsynced))
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

func (l *Miner) createBlock() *fullBlockWithHash {
	//---------------------------
	// mempool and chain rlocked
	//---------------------------
	l.chainMtx.RLock()
	l.memPoolMtx.RLock()

	txList := []*pb.Tx{CoinBaseTx(l.minerPubKey, 0)}
	var memPoolTotalFee uint64

	minUncheckedIdx := 0
	for i, tx := range l.memPool {
		minUncheckedIdx = i + 1
		if len(txList) >= blockLimit {
			break
		}

		if txW := l.findTxByHash(Hash(tx.Tx)); txW != nil && l.isBlockOnChain(txW.Block) {
			// ignore tx that is already on the chain
			continue
		} else {
			memPoolTotalFee += tx.Fee
			txList = append(txList, tx.Tx)
		}
	}
	txList[0].TxOutList[0].Value = memPoolTotalFee + MinerReward
	l.memPoolMtx.RUnlock()
	l.chainMtx.RUnlock()
	//-----------------------------
	// mempool and chain runlocked
	//-----------------------------

	n := uint64(len(txList))
	numTotalTx := maxUInt64(power2Ceil(n), uint64(2)) // at least two transactions
	for i := n; i < numTotalTx; i++ {                 // padding with empty transactions
		txList = append(txList, &pb.Tx{})
	}

	// compute merkleTree
	merkleTree := make([]*pb.TxMerkleNode, 2*numTotalTx-2)
	// generate hash of transaction
	for i := uint64(0); i < numTotalTx; i++ {
		merkleTree[numTotalTx-2+i] = pb.NewMerkleTreeNode(Hash(txList[i]))
	}
	// verify merkle nodes
	for i := int(numTotalTx) - 3; i >= 0; i-- {
		merkleTree[i] = pb.NewMerkleTreeNode(Hash(merkleTree[2*i+2], merkleTree[2*i+3]))
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

	timestamp := time.Now().UnixMilli()
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
	l.logger.Infow("start mining a block",
		zap.String("prevHash", b2str(prevBlock.Hash)),
		zap.String("merkleRoot", b2str(merkleRootHash)),
		zap.Int64("timestamp", timestamp),
		zap.Int64("height", b.Header.Height),
		zap.Int("numTx", len(b.TxList)),
		zap.Uint64("numValidTx", n),
	)
	l.mine(b) // it takes a long time!

	//-----------------------
	// chain locked
	//-----------------------
	l.chainMtx.Lock()
	defer l.chainMtx.Unlock()

	// add new block to mainChain if it is fresh
	if bytes.Equal(l.mainChain[len(l.mainChain)-1].Hash, prevBlock.Hash) {
		fb := makeFullBlockWithHash(b)
		l.mainChain = append(l.mainChain, fb)
		l.hashToBlock.Insert(Hash(b.Header), b)
		l.updateTxInfo(fb)

		//-----------------------
		// memPool locked
		//-----------------------
		l.memPoolMtx.Lock()
		l.memPool = l.memPool[minUncheckedIdx:]
		l.memPoolMtx.Unlock()
		//-----------------------
		// memPool unlocked
		//-----------------------

		l.logger.Infow("new mined block added to main chain",
			zap.Int64("height", fb.Block.Header.Height),
			zap.String("hash", b2str(fb.Hash)))
		return fb
	} else {
		l.logger.Infow("mined block is superceded", zap.Int("h", len(l.mainChain)-1))
		return nil
	}
	//-----------------------
	// chain unlocked
	//-----------------------
}

func (l *Miner) mine(b *pb.FullBlock) {
	miningTime := -math.Log(rand.Float64()) * float64(avgMiningTime)
	time.Sleep(time.Duration(miningTime))

	b.Header.BlockNounce = make([]byte, NounceLen)
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

	// verify transaction
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

func (l *Miner) isBlockOnChain(b *fullBlockWithHash) bool {
	height := b.Block.Header.Height
	if height <= int64(len(l.mainChain)) && bytes.Equal(l.mainChain[height].Hash, b.Hash) {
		return true
	} else {
		return false
	}
}

func (l *Miner) verifyTx(t *pb.Tx) (minerFee uint64, err error) {
	// TODO: discards transactions appearing in previous block
	totalInput := uint64(0)
	for i, txin := range t.TxInList {
		if prevTxW := l.findTxByHash(txin.PrevTx.Bytes); prevTxW == nil {
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
