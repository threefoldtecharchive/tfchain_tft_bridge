package bridge

import (
	"context"
	"fmt"
	"math/big"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
	subpkg "github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

func (bridge *Bridge) handleWithdrawCreated(ctx context.Context, withdraw subpkg.WithdrawCreatedEvent) error {
	burned, err := bridge.subClient.IsBurnedAlready(bridge.subClient.Identity, types.U64(withdraw.ID))
	if err != nil {
		return err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", withdraw.ID)
		return errors.New("tx burned already")
	}

	if err := bridge.wallet.CheckAccount(withdraw.Target); err != nil {
		log.Info().Msgf("tx with id: %d is an invalid burn transaction, minting on chain again...", withdraw.ID)
		mintID := fmt.Sprintf("refund-%d", withdraw.ID)
		err := bridge.handleMint(big.NewInt(int64(withdraw.Amount)), substrate.AccountID(withdraw.Source), mintID)
		if err != nil {
			return err
		}
		log.Info().Msgf("setting invalid burn transaction (%d) as executed", withdraw.ID)
		call, err := bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdraw.ID)
		if err != nil {
			return err
		}
		hash, err := bridge.subClient.CallExtrinsic(call)
		if err != nil {
			return err
		}
		log.Info().Msgf("call submitted with hash %s", hash)
		return nil
	}

	amount := big.NewInt(int64(withdraw.Amount))
	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, withdraw.Target, amount.Uint64(), withdraw.ID)
	if err != nil {
		return err
	}
	log.Info().Msgf("stellar account sequence number: %d", sequenceNumber)

	call, err := bridge.subClient.ProposeBurnTransactionOrAddSig(bridge.subClient.Identity, withdraw.ID, withdraw.Target, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
	if err != nil {
		return err
	}
	hash, err := bridge.subClient.CallExtrinsic(call)
	if err != nil {
		return err
	}
	log.Info().Msgf("call submitted with hash %s", hash.Hex())

	return nil
}

func (bridge *Bridge) handleWithdrawExpired(ctx context.Context, withdrawExpired subpkg.WithdrawExpiredEvent) error {
	if err := bridge.wallet.CheckAccount(withdrawExpired.Target); err != nil {
		log.Info().Msgf("tx with id: %d is an invalid burn transaction, setting burn as executed since we have no way to recover...", withdrawExpired.ID)
		call, err := bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdrawExpired.ID)
		if err != nil {
			return err
		}
		hash, err := bridge.subClient.CallExtrinsic(call)
		if err != nil {
			return err
		}
		log.Info().Msgf("call submitted with hash %s", hash)
		return nil
	}

	amount := big.NewInt(int64(withdrawExpired.Amount))
	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, withdrawExpired.Target, amount.Uint64(), withdrawExpired.ID)
	if err != nil {
		return err
	}
	log.Info().Msgf("stellar account sequence number: %d", sequenceNumber)

	call, err := bridge.subClient.ProposeBurnTransactionOrAddSig(bridge.subClient.Identity, withdrawExpired.ID, withdrawExpired.Target, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
	if err != nil {
		return err
	}
	hash, err := bridge.subClient.CallExtrinsic(call)
	if err != nil {
		return err
	}
	log.Info().Msgf("call submitted with hash %s", hash.Hex())
	return nil
}

func (bridge *Bridge) handleWithdrawReady(ctx context.Context, withdrawReady subpkg.WithdrawReadyEvent) error {
	burned, err := bridge.subClient.IsBurnedAlready(bridge.subClient.Identity, types.U64(withdrawReady.ID))
	if err != nil {
		return err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", withdrawReady.ID)
		return errors.New("tx burned already")
	}

	burnTx, err := bridge.subClient.GetBurnTransaction(bridge.subClient.Identity, types.U64(withdrawReady.ID))
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

	call, err := bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Identity, withdrawReady.ID)
	if err != nil {
		return err
	}
	hash, err := bridge.subClient.CallExtrinsic(call)
	if err != nil {
		return err
	}
	log.Info().Msgf("call submitted with hash %s", hash.Hex())
	return nil
}
