package bridge

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
	subpkg "github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

func (bridge *Bridge) handleWithdrawCreated(ctx context.Context, withdraw subpkg.WithdrawCreatedEvent) error {
	burned, err := bridge.subClient.IsBurnedAlready(types.U64(withdraw.ID))
	if err != nil {
		return err
	}

	if burned {
		log.Info().Uint64("ID", uint64(withdraw.ID)).Msgf("tx is burned already, skipping...")
		return errors.New("tx burned already")
	}

	if err := bridge.wallet.CheckAccount(withdraw.Target); err != nil {
		log.Info().Uint64("ID", uint64(withdraw.ID)).Msg("tx is an invalid burn transaction, minting on chain again...")
		mintID := fmt.Sprintf("refund-%d", withdraw.ID)
		err := bridge.handleMint(big.NewInt(int64(withdraw.Amount)), substrate.AccountID(withdraw.Source), mintID)
		for err != nil {
			log.Err(err).Msg("error while minting on chain again for invalid withdraw")

			select {
			case <-ctx.Done():
				return err
			case <-time.After(10 * time.Second):
				err = bridge.handleMint(big.NewInt(int64(withdraw.Amount)), substrate.AccountID(withdraw.Source), mintID)
			}
		}
		if err != nil {
			return errors.Wrap(err, "failed to handle mint on chain again for invalid withdraw")
		}

		log.Info().Uint64("ID", uint64(withdraw.ID)).Msg("setting invalid burn transaction as executed")
		err = bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdraw.ID)
		for err != nil {
			log.Err(err).Msg("error while setting burn transaction as executed")

			select {
			case <-ctx.Done():
				return err
			case <-time.After(10 * time.Second):
				err = bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdraw.ID)
			}
		}
		if err != nil {
			return errors.Wrap(err, "failed to handle setting burn transaction as executed")
		}

		return nil
	}

	amount := big.NewInt(int64(withdraw.Amount))
	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, withdraw.Target, amount.Uint64(), withdraw.ID)
	if err != nil {
		return err
	}
	log.Info().Msgf("stellar account sequence number: %d", sequenceNumber)

	return bridge.subClient.ProposeBurnTransactionOrAddSig(bridge.subClient.Identity, withdraw.ID, withdraw.Target, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) handleWithdrawExpired(ctx context.Context, withdrawExpired subpkg.WithdrawExpiredEvent) error {
	if err := bridge.wallet.CheckAccount(withdrawExpired.Target); err != nil {
		log.Info().Uint64("ID", uint64(withdrawExpired.ID)).Msg("tx is an invalid burn transaction, setting burn as executed since we have no way to recover...")
		err = bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdrawExpired.ID)
		for err != nil {
			log.Err(err).Msg("error while setting burn transaction as executed")

			select {
			case <-ctx.Done():
				return err
			case <-time.After(10 * time.Second):
				err = bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdrawExpired.ID)
			}
		}
		if err != nil {
			return errors.Wrap(err, "failed to handle setting burn transaction as executed")
		}

		return nil
	}

	amount := big.NewInt(int64(withdrawExpired.Amount))
	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, withdrawExpired.Target, amount.Uint64(), withdrawExpired.ID)
	if err != nil {
		return err
	}
	log.Info().Msgf("stellar account sequence number: %d", sequenceNumber)

	return bridge.subClient.ProposeBurnTransactionOrAddSig(bridge.subClient.Identity, withdrawExpired.ID, withdrawExpired.Target, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) handleWithdrawReady(ctx context.Context, withdrawReady subpkg.WithdrawReadyEvent) error {
	burned, err := bridge.subClient.IsBurnedAlready(types.U64(withdrawReady.ID))
	if err != nil {
		return nil
	}

	if burned {
		log.Info().Uint64("ID", uint64(withdrawReady.ID)).Msg("tx is burned already, skipping...")
		return nil
	}

	burnTx, err := bridge.subClient.GetBurnTransaction(types.U64(withdrawReady.ID))
	if err != nil {
		return err
	}

	if len(burnTx.Signatures) == 0 {
		log.Info().Msg("found 0 signatures, aborting")
		return errors.New("no signatures")
	}

	// todo add memo hash
	err = bridge.wallet.CreatePaymentWithSignaturesAndSubmit(ctx, burnTx.Target, uint64(burnTx.Amount), "", burnTx.Signatures, int64(burnTx.SequenceNumber))
	if err != nil {
		return err
	}

	return bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdrawReady.ID)
}
