use log::info;
use tfchain_client::{
    client::{self, Hash},
    runtimes::devnet::devnet::tft_bridge_module::events::{
        BurnTransactionCreated, BurnTransactionExpired, BurnTransactionReady,
        RefundTransactionExpired, RefundTransactionReady,
    },
};

pub struct Bridge {
    tfchain_client: client::TfchainClient,
}

use futures::StreamExt;

impl Bridge {
    pub async fn new() -> Bridge {
        // Create a client to use:
        let phrase =
            "oyster orient plunge devote light wrap hold mother essence casino rebel distance";
        let p = client::get_from_seed(phrase, None);
        // let url = String::from("ws://185.206.122.126:9944");
        let url = String::from("wss://tfchain.dev.grid.tf:443");
        let tfchain_client = client::TfchainClient::new(url, p, "devnet")
            .await
            .expect("failed to create client");

        Bridge { tfchain_client }
    }

    pub async fn get_events_for_block(&self, block_hash: Hash) {
        // let b = "7badc36b2ccc0bd8832f2a2c31c4d7fa6d96b2316ea6efd20a9765ce26ca9a66";
        // let x = hex::decode(b).unwrap();
        // let hash = Hash::from_slice(&x);
        let events = self
            .tfchain_client
            .api
            .events()
            .at(Some(block_hash))
            .await
            .unwrap();

        for e in events.iter() {
            match e {
                Ok(event) => {
                    println!(
                        "event ({}, {}): {} - {}",
                        event.pallet_index(),
                        event.index(),
                        event.pallet_name(),
                        event.variant_name()
                    );
                }
                _ => (),
            }
        }
    }

    pub async fn subscribe_events(&self) {
        let mut bridge_events = self
            .tfchain_client
            .api
            .events()
            .subscribe()
            .await
            .unwrap()
            .filter_events::<(
                // RefundTransactionReady means a refund is ready to be made from the vault
                RefundTransactionReady,
                // RefundTransactionExpired means a refund from the vault is never picked up by peers
                // We can try to initiate the singing of this refund again
                RefundTransactionExpired,
                // BurnTransactionCreated means a user wants to swap from Tfchain to his stellar account
                BurnTransactionCreated,
                // BurnTransactionReady means the swap is ready to be made from the vault
                BurnTransactionReady,
                // BurnTransactionExpired means the swap is never picked up by peers
                // We can try to initiate the singing of this swap again
                BurnTransactionExpired,
            )>();

        info!("subscription for bridge events opened...");

        while let Some(ev) = bridge_events.next().await {
            match ev {
                Ok(event_details) => {
                    let block_hash = event_details.block_hash;
                    info!("Event found at block: {:?}", block_hash);
                    let event = event_details.event;

                    if let (Some(refund_ready), _, _, _, _) = &event {
                        info!("  refund ready event: {refund_ready:?}");
                    }

                    if let (_, Some(refund_expired), _, _, _) = &event {
                        info!("  refund expired event: {refund_expired:?}");
                    }

                    if let (_, _, Some(burn_created), _, _) = &event {
                        info!("  burn createad event: {burn_created:?}");
                    }

                    if let (_, _, _, Some(burn_ready), _) = &event {
                        info!("  burn ready event: {burn_ready:?}");
                    }

                    if let (_, _, _, _, Some(burn_expired)) = &event {
                        info!("  burn expired event: {burn_expired:?}");
                    }
                }
                _ => (),
            }
        }
    }
}
