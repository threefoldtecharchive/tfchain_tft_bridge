#![cfg_attr(not(feature = "std"), no_std)]

/// Edit this file to define custom logic or remove it if it is not needed.
/// Learn more about FRAME and the core library of Substrate FRAME pallets:
/// https://substrate.dev/docs/en/knowledgebase/runtime/frame
use sp_std::prelude::*;

use codec::{Decode, Encode};
use frame_support::dispatch::DispatchErrorWithPostInfo;
use frame_support::{
    ensure, log,
    traits::{Currency, EnsureOrigin, OnUnbalanced, ReservableCurrency},
};
use frame_system::{self as system, ensure_signed};
use scale_info::TypeInfo;
use sp_runtime::SaturatedConversion;

// Re-export pallet items so that they can be accessed from the crate namespace.
pub use pallet::*;

#[cfg(test)]
mod mock;

#[cfg(test)]
mod tests;

// MintTransaction contains all the information about
// Stellar -> TF Chain minting transaction.
// if the votes field is larger then (number of validators / 2) + 1 , the transaction will be minted
#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug, TypeInfo)]
pub struct MintTransaction<AccountId, BlockNumber> {
    pub amount: u64,
    pub target: AccountId,
    pub block: BlockNumber,
    pub votes: u32,
}

// BurnTransaction contains all the information about
// TF Chain -> Stellar burn transaction
// Transaction is ready when (number of validators / 2) + 1 signatures are present
#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug, TypeInfo)]
pub struct BurnTransaction<BlockNumber> {
    pub block: BlockNumber,
    pub amount: u64,
    pub target: Vec<u8>,
    pub signatures: Vec<StellarSignature>,
    pub sequence_number: u64,
}

#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug, TypeInfo)]
pub struct RefundTransaction<BlockNumber> {
    pub block: BlockNumber,
    pub amount: u64,
    pub target: Vec<u8>,
    pub tx_hash: Vec<u8>,
    pub signatures: Vec<StellarSignature>,
    pub sequence_number: u64,
}

#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug, TypeInfo)]
pub struct StellarSignature {
    pub signature: Vec<u8>,
    pub stellar_pub_key: Vec<u8>,
}

// Definition of the pallet logic, to be aggregated at runtime definition
// through `construct_runtime`.
#[frame_support::pallet]
pub mod pallet {
    use super::*;
    use frame_support::pallet_prelude::*;
    use frame_system::pallet_prelude::*;

    // balance type using reservable currency type
    pub type BalanceOf<T> =
        <<T as Config>::Currency as Currency<<T as system::Config>::AccountId>>::Balance;
    pub type NegativeImbalanceOf<T> =
        <<T as Config>::Currency as Currency<<T as system::Config>::AccountId>>::NegativeImbalance;

    #[pallet::pallet]
    #[pallet::generate_store(pub(super) trait Store)]
    #[pallet::without_storage_info]
    pub struct Pallet<T>(_);

