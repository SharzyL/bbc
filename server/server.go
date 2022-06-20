package main

import (
	"encoding/hex"
	"encoding/json"
	"github.com/alecthomas/kong"
	"log"
	"os"

	"github.com/SharzyL/bbc/bbc"
)

func main() {
	var opts struct {
		Key    string   `short:"k" required:"true"`
		Listen string   `short:"l" default:"0.0.0.0:30001"`
		Addr   string   `short:"a"`
		Peer   []string `short:"p"`

		Storage string `short:"s" default:".bbc_storage"`
		Clean   bool

		Loglevel string
		Verbose  bool
	}
	var keys struct {
		PubKey  string `json:"pubKey"`
		PrivKey string `json:"privKey"`
	}

	_ = kong.Parse(&opts)
	keyBytes, err := os.ReadFile(opts.Key)
	if err != nil {
		log.Panicf("cannot open key file: %v", err)
	}
	err = json.Unmarshal(keyBytes, &keys)
	if err != nil {
		log.Panicf("cannot parse key file: %v", err)
	}

	pubKey, err := hex.DecodeString(keys.PubKey)
	if err != nil {
		log.Panicf("canont parse pubkey '%s': %v", keys.PubKey, err)
	}
	privKey, err := hex.DecodeString(keys.PrivKey)
	if err != nil {
		log.Panicf("canont parse privkey '%s': %v", keys.PrivKey, err)
	}

	if opts.Verbose {
		opts.Loglevel = "DEBUG"
	}

	if len(opts.Addr) == 0 {
		opts.Addr = opts.Listen
	}

	minerOpts := &bbc.MinerOptions{
		PubKey:       pubKey,
		PrivKey:      privKey,
		ListenAddr:   opts.Listen,
		SelfAddr:     opts.Addr,
		PeerAddrList: opts.Peer,
		Loglevel:     opts.Loglevel,
		StorageDir:   opts.Storage,
	}
	miner := bbc.NewMiner(minerOpts)
	if opts.Clean {
		miner.CleanDiskBlocks()
	} else {
		miner.LoadBlocksFromDisk()
	}

	miner.MainLoop()
}
