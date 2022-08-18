use clap::{AppSettings, Parser};
// use codec::Decode;
// use futures::StreamExt;
// use std::time::Duration;

pub mod bridge;

#[derive(Parser, Debug)]
#[clap(about = "Tfchain bridge with Stellar.", version, author)]
#[clap(global_setting(AppSettings::DeriveDisplayOrder))]
pub struct Args {
    #[clap(
        long,
        help = "Dev mode (equivalent to `--use-dev-key --mnemonic='//Alice'`)"
    )]
    dev: bool,
}

pub async fn bridge_main() {
    env_logger::builder()
        .filter_level(log::LevelFilter::Info)
        .parse_default_env()
        .init();

    let args = Args::from_args();

    let b = bridge::Bridge::new().await;

    // b.get_events().await;
    b.subscribe_events().await;
    println!("args: {:?}", args);
}
