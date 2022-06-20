# BBC

[![](https://tokei.rs/b1/github/SharzyL/bbc?category=code)](https://github.com/SharzyL/bbc)

bbc, which is Barebone BlockChain, or Basic Blockchain Consensus, or Beginner Blockchain Code, or Buy Bbc Coin, or Big Banana Coin, or whatever you like.

It looks usable.

## Usage

### Setup Miners

Prepare storage folder and keypair
```shell
mkdir .bbc_storage  # the folder to store blockchain data
mkdir conf # the folder to store configurations
go run wallet/wallet.go genkey > conf/miner1.json # generate a key pair for the node
```

Set up a server listening on 30001 port, it broadcast its own address as `miner1.example.com:30001`, and connect to peers at `miner2.example.com:30001` and `miner3.example3.com:30001`. The miner will store the mining reward in the address corresponding to the keypair in `conf/miner1.json`.
```shell
go run server/server.go --key conf/miner1.json \
  -l 0.0.0.0:30001 \
  -a miner1.example.com:30001 \
  -p miner2.example.com:30001 \
  -p miner3.example.com:30001
```

Run `go run tester/server.go -h` for other available options.

### Use Wallet

On your client, put the following content in `conf/wallet.json`

```json
{
    "pubKey": "f936778f7b3e00e2e47b416c316d57d472db490f80975e6f5410dd4e6d150536",
    "privKey": "d8258ada9378e2ca7d39ee9d18ab5ef5e6a35ae79c07df643fcdcca51334243cf936778f7b3e00e2e47b416c316d57d472db490f80975e6f5410dd4e6d150536",
    "miners": [
        "miner1.example:30001",
        "miner2.example:30001",
        "miner3.example:30001"
    ],
    "addr": {
        "miner1": "f936778f7b3e00e2e47b416c316d57d472db490f80975e6f5410dd4e6d150536",
        "miner2": "3a735e030663bda5f180ff273e6aca2a380a4a76d17a5956edd58c1683950483",
        "miner3": "46a967e84e759488e439644fe316c0c1be30976cc9489694f6e3d3f136827043"
    }
}
```

```shell
# query the balance of miner1
go run wallet/wallet.go balance -a miner1

# transfer 400 coins to miner2, with 5 coin of transaction fee sent to the miner
go run wallet/wallet.go transfer -to miner2 400 --fee 5
```

Run `go run wallet/wallet.go -h` for more available actions.
