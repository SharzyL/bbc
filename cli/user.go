package main

import (
	"encoding/hex"
	"github.com/SharzyL/bbc/bbc"
	"github.com/jessevdk/go-flags"
	"os"
)

func main() {
	var opts struct {
		IntervalMs int      `short:"f" long:"freq" default:"100"`
		Servers    []string `short:"p" long:"peer"`
	}
	_, err := flags.Parse(&opts)
	if err != nil {
		if flags.WroteHelp(err) {
			return
		} else {
			os.Exit(1)
		}
	}
	pubKey, _ := hex.DecodeString("e67af31affc28963b331eca5409e7d33b1c1d4b35aeb5b4db0c2be320095f81c")
	privKey, _ := hex.DecodeString("552d9e1e0250d975ff4b6129a5d1bf3f7dec9e85b20862af3eed4a1ffc542bd6e67af31affc28963b331eca5409e7d33b1c1d4b35aeb5b4db0c2be320095f81c")

	miner := bbc.NewUser(pubKey, privKey, opts.Servers)
	miner.MainLoop()
}
