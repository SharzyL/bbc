package bbc

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"net"
	"os"
	"strings"
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
	ListenAddr string // network addr to bind to
	SelfAddr   string // network addr of self, which can be accessed by others

	minerPubKey  []byte
	minerPrivKey []byte
	prfPadding   []byte // we will use y = Hash(prfPadding, x) to construct a pseudo-random function

	mainChain []*fullBlockWithHash

	hashToTx     *trie.Trie // stores *txWithConsumer
	hashToBlock  *trie.Trie // stores *pb.FullBlock
	pubKeyToUtxo *trie.Trie // stores []*utxoRecord

	memPool []*txWithFee // transactions waiting to be packed into a block

	rpcHandler minerRpcHandler

	peerMgr               *peerMgr
	storageMgr            *storageMgr
	hashesUnderSyncing    list.List // store a list of headers that is being synced, to avoid repeated sync
	hashesUnderSyncingMtx *sync.RWMutex

	memPoolMtx      *sync.RWMutex
	chainMtx        *sync.RWMutex
	miningInterrupt *atomic.Bool // used to interrupt mining when current mining may become unnecessary

	logger *zap.SugaredLogger
}

type MinerOptions struct {
	PubKey  []byte
	PrivKey []byte

	ListenAddr string
	SelfAddr   string

	PeerAddrList []string
	Loglevel     string

	StorageDir string
}

func NewMiner(opts *MinerOptions) *Miner {
	logger := GetLogger(opts.Loglevel)

	if len(opts.PubKey) != PubKeyLen {
		logger.Fatalw("incorrect pubKey length", zap.Int("expect", PubKeyLen), zap.Int("actual", len(opts.PubKey)))
	}
	if len(opts.PrivKey) != PrivKeyLen {
		logger.Fatalw("incorrect privKey length", zap.Int("expect", PrivKeyLen), zap.Int("actual", len(opts.PrivKey)))
	}

	memPoolMtx := &sync.RWMutex{}
	chainMtx := &sync.RWMutex{}

	if strings.HasPrefix(opts.SelfAddr, "0.0.0.0:") || strings.HasPrefix(opts.SelfAddr, "[::]:") {
		logger.Panic("cannot use zero address as self address", zap.String("selfAddr", opts.SelfAddr))
	}

	sMgr, err := newStorageMgr(opts.StorageDir)
	if err != nil {
		logger.Panic("failed to create storage: %v", zap.Error(err))
	}

	miner := Miner{
		ListenAddr: opts.ListenAddr,
		SelfAddr:   opts.SelfAddr,

		minerPubKey:  opts.PubKey,
		minerPrivKey: opts.PrivKey,

		mainChain: make([]*fullBlockWithHash, 0, 1), // reserve space for genesis block

		hashToTx:     trie.NewTrie(),
		hashToBlock:  trie.NewTrie(),
		pubKeyToUtxo: trie.NewTrie(),

		memPool: make([]*txWithFee, 0, 1000),

		rpcHandler: minerRpcHandler{}, // init self pointer later

		peerMgr:               newPeerMgr(logger),
		storageMgr:            sMgr,
		hashesUnderSyncing:    list.List{},
		hashesUnderSyncingMtx: &sync.RWMutex{},

		memPoolMtx:      memPoolMtx,
		chainMtx:        chainMtx,
		miningInterrupt: atomic.NewBool(false),

		logger: logger,
	}
	miner.rpcHandler.l = &miner

	for _, peerAddr := range opts.PeerAddrList {
		miner.peerMgr.addPeer(peerAddr) // at initialization, no need to lock
	}

	logger.Infow("add miner pubkey", zap.String("hash", b2str(opts.PubKey)))

	genesisBlock := makeFullBlockWithHash(GenesisBlock())
	miner.hashToBlock.Insert(genesisBlock.Hash, genesisBlock.Block)
	miner.mainChain = append(miner.mainChain, genesisBlock)

	logger.Infow("add genesis block", zap.String("hash", b2str(genesisBlock.Hash)))
	return &miner
}

