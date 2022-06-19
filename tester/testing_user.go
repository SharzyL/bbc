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
		Miners      []string `short:"m" long:"miner"`
		NumWorks    int      `short:"n" default:"1000"`
		Concurrency int      `short:"c" default:"20"`
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
	pool := make(chan struct{}, opts.Concurrency)

	startTime := time.Now()

	clients := make([]pb.MinerClient, 0, len(opts.Miners))
	for _, miner := range opts.Miners {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		conn, err := grpc.DialContext(ctx, miner, grpc.WithInsecure(), grpc.WithBlock())
		cancel()
		if err != nil {
			log.Panicf("failed to dial peer: %v", err)
			return
		}
		clients = append(clients, pb.NewMinerClient(conn))
	}

	wg := sync.WaitGroup{}
	wg.Add(opts.NumWorks)

	for i := 0; i < opts.NumWorks; i++ {
		go func(i int) {
			defer wg.Done()
			pool <- struct{}{}
			defer func() {
				<-pool
			}()

			log.Printf("start %d", i)
			grStartTime := time.Now()

			for _, cli := range clients {
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
				_, err := cli.UploadTx(ctx, tx)
				cancel()
				if err != nil {
					log.Panicf("fail to upload tx to server: %v", err)
				}
				log.Printf("end %d after %d ms", i, time.Now().Sub(grStartTime).Milliseconds())
			}
		}(i)
	}
	wg.Wait()

	totalTime := time.Now().Sub(startTime)
	fmt.Printf("%d tasks finished after %d ms\n", opts.NumWorks, totalTime.Milliseconds())
}
