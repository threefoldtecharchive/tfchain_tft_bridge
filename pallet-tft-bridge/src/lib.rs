#![cfg_attr(not(feature = "std"), no_std)]

use frame_support::{
	decl_event, decl_module, decl_storage, decl_error, ensure, debug,
	traits::{Currency, OnUnbalanced, ReservableCurrency, Vec, EnsureOrigin},
};
use frame_system::{self as system, ensure_signed, RawOrigin};
use sp_runtime::{DispatchResult, DispatchError};
use codec::{Decode, Encode};
use sp_runtime::traits::SaturatedConversion;

#[cfg(test)]
mod tests;

#[cfg(test)]
mod mock;

// balance type using reservable currency type
type BalanceOf<T> =
	<<T as Config>::Currency as Currency<<T as system::Config>::AccountId>>::Balance;
type NegativeImbalanceOf<T> =
	<<T as Config>::Currency as Currency<<T as system::Config>::AccountId>>::NegativeImbalance;

pub trait Config: system::Config {
	type Event: From<Event<Self>> + Into<<Self as frame_system::Config>::Event>;

	/// Currency type for this pallet.
	type Currency: Currency<Self::AccountId> + ReservableCurrency<Self::AccountId>;

	/// Handler for the unbalanced decrement when slashing (burning collateral)
	type Burn: OnUnbalanced<NegativeImbalanceOf<Self>>;

	/// Origin for signing important extrinsics
	/// Can be the root of another origin configured in the runtime
	type ExternalOrigin: EnsureOrigin<Self::Origin>;
}

decl_event!(
	pub enum Event<T>
	where
		AccountId = <T as system::Config>::AccountId,
		BlockNumber = <T as system::Config>::BlockNumber,
	{
		// Minting events
		MintTransactionProposed(Vec<u8>, AccountId, u64),
		MintTransactionVoted(Vec<u8>),
		MintCompleted(MintTransaction<AccountId, BlockNumber>),
		MintTransactionExpired(Vec<u8>, u64, AccountId),
		// Burn events
		BurnTransactionCreated(u64, Vec<u8>, u64),
		BurnTransactionProposed(u64, Vec<u8>, u64),
		BurnTransactionSignatureAdded(u64, StellarSignature),
		BurnTransactionReady(u64),
		BurnTransactionProcessed(BurnTransaction<BlockNumber>),
		BurnTransactionExpired(u64, Vec<u8>, u64),
		// Refund events
		RefundTransactionCreated(Vec<u8>, Vec<u8>, u64),
		RefundTransactionsignatureAdded(Vec<u8>, StellarSignature),
		RefundTransactionReady(Vec<u8>),
		RefundTransactionProcessed(RefundTransaction<BlockNumber>),
		RefundTransactionExpired(Vec<u8>, Vec<u8>, u64),
	}
);

decl_error! {
	/// Error for the vesting module.
	pub enum Error for Module<T: Config> {
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
	}
}

// MintTransaction contains all the information about
// Stellar -> TF Chain minting transaction.
// if the votes field is larger then (number of validators / 2) + 1 , the transaction will be minted
#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct MintTransaction<AccountId, BlockNumber> {
	pub amount: u64,
	pub target: AccountId,
	pub block: BlockNumber,
	pub votes: u32,
}

// BurnTransaction contains all the information about
// TF Chain -> Stellar burn transaction
// Transaction is ready when (number of validators / 2) + 1 signatures are present
#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct BurnTransaction<BlockNumber> {
	pub block: BlockNumber,
	pub amount: u64,
	pub target: Vec<u8>,
	pub signatures: Vec<StellarSignature>,
	pub sequence_number: u64,
}

#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct RefundTransaction<BlockNumber> {
	pub block: BlockNumber,
	pub amount: u64,
	pub target: Vec<u8>,
	pub tx_hash: Vec<u8>,
	pub signatures: Vec<StellarSignature>,
	pub sequence_number: u64,
}

