package signer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

// ErrHardwareBlindSigningDisabled rejects hash-only EIP-712 without opt-in.
var ErrHardwareBlindSigningDisabled = errors.New("structured dispatcher: hardware blind EIP-712 signing is disabled")

// StructuredDispatcherOptions controls explicit hardware signing policy.
type StructuredDispatcherOptions struct {
	AllowBlindHardwareEIP712 bool
}

// StructuredDispatcher routes immutable intents without degrading hardware
// requests to opaque digests.
type StructuredDispatcher struct {
	software            evm.StructuredSigner
	cloud               evm.StructuredSigner
	ledger              *LedgerSigner
	trezor              *TrezorSigner
	accounts            AccountLookup
	transactionVerifier wallet.TransactionApprovalVerifier
	options             StructuredDispatcherOptions
}

// NewStructuredDispatcher creates a structured signer router.
func NewStructuredDispatcher(
	software, cloud evm.StructuredSigner,
	ledger *LedgerSigner,
	trezor *TrezorSigner,
	accounts AccountLookup,
	transactionVerifier wallet.TransactionApprovalVerifier,
	options StructuredDispatcherOptions,
) (*StructuredDispatcher, error) {
	if software == nil || accounts == nil || transactionVerifier == nil {
		return nil, fmt.Errorf("structured dispatcher: software signer, accounts, and verifier required")
	}
	return &StructuredDispatcher{
		software: software, cloud: cloud, ledger: ledger, trezor: trezor,
		accounts: accounts, transactionVerifier: transactionVerifier, options: options,
	}, nil
}

// SignTransaction routes a complete frozen transaction intent.
func (dispatcher *StructuredDispatcher) SignTransaction(ctx context.Context, handle wallet.CapabilityHandle, intent evm.TransactionSigningIntent) (wallet.SoftwareSigningResult, error) {
	if err := intent.Validate(); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	account, err := dispatcher.account(ctx, intent.AccountID, intent.From)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	switch account.SignerKind {
	case wallet.SignerKindSoftware:
		return dispatcher.software.SignTransaction(ctx, handle, intent)
	case wallet.SignerKindCloud:
		if dispatcher.cloud == nil {
			return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: cloud signer unavailable")
		}
		return dispatcher.cloud.SignTransaction(ctx, handle, intent)
	case wallet.SignerKindHardware:
		if err := dispatcher.transactionVerifier.VerifyTransactionApproval(ctx, wallet.TransactionApprovalBinding{
			AccountID: intent.AccountID, ChainID: intent.ChainID, Digest: intent.Digest, ApprovalID: intent.ApprovalID,
		}); err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
		return dispatcher.signHardwareTransaction(ctx, account, intent)
	default:
		return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: signer kind %q cannot sign EOA transaction", account.SignerKind)
	}
}

// SignPersonalMessage routes the complete raw EIP-191 message.
func (dispatcher *StructuredDispatcher) SignPersonalMessage(ctx context.Context, handle wallet.CapabilityHandle, intent evm.PersonalMessageSigningIntent) (wallet.SoftwareSigningResult, error) {
	if err := intent.Validate(); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	account, err := dispatcher.account(ctx, intent.AccountID, intent.Signer)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	switch account.SignerKind {
	case wallet.SignerKindSoftware:
		return dispatcher.software.SignPersonalMessage(ctx, handle, intent)
	case wallet.SignerKindCloud:
		if dispatcher.cloud == nil {
			return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: cloud signer unavailable")
		}
		return dispatcher.cloud.SignPersonalMessage(ctx, handle, intent)
	case wallet.SignerKindHardware:
		if strings.HasPrefix(account.SignerReference, "ledger:v1:") {
			if dispatcher.ledger == nil {
				return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: Ledger unavailable")
			}
			return dispatcher.ledger.SignPersonalMessage(ctx, LedgerPersonalMessageRequest{
				AccountID: intent.AccountID, Message: append([]byte(nil), intent.Message...),
				IntentHash: intent.IntentHash, ApprovalID: intent.ApprovalID,
			})
		}
		if strings.HasPrefix(account.SignerReference, "trezor:v1:") {
			if dispatcher.trezor == nil {
				return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: Trezor unavailable")
			}
			return dispatcher.trezor.SignPersonalMessage(ctx, TrezorPersonalMessageRequest{
				AccountID: intent.AccountID, Message: append([]byte(nil), intent.Message...),
				IntentHash: intent.IntentHash, ApprovalID: intent.ApprovalID,
			})
		}
	}
	return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: unsupported personal-message route")
}

