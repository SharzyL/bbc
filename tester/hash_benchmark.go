package main

import (
	"fmt"
	"github.com/jessevdk/go-flags"
	"math/bits"
	"math/rand"
	"os"
	"time"

	"github.com/schollz/progressbar/v3"

	"github.com/SharzyL/bbc/bbc"
	"github.com/SharzyL/bbc/bbc/pb"
)

func randHash() *pb.HashVal {
	h := [bbc.NounceLen]byte{}
	rand.Read(h[:])
	return pb.NewHashVal(h[:])
}

func hasLeadingZeros(b []byte, len int) bool {
	byteNum := len / 8
	remainingBits := len % 8
	for i := 0; i < byteNum; i++ {
		if b[i] != 0 {
			return false
		}
	}
	if remainingBits > 0 && bits.LeadingZeros8(b[byteNum]) < remainingBits {
		return false
	}
	return true
}

func Mine(h *pb.BlockHeader, prefixLen int) (success bool) {
	headerBytes := h.ToBytes()
	hasher := bbc.NewHashState()
	nounceStartIdx := len(headerBytes) - bbc.NounceLen

	sep := 100000
	bar := progressbar.Default(-1)
	for {
		for i := 0; i < sep; i++ {
			hasher.Reset()
			rand.Read(headerBytes[nounceStartIdx:])
			_, _ = hasher.Write(headerBytes)
			hashVal := hasher.Sum(nil)

			if hasLeadingZeros(hashVal, prefixLen) {
				h.BlockNounce = headerBytes[nounceStartIdx:]
				_ = bar.Close()
				return true
			}
		}
		_ = bar.Add(sep)
	}
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
	Mine(&header, opts.Prefix)
	miningTime := time.Now().Sub(startTime)

	fmt.Printf("Mining a block after %d ms: %x\n", miningTime.Milliseconds(), bbc.Hash(&header))
}
