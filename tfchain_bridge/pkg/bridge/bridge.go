package bridge

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/tfchain_bridge/pkg"
	"github.com/threefoldtech/tfchain_bridge/pkg/stellar"
	subpkg "github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

const (
	BridgeNetwork = "stellar"
)

// Bridge is a high lvl structure which listens on contract events and bridge-related
// stellar transactions, and handles them
type Bridge struct {
	wallet           *stellar.StellarWallet
	subClient        *subpkg.SubstrateClient
	blockPersistency *pkg.ChainPersistency
	config           *pkg.BridgeConfig
	depositFee       int64
}

func NewBridge(ctx context.Context, cfg pkg.BridgeConfig) (*Bridge, error) {
	subClient, err := subpkg.NewSubstrateClient(cfg.TfchainURL, cfg.TfchainSeed)
	if err != nil {
		return nil, err
	}

	isValidator, err := subClient.IsValidator(subClient.Identity)
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
		err = blockPersistency.SaveHeight(0)
		if err != nil {
			return nil, err
		}
	}

	// fetch the configured depositfee
	depositFee, err := subClient.GetDepositFee(subClient.Identity)
	if err != nil {
		return nil, err
	}

	bridge := &Bridge{
		subClient:        subClient,
		blockPersistency: blockPersistency,
		wallet:           wallet,
		config:           &cfg,
		depositFee:       depositFee,
	}

	return bridge, nil
}

func (bridge *Bridge) Start(ctx context.Context) error {
	height, err := bridge.blockPersistency.GetHeight()
	if err != nil {
		return errors.Wrap(err, "failed to get block height from persistency")
	}
	log.Info().Msg("starting stellar subscription...")
	stellarSub, err := bridge.wallet.StreamBridgeStellarTransactions(ctx, height.StellarCursor)
	if err != nil {
		return errors.Wrap(err, "failed to monitor bridge account")
	}

	log.Info().Msg("starting tfchain subscription...")
	tfchainSub, err := bridge.subClient.SubscribeTfchainBridgeEvents(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to subscribe to tfchain")
	}

	for {
		select {
		case data := <-tfchainSub:
			if data.Err != nil {
				return errors.Wrap(err, "failed to process events")
			}
			for _, withdrawCreatedEvent := range data.Events.WithdrawCreatedEvents {
				err := bridge.handleWithdrawCreated(ctx, withdrawCreatedEvent)
				if err != nil {
					return errors.Wrap(err, "failed to handle withdraw created")
				}
			}
			for _, withdrawExpiredEvent := range data.Events.WithdrawExpiredEvents {
				err := bridge.handleWithdrawExpired(ctx, withdrawExpiredEvent)
				if err != nil {
					return errors.Wrap(err, "failed to handle withdraw expired")
				}
			}
			for _, withdawReadyEvent := range data.Events.WithdrawReadyEvents {
				err := bridge.handleWithdrawReady(ctx, withdawReadyEvent)
				if err != nil {
					return errors.Wrap(err, "failed to handle withdraw ready")
				}
			}
			for _, refundReadyEvent := range data.Events.RefundReadyEvents {
				err := bridge.handleRefundReady(ctx, refundReadyEvent)
				if err != nil {
					return errors.Wrap(err, "failed to handle refund ready")
				}
			}
			for _, refundExpiredEvent := range data.Events.RefundExpiredEvents {
				err := bridge.handleRefundExpired(ctx, refundExpiredEvent)
				if err != nil {
					return errors.Wrap(err, "failed to handle refund expired")
				}
			}
		case data := <-stellarSub:
			if data.Err != nil {
				return errors.Wrap(err, "failed to get mint events")
			}

			for _, mEvent := range data.Events {
				err := bridge.mint(mEvent.Senders, mEvent.Tx)
				for err != nil {
					log.Err(err).Msg("Error occured while minting")
					if errors.Is(err, pkg.ErrTransactionAlreadyRefunded) {
						continue
					}

					select {
					case <-ctx.Done():
						return err
					case <-time.After(10 * time.Second):
						err = bridge.mint(mEvent.Senders, mEvent.Tx)
					}
				}
				if err != nil {
					return errors.Wrap(err, "failed to handle mint")
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
