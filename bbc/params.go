package bbc

import "time"

// creating new block
const maxTimeErrorAllowed = 2 * time.Second

// advertisement
const peerDeadTimeout = 10 * time.Second  // if advertise failing lasted so long, regard the peer as dead
const advertiseInterval = 3 * time.Second // the minimum interval between advertise to the same peer

// respond PeekChain
const peekChainDefaultLimit = int64(10)

// transaction fee
const feePerTx = uint64(1)

// mining
const defaultMiningDifficulty = 25 // mining difficulty at the first block
const miningDifficultyBlocks = 10  // recalculate difficulty accroding to recent x blocks
const expectedMiningTime = 10 * time.Second

// general rpc config
const rpcTimeout = 3 * time.Second
const longRpcTimeout = 15 * time.Second
