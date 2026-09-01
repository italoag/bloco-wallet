package evm

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	"blocowallet/internal/wallet"

	gethaccounts "github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// TransactionSigningIntent carries the frozen unsigned transaction and all
// approval bindings a hardware signer must verify and display.
type TransactionSigningIntent struct {
	AccountID           string
	From                common.Address
	ChainID             uint64
	Digest              [32]byte
	PlanHash            [32]byte
	ApprovalID          string
	UnsignedTransaction []byte
}

// Transaction reconstructs an isolated unsigned transaction copy.
func (intent TransactionSigningIntent) Transaction() (*types.Transaction, error) {
	if len(intent.UnsignedTransaction) == 0 {
		return nil, fmt.Errorf("structured signer: unsigned transaction missing")
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(intent.UnsignedTransaction); err != nil {
		return nil, fmt.Errorf("structured signer: unsigned transaction: %w", err)
	}
	return &transaction, nil
}

// Validate checks that the serialized transaction hashes to the approved
// digest under the bound chain.
func (intent TransactionSigningIntent) Validate() error {
	if intent.AccountID == "" || intent.From == (common.Address{}) || intent.ChainID == 0 || intent.Digest == ([32]byte{}) || intent.PlanHash == ([32]byte{}) || intent.ApprovalID == "" {
		return fmt.Errorf("structured signer: incomplete transaction intent")
	}
	transaction, err := intent.Transaction()
	if err != nil {
		return err
	}
	transactionSigner, err := signerForTransaction(transaction, new(big.Int).SetUint64(intent.ChainID))
	if err != nil {
		return err
	}
	if transactionSigner.Hash(transaction) != common.BytesToHash(intent.Digest[:]) {
		return fmt.Errorf("structured signer: transaction digest mismatch")
	}
	return nil
}

// PersonalMessageSigningIntent carries the raw message shown by hardware.
type PersonalMessageSigningIntent struct {
	AccountID  string
	Signer     common.Address
	Message    []byte
	Origin     string
	Digest     [32]byte
	IntentHash [32]byte
	ApprovalID string
}

// Validate checks the EIP-191 digest and approval bindings.
func (intent PersonalMessageSigningIntent) Validate() error {
	if intent.AccountID == "" || intent.Signer == (common.Address{}) || len(intent.Message) == 0 || intent.Origin == "" || intent.Digest == ([32]byte{}) || intent.IntentHash == ([32]byte{}) || intent.ApprovalID == "" {
		return fmt.Errorf("structured signer: incomplete personal-message intent")
	}
	if len(intent.Message) > MaxPersonalSignMessageBytes {
		return fmt.Errorf("structured signer: personal-message size")
	}
	if common.BytesToHash(gethaccounts.TextHash(intent.Message)) != common.BytesToHash(intent.Digest[:]) {
		return fmt.Errorf("structured signer: personal-message digest mismatch")
	}
	if personalSignIntentHash(intent.AccountID, intent.Signer, intent.Origin, intent.Message, intent.Digest) != intent.IntentHash {
		return fmt.Errorf("structured signer: personal-message commitment mismatch")
	}
	return nil
}

// EIP712SigningIntent carries canonical typed data plus the two EIP-712
// hashes consumed by hash-capable hardware.
type EIP712SigningIntent struct {
	AccountID           string
	Signer              common.Address
	ChainID             uint64
	Origin              string
	CanonicalJSON       []byte
	DomainSeparatorHash [32]byte
	MessageHash         [32]byte
	Digest              [32]byte
	IntentHash          [32]byte
	ApprovalID          string
}

// Validate checks the EIP-712 digest and structured payload bindings.
func (intent EIP712SigningIntent) Validate() error {
	if intent.AccountID == "" || intent.Signer == (common.Address{}) || intent.ChainID == 0 || intent.Origin == "" || len(intent.CanonicalJSON) == 0 || intent.DomainSeparatorHash == ([32]byte{}) || intent.MessageHash == ([32]byte{}) || intent.Digest == ([32]byte{}) || intent.IntentHash == ([32]byte{}) || intent.ApprovalID == "" {
		return fmt.Errorf("structured signer: incomplete EIP-712 intent")
	}
	if len(intent.CanonicalJSON) > MaxEIP712TypedDataBytes {
		return fmt.Errorf("structured signer: EIP-712 payload size")
	}
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, intent.DomainSeparatorHash[:], intent.MessageHash[:])
	if digest != common.BytesToHash(intent.Digest[:]) {
		return fmt.Errorf("structured signer: EIP-712 digest mismatch")
	}
	prepared, err := PrepareEIP712Sign(PrepareEIP712SignRequest{
		AccountID: intent.AccountID, Signer: intent.Signer, ChainID: intent.ChainID,
		TypedData: intent.CanonicalJSON, Origin: intent.Origin,
	})
	if err != nil {
		return err
	}
	preview := prepared.Preview()
	if !bytes.Equal(preview.CanonicalJSON, intent.CanonicalJSON) || preview.DomainSeparatorHash != intent.DomainSeparatorHash || preview.MessageHash != intent.MessageHash || preview.Digest != intent.Digest || preview.IntentHash != intent.IntentHash {
		return fmt.Errorf("structured signer: EIP-712 payload commitment mismatch")
	}
	return nil
}

