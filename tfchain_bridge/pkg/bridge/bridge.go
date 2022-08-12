package bridge

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	hProtocol "github.com/stellar/go/protocols/horizon"
	"github.com/threefoldtech/substrate-client"
	"github.com/threefoldtech/tfchain_bridge/pkg"
	"github.com/threefoldtech/tfchain_bridge/pkg/stellar"
	subpkg "github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

const (
	BridgeNetwork = "stellar"
)

// Bridge is a high lvl structure which listens on contract events and bridge-related
// stellar transactions, and handles them
type Bridge struct {
	wallet      *stellar.StellarWallet
	subClient   *subpkg.SubstrateClient
	persistency *pkg.ChainPersistency
	mut         sync.Mutex
	config      *pkg.BridgeConfig
	depositFee  int64
}

func NewBridge(ctx context.Context, cfg pkg.BridgeConfig) (*Bridge, error) {
	tfchainIdentity, err := substrate.NewIdentityFromSr25519Phrase(cfg.TfchainSeed)
	if err != nil {
		return nil, err
	}

	subClient, err := subpkg.NewSubstrateClient(cfg.TfchainURL, tfchainIdentity, &Bridge{})
	if err != nil {
		return nil, err
	}

	isValidator, err := subClient.IsValidator(tfchainIdentity)
	if err != nil {
		return nil, err
	}

	if !isValidator {
		return nil, fmt.Errorf("account provided is not a validator for the bridge runtime")
	}

	persistency, err := pkg.InitPersist(cfg.PersistencyFile)
	if err != nil {
		return nil, err
	}

	wallet, err := stellar.NewStellarWallet(ctx, &cfg.StellarConfig)
	if err != nil {
		return nil, err
	}

	if cfg.RescanBridgeAccount {
		// saving the cursor to 0 will trigger the bridge stellar account
		// to scan for every transaction ever made on the bridge account
		// and mint accordingly
		err = persistency.SaveStellarCursor("0")
		if err != nil {
			return nil, err
		}
	}

	// fetch the configured depositfee
	depositFee, err := subClient.GetDepositFee(tfchainIdentity)
	if err != nil {
		return nil, err
	}

	bridge := &Bridge{
		subClient:   subClient,
		persistency: persistency,
		wallet:      wallet,
		config:      &cfg,
		depositFee:  depositFee,
	}

	bridge.subClient.SetTransactor(bridge)

	return bridge, nil
}

