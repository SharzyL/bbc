package bbc

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/SharzyL/bbc/bbc/pb"
)

type minerRpcHandler struct {
	pb.UnimplementedMinerServer
	l *Miner

	memPoolFullChan chan<- struct{}
}

// TODO: check input array length when receiving requests

func (s *minerRpcHandler) PeekChain(ctx context.Context, req *pb.PeekChainReq) (ans *pb.PeekChainAns, err error) {
	l := s.l

	reqHashStr := "nil"
	if req.TopHash != nil {
		reqHashStr = b2str(req.TopHash.Bytes)
	}
	l.logger.Debugw("receive PeekChain request",
		zap.String("hash", reqHashStr),
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
		zap.String("addr", req.Addr),
		zap.Int64("h", header.Height),
		zap.Int64("selfH", mainChainHeight),
		zap.String("hashH", b2str(Hash(header))))

	if header.Height > mainChainHeight {
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
	l.logger.Debugw("receive GetFullBlock request",
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
		case <-time.After(time.Millisecond):
		}
	}
	l.logger.Debugw("finish receiving tx",
		zap.Int64("t", tx.Timestamp),
		zap.Uint64("fee", fee),
		zap.Int("l", len(l.memPool)))
	return &pb.UploadTxAns{}, nil
}

func (s *minerRpcHandler) LookupUtxo(ctx context.Context, pubKey *pb.PubKey) (*pb.LookupUtxoAns, error) {
	l := s.l
	if pubKey == nil || len(pubKey.Bytes) != PubKeyLen {
		return nil, fmt.Errorf("invalid pubkey")
	}
	l.chainMtx.RLock()
	chain := l.mainChain
	defer l.chainMtx.RUnlock()

	var utxoList []*pb.Utxo
	for _, b := range chain {
		for _, tx := range b.Block.TxList {
			if !tx.Valid {
				continue
			}
			txHash := Hash(tx)
			txw := l.findTxByHash(txHash)

			for i, txOut := range tx.TxOutList {
				if err := l.isTxOutSpent(txw, uint32(i)); err == nil && bytes.Equal(pubKey.Bytes, txOut.ReceiverPubKey.Bytes) {
					utxoList = append(utxoList, &pb.Utxo{
						Value:    txOut.Value,
						TxHash:   pb.NewHashVal(txHash),
						TxOutIdx: uint32(i),
						PubKey:   txOut.ReceiverPubKey,
					})
				}
			}
		}
	}
	return &pb.LookupUtxoAns{UtxoList: utxoList}, nil
}