    #[pallet::storage]
    #[pallet::getter(fn validator_accounts)]
    pub type Validators<T: Config> = StorageValue<_, Vec<T::AccountId>, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn fee_account)]
    pub type FeeAccount<T: Config> = StorageValue<_, T::AccountId, OptionQuery>;

    #[pallet::storage]
    #[pallet::getter(fn mint_transactions)]
    pub type MintTransactions<T: Config> = StorageMap<
        _,
        Blake2_128Concat,
        Vec<u8>,
        MintTransaction<T::AccountId, T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn executed_mint_transactions)]
    pub type ExecutedMintTransactions<T: Config> = StorageMap<
        _,
        Blake2_128Concat,
        Vec<u8>,
        MintTransaction<T::AccountId, T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn burn_transactions)]
    pub type BurnTransactions<T: Config> =
        StorageMap<_, Blake2_128Concat, u64, BurnTransaction<T::BlockNumber>, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn executed_burn_transactions)]
    pub type ExecutedBurnTransactions<T: Config> =
        StorageMap<_, Blake2_128Concat, u64, BurnTransaction<T::BlockNumber>, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn refund_transactions)]
    pub type RefundTransactions<T: Config> =
        StorageMap<_, Blake2_128Concat, Vec<u8>, RefundTransaction<T::BlockNumber>, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn executed_refund_transactions)]
    pub type ExecutedRefundTransactions<T: Config> =
        StorageMap<_, Blake2_128Concat, Vec<u8>, RefundTransaction<T::BlockNumber>, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn burn_transaction_id)]
    pub type BurnTransactionID<T: Config> = StorageValue<_, u64, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn withdraw_fee)]
    pub type WithdrawFee<T: Config> = StorageValue<_, u64, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn deposit_fee)]
    pub type DepositFee<T: Config> = StorageValue<_, u64, ValueQuery>;

    #[pallet::config]
    pub trait Config: frame_system::Config {
        type Event: From<Event<Self>> + IsType<<Self as frame_system::Config>::Event>;

        /// Currency type for this pallet.
        type Currency: Currency<Self::AccountId> + ReservableCurrency<Self::AccountId>;

        /// Handler for the unbalanced decrement when slashing (burning collateral)
        type Burn: OnUnbalanced<NegativeImbalanceOf<Self>>;

        /// Origin for restricted extrinsics
        /// Can be the root or another origin configured in the runtime
        type RestrictedOrigin: EnsureOrigin<Self::Origin>;

        // Retry interval for expired transactions
        type RetryInterval: Get<u32>;
    }

    #[pallet::event]
    #[pallet::generate_deposit(pub(super) fn deposit_event)]
    pub enum Event<T: Config> {
        // Minting events
        MintTransactionProposed(Vec<u8>, T::AccountId, u64),
        MintTransactionVoted(Vec<u8>),
        MintCompleted(MintTransaction<T::AccountId, T::BlockNumber>),
        MintTransactionExpired(Vec<u8>, u64, T::AccountId),
        // Burn events
        BurnTransactionCreated(u64, Vec<u8>, u64),
        BurnTransactionProposed(u64, Vec<u8>, u64),
        BurnTransactionSignatureAdded(u64, StellarSignature),
        BurnTransactionReady(u64),
        BurnTransactionProcessed(BurnTransaction<T::BlockNumber>),
        BurnTransactionExpired(u64, Vec<u8>, u64),
        // Refund events
        RefundTransactionCreated(Vec<u8>, Vec<u8>, u64),
        RefundTransactionsignatureAdded(Vec<u8>, StellarSignature),
        RefundTransactionReady(Vec<u8>),
        RefundTransactionProcessed(RefundTransaction<T::BlockNumber>),
        RefundTransactionExpired(Vec<u8>, Vec<u8>, u64),
    }

    #[pallet::error]
    pub enum Error<T> {
        ValidatorExists,
        ValidatorNotExists,
        TransactionValidatorExists,
        TransactionValidatorNotExists,
        MintTransactionExists,
        MintTransactionAlreadyExecuted,
        MintTransactionNotExists,
        BurnTransactionExists,
        BurnTransactionNotExists,
        BurnSignatureExists,
        EnoughBurnSignaturesPresent,
        RefundSignatureExists,
        BurnTransactionAlreadyExecuted,
        RefundTransactionNotExists,
        RefundTransactionAlreadyExecuted,
        EnoughRefundSignaturesPresent,
        NotEnoughBalanceToSwap,
        AmountIsLessThanWithdrawFee,
        AmountIsLessThanDepositFee,
        WrongParametersProvided,
    }

    #[pallet::genesis_config]
    pub struct GenesisConfig<T: Config> {
        pub validator_accounts: Option<Vec<T::AccountId>>,
        pub fee_account: Option<T::AccountId>,
        pub withdraw_fee: u64,
        pub deposit_fee: u64,
    }

    // The default value for the genesis config type.
    #[cfg(feature = "std")]
    impl<T: Config> Default for GenesisConfig<T> {
        fn default() -> Self {
            Self {
                validator_accounts: None,
                fee_account: None,
                withdraw_fee: Default::default(),
                deposit_fee: Default::default(),
            }
        }
    }

    #[pallet::genesis_build]
    impl<T: Config> GenesisBuild<T> for GenesisConfig<T> {
        fn build(&self) {
            if let Some(validator_accounts) = &self.validator_accounts {
                Validators::<T>::put(validator_accounts);
            }

            if let Some(ref fee_account) = self.fee_account {
                FeeAccount::<T>::put(fee_account);
            }
            WithdrawFee::<T>::put(self.withdraw_fee);
            DepositFee::<T>::put(self.deposit_fee)
        }
    }

    #[pallet::hooks]
    impl<T: Config> Hooks<BlockNumberFor<T>> for Pallet<T> {
        fn on_finalize(block: T::BlockNumber) {
            let current_block_u64: u64 = block.saturated_into::<u64>();

            for (tx_id, mut tx) in BurnTransactions::<T>::iter() {
                let tx_block_u64: u64 = tx.block.saturated_into::<u64>();
                // if x blocks have passed since the tx got submitted
                // we can safely assume this tx is fault
                // add the faulty tx to the expired tx list
                if current_block_u64 - tx_block_u64 >= T::RetryInterval::get().into() {
                    // reset signatures and sequence number
                    tx.signatures = Vec::new();
                    tx.sequence_number = 0;
                    tx.block = block;

                    // update tx in storage
                    BurnTransactions::<T>::insert(&tx_id, &tx);

                    // Emit event
                    Self::deposit_event(Event::BurnTransactionExpired(tx_id, tx.target, tx.amount));
                }
            }

            for (tx_id, mut tx) in RefundTransactions::<T>::iter() {
                let tx_block_u64: u64 = tx.block.saturated_into::<u64>();
                // if x blocks have passed since the tx got submitted
                // we can safely assume this tx is fault
                // add the faulty tx to the expired tx list
                if current_block_u64 - tx_block_u64 >= T::RetryInterval::get().into() {
                    // reset signatures and sequence number
                    tx.signatures = Vec::new();
                    tx.sequence_number = 0;
                    tx.block = block;

                    // update tx in storage
                    RefundTransactions::<T>::insert(&tx_id, &tx);

                    // Emit event
                    Self::deposit_event(Event::RefundTransactionExpired(
                        tx_id, tx.target, tx.amount,
                    ));
                }
            }
        }
    }

    #[pallet::call]
    impl<T: Config> Pallet<T> {
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn add_bridge_validator(
            origin: OriginFor<T>,
            target: T::AccountId,
        ) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            Self::add_validator_account(target)
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn remove_bridge_validator(
            origin: OriginFor<T>,
            target: T::AccountId,
        ) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            Self::remove_validator_account(target)
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn set_fee_account(
            origin: OriginFor<T>,
            target: T::AccountId,
        ) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            FeeAccount::<T>::set(Some(target));
            Ok(().into())
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn set_withdraw_fee(origin: OriginFor<T>, amount: u64) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            WithdrawFee::<T>::set(amount);
            Ok(().into())
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn set_deposit_fee(origin: OriginFor<T>, amount: u64) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            DepositFee::<T>::set(amount);
            Ok(().into())
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn swap_to_stellar(
            origin: OriginFor<T>,
            target_stellar_address: Vec<u8>,
            amount: BalanceOf<T>,
        ) -> DispatchResultWithPostInfo {
            let source = ensure_signed(origin)?;
            Self::burn_tft(source, target_stellar_address, amount)
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn propose_or_vote_mint_transaction(
            origin: OriginFor<T>,
            transaction: Vec<u8>,
            target: T::AccountId,
            amount: u64,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::propose_or_vote_stellar_mint_transaction(validator, transaction, target, amount)
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn propose_burn_transaction_or_add_sig(
            origin: OriginFor<T>,
            transaction_id: u64,
            target: Vec<u8>,
            amount: u64,
            signature: Vec<u8>,
            stellar_pub_key: Vec<u8>,
            sequence_number: u64,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::propose_stellar_burn_transaction_or_add_sig(
                validator,
                transaction_id,
                target,
                amount,
                signature,
                stellar_pub_key,
                sequence_number,
            )
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn set_burn_transaction_executed(
            origin: OriginFor<T>,
            transaction_id: u64,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::set_stellar_burn_transaction_executed(validator, transaction_id)
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn create_refund_transaction_or_add_sig(
            origin: OriginFor<T>,
            tx_hash: Vec<u8>,
            target: Vec<u8>,
            amount: u64,
            signature: Vec<u8>,
            stellar_pub_key: Vec<u8>,
            sequence_number: u64,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::create_stellar_refund_transaction_or_add_sig(
                validator,
                tx_hash,
                target,
                amount,
                signature,
                stellar_pub_key,
                sequence_number,
            )
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn set_refund_transaction_executed(
            origin: OriginFor<T>,
            tx_hash: Vec<u8>,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::set_stellar_refund_transaction_executed(validator, tx_hash)
        }

        #[pallet::weight(10_000 + T::DbWeight::get().writes(1))]
        pub fn purge_storage(origin: OriginFor<T>) -> DispatchResultWithPostInfo {
            let _validator = ensure_signed(origin)?;
            Self::purge_stellar_storage()
        }
    }
}

use frame_support::pallet_prelude::DispatchResultWithPostInfo;
// Internal functions of the pallet
impl<T: Config> Pallet<T> {
    pub fn mint_tft(
        tx_id: Vec<u8>,
        mut tx: MintTransaction<T::AccountId, T::BlockNumber>,
    ) -> DispatchResultWithPostInfo {
        let deposit_fee = DepositFee::<T>::get();
        ensure!(
            tx.amount > deposit_fee,
            Error::<T>::AmountIsLessThanDepositFee
        );

        // caculate amount - deposit fee
        let new_amount = tx.amount - deposit_fee;

        // transfer new amount to target
        let amount_as_balance = BalanceOf::<T>::saturated_from(new_amount);
        T::Currency::deposit_creating(&tx.target, amount_as_balance);
        // transfer deposit fee to fee wallet
        let deposit_fee_b = BalanceOf::<T>::saturated_from(deposit_fee);

        if let Some(fee_account) = FeeAccount::<T>::get() {
            T::Currency::deposit_creating(&fee_account, deposit_fee_b);
        }

        // Remove tx from storage
        MintTransactions::<T>::remove(tx_id.clone());
        // Insert into executed transactions
        let now = <system::Pallet<T>>::block_number();
        tx.block = now;
        ExecutedMintTransactions::<T>::insert(tx_id, &tx);

        Self::deposit_event(Event::MintCompleted(tx));

        Ok(().into())
    }

    pub fn burn_tft(
        source: T::AccountId,
        target_stellar_address: Vec<u8>,
        amount: BalanceOf<T>,
    ) -> DispatchResultWithPostInfo {
        let withdraw_fee = WithdrawFee::<T>::get();
        let withdraw_fee_b = BalanceOf::<T>::saturated_from(withdraw_fee);
        let free_balance: BalanceOf<T> = T::Currency::free_balance(&source);
        // Make sure the user wants to swap more than the burn fee
        ensure!(
            amount > withdraw_fee_b,
            Error::<T>::AmountIsLessThanWithdrawFee
        );

        // Make sure the user has enough balance to swap the amount
        ensure!(free_balance >= amount, Error::<T>::NotEnoughBalanceToSwap);
        // transfer amount - fee to target account
        let imbalance = T::Currency::slash(&source, amount).0;
        T::Burn::on_unbalanced(imbalance);

        // transfer withdraw fee to fee wallet
        let burn_fee_b = BalanceOf::<T>::saturated_from(withdraw_fee);
        if let Some(fee_account) = FeeAccount::<T>::get() {
            T::Currency::deposit_creating(&fee_account, burn_fee_b);
        }

        // increment burn transaction id
        let mut burn_id = BurnTransactionID::<T>::get();
        burn_id += 1;
        BurnTransactionID::<T>::put(burn_id);

        let burn_amount_as_u64 = amount.saturated_into::<u64>() - withdraw_fee;
        Self::deposit_event(Event::BurnTransactionCreated(
            burn_id,
            target_stellar_address.clone(),
            burn_amount_as_u64,
        ));

        // Create transaction with empty signatures
        let now = <frame_system::Pallet<T>>::block_number();
        let tx = BurnTransaction {
            block: now,
            amount: burn_amount_as_u64,
            target: target_stellar_address,
            signatures: Vec::new(),
            sequence_number: 0,
        };
        BurnTransactions::<T>::insert(burn_id, &tx);

        Ok(().into())
    }

    pub fn create_stellar_refund_transaction_or_add_sig(
        validator: T::AccountId,
        tx_hash: Vec<u8>,
        target: Vec<u8>,
        amount: u64,
        signature: Vec<u8>,
        stellar_pub_key: Vec<u8>,
        sequence_number: u64,
    ) -> DispatchResultWithPostInfo {
        Self::check_if_validator_exists(validator.clone())?;

        // make sure we don't duplicate the transaction
        // ensure!(!MintTransactions::<T>::contains_key(tx_id.clone()), Error::<T>::MintTransactionExists);
        if RefundTransactions::<T>::contains_key(tx_hash.clone()) {
            return Self::add_stellar_sig_refund_transaction(
                tx_hash.clone(),
                signature,
                stellar_pub_key,
                sequence_number,
            );
        }

        let now = <frame_system::Pallet<T>>::block_number();
        let tx = RefundTransaction {
            block: now,
            target: target.clone(),
            amount,
            tx_hash: tx_hash.clone(),
            signatures: Vec::new(),
            sequence_number,
        };
        RefundTransactions::<T>::insert(tx_hash.clone(), &tx);

        Self::add_stellar_sig_refund_transaction(
            tx_hash.clone(),
            signature,
            stellar_pub_key,
            sequence_number,
        )?;

        Self::deposit_event(Event::RefundTransactionCreated(
            tx_hash.clone(),
            target,
            amount,
        ));

        Ok(().into())
    }

    pub fn propose_or_vote_stellar_mint_transaction(
        validator: T::AccountId,
        tx_id: Vec<u8>,
        target: T::AccountId,
        amount: u64,
    ) -> DispatchResultWithPostInfo {
        Self::check_if_validator_exists(validator.clone())?;
        // check if it already has been executed in the past
        ensure!(
            !ExecutedMintTransactions::<T>::contains_key(tx_id.clone()),
            Error::<T>::MintTransactionAlreadyExecuted
        );
        // make sure we don't duplicate the transaction
        // ensure!(!MintTransactions::<T>::contains_key(tx_id.clone()), Error::<T>::MintTransactionExists);
        if MintTransactions::<T>::contains_key(tx_id.clone()) {
            return Self::vote_stellar_mint_transaction(tx_id);
        }

        let now = <frame_system::Pallet<T>>::block_number();
        let tx = MintTransaction {
            amount,
            target: target.clone(),
            block: now,
            votes: 0,
        };
        MintTransactions::<T>::insert(tx_id.clone(), &tx);

        // Vote already
        Self::vote_stellar_mint_transaction(tx_id.clone())?;

        Self::deposit_event(Event::MintTransactionProposed(tx_id, target, amount));

        Ok(().into())
    }

    pub fn vote_stellar_mint_transaction(tx_id: Vec<u8>) -> DispatchResultWithPostInfo {
        let mint_transaction = MintTransactions::<T>::get(tx_id.clone());
        match mint_transaction {
            Some(mut tx) => {
                // increment amount of votes
                tx.votes += 1;

                // deposit voted event
                Self::deposit_event(Event::MintTransactionVoted(tx_id.clone()));

                // update the transaction
                MintTransactions::<T>::insert(&tx_id, &tx);

                let validators = Validators::<T>::get();
                // If majority aggrees on the transaction, mint tokens to target address
                if tx.votes as usize >= (validators.len() / 2) + 1 {
                    log::info!("enough votes, minting transaction...");
                    Self::mint_tft(tx_id.clone(), tx)?;
                }
            }
            None => (),
        }

        Ok(().into())
    }

    pub fn propose_stellar_burn_transaction_or_add_sig(
        validator: T::AccountId,
        tx_id: u64,
        target: Vec<u8>,
        amount: u64,
        signature: Vec<u8>,
        stellar_pub_key: Vec<u8>,
        sequence_number: u64,
    ) -> DispatchResultWithPostInfo {
        Self::check_if_validator_exists(validator.clone())?;

        // check if it already has been executed in the past
        ensure!(
            !ExecutedBurnTransactions::<T>::contains_key(tx_id),
            Error::<T>::BurnTransactionAlreadyExecuted
        );

        let mut burn_tx = BurnTransactions::<T>::get(tx_id);
        ensure!(
            BurnTransactions::<T>::contains_key(tx_id),
            Error::<T>::BurnTransactionNotExists
        );

        ensure!(
            burn_tx.amount == amount,
            Error::<T>::WrongParametersProvided
        );
        ensure!(
            burn_tx.target == target,
            Error::<T>::WrongParametersProvided
        );

        if BurnTransactions::<T>::contains_key(tx_id) {
            return Self::add_stellar_sig_burn_transaction(
                tx_id,
                signature,
                stellar_pub_key,
                sequence_number,
            );
        }

        let now = <frame_system::Pallet<T>>::block_number();

        burn_tx.block = now;
        burn_tx.sequence_number = sequence_number;
        BurnTransactions::<T>::insert(tx_id.clone(), &burn_tx);

        Self::add_stellar_sig_burn_transaction(tx_id, signature, stellar_pub_key, sequence_number)?;

        Self::deposit_event(Event::BurnTransactionProposed(tx_id, target, amount));

        Ok(().into())
    }

    pub fn add_stellar_sig_burn_transaction(
        tx_id: u64,
        signature: Vec<u8>,
        stellar_pub_key: Vec<u8>,
        sequence_number: u64,
    ) -> DispatchResultWithPostInfo {
        let mut tx = BurnTransactions::<T>::get(&tx_id);

        let validators = Validators::<T>::get();
        if tx.signatures.len() == (validators.len() / 2) + 1 {
            return Err(DispatchErrorWithPostInfo::from(
                Error::<T>::EnoughBurnSignaturesPresent,
            ));
        }

        // check if the signature already exists
        ensure!(
            !tx.signatures
                .iter()
                .any(|sig| sig.stellar_pub_key == stellar_pub_key),
            Error::<T>::BurnSignatureExists
        );
        ensure!(
            !tx.signatures.iter().any(|sig| sig.signature == signature),
            Error::<T>::BurnSignatureExists
        );

        // add the signature
        let stellar_signature = StellarSignature {
            signature,
            stellar_pub_key,
        };

        tx.sequence_number = sequence_number;
        tx.signatures.push(stellar_signature.clone());
        BurnTransactions::<T>::insert(tx_id, &tx);
        Self::deposit_event(Event::BurnTransactionSignatureAdded(
            tx_id,
            stellar_signature,
        ));

        if tx.signatures.len() >= (validators.len() / 2) + 1 {
            Self::deposit_event(Event::BurnTransactionReady(tx_id));
            BurnTransactions::<T>::insert(tx_id, tx);
        }

        Ok(().into())
    }

    pub fn set_stellar_burn_transaction_executed(
        validator: T::AccountId,
        tx_id: u64,
    ) -> DispatchResultWithPostInfo {
        Self::check_if_validator_exists(validator)?;

        ensure!(
            !ExecutedBurnTransactions::<T>::contains_key(tx_id),
            Error::<T>::BurnTransactionAlreadyExecuted
        );
        ensure!(
            BurnTransactions::<T>::contains_key(tx_id),
            Error::<T>::BurnTransactionNotExists
        );

        let tx = BurnTransactions::<T>::get(tx_id);

        BurnTransactions::<T>::remove(tx_id);
        ExecutedBurnTransactions::<T>::insert(tx_id, &tx);

        Self::deposit_event(Event::BurnTransactionProcessed(tx));

        Ok(().into())
    }

    pub fn add_stellar_sig_refund_transaction(
        tx_hash: Vec<u8>,
        signature: Vec<u8>,
        stellar_pub_key: Vec<u8>,
        sequence_number: u64,
    ) -> DispatchResultWithPostInfo {
        let mut tx = RefundTransactions::<T>::get(&tx_hash);

        let validators = Validators::<T>::get();
        if tx.signatures.len() == (validators.len() / 2) + 1 {
            return Err(DispatchErrorWithPostInfo::from(
                Error::<T>::EnoughRefundSignaturesPresent,
            ));
        }

        // check if the signature already exists
        ensure!(
            !tx.signatures
                .iter()
                .any(|sig| sig.stellar_pub_key == stellar_pub_key),
            Error::<T>::RefundSignatureExists
        );
        ensure!(
            !tx.signatures.iter().any(|sig| sig.signature == signature),
            Error::<T>::RefundSignatureExists
        );

        // add the signature
        let stellar_signature = StellarSignature {
            signature,
            stellar_pub_key,
        };

        tx.sequence_number = sequence_number;
        tx.signatures.push(stellar_signature.clone());
        RefundTransactions::<T>::insert(&tx_hash, &tx);
        Self::deposit_event(Event::RefundTransactionsignatureAdded(
            tx_hash.clone(),
            stellar_signature,
        ));
        // if more then then the half of all validators
        // submitted their signature we can emit an event that a transaction
        // is ready to be submitted to the stellar network
        if tx.signatures.len() >= (validators.len() / 2) + 1 {
            Self::deposit_event(Event::RefundTransactionReady(tx_hash.clone()));
            RefundTransactions::<T>::insert(tx_hash, tx);
        }

        Ok(().into())
    }

    pub fn set_stellar_refund_transaction_executed(
        validator: T::AccountId,
        tx_id: Vec<u8>,
    ) -> DispatchResultWithPostInfo {
        Self::check_if_validator_exists(validator)?;

        ensure!(
            !ExecutedRefundTransactions::<T>::contains_key(&tx_id),
            Error::<T>::RefundTransactionAlreadyExecuted
        );
        ensure!(
            RefundTransactions::<T>::contains_key(&tx_id),
            Error::<T>::RefundTransactionNotExists
        );

        let tx = RefundTransactions::<T>::get(&tx_id);

        RefundTransactions::<T>::remove(&tx_id);
        ExecutedRefundTransactions::<T>::insert(tx_id.clone(), &tx);

        Self::deposit_event(Event::RefundTransactionProcessed(tx));

        Ok(().into())
    }

    pub fn add_validator_account(target: T::AccountId) -> DispatchResultWithPostInfo {
        let mut validators = Validators::<T>::get();

        match validators.binary_search(&target) {
            Ok(_) => Err(Error::<T>::ValidatorExists.into()),
            // If the search fails, the caller is not a member and we learned the index where
            // they should be inserted
            Err(index) => {
                validators.insert(index, target.clone());
                Validators::<T>::put(validators);
                Ok(().into())
            }
        }
    }

    pub fn remove_validator_account(target: T::AccountId) -> DispatchResultWithPostInfo {
        let mut validators = Validators::<T>::get();

        match validators.binary_search(&target) {
            Ok(index) => {
                validators.remove(index);
                Validators::<T>::put(validators);
                Ok(().into())
            }
            Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
        }
    }

    pub fn purge_stellar_storage() -> DispatchResultWithPostInfo {
        let limit = None; // delete all values
        MintTransactions::<T>::remove_all(limit);
        ExecutedMintTransactions::<T>::remove_all(limit);
        BurnTransactions::<T>::remove_all(limit);
        ExecutedBurnTransactions::<T>::remove_all(limit);
        RefundTransactions::<T>::remove_all(limit);
        ExecutedRefundTransactions::<T>::remove_all(limit);
        Ok(().into())
    }

    fn check_if_validator_exists(validator: T::AccountId) -> DispatchResultWithPostInfo {
        let validators = Validators::<T>::get();
        match validators.binary_search(&validator) {
            Ok(_) => Ok(().into()),
            Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
        }
    }
}
