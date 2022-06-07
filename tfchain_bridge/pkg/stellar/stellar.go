package stellar

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/stellar/go/amount"
	"github.com/stellar/go/clients/horizonclient"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	hProtocol "github.com/stellar/go/protocols/horizon"
	horizoneffects "github.com/stellar/go/protocols/horizon/effects"
	"github.com/stellar/go/protocols/horizon/operations"
	"github.com/stellar/go/txnbuild"
	"github.com/threefoldtech/substrate-client"
	"github.com/threefoldtech/tfchain_bridge/pkg"
)

const (
	TFTMainnet = "TFT:GBOVQKJYHXRR3DX6NOX2RRYFRCUMSADGDESTDNBDS6CDVLGVESRTAC47"
	TFTTest    = "TFT:GA47YZA3PKFUZMPLQ3B5F2E3CJIB57TGGU7SPCQT2WAEYKN766PWIMB3"

	stellarPrecision       = 1e7
	stellarPrecisionDigits = 7
)

// stellarWallet is the bridge wallet
// Payments will be funded and fees will be taken with this wallet
type StellarWallet struct {
	keypair        *keypair.Full
	config         *pkg.StellarConfig
	signatureCount int
	sequenceNumber int64
}

func NewStellarWallet(ctx context.Context, config *pkg.StellarConfig) (*StellarWallet, error) {
	kp, err := keypair.ParseFull(config.StellarSeed)

	if err != nil {
		return nil, err
	}

	w := &StellarWallet{
		keypair: kp,
		config:  config,
	}

	account, err := w.GetAccountDetails(config.StellarBridgeAccount)
	if err != nil {
		return nil, err
	}
	log.Info().Msgf("required signature count %d", int(account.Thresholds.MedThreshold))
	w.signatureCount = int(account.Thresholds.MedThreshold)

	w.sequenceNumber, err = account.GetSequenceNumber()
	if err != nil {
		return nil, err
	}
	log.Info().Msgf("account %s loaded with sequence number %d", account.AccountID, w.sequenceNumber)

	return w, nil
}

func (w *StellarWallet) CreatePaymentAndReturnSignature(ctx context.Context, target string, amount uint64, txID uint64) (string, uint64, error) {
	txnBuild, err := w.generatePaymentOperation(amount, target, 0)
	if err != nil {
		return "", 0, err
	}

	txn, err := w.createTransaction(ctx, txnBuild, true)
	if err != nil {
		return "", 0, err
	}

	signatures := txn.Signatures()

	return base64.StdEncoding.EncodeToString(signatures[0].Signature), uint64(txn.SequenceNumber()), nil
}

func (w *StellarWallet) CreatePaymentWithSignaturesAndSubmit(ctx context.Context, target string, amount uint64, txHash string, signatures []substrate.StellarSignature, sequenceNumber int64) error {
	txnBuild, err := w.generatePaymentOperation(amount, target, sequenceNumber)
	if err != nil {
		return err
	}

	txn, err := w.createTransaction(ctx, txnBuild, false)
	if err != nil {
		return err
	}

	if len(signatures) < w.signatureCount {
		return errors.New("not enough signatures, aborting")
	}

	requiredSignatures := signatures[:w.signatureCount]
	for _, sig := range requiredSignatures {
		log.Debug().Msgf("adding signature %s, account %s", string(sig.Signature), string(sig.StellarAddress))
		txn, err = txn.AddSignatureBase64(w.GetNetworkPassPhrase(), string(sig.StellarAddress), string(sig.Signature))
		if err != nil {
			return err
		}
	}

	return w.submitTransaction(ctx, txn)
}

func (w *StellarWallet) CreateRefundPaymentWithSignaturesAndSubmit(ctx context.Context, target string, amount uint64, txHash string, signatures []substrate.StellarSignature, sequenceNumber int64) error {
	txnBuild, err := w.generatePaymentOperation(amount, target, sequenceNumber)
	if err != nil {
		return err
	}

	parsedMessage, err := hex.DecodeString(txHash)
	if err != nil {
		return err
	}

	var memo [32]byte
	copy(memo[:], parsedMessage)

	txnBuild.Memo = txnbuild.MemoReturn(memo)

	txn, err := w.createTransaction(ctx, txnBuild, false)
	if err != nil {
		return err
	}

	if len(signatures) < w.signatureCount {
		return errors.New("not enough signatures, aborting")
	}

	requiredSignatures := signatures[:w.signatureCount]
	for _, sig := range requiredSignatures {
		log.Debug().Msgf("adding signature %s, account %s", string(sig.Signature), string(sig.StellarAddress))
		txn, err = txn.AddSignatureBase64(w.GetNetworkPassPhrase(), string(sig.StellarAddress), string(sig.Signature))
		if err != nil {
			return err
		}
	}

	return w.submitTransaction(ctx, txn)
}

