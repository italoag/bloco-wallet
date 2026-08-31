package signer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

var (
	// ErrTrezorLocked is returned when the device requires PIN/passphrase
	// entry that has not completed.
	ErrTrezorLocked = errors.New("trezor signer: device locked")
	// ErrTrezorSignature is returned when the device signature does not
	// recover the expected account address.
	ErrTrezorSignature = errors.New("trezor signer: signature verification failed")
)

// TrezorFeatures describes the device after initialization, mirroring the
// Initialize message of the Trezor protocol.
type TrezorFeatures struct {
	Model                string
	Version              string
	Initialized          bool
	PinProtection        bool
	PassphraseProtection bool
}

// TrezorDevice is the transport contract derived from the Trezor protocol
// messages (Initialize, EthereumGetPublicKey, EthereumSignTypedMessage).
// The emulator and physical devices both speak these messages; test
// transports model the firmware responses.
type TrezorDevice interface {
	Initialize(ctx context.Context) (TrezorFeatures, error)
	EthereumGetPublicKey(ctx context.Context, derivationPath string) ([]byte, error)
	EthereumSignTypedMessage(ctx context.Context, derivationPath string, messageHash [32]byte) ([]byte, error)
}

// TrezorSigner signs approved digests through a Trezor device. The device
// never reveals the private key; every signature is verified against the
// account address before exposure.
type TrezorSigner struct {
	device                      TrezorDevice
	accounts                    AccountLookup
	transactionApprovalVerifier wallet.TransactionApprovalVerifier
	messageApprovalVerifier     wallet.MessageApprovalVerifier
}

// NewTrezorSigner creates the hardware signer.
func NewTrezorSigner(device TrezorDevice, accounts AccountLookup, transactionVerifier wallet.TransactionApprovalVerifier, messageVerifier wallet.MessageApprovalVerifier) (*TrezorSigner, error) {
	if device == nil || accounts == nil || transactionVerifier == nil || messageVerifier == nil {
		return nil, fmt.Errorf("trezor signer: device, accounts, and verifiers are required")
	}
	return &TrezorSigner{
		device: device, accounts: accounts,
		transactionApprovalVerifier: transactionVerifier,
		messageApprovalVerifier:     messageVerifier,
	}, nil
}

// Sign implements ApprovedDigestSigner.
func (signer *TrezorSigner) Sign(ctx context.Context, handle wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	if signer == nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: nil signer")
	}
	account, err := signer.accounts.GetAccount(ctx, request.AccountID)
	if err != nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: account: %w", err)
	}
	if account.SignerKind != wallet.SignerKindHardware {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: account is not a hardware account")
	}
	derivationPath, err := trezorDerivationPath(account.SignerReference)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	expectedAddress := common.HexToAddress(account.Address)
	if expectedAddress == (common.Address{}) {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: invalid account address")
	}
	switch request.Purpose {
	case wallet.SigningPurposeTransaction:
		if err := signer.transactionApprovalVerifier.VerifyTransactionApproval(ctx, wallet.TransactionApprovalBinding{
			AccountID: request.AccountID, ChainID: request.ChainID, Digest: request.Digest, ApprovalID: request.ApprovalID,
		}); err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
	case wallet.SigningPurposeMessage:
		if err := signer.messageApprovalVerifier.VerifyMessageApproval(ctx, wallet.MessageApprovalBinding{
			AccountID: request.AccountID, Scheme: request.MessageScheme, ChainID: request.ChainID,
			Digest: request.Digest, IntentHash: request.IntentHash, ApprovalID: request.ApprovalID,
		}); err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
	default:
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: unsupported purpose")
	}
	features, err := signer.device.Initialize(ctx)
	if err != nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: initialize: %w", err)
	}
	if !features.Initialized {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: device not initialized")
	}
	if features.PinProtection || features.PassphraseProtection {
		// The firmware marks PIN/passphrase entry on the device itself; the
		// transport reports when entry is still pending.
		return wallet.SoftwareSigningResult{}, ErrTrezorLocked
	}
	signature, err := signer.device.EthereumSignTypedMessage(ctx, derivationPath, request.Digest)
	if err != nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: sign: %w", err)
	}
	signature, err = normalizeSignature(signature)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := verifyECDSASignature(expectedAddress, request.Digest, signature); err != nil {
		return wallet.SoftwareSigningResult{}, ErrTrezorSignature
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: request.Purpose, MessageScheme: request.MessageScheme,
		ChainID: request.ChainID, Digest: request.Digest, IntentHash: request.IntentHash,
		Signature: append([]byte(nil), signature...),
	}, nil
}

// normalizeSignature accepts the firmware v encoding (0/1 or 27/28) and
// produces the canonical 0/1 form.
func normalizeSignature(signature []byte) ([]byte, error) {
	if len(signature) != 65 {
		return nil, fmt.Errorf("trezor signer: signature size")
	}
	normalized := append([]byte(nil), signature...)
	switch normalized[64] {
	case 27:
		normalized[64] = 0
	case 28:
		normalized[64] = 1
	case 0, 1:
	default:
		return nil, fmt.Errorf("trezor signer: signature recovery id")
	}
	return normalized, nil
}

// trezorDerivationPath extracts the canonical path from a Trezor signer
// reference ("trezor:v1:m/44'/60'/0'/0/0").
func trezorDerivationPath(reference string) (string, error) {
	const prefix = "trezor:v1:"
	if !strings.HasPrefix(reference, prefix) {
		return "", fmt.Errorf("trezor signer: invalid signer reference")
	}
	path := strings.TrimPrefix(reference, prefix)
	if _, err := wallet.ParseDerivationPath(path); err != nil {
		return "", fmt.Errorf("trezor signer: invalid derivation path: %w", err)
	}
	return path, nil
}
