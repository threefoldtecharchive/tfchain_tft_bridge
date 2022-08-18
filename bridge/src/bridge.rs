use tfchain_client::client::{self, TfchainClient};
pub struct Bridge {
    tfchain_client: client::TfchainClient,
}
use subxt::{Config, PolkadotConfig};
type Hash = <PolkadotConfig as Config>::Hash;
use tfchain_client::runtimes::devnet::{
    BurnTransactionReady, BurnTransactionSignatureAdded, MintTransactionProposed,
};

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

    pub async fn get_events(&self) {
        let b = "7badc36b2ccc0bd8832f2a2c31c4d7fa6d96b2316ea6efd20a9765ce26ca9a66";
        let x = hex::decode(b).unwrap();
        let hash = Hash::from_slice(&x);
        let events = self
            .tfchain_client
            .api
            .events()
            .at(Some(hash))
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
                MintTransactionProposed,
                BurnTransactionSignatureAdded,
                BurnTransactionReady,
            )>();

        while let Some(ev) = bridge_events.next().await {
            match ev {
                Ok(event_details) => {
                    let block_hash = event_details.block_hash;
                    let event = event_details.event;
                    println!("Event at {:?}:", block_hash);

                    if let (Some(mint), _, _) = &event {
                        println!("  Mint event: {mint:?}");
                    }

                    if let (_, Some(burn_signature_added), _) = &event {
                        println!("  Burn sig added event: {burn_signature_added:?}");
                    }

                    if let (_, _, Some(burn_ready)) = &event {
                        println!("  Burn ready event: {burn_ready:?}");
                    }
                }
                _ => (),
            }
        }
    }
}
