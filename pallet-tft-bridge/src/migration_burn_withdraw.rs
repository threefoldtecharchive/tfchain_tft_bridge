use crate::Config;
use crate::*;
use frame_support::{
    migration::move_prefix,
    pallet_prelude::ValueQuery,
    storage::{
        storage_prefix,
        unhashed::{exists, kill_prefix},
    },
    storage_alias,
    traits::Get,
    traits::OnRuntimeUpgrade,
    weights::Weight,
    Blake2_128Concat,
};
use log::info;
use sp_std::marker::PhantomData;

#[cfg(feature = "try-runtime")]
use frame_support::traits::OnRuntimeUpgradeHelpersExt;
#[cfg(feature = "try-runtime")]
use sp_runtime::SaturatedConversion;

#[storage_alias]
pub type BurnTransactions<T: Config> = StorageMap<
    Pallet<T>,
    Blake2_128Concat,
    u64,
    types::BurnTransaction<<T as system::Config>::BlockNumber>,
    ValueQuery,
>;

#[storage_alias]
pub type ExecutedBurnTransactions<T: Config> = StorageMap<
    Pallet<T>,
    Blake2_128Concat,
    u64,
    types::BurnTransaction<<T as system::Config>::BlockNumber>,
    ValueQuery,
>;

#[storage_alias]
pub type BurnTransactionID<T: Config> = StorageValue<Pallet<T>, u64, ValueQuery>;

#[storage_alias]
pub type BurnFee<T: Config> = StorageValue<Pallet<T>, u64, ValueQuery>;

pub struct RenameBurnToWithdraw<T: Config>(PhantomData<T>);

impl<T: Config> OnRuntimeUpgrade for RenameBurnToWithdraw<T> {
    #[cfg(feature = "try-runtime")]
    fn pre_upgrade() -> Result<(), &'static str> {
        // Store number of transactions in temp storage
        let tx_count: u64 = BurnTransactions::<T>::iter_keys().count().saturated_into();
        let executed_tx_count: u64 = ExecutedBurnTransactions::<T>::iter_keys()
            .count()
            .saturated_into();
        let tx_id = BurnTransactionID::<T>::get();
        let tx_fee = BurnFee::<T>::get();

        Self::set_temp_storage(tx_count, "pre_tx_count");
        Self::set_temp_storage(executed_tx_count, "pre_executed_tx_count");
        Self::set_temp_storage(tx_id, "pre_tx_id");
        Self::set_temp_storage(tx_fee, "pre_tx_fee");

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

        Ok(())
    }

    fn on_runtime_upgrade() -> Weight {
        rename_burn_to_withdraw::<T>()
    }

    #[cfg(feature = "try-runtime")]
    fn post_upgrade() -> Result<(), &'static str> {
        assert!(PalletVersion::<T>::get() >= types::StorageVersion::V2);

        let pre_tx_count = Self::get_temp_storage("pre_tx_count").unwrap_or(0u64);
        let pre_executed_tx_count = Self::get_temp_storage("pre_executed_tx_count").unwrap_or(0u64);
        let pre_tx_id = Self::get_temp_storage("pre_tx_id").unwrap_or(0u64);
        let pre_tx_fee = Self::get_temp_storage("pre_tx_fee").unwrap_or(0u64);

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
    let mut writes = 0;

    // Move burn tx storage to withdraw tx storage
    move_prefix(
        &storage_prefix(b"TFTBridgeModule", b"BurnTransactions"),
        &storage_prefix(b"TFTBridgeModule", b"WithdrawTransactions"),
    );
    assert_eq!(BurnTransactions::<T>::iter_keys().count(), 0);
    writes += WithdrawTransactions::<T>::iter_keys().count();

    // Move executed burn tx storage to executed withdraw tx storage
    move_prefix(
        &storage_prefix(b"TFTBridgeModule", b"ExecutedBurnTransactions"),
        &storage_prefix(b"TFTBridgeModule", b"ExecutedWithdrawTransactions"),
    );
    assert_eq!(ExecutedBurnTransactions::<T>::iter_keys().count(), 0);
    writes += ExecutedWithdrawTransactions::<T>::iter_keys().count();

    // Copy withdraw values from burn values
    WithdrawTransactionID::<T>::set(BurnTransactionID::<T>::get());
    WithdrawFee::<T>::set(BurnFee::<T>::get());
    writes += 2;

    // At this stage reads = writes
    let reads = writes;

    // Kill old storage prefixes
    kill_prefix(
        &storage_prefix(b"TFTBridgeModule", b"BurnTransactions"),
        None,
    );
    kill_prefix(
        &storage_prefix(b"TFTBridgeModule", b"ExecutedBurnTransactions"),
        None,
    );
    kill_prefix(
        &storage_prefix(b"TFTBridgeModule", b"BurnTransactionID"),
        None,
    );
    kill_prefix(&storage_prefix(b"TFTBridgeModule", b"BurnFee"), None);

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

    // Update pallet storage version
    PalletVersion::<T>::set(types::StorageVersion::V2);
    writes += 1;
    info!(" <<< Storage version upgraded");

    // Return the weight consumed by the migration.
    T::DbWeight::get().reads_writes(reads as Weight, writes as Weight)
}
