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
		TransactionProposed(Vec<u8>),
	}
);

decl_error! {
	/// Error for the vesting module.
	pub enum Error for Module<T: Trait> {
		ValidatorExists,
		ValidatorNotExists,
		TransactionExists,
		TransactionNotExists,
	}
}

#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct StellarTransaction <BalanceOf, AccountId>{
	pub amount: BalanceOf,
	pub target: AccountId,
}

decl_storage! {
	trait Store for Module<T: Trait> as TFTBridgeModule {
		pub Validators get(fn validator_accounts): Vec<T::AccountId>;

		pub Transactions get(fn transaction): map hasher(blake2_128_concat) Vec<u8> => StellarTransaction<BalanceOf<T>, T::AccountId>;
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
            ensure_root(origin)?;
            Self::add_validator_account(target)?;
		}
		
		#[weight = 10_000]
		fn remove_validator(origin, target: T::AccountId){
            ensure_root(origin)?;
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
	}
}

impl<T: Trait> Module<T> {
	pub fn mint_tft(target: T::AccountId, amount: BalanceOf<T>) {        
        T::Currency::deposit_creating(&target, amount);
    
        let now = <system::Module<T>>::block_number();
        Self::deposit_event(RawEvent::AccountFunded(target, amount, now));
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
		ensure!(!Transactions::<T>::contains_key(tx_id.clone()), Error::<T>::TransactionExists);
		
		let tx = StellarTransaction {
			amount,
			target
		};
		Transactions::<T>::insert(tx_id.clone(), &tx);
		
		ensure!(TransactionValidators::<T>::contains_key(tx_id.clone()), Error::<T>::TransactionNotExists);
		
		// list where voters are kept
		let mut voters: Vec<T::AccountId> = Vec::new();
		// init the list with the validator proposing this transaction
		voters.push(origin);

		TransactionValidators::<T>::insert(&tx_id.clone(), voters);

		Self::deposit_event(RawEvent::TransactionProposed(tx_id));

		Ok(())
	}

	pub fn vote_stellar_transaction(validator: T::AccountId, tx_id: Vec<u8>) -> DispatchResult {
		ensure!(!TransactionValidators::<T>::contains_key(tx_id.clone()), Error::<T>::TransactionExists);
		
		// fetch the validators list
		let mut stellar_transaction_validators = TransactionValidators::<T>::get(tx_id.clone());
		
		let validators = Validators::<T>::get();
		match validators.binary_search(&validator) {
			Ok(_) => {
				// if the validator is a valid validator, we can add it's vote to the stellar transaction
				// if the vote is already there, return an error
				match stellar_transaction_validators.binary_search(&validator) {
					Ok(_) => Err(Error::<T>::ValidatorExists.into()),
					Err(index) => {
						stellar_transaction_validators.insert(index, validator.clone());
						debug::info!("inserting vote for validator {:?}", validator.clone());
						TransactionValidators::<T>::insert(tx_id.clone(), &stellar_transaction_validators);

						// If majority aggrees on the transaction, mint tokens to target address
						if stellar_transaction_validators.len() > (validators.len() + 1) {
							let tx = Transactions::<T>::get(&tx_id.clone());

							debug::info!("enough votes, minting transaction...");
							Self::mint_tft(tx.target, tx.amount);
						}

						Ok(())
					}
				}
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}
}
