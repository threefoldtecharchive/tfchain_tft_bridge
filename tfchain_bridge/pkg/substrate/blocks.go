package substrate

import (
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/pkg/errors"
)

func (s *SubstrateClient) GetCurrentHeight() (uint32, error) {
	var blockNumber uint32
	key, err := types.CreateStorageKey(s.meta, "System", "Number", nil)
	if err != nil {
		err = errors.Wrap(err, "failed to create storage key")
		return 0, err
	}

	ok, err := s.cl.RPC.State.GetStorageLatest(key, &blockNumber)
	if err != nil {
		return 0, err
	}

	if !ok {
		return 0, errors.New("block number not found")
	}

	return blockNumber, nil
}

func (s *SubstrateClient) FetchEventsForBlockRange(start uint32, end uint32) (types.StorageKey, []types.StorageChangeSet, error) {
	key, err := types.CreateStorageKey(s.meta, "System", "Events", nil)
	if err != nil {
		return key, nil, err
	}

	lbh, err := s.cl.RPC.Chain.GetBlockHash(uint64(start))
	if err != nil {
		return key, nil, err
	}

	uph, err := s.cl.RPC.Chain.GetBlockHash(uint64(end))
	if err != nil {
		return key, nil, err
	}

	rawSet, err := s.cl.RPC.State.QueryStorage([]types.StorageKey{key}, lbh, uph)
	if err != nil {
		return key, nil, err
	}

	return key, rawSet, nil
}

func (s *SubstrateClient) GetBlock(block types.Hash) (*types.SignedBlock, error) {
	return s.cl.RPC.Chain.GetBlock(block)
}
