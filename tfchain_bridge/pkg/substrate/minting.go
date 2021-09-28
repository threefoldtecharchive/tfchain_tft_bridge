package substrate

import (
	"fmt"
	"math/big"

	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

var (
	ErrMintTransactionNotFound = fmt.Errorf("mint tx not found")
)

type MintTransaction struct {
	Amount types.U64
	Target types.AccountID
	Block  types.BlockNumber
	Votes  types.U32
}

func (s *SubstrateClient) IsMintedAlready(identity *Identity, mintTxID string) (exists bool, err error) {
	log.Debug().Msgf("trying to retrieve tx with hash: %s", mintTxID)
	bytes, err := types.EncodeToBytes(mintTxID)
	if err != nil {
		return false, errors.Wrap(err, "substrate: encoding error building query arguments")
	}

	var mintTX MintTransaction
	key, err := types.CreateStorageKey(s.meta, "TFTBridgeModule", "ExecutedMintTransactions", bytes, nil)
	if err != nil {
		err = errors.Wrap(err, "failed to create storage key")
		return
	}

	ok, err := s.cl.RPC.State.GetStorageLatest(key, &mintTX)
	if err != nil {
		return false, err
	}

	if !ok {
		return false, ErrMintTransactionNotFound
	}

	log.Info().Msgf("minting tx found %+v", mintTX)

	return true, nil
}

func (s *SubstrateClient) ProposeOrVoteMintTransaction(identity *Identity, txID string, target AccountID, amount *big.Int) error {
	c, err := types.NewCall(s.meta, "TFTBridgeModule.propose_or_vote_mint_transaction",
		txID, target, types.U64(amount.Uint64()),
	)

	if err != nil {
		return errors.Wrap(err, "failed to create call")
	}

	if _, err := s.call(identity, c); err != nil {
		return errors.Wrap(err, "failed to propose or vote mint transaction")
	}

	return nil
}
