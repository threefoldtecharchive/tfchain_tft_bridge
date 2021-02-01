# Vesting on parity

## Goal

We want to put a lock on an account's funds for a specific duration. The duration is always a positive number of months.

Tokens can unlock gradually if the price of TFT rises and is above a treshold.

**Current month and previous months unlock if:** 

```
TFT Price > (month * 0.5%) + 0.20
```

For example. A user wants to vest 1200 tokens for 1 year, after 1 year the vested tokens unlock again. In this case there are 12 vesting months where each month unlocks 100 tokens (1200 / 12).
Let's say the TFT price at the start of the vesting period is 0.10 dollar, if for example in the second month while the user is vesting, the TFT price rises to 0.22 dollar then month 1 and month 2 vested token unlock (2 * 100 = 200 tokens) and the user can spend these tokens again.

TFT Price at month 2: 

```
0.22 > (2 * 0.5%) + 0.20 = 0.21 
```

## Technical implementation

1. We need another Substrate Pallet that extends the user's balance object with a vested amount of tokens. Inspiration https://github.com/paritytech/substrate/tree/2d597fc2a2ccbeae0e5b832b976d2ca9558fc2c7/frame/vesting. This pallet should also expose methods to `vest` and `unvest`. The vesting pallet should check every X blocks if there are balances that needs to be unlocked. This can be done with an offchain worker.

2. We need a price oracle that fetches the price of the TFT tokens and stores it so the vesting pallet can read this price to calculate wether or not some vested balances should unlock. Work has been done already here: https://github.com/threefoldtech/substrate-pallet-tft-price. But it needs to also store an average of a certain period X so a spike in price will not cause the vesting pallet to unlock everything whilst it should not. Rather it should rely on a calculated average price (eg. 5min).

## Proposal

A user can choose whenever he wishes to unlock vested tokens. Every block a certain amount of tokens unlock, a user can choose to `unvest` all the tokens unlocked by X blocks. This process be automated by us, for example: `unvest` every month. If a user wishes to `unvest` twice every month, we should stil allow this. This will yield the same amount of tokens anyway.

Example:

**Vesting schedule**

- 1200 tokens
- 12 months
- 0.40 dollar TFT price

after 15 days since the start of the vesting duration, around 50 tokens are unlocked. A user can now claim 50 tokens or wait until the automated `unvest` is called by us (after 30 days for example). This gives us also the option to let the user not claim funds until the end of the vesting period! In this case we disable automatic `unvest` and let the user decide whenever he wishes to `unvest`.

Pros:

- a user can decide when to `unvest` unlockable tokens
- gives us the benifit that a tokens might be longer locked (if the user decides this)
- `unvest` is controlled by the user and not by us (reduces chance of failure and bugs).

Cons:

- multiple unlocks in a specific duration can costs some fees for the user (shouldn't be much anyway).