#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct StellarSignature {
	pub signature: Vec<u8>,
	pub stellar_pub_key: Vec<u8>,
}

pub const AMOUNT_OF_BLOCKS_TO_EXPIRE: u64 = 20;

decl_storage! {
	trait Store for Module<T: Config> as TFTBridgeModule {
		pub Validators get(fn validator_accounts): Vec<T::AccountId>;
		pub FeeAccount get(fn fee_account): T::AccountId;

		// MintTransaction storage maps will contain all the transaction for a Stellar -> TF Chain swap
		pub MintTransactions get(fn mint_transactions): map hasher(blake2_128_concat) Vec<u8> => MintTransaction<T::AccountId, T::BlockNumber>;
		pub ExecutedMintTransactions get(fn executed_mint_transactions): map hasher(blake2_128_concat) Vec<u8> => MintTransaction<T::AccountId, T::BlockNumber>;

		// BurnTransaction storage maps will contain all the transaction for TF Chain -> Stellar swap
		pub BurnTransactions get(fn burn_transactions): map hasher(blake2_128_concat) u64 => BurnTransaction<T::BlockNumber>;
		pub ExecutedBurnTransactions get(fn executed_burn_transactions): map hasher(blake2_128_concat) u64 => BurnTransaction<T::BlockNumber>;

		// RefundTransaction storage maps will contain all the refund transactions
		// Maps a stellar transactionhash to a refund transaction
		pub RefundTransactions get(fn refund_transactions): map hasher(blake2_128_concat) Vec<u8> => RefundTransaction<T::BlockNumber>;
		pub ExecutedRefundTransactions get(fn executed_refund_transactions): map hasher(blake2_128_concat) Vec<u8> => RefundTransaction<T::BlockNumber>;

		pub BurnTransactionID: u64;
		pub WithdrawFee: u64;
		pub DepositFee: u64;
	}

	add_extra_genesis {
		config(validator_accounts): Vec<T::AccountId>;
		config(fee_account): T::AccountId;
		config(withdraw_fee): u64;
		config(deposit_fee): u64;

		build(|_config| {
			let fee_account = _config.fee_account.clone();
			let _ = <Module<T>>::set_fee_account(RawOrigin::Root.into(), fee_account);

			let validator_accounts = _config.validator_accounts.clone();

			for validator in validator_accounts {
				let _ = <Module<T>>::add_bridge_validator(RawOrigin::Root.into(), validator);
			}

			WithdrawFee::set(_config.withdraw_fee);
			DepositFee::set(_config.deposit_fee);
		});
	}
}

