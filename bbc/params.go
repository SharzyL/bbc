package bbc

import "time"

const avgMiningTime = 5 * time.Second
const newBlockTime = 5 * time.Second

const blockLimit = 16

const adverticeTimeout = 5 * time.Second
const rpcTimeout = 1 * time.Second
const peekChainDefaultLimit = int64(10)