// TransactionIntentSigner signs frozen EVM transaction intents.
type TransactionIntentSigner interface {
	SignTransaction(context.Context, wallet.CapabilityHandle, TransactionSigningIntent) (wallet.SoftwareSigningResult, error)
}

// MessageIntentSigner signs raw personal messages and canonical EIP-712 data.
type MessageIntentSigner interface {
	SignPersonalMessage(context.Context, wallet.CapabilityHandle, PersonalMessageSigningIntent) (wallet.SoftwareSigningResult, error)
	SignEIP712(context.Context, wallet.CapabilityHandle, EIP712SigningIntent) (wallet.SoftwareSigningResult, error)
}

// StructuredSigner supports every structured signing intent.
type StructuredSigner interface {
	TransactionIntentSigner
	MessageIntentSigner
}

// DigestSignerAdapter preserves software/cloud compatibility while the engine
// transitions to structured intents.
type DigestSignerAdapter struct {
	signer ApprovedDigestSigner
}

// NewDigestSignerAdapter wraps a legacy digest signer.
func NewDigestSignerAdapter(signer ApprovedDigestSigner) (*DigestSignerAdapter, error) {
	if signer == nil {
		return nil, fmt.Errorf("structured signer: digest signer required")
	}
	return &DigestSignerAdapter{signer: signer}, nil
}

// SignTransaction validates the intent before delegating its approved digest.
func (adapter *DigestSignerAdapter) SignTransaction(ctx context.Context, handle wallet.CapabilityHandle, intent TransactionSigningIntent) (wallet.SoftwareSigningResult, error) {
	if adapter == nil || adapter.signer == nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("structured signer: digest adapter unavailable")
	}
	if err := intent.Validate(); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return adapter.signer.Sign(ctx, handle, wallet.SoftwareSigningRequest{
		AccountID: intent.AccountID, Purpose: wallet.SigningPurposeTransaction,
		ChainID: intent.ChainID, Digest: intent.Digest, ApprovalID: intent.ApprovalID,
	})
}

// SignPersonalMessage validates the raw message before delegating its digest.
func (adapter *DigestSignerAdapter) SignPersonalMessage(ctx context.Context, handle wallet.CapabilityHandle, intent PersonalMessageSigningIntent) (wallet.SoftwareSigningResult, error) {
	if adapter == nil || adapter.signer == nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("structured signer: digest adapter unavailable")
	}
	if err := intent.Validate(); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return adapter.signer.Sign(ctx, handle, wallet.SoftwareSigningRequest{
		AccountID: intent.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP191Personal, Digest: intent.Digest,
		IntentHash: intent.IntentHash, ApprovalID: intent.ApprovalID,
	})
}

// SignEIP712 validates the canonical hash pair before delegating its digest.
func (adapter *DigestSignerAdapter) SignEIP712(ctx context.Context, handle wallet.CapabilityHandle, intent EIP712SigningIntent) (wallet.SoftwareSigningResult, error) {
	if adapter == nil || adapter.signer == nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("structured signer: digest adapter unavailable")
	}
	if err := intent.Validate(); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return adapter.signer.Sign(ctx, handle, wallet.SoftwareSigningRequest{
		AccountID: intent.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP712, ChainID: intent.ChainID,
		Digest: intent.Digest, IntentHash: intent.IntentHash, ApprovalID: intent.ApprovalID,
	})
}
