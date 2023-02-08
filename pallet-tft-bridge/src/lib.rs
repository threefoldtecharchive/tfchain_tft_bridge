#![cfg_attr(not(feature = "std"), no_std)]

use frame_support::{
    dispatch::DispatchErrorWithPostInfo,
    ensure, log,
    traits::{
        Currency, EnsureOrigin, ExistenceRequirement, OnUnbalanced, ReservableCurrency,
        WithdrawReasons,
    },
};
use frame_system::{self as system, ensure_signed};
use sp_runtime::SaturatedConversion;

use sp_std::prelude::*;
use substrate_stellar_sdk as stellar;

// Re-export pallet items so that they can be accessed from the crate namespace.
pub use pallet::*;

pub mod migrations;
pub mod types;

#[cfg(test)]
mod mock;

#[cfg(test)]
mod tests;

// Definition of the pallet logic, to be aggregated at runtime definition
// through `construct_runtime`.
#[frame_support::pallet]
pub mod pallet {
    use super::types;
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
        types::MintTransaction<T::AccountId, T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn executed_mint_transactions)]
    pub type ExecutedMintTransactions<T: Config> = StorageMap<
        _,
        Blake2_128Concat,
        Vec<u8>,
        types::MintTransaction<T::AccountId, T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn withdraw_transactions)]
    pub type WithdrawTransactions<T: Config> = StorageMap<
        _,
        Blake2_128Concat,
        u64,
        types::WithdrawTransaction<T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn executed_withdraw_transactions)]
    pub type ExecutedWithdrawTransactions<T: Config> = StorageMap<
        _,
        Blake2_128Concat,
        u64,
        types::WithdrawTransaction<T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn refund_transactions)]
    pub type RefundTransactions<T: Config> = StorageMap<
        _,
        Blake2_128Concat,
        Vec<u8>,
        types::RefundTransaction<T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn executed_refund_transactions)]
    pub type ExecutedRefundTransactions<T: Config> = StorageMap<
        _,
        Blake2_128Concat,
        Vec<u8>,
        types::RefundTransaction<T::BlockNumber>,
        OptionQuery,
    >;

    #[pallet::storage]
    #[pallet::getter(fn withdraw_transaction_id)]
    pub type WithdrawTransactionID<T: Config> = StorageValue<_, u64, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn withdraw_fee)]
    pub type WithdrawFee<T: Config> = StorageValue<_, u64, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn deposit_fee)]
    pub type DepositFee<T: Config> = StorageValue<_, u64, ValueQuery>;

    #[pallet::storage]
    #[pallet::getter(fn pallet_version)]
    pub type PalletVersion<T> = StorageValue<_, types::StorageVersion, ValueQuery>;

    #[pallet::config]
    pub trait Config: frame_system::Config + pallet_balances::Config {
        type RuntimeEvent: From<Event<Self>> + IsType<<Self as frame_system::Config>::RuntimeEvent>;

        /// Currency type for this pallet.
        type Currency: Currency<Self::AccountId> + ReservableCurrency<Self::AccountId>;

        /// Handler for the unbalanced decrement when slashing (withdrawing collateral)
        type Withdraw: OnUnbalanced<NegativeImbalanceOf<Self>>;

        /// Origin for restricted extrinsics
        /// Can be the root or another origin configured in the runtime
        type RestrictedOrigin: EnsureOrigin<Self::RuntimeOrigin>;
    }

    #[pallet::event]
    #[pallet::generate_deposit(pub(super) fn deposit_event)]
    pub enum Event<T: Config> {
        // Minting events
        MintTransactionProposed(Vec<u8>, T::AccountId, u64),
        MintTransactionVoted(Vec<u8>),
        MintCompleted(types::MintTransaction<T::AccountId, T::BlockNumber>),
        // Withdraw events
        WithdrawTransactionCreated(u64, T::AccountId, Vec<u8>, u64),
        WithdrawTransactionProposed(u64, Vec<u8>, u64),
        WithdrawTransactionSignatureAdded(u64, types::StellarSignature),
        WithdrawTransactionReady(u64),
        WithdrawTransactionProcessed(types::WithdrawTransaction<T::BlockNumber>),
        // Refund events
        RefundTransactionCreated(Vec<u8>, Vec<u8>, u64),
        RefundTransactionsignatureAdded(Vec<u8>, types::StellarSignature),
        RefundTransactionReady(Vec<u8>),
        RefundTransactionProcessed(types::RefundTransaction<T::BlockNumber>),
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
        WithdrawTransactionExists,
        WithdrawTransactionNotExists,
        WithdrawSignatureExists,
        EnoughWithdrawSignaturesPresent,
        RefundSignatureExists,
        WithdrawTransactionAlreadyExecuted,
        RefundTransactionNotExists,
        RefundTransactionAlreadyExecuted,
        EnoughRefundSignaturesPresent,
        NotEnoughBalanceToSwap,
        AmountIsLessThanWithdrawFee,
        AmountIsLessThanDepositFee,
        WrongParametersProvided,
        InvalidStellarPublicKey,
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

    #[pallet::call]
    impl<T: Config> Pallet<T> {
        #[pallet::call_index(0)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn add_bridge_validator(
            origin: OriginFor<T>,
            target: T::AccountId,
        ) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            Self::add_validator_account(target)
        }

        #[pallet::call_index(1)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn remove_bridge_validator(
            origin: OriginFor<T>,
            target: T::AccountId,
        ) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            Self::remove_validator_account(target)
        }

        #[pallet::call_index(2)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn set_fee_account(
            origin: OriginFor<T>,
            target: T::AccountId,
        ) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            FeeAccount::<T>::set(Some(target));
            Ok(().into())
        }

        #[pallet::call_index(3)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn set_withdraw_fee(origin: OriginFor<T>, amount: u64) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            WithdrawFee::<T>::set(amount);
            Ok(().into())
        }

        #[pallet::call_index(4)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn set_deposit_fee(origin: OriginFor<T>, amount: u64) -> DispatchResultWithPostInfo {
            T::RestrictedOrigin::ensure_origin(origin)?;
            DepositFee::<T>::set(amount);
            Ok(().into())
        }

        #[pallet::call_index(5)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn swap_to_stellar(
            origin: OriginFor<T>,
            target_stellar_address: Vec<u8>,
            amount: BalanceOf<T>,
        ) -> DispatchResultWithPostInfo {
            let source = ensure_signed(origin)?;
            Self::withdraw_tft(source, target_stellar_address, amount)
        }

        #[pallet::call_index(6)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn propose_or_vote_mint_transaction(
            origin: OriginFor<T>,
            transaction: Vec<u8>,
            target: T::AccountId,
            amount: u64,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::propose_or_vote_stellar_mint_transaction(validator, transaction, target, amount)
        }

        #[pallet::call_index(7)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn propose_withdraw_transaction_or_add_sig(
            origin: OriginFor<T>,
            transaction_id: u64,
            target: Vec<u8>,
            amount: u64,
            signature: Vec<u8>,
            stellar_pub_key: Vec<u8>,
            sequence_number: u64,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::propose_stellar_withdraw_transaction_or_add_sig(
                validator,
                transaction_id,
                target,
                amount,
                signature,
                stellar_pub_key,
                sequence_number,
            )
        }

        #[pallet::call_index(8)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn set_withdraw_transaction_executed(
            origin: OriginFor<T>,
            transaction_id: u64,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::set_stellar_withdraw_transaction_executed(validator, transaction_id)
        }

        #[pallet::call_index(9)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
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

        #[pallet::call_index(10)]
        #[pallet::weight(10_000 + T::DbWeight::get().writes(1).ref_time())]
        pub fn set_refund_transaction_executed(
            origin: OriginFor<T>,
            tx_hash: Vec<u8>,
        ) -> DispatchResultWithPostInfo {
            let validator = ensure_signed(origin)?;
            Self::set_stellar_refund_transaction_executed(validator, tx_hash)
        }
    }
}