decl_module! {
	pub struct Module<T: Config> for enum Call where origin: T::Origin {
		fn deposit_event() = default;
		#[weight = 10_000]
		fn add_bridge_validator(origin, target: T::AccountId){
            T::ExternalOrigin::ensure_origin(origin)?;
            Self::add_validator_account(target)?;
		}
		#[weight = 10_000]
		fn remove_bridge_validator(origin, target: T::AccountId){
            T::ExternalOrigin::ensure_origin(origin)?;
            Self::remove_validator_account(target)?;
		}

		#[weight = 10_000]
		fn set_fee_account(origin, target: T::AccountId) {
			T::ExternalOrigin::ensure_origin(origin)?;
			FeeAccount::<T>::set(target);
		}

		#[weight = 10_000]
		fn set_withdraw_fee(origin, amount: u64) {
			T::ExternalOrigin::ensure_origin(origin)?;
			WithdrawFee::set(amount);
		}

		#[weight = 10_000]
		fn set_deposit_fee(origin, amount: u64) {
			T::ExternalOrigin::ensure_origin(origin)?;
			DepositFee::set(amount);
		}

		#[weight = 10_000]
		fn swap_to_stellar(origin, target_stellar_address: Vec<u8>, amount: BalanceOf<T>){
			let source = ensure_signed(origin)?;
			Self::burn_tft(source, target_stellar_address, amount)?;
		}
		#[weight = 10_000]
		fn propose_or_vote_mint_transaction(origin, transaction: Vec<u8>, target: T::AccountId, amount: u64){
			let validator = ensure_signed(origin)?;
			Self::propose_or_vote_stellar_mint_transaction(validator, transaction, target, amount)?;
		}

		#[weight = 10_000]
		fn propose_burn_transaction_or_add_sig(origin, transaction_id: u64, target: Vec<u8>, amount: u64, signature: Vec<u8>, stellar_pub_key: Vec<u8>, sequence_number: u64){
			let validator = ensure_signed(origin)?;
			Self::propose_stellar_burn_transaction_or_add_sig(validator, transaction_id, target, amount, signature, stellar_pub_key, sequence_number)?;
		}

		#[weight = 10_000]
		fn set_burn_transaction_executed(origin, transaction_id: u64) {
			let validator = ensure_signed(origin)?;
			Self::set_stellar_burn_transaction_executed(validator, transaction_id)?;
		}

		#[weight = 10_000]
		fn create_refund_transaction_or_add_sig(origin, tx_hash: Vec<u8>, target: Vec<u8>, amount: u64, signature: Vec<u8>, stellar_pub_key: Vec<u8>, sequence_number: u64){
			let validator = ensure_signed(origin)?;
			Self::create_stellar_refund_transaction_or_add_sig(validator, tx_hash, target, amount, signature, stellar_pub_key, sequence_number)?;
		}

		#[weight = 10_000]
		fn set_refund_transaction_executed(origin, tx_hash: Vec<u8>) {
			let validator = ensure_signed(origin)?;
			Self::set_stellar_refund_transaction_executed(validator, tx_hash)?;
		}

		fn on_finalize(block: T::BlockNumber) {
			let current_block_u64: u64 = block.saturated_into::<u64>();

			for (tx_id, mut tx) in BurnTransactions::<T>::iter() {
				let tx_block_u64: u64 = tx.block.saturated_into::<u64>();
				// if x blocks have passed since the tx got submitted
				// we can safely assume this tx is fault
				// add the faulty tx to the expired tx list
				if current_block_u64 - tx_block_u64 >= AMOUNT_OF_BLOCKS_TO_EXPIRE {
					// reset signatures and sequence number
					tx.signatures = Vec::new();
					tx.sequence_number = 0;
					tx.block = block;

					// update tx in storage
					BurnTransactions::<T>::insert(tx_id.clone(), &tx);

					// Emit event
					Self::deposit_event(RawEvent::BurnTransactionExpired(tx_id, tx.target, tx.amount));
				}
			}

			for (tx_id, mut tx) in RefundTransactions::<T>::iter() {
				let tx_block_u64: u64 = tx.block.saturated_into::<u64>();
				// if x blocks have passed since the tx got submitted
				// we can safely assume this tx is fault
				// add the faulty tx to the expired tx list
				if current_block_u64 - tx_block_u64 >= AMOUNT_OF_BLOCKS_TO_EXPIRE {
					// reset signatures and sequence number
					tx.signatures = Vec::new();
					tx.sequence_number = 0;
					tx.block = block;

					// update tx in storage
					RefundTransactions::<T>::insert(tx_id.clone(), &tx);

					// Emit event
					Self::deposit_event(RawEvent::RefundTransactionExpired(tx_id, tx.target, tx.amount));
				}
			}
		}
	}
}

