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
	ErrBurnTransactionNotFound = fmt.Errorf("burn tx not found")
	ErrFailedToDecode          = fmt.Errorf("failed to decode events, skipping")
)

type BurnTransaction struct {
	Block      types.U32
	Amount     types.U64
	Target     AccountID
	Signatures []pkg.StellarSignature
}

func (s *Substrate) SubscribeBurnEvents(burnChan chan BurnTransactionCreated, burnReadyChan chan BurnTransactionReady, blockpersistency *pkg.ChainPersistency) error {
	// Subscribe to system events via storage
	key, err := types.CreateStorageKey(s.meta, "System", "Events", nil)
	if err != nil {
		return err
	}

	sub, err := s.cl.RPC.State.SubscribeStorageRaw([]types.StorageKey{key})
	if err != nil {
		return err
	}
	defer unsubscribe(sub)

	// outer for loop for subscription notifications
	for {
		set := <-sub.Chan()
		// inner loop for the changes within one of those notifications

		err := s.ProcessBurnEvents(burnChan, burnReadyChan, key, []types.StorageChangeSet{set})
		if err != nil {
			log.Err(err).Msg("error while processing burn events")
		}

		bl, err := s.cl.RPC.Chain.GetBlock(set.Block)
		if err != nil {
			return err
		}
		log.Info().Msgf("events for blockheight %+v processed, saving blockheight to persistency file now...", bl.Block.Header.Number)
		err = blockpersistency.SaveHeight(uint32(bl.Block.Header.Number))
		if err != nil {
			return err
		}
	}
}

func (s *Substrate) ProcessBurnEvents(burnChan chan BurnTransactionCreated, burnReadyChan chan BurnTransactionReady, key types.StorageKey, changeset []types.StorageChangeSet) error {
	for _, set := range changeset {
		for _, chng := range set.Changes {
			if !types.Eq(chng.StorageKey, key) || !chng.HasStorageData {
				// skip, we are only interested in events with content
				continue
			}

			// Decode the event records
			events := EventRecords{}
			err := types.EventRecordsRaw(chng.StorageData).DecodeEventRecords(s.meta, &events)
			if err != nil {
				log.Err(ErrFailedToDecode)
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionCreated {
				burnChan <- e
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionReady {
				burnReadyChan <- e
			}
		}
	}

	return nil
}

func unsubscribe(sub *state.StorageSubscription) {
	log.Info().Msg("unsubscribing from tfchain")
	sub.Unsubscribe()
}

func (s *Substrate) ProposeBurnTransactionOrAddSig(identity *Identity, txID uint64, target AccountID, amount *big.Int, signature string, stellarAddress string) error {
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

func (s *Substrate) SetBurnTransactionExecuted(identity *Identity, txID uint64) error {
	log.Info().Msg("setting burn transaction as executed")
	c, err := types.NewCall(s.meta, "TFTBridgeModule.set_burn_transaction_executed", txID)

	if err != nil {
		return errors.Wrap(err, "failed to create call")
	}

	if _, err := s.call(identity, c); err != nil {
		return errors.Wrap(err, "failed to set burn transaction executed")
	}

	return nil
}

func (s *Substrate) GetBurnTransaction(identity *Identity, burnTransactionID types.U64) (*BurnTransaction, error) {
	log.Info().Msgf("trying to retrieve burn transaction with id: %s", burnTransactionID)
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

func (s *Substrate) IsBurnedAlready(identity *Identity, burnTransactionID types.U64) (exists bool, err error) {
	log.Info().Msgf("trying to retrieve executed burn transaction with id: %s", burnTransactionID)
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
