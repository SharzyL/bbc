package bbc

import "time"

// creating new block
const newBlockTime = 5 * time.Second
const blockLimit = 128

// advertisement
const advertiseTimeout = 20 * time.Second

// respond PeekChain
const peekChainDefaultLimit = int64(10)

// mining
const defaultMiningDifficulty = 23 // mining difficulty at the first block
const miningDifficultyBlocks = 10
const expectedMiningTime = 10 * time.Second

// general rpc config
const rpcTimeout = 3 * time.Second
