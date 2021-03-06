package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/alecthomas/kong"
	"google.golang.org/grpc"

	"github.com/SharzyL/bbc/bbc"
	"github.com/SharzyL/bbc/bbc/pb"
)

const rpcTimeout = 3 * time.Second

type Wallet struct {
	PubKey  []byte
	PrivKey []byte

	DefaultServer string
	Miners        []string
	Addr          map[string][]byte // i.e. pubkey

	Conns   []*grpc.ClientConn
	Clients []pb.MinerClient
}

type WalletConfig struct {
	PubKey  string            `json:"pubKey"`
	PrivKey string            `json:"privKey"`
	Miners  []string          `json:"miners"` // must be nonempty, first one is the default miner
	Addr    map[string]string `json:"addr"`
}

func NewWallet(conf *WalletConfig, server string) *Wallet {
	pubKey, err := hex.DecodeString(conf.PubKey)
	if err != nil {
		log.Panicf("canont parse pubkey '%s': %v", conf.PubKey, err)
	}
	if len(pubKey) != bbc.PubKeyLen {
		log.Panicf("incorrect pubkey len, expected %d, actual %d", bbc.PubKeyLen, len(pubKey))
	}
	privKey, err := hex.DecodeString(conf.PrivKey)
	if err != nil {
		log.Panicf("canont parse privkey '%s': %v", conf.PrivKey, err)
	}
	if len(privKey) != bbc.PrivKeyLen {
		log.Panicf("incorrect pubkey len, expected %d, actual %d", bbc.PrivKeyLen, len(privKey))
	}

	var defaultServer string
	if len(server) != 0 {
		defaultServer = server
	} else if len(conf.Miners) != 0 {
		defaultServer = conf.Miners[0]
	} else {
		log.Panicf("no miner in config and no default server specified")
	}

	addr := make(map[string][]byte)
	for name, pubKey := range conf.Addr {
		binPubKey, _ := hex.DecodeString(pubKey)
		addr[name] = binPubKey
	}
	return &Wallet{
		PubKey:        pubKey,
		PrivKey:       privKey,
		DefaultServer: defaultServer,
		Miners:        conf.Miners,
		Addr:          addr,
		Conns:         []*grpc.ClientConn{},
		Clients:       []pb.MinerClient{},
	}
}

func (w *Wallet) ConnectOne() {
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, w.DefaultServer, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(200*1024*1024))) // 200MB block at most
	if err != nil {
		log.Panicf("cannot dial %s: %v", w.Miners[0], err)
	}
	w.Conns = append(w.Conns, conn)
	w.Clients = append(w.Clients, pb.NewMinerClient(conn))
}

func (w *Wallet) ConnectAll() {
	mtx := sync.Mutex{}
	wg := sync.WaitGroup{}
	wg.Add(len(w.Miners))
	for _, miner := range w.Miners {
		go func(miner string) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			defer cancel()
			conn, err := grpc.DialContext(ctx, miner, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				log.Panicf("cannot dial %s: %v", miner, err)
			}

			mtx.Lock()
			defer mtx.Unlock()
			w.Conns = append(w.Conns, conn)
			w.Clients = append(w.Clients, pb.NewMinerClient(conn))
		}(miner)
	}
	wg.Wait()
}

func (w *Wallet) Close() {
	for _, conn := range w.Conns {
		if conn != nil {
			_ = conn.Close()
		}
	}
}

