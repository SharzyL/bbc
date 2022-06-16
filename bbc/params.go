package bbc

import "time"

// creating new block
const newBlockTime = 5 * time.Second
const blockLimit = 128
const maxTimeErrorAllowed = 2 * time.Second

// advertisement
const peerDeadTimeout = 5 * time.Second   // if advertise failing lasted so long, regard the peer as dead
const advertiseInterval = 1 * time.Second // the minimum interval between advertise to the same peer

// respond PeekChain
const peekChainDefaultLimit = int64(10)

// transaction fee
const feePerTx = uint64(1)

// mining
const defaultMiningDifficulty = 24 // mining difficulty at the first block
const miningDifficultyBlocks = 10
const expectedMiningTime = 10 * time.Second

// general rpc config
const rpcTimeout = 10 * time.Second