func (w *StellarWallet) CreateRefundAndReturnSignature(ctx context.Context, target string, amount uint64, message string) (string, uint64, error) {
	txnBuild, err := w.generatePaymentOperation(amount, target, 0)
	if err != nil {
		return "", 0, err
	}

	parsedMessage, err := hex.DecodeString(message)
	if err != nil {
		return "", 0, err
	}

	var memo [32]byte
	copy(memo[:], parsedMessage)

	txnBuild.Memo = txnbuild.MemoReturn(memo)

	txn, err := w.createTransaction(ctx, txnBuild, true)
	if err != nil {
		return "", 0, err
	}

	signatures := txn.Signatures()

	return base64.StdEncoding.EncodeToString(signatures[0].Signature), uint64(txn.SequenceNumber()), nil
}

func (w *StellarWallet) generatePaymentOperation(amount uint64, destination string, sequenceNumber int64) (txnbuild.TransactionParams, error) {
	// if amount is zero, do nothing
	if amount == 0 {
		return txnbuild.TransactionParams{}, errors.New("invalid amount")
	}

	sourceAccount, err := w.GetAccountDetails(w.config.StellarBridgeAccount)
	if err != nil {
		return txnbuild.TransactionParams{}, errors.Wrap(err, "failed to get source account")
	}

	asset := w.GetAssetCodeAndIssuer()

	var paymentOperations []txnbuild.Operation
	paymentOP := txnbuild.Payment{
		Destination: destination,
		Amount:      big.NewRat(int64(amount), stellarPrecision).FloatString(stellarPrecisionDigits),
		Asset: txnbuild.CreditAsset{
			Code:   asset[0],
			Issuer: asset[1],
		},
		SourceAccount: sourceAccount.AccountID,
	}
	paymentOperations = append(paymentOperations, &paymentOP)

	if sequenceNumber == 0 {
		w.sequenceNumber = w.sequenceNumber + 1
	} else {
		w.sequenceNumber = int64(sequenceNumber)
	}

	txnBuild := txnbuild.TransactionParams{
		Operations:           paymentOperations,
		Timebounds:           txnbuild.NewInfiniteTimeout(),
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: sourceAccount.AccountID, Sequence: w.sequenceNumber},
		BaseFee:              txnbuild.MinBaseFee * 1000,
		IncrementSequenceNum: false,
	}

	return txnBuild, nil
}

func (w *StellarWallet) createTransaction(ctx context.Context, txn txnbuild.TransactionParams, sign bool) (*txnbuild.Transaction, error) {
	tx, err := txnbuild.NewTransaction(txn)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build transaction")
	}

	if sign {
		tx, err = tx.Sign(w.GetNetworkPassPhrase(), w.keypair)
		if err != nil {
			if hError, ok := err.(*horizonclient.Error); ok {
				log.Error().Msgf("Error submitting tx %+v", hError.Problem.Extras)
			}
			return nil, errors.Wrap(err, "failed to sign transaction with keypair")
		}
	}

	return tx, nil
}

func (w *StellarWallet) submitTransaction(ctx context.Context, txn *txnbuild.Transaction) error {
	client, err := w.GetHorizonClient()
	if err != nil {
		return errors.Wrap(err, "failed to get horizon client")
	}

	// Submit the transaction
	txResult, err := client.SubmitTransaction(txn)
	if err != nil {
		log.Info().Msg(err.Error())
		if hError, ok := err.(*horizonclient.Error); ok {
			if ok {
				log.Err(err).Msgf("error while submitting transaction %+v", hError.Problem.Extras)
			}
		}
		errSequence := w.resetAccountSequence()
		if errSequence != nil {
			return errSequence
		}
		return errors.Wrap(err, "error submitting transaction")
	}
	log.Info().Msg(fmt.Sprintf("transaction: %s submitted to the stellar network..", txResult.Hash))
	return nil
}

func (w *StellarWallet) resetAccountSequence() error {
	log.Info().Msgf("resetting account sequence")
	account, err := w.GetAccountDetails(w.config.StellarBridgeAccount)
	if err != nil {
		return err
	}

	w.sequenceNumber, err = account.GetSequenceNumber()
	if err != nil {
		return err
	}

	return nil
}

func (w *StellarWallet) GetKeypair() *keypair.Full {
	return w.keypair
}

// mint handler
type mint func(map[string]*big.Int, hProtocol.Transaction) error

// // refund handler
// type refund func(context.Context, string, int64, string) error