func (l *Miner) MainLoop() {
	go l.serveLoop()
	go l.advertiseLoop()

	for {
		newBlock := l.createBlock()
		if newBlock != nil {
			l.peerMgr.mtx.RLock()
			l.peerMgr.goForEachAlivePeer(func(p string) {
				l.sendAdvertisement(newBlock.Block.Header, p)
			})
			l.peerMgr.mtx.RUnlock()
		}
	}
}

func (l *Miner) LoadBlocksFromDisk() {
	h := int64(1)
	for {
		b, err := l.storageMgr.LoadBlock(h)
		if err != nil {
			if os.IsNotExist(err) {
				break
			} else {
				l.logger.Panic("failed to load block of height %d: %v", h, err)
			}
		}

		fb := makeFullBlockWithHash(b)
		err = l.verifyBlock(b)
		if err != nil {
			l.logger.Panic("failed to verify block from disk", zap.Int64("height", h), zap.Error(err))
		}
		l.mainChain = append(l.mainChain, fb)
		l.hashToBlock.Insert(Hash(b.Header), b)
		l.updateTxInfo(fb)
		h++
	}
	if h == 1 {
		l.logger.Infow("no block is loaded from disk", zap.String("folder", l.storageMgr.Folder))
	} else {
		l.logger.Infow("successfully load blocks from disk", zap.Int64("height", h), zap.String("folder", l.storageMgr.Folder))
	}
}

func (l *Miner) CleanDiskBlocks() {
	err := l.storageMgr.cleanBlocks()
	if err != nil {
		l.logger.Panic("failed to clean blocks: %v", err)
	}
}

func (l *Miner) serveLoop() {
	lis, err := net.Listen("tcp", l.ListenAddr)
	if err != nil {
		l.logger.Fatalw("failed to listen", zap.Error(err))
	}
	s := grpc.NewServer()
	defer s.GracefulStop()
	pb.RegisterMinerServer(s, &minerRpcHandler{
		l: l,
	})
	l.logger.Infow("starting mainloop",
		zap.String("listen", l.ListenAddr), zap.String("selfAddr", l.SelfAddr))
	if err := s.Serve(lis); err != nil {
		l.logger.Fatalw("failed to serve %v", zap.Error(err))
	}
}

func (l *Miner) advertiseLoop() {
	// first advertise genesis block, to attract their own advertisement
	l.chainMtx.RLock()
	l.peerMgr.mtx.RLock()
	l.peerMgr.goForEachAlivePeer(func(p string) {
		go l.sendAdvertisement(l.mainChain[0].Block.Header, p)
	})
	l.peerMgr.mtx.RUnlock()
	l.chainMtx.RUnlock()

	time.Sleep(advertiseInterval)
	for {
		l.chainMtx.RLock()
		headerToAdvertise := l.mainChain[len(l.mainChain)-1].Block.Header
		l.chainMtx.RUnlock()

		l.peerMgr.mtx.RLock()
		peerAddr := l.peerMgr.getNextToAdvertise(headerToAdvertise)
		l.peerMgr.mtx.RUnlock()

		// advertise new block to peers
		if peerAddr != nil {
			// here we call onStartAdvertise to force update lastTryAdvTime and lastTryAdvHeader
			l.peerMgr.mtx.Lock()
			l.peerMgr.onStartAdvertise(peerAddr.addr, headerToAdvertise)
			l.peerMgr.mtx.Unlock()

			go l.sendAdvertisement(headerToAdvertise, peerAddr.addr)
		} else {
			time.Sleep(advertiseInterval)
		}
	}
}

