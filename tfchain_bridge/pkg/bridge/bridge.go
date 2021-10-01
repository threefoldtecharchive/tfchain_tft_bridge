package bridge

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v3/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/rs/zerolog/log"
	"github.com/stellar/go/strkey"
	"github.com/threefoldtech/tfchain_bridge/pkg"
	"github.com/threefoldtech/tfchain_bridge/pkg/stellar"
	"github.com/threefoldtech/tfchain_bridge/pkg/substrate"
)

var (
	errInsufficientDepositAmount = errors.New("deposited amount is <= Fee")
)

const (
	// Withdrawing from smartchain to Stellar fee
	WithdrawFee   = int64(1 * 1e7)
	BridgeNetwork = "stellar"
)

// Bridge is a high lvl structure which listens on contract events and bridge-related
// stellar transactions, and handles them
type Bridge struct {
	wallet           *stellar.StellarWallet
	subClient        *substrate.SubstrateClient
	identity         substrate.Identity
	blockPersistency *pkg.ChainPersistency
	mut              sync.Mutex
	config           *pkg.BridgeConfig
}

func NewBridge(ctx context.Context, cfg pkg.BridgeConfig) (*Bridge, error) {
	subClient, err := substrate.NewSubstrateClient(cfg.TfchainURL)
	if err != nil {
		return nil, err
	}

	tfchainIdentity, err := substrate.IdentityFromPhrase(cfg.TfchainSeed)
	if err != nil {
		return nil, err
	}

	isValidator, err := subClient.IsValidator(&tfchainIdentity)
	if err != nil {
		return nil, err
	}

	if !isValidator {
		return nil, fmt.Errorf("account provided is not a validator for the bridge runtime")
	}

	blockPersistency, err := pkg.InitPersist(cfg.PersistencyFile)
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
		err = blockPersistency.SaveStellarCursor("0")
		if err != nil {
			return nil, err
		}
		err = blockPersistency.SaveHeight(0)
		if err != nil {
			return nil, err
		}
	}

	bridge := &Bridge{
		subClient:        subClient,
		identity:         tfchainIdentity,
		blockPersistency: blockPersistency,
		wallet:           wallet,
		config:           &cfg,
	}

	return bridge, nil
}

func (bridge *Bridge) Start(ctx context.Context) error {
	// all extrinsics to be submitted will be pushed to this channel
	submitExtrinsicChan := make(chan types.Call)

	go func() {
		for call := range submitExtrinsicChan {
			log.Info().Msgf("call ready to be submitted")
			hash, err := bridge.subClient.Call(&bridge.identity, call)
			if err != nil {
				log.Error().Msgf("error occurred while submitting call %+v", err)
			}
			log.Info().Msgf("call submitted, hash=%s", hash.Hex())
		}
	}()

	go func() {
		log.Info().Msg("starting minting subscription...")
		if err := bridge.wallet.MonitorBridgeAccountAndMint(ctx, bridge.mint, bridge.refund, bridge.blockPersistency); err != nil {
			panic(err)
		}
	}()

	go func() {
		log.Info().Msg("started subs...")
		sub, key, err := bridge.subClient.SubscribeEvents()
		if err != nil {
			panic(err)
		}

		err = bridge.ProcessSubscription(sub, submitExtrinsicChan, key)
		if err != nil {
			log.Err(err)
		}
	}()

	currentBlockNumber, err := bridge.subClient.GetCurrentHeight()
	if err != nil {
		return err
	}
	log.Info().Msgf("current blockheight: %d", currentBlockNumber)

	height, err := bridge.blockPersistency.GetHeight()
	if err != nil {
		return err
	}

	if height.LastHeight < currentBlockNumber {
		// TODO replay all events from lastheight until current height
		log.Info().Msgf("saved height is %d, need to sync from saved height until current height..", height.LastHeight)
		key, set, err := bridge.subClient.FetchEventsForBlockRange(height.LastHeight, currentBlockNumber)
		if err != nil {
			return err
		}

		err = bridge.processEvents(submitExtrinsicChan, key, set)
		if err != nil {
			if err == substrate.ErrFailedToDecode {
				log.Err(err)
			} else {
				return err
			}
		}
	}

	return nil
}

func (bridge *Bridge) ProcessSubscription(sub *state.StorageSubscription, callChan chan types.Call, key types.StorageKey) error {
	for {
		set := <-sub.Chan()
		// inner loop for the changes within one of those notifications

		err := bridge.processEvents(callChan, key, []types.StorageChangeSet{set})
		if err != nil {
			log.Err(err).Msg("error while processing events")
		}

		bl, err := bridge.subClient.GetBlock(set.Block)
		if err != nil {
			return err
		}
		log.Debug().Msgf("events for blockheight %+v processed, saving blockheight to persistency file now...", bl.Block.Header.Number)
		err = bridge.blockPersistency.SaveHeight(uint32(bl.Block.Header.Number))
		if err != nil {
			return err
		}
	}
}

