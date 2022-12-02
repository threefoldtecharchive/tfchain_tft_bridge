use crate::Config;
use crate::*;
use frame_support::{
    migration::move_prefix, storage::storage_prefix, traits::Get, traits::OnRuntimeUpgrade,
    weights::Weight,
};
use log::info;
use sp_std::marker::PhantomData;

#[cfg(feature = "try-runtime")]
use frame_support::traits::OnRuntimeUpgradeHelpersExt;
#[cfg(feature = "try-runtime")]
use sp_runtime::SaturatedConversion;

pub struct RenameBurnToWithdraw<T: Config>(PhantomData<T>);

impl<T: Config> OnRuntimeUpgrade for RenameBurnToWithdraw<T> {
    #[cfg(feature = "try-runtime")]
    fn pre_upgrade() -> Result<(), &'static str> {
        assert!(PalletVersion::<T>::get() >= types::StorageVersion::V1);

        // Store number of transactions in temp storage
        let tx_count = BurnTransactions::<T>::iter_keys().count();
        Self::set_temp_storage(tx_count, "pre_tx_count");
        let executed_tx_count = ExecutedBurnTransactions::<T>::iter_keys().count();
        Self::set_temp_storage(executed_tx_count, "pre_executed_tx_count");

        log::info!("🔎 RenameBurnToWithdraw pre migration:");
        log::info!(" <- burn tx count: {:?}", tx_count);
        log::info!(" <- executed burn tx count: {:?}", executed_tx_count);
        log::info!(" <- burn tx id: {:?}", Pallet::<T>::burn_transaction_id());
        log::info!(" <- burn fee: {:?}", Pallet::<T>::burn_fee());
        log::info!(
            " -> withdraw tx count: {:?}",
            WithdrawTransactions::<T>::iter_keys().count()
        );
        log::info!(
            " -> executed withdraw tx count: {:?}",
            ExecutedWithdrawTransactions::<T>::iter_keys().count()
        );
        log::info!(
            " -> withdraw tx id: {:?}",
            Pallet::<T>::withdraw_transaction_id()
        );
        log::info!(" -> withdraw fee: {:?}", Pallet::<T>::withdraw_fee());
        log::info!("👥  TFChain TFT Bridge pallet to V2 passes PRE migrate checks ✅",);

        Ok(())
    }

    fn on_runtime_upgrade() -> Weight {
        if PalletVersion::<T>::get() == types::StorageVersion::V1 {
            rename_burn_to_withdraw::<T>()
        } else {
            info!(" >>> Unused migration");
            return 0;
        }
    }

    #[cfg(feature = "try-runtime")]
    fn post_upgrade() -> Result<(), &'static str> {
        assert!(PalletVersion::<T>::get() >= types::StorageVersion::V2);

        let pre_tx_count = Self::get_temp_storage("pre_tx_count").unwrap_or(0u64);
        let pre_executed_tx_count = Self::get_temp_storage("pre_executed_tx_count").unwrap_or(0u64);

        let post_tx_count = WithdrawTransactions::<T>::iter_keys().count();
        let post_executed_tx_count = ExecutedWithdrawTransactions::<T>::iter_keys().count();

        log::info!("🔎 RenameBurnToWithdraw post migration:");
        log::info!(" <- burn tx count: {:?}",);
        log::info!(" <- executed burn tx count: {:?}",);
        log::info!(" <- burn tx id: {:?}", Pallet::<T>::burn_transaction_id());
        log::info!(" <- burn fee: {:?}", Pallet::<T>::burn_fee());
        log::info!(" -> withdraw tx count: {:?}", post_tx_count);
        log::info!(
            " -> executed withdraw tx count: {:?}",
            post_executed_tx_count
        );
        log::info!(
            " -> withdraw tx id: {:?}",
            Pallet::<T>::withdraw_transaction_id()
        );
        log::info!(" -> withdraw fee: {:?}", Pallet::<T>::withdraw_fee());

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
            Pallet::<T>::withdraw_transaction_id(),
            Pallet::<T>::burn_transaction_id(),
            "Number of executed transactions migrated does not match"
        );
        assert_eq!(
            Pallet::<T>::withdraw_fee(),
            Pallet::<T>::burn_fee(),
            "Number of executed transactions migrated does not match"
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
    let mut reads = 0;
    let mut writes = 0;

    move_prefix(
        &storage_prefix(b"TFTBridgeModule", b"BurnTransactions"),
        &storage_prefix(b"TFTBridgeModule", b"WithdrawTransactions"),
    );
    reads += BurnTransactions::<T>::iter_keys().count();
    writes += WithdrawTransactions::<T>::iter_keys().count();

    move_prefix(
        &storage_prefix(b"TFTBridgeModule", b"ExecutedBurnTransactions"),
        &storage_prefix(b"TFTBridgeModule", b"ExecutedWithdrawTransactions"),
    );
    reads += ExecutedBurnTransactions::<T>::iter_keys().count();
    writes += ExecutedWithdrawTransactions::<T>::iter_keys().count();

    WithdrawTransactionID::<T>::set(Pallet::<T>::burn_transaction_id());
    WithdrawFee::<T>::set(Pallet::<T>::burn_fee());
    reads += 2;
    writes += 2;

    // Update pallet storage version
    PalletVersion::<T>::set(types::StorageVersion::V2);
    writes += 1;
    info!(" <<< Storage version upgraded");

    // Return the weight consumed by the migration.
    T::DbWeight::get().reads_writes(reads as Weight, writes as Weight)
}
