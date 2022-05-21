package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/alecthomas/kong"
	"google.golang.org/grpc"

	"github.com/SharzyL/bbc/bbc"
	"github.com/SharzyL/bbc/bbc/pb"
)

const rpcTimeout = 1 * time.Second

type Wallet struct {
	PubKey  []byte
	PrivKey []byte

	Miner string
	Addr  map[string][]byte

	Conn   *grpc.ClientConn
	Client pb.MinerClient

	UtxoList []*pb.Utxo
}

type WalletConfig struct {
	PubKey  string            `json:"pubKey"`
	PrivKey string            `json:"privKey"`
	Miner   string            `json:"miner"`
	Addr    map[string]string `json:"addr"`
}

func NewWallet(conf *WalletConfig) *Wallet {
	pubKey, err := hex.DecodeString(conf.PubKey)
	if err != nil {
		log.Panicf("canont parse pubkey '%s': %v", conf.PubKey, err)
	}
	privKey, err := hex.DecodeString(conf.PrivKey)
	if err != nil {
		log.Panicf("canont parse privkey '%s': %v", conf.PrivKey, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, conf.Miner, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Panicf("cannot dial %s: %v", conf.Miner, err)
	}

	addr := make(map[string][]byte)
	for name, pubKey := range conf.Addr {
		binPubKey, _ := hex.DecodeString(pubKey)
		addr[name] = binPubKey
	}
	return &Wallet{
		PubKey:   pubKey,
		PrivKey:  privKey,
		Miner:    conf.Miner,
		Addr:     addr,
		Conn:     conn,
		Client:   pb.NewMinerClient(conn),
		UtxoList: []*pb.Utxo{},
	}
}

func (w *Wallet) Close() {
	if w.Conn != nil {
		_ = w.Conn.Close()
	}
}

func (w *Wallet) GetUtxo() {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	lookupAns, err := w.Client.LookupUtxo(ctx, &pb.PubKey{Bytes: w.PubKey})
	if err != nil {
		log.Fatalf("cannot lookup utxo: %v", err)
	}
	w.UtxoList = lookupAns.UtxoList
}

func (w *Wallet) CmdChain(height int64) {
	var hash *pb.HashVal
	if height < 0 {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		peekResp, err := w.Client.PeekChain(ctx, &pb.PeekChainReq{TopHash: nil, Limit: nil})
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
		peekResp, err := w.Client.PeekChainByHeight(ctx, &pb.PeekChainByHeightReq{Height: height})
		if err != nil {
			log.Fatalf("failed to PeekChain: %v", err)
		}
		if peekResp.Header == nil {
			log.Fatalf("too high")
		}
		hash = pb.NewHashVal(bbc.Hash(peekResp.Header))
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	block, err := w.Client.GetFullBlock(ctx, hash)
	defer cancel()
	if err != nil {
		log.Fatalf("failed to GetFullBlock: %v", err)
	}
	bbc.PrintBlock(block, 0)
}

func (w *Wallet) CmdShow(verbose bool) {
	w.GetUtxo()
	fmt.Printf("pubkey: %x\n", w.PubKey)
	balance := uint64(0)
	for i, utxo := range w.UtxoList {
		if verbose {
			fmt.Printf("Utxo %d:\n", i)
			bbc.PrintUtxo(utxo, 2)
		}
		balance += utxo.Value
	}
	fmt.Printf("current balance: %d\n", balance)
}

func (w *Wallet) CmdTransfer(toAddr string, value uint64, fee uint64, nowait bool) {
	w.GetUtxo()
	addr, found := w.Addr[toAddr]
	if !found {
		parsedAddr, err := hex.DecodeString(toAddr)
		if err != nil {
			log.Fatalf("cannot parse toAddr '%s': %v", toAddr, err)
		}
		addr = parsedAddr
	}

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
		if totalUtxoValue >= value {
			break
		}
	}
	if totalUtxoValue < value {
		log.Panicf("balance not enough")
	}
	txOut := &pb.TxOut{
		Value:          value,
		ReceiverPubKey: &pb.PubKey{Bytes: w.PubKey},
	}
	txOutList = append(txOutList, txOut)
	if totalUtxoValue > value {
		txOut2 := &pb.TxOut{
			Value:          totalUtxoValue - value, // TODO: add miner fee
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

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	_, err := w.Client.UploadTx(ctx, tx)
	if err != nil {
		log.Panicf("failed to upload tx: %v", err)
	}
	log.Printf("successfully upload tx to miner (%s)", w.Miner)

	if !nowait {
		log.Printf("waiting for the tx to appear on the chain")
		txHash := bbc.Hash(tx)
		for {
			ctx, cancel = context.WithTimeout(context.Background(), rpcTimeout)
			findTxAns, err := w.Client.FindTx(ctx, pb.NewHashVal(txHash))
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
}

type walletArgs struct {
	Config string   `short:"f" default:"./wallet.json"`
	Genkey struct{} `cmd:""`
	Chain  struct {
		Height int64 `short:"l" default:"-1"`
	} `cmd:""`
	Show struct {
		Verbose bool `short:"v"`
	} `cmd:""`
	Transfer struct {
		To     string `short:"t" required:"true"`
		Nowait bool
		Fee    uint64 `default:"0"`
		Value  uint64 `arg:""`
	} `cmd:""`
}

func main() {
	var args walletArgs
	ctx := kong.Parse(&args)

	confBytes, err := os.ReadFile(args.Config)
	if err != nil {
		log.Fatalf("failed to open config: %v", err)
	}

	var conf WalletConfig
	err = json.Unmarshal(confBytes, &conf)
	if err != nil {
		log.Fatalf("failed to parse conf: %v", err)
	}

	switch ctx.Command() {
	case "genkey":
		pk, sk := bbc.GenKey()
		fmt.Printf("{\n    \"pubKey\": \"%x\",\n    \"privKey\": \"%x\"\n}", pk, sk)
	case "chain":
		wallet := NewWallet(&conf)
		defer wallet.Close()
		wallet.CmdChain(args.Chain.Height)
	case "show":
		wallet := NewWallet(&conf)
		defer wallet.Close()
		wallet.CmdShow(args.Show.Verbose)
	case "transfer <value>":
		wallet := NewWallet(&conf)
		defer wallet.Close()
		wallet.CmdTransfer(args.Transfer.To, args.Transfer.Value, args.Transfer.Fee, args.Transfer.Nowait)
	default:
		log.Panicf("unknown command '%s'", ctx.Command())
	}
}
