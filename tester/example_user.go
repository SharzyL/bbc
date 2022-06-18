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
		Miner       string `short:"m" long:"miner" required:"true"`
		NumWorks    int    `short:"n" default:"1000"`
		Concurrency int    `short:"c" default:"20"`
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

	wg := sync.WaitGroup{}
	wg.Add(opts.NumWorks)

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, opts.Miner, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Panicf("failed to dial peer: %v", err)
		return
	}
	defer conn.Close()

	client := pb.NewMinerClient(conn)

	for i := 0; i < opts.NumWorks; i++ {
		go func(i int) {
			defer wg.Done()

			pool <- struct{}{}
			defer func() {
				<-pool
			}()

			log.Printf("start %d", i)
			grStartTime := time.Now()
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
	fmt.Printf("%d tasks finished after %d ms\n", opts.NumWorks, totalTime.Milliseconds())
}
