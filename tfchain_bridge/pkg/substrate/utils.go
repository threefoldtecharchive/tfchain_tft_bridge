package substrate

import (
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/tfchain_bridge/pkg"
	"github.com/vedhavyas/go-subkey"
	subkeyEd25519 "github.com/vedhavyas/go-subkey/ed25519"
	"golang.org/x/crypto/blake2b"
)

// https://github.com/threefoldtech/tfchain_pallets/blob/bc9c5d322463aaf735212e428da4ea32b117dc24/pallet-smart-contract/src/lib.rs#L58
var smartContractModuleErrors = []string{
	"TwinNotExists",
	"NodeNotExists",
	"FarmNotExists",
	"FarmHasNotEnoughPublicIPs",
	"FarmHasNotEnoughPublicIPsFree",
	"FailedToReserveIP",
	"FailedToFreeIPs",
	"ContractNotExists",
	"TwinNotAuthorizedToUpdateContract",
	"TwinNotAuthorizedToCancelContract",
	"NodeNotAuthorizedToDeployContract",
	"NodeNotAuthorizedToComputeReport",
	"PricingPolicyNotExists",
	"ContractIsNotUnique",
	"NameExists",
	"NameNotValid",
}

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

// Sign signs data with the private key under the given derivation path, returning the signature. Requires the subkey
// command to be in path
func signBytes(data []byte, privateKeyURI string) ([]byte, error) {
	// if data is longer than 256 bytes, hash it first
	if len(data) > 256 {
		h := blake2b.Sum256(data)
		data = h[:]
	}

	scheme := subkeyEd25519.Scheme{}
	kyr, err := subkey.DeriveKeyPair(scheme, privateKeyURI)
	if err != nil {
		return nil, err
	}

	signature, err := kyr.Sign(data)
	if err != nil {
		return nil, err
	}

	return signature, nil
}

// Sign adds a signature to the extrinsic
func (s *SubstrateClient) sign(e *types.Extrinsic, signer *Identity, o types.SignatureOptions) error {
	if e.Type() != types.ExtrinsicVersion4 {
		return fmt.Errorf("unsupported extrinsic version: %v (isSigned: %v, type: %v)", e.Version, e.IsSigned(), e.Type())
	}

	mb, err := types.EncodeToBytes(e.Method)
	if err != nil {
		return err
	}

	era := o.Era
	if !o.Era.IsMortalEra {
		era = types.ExtrinsicEra{IsImmortalEra: true}
	}

	payload := types.ExtrinsicPayloadV4{
		ExtrinsicPayloadV3: types.ExtrinsicPayloadV3{
			Method:      mb,
			Era:         era,
			Nonce:       o.Nonce,
			Tip:         o.Tip,
			SpecVersion: o.SpecVersion,
			GenesisHash: o.GenesisHash,
			BlockHash:   o.BlockHash,
		},
		TransactionVersion: o.TransactionVersion,
	}

	signerPubKey := types.NewMultiAddressFromAccountID(signer.PublicKey)

	b, err := types.EncodeToBytes(payload)
	if err != nil {
		return err
	}

	sig, err := signBytes(b, signer.URI)

	if err != nil {
		return err
	}

	extSig := types.ExtrinsicSignatureV4{
		Signer:    signerPubKey,
		Signature: types.MultiSignature{IsEd25519: true, AsEd25519: types.NewSignature(sig)},
		Era:       era,
		Nonce:     o.Nonce,
		Tip:       o.Tip,
	}

	e.Signature = extSig

	// mark the extrinsic as signed
	e.Version |= types.ExtrinsicBitSigned

	return nil
}

func (s *SubstrateClient) call(identity *Identity, call types.Call) (hash types.Hash, err error) {
	// Create the extrinsic
	ext := types.NewExtrinsic(call)

	genesisHash, err := s.cl.RPC.Chain.GetBlockHash(0)
	if err != nil {
		return hash, errors.Wrap(err, "failed to get genesisHash")
	}

	rv, err := s.cl.RPC.State.GetRuntimeVersionLatest()
	if err != nil {
		return hash, err
	}

	//node.Address =identity.PublicKey
	account, err := s.getAccount(identity, s.meta)
	if err != nil {
		return hash, errors.Wrap(err, "failed to get account")
	}

	o := types.SignatureOptions{
		BlockHash:          genesisHash,
		Era:                types.ExtrinsicEra{IsMortalEra: false},
		GenesisHash:        genesisHash,
		Nonce:              types.NewUCompactFromUInt(uint64(account.Nonce)),
		SpecVersion:        rv.SpecVersion,
		Tip:                types.NewUCompactFromUInt(0),
		TransactionVersion: 1,
	}

	err = s.sign(&ext, identity, o)
	if err != nil {
		return hash, errors.Wrap(err, "failed to sign")
	}

	// Send the extrinsic
	sub, err := s.cl.RPC.Author.SubmitAndWatchExtrinsic(ext)
	if err != nil {
		return hash, errors.Wrap(err, "failed to submit extrinsic")
	}

	defer sub.Unsubscribe()

	for event := range sub.Chan() {
		if event.IsFinalized {
			hash = event.AsFinalized
			break
		} else if event.IsDropped || event.IsInvalid {
			return hash, fmt.Errorf("failed to make call")
		}
	}

	return hash, nil
}

func (s *SubstrateClient) checkForError(blockHash types.Hash, signer types.AccountID) error {
	meta, err := s.cl.RPC.State.GetMetadataLatest()
	if err != nil {
		return err
	}

	key, err := types.CreateStorageKey(meta, "System", "Events", nil, nil)
	if err != nil {
		return err
	}

	raw, err := s.cl.RPC.State.GetStorageRaw(key, blockHash)
	if err != nil {
		return err
	}

	block, err := s.cl.RPC.Chain.GetBlock(blockHash)
	if err != nil {
		return err
	}

	events := EventRecords{}
	err = types.EventRecordsRaw(*raw).DecodeEventRecords(meta, &events)
	if err != nil {
		log.Debug().Msgf("Failed to decode event %+v", err)
		return nil
	}

	if len(events.System_ExtrinsicFailed) > 0 {
		for _, e := range events.System_ExtrinsicFailed {
			who := block.Block.Extrinsics[e.Phase.AsApplyExtrinsic].Signature.Signer.AsID
			if signer == who {
				return fmt.Errorf(smartContractModuleErrors[e.DispatchError.Error])
			}
		}
	}

	return nil
}
