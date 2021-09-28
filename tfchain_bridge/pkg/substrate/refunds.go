package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/tfchain_bridge/pkg"
)

type RefundTransaction struct {
	Block      types.U32
	Amount     types.U64
	Target     string
	TxHash     string
	Signatures []pkg.StellarSignature
}

func (s *SubstrateClient) CreateRefundTransactionOrAddSig(identity *Identity, tx_hash string, target string, amount int64, signature string, stellarAddress string) error {
	c, err := types.NewCall(s.meta, "TFTBridgeModule.create_refund_transaction_or_add_sig",
		tx_hash, target, types.U64(amount), signature, stellarAddress,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create call")
	}

	if _, err := s.call(identity, c); err != nil {
		return errors.Wrap(err, "failed to propose or add sig for a refund transaction")
	}

	return nil
}

func (s *SubstrateClient) SetRefundTransactionExecuted(identity *Identity, txHash string) error {
	log.Debug().Msg("setting refund transaction as executed")
	c, err := types.NewCall(s.meta, "TFTBridgeModule.set_refund_transaction_executed", txHash)

	if err != nil {
		return errors.Wrap(err, "failed to create call")
	}

	if _, err := s.call(identity, c); err != nil {
		return errors.Wrap(err, "failed to set refund transaction executed")
	}

	return nil
}

func (s *SubstrateClient) IsRefundedAlready(identity *Identity, txHash string) (exists bool, err error) {
	log.Debug().Msgf("trying to retrieve executed refund transaction with id: %s", txHash)
	bytes, err := types.EncodeToBytes(txHash)
	if err != nil {
		return false, errors.Wrap(err, "substrate: encoding error building query arguments")
	}

	var refundTx RefundTransaction
	key, err := types.CreateStorageKey(s.meta, "TFTBridgeModule", "ExecutedRefundTransactions", bytes, nil)
	if err != nil {
		err = errors.Wrap(err, "failed to create storage key")
		return
	}

	ok, err := s.cl.RPC.State.GetStorageLatest(key, &refundTx)
	if err != nil {
		return false, err
	}

	if !ok {
		return false, nil
	}

	return true, nil
}

func (s *SubstrateClient) GetRefundTransaction(identity *Identity, txHash string) (*RefundTransaction, error) {
	log.Debug().Msgf("trying to retrieve refund transaction with tx_hash: %s", txHash)
	bytes, err := types.EncodeToBytes(txHash)
	if err != nil {
		return nil, errors.Wrap(err, "substrate: encoding error building query arguments")
	}

	var refundTx RefundTransaction
	key, err := types.CreateStorageKey(s.meta, "TFTBridgeModule", "RefundTransactions", bytes, nil)
	if err != nil {
		err = errors.Wrap(err, "failed to create storage key")
		return nil, err
	}

	ok, err := s.cl.RPC.State.GetStorageLatest(key, &refundTx)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, ErrBurnTransactionNotFound
	}

	return &refundTx, nil
}
