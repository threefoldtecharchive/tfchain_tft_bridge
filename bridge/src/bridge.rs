use tfchain_client::client;
pub struct Bridge {
    tfchain_client: client::TfchainClient,
}

impl Bridge {
    pub async fn new() -> Bridge {
        // Create a client to use:
        let phrase =
            "oyster orient plunge devote light wrap hold mother essence casino rebel distance";
        let p = client::get_from_seed(phrase, None);
        let tfchain_client =
            client::TfchainClient::new(String::from("ws://localhost:9944"), p, "devnet")
                .await
                .expect("failed to create client");

        Bridge { tfchain_client }
    }
}

// let mut transfer_events = api_client
//     .api
//     .events()
//     .subscribe()
//     .await?
//     .filter_events::<(devnet::devnet::tft_bridge_module::events::MintTransactionProposed,)>(
//     );
