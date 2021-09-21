package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
)

func (s *Substrate) SubscribeBurnEvents(burnChan chan BurnTransactionCreated) error {
	// Subscribe to system events via storage
	key, err := types.CreateStorageKey(s.meta, "System", "Events", nil)
	if err != nil {
		panic(err)
	}

	sub, err := s.cl.RPC.State.SubscribeStorageRaw([]types.StorageKey{key})
	if err != nil {
		panic(err)
	}
	defer sub.Unsubscribe()

	// outer for loop for subscription notifications
	for {
		set := <-sub.Chan()
		// inner loop for the changes within one of those notifications
		for _, chng := range set.Changes {
			if !types.Eq(chng.StorageKey, key) || !chng.HasStorageData {
				// skip, we are only interested in events with content
				continue
			}

			// Decode the event records
			events := EventRecords{}
			err = types.EventRecordsRaw(chng.StorageData).DecodeEventRecords(s.meta, &events)
			if err != nil {
				panic(err)
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionCreated {
				burnChan <- e
			}
		}
	}
}
