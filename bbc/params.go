package bbc

import "time"

const newBlockTime = 5 * time.Second

const blockLimit = 128

const advertiseTimeout = 20 * time.Second
const peekChainDefaultLimit = int64(10)

const defaultMiningDifficulty = 23 // mining difficulty at the first block
const miningDifficultyBlocks = 10
const expectedMiningTime = 10 * time.Second

const DefaultRpcTimeout = 3 * time.Second
