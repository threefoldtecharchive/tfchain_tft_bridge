package substrate

import (
	"context"

	"github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
)

func (s *SubstrateClient) SubscribeEvents() (*state.StorageSubscription, types.StorageKey, error) {
	cl, meta, err := s.GetClient()
	if err != nil {
		return nil, nil, err
	}

	// Subscribe to system events via storage
	key, err := types.CreateStorageKey(meta, "System", "Events", nil)
	if err != nil {
		return nil, nil, err
	}

	sub, err := cl.RPC.State.SubscribeStorageRaw([]types.StorageKey{key})
	if err != nil {
		return nil, nil, err
	}
	// defer unsubscribe(sub)
	return sub, key, err
}

func (client *SubstrateClient) ProcessEventsForHeight(height uint32) error {
	log.Info().Msgf("fetching events for blockheight %d", height)
	records, err := client.GetEventsForBlock(height)
	if err != nil {
		log.Err(err).Msgf("failed to decode block with height %d", height)
		return err
	}

	err = client.Handle(records)
	if err != nil {
		if err == substrate.ErrFailedToDecode {
			log.Err(err).Msgf("failed to decode events at block %d", height)
			return err
		} else {
			return err
		}
	}

	return nil
}

func (client *SubstrateClient) Handle(events *substrate.EventRecords) error {
	for _, e := range events.TFTBridgeModule_RefundTransactionReady {
		log.Info().Msg("found refund transaction ready event")
		call, err := client.transactor.SubmitRefundTransaction(context.Background(), e)
		if err != nil {
			log.Err(err).Msgf("error occured while processing refund transaction ready")
			continue
		}
		client.CallChan <- call
	}

	for _, e := range events.TFTBridgeModule_BurnTransactionCreated {
		log.Info().Uint64("ID", uint64(e.BurnTransactionID)).Msg("found burn transaction created event")
		call, err := client.transactor.ProposeBurnTransaction(context.Background(), e)
		if err != nil {
			log.Err(err).Msgf("error occured while processing burn transaction created")
			continue
		}
		client.CallChan <- call
	}

	for _, e := range events.TFTBridgeModule_BurnTransactionReady {
		log.Info().Uint64("ID", uint64(e.BurnTransactionID)).Msg("found burn transaction ready event")
		call, err := client.transactor.SubmitBurnTransaction(context.Background(), e)
		if err != nil {
			log.Err(err).Msgf("error occured while processing burn transaction ready")
			continue
		}
		client.CallChan <- call
	}

	for _, e := range events.TFTBridgeModule_BurnTransactionExpired {
		log.Info().Uint64("ID", uint64(e.BurnTransactionID)).Msg("found burn transaction expired event")
		call, err := client.transactor.ProposeBurnTransaction(context.Background(), e)
		if err != nil {
			log.Err(err).Msgf("error occured while processing burn transaction expired")
			continue
		}
		client.CallChan <- call
	}

	for _, e := range events.TFTBridgeModule_RefundTransactionExpired {
		log.Info().Msgf("found expired refund transaction")
		call, err := client.transactor.CreateRefund(context.Background(), e)
		if err != nil {
			log.Err(err).Msgf("error occured while processing refund transaction expired")
			continue
		}
		client.CallChan <- call
	}

	return nil
}
