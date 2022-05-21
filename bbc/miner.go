package bbc

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/atomic"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/SharzyL/bbc/bbc/pb"
	"github.com/SharzyL/bbc/bbc/trie"
)

// Miner should be not be initialized manually, use NewMiner instead
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

	memPoolMtx      *sync.RWMutex
	chainMtx        *sync.RWMutex
	miningInterrupt *atomic.Bool // used to interrupt mining when current mining may become unnecessary

	logger *zap.SugaredLogger
}

func NewMiner(pubKey []byte, privKey []byte, selfAddr string, peerAddrList []string) *Miner {
	logger := GetLogger()

	if len(pubKey) != PubKeyLen {
		logger.Fatalw("incorrect pubKey length", zap.Int("expect", PubKeyLen), zap.Int("actual", len(pubKey)))
	}
	if len(privKey) != PrivKeyLen {
		logger.Fatalw("incorrect privKey length", zap.Int("expect", PrivKeyLen), zap.Int("actual", len(privKey)))
	}

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

		memPoolMtx:      memPoolMtx,
		chainMtx:        chainMtx,
		miningInterrupt: atomic.NewBool(false),

		logger: logger,
	}
	miner.rpcHandler.l = &miner

	logger.Infow("add miner pubkey", zap.String("hash", b2str(pubKey)))

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
		for {
			newBlock := l.createBlock()
			if newBlock != nil {
				newBlockChan <- newBlock // send new block to advertise thread
				break
			}
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
		case <-time.After(advertiseTimeout):
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
				l.logger.Debugw("advertise block to peer",
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
	// prerequisite: topHeader.length >= mainCHain.length, topHeader.top != mainChain.top

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
			l.logger.Errorw("failed to PeekChain", zap.Error(err))
			return
		} else if len(ans.Headers) == 0 {
			l.logger.Errorw("unexpected empty peek result")
			return
		}

		l.logger.Debugw("PeekChainReq responds", zap.String("addr", addr),
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
					zap.Error(err),
					zap.String("addr", addr),
					zap.String("hash", b2str(hash)))
				return
			}
			recvBlockHash := Hash(fullBlock.Header)
			if !bytes.Equal(recvBlockHash, hash) {
				l.logger.Errorw("receive block hash not match",
					zap.String("expect", b2str(hash)),
					zap.String("actual", b2str(recvBlockHash)))
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
		l.miningInterrupt.Store(true)
	}
	success = true
	l.logger.Infow("success to sync block from peer",
		zap.String("peerAddr", addr),
		zap.Int("height", len(l.mainChain)),
		zap.Int64("fromHeight", minUnsynced))
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
	difficulty := l.getDifficulty(l.mainChain, int64(len(l.mainChain))-1)
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
			Difficulty:  difficulty,
			BlockNounce: make([]byte, NounceLen),
		},
		TxList:     txList,
		MerkleTree: merkleTree,
	}
	l.logger.Infow("start mining a block",
		zap.String("prevHash", b2str(prevBlock.Hash)),
		zap.String("merkleRoot", b2str(merkleRootHash)),
		zap.Int64("timestamp", timestamp),
		zap.Int64("height", b.Header.Height),
		zap.Uint64("difficulty", b.Header.Difficulty),
		zap.Int("numTx", len(b.TxList)),
		zap.Uint64("numValidTx", n),
	)

	// start mining, takes a long time!
	startMineTime := time.Now()
	suc := Mine(b.Header, l.miningInterrupt, int(difficulty))
	miningTime := time.Now().Sub(startMineTime)
	if !suc {
		l.logger.Infow("mining interrupted", zap.Duration("usedTime", miningTime))
		return nil
	} else {
		l.logger.Infow("mined a new block",
			zap.Duration("usedTime", miningTime),
			zap.String("nounce", b2str(b.Header.BlockNounce)),
		)
	}

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
			zap.String("hash", b2str(fb.Hash)),
			zap.String("nounce", b2str(b.Header.BlockNounce)),
			zap.Int64("height", fb.Block.Header.Height),
		)
		return fb
	} else {
		l.logger.Infow("mined block is superceded",
			zap.Int64("blockHeight", b.Header.Height),
			zap.Int("chainHeight", len(l.mainChain)-1),
		)
		return nil
	}
	//-----------------------
	// chain unlocked
	//-----------------------
}

