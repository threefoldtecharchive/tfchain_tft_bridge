package bridge

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
	"github.com/threefoldtech/tfchain_bridge/pkg"
	subpkg "github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

func (bridge *Bridge) handleWithdrawCreated(ctx context.Context, withdraw subpkg.WithdrawCreatedEvent) error {
	withdrawn, err := bridge.subClient.IsAlreadyWithdrawn(types.U64(withdraw.ID))
	if err != nil {
		return err
	}

	if withdrawn {
		log.Info().Uint64("ID", uint64(withdraw.ID)).Msgf("tx is already withdrawn, skipping...")
		return pkg.ErrTransactionAlreadyWithdrawn
	}

	if err := bridge.wallet.CheckAccount(withdraw.Target); err != nil {
		return bridge.handleBadWithdraw(ctx, withdraw)
	}

	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, withdraw.Target, withdraw.Amount, withdraw.ID)
	if err != nil {
		return err
	}
	log.Debug().Msgf("stellar account sequence number: %d", sequenceNumber)

	return bridge.subClient.RetryProposeWithdrawOrAddSig(ctx, withdraw.ID, withdraw.Target, big.NewInt(int64(withdraw.Amount)), signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) handlePendingWithdraw(ctx context.Context, pendingWithdraw substrate.WithdrawTransaction) error {
	if err := bridge.wallet.CheckAccount(pendingWithdraw.Target); err != nil {
		log.Info().Uint64("ID", uint64(pendingWithdraw.ID)).Msg("tx is an invalid withdraw transaction, setting withdraw as executed since we have no way to recover...")
		return bridge.subClient.RetrySetWithdrawExecuted(ctx, pendingWithdraw.ID)
	}

	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, pendingWithdraw.Target, uint64(pendingWithdraw.Amount), pendingWithdraw.ID)
	if err != nil {
		return err
	}
	log.Debug().Msgf("stellar account sequence number: %d", sequenceNumber)

	return bridge.subClient.RetryProposeWithdrawOrAddSig(ctx, pendingWithdraw.ID, pendingWithdraw.Target, big.NewInt(int64(pendingWithdraw.Amount)), signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) handleWithdrawExpired(ctx context.Context, withdrawExpired subpkg.WithdrawExpiredEvent) error {
	if err := bridge.wallet.CheckAccount(withdrawExpired.Target); err != nil {
		log.Info().Uint64("ID", uint64(withdrawExpired.ID)).Msg("tx is an invalid withdraw transaction, setting withdraw as executed since we have no way to recover...")
		return bridge.subClient.RetrySetWithdrawExecuted(ctx, withdrawExpired.ID)
	}

	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, withdrawExpired.Target, withdrawExpired.Amount, withdrawExpired.ID)
	if err != nil {
		return err
	}
	log.Debug().Msgf("stellar account sequence number: %d", sequenceNumber)

	return bridge.subClient.RetryProposeWithdrawOrAddSig(ctx, withdrawExpired.ID, withdrawExpired.Target, big.NewInt(int64(withdrawExpired.Amount)), signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) handleWithdrawReady(ctx context.Context, withdrawReady subpkg.WithdrawReadyEvent) error {
	withdrawn, err := bridge.subClient.IsAlreadyWithdrawn(types.U64(withdrawReady.ID))
	if err != nil {
		return err
	}

	if withdrawn {
		log.Info().Uint64("ID", uint64(withdrawReady.ID)).Msg("tx is already withdrawn, skipping...")
		return pkg.ErrTransactionAlreadyWithdrawn
	}

	withdrawTx, err := bridge.subClient.GetWithdrawTransaction(types.U64(withdrawReady.ID))
	if err != nil {
		return err
	}

	if len(withdrawTx.Signatures) == 0 {
		log.Info().Msg("found 0 signatures, aborting")
		return pkg.ErrNoSignatures
	}

	// todo add memo hash
	err = bridge.wallet.CreatePaymentWithSignaturesAndSubmit(ctx, withdrawTx.Target, uint64(withdrawTx.Amount), "", withdrawTx.Signatures, int64(withdrawTx.SequenceNumber))
	if err != nil {
		return err
	}

	return bridge.subClient.RetrySetWithdrawExecuted(ctx, withdrawReady.ID)
}

func (bridge *Bridge) handleBadWithdraw(ctx context.Context, withdraw subpkg.WithdrawCreatedEvent) error {
	log.Info().Uint64("ID", uint64(withdraw.ID)).Msg("tx is an invalid withdraw transaction, minting on chain again...")
	mintID := fmt.Sprintf("refund-%d", withdraw.ID)

	minted, err := bridge.subClient.IsAlreadyMinted(mintID)
	if err != nil {
		if !errors.Is(err, substrate.ErrMintTransactionNotFound) {
			return err
		}
	}

	if minted {
		log.Debug().Str("txHash", mintID).Msg("transaction is already minted")
		return pkg.ErrTransactionAlreadyMinted
	}

	log.Info().Str("mintID", mintID).Msg("going to propose mint transaction")
	err = bridge.subClient.RetryProposeMintOrVote(ctx, mintID, substrate.AccountID(withdraw.Source), big.NewInt(int64(withdraw.Amount)))
	if err != nil {
		return err
	}

	log.Info().Uint64("ID", uint64(withdraw.ID)).Msg("setting invalid withdraw transaction as executed")
	return bridge.subClient.RetrySetWithdrawExecuted(ctx, withdraw.ID)
}
