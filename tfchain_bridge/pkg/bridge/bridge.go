package bridge

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"sync"

	"github.com/centrifuge/go-substrate-rpc-client/v3/types"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	hProtocol "github.com/stellar/go/protocols/horizon"
	"github.com/stellar/go/strkey"
	"github.com/threefoldtech/substrate-client"
	subclient "github.com/threefoldtech/substrate-client"
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

	tfchainIdentity, err := substrate.NewIdentityFromSr25519Phrase(cfg.TfchainSeed)
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
	depositFee, err := subClient.GetDepositFee(tfchainIdentity)
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
			hash, err := bridge.subClient.Substrate.Call(cl, meta, bridge.identity, ext.call)
			if err != nil {
				ext.err <- err
				log.Error().Msgf("error occurred while submitting call %+v", err)
			}
			log.Info().Msgf("call submitted, hash=%s", hash.Hex())
			// close channel
			close(ext.err)
		}
	}()

	height, err := bridge.blockPersistency.GetHeight()
	if err != nil {
		return errors.Wrap(err, "failed to get block height from persistency")
	}

	go func() {
		log.Info().Msg("starting minting subscription...")
		if err := bridge.wallet.MonitorBridgeAccountAndMint(ctx, bridge.mint, height.StellarCursor); err != nil {
			panic(err)
		}
	}()

	cl, _, err := bridge.subClient.GetClient()
	if err != nil {
		return errors.Wrap(err, "failed to get client")
	}

	chainHeadsSub, err := cl.RPC.Chain.SubscribeFinalizedHeads()
	if err != nil {
		return errors.Wrap(err, "failed to subscribe to finalized heads")
	}

	currentBlockNumber, err := bridge.subClient.GetCurrentHeight()
	if err != nil {
		return errors.Wrap(err, "failed to get current height")
	}
	log.Info().Msgf("saved height is %d, need to sync from saved height until current height %d", height.LastHeight, currentBlockNumber)

	if height.LastHeight < currentBlockNumber {
		for height := height.LastHeight; height < currentBlockNumber; height++ {
			err := bridge.processEventsForHeight(height)
			if err != nil {
				return errors.Wrap(err, "failed to process events for height")

			}
		}
	}

	log.Info().Msgf("bridge synced, resuming normal operations")
	go func() {
		for {
			select {
			case head := <-chainHeadsSub.Chan():
				height, err := bridge.blockPersistency.GetHeight()
				if err != nil {
					panic(err)
				}
				for i := height.LastHeight; i < uint32(head.Number); i++ {
					err := bridge.processEventsForHeight(uint32(head.Number))
					if err != nil {
						panic(err)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

func (bridge *Bridge) processEventsForHeight(height uint32) error {
	log.Info().Msgf("fetching events for blockheight %d", height)
	records, err := bridge.subClient.GetEventsForBlock(height)
	if err != nil {
		log.Info().Msgf("failed to decode block with height %d", height)
		return err
	}

	err = bridge.processEventRecords(records)
	if err != nil {
		if err == substrate.ErrFailedToDecode {
			log.Err(err).Msgf("failed to decode events at block %d", height)
			return err
		} else {
			return err
		}
	}

	log.Debug().Msgf("events for blockheight %+v processed, saving blockheight to persistency file now...", height)
	err = bridge.blockPersistency.SaveHeight(height)
	if err != nil {
		return err
	}

	return nil
}

func (bridge *Bridge) processEventRecords(events *subclient.EventRecords) error {
	for _, e := range events.TFTBridgeModule_RefundTransactionReady {
		log.Info().Msg("found refund transaction ready event")
		call, err := bridge.submitRefundTransaction(context.Background(), e)
		if err != nil {
			log.Info().Msgf("error occured: +%s", err.Error())
			continue
		}
		bridge.extrinsicsChan <- Extrinsic{
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
		bridge.extrinsicsChan <- Extrinsic{
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
		bridge.extrinsicsChan <- Extrinsic{
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
		bridge.extrinsicsChan <- Extrinsic{
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
		bridge.extrinsicsChan <- Extrinsic{
			call: *call,
		}
	}

	return nil
}

// mint handler for stellar
func (bridge *Bridge) mint(senders map[string]*big.Int, tx hProtocol.Transaction) error {
	log.Info().Msg("calling mint now")

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

	// TODO check if we already minted for this txid
	minted, err := bridge.subClient.IsMintedAlready(bridge.identity, tx.Hash)
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
		log.Info().Msgf("error while decoding tx memo, %s", err.Error())
		// memo is not formatted correctly, issue a refund
		return bridge.refund(context.Background(), receiver, depositedAmount.Int64(), tx)
	}

	log.Info().Int64("amount", depositedAmount.Int64()).Str("tx_id", tx.Hash).Msgf("target substrate address to mint on: %s", destinationSubstrateAddress)

	accountID, err := substrate.FromAddress(destinationSubstrateAddress)
	if err != nil {
		return err
	}

	call, err := bridge.subClient.ProposeOrVoteMintTransaction(bridge.identity, tx.Hash, accountID, depositedAmount)
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
	refunded, err := bridge.subClient.IsRefundedAlready(bridge.identity, txHash)
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

	return bridge.subClient.CreateRefundTransactionOrAddSig(bridge.identity, txHash, destination, amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) submitRefundTransaction(ctx context.Context, refundReadyEvent subclient.RefundTransactionReady) (*types.Call, error) {
	refunded, err := bridge.subClient.IsRefundedAlready(bridge.identity, string(refundReadyEvent.RefundTransactionHash))
	if err != nil {
		return nil, err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", string(refundReadyEvent.RefundTransactionHash))
		return nil, errors.New("tx refunded already")
	}

	refund, err := bridge.subClient.GetRefundTransaction(bridge.identity, string(refundReadyEvent.RefundTransactionHash))
	if err != nil {
		return nil, err
	}

	err = bridge.wallet.CreateRefundPaymentWithSignaturesAndSubmit(ctx, refund.Target, uint64(refund.Amount), refund.TxHash, refund.Signatures, int64(refund.SequenceNumber))
	if err != nil {
		return nil, err
	}

	return bridge.subClient.SetRefundTransactionExecuted(bridge.identity, refund.TxHash)
}

func (bridge *Bridge) proposeBurnTransaction(ctx context.Context, burnCreatedEvent subclient.BridgeBurnTransactionCreated) (*types.Call, error) {
	log.Info().Msg("going to propose burn transaction")
	burned, err := bridge.subClient.IsBurnedAlready(bridge.identity, burnCreatedEvent.BurnTransactionID)
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
	log.Info().Msgf("seq number: %d", sequenceNumber)

	return bridge.subClient.ProposeBurnTransactionOrAddSig(bridge.identity, uint64(burnCreatedEvent.BurnTransactionID), string(burnCreatedEvent.Target), amount, signature, bridge.wallet.GetKeypair().Address(), sequenceNumber)
}

func (bridge *Bridge) submitBurnTransaction(ctx context.Context, burnReadyEvent subclient.BurnTransactionReady) (*types.Call, error) {
	burned, err := bridge.subClient.IsBurnedAlready(bridge.identity, burnReadyEvent.BurnTransactionID)

	if err != nil {
		return nil, err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", burnReadyEvent.BurnTransactionID)
		return nil, errors.New("tx burned already")
	}

	burnTx, err := bridge.subClient.GetBurnTransaction(bridge.identity, burnReadyEvent.BurnTransactionID)
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

	return bridge.subClient.SetBurnTransactionExecuted(bridge.identity, uint64(burnReadyEvent.BurnTransactionID))
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

func (bridge *Bridge) Close() error {
	bridge.mut.Lock()
	defer bridge.mut.Unlock()
	return nil
}
