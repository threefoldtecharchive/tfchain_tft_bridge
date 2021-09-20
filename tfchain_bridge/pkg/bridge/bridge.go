package bridge

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/threefoldtech/tfchain_bridge/pkg"
	"github.com/threefoldtech/tfchain_bridge/pkg/stellar"
	"github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

const (
	// Depositing from Stellar to smart chain fee
	DepositFee = 50 * 1e7
	// Withdrawing from smartchain to Stellar fee
	WithdrawFee   = int64(1 * 1e7)
	BridgeNetwork = "stellar"
)

// Bridge is a high lvl structure which listens on contract events and bridge-related
// stellar transactions, and handles them
type Bridge struct {
	wallet           *stellar.StellarWallet
	subClient        *substrate.Substrate
	blockPersistency *pkg.ChainPersistency
	mut              sync.Mutex
	config           *pkg.BridgeConfig
	depositFee       *big.Int
}

func NewBridge(ctx context.Context, cfg pkg.BridgeConfig) (*Bridge, error) {
	subClient, err := substrate.NewSubstrate(cfg.TfchainURL)
	if err != nil {
		return nil, err
	}

	tfchainIdentity, err := substrate.IdentityFromPhrase(cfg.TfchainSeed)
	if err != nil {
		return nil, err
	}

	isValidator, err := subClient.IsValidator(&tfchainIdentity)
	if err != nil {
		return nil, err
	}

	if !isValidator {
		return nil, fmt.Errorf("account provided is not a validator for the bridge runtime")
	}

	blockPersistency, err := pkg.InitPersist(cfg.PersistencyFile)
	if err != nil {
		return nil, err
	}

	wallet, err := stellar.NewStellarWallet(ctx, &cfg.StellarConfig)
	if err != nil {
		return nil, err
	}

	if cfg.RescanBridgeAccount {
		// saving the cursor to 0 will trigger the bridge stellar account
		// to scan for every transaction ever made on the bridge account
		// and mint accordingly
		err = blockPersistency.SaveStellarCursor("0")
		if err != nil {
			return nil, err
		}
	}

	var depositFee big.Int
	depositFee.SetInt64(DepositFee)
	bridge := &Bridge{
		subClient:        subClient,
		blockPersistency: blockPersistency,
		wallet:           wallet,
		config:           &cfg,
		depositFee:       &depositFee,
	}

	return bridge, nil
}