func (bridge *Bridge) processEvents(callChan chan types.Call, key types.StorageKey, changeset []types.StorageChangeSet) error {
	for _, set := range changeset {
		for _, chng := range set.Changes {
			if !types.Eq(chng.StorageKey, key) || !chng.HasStorageData {
				// skip, we are only interested in events with content
				continue
			}

			meta := bridge.subClient.GetMeta()
			// Decode the event records
			events := substrate.EventRecords{}
			err := types.EventRecordsRaw(chng.StorageData).DecodeEventRecords(meta, &events)
			if err != nil {
				log.Err(err)
			}

			for _, e := range events.TFTBridgeModule_RefundTransactionReady {
				log.Info().Msg("found refund transaction ready event")
				call, err := bridge.submitRefundTransaction(context.Background(), e)
				if err != nil {
					log.Err(err)
					continue
				}
				callChan <- *call
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionCreated {
				log.Info().Msg("found burn transaction creted event")
				call, err := bridge.proposeBurnTransaction(context.Background(), e, true)
				if err != nil {
					log.Err(err)
					continue
				}
				callChan <- *call
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionReady {
				log.Info().Msg("found burn transaction ready event")
				call, err := bridge.submitBurnTransaction(context.Background(), e)
				if err != nil {
					log.Err(err).Msg("failed to submit burn transaction")
					continue
				}
				fmt.Println(call)
				callChan <- *call
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionExpired {
				log.Info().Msg("found burn transaction expired event")
				call, err := bridge.proposeBurnTransaction(context.Background(), e, false)
				if err != nil {
					log.Err(err)
					continue
				}
				callChan <- *call
			}

			for _, e := range events.TFTBridgeModule_RefundTransactionExpired {
				log.Info().Msgf("found expired refund transaction")
				call, err := bridge.createRefund(context.Background(), string(e.Target), int64(e.Amount), string(e.RefundTransactionHash))
				if err != nil {
					log.Err(err)
					continue
				}
				callChan <- *call
			}
		}
	}

	return nil
}

// mint handler for stellar
func (bridge *Bridge) mint(receiver string, depositedAmount *big.Int, txID string) error {
	log.Info().Msg("calling mint now")
	// TODO check if we already minted for this txid
	minted, err := bridge.subClient.IsMintedAlready(&bridge.identity, txID)
	if err != nil && err != substrate.ErrMintTransactionNotFound {
		return err
	}

	if minted {
		log.Error().Msgf("transaction with hash %s is already minted", txID)
		return nil
	}

	// fetch the configured depositfee
	depositFee, err := bridge.subClient.GetDepositFee(&bridge.identity)
	if err != nil {
		return err
	}

	if depositedAmount.Cmp(big.NewInt(depositFee)) <= 0 {
		log.Error().Int("amount", int(depositedAmount.Int64())).Str("txID", txID).Msg("Deposited amount is <= Fee, should be returned")
		return errInsufficientDepositAmount
	}
	amount := &big.Int{}
	// multiply the amount of tokens to be minted * the multiplier
	amount.Mul(depositedAmount, big.NewInt(bridge.config.TokenMultiplier))

	substrateAddressBytes, err := getSubstrateAddressFromStellarAddress(receiver)
	if err != nil {
		return err
	}

	substrateAddress, err := substrate.FromEd25519Bytes(substrateAddressBytes)
	if err != nil {
		return err
	}
	log.Info().Int64("amount", amount.Int64()).Str("tx_id", txID).Msgf("target substrate address to mint on: %s", substrateAddress)

	accountID, err := substrate.FromAddress(substrateAddress)
	if err != nil {
		return err
	}

	call, err := bridge.subClient.ProposeOrVoteMintTransaction(&bridge.identity, txID, accountID, amount)
	if err != nil {
		return err
	}
	hash, err := bridge.subClient.Call(&bridge.identity, *call)
	if err != nil {
		return err
	}
	log.Info().Msgf("minting transaction included in hash %s", hash.Hex())

	return nil
}

// refund handler for stellar
func (bridge *Bridge) refund(ctx context.Context, destination string, amount int64, txHash string) error {
	call, err := bridge.createRefund(ctx, destination, amount, txHash)
	if err != nil {
		return err
	}
	hash, err := bridge.subClient.Call(&bridge.identity, *call)
	if err != nil {
		return err
	}
	log.Info().Msgf("refund transaction included in hash %s", hash.Hex())

	return nil
}

func (bridge *Bridge) createRefund(ctx context.Context, destination string, amount int64, txHash string) (*types.Call, error) {
	refunded, err := bridge.subClient.IsRefundedAlready(&bridge.identity, txHash)
	if err != nil {
		return nil, err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", txHash)
		return nil, errors.New("tx refunded already")
	}

	signature, sequenceNumber, err := bridge.wallet.CreateRefundAndReturnSignature(ctx, destination, uint64(amount), txHash)
	if err != nil {
		return nil, err
	}

	return bridge.subClient.CreateRefundTransactionOrAddSig(&bridge.identity, txHash, destination, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) submitRefundTransaction(ctx context.Context, refundReadyEvent substrate.RefundTransactionReady) (*types.Call, error) {
	refunded, err := bridge.subClient.IsRefundedAlready(&bridge.identity, string(refundReadyEvent.RefundTransactionHash))
	if err != nil {
		return nil, err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", string(refundReadyEvent.RefundTransactionHash))
		return nil, errors.New("tx refunded already")
	}

	refund, err := bridge.subClient.GetRefundTransaction(&bridge.identity, string(refundReadyEvent.RefundTransactionHash))
	if err != nil {
		return nil, err
	}

	err = bridge.wallet.CreateRefundPaymentWithSignaturesAndSubmit(ctx, refund.Target, uint64(refund.Amount), refund.TxHash, refund.Signatures, int64(refund.SequenceNumber))
	if err != nil {
		return nil, err
	}

	return bridge.subClient.SetRefundTransactionExecuted(&bridge.identity, refund.TxHash)
}

func (bridge *Bridge) proposeBurnTransaction(ctx context.Context, burnCreatedEvent substrate.BurnTransactionCreated, divide bool) (*types.Call, error) {
	log.Info().Msg("going to propose burn transaction")
	burned, err := bridge.subClient.IsBurnedAlready(&bridge.identity, burnCreatedEvent.BurnTransactionID)
	if err != nil {
		return nil, err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", burnCreatedEvent.BurnTransactionID)
		return nil, errors.New("tx burned already")
	}

	stellarAddress, err := getStellarAddressFromSubstrateAccountID(burnCreatedEvent.Target)
	if err != nil {
		return nil, err
	}

	amount := big.NewInt(int64(burnCreatedEvent.Amount))
	if divide {
		// divide the amount of tokens to be burned / the multiplier
		amount = amount.Div(amount, big.NewInt(bridge.config.TokenMultiplier))
	}

	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, stellarAddress, amount.Uint64(), uint64(burnCreatedEvent.BurnTransactionID))
	if err != nil {
		return nil, err
	}
	log.Info().Msgf("seq number: %d", sequenceNumber)

	return bridge.subClient.ProposeBurnTransactionOrAddSig(&bridge.identity, uint64(burnCreatedEvent.BurnTransactionID), substrate.AccountID(burnCreatedEvent.Target), amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) submitBurnTransaction(ctx context.Context, burnReadyEvent substrate.BurnTransactionReady) (*types.Call, error) {
	burned, err := bridge.subClient.IsBurnedAlready(&bridge.identity, burnReadyEvent.BurnTransactionID)

	if err != nil {
		return nil, err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", burnReadyEvent.BurnTransactionID)
		return nil, errors.New("tx burned already")
	}

	burnTx, err := bridge.subClient.GetBurnTransaction(&bridge.identity, burnReadyEvent.BurnTransactionID)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("burn tx signatures +%v", burnTx.Signatures)
	if len(burnTx.Signatures) == 0 {
		log.Info().Msg("found 0 signatures, aborting")
		return nil, errors.New("no signatures")
	}

	stellarAddress, err := getStellarAddressFromSubstrateAccountID(substrate.AccountID(burnTx.Target))
	if err != nil {
		return nil, err
	}

	// todo add memo hash
	err = bridge.wallet.CreatePaymentWithSignaturesAndSubmit(ctx, stellarAddress, uint64(burnTx.Amount), "", burnTx.Signatures, int64(burnTx.SequenceNumber))
	if err != nil {
		return nil, err
	}

	return bridge.subClient.SetBurnTransactionExecuted(&bridge.identity, uint64(burnReadyEvent.BurnTransactionID))
}

func getSubstrateAddressFromStellarAddress(address string) ([]byte, error) {
	versionbyte, pubkeydata, err := strkey.DecodeAny(address)
	if err != nil {
		return nil, err
	}
	if versionbyte != strkey.VersionByteAccountID {
		err = fmt.Errorf("%s is not a valid Stellar address", address)
		return nil, err
	}
	pubkey, err := crypto.UnmarshalEd25519PublicKey(pubkeydata)
	if err != nil {
		return nil, err
	}

	bytes, err := pubkey.Raw()
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func getStellarAddressFromSubstrateAccountID(accountID substrate.AccountID) (string, error) {
	return strkey.Encode(strkey.VersionByteAccountID, accountID.PublicKey())
}

func (bridge *Bridge) Close() error {
	bridge.mut.Lock()
	defer bridge.mut.Unlock()
	return nil
}
