# TF Chain Stellar Bridge

This document describes the functionality of the TF Chain <-> Stellar bridge in this repository.

## Components

There are 3 main components to get the bridge working.

- Central stellar bridge wallet (requires multisig in order to be secure)
- The `pallet-tft-bridge` (the runtime functionality)
- Validator daemons

Note: There must always be as many validator daemons as there are signers on the stellar bridge wallet account.

## Pallet TFT Bridge

Is the main runtime functionality, distributes consensus for minting / burning tokens on TF Chain and manages signature passaround for the validators in order to execute a multisig transaction on the central Stellar bridge wallet.

Contains following extrinsic to call:

#### Admin setup
- `add_validator` (root call for the admin to setup the bridge)
- `remove_validator` (root call for the admin to setup the bridge)
#### Stellar -> TF Chain (minting on TF Chain)
- `propose_mint_transaction(transaction, target, amount)`
- `vote_mint_transaction(transactionHash)`
#### TF Chain -> Stellar (burning on TF Chain)
- `propose_burn_transaction(transaction, target, amount)`
- `add_sig_burn_transaction(transactionHash, signature)`
#### User callable extrinsic
- `swap_to_stellar(target, amount)`

## Validator daemon

Is a signer of the multisig Stellar bridge wallet. This code does the following things:

- Monitors central Stellar bridge wallet for incoming transactions
- Monitors events on chain

More will be explained below

## Stellar swap to TF Chain flow

A user looks up the Stellar bridge wallet address for a swap to TF Chain. He then sends a transaction with some amount to the bridge wallet. 

Now the validator daemons monitoring the bridge wallet will execute a `propose_mint_transaction`. Only one of the validator daemons will get a success executing this transaction, the other daemons will get a failed execution because the transaction is already proposed by another daemon. With this failed execution they will create a `vote_mint_transaction`. This will vote that transaction is a valid transaction. The daemons know this is a valid transaction because they all are monitoring the bridge wallet.

If more then *(number of validators / 2) + 1* voted that this mint transaction is valid, then the chain will execute a `mint` on the target address for the specified amount of tokens.

## TF Chain swap to Stellar

A user will call `swap_to_stellar(optional target, amount)`. The validator daemons will pick up a an event containing information about the swap to stellar. The validators will call `propose_burn_transaction`. Only one of the validator daemons will get a success executing this transaction, the other daemons will get a failed execution because the transaction is already proposed by another daemon. With this failed execution they will retrieve the burn transaction from storage, format the XDR and sign it with their stellar key. The daemon will then extrapolate the signature and then they will create a `add_sign_burn_transaction` passing the signature.

If more then *(number of validators / 2) + 1* signatures are present on the burn transaction, the transaction is considered valid and the chain will emit an event that the transaction is ready to be submitted.

The daemons will see this transaction ready event and retrieve the transcation object from storage. They will submit the transaction, along with all the signatures to the stellar network. Only one validator will succeed in this operation, the other validators will get an error, but they can ignore that because stellar has a double spend protection mechanism.