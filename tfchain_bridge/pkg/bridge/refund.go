package bridge

import (
	"context"

	"github.com/rs/zerolog/log"
	hProtocol "github.com/stellar/go/protocols/horizon"
	"github.com/threefoldtech/tfchain_bridge/pkg"
	subpkg "github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

// refund handler for stellar
func (bridge *Bridge) refund(ctx context.Context, destination string, amount int64, tx hProtocol.Transaction) error {
	err := bridge.handleRefundExpired(ctx, subpkg.RefundTransactionExpiredEvent{
		Hash:   tx.Hash,
		Amount: uint64(amount),
		Target: destination,
	})
	if err != nil {
		return err
	}

	// save cursor
	cursor := tx.PagingToken()
	log.Info().Msgf("saving cursor now %s", cursor)
	err = bridge.blockPersistency.SaveStellarCursor(cursor)
	if err != nil {
		log.Error().Msgf("error while saving cursor:", err.Error())
		return err
	}
	return nil
}

func (bridge *Bridge) handleRefundExpired(ctx context.Context, refundExpiredEvent subpkg.RefundTransactionExpiredEvent) error {
	refunded, err := bridge.subClient.IsRefundedAlready(bridge.subClient.Identity, refundExpiredEvent.Hash)
	if err != nil {
		return err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", refundExpiredEvent.Hash)
		return pkg.ErrTransactionAlreadyRefunded
	}

	signature, sequenceNumber, err := bridge.wallet.CreateRefundAndReturnSignature(ctx, refundExpiredEvent.Target, refundExpiredEvent.Amount, refundExpiredEvent.Hash)
	if err != nil {
		return err
	}

	call, err := bridge.subClient.CreateRefundTransactionOrAddSig(bridge.subClient.Identity, refundExpiredEvent.Hash, refundExpiredEvent.Target, int64(refundExpiredEvent.Amount), signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
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

func (bridge *Bridge) handleRefundReady(ctx context.Context, refundReadyEvent subpkg.RefundTransactionReadyEvent) error {
	refunded, err := bridge.subClient.IsRefundedAlready(bridge.subClient.Identity, refundReadyEvent.Hash)
	if err != nil {
		return err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", refundReadyEvent.Hash)
		return pkg.ErrTransactionAlreadyRefunded
	}

	refund, err := bridge.subClient.GetRefundTransaction(bridge.subClient.Identity, refundReadyEvent.Hash)
	if err != nil {
		return err
	}

	err = bridge.wallet.CreateRefundPaymentWithSignaturesAndSubmit(ctx, refund.Target, uint64(refund.Amount), refund.TxHash, refund.Signatures, int64(refund.SequenceNumber))
	if err != nil {
		return err
	}

	call, err := bridge.subClient.SetRefundTransactionExecuted(bridge.subClient.Identity, refund.TxHash)
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
