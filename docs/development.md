# Development

In this document we will explain how you can setup a local instance of the bridge and develop against it.

## Installing

See [installing](./intall.md)

## Running a local instance

The local instance will consist of a connection between a tfchain that runs in development mode and Stellar Testnet.

### Step 1: Run tfchain

See [tfchain](https://github.com/threefoldtech/tfchain/blob/development/docs/development/development.md)

### Step 2: Run bridge daemons

In this example we generated 2 keypairs using this [tool](https://github.com/threefoldfoundation/tft/tree/main/bsc/bridges/stellar/utils)

```sh
➜ go run main.go generate --count 2
generating 2 accounts and setting account options ...

Bridge master address: GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6
Bridge master secret: SDUHCIXQGCPQ2535FM2QK6KX43XH7RLCJ33RGYOKUYLA7E5W7LPBNEV7

Other signer address: GBFJM4HM4O7LTR2XLIIJMFPETKRQSRV6BF65DAUNR43WFE325FTLCURQ
Other signer secret: SCMEF63H2BA7WZDDBNGUP6YSGVW3O356CFOE4SHEGLQX3TXULEGEIXSG
```

The bridge master address is the address that will essentially vault the TFT. The other signer addresses / secrets are used to perform multisig on the master bridge wallet address. Running the above script will set all the generated signer addresses as a signer on the bridge master address.

Following predefined tfchain keys can be used to start a bridge daemon:

- `quarter between satisfy three sphere six soda boss cute decade old trend`
- `employ split promote annual couple elder remain cricket company fitness senior fiscal`

Change directory to the tfchain_bridge daemon (Make sure tfchain is running on your host)

```
cd tfchain_bridge
```

Now open a first terminal pane and execute:

```sh
./tfchain_bridge --secret SDUHCIXQGCPQ2535FM2QK6KX43XH7RLCJ33RGYOKUYLA7E5W7LPBNEV7 --tfchainurl ws://localhost:9944 --tfchainseed "quarter between satisfy three sphere six soda boss cute decade old trend" --bridgewallet GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6 --persistency ./signer1.json --network testnet
```

See we use the bridge master secret and wallet generated above in the run command.

Now Open a second terminal pane and execute:

```sh
./tfchain_bridge --secret SCMEF63H2BA7WZDDBNGUP6YSGVW3O356CFOE4SHEGLQX3TXULEGEIXSG --tfchainurl ws://localhost:9944 --tfchainseed "employ split promote annual couple elder remain cricket company fitness senior fiscal" --bridgewallet GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6 --persistency ./signer2.json --network testnet
```

See we used the second generated steller secret but we still use the bridge master address as bridge wallet address.

If all goes well, you should see something similar to following output:

```sh
5:25PM INF required signature count 2
5:25PM INF account GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6 loaded with sequence number 4328279062347778
5:25PM INF starting stellar subscription...
5:25PM INF starting tfchain subscription...
5:25PM INF awaiting signal
5:25PM INF fetching stellar transactions account=GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6 cursor=0 horizon=https://horizon-testnet.stellar.org/
5:25PM INF fetching events for blockheight ID=6
5:25PM INF received transaction on bridge stellar account hash=7a8c181f5e738ffeb68dda6518adf3ce4cf99777a4bd98a43dfed38ca0f99912
5:25PM INF received transaction on bridge stellar account hash=a8609616ac84f57eeadae8e6cde88025d0ab2ecbe8f1c70c7162b7548f20ae9a
5:25PM INF received transaction on bridge stellar account hash=a2eef986828038570a56314801626ee53e141408f0fc3c3eb96cca62fae81436
5:25PM INF fetching stellar transactions account=GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6 cursor=4328296242225152 horizon=https://horizon-testnet.stellar.org/
5:25PM INF fetching events for blockheight ID=7
5:25PM INF fetching events for blockheight ID=8
5:25PM INF fetching stellar transactions account=GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6 cursor=4328296242225152 horizon=https://horizon-testnet.stellar.org/
5:25PM INF fetching events for blockheight ID=9
5:25PM INF fetching stellar transactions account=GAYJSBPBQ3J32CZZ72OM3GZP646KSVD3V5QB3WBJSSGPYHYS5MZSS4Z6 cursor=4328296242225152 horizon=https://horizon-testnet.stellar.org/
5:25PM INF fetching events for blockheight ID=10
5:25PM INF fetching events for blockheight ID=11
```

# Step 3: Deposit some TFT

TODO
