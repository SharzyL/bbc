package bbc

import (
	"context"
	"crypto/ed25519"
	"github.com/SharzyL/bbc/bbc/pb"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"time"
)

type User struct {
	privKey []byte
	pubKey  []byte
	logger  *zap.SugaredLogger
	servers []string
}

func NewUser(servers []string) *User {
	logger, _ := zap.NewDevelopment()
	sugarLogger := logger.Sugar()
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		sugarLogger.Panicw("cannot gen key", zap.Error(err))
	} else {
		sugarLogger.Infow("gen key for miner", zap.String("pubKey", b2str(pubKey)))
	}

	return &User{
		privKey: privKey,
		pubKey:  pubKey,
		logger:  sugarLogger,
		servers: servers,
	}
}

func (u *User) MainLoop(txIntervalMs int) {
	for {
		tx := &pb.Tx{
			Valid:     true,
			TxInList:  make([]*pb.TxIn, 0),
			TxOutList: make([]*pb.TxOut, 0),
			Timestamp: time.Now().UnixMilli(),
		}

		time.Sleep(time.Duration(txIntervalMs) * time.Millisecond)

		for _, server := range u.servers {
			go func(server string) {
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
	}
}
