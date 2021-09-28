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
	"github.com/threefoldtech/tfchain_bridge/pkg"
)

const (
	TFTMainnet = "TFT:GBOVQKJYHXRR3DX6NOX2RRYFRCUMSADGDESTDNBDS6CDVLGVESRTAC47"
	TFTTest    = "TFT:GA47YZA3PKFUZMPLQ3B5F2E3CJIB57TGGU7SPCQT2WAEYKN766PWIMB3"

	stellarPrecision       = 1e7
	stellarPrecisionDigits = 7
	withdrawFee            = int64(1 * 1e7)
	depositFee             = 50 * 1e7
)

var errInsufficientDepositAmount = errors.New("deposited amount is <= Fee")

// stellarWallet is the bridge wallet
// Payments will be funded and fees will be taken with this wallet
type StellarWallet struct {
	keypair                   *keypair.Full
	config                    *pkg.StellarConfig
	stellarTransactionStorage *StellarTransactionStorage
	signatureCount            int
}

func NewStellarWallet(ctx context.Context, config *pkg.StellarConfig) (*StellarWallet, error) {
	kp, err := keypair.ParseFull(config.StellarSeed)

	if err != nil {
		return nil, err
	}

	stellarTransactionStorage := NewStellarTransactionStorage(config.StellarNetwork, kp.Address())
	w := &StellarWallet{
		keypair:                   kp,
		config:                    config,
		stellarTransactionStorage: stellarTransactionStorage,
	}

	account, err := w.GetAccountDetails(config.StellarBridgeAccount)
	if err != nil {
		return nil, err
	}
	log.Info().Msgf("required signature count %d", int(account.Thresholds.MedThreshold))
	w.signatureCount = int(account.Thresholds.MedThreshold)

	return w, nil
}

func (w *StellarWallet) CreatePaymentAndReturnSignature(ctx context.Context, target string, amount uint64, txID uint64, includeWithdrawFee bool) (string, error) {
	txnBuild, err := w.generatePaymentOperation(amount, target, includeWithdrawFee)
	if err != nil {
		return "", err
	}

	txn, err := w.createTransaction(ctx, txnBuild, true)
	if err != nil {
		return "", err
	}

	signatures := txn.Signatures()

	return base64.StdEncoding.EncodeToString(signatures[0].Signature), nil
}

