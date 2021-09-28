# TFT Tfchain Stellar bridge

## Stellar TFT to Tfchain TFT

The bridge monitors a central stellar account that is goverened by the threefoldfoundation. When a user sends an amount of TFT to that stellar account, the bridge will pick up this transaction.

### Destination

In the memo text of this transaction is the Tfchain address of the receiver.

**TODO: how to convert/encode an SS58 address so it fits in the Stellar memo text.**
