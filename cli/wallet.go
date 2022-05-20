package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/SharzyL/bbc/bbc"
	"google.golang.org/grpc"
	"log"
	"os"
	"time"

	"github.com/SharzyL/bbc/bbc/pb"
	"github.com/jessevdk/go-flags"
)

const rpcTimeout = 1 * time.Second

type Wallet struct {
	PubKey  []byte
	PrivKey []byte

	UtxoList []*pb.Utxo
}

var wallet *Wallet

type BlockCmd struct {
	Miner  string `short:"m" long:"miner" required:"true"`
	Height int64  `short:"l" long:"height" default:"-1"`
}

func NewClient(minerAddr string) (*pb.MinerClient, *grpc.ClientConn) {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, minerAddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Panicf("cannot dial %s: %v", minerAddr, err)
	}
	client := pb.NewMinerClient(conn)
	return &client, conn
}

//-------------------------
// Block command
//-------------------------

func (w *Wallet) CmdBlock(x *BlockCmd) {
	c, conn := NewClient(x.Miner)
	client := *c
	defer conn.Close()

	var hash *pb.HashVal
	if x.Height < 0 {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
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
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
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

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	block, err := client.GetFullBlock(ctx, hash)
	defer cancel()
	if err != nil {
		log.Fatalf("failed to GetFullBlock: %v", err)
	}
	bbc.PrintBlock(block, 0)
}

func (x *BlockCmd) Execute(args []string) error {
	wallet.CmdBlock(x)
	return nil
}

//-------------------------
// Show command
//-------------------------

type ShowCmd struct {
	Miner   string `short:"m" long:"miner" required:"true"`
	Verbose bool   `short:"v" long:"verbose"`
}

func (w *Wallet) CmdShow(x *ShowCmd) {
	c, conn := NewClient(x.Miner)
	client := *c
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	lookupAns, err := client.LookupUtxo(ctx, &pb.PubKey{Bytes: w.PubKey})
	if err != nil {
		log.Fatalf("cannot lookup utxo: %v", err)
	}

	fmt.Printf("pubkey: %x\n", w.PubKey)
	w.UtxoList = lookupAns.UtxoList
	balance := uint64(0)
	for i, utxo := range w.UtxoList {
		if x.Verbose {
			fmt.Printf("Utxo %d:\n", i)
			bbc.PrintUtxo(utxo, 2)
		}
		balance += utxo.Value
	}
	fmt.Printf("current balance: %d\n", balance)
}

func (x *ShowCmd) Execute(args []string) error {
	wallet.CmdShow(x)
	return nil
}

//-------------------------
// Spend command
//-------------------------

type SpendCmd struct {
	Miner  string `short:"m" long:"miner" required:"true"`
	ToAddr string `short:"a" logn:"addr" required:"true"`
	Value  uint64 `short:"v" logn:"amount" required:"true"`
}

func (w *Wallet) CmdSpend(x *SpendCmd) {
	addr, _ := hex.DecodeString(x.ToAddr)

	c, conn := NewClient(x.Miner)
	client := *c
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	lookupAns, err := client.LookupUtxo(ctx, &pb.PubKey{Bytes: w.PubKey})
	if err != nil {
		log.Fatalf("cannot lookup utxo: %v", err)
	}
	w.UtxoList = lookupAns.UtxoList

	var txInList []*pb.TxIn
	var txOutList []*pb.TxOut
	totalUtxoValue := uint64(0)
	for _, utxo := range w.UtxoList {
		totalUtxoValue += utxo.Value
		txIn := &pb.TxIn{
			PrevTx:     utxo.TxHash,
			PrevOutIdx: utxo.TxOutIdx,
			Sig:        nil,
		}
		txIn.Sig = pb.NewSigVal(bbc.Sign(txIn, w.PrivKey))
		txInList = append(txInList, txIn)
		if totalUtxoValue >= x.Value {
			break
		}
	}
	if totalUtxoValue < x.Value {
		log.Panicf("balance not enough")
	}
	txOut := &pb.TxOut{
		Value:          x.Value,
		ReceiverPubKey: &pb.PubKey{Bytes: w.PubKey},
	}
	txOutList = append(txOutList, txOut)
	if totalUtxoValue > x.Value {
		txOut2 := &pb.TxOut{
			Value:          totalUtxoValue - x.Value, // TODO: add miner fee
			ReceiverPubKey: &pb.PubKey{Bytes: addr},
		}
		txOutList = append(txOutList, txOut2)
	}
	tx := &pb.Tx{
		Valid:     true,
		TxInList:  txInList,
		TxOutList: txOutList,
		Timestamp: time.Now().UnixMilli(),
	}

	ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	_, err = client.UploadTx(ctx, tx)
	if err != nil {
		log.Panicf("failed to upload tx: %v", err)
	}

	txHash := bbc.Hash(tx)
	for {
		ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
		findTxAns, err := client.FindTx(ctx, pb.NewHashVal(txHash))
		cancel()
		if err != nil {
			log.Panicf("failed to findTX: %v", err)
		}

		if findTxAns.BlockHeader != nil {
			bbc.PrintBlockHeader(findTxAns.BlockHeader, 0)
			return
		}
		time.Sleep(time.Second)
	}
}

func (x *SpendCmd) Execute(args []string) error {
	wallet.CmdSpend(x)
	return nil
}

type GlobalOpts struct {
	PrivKey string `long:"sk" required:"true"`
	PubKey  string `long:"pk" required:"true"`
}

func main() {
	pubKey, _ := hex.DecodeString("e67af31affc28963b331eca5409e7d33b1c1d4b35aeb5b4db0c2be320095f81c")
	privKey, _ := hex.DecodeString("552d9e1e0250d975ff4b6129a5d1bf3f7dec9e85b20862af3eed4a1ffc542bd6e67af31affc28963b331eca5409e7d33b1c1d4b35aeb5b4db0c2be320095f81c")
	wallet = &Wallet{
		PubKey:   pubKey,
		PrivKey:  privKey,
		UtxoList: []*pb.Utxo{},
	}
	var blockCommand BlockCmd
	var ShowCommand ShowCmd
	var spendCommand SpendCmd

	parser := flags.NewNamedParser("viewer", flags.Default)
	_, _ = parser.AddCommand("block", "view block", "view block", &blockCommand)
	_, _ = parser.AddCommand("show", "show account", "show account", &ShowCommand)
	_, _ = parser.AddCommand("spend", "spend money", "spend money", &spendCommand)

	_, err := parser.Parse()
	if err != nil {
		if flags.WroteHelp(err) {
			return
		} else {
			os.Exit(1)
		}
	}
}