impl<T: Config> Module<T> {
	pub fn mint_tft(
		tx_id: Vec<u8>,
		mut tx: MintTransaction<T::AccountId, T::BlockNumber>,
	) -> DispatchResult {
		let deposit_fee = DepositFee::get();
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
		T::Currency::deposit_creating(&FeeAccount::<T>::get(), deposit_fee_b);
		// Remove tx from storage
		MintTransactions::<T>::remove(tx_id.clone());
		// Insert into executed transactions
		let now = <system::Module<T>>::block_number();
		tx.block = now;
		ExecutedMintTransactions::<T>::insert(tx_id, &tx);

		Self::deposit_event(RawEvent::MintCompleted(tx));

		Ok(())
	}

	pub fn burn_tft(
		source: T::AccountId,
		target_stellar_address: Vec<u8>,
		amount: BalanceOf<T>,
	) -> DispatchResult {
		let withdraw_fee = WithdrawFee::get();
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
		T::Currency::deposit_creating(&FeeAccount::<T>::get(), burn_fee_b);

		// increment burn transaction id
		let mut burn_id = BurnTransactionID::get();
		burn_id += 1;
		BurnTransactionID::put(burn_id);

		let burn_amount_as_u64 = amount.saturated_into::<u64>() - withdraw_fee;
		Self::deposit_event(RawEvent::BurnTransactionCreated(
			burn_id,
			target_stellar_address,
			burn_amount_as_u64,
		));

		Ok(())
	}

	pub fn create_stellar_refund_transaction_or_add_sig(
		validator: T::AccountId,
		tx_hash: Vec<u8>,
		target: Vec<u8>,
		amount: u64,
		signature: Vec<u8>,
		stellar_pub_key: Vec<u8>,
		sequence_number: u64,
	) -> DispatchResult {
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

		let now = <frame_system::Module<T>>::block_number();
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

		Self::deposit_event(RawEvent::RefundTransactionCreated(
			tx_hash.clone(),
			target,
			amount,
		));

		Ok(())
	}

	pub fn propose_or_vote_stellar_mint_transaction(
		validator: T::AccountId,
		tx_id: Vec<u8>,
		target: T::AccountId,
		amount: u64,
	) -> DispatchResult {
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

		let now = <frame_system::Module<T>>::block_number();
		let tx = MintTransaction {
			amount,
			target: target.clone(),
			block: now,
			votes: 0,
		};
		MintTransactions::<T>::insert(tx_id.clone(), &tx);

		// Vote already
		Self::vote_stellar_mint_transaction(tx_id.clone())?;

		Self::deposit_event(RawEvent::MintTransactionProposed(tx_id, target, amount));

		Ok(())
	}

	pub fn vote_stellar_mint_transaction(tx_id: Vec<u8>) -> DispatchResult {
		let mut mint_transaction = MintTransactions::<T>::get(tx_id.clone());
		// increment amount of votes
		mint_transaction.votes += 1;

		// deposit voted event
		Self::deposit_event(RawEvent::MintTransactionVoted(tx_id.clone()));

		// update the transaction
		MintTransactions::<T>::insert(&tx_id, &mint_transaction);

		let validators = Validators::<T>::get();
		// If majority aggrees on the transaction, mint tokens to target address
		if mint_transaction.votes as usize >= (validators.len() / 2) + 1 {
			debug::info!("enough votes, minting transaction...");
			Self::mint_tft(tx_id.clone(), mint_transaction)?;
		}

		Ok(())
	}

	pub fn propose_stellar_burn_transaction_or_add_sig(
		validator: T::AccountId,
		tx_id: u64,
		target: Vec<u8>,
		amount: u64,
		signature: Vec<u8>,
		stellar_pub_key: Vec<u8>,
		sequence_number: u64,
	) -> DispatchResult {
		// check if it already has been executed in the past
		ensure!(
			!ExecutedBurnTransactions::<T>::contains_key(tx_id),
			Error::<T>::BurnTransactionAlreadyExecuted
		);

		Self::check_if_validator_exists(validator.clone())?;
		if BurnTransactions::<T>::contains_key(tx_id) {
			return Self::add_stellar_sig_burn_transaction(
				tx_id,
				signature,
				stellar_pub_key,
				sequence_number,
			);
		}

		let now = <frame_system::Module<T>>::block_number();
		let tx = BurnTransaction {
			block: now,
			target: target.clone(),
			amount,
			signatures: Vec::new(),
			sequence_number,
		};
		BurnTransactions::<T>::insert(tx_id.clone(), &tx);

		Self::add_stellar_sig_burn_transaction(tx_id, signature, stellar_pub_key, sequence_number)?;

		Self::deposit_event(RawEvent::BurnTransactionProposed(tx_id, target, amount));

		Ok(())
	}

