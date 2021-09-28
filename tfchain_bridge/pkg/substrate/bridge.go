package substrate

import (
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/pkg/errors"
)

var (
	errValidatorNotFound = fmt.Errorf("validator not found")
)

func (s *SubstrateClient) IsValidator(identity *Identity) (exists bool, err error) {
	var validators []AccountID
	key, err := types.CreateStorageKey(s.meta, "TFTBridgeModule", "Validators")
	if err != nil {
		err = errors.Wrap(err, "failed to create storage key")
		return
	}

	ok, err := s.cl.RPC.State.GetStorageLatest(key, &validators)
	if err != nil || !ok {
		if !ok {
			return false, errValidatorNotFound
		}

		return
	}

	exists = false
	for _, validator := range validators {
		if validator.String() == identity.Address {
			return true, nil
		}
	}

	return
}
