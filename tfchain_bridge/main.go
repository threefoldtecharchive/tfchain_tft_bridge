package main

import (
	"context"

	flag "github.com/spf13/pflag"
	"github.com/threefoldtech/tfchain_bridge/pkg"
	"github.com/threefoldtech/tfchain_bridge/pkg/bridge"
)

func main() {
	var bridgeCfg pkg.BridgeConfig

	flag.StringVar(&bridgeCfg.TfchainURL, "tfchainurl", "", "Tfchain websocket url")
	flag.StringVar(&bridgeCfg.TfchainSeed, "tfchainseed", "", "Tfchain secret seed")
	flag.StringVar(&bridgeCfg.StellarBridgeAccount, "bridgewallet", "", "stellar bridge wallet")
	flag.StringVar(&bridgeCfg.StellarSeed, "secret", "", "stellar secret")
	flag.StringVar(&bridgeCfg.StellarNetwork, "network", "testnet", "stellar network url")

	flag.Parse()

	_, err := bridge.NewBridge(context.Background(), bridgeCfg)
	if err != nil {
		panic(err)
	}
}
