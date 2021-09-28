package substrate

import (
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/tfchain_bridge/pkg"
)

// TODO: add all events from SmartContractModule and TfgridModule

// ContractCanceled is the contract canceled event
type BurnTransactionCreated struct {
	Phase             types.Phase
	BurnTransactionID types.U64
	Target            AccountID
	Amount            types.U64
	Topics            []types.Hash
}

type BurnTransactionReady struct {
	Phase             types.Phase
	BurnTransactionID types.U64
	Topics            []types.Hash
}

type BurnTransactionSignatureAdded struct {
	Phase             types.Phase
	BurnTransactionID types.U64
	Signature         pkg.StellarSignature
	Topics            []types.Hash
}

type BurnTransactionProposed struct {
	Phase             types.Phase
	BurnTransactionID types.U64
	Target            AccountID
	Amount            types.U64
	Topics            []types.Hash
}

type RefundTransactionCreated struct {
	Phase                 types.Phase
	RefundTransactionHash []byte
	Target                []byte
	Amount                types.U64
	Topics                []types.Hash
}

type RefundTransactionsignatureAdded struct {
	Phase                 types.Phase
	RefundTransactionHash []byte
	Signature             pkg.StellarSignature
	Topics                []types.Hash
}

type RefundTransactionReady struct {
	Phase                 types.Phase
	RefundTransactionHash []byte
	Topics                []types.Hash
}

// EventRecords is a struct that extends the default events with our events
type EventRecords struct {
	types.EventRecords
	TFTBridgeModule_BurnTransactionCreated          []BurnTransactionCreated          //nolint:stylecheck,golint
	TFTBridgeModule_BurnTransactionReady            []BurnTransactionReady            //nolint:stylecheck,golint
	TFTBridgeModule_BurnTransactionSignatureAdded   []BurnTransactionSignatureAdded   //nolint:stylecheck,golint
	TFTBridgeModule_BurnTransactionProposed         []BurnTransactionProposed         //nolint:stylecheck,golint
	TFTBridgeModule_RefundTransactionCreated        []RefundTransactionCreated        //nolint:stylecheck,golint
	TFTBridgeModule_RefundTransactionsignatureAdded []RefundTransactionsignatureAdded //nolint:stylecheck,golint
	TFTBridgeModule_RefundTransactionReady          []RefundTransactionReady          //nolint:stylecheck,golint
}

func (s *SubstrateClient) SubscribeEvents(burnChan chan BurnTransactionCreated, burnReadyChan chan BurnTransactionReady, refundReadyChan chan RefundTransactionReady, blockpersistency *pkg.ChainPersistency) error {
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

		err := s.ProcessEvents(burnChan, burnReadyChan, refundReadyChan, key, []types.StorageChangeSet{set})
		if err != nil {
			log.Err(err).Msg("error while processing events")
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

func (s *SubstrateClient) ProcessEvents(burnChan chan BurnTransactionCreated, burnReadyChan chan BurnTransactionReady, refundReadyChan chan RefundTransactionReady, key types.StorageKey, changeset []types.StorageChangeSet) error {
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
				fmt.Println(err)
				log.Err(ErrFailedToDecode)
			}

			for _, e := range events.TFTBridgeModule_RefundTransactionReady {
				refundReadyChan <- e
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