// MonitorBridgeAccountAndMint is a blocking function that keeps monitoring
// the bridge account on the Stellar network for new transactions and calls the
// mint function when a deposit is made
func (w *StellarWallet) MonitorBridgeAccountAndMint(ctx context.Context, mintFn mint, stellarCursor string) error {
	transactionHandler := func(tx hProtocol.Transaction) {
		if !tx.Successful {
			return
		}
		log.Info().Str("hash", tx.Hash).Msg("Received transaction on bridge stellar account")

		effects, err := w.getTransactionEffects(tx.Hash)
		if err != nil {
			log.Error().Str("error while fetching transaction effects:", err.Error())
			return
		}

		asset := w.GetAssetCodeAndIssuer()

		for _, effect := range effects.Embedded.Records {
			if effect.GetAccount() != w.config.StellarBridgeAccount {
				continue
			}

			if effect.GetType() != "account_credited" {
				continue
			}

			creditedEffect := effect.(horizoneffects.AccountCredited)
			if creditedEffect.Asset.Code != asset[0] && creditedEffect.Asset.Issuer != asset[1] {
				continue
			}

			ops, err := w.getOperationEffect(tx.Hash)
			if err != nil {
				continue
			}

			senders := make(map[string]*big.Int)
			for _, op := range ops.Embedded.Records {
				if op.GetType() != "payment" {
					return
				}

				paymentOpation := op.(operations.Payment)
				if paymentOpation.To != w.config.StellarBridgeAccount {
					continue
				}

				parsedAmount, err := amount.ParseInt64(paymentOpation.Amount)
				if err != nil {
					continue
				}

				depositedAmount := big.NewInt(int64(parsedAmount))
				if _, ok := senders[paymentOpation.From]; !ok {
					senders[paymentOpation.From] = depositedAmount
				} else {
					senderAmount := senders[paymentOpation.From]
					senderAmount = senderAmount.Add(senderAmount, depositedAmount)
					senders[paymentOpation.From] = senderAmount
				}
			}

			err = mintFn(senders, tx)
			for err != nil {
				log.Error().Msg(fmt.Sprintf("Error occured while minting: %s", err.Error()))
				if errors.Is(err, pkg.ErrTransactionAlreadyRefunded) {
					return
				}

				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Second):
					err = mintFn(senders, tx)
				}
			}

		}
	}
	return w.StreamBridgeStellarTransactions(ctx, stellarCursor, transactionHandler)
}

// GetAccountDetails gets account details based an a Stellar address
func (w *StellarWallet) GetAccountDetails(address string) (account hProtocol.Account, err error) {
	client, err := w.GetHorizonClient()
	if err != nil {
		return hProtocol.Account{}, err
	}
	ar := horizonclient.AccountRequest{AccountID: address}
	account, err = client.AccountDetail(ar)
	if err != nil {
		return hProtocol.Account{}, errors.Wrapf(err, "failed to get account details for account: %s", address)
	}
	return account, nil
}

func (w *StellarWallet) StreamBridgeStellarTransactions(ctx context.Context, cursor string, handler func(op hProtocol.Transaction)) error {
	client, err := w.GetHorizonClient()
	if err != nil {
		return err
	}

	opRequest := horizonclient.TransactionRequest{
		ForAccount: w.config.StellarBridgeAccount,
		Cursor:     cursor,
	}
	log.Info().Msgf("Start fetching stellar transactions", "horizon", client.HorizonURL, "account", opRequest.ForAccount, "cursor", opRequest.Cursor)

	for {
		if ctx.Err() != nil {
			return nil
		}

		response, err := client.Transactions(opRequest)
		if err != nil {
			log.Info().Msgf("Error getting transactions for stellar account", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
				continue
			}

		}
		for _, tx := range response.Embedded.Records {
			handler(tx)
			opRequest.Cursor = tx.PagingToken()
		}
		if len(response.Embedded.Records) == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
			}
		}

	}

}

func (w *StellarWallet) getTransactionEffects(txHash string) (effects horizoneffects.EffectsPage, err error) {
	client, err := w.GetHorizonClient()
	if err != nil {
		return effects, err
	}

	effectsReq := horizonclient.EffectRequest{
		ForTransaction: txHash,
	}
	effects, err = client.Effects(effectsReq)
	if err != nil {
		return effects, err
	}

	return effects, nil
}

func (w *StellarWallet) getOperationEffect(txHash string) (ops operations.OperationsPage, err error) {
	client, err := w.GetHorizonClient()
	if err != nil {
		return ops, err
	}

	opsRequest := horizonclient.OperationRequest{
		ForTransaction: txHash,
	}
	ops, err = client.Operations(opsRequest)
	if err != nil {
		return ops, err
	}

	return ops, nil
}

// GetHorizonClient gets the horizon client based on the wallet's network
func (w *StellarWallet) GetHorizonClient() (*horizonclient.Client, error) {
	if w.config.StellarHorizonUrl != "" {
		return &horizonclient.Client{HorizonURL: w.config.StellarHorizonUrl}, nil
	}

	switch w.config.StellarNetwork {
	case "testnet":
		return horizonclient.DefaultTestNetClient, nil
	case "production":
		return horizonclient.DefaultPublicNetClient, nil
	default:
		return nil, errors.New("network is not supported")
	}
}

// GetNetworkPassPhrase gets the Stellar network passphrase based on the wallet's network
func (w *StellarWallet) GetNetworkPassPhrase() string {
	switch w.config.StellarNetwork {
	case "testnet":
		return network.TestNetworkPassphrase
	case "production":
		return network.PublicNetworkPassphrase
	default:
		return network.TestNetworkPassphrase
	}
}

func (w *StellarWallet) GetAssetCodeAndIssuer() []string {
	switch w.config.StellarNetwork {
	case "testnet":
		return strings.Split(TFTTest, ":")
	case "production":
		return strings.Split(TFTMainnet, ":")
	default:
		return strings.Split(TFTTest, ":")
	}
}