func (w *Wallet) GetUtxo(pubKey []byte) []*pb.Utxo {
	if pubKey == nil {
		pubKey = w.PubKey
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	lookupAns, err := w.Clients[0].LookupUtxo(ctx, &pb.PubKey{Bytes: pubKey})
	if err != nil {
		log.Fatalf("cannot lookup utxo: %v", err)
	}
	return lookupAns.UtxoList
}

func (w *Wallet) CmdStatus() {
	w.ConnectOne()
	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	defer cancel()
	ans, err := w.Clients[0].GetStatus(ctx, &pb.GetStatusReq{})
	if err != nil {
		log.Fatalf("failed to GetStatus: %v", err)
	}
	fmt.Print(ans.Description)
}

func (w *Wallet) CmdChain(height int64) {
	w.ConnectOne()
	var hash *pb.HashVal
	if height < 0 {
		ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
		defer cancel()
		peekResp, err := w.Clients[0].PeekChain(ctx, &pb.PeekChainReq{TopHash: nil, Limit: nil})
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
		peekResp, err := w.Clients[0].PeekChainByHeight(ctx, &pb.PeekChainByHeightReq{Height: height})
		if err != nil {
			log.Fatalf("failed to PeekChain: %v", err)
		}
		if peekResp.Header == nil {
			log.Fatalf("too high")
		}
		hash = pb.NewHashVal(bbc.Hash(peekResp.Header))
	}

	ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
	block, err := w.Clients[0].GetFullBlock(ctx, hash)
	defer cancel()
	if err != nil {
		log.Fatalf("failed to GetFullBlock: %v", err)
	}
	bbc.PrintBlock(block, 0, os.Stdout)
}

func (w *Wallet) CmdBalance(showUtxo bool, account string) {
	w.ConnectOne()
	var pubKey []byte
	if len(account) == 0 {
		pubKey = w.PubKey
	} else {
		pubKey = w.Addr[account]
		if pubKey == nil { // when not finding acount as known accounts, parse it as hex string
			if len(account) != bbc.PubKeyLen {
				log.Fatalf("incorret pubkey length (expected %d, actual %d)", bbc.PubKeyLen, len(pubKey))
			}
			var err error
			pubKey, err = hex.DecodeString(account)
			if err != nil {
				log.Fatalf("cannot parse toAddr '%s': %v", account, err)
			}
		}
	}

	fmt.Printf("pubkey: %x\n", pubKey)
	utxoList := w.GetUtxo(pubKey)
	balance := uint64(0)
	for i, utxo := range utxoList {
		if showUtxo {
			fmt.Printf("Utxo %d:\n", i)
			bbc.PrintUtxo(utxo, 2, os.Stdout)
		}
		balance += utxo.Value
	}
	fmt.Printf("current balance: %d\n", balance)
}

func (w *Wallet) CmdTransfer(toAddr string, value uint64, fee uint64, nowait bool) {
	w.ConnectAll()
	utxoList := w.GetUtxo(nil)
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
	inputValue := value + fee
	totalUtxoValue := uint64(0)
	for _, utxo := range utxoList {
		totalUtxoValue += utxo.Value
		txIn := &pb.TxIn{
			PrevTx:     utxo.TxHash,
			PrevOutIdx: utxo.TxOutIdx,
			Sig:        nil,
		}
		txIn.Sig = pb.NewSigVal(bbc.Sign(txIn, w.PrivKey))
		txInList = append(txInList, txIn)
		if totalUtxoValue >= inputValue {
			break
		}
	}
	if totalUtxoValue < inputValue {
		log.Panicf("balance not enough")
	}
	txOut := &pb.TxOut{
		Value:          value,
		ReceiverPubKey: &pb.PubKey{Bytes: addr},
	}
	txOutList = append(txOutList, txOut)
	if totalUtxoValue > value {
		txOut2 := &pb.TxOut{
			Value:          totalUtxoValue - inputValue, // TODO: add miner fee
			ReceiverPubKey: &pb.PubKey{Bytes: w.PubKey},
		}
		txOutList = append(txOutList, txOut2)
	}
	tx := &pb.Tx{
		Valid:     true,
		TxInList:  txInList,
		TxOutList: txOutList,
		Timestamp: time.Now().UnixMilli(),
	}

	wg := sync.WaitGroup{}
	wg.Add(len(w.Clients))
	for i, client := range w.Clients {
		go func(i int, client pb.MinerClient) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			_, err := client.UploadTx(ctx, tx)
			cancel()
			if err != nil {
				log.Panicf("failed to upload tx: %v", err)
			}
			log.Printf("successfully upload tx to miner (%s)", w.Miners[i])
		}(i, client)
	}
	wg.Wait()

	if !nowait {
		log.Printf("waiting for the tx to appear on the chain")
		txHash := bbc.Hash(tx)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), rpcTimeout)
			findTxAns, err := w.Clients[0].FindTx(ctx, pb.NewHashVal(txHash))
			cancel()
			if err != nil {
				log.Panicf("failed to findTX: %v", err)
			}

			if findTxAns.BlockHeader != nil {
				log.Printf("detect the tx in chain on (%s)", w.Miners[0])
				bbc.PrintBlockHeader(findTxAns.BlockHeader, 0, os.Stdout)
				return
			}
			time.Sleep(time.Second)
		}
	}
}

type walletArgs struct {
	Config string `short:"f" default:"conf/wallet.json" help:"conf file location, should be a json file"`
	Server string `short:"s" default:"" help:"default server to connected to"`

	// all commands
	Status struct{} `cmd:"" help:"get server status"`
	Genkey struct{} `cmd:"" help:"generate key file"`
	Chain  struct {
		Height int64 `short:"l" default:"-1"`
	} `cmd:"" help:"inspect main chain on server"`
	Balance struct {
		Utxo    bool   `short:"u" help:"print utxo list"`
		Account string `short:"a" help:"addr of account"`
	} `cmd:"" help:"query balance"`
	Transfer struct {
		To     string `short:"t" required:"true" help:"the name or addr of recipient"`
		Nowait bool   `help:"no waiting for the transaction appearing on mainchain"`
		Fee    uint64 `default:"0" help:"attach transaction fee"`
		Value  uint64 `arg:"" help:"amount of money to transfer, fee NOT included"`
	} `cmd:"" help:"transfer money"`
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

	if ctx.Command() == "genkey" {
		pk, sk := bbc.GenKey()
		fmt.Printf("{\n    \"pubKey\": \"%x\",\n    \"privKey\": \"%x\"\n}", pk, sk)
		return
	}

	wallet := NewWallet(&conf, args.Server)
	defer wallet.Close()

	switch ctx.Command() {
	case "status":
		wallet.CmdStatus()
	case "chain":
		wallet.CmdChain(args.Chain.Height)
	case "balance":
		wallet.CmdBalance(args.Balance.Utxo, args.Balance.Account)
	case "transfer <value>":
		wallet.CmdTransfer(args.Transfer.To, args.Transfer.Value, args.Transfer.Fee, args.Transfer.Nowait)
	default:
		log.Panicf("unknown command '%s'", ctx.Command())
	}
}
