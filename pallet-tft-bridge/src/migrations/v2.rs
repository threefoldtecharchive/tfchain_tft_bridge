use crate::Config;
use crate::*;
use frame_support::{
    migration::{clear_storage_prefix, move_prefix},
    pallet_prelude::ValueQuery,
    storage::{storage_prefix, unhashed::exists},
    storage_alias,
    traits::Get,
    traits::OnRuntimeUpgrade,
    weights::Weight,
    Blake2_128Concat,
};
use log::info;
use sp_std::marker::PhantomData;

#[storage_alias]
pub type BurnTransactions<T: Config> = StorageMap<
    Pallet<T>,
    Blake2_128Concat,
    u64,
    super::types::v1::BurnTransaction<<T as system::Config>::BlockNumber>,
    ValueQuery,
>;

#[storage_alias]
pub type ExecutedBurnTransactions<T: Config> = StorageMap<
    Pallet<T>,
    Blake2_128Concat,
    u64,
    super::types::v1::BurnTransaction<<T as system::Config>::BlockNumber>,
    ValueQuery,
>;

#[storage_alias]
pub type BurnTransactionID<T: Config> = StorageValue<Pallet<T>, u64, ValueQuery>;

#[storage_alias]
pub type BurnFee<T: Config> = StorageValue<Pallet<T>, u64, ValueQuery>;

pub struct RenameBurnToWithdraw<T: Config>(PhantomData<T>);

impl<T: Config> OnRuntimeUpgrade for RenameBurnToWithdraw<T> {
    #[cfg(feature = "try-runtime")]
    fn pre_upgrade() -> Result<Vec<u8>, &'static str> {
        // Store number of transactions in temp storage
        let tx_count: u64 = BurnTransactions::<T>::iter_keys().count().saturated_into();
        let executed_tx_count: u64 = ExecutedBurnTransactions::<T>::iter_keys()
            .count()
            .saturated_into();
        let tx_id = BurnTransactionID::<T>::get();
        let tx_fee = BurnFee::<T>::get();

        let pre_data = [
            u64::to_le_bytes(tx_count).to_vec(),
            u64::to_le_bytes(executed_tx_count).to_vec(),
            u64::to_le_bytes(tx_id).to_vec(),
            u64::to_le_bytes(tx_fee).to_vec(),
        ]
        .concat();

        // Display pre migration state
        log::info!("🔎 RenameBurnToWithdraw pre migration:");
        log::info!(" <-- burn tx count: {:?}", tx_count);
        log::info!(" <-- executed burn tx count: {:?}", executed_tx_count);
        log::info!(" <-- burn tx id: {:?}", tx_id);
        log::info!(" <-- burn fee: {:?}", tx_fee);
        log::info!(
            " --> withdraw tx count: {:?}",
            WithdrawTransactions::<T>::iter_keys().count()
        );
        log::info!(
            " --> executed withdraw tx count: {:?}",
            ExecutedWithdrawTransactions::<T>::iter_keys().count()
        );
        log::info!(
            " --> withdraw tx id: {:?}",
            Pallet::<T>::withdraw_transaction_id()
        );
        log::info!(" --> withdraw fee: {:?}", Pallet::<T>::withdraw_fee());
        log::info!("👥  TFChain TFT Bridge pallet to V2 passes PRE migrate checks ✅",);

