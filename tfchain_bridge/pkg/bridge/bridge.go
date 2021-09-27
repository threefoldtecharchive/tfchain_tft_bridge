package bridge

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"

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
	// Depositing from Stellar to smart chain fee
	DepositFee = 50 * 1e7
	// Withdrawing from smartchain to Stellar fee
	WithdrawFee   = int64(1 * 1e7)
	BridgeNetwork = "stellar"
)

// Bridge is a high lvl structure which listens on contract events and bridge-related
// stellar transactions, and handles them
type Bridge struct {
	wallet           *stellar.StellarWallet
	subClient        *substrate.Substrate
	identity         substrate.Identity
	blockPersistency *pkg.ChainPersistency
	mut              sync.Mutex
	config           *pkg.BridgeConfig
	depositFee       *big.Int
}

func NewBridge(ctx context.Context, cfg pkg.BridgeConfig) (*Bridge, error) {
	subClient, err := substrate.NewSubstrate(cfg.TfchainURL)
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
	}

	var depositFee big.Int
	depositFee.SetInt64(DepositFee)
	bridge := &Bridge{
		subClient:        subClient,
		identity:         tfchainIdentity,
		blockPersistency: blockPersistency,
		wallet:           wallet,
		config:           &cfg,
		depositFee:       &depositFee,
	}

	return bridge, nil
}

