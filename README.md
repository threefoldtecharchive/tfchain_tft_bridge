# Tfchain TFT bridge

Bridge TFT between a TFchain and the Stellar network.

There are 3 components that make up this bridge:

- A pallet that needs to be included in the tfchain runtime: [pallet-tft-bridge](./pallet-tft-bridge)
- Bridge daemons: [tfchain_bridge](./tfchain_bridge)
- A UI that simplifies interacting with bridge: [frontend](./tfchain_bridge/frontend)

## deployments

This bridge is deployed for tfchain testnet and tfchain mainnet.

The UI is also publicly available for the testnet network:

- [testnet bridge UI](https://bridge.test.grid.tf/)