        Ok(pre_data)
    }

    fn on_runtime_upgrade() -> Weight {
        rename_burn_to_withdraw::<T>()
    }

    #[cfg(feature = "try-runtime")]
    fn post_upgrade(pre_data: Vec<u8>) -> Result<(), &'static str> {
        assert!(PalletVersion::<T>::get() >= types::StorageVersion::V2);

        let pre_tx_count = u64::from_le_bytes(pre_data[0..8].try_into().unwrap_or([0_u8; 8]));
        let pre_executed_tx_count =
            u64::from_le_bytes(pre_data[8..16].try_into().unwrap_or([0_u8; 8]));
        let pre_tx_id = u64::from_le_bytes(pre_data[16..24].try_into().unwrap_or([0_u8; 8]));
        let pre_tx_fee = u64::from_le_bytes(pre_data[24..].try_into().unwrap_or([0_u8; 8]));

        let post_tx_count: u64 = WithdrawTransactions::<T>::iter_keys()
            .count()
            .saturated_into();
        let post_executed_tx_count: u64 = ExecutedWithdrawTransactions::<T>::iter_keys()
            .count()
            .saturated_into();
        let post_tx_id = Pallet::<T>::withdraw_transaction_id();
        let post_tx_fee = Pallet::<T>::withdraw_fee();

        // Display post migration state
        log::info!("🔎 RenameBurnToWithdraw post migration:");
        log::info!(
            " <-- burn tx count: {:?}",
            BurnTransactions::<T>::iter_keys().count()
        );
        log::info!(
            " <-- executed burn tx count: {:?}",
            ExecutedBurnTransactions::<T>::iter_keys().count()
        );
        log::info!(" <-- burn tx id: {:?}", BurnTransactionID::<T>::get());
        log::info!(" <-- burn fee: {:?}", BurnFee::<T>::get());
        log::info!(" --> withdraw tx count: {:?}", post_tx_count);
        log::info!(
            " --> executed withdraw tx count: {:?}",
            post_executed_tx_count
        );
        log::info!(" --> withdraw tx id: {:?}", post_tx_id);
        log::info!(" --> withdraw fee: {:?}", post_tx_fee);

        // Check transactions against pre-check result
        assert_eq!(
            pre_tx_count, post_tx_count,
            "Number of transactions migrated does not match"
        );
        assert_eq!(
            pre_executed_tx_count, post_executed_tx_count,
            "Number of executed transactions migrated does not match"
        );
        assert_eq!(
            pre_tx_id, post_tx_id,
            "Transaction id migrated does not match"
        );
        assert_eq!(
            pre_tx_fee, post_tx_fee,
            "Transaction fee migrated does not match"
        );

        info!(
            "👥  TFChain TFT Bridge pallet migration to {:?} passes POST migrate checks ✅",
            Pallet::<T>::pallet_version()
        );

        Ok(())
    }
}

pub fn rename_burn_to_withdraw<T: Config>() -> frame_support::weights::Weight {
    info!(" >>> Migrating transactions storage...");
    let mut reads_writes = 0;

    // Move burn tx storage to withdraw tx storage
    move_prefix(
        &storage_prefix(b"TFTBridgeModule", b"BurnTransactions"),
        &storage_prefix(b"TFTBridgeModule", b"WithdrawTransactions"),
    );
    assert_eq!(BurnTransactions::<T>::iter_keys().count(), 0);
    reads_writes += WithdrawTransactions::<T>::iter_keys().count();

    // Move executed burn tx storage to executed withdraw tx storage
    move_prefix(
        &storage_prefix(b"TFTBridgeModule", b"ExecutedBurnTransactions"),
        &storage_prefix(b"TFTBridgeModule", b"ExecutedWithdrawTransactions"),
    );
    assert_eq!(ExecutedBurnTransactions::<T>::iter_keys().count(), 0);
    reads_writes += ExecutedWithdrawTransactions::<T>::iter_keys().count();

    // Copy withdraw values from burn values
    WithdrawTransactionID::<T>::set(BurnTransactionID::<T>::get());
    WithdrawFee::<T>::set(BurnFee::<T>::get());
    reads_writes += 2;

    // Remove all items under BurnTransactions
    let _ = clear_storage_prefix(b"TFTBridgeModule", b"BurnTransactions", b"", None, None);
    // Remove all items under ExecutedBurnTransactions
    let _ = clear_storage_prefix(
        b"TFTBridgeModule",
        b"ExecutedBurnTransactions",
        b"",
        None,
        None,
    );
    // Remove all items under BurnTransactionID
    let _ = clear_storage_prefix(b"TFTBridgeModule", b"BurnTransactionID", b"", None, None);
    // Remove all items under BurnFee
    let _ = clear_storage_prefix(b"TFTBridgeModule", b"BurnFee", b"", None, None);

    // Ensure old storage prefixes does not exist anymore
    assert_eq!(
        exists(&storage_prefix(b"TFTBridgeModule", b"BurnTransactions")),
        false
    );
    assert_eq!(
        exists(&storage_prefix(
            b"TFTBridgeModule",
            b"ExecutedBurnTransactions"
        )),
        false
    );
    assert_eq!(
        exists(&storage_prefix(b"TFTBridgeModule", b"BurnTransactionID")),
        false
    );
    assert_eq!(
        exists(&storage_prefix(b"TFTBridgeModule", b"BurnFee")),
        false
    );

    info!(" <<< Transactions storage updated! Renaming \"Burn\" => \"Withdraw\" ✅",);

    // Update pallet storage version
    PalletVersion::<T>::set(types::StorageVersion::V2);
    info!(" <<< Transactions migration success, storage version upgraded");

    // Return the weight consumed by the migration.
    T::DbWeight::get().reads_writes(reads_writes as u64, reads_writes as u64 + 1)
}
