package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v4/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
)

type EventSubscription struct {
	Events Events
	Err    error
}

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

func (client *SubstrateClient) processEventsForHeight(height uint32) (Events, error) {
	log.Info().Uint32("ID", height).Msg("fetching events for blockheight")
	records, err := client.GetEventsForBlock(height)
	if err != nil {
		log.Err(err).Uint32("ID", height).Msg("failed to decode block for height")
		return Events{}, err
	}

	return client.processEventRecords(records), nil
}

func (client *SubstrateClient) processEventRecords(events *substrate.EventRecords) Events {
	var refundTransactionReadyEvents []RefundTransactionReadyEvent
	var refundTransactionExpiredEvents []RefundTransactionExpiredEvent
	var withdrawCreatedEvents []WithdrawCreatedEvent
	var withdrawReadyEvents []WithdrawReadyEvent
	var withdrawExpiredEvents []WithdrawExpiredEvent

	for _, e := range events.TFTBridgeModule_RefundTransactionReady {
		log.Info().Str("hash", string(e.RefundTransactionHash)).Msg("found refund transaction ready event")
		refundTransactionReadyEvents = append(refundTransactionReadyEvents, RefundTransactionReadyEvent{
			Hash: string(e.RefundTransactionHash),
		})
	}

	for _, e := range events.TFTBridgeModule_RefundTransactionExpired {
		log.Info().Str("hash", string(e.RefundTransactionHash)).Msgf("found expired refund transaction")
		refundTransactionExpiredEvents = append(refundTransactionExpiredEvents, RefundTransactionExpiredEvent{
			Hash:   string(e.RefundTransactionHash),
			Target: string(e.Target),
			Amount: uint64(e.Amount),
		})
	}

	for _, e := range events.TFTBridgeModule_WithdrawTransactionCreated {
		log.Info().Uint64("ID", uint64(e.WithdrawTransactionID)).Msg("found withdraw transaction created event")
		withdrawCreatedEvents = append(withdrawCreatedEvents, WithdrawCreatedEvent{
			ID:     uint64(e.WithdrawTransactionID),
			Source: e.Source,
			Target: string(e.Target),
			Amount: uint64(e.Amount),
		})
	}

	for _, e := range events.TFTBridgeModule_WithdrawTransactionReady {
		log.Info().Uint64("ID", uint64(e.WithdrawTransactionID)).Msg("found withdraw transaction ready event")
		withdrawReadyEvents = append(withdrawReadyEvents, WithdrawReadyEvent{
			ID: uint64(e.WithdrawTransactionID),
		})
	}

	for _, e := range events.TFTBridgeModule_WithdrawTransactionExpired {
		log.Info().Uint64("ID", uint64(e.WithdrawTransactionID)).Msg("found withdraw transaction expired event")
		withdrawExpiredEvents = append(withdrawExpiredEvents, WithdrawExpiredEvent{
			ID:     uint64(e.WithdrawTransactionID),
			Target: string(e.Target),
			Amount: uint64(e.Amount),
		})
	}

	return Events{
		WithdrawCreatedEvents: withdrawCreatedEvents,
		WithdrawReadyEvents:   withdrawReadyEvents,
		WithdrawExpiredEvents: withdrawExpiredEvents,
		RefundReadyEvents:     refundTransactionReadyEvents,
		RefundExpiredEvents:   refundTransactionExpiredEvents,
	}
}
