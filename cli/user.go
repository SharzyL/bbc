package main

import (
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
	miner := bbc.NewUser(opts.Servers)
	miner.MainLoop(opts.IntervalMs)
}
