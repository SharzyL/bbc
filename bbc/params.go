package bbc

import "time"

const newBlockTime = 5 * time.Second

const blockLimit = 128

const advertiseTimeout = 20 * time.Second
const peekChainDefaultLimit = int64(10)

const miningDifficulty = 24 // expected minig time is near 10s

const DefaultRpcTimeout = 3 * time.Second