func (l *Miner) getDifficulty(chain []*fullBlockWithHash, height int64) uint64 {
	// the difficulty is set that according to the difficulty of the last k blocks,
	// the next block should be mined out every [exp, 2 * exp) second
	// TODO: add timestamp check when creating new block
	if height <= 2 {
		return defaultMiningDifficulty
	}
	k := minInt64(height-1, miningDifficultyBlocks) // starting from block 1

	totalHashNum := uint64(0)
	for i := height - k; i < height; i++ {
		totalHashNum += 1 << chain[i].Block.Header.Difficulty
	}
	timeGap := chain[height-1].Block.Header.Timestamp - chain[height-k].Block.Header.Timestamp
	estimatedComputingPower := totalHashNum / uint64(timeGap) // unit hash/ms
	return uint64(log2Floor(uint64(expectedMiningTime.Milliseconds()) * estimatedComputingPower))
}

func (l *Miner) verifyBlock(b *pb.FullBlock) error {
	header := b.Header
	h := header.Height
	prevHeader := l.mainChain[h-1].Block.Header
	prevHash := l.mainChain[h-1].Hash

	// verify timestamp
	if header.Timestamp < prevHeader.Timestamp { // asssuption is that genesis block is very early
		return fmt.Errorf("block timestamp %d is smaller than prev %d", header.Timestamp, prevHeader.Timestamp)
	}
	nowMs := time.Now().UnixMilli()
	if header.Timestamp > nowMs+maxTimeErrorAllowed.Milliseconds() {
		return fmt.Errorf("block timestamp %d is from future (current %d)", header.Timestamp, nowMs)
	}

	// verify prevHash
	if !bytes.Equal(prevHash, header.PrevHash.Bytes) {
		return fmt.Errorf("prevHash %x not consistent with that on chain %x", b2str(prevHash), b2str(header.PrevHash.Bytes))
	}

	// verify size
	if len(b.TxList) < 2 {
		return fmt.Errorf("block should contain at least two transactions, actual %d", len(b.TxList))
	}
	d := log2Floor(uint64(len(b.TxList)))
	numTx := 1 << d
	if len(b.TxList) != numTx {
		return fmt.Errorf("num of transactions %d not a power of 2", len(b.TxList))
	}
	if len(b.MerkleTree) != 2*numTx-2 {
		return fmt.Errorf("len of merkle tree %d not consistent with num of tx %d", len(b.MerkleTree), numTx)
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
	expectedDifficulty := l.getDifficulty(l.mainChain, b.Header.Height-1)
	if b.Header.Difficulty != expectedDifficulty {
		return fmt.Errorf("wrong difficulty, expected %d, actual %d", expectedDifficulty, b.Header.Difficulty)
	}
	if !hasLeadingZeros(Hash(b.Header), defaultMiningDifficulty) {
		return fmt.Errorf("verify nounce failed, header hash %x", Hash(b.Header))
	}

	// verify transaction
	coinBaseVal, err := l.verifyCoinBase(b.TxList[0])
	if err != nil {
		return fmt.Errorf("verify coinbase failed: %v", err)
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
	nowMs := time.Now().UnixMilli()
	if t.Timestamp > nowMs+maxTimeErrorAllowed.Milliseconds() {
		return 0, fmt.Errorf("tx (timestamp %d) from the future (now %d)", t.Timestamp, nowMs)
	}
	totalInput := uint64(0)
	for i, txin := range t.TxInList {
		if len(txin.Sig.Bytes) != SigLen {
			return 0, fmt.Errorf("illegal signature length")
		}
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
