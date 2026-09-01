// Package signer implements external and multisig signing adapters:
// a Vault-compatible cloud signer and Safe/EIP-1271 message composition.
// Contract tests drive the adapters with local mocks; no adapter is treated
// as live-verified until a real device or provider sandbox passes.
package signer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	ErrCloudSigningDenied = errors.New("cloud signer: signing denied")
	ErrCloudSignature     = errors.New("cloud signer: signature verification failed")
)

// RemoteSigningRequest is the intent sent to a remote signing service.
type RemoteSigningRequest struct {
	AccountID string
	ChainID   uint64
	Digest    [32]byte
}

// RemoteSigningResult is the response from a remote signing service.
type RemoteSigningResult struct {
	Signature []byte
}

// RemoteSigningAPI signs an approved digest remotely.
type RemoteSigningAPI interface {
	Sign(ctx context.Context, request RemoteSigningRequest) (RemoteSigningResult, error)
}

// CloudSigner signs approved digests through a remote signing API. It never
// holds the private key and verifies every returned signature against the
// account address before exposing it.
type CloudSigner struct {
	api                         RemoteSigningAPI
	accounts                    AccountLookup
	transactionApprovalVerifier wallet.TransactionApprovalVerifier
	messageApprovalVerifier     wallet.MessageApprovalVerifier
}

// AccountLookup resolves account metadata for signature verification.
type AccountLookup interface {
	GetAccount(context.Context, string) (*wallet.Account, error)
}

// NewCloudSigner creates a cloud signer.
func NewCloudSigner(api RemoteSigningAPI, accounts AccountLookup, transactionVerifier wallet.TransactionApprovalVerifier, messageVerifier wallet.MessageApprovalVerifier) (*CloudSigner, error) {
	if api == nil || accounts == nil || transactionVerifier == nil || messageVerifier == nil {
		return nil, fmt.Errorf("cloud signer: api, accounts, and verifiers are required")
	}
	return &CloudSigner{
		api: api, accounts: accounts,
		transactionApprovalVerifier: transactionVerifier,
		messageApprovalVerifier:     messageVerifier,
	}, nil
}

// Sign implements ApprovedDigestSigner with the same approval gates as the
// software signer.
func (signer *CloudSigner) Sign(ctx context.Context, handle wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	if signer == nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("cloud signer: nil signer")
	}
	account, err := signer.accounts.GetAccount(ctx, request.AccountID)
	if err != nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("cloud signer: account: %w", err)
	}
	if account.SignerKind != wallet.SignerKindCloud {
		return wallet.SoftwareSigningResult{}, ErrCloudSigningDenied
	}
	expectedAddress := common.HexToAddress(account.Address)
	if expectedAddress == (common.Address{}) {
		return wallet.SoftwareSigningResult{}, ErrCloudSigningDenied
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
		return wallet.SoftwareSigningResult{}, fmt.Errorf("cloud signer: unsupported purpose")
	}
	result, err := signer.api.Sign(ctx, RemoteSigningRequest{
		AccountID: request.AccountID, ChainID: request.ChainID, Digest: request.Digest,
	})
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := verifyECDSASignature(expectedAddress, request.Digest, result.Signature); err != nil {
		return wallet.SoftwareSigningResult{}, ErrCloudSignature
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: request.Purpose, MessageScheme: request.MessageScheme,
		ChainID: request.ChainID, Digest: request.Digest, IntentHash: request.IntentHash,
		Signature: append([]byte(nil), result.Signature...),
	}, nil
}

// verifyECDSASignature recovers the signer from an Ethereum signature and
// compares it with the expected address.
func verifyECDSASignature(expected common.Address, digest [32]byte, signature []byte) error {
	if len(signature) != 65 {
		return fmt.Errorf("cloud signer: signature size")
	}
	if signature[64] != 0 && signature[64] != 1 {
		return fmt.Errorf("cloud signer: signature recovery id")
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	if !crypto.ValidateSignatureValues(signature[64], r, s, true) {
		return fmt.Errorf("cloud signer: invalid or malleable signature values")
	}
	pubKey, err := crypto.SigToPub(digest[:], signature)
	if err != nil {
		return fmt.Errorf("cloud signer: recovery: %w", err)
	}
	if crypto.PubkeyToAddress(*pubKey) != expected {
		return ErrCloudSignature
	}
	return nil
}

// VaultCompatibleAPI signs digests through a HashiCorp Vault-like endpoint:
// POST {endpoint}/sign with the digest and account, Bearer token auth.
type VaultCompatibleAPI struct {
	Endpoint      string
	TokenProvider func() (string, error)
	Client        *http.Client
}

// NewVaultCompatibleAPI creates the HTTP adapter.
func NewVaultCompatibleAPI(endpoint string, tokenProvider func() (string, error)) (*VaultCompatibleAPI, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("cloud signer: endpoint required")
	}
	if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
		return nil, fmt.Errorf("cloud signer: endpoint must be http(s)")
	}
	if tokenProvider == nil {
		return nil, fmt.Errorf("cloud signer: token provider required")
	}
	return &VaultCompatibleAPI{
		Endpoint:      strings.TrimSuffix(endpoint, "/"),
		TokenProvider: tokenProvider,
		Client:        &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Sign performs the remote signing round trip.
func (api *VaultCompatibleAPI) Sign(ctx context.Context, request RemoteSigningRequest) (RemoteSigningResult, error) {
	if api == nil || api.Client == nil {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: api not configured")
	}
	token, err := api.TokenProvider()
	if err != nil || token == "" {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: token unavailable")
	}
	payload, err := json.Marshal(map[string]any{
		"account_id": request.AccountID,
		"chain_id":   request.ChainID,
		"digest":     "0x" + hex.EncodeToString(request.Digest[:]),
	})
	if err != nil {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: encode request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, api.Endpoint+"/sign", bytes.NewReader(payload))
	if err != nil {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: build request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := api.Client.Do(httpRequest)
	if err != nil {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: transport: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: read response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
			return RemoteSigningResult{}, ErrCloudSigningDenied
		}
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: status %d", response.StatusCode)
	}
	var decoded struct {
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: decode response: %w", err)
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(decoded.Signature), "0x"))
	if err != nil || len(signature) != 65 {
		return RemoteSigningResult{}, fmt.Errorf("cloud signer: malformed signature")
	}
	return RemoteSigningResult{Signature: signature}, nil
}

var _ = ecdsa.PublicKey{}