func (w *StellarWallet) CreatePaymentWithSignaturesAndSubmit(ctx context.Context, target string, amount uint64, txHash string, includeWithdrawFee bool, signatures []pkg.StellarSignature) error {
	txnBuild, err := w.generatePaymentOperation(amount, target, includeWithdrawFee)
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

func (w *StellarWallet) CreateRefundPaymentWithSignaturesAndSubmit(ctx context.Context, target string, amount uint64, txHash string, includeWithdrawFee bool, signatures []pkg.StellarSignature) error {
	txnBuild, err := w.generatePaymentOperation(amount, target, includeWithdrawFee)
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

func (w *StellarWallet) GetKeypair() *keypair.Full {
	return w.keypair
}

func (w *StellarWallet) CreateRefundAndReturnSignature(ctx context.Context, target string, amount uint64, message string, includeWithdrawFee bool) (string, error) {
	txnBuild, err := w.generatePaymentOperation(amount, target, includeWithdrawFee)
	if err != nil {
		return "", err
	}

	parsedMessage, err := hex.DecodeString(message)
	if err != nil {
		return "", err
	}

	var memo [32]byte
	copy(memo[:], parsedMessage)

	txnBuild.Memo = txnbuild.MemoReturn(memo)

	txn, err := w.createTransaction(ctx, txnBuild, true)
	if err != nil {
		return "", err
	}

	signatures := txn.Signatures()

	return base64.StdEncoding.EncodeToString(signatures[0].Signature), nil
}

func (w *StellarWallet) generatePaymentOperation(amount uint64, destination string, includeWithdrawFee bool) (txnbuild.TransactionParams, error) {
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

	if includeWithdrawFee {
		feePaymentOP := txnbuild.Payment{
			Destination: w.config.StellarFeeWallet,
			Amount:      big.NewRat(withdrawFee, stellarPrecision).FloatString(stellarPrecisionDigits),
			Asset: txnbuild.CreditAsset{
				Code:   asset[0],
				Issuer: asset[1],
			},
			SourceAccount: sourceAccount.AccountID,
		}
		paymentOperations = append(paymentOperations, &feePaymentOP)
	}

	txnBuild := txnbuild.TransactionParams{
		Operations:           paymentOperations,
		Timebounds:           txnbuild.NewInfiniteTimeout(),
		SourceAccount:        &sourceAccount,
		BaseFee:              txnbuild.MinBaseFee * 3,
		IncrementSequenceNum: true,
	}

	return txnBuild, nil
}

func (w *StellarWallet) createTransaction(ctx context.Context, txn txnbuild.TransactionParams, sign bool) (*txnbuild.Transaction, error) {
	tx, err := txnbuild.NewTransaction(txn)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build transaction")
	}

	// check if a similar transaction with a memo was made before
	exists, err := w.stellarTransactionStorage.TransactionWithMemoExists(tx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to check transaction storage for existing transaction hash")
	}
	// if the transaction exists, return with nil error
	if exists {
		log.Info().Msg("Transaction with this hash already executed, skipping now..")
		return nil, errors.New("transaction with this has already executed")
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
		if hError, ok := err.(*horizonclient.Error); ok {
			log.Error().Msgf("Error submitting tx %+v", hError.Problem.Extras)
		}
		return errors.Wrap(err, "error submitting transaction")
	}
	log.Info().Msg(fmt.Sprintf("transaction: %s submitted to the stellar network..", txResult.Hash))

	w.stellarTransactionStorage.StoreTransactionWithMemo(txn)
	if err != nil {
		return errors.Wrap(err, "failed to store transaction with memo")
	}

	return nil
}

// mint handler
type mint func(string, *big.Int, string) error

// refund handler
type refund func(context.Context, string, int64, string) error

// MonitorBridgeAccountAndMint is a blocking function that keeps monitoring
// the bridge account on the Stellar network for new transactions and calls the
// mint function when a deposit is made
func (w *StellarWallet) MonitorBridgeAccountAndMint(ctx context.Context, mintFn mint, refundFn refund, persistency *pkg.ChainPersistency) error {
	transactionHandler := func(tx hProtocol.Transaction) {
		if !tx.Successful {
			return
		}
		log.Info().Str("hash", tx.Hash).Msg("Received transaction on bridge stellar account")

		// data, err := base64.StdEncoding.DecodeString(tx.Memo)
		// if err != nil {
		// 	log.Error("error decoding transaction memo", "error", err.Error())
		// 	return
		// }

		// if len(data) != 20 {
		// 	return
		// }
		// var subAddress SubstrateAddress
		// copy(subAddress[0:20], data)

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
			if effect.GetType() == "account_credited" {
				creditedEffect := effect.(horizoneffects.AccountCredited)
				if creditedEffect.Asset.Code != asset[0] && creditedEffect.Asset.Issuer != asset[1] {
					continue
				}
				parsedAmount, err := amount.ParseInt64(creditedEffect.Amount)
				if err != nil {
					continue
				}

				depositedAmount := big.NewInt(int64(parsedAmount))

				err = mintFn(tx.Account, depositedAmount, tx.Hash)
				for err != nil {
					log.Error().Msg(fmt.Sprintf("Error occured while minting: %s", err.Error()))

					if err.Error() == errInsufficientDepositAmount.Error() {
						log.Warn().Msgf("User is trying to swap less than the fee amount, refunding now", "amount", parsedAmount)
						ops, err := w.getOperationEffect(tx.Hash)
						if err != nil {
							continue
						}
						for _, op := range ops.Embedded.Records {
							if op.GetType() == "payment" {
								paymentOpation := op.(operations.Payment)

								if paymentOpation.To == w.config.StellarBridgeAccount {
									log.Warn().Msg("Calling refund")
									err := refundFn(ctx, paymentOpation.From, parsedAmount, tx.Hash)
									if err != nil {
										log.Error().Msgf("error while trying to refund user", "err", err.Error())
									}
								}
							}
						}
						return
					}

					select {
					case <-ctx.Done():
						return
					case <-time.After(10 * time.Second):
						err = mintFn(tx.Account, depositedAmount, tx.Hash)
					}
				}
				if w.config.StellarFeeWallet != "" {
					log.Info().Msgf("Trying to transfer the fees generated to the fee wallet", "address", w.config.StellarFeeWallet)

					// convert tx hash string to bytes
					parsedMessage, err := hex.DecodeString(tx.Hash)
					if err != nil {
						return
					}
					var memo [32]byte
					copy(memo[:], parsedMessage)

					err = refundFn(ctx, w.config.StellarFeeWallet, depositFee, tx.Hash)
					if err != nil {
						log.Error().Msgf("error while trying to create a payment to the fee wallet", "err", err.Error())
					}
				}

				log.Info().Msg("Mint succesfull, saving cursor now")

				// save cursor
				cursor := tx.PagingToken()
				err = persistency.SaveStellarCursor(cursor)
				if err != nil {
					log.Error().Msgf("error while saving cursor:", err.Error())
					return
				}
			}
		}

	}

	// get saved cursor
	blockHeight, err := persistency.GetHeight()
	for err != nil {
		log.Warn().Msgf("Error getting the bridge persistency", "error", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
			blockHeight, err = persistency.GetHeight()
		}
	}

	return w.StreamBridgeStellarTransactions(ctx, blockHeight.StellarCursor, transactionHandler)
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

func (w *StellarWallet) ScanBridgeAccount() error {
	return w.stellarTransactionStorage.ScanBridgeAccount()
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
