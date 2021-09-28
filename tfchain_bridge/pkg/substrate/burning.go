package substrate

import (
	"fmt"
	"math/big"

	"github.com/centrifuge/go-substrate-rpc-client/v3/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/tfchain_bridge/pkg"
)

var (
	ErrBurnTransactionNotFound   = fmt.Errorf("burn tx not found")
	ErrRefundTransactionNotFound = fmt.Errorf("refund tx not found")
	ErrFailedToDecode            = fmt.Errorf("failed to decode events, skipping")
)

type BurnTransaction struct {
	Block      types.U32
	Amount     types.U64
	Target     AccountID
	Signatures []pkg.StellarSignature
}

func unsubscribe(sub *state.StorageSubscription) {
	log.Debug().Msg("unsubscribing from tfchain")
	sub.Unsubscribe()
}

func (s *SubstrateClient) ProposeBurnTransactionOrAddSig(identity *Identity, txID uint64, target AccountID, amount *big.Int, signature string, stellarAddress string) error {
	c, err := types.NewCall(s.meta, "TFTBridgeModule.propose_burn_transaction_or_add_sig",
		txID, target, types.U64(amount.Uint64()), signature, stellarAddress,
	)

	if err != nil {
		return errors.Wrap(err, "failed to create call")
	}

	if _, err := s.call(identity, c); err != nil {
		return errors.Wrap(err, "failed to propose or add sig for a burn transaction")
	}

	return nil
}

func (s *SubstrateClient) SetBurnTransactionExecuted(identity *Identity, txID uint64) error {
	log.Debug().Msg("setting burn transaction as executed")
	c, err := types.NewCall(s.meta, "TFTBridgeModule.set_burn_transaction_executed", txID)

	if err != nil {
		return errors.Wrap(err, "failed to create call")
	}

	if _, err := s.call(identity, c); err != nil {
		return errors.Wrap(err, "failed to set burn transaction executed")
	}

	return nil
}

func (s *SubstrateClient) GetBurnTransaction(identity *Identity, burnTransactionID types.U64) (*BurnTransaction, error) {
	log.Debug().Msgf("trying to retrieve burn transaction with id: %s", burnTransactionID)
	bytes, err := types.EncodeToBytes(burnTransactionID)
	if err != nil {
		return nil, errors.Wrap(err, "substrate: encoding error building query arguments")
	}

	var burnTx BurnTransaction
	key, err := types.CreateStorageKey(s.meta, "TFTBridgeModule", "BurnTransactions", bytes, nil)
	if err != nil {
		err = errors.Wrap(err, "failed to create storage key")
		return nil, err
	}

	ok, err := s.cl.RPC.State.GetStorageLatest(key, &burnTx)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, ErrBurnTransactionNotFound
	}

	return &burnTx, nil
}

func (s *SubstrateClient) IsBurnedAlready(identity *Identity, burnTransactionID types.U64) (exists bool, err error) {
	log.Debug().Msgf("trying to retrieve executed burn transaction with id: %s", burnTransactionID)
	bytes, err := types.EncodeToBytes(burnTransactionID)
	if err != nil {
		return false, errors.Wrap(err, "substrate: encoding error building query arguments")
	}

	var burnTx BurnTransaction
	key, err := types.CreateStorageKey(s.meta, "TFTBridgeModule", "ExecutedBurnTransactions", bytes, nil)
	if err != nil {
		err = errors.Wrap(err, "failed to create storage key")
		return
	}

	ok, err := s.cl.RPC.State.GetStorageLatest(key, &burnTx)
	if err != nil {
		return false, err
	}

	if !ok {
		return false, nil
	}

	return true, nil
}