use frame_support::pallet_prelude::DispatchResultWithPostInfo;
// Internal functions of the pallet
impl<T: Config> Pallet<T> {
    pub fn mint_tft(
        tx_id: Vec<u8>,
        mut tx: types::MintTransaction<T::AccountId, T::BlockNumber>,
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

    pub fn withdraw_tft(
        source: T::AccountId,
        target_stellar_address: Vec<u8>,
        amount: BalanceOf<T>,
    ) -> DispatchResultWithPostInfo {
        let _ = stellar::PublicKey::from_encoding(target_stellar_address.clone())
            .map_err(|_| <Error<T>>::InvalidStellarPublicKey)?;

        let withdraw_fee = WithdrawFee::<T>::get();
        let withdraw_fee_b = BalanceOf::<T>::saturated_from(withdraw_fee);
        // Make sure the user wants to swap more than the withdraw fee
        ensure!(
            amount > withdraw_fee_b,
            Error::<T>::AmountIsLessThanWithdrawFee
        );

        let usable_balance = Self::get_usable_balance(&source);
        // Make sure the user has enough usable balance to swap the amount
        ensure!(amount <= usable_balance, Error::<T>::NotEnoughBalanceToSwap);

        // transfer amount - fee to target account
        let value = T::Currency::withdraw(
            &source,
            amount,
            WithdrawReasons::TRANSFER,
            ExistenceRequirement::KeepAlive,
        )?;
        T::Withdraw::on_unbalanced(value);

        // transfer withdraw fee to fee wallet
        if let Some(fee_account) = FeeAccount::<T>::get() {
            T::Currency::deposit_creating(&fee_account, withdraw_fee_b);
        }

        // increment withdraw transaction id
        let mut withdraw_id = WithdrawTransactionID::<T>::get();
        withdraw_id += 1;
        WithdrawTransactionID::<T>::put(withdraw_id);

        let withdraw_amount_as_u64 = amount.saturated_into::<u64>() - withdraw_fee;
        Self::deposit_event(Event::WithdrawTransactionCreated(
            withdraw_id,
            source,
            target_stellar_address.clone(),
            withdraw_amount_as_u64,
        ));

        // Create transaction with empty signatures
        let now = <frame_system::Pallet<T>>::block_number();
        let tx = types::WithdrawTransaction {
            id: withdraw_id,
            block: now,
            amount: withdraw_amount_as_u64,
            target: target_stellar_address,
            signatures: Vec::new(),
            sequence_number: 0,
        };
        WithdrawTransactions::<T>::insert(withdraw_id, &tx);

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
        let tx = types::RefundTransaction {
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
        let tx = types::MintTransaction {
            amount,
            target: target.clone(),
            block: now,
            votes: 0,
        };
        MintTransactions::<T>::insert(&tx_id, &tx);

        Self::deposit_event(Event::MintTransactionProposed(
            tx_id.clone(),
            target,
            amount,
        ));

        // Vote already
        Self::vote_stellar_mint_transaction(tx_id)?;

        Ok(().into())
    }

    pub fn vote_stellar_mint_transaction(tx_id: Vec<u8>) -> DispatchResultWithPostInfo {
        let mint_transaction = MintTransactions::<T>::get(&tx_id);
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

    pub fn propose_stellar_withdraw_transaction_or_add_sig(
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
            !ExecutedWithdrawTransactions::<T>::contains_key(tx_id),
            Error::<T>::WithdrawTransactionAlreadyExecuted
        );

        let mut withdraw_tx = WithdrawTransactions::<T>::get(tx_id)
            .ok_or(Error::<T>::WithdrawTransactionNotExists)?;

        ensure!(
            withdraw_tx.amount == amount,
            Error::<T>::WrongParametersProvided
        );
        ensure!(
            withdraw_tx.target == target,
            Error::<T>::WrongParametersProvided
        );

        if WithdrawTransactions::<T>::contains_key(tx_id) {
            return Self::add_stellar_sig_withdraw_transaction(
                tx_id,
                signature,
                stellar_pub_key,
                sequence_number,
            );
        }

        let now = <frame_system::Pallet<T>>::block_number();

        withdraw_tx.block = now;
        withdraw_tx.sequence_number = sequence_number;
        WithdrawTransactions::<T>::insert(tx_id.clone(), &withdraw_tx);

        Self::add_stellar_sig_withdraw_transaction(
            tx_id,
            signature,
            stellar_pub_key,
            sequence_number,
        )?;

        Self::deposit_event(Event::WithdrawTransactionProposed(tx_id, target, amount));

        Ok(().into())
    }

    pub fn add_stellar_sig_withdraw_transaction(
        tx_id: u64,
        signature: Vec<u8>,
        stellar_pub_key: Vec<u8>,
        sequence_number: u64,
    ) -> DispatchResultWithPostInfo {
        let mut tx =
            WithdrawTransactions::<T>::get(&tx_id).ok_or(Error::<T>::WithdrawTransactionExists)?;

        let validators = Validators::<T>::get();
        if tx.signatures.len() == (validators.len() / 2) + 1 {
            return Err(DispatchErrorWithPostInfo::from(
                Error::<T>::EnoughWithdrawSignaturesPresent,
            ));
        }

        // check if the signature already exists
        ensure!(
            !tx.signatures
                .iter()
                .any(|sig| sig.stellar_pub_key == stellar_pub_key),
            Error::<T>::WithdrawSignatureExists
        );
        ensure!(
            !tx.signatures.iter().any(|sig| sig.signature == signature),
            Error::<T>::WithdrawSignatureExists
        );

        // add the signature
        let stellar_signature = types::StellarSignature {
            signature,
            stellar_pub_key,
        };

        tx.sequence_number = sequence_number;
        tx.signatures.push(stellar_signature.clone());
        WithdrawTransactions::<T>::insert(tx_id, &tx);
        Self::deposit_event(Event::WithdrawTransactionSignatureAdded(
            tx_id,
            stellar_signature,
        ));

        if tx.signatures.len() >= (validators.len() / 2) + 1 {
            Self::deposit_event(Event::WithdrawTransactionReady(tx_id));
            WithdrawTransactions::<T>::insert(tx_id, tx);
        }

        Ok(().into())
    }

    pub fn set_stellar_withdraw_transaction_executed(
        validator: T::AccountId,
        tx_id: u64,
    ) -> DispatchResultWithPostInfo {
        Self::check_if_validator_exists(validator)?;

        ensure!(
            !ExecutedWithdrawTransactions::<T>::contains_key(tx_id),
            Error::<T>::WithdrawTransactionAlreadyExecuted
        );
        ensure!(
            WithdrawTransactions::<T>::contains_key(tx_id),
            Error::<T>::WithdrawTransactionNotExists
        );

        let tx =
            WithdrawTransactions::<T>::get(tx_id).ok_or(Error::<T>::WithdrawTransactionExists)?;

        WithdrawTransactions::<T>::remove(tx_id);
        ExecutedWithdrawTransactions::<T>::insert(tx_id, &tx);

        Self::deposit_event(Event::WithdrawTransactionProcessed(tx));

        Ok(().into())
    }

    pub fn add_stellar_sig_refund_transaction(
        tx_hash: Vec<u8>,
        signature: Vec<u8>,
        stellar_pub_key: Vec<u8>,
        sequence_number: u64,
    ) -> DispatchResultWithPostInfo {
        let mut tx =
            RefundTransactions::<T>::get(&tx_hash).ok_or(Error::<T>::RefundTransactionNotExists)?;

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
        let stellar_signature = types::StellarSignature {
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

        let tx =
            RefundTransactions::<T>::get(&tx_id).ok_or(Error::<T>::RefundTransactionNotExists)?;

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

    fn check_if_validator_exists(validator: T::AccountId) -> DispatchResultWithPostInfo {
        let validators = Validators::<T>::get();
        match validators.binary_search(&validator) {
            Ok(_) => Ok(().into()),
            Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
        }
    }

    fn get_usable_balance(account_id: &T::AccountId) -> BalanceOf<T> {
        let balance = pallet_balances::pallet::Pallet::<T>::usable_balance(account_id);
        let b = balance.saturated_into::<u128>();
        BalanceOf::<T>::saturated_from(b)
    }
}
