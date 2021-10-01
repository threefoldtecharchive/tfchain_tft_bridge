package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
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

func (s *SubstrateClient) SubscribeEvents() (*state.StorageSubscription, types.StorageKey, error) {
	// Subscribe to system events via storage
	key, err := types.CreateStorageKey(s.meta, "System", "Events", nil)
	if err != nil {
		return nil, nil, err
	}

	sub, err := s.cl.RPC.State.SubscribeStorageRaw([]types.StorageKey{key})
	if err != nil {
		return nil, nil, err
	}
	// defer unsubscribe(sub)
	return sub, key, err
}
