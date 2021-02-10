#![cfg_attr(not(feature = "std"), no_std)]

//! A Pallet to demonstrate using currency imbalances
//!
//! WARNING: never use this code in production (for demonstration/teaching purposes only)
//! it only checks for signed extrinsics to enable arbitrary minting/slashing!!!

use frame_support::{
	decl_event, decl_module, decl_storage, decl_error, ensure, debug,
	traits::{Currency, OnUnbalanced, ReservableCurrency, Vec},
};
use frame_system::{self as system, ensure_signed, ensure_root};
use sp_runtime::{DispatchResult};
use codec::{Decode, Encode};
use sp_runtime::traits::SaturatedConversion;

// balance type using reservable currency type
type BalanceOf<T> = <<T as Trait>::Currency as Currency<<T as system::Trait>::AccountId>>::Balance;
type NegativeImbalanceOf<T> =
	<<T as Trait>::Currency as Currency<<T as system::Trait>::AccountId>>::NegativeImbalance;

pub trait Trait: system::Trait {
	type Event: From<Event<Self>> + Into<<Self as frame_system::Trait>::Event>;

	/// Currency type for this pallet.
	type Currency: Currency<Self::AccountId> + ReservableCurrency<Self::AccountId>;

	/// Handler for the unbalanced decrement when slashing (burning collateral)
	type Burn: OnUnbalanced<NegativeImbalanceOf<Self>>;
}

decl_event!(
	pub enum Event<T>
	where
		AccountId = <T as system::Trait>::AccountId,
		Balance = BalanceOf<T>,
		BlockNumber = <T as system::Trait>::BlockNumber,
	{
		AccountDrained(AccountId, Balance, BlockNumber),
		AccountFunded(AccountId, Balance, BlockNumber),
		TransactionProposed(Vec<u8>, AccountId, Balance),
	}
);

decl_error! {
	/// Error for the vesting module.
	pub enum Error for Module<T: Trait> {
		ValidatorExists,
		ValidatorNotExists,
		TransactionValidatorExists,
		TransactionValidatorNotExists,
		TransactionExists,
		TransactionNotExists,
	}
}

#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct StellarTransaction <BalanceOf, AccountId, BlockNumber>{
	pub amount: BalanceOf,
	pub target: AccountId,
	pub block: BlockNumber,
}

decl_storage! {
	trait Store for Module<T: Trait> as TFTBridgeModule {
		pub Validators get(fn validator_accounts): Vec<T::AccountId>;

		pub Transactions get(fn transactions): map hasher(blake2_128_concat) Vec<u8> => StellarTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>;
		
		pub ExpiredTransactions get(fn expired_transactions): map hasher(blake2_128_concat) Vec<u8> => StellarTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>;
		pub ExecutedTransactions get(fn executed_transactions): map hasher(blake2_128_concat) Vec<u8> => StellarTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>;

		pub TransactionValidators get(fn transaction_validators): map hasher(blake2_128_concat) Vec<u8> => Vec<T::AccountId>;
	}
}

decl_module! {
	pub struct Module<T: Trait> for enum Call where origin: T::Origin {
		fn deposit_event() = default;

        #[weight = 10_000]
		fn swap_to_stellar(origin, target: T::AccountId, amount: BalanceOf<T>){
            ensure_signed(origin)?;
            Self::burn_tft(target, amount);
		}
		
		#[weight = 10_000]
		fn add_validator(origin, target: T::AccountId){
            ensure_signed(origin)?;
            Self::add_validator_account(target)?;
		}
		
		#[weight = 10_000]
		fn remove_validator(origin, target: T::AccountId){
            ensure_signed(origin)?;
            Self::remove_validator_account(target)?;
		}
		
		#[weight = 10_000]
		fn propose_transaction(origin, transaction: Vec<u8>, target: T::AccountId, amount: BalanceOf<T>){
            let validator = ensure_signed(origin)?;
            Self::propose_stellar_transaction(validator, transaction, target, amount)?;
		}
		
		#[weight = 10_000]
		fn vote_transaction(origin, transaction: Vec<u8>){
            let validator = ensure_signed(origin.clone())?;
			Self::vote_stellar_transaction(validator, transaction)?;
		}

		fn on_finalize(block: T::BlockNumber) {
			let current_block_u64: u64 = block.saturated_into::<u64>();

			for (tx_id, tx) in Transactions::<T>::iter() {
				let tx_block_u64: u64 = tx.block.saturated_into::<u64>();
				// if 100 blocks have passed since the tx got submitted
				// we can safely assume this tx is fault
				// add the faulty tx to the expired tx list
				if current_block_u64 - tx_block_u64 >= 100 {
					// Remove tx from storage
					Transactions::<T>::remove(tx_id.clone());
					// Remove tx validators from storage
					TransactionValidators::<T>::remove(tx_id.clone());
					// Insert into expired transactions list
					ExpiredTransactions::<T>::insert(tx_id, tx);
				}
			}
		}
	}
}

