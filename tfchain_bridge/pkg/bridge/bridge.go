package bridge

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v3/rpc/state"
	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/rs/zerolog/log"
	hProtocol "github.com/stellar/go/protocols/horizon"
	"github.com/stellar/go/strkey"
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
	wallet           *stellar.StellarWallet
	subClient        *subpkg.SubstrateClient
	identity         substrate.Identity
	blockPersistency *pkg.ChainPersistency
	mut              sync.Mutex
	config           *pkg.BridgeConfig
	extrinsicsChan   chan Extrinsic
	depositFee       int64
}

type Extrinsic struct {
	call types.Call
	err  chan error
}

func NewBridge(ctx context.Context, cfg pkg.BridgeConfig) (*Bridge, error) {
	subClient, err := subpkg.NewSubstrateClient(cfg.TfchainURL)
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

	// fetch the configured depositfee
	depositFee, err := subClient.GetDepositFee(&tfchainIdentity)
	if err != nil {
		return nil, err
	}

	bridge := &Bridge{
		subClient:        subClient,
		identity:         tfchainIdentity,
		blockPersistency: blockPersistency,
		wallet:           wallet,
		config:           &cfg,
		depositFee:       depositFee,
	}

	return bridge, nil
}

func (bridge *Bridge) Start(ctx context.Context) error {
	// all extrinsics to be submitted will be pushed to this channel
	submitExtrinsicChan := make(chan Extrinsic)
	bridge.extrinsicsChan = submitExtrinsicChan

	go func() {
		for ext := range bridge.extrinsicsChan {
			cl, meta, err := bridge.subClient.GetClient()
			if err != nil {
				ext.err <- err
			}
			log.Info().Msgf("call ready to be submitted")
			hash, err := bridge.subClient.Substrate.Call(cl, meta, &bridge.identity, ext.call)
			if err != nil {
				ext.err <- err
				log.Error().Msgf("error occurred while submitting call %+v", err)
			}
			if ext.err != nil {
				close(ext.err)
			}
			log.Info().Msgf("call submitted, hash=%s", hash.Hex())
		}
	}()

	height, err := bridge.blockPersistency.GetHeight()
	if err != nil {
		return err
	}

	go func() {
		log.Info().Msg("starting minting subscription...")
		if err := bridge.wallet.MonitorBridgeAccountAndMint(ctx, bridge.mint, height.StellarCursor); err != nil {
			panic(err)
		}
	}()

	go func() {
		log.Info().Msg("started subs...")
		sub, key, err := bridge.subClient.SubscribeEvents()
		if err != nil {
			panic(err)
		}

		err = bridge.ProcessSubscription(sub, bridge.extrinsicsChan, key)
		if err != nil {
			log.Err(err)
		}
	}()

	currentBlockNumber, err := bridge.subClient.GetCurrentHeight()
	if err != nil {
		return err
	}
	log.Info().Msgf("current blockheight: %d", currentBlockNumber)

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

func (bridge *Bridge) ProcessSubscription(sub *state.StorageSubscription, callChan chan Extrinsic, key types.StorageKey) error {
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

func (bridge *Bridge) processEvents(callChan chan Extrinsic, key types.StorageKey, changeset []types.StorageChangeSet) error {
	for _, set := range changeset {
		for _, chng := range set.Changes {
			if !types.Eq(chng.StorageKey, key) || !chng.HasStorageData {
				// skip, we are only interested in events with content
				continue
			}

			_, meta, err := bridge.subClient.GetClient()
			if err != nil {
				return err
			}

			// Decode the event records
			events := subpkg.EventRecords{}
			err = types.EventRecordsRaw(chng.StorageData).DecodeEventRecords(meta, &events)
			if err != nil {
				log.Err(err)
			}

			for _, e := range events.TFTBridgeModule_RefundTransactionReady {
				log.Info().Msg("found refund transaction ready event")
				call, err := bridge.submitRefundTransaction(context.Background(), e)
				if err != nil {
					log.Info().Msgf("error occured: +%s", err.Error())
					continue
				}
				callChan <- Extrinsic{
					call: *call,
				}
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionCreated {
				log.Info().Msg("found burn transaction creted event")
				call, err := bridge.proposeBurnTransaction(context.Background(), e)
				if err != nil {
					log.Info().Msgf("error occured: +%s", err.Error())
					continue
				}
				callChan <- Extrinsic{
					call: *call,
				}
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionReady {
				log.Info().Msg("found burn transaction ready event")
				call, err := bridge.submitBurnTransaction(context.Background(), e)
				if err != nil {
					log.Info().Msgf("error occured: +%s", err.Error())
					continue
				}
				fmt.Println(call)
				callChan <- Extrinsic{
					call: *call,
				}
			}

			for _, e := range events.TFTBridgeModule_BurnTransactionExpired {
				log.Info().Msg("found burn transaction expired event")
				call, err := bridge.proposeBurnTransaction(context.Background(), e)
				if err != nil {
					log.Info().Msgf("error occured: +%s", err.Error())
					continue
				}
				callChan <- Extrinsic{
					call: *call,
				}
			}

			for _, e := range events.TFTBridgeModule_RefundTransactionExpired {
				log.Info().Msgf("found expired refund transaction")
				call, err := bridge.createRefund(context.Background(), string(e.Target), int64(e.Amount), string(e.RefundTransactionHash))
				if err != nil {
					log.Info().Msgf("error occured: +%s", err.Error())
					continue
				}
				callChan <- Extrinsic{
					call: *call,
				}
			}
		}
	}

	return nil
}

// mint handler for stellar
func (bridge *Bridge) mint(receiver string, depositedAmount *big.Int, tx hProtocol.Transaction) error {
	log.Info().Msg("calling mint now")
	// TODO check if we already minted for this txid
	minted, err := bridge.subClient.IsMintedAlready(&bridge.identity, tx.Hash)
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

	var destinationSubstrateAddress string

	substrateAddressBytes, err := getSubstrateAddressFromStellarAddress(receiver)
	if err != nil {
		return err
	}
	destinationSubstrateAddress, err = substrate.FromEd25519Bytes(substrateAddressBytes)
	if err != nil {
		return err
	}

	// if there is a memo try to infer the destination address from it
	if tx.Memo != "" {
		destinationSubstrateAddress, err = bridge.getSubstrateAddressFromMemo(tx.Memo)
		if err != nil {
			log.Info().Msgf("error while decoding tx memo, %s", err.Error())
			// memo is not formatted correctly, issue a refund
			return bridge.refund(context.Background(), receiver, depositedAmount.Int64(), tx)
		}

	}

	log.Info().Int64("amount", depositedAmount.Int64()).Str("tx_id", tx.Hash).Msgf("target substrate address to mint on: %s", destinationSubstrateAddress)

	accountID, err := substrate.FromAddress(destinationSubstrateAddress)
	if err != nil {
		return err
	}

	call, err := bridge.subClient.ProposeOrVoteMintTransaction(&bridge.identity, tx.Hash, accountID, depositedAmount)
	if err != nil {
		return err
	}

	errChan := make(chan error)
	bridge.extrinsicsChan <- Extrinsic{
		call: *call,
		err:  errChan,
	}

	if err := <-errChan; err != nil {
		return err
	}

	log.Info().Msg("Mint succesfull, saving cursor now")
	// save cursor
	cursor := tx.PagingToken()
	err = bridge.blockPersistency.SaveStellarCursor(cursor)
	if err != nil {
		log.Error().Msgf("error while saving cursor:", err.Error())
		return err
	}

	return nil
}

// refund handler for stellar
func (bridge *Bridge) refund(ctx context.Context, destination string, amount int64, tx hProtocol.Transaction) error {
	call, err := bridge.createRefund(ctx, destination, amount, tx.Hash)
	if err != nil {
		return err
	}

	if call == nil {
		return nil
	}

	errChan := make(chan error)
	bridge.extrinsicsChan <- Extrinsic{
		call: *call,
		err:  errChan,
	}

	if err := <-errChan; err != nil {
		return err
	}

	// save cursor
	cursor := tx.PagingToken()
	log.Info().Msgf("saving cursor now %s", cursor)
	err = bridge.blockPersistency.SaveStellarCursor(cursor)
	if err != nil {
		log.Error().Msgf("error while saving cursor:", err.Error())
		return err
	}
	return nil
}

func (bridge *Bridge) createRefund(ctx context.Context, destination string, amount int64, txHash string) (*types.Call, error) {
	refunded, err := bridge.subClient.IsRefundedAlready(&bridge.identity, txHash)
	if err != nil {
		return nil, err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", txHash)
		return nil, nil
	}

	signature, sequenceNumber, err := bridge.wallet.CreateRefundAndReturnSignature(ctx, destination, uint64(amount), txHash)
	if err != nil {
		return nil, err
	}

	return bridge.subClient.CreateRefundTransactionOrAddSig(&bridge.identity, txHash, destination, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) submitRefundTransaction(ctx context.Context, refundReadyEvent subpkg.RefundTransactionReady) (*types.Call, error) {
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

func (bridge *Bridge) proposeBurnTransaction(ctx context.Context, burnCreatedEvent subpkg.BurnTransactionCreated) (*types.Call, error) {
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
	signature, sequenceNumber, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, stellarAddress, amount.Uint64(), uint64(burnCreatedEvent.BurnTransactionID))
	if err != nil {
		return nil, err
	}
	log.Info().Msgf("seq number: %d", sequenceNumber)

	return bridge.subClient.ProposeBurnTransactionOrAddSig(&bridge.identity, uint64(burnCreatedEvent.BurnTransactionID), substrate.AccountID(burnCreatedEvent.Target), amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) submitBurnTransaction(ctx context.Context, burnReadyEvent subpkg.BurnTransactionReady) (*types.Call, error) {
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

func getStellarAddressFromSubstrateAccountID(accountID substrate.AccountID) (string, error) {
	return strkey.Encode(strkey.VersionByteAccountID, accountID.PublicKey())
}

func (bridge *Bridge) Close() error {
	bridge.mut.Lock()
	defer bridge.mut.Unlock()
	return nil
}
