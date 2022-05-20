package bbc

import (
	"fmt"
	"github.com/SharzyL/bbc/bbc/pb"
	"strings"
	"time"
)

func PrintBlock(b *pb.FullBlock, indent int) {
	PrintBlockHeader(b.Header, indent)
	for i, tx := range b.TxList {
		if tx.Valid {
			fmt.Printf("  Tx %d [%x]:\n", i, Hash(tx))
			PrintTx(tx, indent+2)
		}
	}
}

func PrintBlockHeader(h *pb.BlockHeader, indent int) {
	indentStr := strings.Repeat(" ", indent)
	fmt.Printf("%sHash:       %x\n", indentStr, Hash(h))
	fmt.Printf("%sPrevHash:   %x\n", indentStr, h.PrevHash.Bytes)
	fmt.Printf("%sMerkleRoot: %x\n", indentStr, h.MerkleRoot.Bytes)
	fmt.Printf("%sTimestamp:  %s\n", indentStr, time.UnixMilli(h.Timestamp).UTC())
	fmt.Printf("%sHeight:     %d\n", indentStr, h.Height)
}

func PrintTx(tx *pb.Tx, indent int) {
	indentStr := strings.Repeat(" ", indent)
	fmt.Printf("%s  Timestamp: %s\n", indentStr, time.UnixMilli(tx.Timestamp).UTC())
	for j, txin := range tx.TxInList {
		fmt.Printf("%s  TxIn %d:\n", indentStr, j)
		fmt.Printf("%s    PrevTx:     %x\n", indentStr, txin.PrevTx.Bytes)
		fmt.Printf("%s    PrevOutIdx: %d\n", indentStr, txin.PrevOutIdx)
	}
	for j, txout := range tx.TxOutList {
		fmt.Printf("%s  TxOut %d:\n", indentStr, j)
		fmt.Printf("%s    Value: %d\n", indentStr, txout.Value)
		fmt.Printf("%s    ReceiverPubKey: %x\n", indentStr, txout.ReceiverPubKey.Bytes)
	}
}

func PrintUtxo(utxo *pb.Utxo, indent int) {
	indentStr := strings.Repeat(" ", indent)
	fmt.Printf("%sValue: %d\n", indentStr, utxo.Value)
	fmt.Printf("%sTxHash: %x\n", indentStr, utxo.TxHash.Bytes)
	fmt.Printf("%sTxOutIdx: %d\n", indentStr, utxo.TxOutIdx)
	fmt.Printf("%sPubKey: %x\n", indentStr, utxo.PubKey.Bytes)
}