func (l *Miner) sendAdvertisement(header *pb.BlockHeader, addr string) {
	l.peerMgr.mtx.Lock()
	l.peerMgr.onStartAdvertise(addr, header)
	l.peerMgr.mtx.Unlock()

	// connect to peer
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock())
	cancel()
	if err != nil {
		l.logger.Warnw("failed to dial peer", zap.String("peerAddr", addr), zap.Error(err))
		l.peerMgr.mtx.Lock()
		l.peerMgr.onFailedAdvertise(addr)
		l.peerMgr.mtx.Unlock()
		return
	}
	defer conn.Close()

	client := pb.NewMinerClient(conn)

	// prepare peers
	var peers []string
	l.peerMgr.mtx.RLock()
	for addr, peer := range l.peerMgr.peers {
		if !peer.isDead {
			peers = append(peers, addr)
		}
	}
	l.peerMgr.mtx.RUnlock()

	// send advertisement
	l.logger.Debugw("advertise block to peer",
		zap.Int64("height", header.Height),
		zap.String("hash", b2str(Hash(header))),
		zap.String("peer", addr))
	ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
	ans, err := client.AdvertiseBlock(ctx, &pb.AdvertiseBlockReq{
		Header: header,
		Addr:   l.SelfAddr,
		Peers:  peers,
	})
	cancel()
	if err != nil {
		l.logger.Errorw("fail to advertise block to peer", zap.Error(err), zap.String("peerAddr", addr))
		l.peerMgr.mtx.Lock()
		l.peerMgr.onFailedAdvertise(addr)
		l.peerMgr.mtx.Unlock()
		return
	} else {
		l.logger.Debugw("advertise block to peer successful",
			zap.Int64("peerHeight", ans.Header.Height),
			zap.String("hash", b2str(Hash(header))),
			zap.String("peer", addr))
		if ans.Header.Height > header.Height {
			go l.syncBlock(addr, ans.Header)
		}
		l.peerMgr.mtx.Lock()
		l.peerMgr.onSucceedAdvertise(addr, header)
		l.peerMgr.mtx.Unlock()
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
	headerHash := Hash(topHeader)

	// to prevent repeated sync block
	l.hashesUnderSyncingMtx.Lock()
	for e := l.hashesUnderSyncing.Front(); e != nil; e = e.Next() {
		if hash := e.Value.([]byte); bytes.Equal(headerHash, hash) {
			l.logger.Debugw("abort syncing because it is already running",
				zap.String("hash", b2str(Hash(topHeader))),
				zap.Int64("height", topHeader.Height),
				zap.String("peerAddr", addr),
			)
			l.hashesUnderSyncingMtx.Unlock()
			return
		}
	}
	l.hashesUnderSyncing.PushFront(headerHash)
	l.hashesUnderSyncingMtx.Unlock()

	defer func() {
		l.hashesUnderSyncingMtx.Lock()
		for e := l.hashesUnderSyncing.Front(); e != nil; e = e.Next() {
			if hash := e.Value.([]byte); bytes.Equal(headerHash, hash) {
				l.hashesUnderSyncing.Remove(e)
				break
			}
		}
		l.hashesUnderSyncingMtx.Unlock()
	}()

	l.logger.Infow("start syncing according to header",
		zap.String("hash", b2str(Hash(topHeader))),
		zap.Int64("height", topHeader.Height),
		zap.String("peerAddr", addr),
	)

	// connecting to peer
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(200*1024*1024)))
	if err != nil {
		l.logger.Errorw("fail to dial when syncing block", zap.Error(err), zap.String("peerAddr", addr))
		return
	}
	defer conn.Close()

	client := pb.NewMinerClient(conn)

	hashesToSync := make([][]byte, 0, 1) // a list of unsynced block hashes from newest to oldest, hightest first

	l.chainMtx.RLock()
	originalChain := l.mainChain
	l.chainMtx.RUnlock()

	// in case that another sync thread has already completed the syncing
	if topHeader.Height < int64(len(originalChain)-1) || bytes.Equal(originalChain[len(originalChain)-1].Hash, Hash(topHeader)) {
		l.logger.Infow("found no need to sync actually before peeking", zap.Int("mainChainHeight", len(originalChain)-1))
		return
	}

	headersPending := []*pb.BlockHeader{topHeader} // a list of headers, will later be checked
	height := topHeader.Height                     // expected height of the last checked header

	// collecting hashes to sync
