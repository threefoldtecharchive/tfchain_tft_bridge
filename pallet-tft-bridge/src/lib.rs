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

#[cfg(test)]
mod tests;

#[cfg(test)]
mod mock;

// balance type using reservable currency type
type BalanceOf<T> = <<T as Config>::Currency as Currency<<T as system::Config>::AccountId>>::Balance;
type NegativeImbalanceOf<T> =
	<<T as Config>::Currency as Currency<<T as system::Config>::AccountId>>::NegativeImbalance;

pub trait Config: system::Config {
	type Event: From<Event<Self>> + Into<<Self as frame_system::Config>::Event>;

	/// Currency type for this pallet.
	type Currency: Currency<Self::AccountId> + ReservableCurrency<Self::AccountId>;

	/// Handler for the unbalanced decrement when slashing (burning collateral)
	type Burn: OnUnbalanced<NegativeImbalanceOf<Self>>;
}

decl_event!(
	pub enum Event<T>
	where
		AccountId = <T as system::Config>::AccountId,
		Balance = BalanceOf<T>,
		BlockNumber = <T as system::Config>::BlockNumber,
	{
		// Ming events
		MintTransactionProposed(Vec<u8>, AccountId, Balance),
		MintTransactionVoted(Vec<u8>),
		MintCompleted(AccountId, Balance, BlockNumber),
		// Burn events
		BurnTransactionProposed(Vec<u8>, AccountId, Balance),
		BurnTransactionSignatureAdded(Vec<u8>, Vec<u8>, AccountId),
		BurnTransactionReady(Vec<u8>),
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
		MintTransactionNotExists,
		BurnTransactionExists,
		BurnTransactionNotExists,
		BurnSignatureExists,
	}
}

// MintTransaction contains all the information about
// Stellar -> TF Chain minting transaction.
// if the votes field is larger then (number of validators / 2) + 1 , the transaction will be minted
#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct MintTransaction <BalanceOf, AccountId, BlockNumber>{
	pub amount: BalanceOf,
	pub target: AccountId,
	pub block: BlockNumber,
	pub votes: u32,
}

// BurnTransaction contains all the information about
// TF Chain -> Stellar burn transaction
// Transaction is ready when (number of validators / 2) + 1 signatures are present
#[derive(PartialEq, Eq, PartialOrd, Ord, Clone, Encode, Decode, Default, Debug)]
pub struct BurnTransaction <BlockNumber> {
	pub block: BlockNumber,
	pub signatures: Vec<Vec<u8>>
}

decl_storage! {
	trait Store for Module<T: Config> as TFTBridgeModule {
		pub Validators get(fn validator_accounts): Vec<T::AccountId>;

		// MintTransaction storage maps will contain all the transaction for a Stellar -> TF Chain swap
		pub MintTransactions get(fn mint_transactions): map hasher(blake2_128_concat) Vec<u8> => MintTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>;
		pub ExpiredMintTransactions get(fn expired_mint_transactions): map hasher(blake2_128_concat) Vec<u8> => MintTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>;
		pub ExecutedMintTransactions get(fn executed_mint_transactions): map hasher(blake2_128_concat) Vec<u8> => MintTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>;

		// BurnTransaction storage maps will contain all the transaction for TF Chain -> Stellar swap
		pub BurnTransactions get(fn burn_transactions): map hasher(blake2_128_concat) Vec<u8> => BurnTransaction<T::BlockNumber>;
		pub ExecutedBurnTransactions get(fn executed_burn_transactions): map hasher(blake2_128_concat) Vec<u8> => BurnTransaction<T::BlockNumber>;
	}
}

decl_module! {
	pub struct Module<T: Config> for enum Call where origin: T::Origin {
		fn deposit_event() = default;
		
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
		fn swap_to_stellar(origin, target: T::AccountId, amount: BalanceOf<T>){
            ensure_root(origin)?;
            Self::burn_tft(target, amount);
		}
		
		#[weight = 10_000]
		fn propose_mint_transaction(origin, transaction: Vec<u8>, target: T::AccountId, amount: BalanceOf<T>){
            let validator = ensure_signed(origin)?;
            Self::propose_stellar_mint_transaction(validator, transaction, target, amount)?;
		}
		
		#[weight = 10_000]
		fn vote_mint_transaction(origin, transaction: Vec<u8>){
            let validator = ensure_signed(origin.clone())?;
			Self::vote_stellar_mint_transaction(validator, transaction)?;
		}

		#[weight = 10_000]
		fn propose_burn_transaction(origin, transaction: Vec<u8>, target: T::AccountId, amount: BalanceOf<T>){
            let validator = ensure_signed(origin)?;
            Self::propose_stellar_burn_transaction(validator, transaction, target, amount)?;
		}
		
		#[weight = 10_000]
		fn add_sig_burn_transaction(origin, transaction: Vec<u8>, signature: Vec<u8>){
            let validator = ensure_signed(origin)?;
            Self::add_stellar_sig_burn_transaction(validator, transaction, signature)?;
		}

		fn on_finalize(block: T::BlockNumber) {
			let current_block_u64: u64 = block.saturated_into::<u64>();

			for (tx_id, tx) in MintTransactions::<T>::iter() {
				let tx_block_u64: u64 = tx.block.saturated_into::<u64>();
				// if 100 blocks have passed since the tx got submitted
				// we can safely assume this tx is fault
				// add the faulty tx to the expired tx list
				if current_block_u64 - tx_block_u64 >= 100 {
					// Remove tx from storage
					MintTransactions::<T>::remove(tx_id.clone());
					// Insert into expired transactions list
					ExpiredMintTransactions::<T>::insert(tx_id, tx);
				}
			}
		}
	}
}

