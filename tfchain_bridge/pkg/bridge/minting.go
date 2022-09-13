package bridge

import (
	"context"
	"math/big"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	hProtocol "github.com/stellar/go/protocols/horizon"
	"github.com/threefoldtech/substrate-client"
)

// mint handler for stellar
func (bridge *Bridge) mint(senders map[string]*big.Int, tx hProtocol.Transaction) error {
	log.Info().Msg("calling mint now")

	if len(senders) > 1 {
		log.Info().Msgf("cannot process mint transaction, multiple senders found, refunding now")
		for sender, depositAmount := range senders {
			return bridge.refund(context.Background(), sender, depositAmount.Int64(), tx)
		}
	}

	var receiver string
	var depositedAmount *big.Int
	for receiv, amount := range senders {
		receiver = receiv
		depositedAmount = amount
	}

	if tx.Memo == "" {
		log.Info().Msgf("transaction with hash %s has empty memo, refunding now", tx.Hash)
		return bridge.refund(context.Background(), receiver, depositedAmount.Int64(), tx)
	}

	if tx.MemoType == "return" {
		log.Debug().Msgf("transaction with hash %s has a return memo hash, skipping this transaction", tx.Hash)
		// save cursor
		cursor := tx.PagingToken()
		err := bridge.blockPersistency.SaveStellarCursor(cursor)
		if err != nil {
			log.Err(err).Msgf("error while saving cursor")
			return err
		}
		log.Info().Msg("stellar cursor saved")
		return nil
	}

	// TODO check if we already minted for this txid
	minted, err := bridge.subClient.IsMintedAlready(bridge.subClient.Identity, tx.Hash)
	if err != nil && err != substrate.ErrMintTransactionNotFound {
		return err
	}

	if minted {
		log.Error().Msgf("transaction with hash %s is already minted", tx.Hash)
		return nil
	}

	// if the deposited amount is lower than the depositfee, trigger a refund
	if depositedAmount.Cmp(big.NewInt(bridge.depositFee)) <= 0 {
		return bridge.refund(context.Background(), receiver, depositedAmount.Int64(), tx)
	}

	destinationSubstrateAddress, err := bridge.getSubstrateAddressFromMemo(tx.Memo)
	if err != nil {
		log.Err(err).Msgf("error while decoding tx memo")
		// memo is not formatted correctly, issue a refund
		return bridge.refund(context.Background(), receiver, depositedAmount.Int64(), tx)
	}

	log.Info().Int64("amount", depositedAmount.Int64()).Str("tx_id", tx.Hash).Msgf("target substrate address to mint on: %s", destinationSubstrateAddress)

	accountID, err := substrate.FromAddress(destinationSubstrateAddress)
	if err != nil {
		return err
	}

	call, err := bridge.subClient.ProposeOrVoteMintTransaction(bridge.subClient.Identity, tx.Hash, accountID, depositedAmount)
	if err != nil {
		return err
	}

	hash, err := bridge.subClient.CallExtrinsic(call)
	if err != nil {
		return err
	}
	log.Debug().Msgf("mint call submitted with hash: %s", hash.Hex())

	log.Info().Msg("Mint succesfull, saving cursor now")
	// save cursor
	cursor := tx.PagingToken()
	err = bridge.blockPersistency.SaveStellarCursor(cursor)
	if err != nil {
		log.Err(err).Msgf("error while saving cursor")
		return err
	}

	return nil
}

func (bridge *Bridge) handleMint(amount *big.Int, target substrate.AccountID, mintID string) error {
	// TODO check if we already minted for this txid
	minted, err := bridge.subClient.IsMintedAlready(bridge.subClient.Identity, mintID)
	if err != nil && err != substrate.ErrMintTransactionNotFound {
		return err
	}

	if minted {
		log.Debug().Msgf("transaction with id %s is already minted", mintID)
		return errors.New("transaction already minted")
	}

	call, err := bridge.subClient.ProposeOrVoteMintTransaction(bridge.subClient.Identity, mintID, target, amount)
	if err != nil {
		return err
	}

	hash, err := bridge.subClient.CallExtrinsic(call)
	if err != nil {
		return err
	}
	log.Info().Msgf("mint call submitted with hash: %s", hash.Hex())
	return nil
}

func (bridge *Bridge) getSubstrateAddressFromMemo(memo string) (string, error) {
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
		twin, err := bridge.subClient.GetTwin(uint32(id))
		if err != nil {
			return "", err
		}
		return twin.Account.String(), nil
	case "farm":
		farm, err := bridge.subClient.GetFarm(uint32(id))
		if err != nil {
			return "", err
		}
		twin, err := bridge.subClient.GetTwin(uint32(farm.TwinID))
		if err != nil {
			return "", err
		}
		return twin.Account.String(), nil
	case "node":
		node, err := bridge.subClient.GetNode(uint32(id))
		if err != nil {
			return "", err
		}
		twin, err := bridge.subClient.GetTwin(uint32(node.TwinID))
		if err != nil {
			return "", err
		}
		return twin.Account.String(), nil
	case "entity":
		entity, err := bridge.subClient.GetEntity(uint32(id))
		if err != nil {
			return "", err
		}
		return entity.Account.String(), nil
	default:
		return "", errors.New("grid type not supported")
	}
}