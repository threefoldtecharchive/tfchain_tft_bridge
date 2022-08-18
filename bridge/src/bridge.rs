use tfchain_client::client::{self, TfchainClient};
pub struct Bridge {
    tfchain_client: client::TfchainClient,
}
use subxt::{Config, PolkadotConfig};
type Hash = <PolkadotConfig as Config>::Hash;

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
}

// let mut transfer_events = api_client
//     .api
//     .events()
//     .subscribe()
//     .await?
//     .filter_events::<(devnet::devnet::tft_bridge_module::events::MintTransactionProposed,)>(
//     );