impl<T: Config> Module<T> {
	pub fn mint_tft(tx_id: Vec<u8>, tx: MintTransaction<BalanceOf<T>, T::AccountId, T::BlockNumber>) {        
        T::Currency::deposit_creating(&tx.target, tx.amount);
	
		// Remove tx from storage
		MintTransactions::<T>::remove(tx_id.clone());
		// Insert into executed transactions
		ExecutedMintTransactions::<T>::insert(tx_id, &tx);

        let now = <system::Module<T>>::block_number();
        Self::deposit_event(RawEvent::MintCompleted(tx.target, tx.amount, now));
    }

    pub fn burn_tft(target: T::AccountId, amount: BalanceOf<T>) {
        let imbalance = T::Currency::slash(&target, amount).0;
        T::Burn::on_unbalanced(imbalance);
	}

	pub fn propose_stellar_mint_transaction(origin: T::AccountId, tx_id: Vec<u8>, target: T::AccountId, amount: BalanceOf<T>) -> DispatchResult {
		// make sure we don't duplicate the transaction
		ensure!(!MintTransactions::<T>::contains_key(tx_id.clone()), Error::<T>::MintTransactionExists);
		
		let validators = Validators::<T>::get();
		match validators.binary_search(&origin) {
			Ok(_) => {
				let now = <frame_system::Module<T>>::block_number();
				let tx = MintTransaction {
					amount,
					target: target.clone(),
					block: now,
					votes: 1
				};
				MintTransactions::<T>::insert(tx_id.clone(), &tx);

				Self::deposit_event(RawEvent::MintTransactionProposed(tx_id, target, amount));

				Ok(())
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}

	pub fn vote_stellar_mint_transaction(validator: T::AccountId, tx_id: Vec<u8>) -> DispatchResult {
		ensure!(MintTransactions::<T>::contains_key(tx_id.clone()), Error::<T>::MintTransactionNotExists);
		
		let validators = Validators::<T>::get();
		match validators.binary_search(&validator) {
			Ok(_) => {
				let mut mint_transaction = MintTransactions::<T>::get(tx_id.clone());
				// increment amount of votes
				mint_transaction.votes += 1;

				// deposit voted event
				Self::deposit_event(RawEvent::MintTransactionVoted(tx_id.clone()));

				// update the transaction
				MintTransactions::<T>::insert(&tx_id, &mint_transaction);

				// If majority aggrees on the transaction, mint tokens to target address
				if mint_transaction.votes as usize >= (validators.len() / 2) + 1 {
					debug::info!("enough votes, minting transaction...");
					Self::mint_tft(tx_id.clone(), mint_transaction);
				}

				Ok(())
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}

	pub fn propose_stellar_burn_transaction(origin: T::AccountId, tx_id: Vec<u8>, target: T::AccountId, amount: BalanceOf<T>) -> DispatchResult {
		// make sure we don't duplicate the transaction
		ensure!(!BurnTransactions::<T>::contains_key(tx_id.clone()), Error::<T>::BurnTransactionExists);
		
		let validators = Validators::<T>::get();
		match validators.binary_search(&origin) {
			Ok(_) => {
				let now = <frame_system::Module<T>>::block_number();
				let tx = BurnTransaction {
					block: now,
					signatures: Vec::new()
				};
				BurnTransactions::<T>::insert(tx_id.clone(), &tx);

				Self::deposit_event(RawEvent::BurnTransactionProposed(tx_id, target, amount));

				Ok(())
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
	}

	pub fn add_stellar_sig_burn_transaction(origin: T::AccountId, tx_id: Vec<u8>, signature: Vec<u8>) -> DispatchResult {
		// make sure tx exists
		ensure!(BurnTransactions::<T>::contains_key(tx_id.clone()), Error::<T>::BurnTransactionNotExists);
		
		let validators = Validators::<T>::get();
		match validators.binary_search(&origin) {
			Ok(_) => {				
				let mut tx = BurnTransactions::<T>::get(&tx_id.clone());
				// check if the signature already exists
				ensure!(!tx.signatures.iter().any(|sig| sig == &signature), Error::<T>::BurnSignatureExists);

				// add the signature
				tx.signatures.push(signature.clone());
				BurnTransactions::<T>::insert(tx_id.clone(), &tx);
				Self::deposit_event(RawEvent::BurnTransactionSignatureAdded(tx_id.clone(), signature, origin));

				// if more then then the half of all validators
				// submitted their signature we can emit an event that a transaction
				// is ready to be submitted to the stellar network
				if tx.signatures.len() >= (validators.len() / 2) + 1 {
					Self::deposit_event(RawEvent::BurnTransactionReady(tx_id.clone()));
					ExecutedBurnTransactions::<T>::insert(tx_id, tx);
				}

				Ok(())
			},
			Err(_) => Err(Error::<T>::ValidatorNotExists.into()),
		}
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
}
