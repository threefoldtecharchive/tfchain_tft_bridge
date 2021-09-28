package substrate

import (
	"fmt"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v3"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

var (
	//ErrInvalidVersion is returned if version 4bytes is invalid
	ErrInvalidVersion = fmt.Errorf("invalid version")
	//ErrUnknownVersion is returned if version number is not supported
	ErrUnknownVersion = fmt.Errorf("unknown version")
	//ErrNotFound is returned if an object is not found
	ErrNotFound = fmt.Errorf("object not found")
)

// Versioned base for all types
type Versioned struct {
	Version uint32
}

// Substrate client
type SubstrateClient struct {
	cl   *gsrpc.SubstrateAPI
	meta *types.Metadata
}

// NewSubstrate creates a substrate client
func NewSubstrateClient(url string) (*SubstrateClient, error) {
	cl, err := gsrpc.NewSubstrateAPI(url)
	if err != nil {
		return nil, err
	}
	meta, err := cl.RPC.State.GetMetadataLatest()
	if err != nil {
		return nil, err
	}

	return &SubstrateClient{
		cl:   cl,
		meta: meta,
	}, nil
}

// Refresh reloads meta from chain!
// not thread safe
func (s *SubstrateClient) Refresh() error {
	meta, err := s.cl.RPC.State.GetMetadataLatest()
	if err != nil {
		return err
	}

	s.meta = meta
	return nil
}

func (s *SubstrateClient) getVersion(b types.StorageDataRaw) (uint32, error) {
	var ver Versioned
	if err := types.DecodeFromBytes(b, &ver); err != nil {
		return 0, errors.Wrapf(ErrInvalidVersion, "failed to load version (reason: %s)", err)
	}

	return ver.Version, nil
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
