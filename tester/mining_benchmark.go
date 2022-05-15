package main

import (
	"fmt"
	"github.com/jessevdk/go-flags"
	"go.uber.org/atomic"
	"math/rand"
	"os"
	"time"

	"github.com/SharzyL/bbc/bbc"
	"github.com/SharzyL/bbc/bbc/pb"
)

func randHash() *pb.HashVal {
	h := [bbc.NounceLen]byte{}
	rand.Read(h[:])
	return pb.NewHashVal(h[:])
}

func main() {
	var opts struct {
		Prefix int `short:"n" long:"len" required:"true"`
	}
	_, err := flags.Parse(&opts)
	if err != nil {
		if flags.WroteHelp(err) {
			return
		} else {
			os.Exit(1)
		}
	}
	header := pb.BlockHeader{
		PrevHash:    randHash(),
		MerkleRoot:  randHash(),
		Timestamp:   time.Now().UnixMilli(),
		Height:      0,
		BlockNounce: make([]byte, bbc.NounceLen),
	}
	startTime := time.Now()
	bbc.Mine(&header, atomic.NewBool(false), opts.Prefix)
	miningTime := time.Now().Sub(startTime)

	fmt.Printf("Mining a block after %d ms: %x\n", miningTime.Milliseconds(), bbc.Hash(&header))
}