func (bridge *Bridge) Start(ctx context.Context) error {
	go func() {
		log.Info().Msg("starting minting subscription...")
		if err := bridge.wallet.MonitorBridgeAccountAndMint(ctx, bridge.mint, bridge.refund, bridge.blockPersistency); err != nil {
			panic(err)
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

	// Channel where withdrawal events are stored
	// Should only be read from by the master bridge
	burnChan := make(chan substrate.BurnTransactionCreated)
	burnReadyChan := make(chan substrate.BurnTransactionReady)
	refundReadyChan := make(chan substrate.RefundTransactionReady)

	go func() {
		for {
			select {
			case burn := <-burnChan:
				log.Info().Int("id", int(burn.BurnTransactionID)).Int64("amount", int64(burn.Amount)).Str("target", burn.Target.String()).Msgf("received burn event")
				err := bridge.proposeBurnTransactionOrAddSig(ctx, burn)
				if err != nil {
					log.Error().Msgf("error occurred while proposing burn tx %+v", err)
				}
			case burnReady := <-burnReadyChan:
				log.Info().Int("id", int(burnReady.BurnTransactionID)).Msgf("received burn ready event")
				err := bridge.submitBurnTransaction(ctx, burnReady)
				if err != nil {
					log.Error().Msgf("error occurred while submitting burn tx %+v", err)
				}
			case refundReady := <-refundReadyChan:
				log.Info().Str("txhash", string(refundReady.RefundTransactionHash)).Msgf("received refund ready event")
				err := bridge.submitRefundTransaction(ctx, refundReady)
				if err != nil {
					log.Error().Msgf("error occurred while submitting refund tx %+v", err)
				}
			}

		}
	}()

	go func() {
		log.Info().Msg("started subs...")
		if err := bridge.subClient.SubscribeEvents(burnChan, burnReadyChan, refundReadyChan, bridge.blockPersistency); err != nil {
			if err != substrate.ErrFailedToDecode {
				panic(err)
			}
		}
	}()

	if height.LastHeight < currentBlockNumber {
		// TODO replay all events from lastheight until current height
		log.Info().Msgf("saved height is %d, need to sync from saved height until current height..", height.LastHeight)
		key, set, err := bridge.subClient.FetchEventsForBlockRange(height.LastHeight, currentBlockNumber)
		if err != nil {
			return err
		}

		err = bridge.subClient.ProcessEvents(burnChan, burnReadyChan, refundReadyChan, key, set)
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

	if depositedAmount.Cmp(bridge.depositFee) <= 0 {
		log.Error().Int("amount", int(depositedAmount.Int64())).Str("txID", txID).Msg("Deposited amount is <= Fee, should be returned")
		return errInsufficientDepositAmount
	}
	amount := &big.Int{}
	amount.Sub(depositedAmount, bridge.depositFee)
	// multiply the amount of tokens to be minted * the multiplier
	amount.Mul(amount, big.NewInt(bridge.config.TokenMultiplier))

	substrateAddressBytes, err := getSubstrateAddressFromStellarAddress(receiver)
	if err != nil {
		return err
	}
	log.Info().Msgf("substrate address bytes %+v", substrateAddressBytes)

	substrateAddress, err := substrate.FromEd25519Bytes(substrateAddressBytes)
	if err != nil {
		return err
	}
	log.Info().Msgf("substrate address %s", substrateAddress)

	accountID, err := substrate.FromAddress(substrateAddress)
	if err != nil {
		return err
	}

	err = bridge.subClient.ProposeOrVoteMintTransaction(&bridge.identity, txID, accountID, amount)
	if err != nil {
		return err
	}

	return nil
}

func (bridge *Bridge) proposeBurnTransactionOrAddSig(ctx context.Context, burnCreatedEvent substrate.BurnTransactionCreated) error {
	burned, err := bridge.subClient.IsBurnedAlready(&bridge.identity, burnCreatedEvent.BurnTransactionID)
	log.Info().Msgf("TX burned? %+v, %+v", burned, err)

	if err != nil {
		return err
	}

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", burnCreatedEvent.BurnTransactionID)
		return nil
	}

	stellarAddress, err := getStellarAddressFromSubstrateAccountID(burnCreatedEvent.Target)
	if err != nil {
		return err
	}

	signature, err := bridge.wallet.CreatePaymentAndReturnSignature(ctx, stellarAddress, uint64(burnCreatedEvent.Amount), uint64(burnCreatedEvent.BurnTransactionID), false)
	if err != nil {
		return err
	}

	amount := big.NewInt(int64(burnCreatedEvent.Amount))
	err = bridge.subClient.ProposeBurnTransactionOrAddSig(&bridge.identity, uint64(burnCreatedEvent.BurnTransactionID), substrate.AccountID(burnCreatedEvent.Target), amount, signature, bridge.wallet.GetKeypair().Address())
	if err != nil {
		return err
	}

	return nil
}

func (bridge *Bridge) submitBurnTransaction(ctx context.Context, burnReadyEvent substrate.BurnTransactionReady) error {
	burned, err := bridge.subClient.IsBurnedAlready(&bridge.identity, burnReadyEvent.BurnTransactionID)
	log.Info().Msgf("TX burned? %+v, %+v", burned, err)

	if err != nil {
		return err
	}

	log.Info().Msgf("TX burned? %+v, %+v", burned, err)

	if burned {
		log.Info().Msgf("tx with id: %d is burned already, skipping...", burnReadyEvent.BurnTransactionID)
		return nil
	}

	burnTx, err := bridge.subClient.GetBurnTransaction(&bridge.identity, burnReadyEvent.BurnTransactionID)
	if err != nil {
		return err
	}

	stellarAddress, err := getStellarAddressFromSubstrateAccountID(substrate.AccountID(burnTx.Target))
	if err != nil {
		return err
	}

	// todo add memo hash
	err = bridge.wallet.CreatePaymentWithSignaturesAndSubmit(ctx, stellarAddress, uint64(burnTx.Amount), "", false, burnTx.Signatures)
	if err != nil {
		return err
	}

	return bridge.subClient.SetBurnTransactionExecuted(&bridge.identity, uint64(burnReadyEvent.BurnTransactionID))
}

func (bridge *Bridge) refund(ctx context.Context, destination string, amount int64, txHash string) error {
	refunded, err := bridge.subClient.IsRefundedAlready(&bridge.identity, txHash)
	log.Info().Msgf("TX refunded? %+v, %+v", refunded, err)

	if err != nil {
		return err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", txHash)
		return nil
	}

	signature, err := bridge.wallet.CreateRefundAndReturnSignature(ctx, destination, uint64(amount), txHash, false)
	if err != nil {
		return err
	}

	err = bridge.subClient.CreateRefundTransactionOrAddSig(&bridge.identity, txHash, destination, amount, signature, bridge.wallet.GetKeypair().Address())
	if err != nil {
		return err
	}

	return nil
}

func (bridge *Bridge) submitRefundTransaction(ctx context.Context, refundReadyEvent substrate.RefundTransactionReady) error {
	refunded, err := bridge.subClient.IsRefundedAlready(&bridge.identity, string(refundReadyEvent.RefundTransactionHash))
	log.Info().Msgf("TX refunded? %+v, %+v", refunded, err)

	if err != nil {
		return err
	}

	if refunded {
		log.Info().Msgf("tx with stellar tx hash: %s is refunded already, skipping...", string(refundReadyEvent.RefundTransactionHash))
		return nil
	}

	refund, err := bridge.subClient.GetRefundTransaction(&bridge.identity, string(refundReadyEvent.RefundTransactionHash))
	if err != nil {
		return err
	}

	err = bridge.wallet.CreatePaymentWithSignaturesAndSubmit(ctx, refund.Target, uint64(refund.Amount), refund.TxHash, false, refund.Signatures)
	if err != nil {
		return err
	}

	return bridge.subClient.SetRefundTransactionExecuted(&bridge.identity, refund.TxHash)
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