findMaxSynced:
	for {
		for _, header := range headersPending {
			if header.Height != height {
				l.logger.Errorw("unexpected height", zap.Int64("expected", height),
					zap.Int64("actual", header.Height))
				return
			} else if header.Height <= int64(len(originalChain))-1 && bytes.Equal(Hash(header), originalChain[height].Hash) {
				// already on the chain, no need to sync
				break findMaxSynced
			} else if header.Height > 0 {
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
		// TODO: parallelize this
		if fullBlock == nil {
			ctx, cancel := context.WithTimeout(context.Background(), longRpcTimeout)
			fullBlock, err = client.GetFullBlock(ctx, pb.NewHashVal(hash))
			cancel()
			if err != nil {
				l.logger.Errorw("fail to GetFullBlock when syncing",
					zap.Error(err),
					zap.String("peerAddr", addr),
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

	// in case that another sync thread has already completed the syncing
	if bytes.Equal(l.mainChain[len(l.mainChain)-1].Hash, newBlocks[len(newBlocks)-1].Hash) {
		l.logger.Infow("found no need to sync before manipulating chain",
			zap.Int("mainChainHeight", len(l.mainChain)-1))
		success = true
		return
	}

	l.mainChain = l.mainChain[:minUnsynced]
	for _, b := range newBlocks {
		l.logger.Infow("trying to append synced block to chain",
			zap.String("hash", b2str(b.Hash)),
			zap.Int64("height", b.Block.Header.Height))
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
	for _, b := range newBlocks {
		err = l.storageMgr.DumpBlock(b.Block)
		if err != nil {
			// notice that we do not think this is a critical error
			l.logger.Errorw("failed to dump block to disk whic syncing", zap.Error(err), zap.Int64("height", b.Block.Header.Height))
			break
		}
	}
	success = true
	l.logger.Infow("success to sync block from peer",
		zap.String("peerAddr", addr),
		zap.Int("height", len(l.mainChain)-1),
		zap.Int64("fromHeight", minUnsynced))
	//----------------------
	// chain unlocked
	//----------------------
}

func (l *Miner) updateTxInfo(fullBlock *fullBlockWithHash) {
	// NOTE: it is co-locked with chainMtx

	// insert all tx to hashToTx
	// update consumers of previous tx
	// update pubKeyToUtxo
	for _, tx := range fullBlock.Block.TxList {
		if tx.Valid {
			txw := makeTxWithConsumer(tx, fullBlock)
			l.hashToTx.Insert(Hash(tx), txw)
			for _, txIn := range tx.TxInList {
				prevTx := l.findTxByHash(txIn.PrevTx.Bytes)
				if txIn.PrevOutIdx >= uint32(len(prevTx.Tx.TxOutList)) {
					l.logger.Errorw("PrevOutIdx too large")
					return
				}
				prevTx.Consumers[txIn.PrevOutIdx] = fullBlock
			}

			for i, txOut := range tx.TxOutList {
				pubKey := txOut.ReceiverPubKey.Bytes
				utxoListUnconverted := l.pubKeyToUtxo.Search(pubKey) // stupid golang
				if utxoListUnconverted == nil {
					utxoListUnconverted = make([]*utxoRecord, 0, 1)
				}
				utxoList := utxoListUnconverted.([]*utxoRecord)

				utxoList = append(utxoList, &utxoRecord{
					Txw:      txw,
					TxOutIdx: uint32(i),
				})
				l.pubKeyToUtxo.Insert(pubKey, utxoList)
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
		err := l.storageMgr.DumpBlock(b)
		if err != nil {
			l.logger.Error("failed to dump block while creating new block", zap.Error(err), zap.Int64("height", b.Header.Height))
		}

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
	if height <= miningDifficultyBlocks {
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

	// verify height
	if int(h) != len(l.mainChain) {
		return fmt.Errorf("height of block incorrect, expected %d, actual %d", len(l.mainChain), h)
	}

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
	if !hasLeadingZeros(Hash(b.Header), int(expectedDifficulty)) {
		return fmt.Errorf("verify nounce failed, header hash %x (difficulty %d)", Hash(b.Header), expectedDifficulty)
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
