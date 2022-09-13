package substrate

import (
	"context"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
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

type SubstrateClient struct {
	*substrate.Substrate
	Identity substrate.Identity
}

// NewSubstrate creates a substrate client
func NewSubstrateClient(url string, seed string) (*SubstrateClient, error) {
	mngr := substrate.NewManager(url)
	cl, err := mngr.Substrate()
	if err != nil {
		return nil, err
	}
	tfchainIdentity, err := substrate.NewIdentityFromSr25519Phrase(seed)
	if err != nil {
		return nil, err
	}

	return &SubstrateClient{
		cl,
		tfchainIdentity,
	}, nil
}

func (client *SubstrateClient) SubscribeTfchainBridgeEvents(ctx context.Context) (chan EventSubscription, error) {
	cl, _, err := client.GetClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get client")
	}

	chainHeadsSub, err := cl.RPC.Chain.SubscribeFinalizedHeads()
	if err != nil {
		return nil, errors.Wrap(err, "failed to subscribe to finalized heads")
	}

	eventChannel := make(chan EventSubscription)
	go func() {
		for {
			select {
			case head := <-chainHeadsSub.Chan():
				events, err := client.processEventsForHeight(uint32(head.Number))
				data := EventSubscription{
					Events: events,
					Err:    err,
				}
				eventChannel <- data
			case <-ctx.Done():
				chainHeadsSub.Unsubscribe()
				return
			}
		}
	}()

	return eventChannel, nil
}

func (client *SubstrateClient) CallExtrinsic(call *types.Call) (*types.Hash, error) {
	cl, meta, err := client.GetClient()
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("call ready to be submitted")
	hash, err := client.Substrate.Call(cl, meta, client.Identity, *call)
	if err != nil {
		log.Error().Msgf("error occurred while submitting call %+v", err)
		return nil, err
	}

	return &hash, nil
}