func (bridge *Bridge) Start(ctx context.Context) error {
	height, err := bridge.persistency.GetHeight()
	if err != nil {
		return errors.Wrap(err, "failed to get block height from persistency")
	}

	cl, _, err := bridge.subClient.GetClient()
	if err != nil {
		return errors.Wrap(err, "failed to get client")
	}

	go func() {
		log.Info().Msg("starting monitoring bridge account...")
		if err := bridge.wallet.MonitorBridgeAccountAndMint(ctx, bridge.mint, height.StellarCursor); err != nil {
			panic(err)
		}
	}()

	ticker := time.NewTicker(6 * time.Second)
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		for range ticker.C {
			height, err := bridge.persistency.GetHeight()
			if err != nil {
				log.Err(err).Msg("failed to get persistency height")
				quit <- struct{}{}
			}
			finalizedHeadHash, err := cl.RPC.Chain.GetFinalizedHead()
			if err != nil {
				log.Err(err).Msg("failed to fetch finalized head")
				continue
			}
			lastBlock, err := cl.RPC.Chain.GetBlock(finalizedHeadHash)
			if err != nil {
				log.Err(err).Msg("failed to fetch block for finalized head")
				continue
			}

			// If we already processed this block, skip
			if lastBlock.Block.Header.Number <= types.BlockNumber(height.LastHeight) {
				continue
			}

			err = bridge.subClient.ProcessEventsForHeight(uint32(lastBlock.Block.Header.Number))
			if err != nil {
				log.Err(err).Msgf("failed to process events for height: %d", lastBlock.Block.Header.Number)
				continue
			}

			log.Debug().Msg("saving processed height now")
			err = bridge.persistency.SaveHeight(uint32(lastBlock.Block.Header.Number))
			if err != nil {
				log.Err(err).Msg("failed to save persistency height")
				quit <- struct{}{}
			}
		}
	}()

	go func() {
		for call := range bridge.subClient.CallChan {
			log.Debug().Msg("received call on channel, executing now")
			hash, err := bridge.subClient.CallExtrinsic(call)
			if err != nil {
				log.Err(err).Msg("failed to make call")
				quit <- struct{}{}
			}
			log.Info().Msgf("call exectued with hash: %s", hash.Hex())
		}
	}()

	for {
		select {
		case <-quit:
			ticker.Stop()
			return errors.New("Bridge stopped unexepectedly")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// mint handler for stellar
func (bridge *Bridge) mint(senders map[string]*big.Int, tx hProtocol.Transaction) error {
	log.Debug().Msg("calling mint now")

	if len(senders) > 1 {
		log.Error().Msgf("cannot process mint transaction, multiple senders found, refunding now")
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
		log.Error().Msgf("transaction with hash %s has empty memo, refunding now", tx.Hash)
		return bridge.refund(context.Background(), receiver, depositedAmount.Int64(), tx)
	}

	if tx.MemoType == "return" {
		log.Error().Msgf("transaction with hash %s has a return memo hash, skipping this transaction", tx.Hash)
		// save cursor
		cursor := tx.PagingToken()
		err := bridge.persistency.SaveStellarCursor(cursor)
		if err != nil {
			log.Error().Msgf("error while saving cursor:", err.Error())
			return err
		}
		log.Debug().Msg("stellar cursor saved")
		return nil
	}

	// TODO check if we already minted for this txid
	minted, err := bridge.subClient.IsMintedAlready(bridge.subClient.Id, tx.Hash)
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

	destinationSubstrateAddress, err := bridge.subClient.GetSubstrateAddressFromMemo(tx.Memo)
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

	call, err := bridge.subClient.ProposeOrVoteMintTransaction(bridge.subClient.Id, tx.Hash, accountID, depositedAmount)
	if err != nil {
		return err
	}

	bridge.subClient.CallChan <- call

	// save cursor
	cursor := tx.PagingToken()
	log.Debug().Msgf("saving cursor now %s", cursor)
	err = bridge.persistency.SaveStellarCursor(cursor)
	if err != nil {
		log.Error().Msgf("error while saving cursor:", err.Error())
		return err
	}

	return nil
}

// refund handler for stellar
func (bridge *Bridge) refund(ctx context.Context, destination string, amount int64, tx hProtocol.Transaction) error {
	log.Debug().Msg("calling refund now")
	call, err := bridge.createRefund(ctx, destination, amount, tx.Hash)
	if err != nil {
		return err
	}

	if call == nil {
		return nil
	}

	bridge.subClient.CallChan <- call

	// save cursor
	cursor := tx.PagingToken()
	log.Debug().Msgf("saving cursor now %s", cursor)
	err = bridge.persistency.SaveStellarCursor(cursor)
	if err != nil {
		log.Error().Msgf("error while saving cursor:", err.Error())
		return err
	}
	return nil
}

func (bridge *Bridge) CreateRefund(ctx context.Context, refundTransactionCreated substrate.RefundTransactionCreated) (*types.Call, error) {
	return bridge.createRefund(ctx, string(refundTransactionCreated.Target), int64(refundTransactionCreated.Amount), string(refundTransactionCreated.RefundTransactionHash))
}

func (bridge *Bridge) createRefund(ctx context.Context, destination string, amount int64, txHash string) (*types.Call, error) {
	refunded, err := bridge.subClient.IsRefundedAlready(bridge.subClient.Id, txHash)
	if err != nil {
		return nil, err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", txHash)
		return nil, pkg.ErrTransactionAlreadyRefunded
	}

	signature, sequenceNumber, err := bridge.wallet.CreateRefundAndReturnSignature(ctx, destination, uint64(amount), txHash)
	if err != nil {
		return nil, err
	}

	return bridge.subClient.CreateRefundTransactionOrAddSig(bridge.subClient.Id, txHash, destination, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)

}

func (bridge *Bridge) SubmitRefundTransaction(ctx context.Context, refundReadyEvent substrate.RefundTransactionReady) (*types.Call, error) {
	refunded, err := bridge.subClient.IsRefundedAlready(bridge.subClient.Id, string(refundReadyEvent.RefundTransactionHash))
	if err != nil {
		return nil, err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", string(refundReadyEvent.RefundTransactionHash))
		return nil, pkg.ErrTransactionAlreadyRefunded
	}

	refund, err := bridge.subClient.GetRefundTransaction(bridge.subClient.Id, string(refundReadyEvent.RefundTransactionHash))
	if err != nil {
		return nil, err
	}

	err = bridge.wallet.CreateRefundPaymentWithSignaturesAndSubmit(ctx, refund.Target, uint64(refund.Amount), refund.TxHash, refund.Signatures, int64(refund.SequenceNumber))
	if err != nil {
		return nil, err
	}

	return bridge.subClient.SetRefundTransactionExecuted(bridge.subClient.Id, refund.TxHash)
}

func (bridge *Bridge) ProposeBurnTransaction(ctx context.Context, burnCreatedEvent substrate.BridgeBurnTransactionCreated) (*types.Call, error) {
	burned, err := bridge.subClient.IsBurnedAlready(bridge.subClient.Id, burnCreatedEvent.BurnTransactionID)
	if err != nil {
		return nil, err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", burnCreatedEvent.BurnTransactionID)
		return nil, errors.New("tx burned already")
	}

	amount := big.NewInt(int64(burnCreatedEvent.Amount))
	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, string(burnCreatedEvent.Target), amount.Uint64(), uint64(burnCreatedEvent.BurnTransactionID))
	if err != nil {
		return nil, err
	}
	log.Debug().Msgf("stellar account sequence number: %d", sequenceNumber)

	return bridge.subClient.ProposeBurnTransactionOrAddSig(bridge.subClient.Id, uint64(burnCreatedEvent.BurnTransactionID), string(burnCreatedEvent.Target), amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) SubmitBurnTransaction(ctx context.Context, burnReadyEvent substrate.BurnTransactionReady) (*types.Call, error) {
	burned, err := bridge.subClient.IsBurnedAlready(bridge.subClient.Id, burnReadyEvent.BurnTransactionID)

	if err != nil {
		return nil, err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", burnReadyEvent.BurnTransactionID)
		return nil, errors.New("tx burned already")
	}

	burnTx, err := bridge.subClient.GetBurnTransaction(bridge.subClient.Id, burnReadyEvent.BurnTransactionID)
	if err != nil {
		return nil, err
	}

	if len(burnTx.Signatures) == 0 {
		log.Info().Msg("found 0 signatures, aborting")
		return nil, errors.New("no signatures")
	}

	// todo add memo hash
	err = bridge.wallet.CreatePaymentWithSignaturesAndSubmit(ctx, burnTx.Target, uint64(burnTx.Amount), "", burnTx.Signatures, int64(burnTx.SequenceNumber))
	if err != nil {
		return nil, err
	}

	return bridge.subClient.SetBurnTransactionExecuted(bridge.subClient.Id, uint64(burnReadyEvent.BurnTransactionID))
}

func (bridge *Bridge) Close() error {
	bridge.mut.Lock()
	defer bridge.mut.Unlock()
	return nil
}
