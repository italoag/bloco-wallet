package signer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"blocowallet/internal/wallet"

	gethaccounts "github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	ErrTrezorLocked               = errors.New("trezor signer: device interaction required")
	ErrTrezorSignature            = errors.New("trezor signer: signature verification failed")
	ErrTrezorTypedHashUnsupported = errors.New("trezor signer: typed-hash signing is only available on Trezor One")
	ErrHardwareIntentRequired     = errors.New("hardware signer: structured signing intent required")
	ErrTrezorInteractionRequired  = errors.New("trezor signer: confirmation handler required")
)

type TrezorFeatures struct {
	Model                string
	Version              string
	Initialized          bool
	PinProtection        bool
	PassphraseProtection bool
}

type TrezorDevice interface {
	Initialize(ctx context.Context) (TrezorFeatures, error)
	EthereumGetPublicKey(ctx context.Context, derivationPath string) ([]byte, error)
	EthereumSignTypedHash(ctx context.Context, derivationPath string, domainSeparatorHash, messageHash [32]byte) ([]byte, error)
	EthereumSignMessage(ctx context.Context, derivationPath string, message []byte) ([]byte, error)
}

type TrezorTypedHashRequest struct {
	AccountID           string
	ChainID             uint64
	DomainSeparatorHash [32]byte
	MessageHash         [32]byte
	IntentHash          [32]byte
	ApprovalID          string
}

type TrezorPersonalMessageRequest struct {
	AccountID  string
	ChainID    uint64
	Message    []byte
	IntentHash [32]byte
	ApprovalID string
}

type TrezorSigner struct {
	device                  TrezorDevice
	accounts                AccountLookup
	messageApprovalVerifier wallet.MessageApprovalVerifier
}

func NewTrezorSigner(device TrezorDevice, accounts AccountLookup, messageVerifier wallet.MessageApprovalVerifier) (*TrezorSigner, error) {
	if device == nil || accounts == nil || messageVerifier == nil {
		return nil, fmt.Errorf("trezor signer: device, accounts, and verifier are required")
	}
	return &TrezorSigner{
		device: device, accounts: accounts,
		messageApprovalVerifier: messageVerifier,
	}, nil
}

func (signer *TrezorSigner) Sign(context.Context, wallet.CapabilityHandle, wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	return wallet.SoftwareSigningResult{}, ErrHardwareIntentRequired
}

func (signer *TrezorSigner) SignTypedHash(ctx context.Context, request TrezorTypedHashRequest) (wallet.SoftwareSigningResult, error) {
	if request.ChainID == 0 || request.ApprovalID == "" || request.IntentHash == ([32]byte{}) {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: incomplete typed-data binding")
	}
	account, derivationPath, expectedAddress, err := signer.resolveAccount(ctx, request.AccountID)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, request.DomainSeparatorHash[:], request.MessageHash[:])
	var digestArray [32]byte
	copy(digestArray[:], digest[:])
	if err := signer.messageApprovalVerifier.VerifyMessageApproval(ctx, wallet.MessageApprovalBinding{
		AccountID: account.AccountID, Scheme: wallet.MessageSigningEIP712, ChainID: request.ChainID,
		Digest: digestArray, IntentHash: request.IntentHash, ApprovalID: request.ApprovalID,
	}); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	features, err := signer.ensureReady(ctx)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if features.Model != "1" {
		return wallet.SoftwareSigningResult{}, ErrTrezorTypedHashUnsupported
	}
	signature, err := signer.device.EthereumSignTypedHash(ctx, derivationPath, request.DomainSeparatorHash, request.MessageHash)
	if err != nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: sign typed hash: %w", err)
	}
	signature, err = normalizeSignature(signature)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := verifyECDSASignature(expectedAddress, digestArray, signature); err != nil {
		return wallet.SoftwareSigningResult{}, ErrTrezorSignature
	}
	return wallet.SoftwareSigningResult{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP712, ChainID: request.ChainID,
		Digest: digestArray, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}

func (signer *TrezorSigner) SignPersonalMessage(ctx context.Context, request TrezorPersonalMessageRequest) (wallet.SoftwareSigningResult, error) {
	if len(request.Message) == 0 || len(request.Message) > 64<<10 {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: message size")
	}
	if request.ChainID != 0 || request.ApprovalID == "" || request.IntentHash == ([32]byte{}) {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: incomplete personal-sign binding")
	}
	account, derivationPath, expectedAddress, err := signer.resolveAccount(ctx, request.AccountID)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	digestBytes := gethaccounts.TextHash(request.Message)
	var digest [32]byte
	copy(digest[:], digestBytes)
	if err := signer.messageApprovalVerifier.VerifyMessageApproval(ctx, wallet.MessageApprovalBinding{
		AccountID: account.AccountID, Scheme: wallet.MessageSigningEIP191Personal, ChainID: request.ChainID,
		Digest: digest, IntentHash: request.IntentHash, ApprovalID: request.ApprovalID,
	}); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if _, err := signer.ensureReady(ctx); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	signature, err := signer.device.EthereumSignMessage(ctx, derivationPath, request.Message)
	if err != nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("trezor signer: sign personal message: %w", err)
	}
	signature, err = normalizeSignature(signature)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := verifyECDSASignature(expectedAddress, digest, signature); err != nil {
		return wallet.SoftwareSigningResult{}, ErrTrezorSignature
	}
	return wallet.SoftwareSigningResult{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP191Personal, ChainID: request.ChainID,
		Digest: digest, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}

func (signer *TrezorSigner) resolveAccount(ctx context.Context, accountID string) (*wallet.Account, string, common.Address, error) {
	if signer == nil {
		return nil, "", common.Address{}, fmt.Errorf("trezor signer: nil signer")
	}
	account, err := signer.accounts.GetAccount(ctx, accountID)
	if err != nil {
		return nil, "", common.Address{}, fmt.Errorf("trezor signer: account: %w", err)
	}
	if account == nil || account.AccountID != accountID || account.State != wallet.AccountStateActive {
		return nil, "", common.Address{}, fmt.Errorf("trezor signer: account binding mismatch or inactive state")
	}
	if account.SignerKind != wallet.SignerKindHardware {
		return nil, "", common.Address{}, fmt.Errorf("trezor signer: account is not a hardware account")
	}
	derivationPath, err := trezorDerivationPath(account.SignerReference)
	if err != nil {
		return nil, "", common.Address{}, err
	}
	expectedAddress := common.HexToAddress(account.Address)
	if expectedAddress == (common.Address{}) {
		return nil, "", common.Address{}, fmt.Errorf("trezor signer: invalid account address")
	}
	return account, derivationPath, expectedAddress, nil
}

func (signer *TrezorSigner) ensureReady(ctx context.Context) (TrezorFeatures, error) {
	features, err := signer.device.Initialize(ctx)
	if err != nil {
		return TrezorFeatures{}, fmt.Errorf("trezor signer: initialize: %w", err)
	}
	if !features.Initialized {
		return TrezorFeatures{}, fmt.Errorf("trezor signer: device not initialized")
	}
	return features, nil
}

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
