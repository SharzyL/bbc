package bbc

import (
	"context"
	"fmt"
	"github.com/SharzyL/bbc/bbc/pb"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"sync"
	"time"
)

type User struct {
	privKey []byte
	pubKey  []byte
	logger  *zap.SugaredLogger
	servers []string
}

func NewUser(pubKey []byte, privKey []byte, servers []string) *User {
	logger := getLogger()

	return &User{
		privKey: privKey,
		pubKey:  pubKey,
		logger:  logger,
		servers: servers,
	}
}

func (u *User) MainLoop() {
	// propagate empty blocks
	coinBase, err := u.waitFirstCoinBase()
	if err != nil {
		u.logger.Panicw("fail get coinbase", zap.Error(err))
	}
	coinBaseOut := coinBase.TxOutList[0]

	txin := &pb.TxIn{
		PrevTx:     pb.NewHashVal(Hash(coinBase)),
		PrevOutIdx: 0,
		Sig:        nil,
	}
	txout1 := &pb.TxOut{
		Value:          1,
		ReceiverPubKey: pb.NewPubKeyVal(u.pubKey),
	}
	txout2 := &pb.TxOut{
		Value:          coinBaseOut.Value - 1,
		ReceiverPubKey: pb.NewPubKeyVal(u.pubKey),
	}
	txin.Sig = pb.NewSigVal(Sign(txin, u.privKey))
	tx := &pb.Tx{
		Valid:     true,
		TxInList:  []*pb.TxIn{txin},
		TxOutList: []*pb.TxOut{txout1, txout2},
		Timestamp: 0,
	}
	u.propagateTx(tx)

	header, err := u.waitTxOnChain(tx)
	if err != nil {
		u.logger.Panicw("fail wait tx", zap.Error(err))
	}
	u.logger.Infow("detect tx on chain",
		zap.String("txHash", b2str(Hash(tx))),
		zap.String("blockHash", b2str(Hash(header))),
		zap.Int64("blockHeight", header.Height),
	)
}

func (u *User) propagateTx(tx *pb.Tx) {
	wg := sync.WaitGroup{}
	wg.Add(len(u.servers))
	for _, server := range u.servers {
		go func(server string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			conn, err := grpc.DialContext(ctx, server, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				u.logger.Errorw("failed to dial peer", zap.Error(err))
				return
			}
			defer conn.Close()

			client := pb.NewMinerClient(conn)

			ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()

			u.logger.Infow("start upload tx",
				zap.String("server", server), zap.Int64("t", tx.Timestamp))
			_, err = client.UploadTx(ctx, tx)
			if err != nil {
				u.logger.Errorw("fail to upload tx to server", zap.Error(err))
				return
			}
		}(server)
	}
	wg.Wait()
}

func (u *User) waitTxOnChain(tx *pb.Tx) (*pb.BlockHeader, error) {
	txHash := Hash(tx)
	for {
		for _, server := range u.servers {
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			conn, err := grpc.DialContext(ctx, server, grpc.WithInsecure(), grpc.WithBlock())
			cancel()
			if err != nil {
				return nil, fmt.Errorf("failed to dial server %s: %v", server, err)
			}

			client := pb.NewMinerClient(conn)

			ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
			findTxAns, err := client.FindTx(ctx, pb.NewHashVal(txHash))
			cancel()
			if err != nil {
				return nil, fmt.Errorf("failed to findTX: %v", err)
			}

			if findTxAns.BlockHeader != nil {
				return findTxAns.BlockHeader, nil
			}
			time.Sleep(time.Second)
		}
	}
}

func (u *User) waitFirstCoinBase() (*pb.Tx, error) {
	for {
		for _, server := range u.servers {
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			conn, err := grpc.DialContext(ctx, server, grpc.WithInsecure(), grpc.WithBlock())
			cancel()
			if err != nil {
				return nil, fmt.Errorf("failed to dial server %s: %v", server, err)
			}

			client := pb.NewMinerClient(conn)

			ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
			peekChainAns, err := client.PeekChainByHeight(ctx, &pb.PeekChainByHeightReq{Height: 1})
			cancel()
			if err != nil {
				return nil, fmt.Errorf("failed to findTX: %v", err)
			}
			if peekChainAns.Header != nil {
				ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
				block, err := client.GetFullBlock(ctx, pb.NewHashVal(Hash(peekChainAns.Header)))
				cancel()
				if err != nil {
					return nil, fmt.Errorf("failed to findTX: %v", err)
				}
				return block.TxList[0], nil
			}
			time.Sleep(time.Second)
		}
	}
}