	pub fn add_stellar_sig_burn_transaction(
		tx_id: u64,
		signature: Vec<u8>,
		stellar_pub_key: Vec<u8>,
		sequence_number: u64,
	) -> DispatchResult {
		let mut tx = BurnTransactions::<T>::get(&tx_id);

		let validators = Validators::<T>::get();
		if tx.signatures.len() == (validators.len() / 2) + 1 {
			return Err(DispatchError::from(Error::<T>::EnoughBurnSignaturesPresent));
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
		Self::deposit_event(RawEvent::BurnTransactionSignatureAdded(
			tx_id,
			stellar_signature,
		));

		if tx.signatures.len() >= (validators.len() / 2) + 1 {
			Self::deposit_event(RawEvent::BurnTransactionReady(tx_id));
			BurnTransactions::<T>::insert(tx_id, tx);
		}

		Ok(())
	}

	pub fn set_stellar_burn_transaction_executed(
		validator: T::AccountId,
		tx_id: u64,
	) -> DispatchResult {
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

		Self::deposit_event(RawEvent::BurnTransactionProcessed(tx));

		Ok(())
	}

	pub fn add_stellar_sig_refund_transaction(
		tx_hash: Vec<u8>,
		signature: Vec<u8>,
		stellar_pub_key: Vec<u8>,
		sequence_number: u64,
	) -> DispatchResult {
		let mut tx = RefundTransactions::<T>::get(&tx_hash);

		let validators = Validators::<T>::get();
		if tx.signatures.len() == (validators.len() / 2) + 1 {
			return Err(DispatchError::from(
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
		Self::deposit_event(RawEvent::RefundTransactionsignatureAdded(
			tx_hash.clone(),
			stellar_signature,
		));
		// if more then then the half of all validators
		// submitted their signature we can emit an event that a transaction
		// is ready to be submitted to the stellar network
		if tx.signatures.len() >= (validators.len() / 2) + 1 {
			Self::deposit_event(RawEvent::RefundTransactionReady(tx_hash.clone()));
			RefundTransactions::<T>::insert(tx_hash, tx);
		}

		Ok(())
	}

	pub fn set_stellar_refund_transaction_executed(
		validator: T::AccountId,
		tx_id: Vec<u8>,
	) -> DispatchResult {
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

		Self::deposit_event(RawEvent::RefundTransactionProcessed(tx));

		Ok(())
	}

	pub fn add_validator_account(target: T::AccountId) -> DispatchResult {
		let mut validators = Validators::<T>::get();

		match validators.binary_search(&target) {
			Ok(_) => Err(Error::<T>::ValidatorExists.into()),
			// If the search fails, the caller is not a member and we learned the index where
			// they should be inserted
			Err(index) => {
				validators.insert(index, target.clone());
				Validators::<T>::put(validators);
				Ok(())
			}
		}
	}

	pub fn remove_validator_account(target: T::AccountId) -> DispatchResult {
		let mut validators = Validators::<T>::get();

		match validators.binary_search(&target) {
			Ok(index) => {
				validators.remove(index);
				Validators::<T>::put(validators);
				Ok(())
			}
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}

	fn check_if_validator_exists(validator: T::AccountId) -> DispatchResult {
		let validators = Validators::<T>::get();
		match validators.binary_search(&validator) {
			Ok(_) => Ok(()),
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}
}
