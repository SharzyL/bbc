package main

import (
	"context"
	"fmt"
	"github.com/SharzyL/bbc/bbc"
	"log"
	"os"
	"time"

	"github.com/jessevdk/go-flags"
	"google.golang.org/grpc"

	"github.com/SharzyL/bbc/bbc/pb"
)

type BlockCommand struct {
	Height int64  `short:"l" long:"height" default:"-1"`
	Server string `short:"a" long:"server"`
}

const rpcTimeout = 1 * time.Second

func printBlock(b *pb.FullBlock) {
	fmt.Printf("Hash:       %x\n", bbc.Hash(b.Header))
	fmt.Printf("PrevHash:   %x\n", b.Header.PrevHash.Bytes)
	fmt.Printf("MerkleRoot: %x\n", b.Header.MerkleRoot.Bytes)
	fmt.Printf("Timestamp:  %s\n", time.UnixMilli(b.Header.Timestamp).UTC())
	fmt.Printf("Height:     %d\n", b.Header.Height)
	for i, tx := range b.TxList {
		if tx.Valid {
			fmt.Printf("Tx %d [%x]:\n", i, bbc.Hash(tx))
			fmt.Printf("  Timestamp: %s\n", time.UnixMilli(tx.Timestamp).UTC())
			for j, txin := range tx.TxInList {
				fmt.Printf("  TxIn %d:\n", j)
				fmt.Printf("    PrevTx:     %x\n", txin.PrevTx.Bytes)
				fmt.Printf("    PrevOutIdx: %d\n", txin.PrevOutIdx)
				fmt.Printf("    Sig:        %x\n", txin.Sig)
			}
			for j, txout := range tx.TxOutList {
				fmt.Printf("  TxOut %d:\n", j)
				fmt.Printf("    Value: %d\n", txout.Value)
				fmt.Printf("    ReceiverPubKey: %x\n", txout.ReceiverPubKey)
			}
		}
	}
}

func (x *BlockCommand) Execute(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, x.Server, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Panicf("cannot dial %s: %v", x.Server, err)
	}
	defer conn.Close()

	client := pb.NewMinerClient(conn)

	var hash *pb.HashVal
	if x.Height < 0 {
		ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		peekResp, err := client.PeekChain(ctx, &pb.PeekChainReq{TopHash: nil, Limit: nil})
		if err != nil {
			log.Fatalf("failed to PeekChain: %v", err)
		}
		if len(peekResp.Headers) == 0 {
			log.Fatalf("empty chain")
		}
		hash = pb.NewHashVal(bbc.Hash(peekResp.Headers[0]))
	} else {
		ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		peekResp, err := client.PeekChainByHeight(ctx, &pb.PeekChainByHeightReq{Height: x.Height})
		if err != nil {
			log.Fatalf("failed to PeekChain: %v", err)
		}
		if peekResp.Header == nil {
			log.Fatalf("too high")
		}
		hash = pb.NewHashVal(bbc.Hash(peekResp.Header))
	}

	ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
	block, err := client.GetFullBlock(ctx, hash)
	defer cancel()
	if err != nil {
		log.Fatalf("failed to GetFullBlock: %v", err)
	}
	printBlock(block)

	return nil
}

func main() {
	parser := flags.NewNamedParser("viewer", flags.Default)
	var blockCommand BlockCommand
	_, _ = parser.AddCommand("block", "view block", "view block", &blockCommand)

	_, err := parser.Parse()
	if err != nil {
		if flags.WroteHelp(err) {
			return
		} else {
			os.Exit(1)
		}
	}
}