impl<T: Trait> Module<T> {
	pub fn mint_tft(tx_id: Vec<u8>, tx: StellarTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>) {        
        T::Currency::deposit_creating(&tx.target, tx.amount);
	
		// Remove tx from storage
		Transactions::<T>::remove(tx_id.clone());
		// Remove tx validators from storage
		TransactionValidators::<T>::remove(tx_id.clone());
		// Insert into executed transactions
		ExecutedTransactions::<T>::insert(tx_id, &tx);

        let now = <system::Module<T>>::block_number();
        Self::deposit_event(RawEvent::AccountFunded(tx.target, tx.amount, now));
    }

    pub fn burn_tft(target: T::AccountId, amount: BalanceOf<T>) {
        let imbalance = T::Currency::slash(&target, amount).0;
        T::Burn::on_unbalanced(imbalance);
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
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}

	pub fn propose_stellar_transaction(origin: T::AccountId, tx_id: Vec<u8>, target: T::AccountId, amount: BalanceOf<T>) -> DispatchResult {
		// make sure we don't duplicate the transaction
		ensure!(!Transactions::<T>::contains_key(tx_id.clone()), Error::<T>::TransactionExists);
		
		let validators = Validators::<T>::get();
		match validators.binary_search(&origin) {
			Ok(_) => {
				// list where voters are kept
				let mut voters: Vec<T::AccountId> = Vec::new();
				// init the list with the validator proposing this transaction
				voters.push(origin);
				
				TransactionValidators::<T>::insert(&tx_id.clone(), voters);
				
				let now = <frame_system::Module<T>>::block_number();
				let tx = StellarTransaction {
					amount,
					target: target.clone(),
					block: now,
				};
				Transactions::<T>::insert(tx_id.clone(), &tx);

				Self::deposit_event(RawEvent::TransactionProposed(tx_id, target, amount));

				Ok(())
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}

	pub fn vote_stellar_transaction(validator: T::AccountId, tx_id: Vec<u8>) -> DispatchResult {
		ensure!(TransactionValidators::<T>::contains_key(tx_id.clone()), Error::<T>::TransactionNotExists);
		
		// fetch the validators list
		let mut stellar_transaction_validators = TransactionValidators::<T>::get(tx_id.clone());
		
		let validators = Validators::<T>::get();
		match validators.binary_search(&validator) {
			Ok(_) => {
				// if the validator is a valid validator, we can add it's vote to the stellar transaction
				// if the vote is already there, return an error
				match stellar_transaction_validators.binary_search(&validator) {
					Ok(_) => Ok(()),
					Err(index) => {
						stellar_transaction_validators.insert(index, validator.clone());
						debug::info!("inserting vote for validator {:?}", validator.clone());
						TransactionValidators::<T>::insert(tx_id.clone(), &stellar_transaction_validators);

						// If majority aggrees on the transaction, mint tokens to target address
						if stellar_transaction_validators.len() > (validators.len() / 2) {
							let tx = Transactions::<T>::get(&tx_id.clone());

							debug::info!("enough votes, minting transaction...");
							Self::mint_tft(tx_id, tx);
						}

						Ok(())
					}
				}
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}
}
