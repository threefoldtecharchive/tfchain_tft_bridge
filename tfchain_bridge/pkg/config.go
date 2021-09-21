package pkg

import "errors"

type BridgeConfig struct {
	TfchainURL          string
	TfchainSeed         string
	RescanBridgeAccount bool
	PersistencyFile     string
	StellarConfig
}

type StellarConfig struct {
	// stellar account to monitor
	StellarBridgeAccount string
	// network for the stellar config
	StellarNetwork string
	// seed for the stellar bridge wallet
	StellarSeed      string
	StellarFeeWallet string
}

var ErrTransactionAlreadyMinted = errors.New("transaction is already minted")
