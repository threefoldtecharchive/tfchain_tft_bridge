package main

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
)

func main() {
	startHeight := 1677587
	endHeight := 1677590

	mngr := substrate.NewManager("wss://tfchain.dev.grid.tf")
	subClient, err := mngr.Substrate()
	if err != nil {
		panic(err)
	}

	withdraws, err := subClient.GetPendingWithdraws()
	if err != nil {
		panic(err)
	}
	fmt.Println(withdraws)

	for i := startHeight; i < endHeight; i++ {
		err := processEventsForHeight(subClient, uint32(i))
		if err != nil {
			panic(err)
		}
	}

}

func processEventsForHeight(subClient *substrate.Substrate, height uint32) error {
	log.Info().Msgf("fetching events for blockheight %d", height)
	records, err := subClient.GetEventsForBlock(height)
	if err != nil {
		log.Info().Msgf("failed to decode block with height %d", height)
		return err
	}

	err = processEventRecords(records)
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

func processEventRecords(events *substrate.EventRecords) error {
	for _, e := range events.TFTBridgeModule_RefundTransactionReady {
		log.Info().Msgf("found refund transaction ready event %s", string(e.RefundTransactionHash))
	}

	for _, e := range events.TFTBridgeModule_WithdrawTransactionCreated {
		log.Info().Uint64("ID", uint64(e.WithdrawTransactionID)).Msg("found withdraw transaction created event")
	}

	for _, e := range events.TFTBridgeModule_WithdrawTransactionReady {
		log.Info().Uint64("ID", uint64(e.WithdrawTransactionID)).Msg("found withdraw transaction ready event")
	}

	for _, e := range events.TFTBridgeModule_WithdrawTransactionExpired {
		log.Info().Uint64("ID", uint64(e.WithdrawTransactionID)).Msg("found withdraw transaction expired event")
	}

	for _, e := range events.TFTBridgeModule_RefundTransactionExpired {
		log.Info().Msgf("found expired refund transaction %s", e.RefundTransactionHash)
	}

	return nil
}
