package pkg

import "errors"

type BridgeConfig struct {
	TfchainURL          string
	TfchainSeed         string
	RescanBridgeAccount bool
	PersistencyFile     string
	// Interval in number of blocks where withdraws / refunds are retried
	RetryInterval uint64
	StellarConfig
}

type StellarConfig struct {
	// stellar account to monitor
	StellarBridgeAccount string
	// network for the stellar config
	StellarNetwork string
	// seed for the stellar bridge wallet
	StellarSeed string
	// url for stellar horizon
	StellarHorizonUrl string
}

type StellarSignature struct {
	Signature      []byte
	StellarAddress []byte
}

var ErrTransactionAlreadyRefunded = errors.New("transaction is already refunded")
var ErrTransactionAlreadyMinted = errors.New("transaction is already minted")
var ErrTransactionAlreadyWithdrawn = errors.New("transaction is already withdrawn")
var ErrNoSignatures = errors.New("transaction has no signatures")
