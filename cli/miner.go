package main

import (
	"github.com/SharzyL/bbc/bbc"
	"github.com/jessevdk/go-flags"
	"os"
)

func main() {
	var opts struct {
		SelfAddr  string   `short:"a" long:"addr" required:"true"`
		PeersAddr []string `short:"p" long:"peer"`
	}
	_, err := flags.Parse(&opts)
	if err != nil {
		if flags.WroteHelp(err) {
			return
		} else {
			os.Exit(1)
		}
	}
	miner := bbc.NewMiner(opts.SelfAddr, opts.PeersAddr)
	miner.MainLoop()
}
