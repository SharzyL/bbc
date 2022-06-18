package bbc

import (
	"fmt"
	"github.com/SharzyL/bbc/bbc/pb"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
	"strings"
	"time"
)

func GetLogger(level string) *zap.SugaredLogger {
	config := zap.NewDevelopmentConfig()

	loglevelStr := strings.ToUpper(level)
	if len(loglevelStr) == 0 {
		loglevelStr = os.Getenv("LOG_LEVEL")
		if len(loglevelStr) == 0 {
			loglevelStr = "INFO"
		}
	}

	var loglevel zapcore.Level
	switch loglevelStr {
	case "DEBUG":
		loglevel = zap.DebugLevel
	case "INFO":
		loglevel = zap.InfoLevel
	case "WARNING":
		loglevel = zap.WarnLevel
	case "ERROR":
		loglevel = zap.ErrorLevel
	case "DPANIC":
		loglevel = zap.DPanicLevel
	case "PANIC":
		loglevel = zap.PanicLevel
	case "FATAL":
		loglevel = zap.FatalLevel
	default:
		panic(fmt.Sprintf("unknown log level '%s'", loglevelStr))
	}

	config.Level.SetLevel(loglevel)
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	logger, _ := config.Build()
	logger.Info("start logging", zap.String("level", loglevelStr))
	return logger.Sugar()
}

func PrintBlock(b *pb.FullBlock, indent int, f io.Writer) {
	PrintBlockHeader(b.Header, indent, f)
	for i, tx := range b.TxList {
		if tx.Valid {
			_, _ = fmt.Fprintf(f, "  Tx %d [%x]:\n", i, Hash(tx))
			PrintTx(tx, indent+2, f)
		}
	}
}

func PrintBlockHeader(h *pb.BlockHeader, indent int, f io.Writer) {
	indentStr := strings.Repeat(" ", indent)
	_, _ = fmt.Fprintf(f, "%sHash:       %x\n", indentStr, Hash(h))
	_, _ = fmt.Fprintf(f, "%sPrevHash:   %x\n", indentStr, h.PrevHash.Bytes)
	_, _ = fmt.Fprintf(f, "%sMerkleRoot: %x\n", indentStr, h.MerkleRoot.Bytes)
	_, _ = fmt.Fprintf(f, "%sTimestamp:  %s\n", indentStr, time.UnixMilli(h.Timestamp).UTC())
	_, _ = fmt.Fprintf(f, "%sHeight:     %d\n", indentStr, h.Height)
	_, _ = fmt.Fprintf(f, "%sDifficulty: %d\n", indentStr, h.Difficulty)
}

func PrintTx(tx *pb.Tx, indent int, f io.Writer) {
	indentStr := strings.Repeat(" ", indent)
	_, _ = fmt.Fprintf(f, "%s  Timestamp: %s\n", indentStr, time.UnixMilli(tx.Timestamp).UTC())
	for j, txin := range tx.TxInList {
		_, _ = fmt.Printf("%s  TxIn %d:\n", indentStr, j)
		_, _ = fmt.Fprintf(f, "%s    PrevTx:     %x\n", indentStr, txin.PrevTx.Bytes)
		_, _ = fmt.Fprintf(f, "%s    PrevOutIdx: %d\n", indentStr, txin.PrevOutIdx)
	}
	for j, txout := range tx.TxOutList {
		_, _ = fmt.Fprintf(f, "%s  TxOut %d:\n", indentStr, j)
		_, _ = fmt.Fprintf(f, "%s    Value: %d\n", indentStr, txout.Value)
		_, _ = fmt.Fprintf(f, "%s    ReceiverPubKey: %x\n", indentStr, txout.ReceiverPubKey.Bytes)
	}
}

func PrintUtxo(utxo *pb.Utxo, indent int, f io.Writer) {
	indentStr := strings.Repeat(" ", indent)
	_, _ = fmt.Fprintf(f, "%sValue: %d\n", indentStr, utxo.Value)
	_, _ = fmt.Fprintf(f, "%sTxHash: %x\n", indentStr, utxo.TxHash.Bytes)
	_, _ = fmt.Fprintf(f, "%sTxOutIdx: %d\n", indentStr, utxo.TxOutIdx)
	_, _ = fmt.Fprintf(f, "%sPubKey: %x\n", indentStr, utxo.PubKey.Bytes)
}
