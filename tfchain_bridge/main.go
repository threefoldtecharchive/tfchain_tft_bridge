package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
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
	flag.StringVar(&bridgeCfg.PersistencyFile, "persistency", "./node.json", "file where last seen blockheight and stellar account cursor is stored")
	flag.Int64Var(&bridgeCfg.TokenMultiplier, "multiplier", 1, "multiplier indicates how many tokens * multiplier must be minted on target chain")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	timeout, timeoutCancel := context.WithTimeout(ctx, time.Second*15)
	defer timeoutCancel()

	br, err := bridge.NewBridge(timeout, bridgeCfg)
	if err != nil {
		panic(err)
	}

	if err = br.Start(ctx); err != nil {
		panic(err)
	}

	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	log.Info().Msg("awaiting signal")
	sig := <-sigs
	log.Info().Str("signal", sig.String()).Msg("signal")
	cancel()
	err = br.Close()
	if err != nil {
		panic(err)
	}

	log.Info().Msg("exiting")
	time.Sleep(time.Second * 5)
}
