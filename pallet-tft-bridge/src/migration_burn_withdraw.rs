use crate::Config;
use crate::*;
use frame_support::{traits::Get, traits::OnRuntimeUpgrade, weights::Weight};
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
        let tx_count: u64 = Nodes::<T>::iter_keys().count().saturated_into();
        Self::set_temp_storage(tx_count, "pre_tx_count");
        let executed_tx_count: u64 = Nodes::<T>::iter_keys().count().saturated_into();
        Self::set_temp_storage(executed_tx_count, "pre_executed_tx_count");
        log::info!(
            "🔎 RenameBurnToWithdraw pre migration: Number of existing transactions {:?} and executed transactions {:?} ",
            tx_count,
            executed_tx_count,
        );

        info!("👥  TFChain TFT Bridge pallet to V2 passes PRE migrate checks ✅",);
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
        assert!(PalletVersion::<T>::get() >= types::StorageVersion::V10Struct);

        // Check number of transactions against pre-check result
        let pre_tx_count = Self::get_temp_storage("pre_tx_count").unwrap_or(0u64);
        let pre_executed_tx_count = Self::get_temp_storage("pre_executed_tx_count").unwrap_or(0u64);
        assert_eq!(
            Nodes::<T>::iter_keys().count().saturated_into::<u64>(),
            pre_tx_count,
            "Number of transactions migrated does not match"
        );
        assert_eq!(
            Nodes::<T>::iter_keys().count().saturated_into::<u64>(),
            pre_executed_tx_count,
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

    let reads = 0;
    let writes = 0;

    // Update pallet storage version
    PalletVersion::<T>::set(types::StorageVersion::V2);
    info!(" <<< Storage version upgraded");

    // Return the weight consumed by the migration.
    T::DbWeight::get().reads_writes(reads as Weight, writes as Weight)
}
