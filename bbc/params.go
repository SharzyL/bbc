package bbc

import "time"

const newBlockTime = 5 * time.Second

const blockLimit = 16

const advertiseTimeout = 20 * time.Second
const rpcTimeout = 1 * time.Second
const peekChainDefaultLimit = int64(10)

const miningDifficulty = 24 // expected minig time is near 10s
