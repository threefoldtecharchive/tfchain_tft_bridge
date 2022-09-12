package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
)

type Events struct {
	WithdrawCreatedEvents []WithdrawCreatedEvent
	WithdrawReadyEvents   []WithdrawReadyEvent
	WithdrawExpiredEvents []WithdrawExpiredEvent
	RefundCreatedEvents   []RefundTransactionCreatedEvent
	RefundReadyEvents     []RefundTransactionReadyEvent
	RefundExpiredEvents   []RefundTransactionExpiredEvent
}

type WithdrawCreatedEvent struct {
	ID     uint64
	Source types.AccountID
	Target string
	Amount uint64
}

type WithdrawReadyEvent struct {
	ID uint64
}

type WithdrawExpiredEvent struct {
	ID     uint64
	Target string
	Amount uint64
}

type RefundTransactionCreatedEvent struct {
	Hash   string
	Target string
	Amount uint64
	TxID   string
}

type RefundTransactionReadyEvent struct {
	Hash string
}

type RefundTransactionExpiredEvent struct {
	Hash   string
	Target string
	Amount uint64
}

func (client *SubstrateClient) SubscribeEvents() (*state.StorageSubscription, types.StorageKey, error) {
	cl, meta, err := client.GetClient()
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

func (client *SubstrateClient) processEventsForHeight(height uint32) error {
	log.Info().Msgf("fetching events for blockheight %d", height)
	records, err := client.GetEventsForBlock(height)
	if err != nil {
		log.Info().Msgf("failed to decode block with height %d", height)
		return err
	}

	err = client.processEventRecords(records)
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

func (client *SubstrateClient) processEventRecords(events *substrate.EventRecords) error {
	var refundTransactionReadyEvents []RefundTransactionReadyEvent
	var refundTransactionExpiredEvents []RefundTransactionExpiredEvent
	var withdrawCreatedEvents []WithdrawCreatedEvent
	var withdrawReadyEvents []WithdrawReadyEvent
	var withdrawExpiredEvents []WithdrawExpiredEvent

	for _, e := range events.TFTBridgeModule_RefundTransactionReady {
		log.Info().Msg("found refund transaction ready event")

		refundTransactionReadyEvents = append(refundTransactionReadyEvents, RefundTransactionReadyEvent{
			Hash: string(e.RefundTransactionHash),
		})
		// call, err := bridge.submitRefundTransaction(context.Background(), e)
		// if err != nil {
		// 	log.Info().Msgf("error occured: +%s", err.Error())
		// 	continue
		// }
		// hash, err := bridge.callExtrinsic(call)
		// if err != nil {
		// 	return err
		// }
		// log.Info().Msgf("submit refund call submitted with hash: %s", hash.Hex())
	}

	for _, e := range events.TFTBridgeModule_RefundTransactionExpired {
		log.Info().Msgf("found expired refund transaction")
		refundTransactionExpiredEvents = append(refundTransactionExpiredEvents, RefundTransactionExpiredEvent{
			Hash:   string(e.RefundTransactionHash),
			Target: string(e.Target),
			Amount: uint64(e.Amount),
		})
		// call, err := bridge.createRefund(context.Background(), string(e.Target), int64(e.Amount), string(e.RefundTransactionHash))
		// if err != nil {
		// 	log.Info().Msgf("error occured: +%s", err.Error())
		// 	continue
		// }
		// hash, err := bridge.callExtrinsic(call)
		// if err != nil {
		// 	return err
		// }
		// log.Info().Msgf("refund call submitted with hash: %s", hash.Hex())
	}

	for _, e := range events.TFTBridgeModule_BurnTransactionCreated {
		log.Info().Uint64("ID", uint64(e.BurnTransactionID)).Msg("found burn transaction created event")

		withdrawCreatedEvents = append(withdrawCreatedEvents, WithdrawCreatedEvent{
			ID:     uint64(e.BurnTransactionID),
			Source: e.Source,
			Target: string(e.Target),
			Amount: uint64(e.Amount),
		})
		// call, err := bridge.handleBurnCreated(context.Background(), e)
		// if err != nil {
		// 	log.Info().Msgf("error occured: +%s", err.Error())
		// 	continue
		// }
		// hash, err := bridge.callExtrinsic(call)
		// if err != nil {
		// 	return err
		// }
		// log.Info().Msgf("propose burn call submitted with hash: %s", hash.Hex())
	}

	for _, e := range events.TFTBridgeModule_BurnTransactionReady {
		log.Info().Uint64("ID", uint64(e.BurnTransactionID)).Msg("found burn transaction ready event")
		withdrawReadyEvents = append(withdrawReadyEvents, WithdrawReadyEvent{
			ID: uint64(e.BurnTransactionID),
		})
		// call, err := bridge.submitBurnTransaction(context.Background(), e)
		// if err != nil {
		// 	log.Info().Msgf("error occured: +%s", err.Error())
		// 	continue
		// }
		// hash, err := bridge.callExtrinsic(call)
		// if err != nil {
		// 	return err
		// }
		// log.Info().Msgf("submit burn call submitted with hash: %s", hash.Hex())
	}

	for _, e := range events.TFTBridgeModule_BurnTransactionExpired {
		log.Info().Uint64("ID", uint64(e.BurnTransactionID)).Msg("found burn transaction expired event")
		withdrawExpiredEvents = append(withdrawExpiredEvents, WithdrawExpiredEvent{
			ID:     uint64(e.BurnTransactionID),
			Target: string(e.Target),
			Amount: uint64(e.Amount),
		})
		// call, err := bridge.handleBurnExpired(context.Background(), e)
		// if err != nil {
		// 	log.Info().Msgf("error occured: +%s", err.Error())
		// 	continue
		// }
		// hash, err := bridge.callExtrinsic(call)
		// if err != nil {
		// 	return err
		// }
		// log.Info().Msgf("propose burn call submitted with hash: %s", hash.Hex())
	}

	client.Events <- Events{
		WithdrawCreatedEvents: withdrawCreatedEvents,
		WithdrawReadyEvents:   withdrawReadyEvents,
		WithdrawExpiredEvents: withdrawExpiredEvents,
		RefundReadyEvents:     refundTransactionReadyEvents,
		RefundExpiredEvents:   refundTransactionExpiredEvents,
	}

	return nil
}
