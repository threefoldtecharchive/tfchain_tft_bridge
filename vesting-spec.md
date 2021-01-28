# Vesting on parity

## Goal

We want to put a lock on an account's funds for a duration defined by us.

Tokens can unlock gradually if the price of TFT rises and is above a treshold.

**Current month and previous months unlock if:** 

```
TFT Price > month * 0,5 % + 0.20
```

For example. A user wants to vest 1200 tokens for 1 year, after 1 year the vested tokens unlock again. 
Let's say the TFT price at the start of the vesting period is 0.10 dollar, if for example in the second month while the user is vesting, the TFT price rises to 0.22 dollar then month 1 and month 2 vested token unlock and the user can withdraw these token.

TFT Price at month 2: 

```
0.22 > 2 * 0.5 % + 0.20 = 0.21 
```

## Technical implementation

1. We need another Substrate Pallet that extends the user's balance object with a vested amount of tokens. Inspiration https://github.com/paritytech/substrate/tree/2d597fc2a2ccbeae0e5b832b976d2ca9558fc2c7/frame/vesting. This pallet should also expose methods to `vest` and `unvest`. The vesting pallet should check every X blocks if there are balances that needs to be unlocked. This can be done with an offchain worker.

2. We need a price oracle that fetches the price of the TFT tokens and stores it so the vesting pallet can read this price to calculate wether or not some vested balances should unlock. Work has been done already here: https://github.com/threefoldtech/substrate-pallet-tft-price. But it needs to also store an average of a certain period X so a spike in price will not cause the vesting pallet to unlock everything whilst it should not. Rather it should rely on a calculated average price (eg. 5min).