// SignEIP712 routes canonical typed data or hash pairs according to device
// capabilities.
func (dispatcher *StructuredDispatcher) SignEIP712(ctx context.Context, handle wallet.CapabilityHandle, intent evm.EIP712SigningIntent) (wallet.SoftwareSigningResult, error) {
	if err := intent.Validate(); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	account, err := dispatcher.account(ctx, intent.AccountID, intent.Signer)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	switch account.SignerKind {
	case wallet.SignerKindSoftware:
		return dispatcher.software.SignEIP712(ctx, handle, intent)
	case wallet.SignerKindCloud:
		if dispatcher.cloud == nil {
			return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: cloud signer unavailable")
		}
		return dispatcher.cloud.SignEIP712(ctx, handle, intent)
	case wallet.SignerKindHardware:
		if strings.HasPrefix(account.SignerReference, "ledger:v1:") {
			if dispatcher.ledger == nil {
				return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: Ledger unavailable")
			}
			if !dispatcher.options.AllowBlindHardwareEIP712 {
				return wallet.SoftwareSigningResult{}, ErrHardwareBlindSigningDisabled
			}
			return dispatcher.ledger.SignTypedHash(ctx, LedgerTypedHashRequest{
				AccountID: intent.AccountID, ChainID: intent.ChainID,
				DomainSeparatorHash: intent.DomainSeparatorHash, MessageHash: intent.MessageHash,
				IntentHash: intent.IntentHash, ApprovalID: intent.ApprovalID,
			})
		}
		if strings.HasPrefix(account.SignerReference, "trezor:v1:") {
			if dispatcher.trezor == nil {
				return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: Trezor unavailable")
			}
			if !dispatcher.options.AllowBlindHardwareEIP712 {
				features, err := dispatcher.trezor.ensureReady(ctx)
				if err != nil {
					return wallet.SoftwareSigningResult{}, err
				}
				if features.Model == "1" {
					return wallet.SoftwareSigningResult{}, ErrHardwareBlindSigningDisabled
				}
			}
			return dispatcher.trezor.SignStructuredTypedData(ctx, TrezorStructuredTypedDataRequest{
				AccountID: intent.AccountID, ChainID: intent.ChainID,
				CanonicalJSON:       append([]byte(nil), intent.CanonicalJSON...),
				DomainSeparatorHash: intent.DomainSeparatorHash, MessageHash: intent.MessageHash,
				IntentHash: intent.IntentHash, ApprovalID: intent.ApprovalID,
			})
		}
	}
	return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: unsupported EIP-712 route")
}

func (dispatcher *StructuredDispatcher) signHardwareTransaction(ctx context.Context, account *wallet.Account, intent evm.TransactionSigningIntent) (wallet.SoftwareSigningResult, error) {
	transaction, err := intent.Transaction()
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	address := common.HexToAddress(account.Address)
	switch {
	case strings.HasPrefix(account.SignerReference, "ledger:v1:"):
		if dispatcher.ledger == nil {
			return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: Ledger unavailable")
		}
		if err := dispatcher.ledger.ensureSecureApp(ctx); err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
		path := strings.TrimPrefix(account.SignerReference, "ledger:v1:")
		signature, err := dispatcher.ledger.device.SignTransaction(ctx, LedgerTransactionIntent{
			UnsignedTransaction: transaction, ChainID: new(big.Int).SetUint64(intent.ChainID),
			DerivationPath: path, Digest: intent.Digest, ExpectedAddress: address,
		})
		if err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
		return transactionSigningResult(intent, signature), nil
	case strings.HasPrefix(account.SignerReference, "trezor:v1:"):
		if dispatcher.trezor == nil {
			return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: Trezor unavailable")
		}
		device, ok := dispatcher.trezor.device.(TrezorTransactionDevice)
		if !ok {
			return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: Trezor transaction capability unavailable")
		}
		if _, err := dispatcher.trezor.ensureReady(ctx); err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
		path := strings.TrimPrefix(account.SignerReference, "trezor:v1:")
		signature, err := device.SignTransaction(ctx, TrezorTransactionIntent{
			UnsignedTransaction: transaction, ChainID: new(big.Int).SetUint64(intent.ChainID),
			DerivationPath: path, Digest: intent.Digest, ExpectedAddress: address,
		})
		if err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
		return transactionSigningResult(intent, signature), nil
	default:
		return wallet.SoftwareSigningResult{}, fmt.Errorf("structured dispatcher: unknown hardware reference")
	}
}

func (dispatcher *StructuredDispatcher) account(ctx context.Context, accountID string, expected common.Address) (*wallet.Account, error) {
	if dispatcher == nil || dispatcher.accounts == nil {
		return nil, fmt.Errorf("structured dispatcher: unavailable")
	}
	account, err := dispatcher.accounts.GetAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("structured dispatcher: account: %w", err)
	}
	if account == nil || account.AccountID != accountID || common.HexToAddress(account.Address) != expected || !account.SignerKind.SupportsEOASigning() || (account.State != wallet.AccountStateActive && account.State != wallet.AccountStateLocked) {
		return nil, fmt.Errorf("structured dispatcher: account binding mismatch")
	}
	return account, nil
}

func transactionSigningResult(intent evm.TransactionSigningIntent, signature []byte) wallet.SoftwareSigningResult {
	return wallet.SoftwareSigningResult{
		AccountID: intent.AccountID, Purpose: wallet.SigningPurposeTransaction,
		ChainID: intent.ChainID, Digest: intent.Digest, Signature: append([]byte(nil), signature...),
	}
}
