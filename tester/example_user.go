package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jessevdk/go-flags"
	"google.golang.org/grpc"

	"github.com/SharzyL/bbc/bbc/pb"
)

const rpcTimeout = 1 * time.Second

func main() {
	var opts struct {
		IntervalMs int    `short:"f" long:"freq" default:"100"`
		Miner      string `short:"m" long:"miner" required:"true"`
	}
	_, err := flags.Parse(&opts)
	if err != nil {
		if flags.WroteHelp(err) {
			return
		} else {
			os.Exit(1)
		}
	}
	tx := &pb.Tx{
		Valid:     true,
		TxInList:  []*pb.TxIn{},
		TxOutList: []*pb.TxOut{},
		Timestamp: time.Now().UnixMilli(),
	}
	totalWorks := 500
	poolSize := 20
	pool := make(chan struct{}, poolSize)
	startTime := time.Now()

	wg := sync.WaitGroup{}
	wg.Add(totalWorks)
	for i := 0; i < totalWorks; i++ {
		go func(i int) {
			pool <- struct{}{}
			defer func() {
				<-pool
			}()

			log.Printf("start %d", i)
			grStartTime := time.Now()
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			conn, err := grpc.DialContext(ctx, opts.Miner, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				log.Panicf("failed to dial peer: %v", err)
				return
			}
			defer conn.Close()

			client := pb.NewMinerClient(conn)

			ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()

			_, err = client.UploadTx(ctx, tx)
			if err != nil {
				log.Panicf("fail to upload tx to server: %v", err)
			}
			log.Printf("end %d after %d ms", i, time.Now().Sub(grStartTime).Milliseconds())
		}(i)
	}
	wg.Wait()

	totalTime := time.Now().Sub(startTime)
	fmt.Printf("finish after %d ms\n", totalTime.Milliseconds())
}
