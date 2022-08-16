package substrate

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/rs/zerolog/log"
	"github.com/threefoldtech/substrate-client"
)

type SubstrateClient struct {
	*substrate.Substrate
	Id         substrate.Identity
	transactor Transactor
}

type Transactor interface {
	SubmitRefundTransaction(ctx context.Context, refundReadyEvent substrate.RefundTransactionReady) (*types.Call, error)
	ProposeBurnTransaction(ctx context.Context, burnCreatedEvent substrate.BridgeBurnTransactionCreated) (*types.Call, error)
	SubmitBurnTransaction(ctx context.Context, burnReadyEvent substrate.BurnTransactionReady) (*types.Call, error)
	CreateRefund(ctx context.Context, refundCreated substrate.RefundTransactionCreated) (*types.Call, error)
}

// NewSubstrate creates a substrate client
func NewSubstrateClient(url string, id substrate.Identity, transactor Transactor) (*SubstrateClient, error) {
	mngr := substrate.NewManager(url)
	cl, err := mngr.Substrate()
	if err != nil {
		return nil, err
	}

	return &SubstrateClient{
		cl,
		id,
		transactor,
	}, nil
}

func (client *SubstrateClient) SetTransactor(transactor Transactor) {
	client.transactor = transactor
}

func (client *SubstrateClient) CallExtrinsic(call *types.Call) (*types.Hash, error) {
	cl, meta, err := client.GetClient()
	if err != nil {
		return nil, err
	}

	log.Debug().Msgf("call ready to be submitted")
	hash, err := client.Substrate.Call(cl, meta, client.Id, *call)
	if err != nil {
		log.Error().Msgf("error occurred while submitting call %+v", err)
		return nil, err
	}
	log.Debug().Msgf("call exectued with hash: %s", hash.Hex())

	return &hash, nil
}

func (client *SubstrateClient) GetSubstrateAddressFromMemo(memo string) (string, error) {
	chunks := strings.Split(memo, "_")
	if len(chunks) != 2 {
		// memo is not formatted correctly, issue a refund
		return "", errors.New("memo text is not correctly formatted")
	}

	id, err := strconv.Atoi(chunks[1])
	if err != nil {
		return "", err
	}

	switch chunks[0] {
	case "twin":
		twin, err := client.GetTwin(uint32(id))
		if err != nil {
			return "", err
		}
		return twin.Account.String(), nil
	case "farm":
		farm, err := client.GetFarm(uint32(id))
		if err != nil {
			return "", err
		}
		twin, err := client.GetTwin(uint32(farm.TwinID))
		if err != nil {
			return "", err
		}
		return twin.Account.String(), nil
	case "node":
		node, err := client.GetNode(uint32(id))
		if err != nil {
			return "", err
		}
		twin, err := client.GetTwin(uint32(node.TwinID))
		if err != nil {
			return "", err
		}
		return twin.Account.String(), nil
	case "entity":
		entity, err := client.GetEntity(uint32(id))
		if err != nil {
			return "", err
		}
		return entity.Account.String(), nil
	default:
		return "", errors.New("grid type not supported")
	}
}
