package wallet

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

type SigningPurpose string

const (
	SigningPurposeTransaction SigningPurpose = "transaction"
	SigningPurposeMessage     SigningPurpose = "message"
)

type MessageSigningScheme string

const (
	MessageSigningEIP191Personal MessageSigningScheme = "eip191_personal"
	MessageSigningEIP712         MessageSigningScheme = "eip712"
)

type SoftwareSigningRequest struct {
	AccountID     string
	Purpose       SigningPurpose
	MessageScheme MessageSigningScheme
	ChainID       uint64
	Digest        [32]byte
	IntentHash    [32]byte
	ApprovalID    string
}

type TransactionApprovalBinding struct {
	AccountID  string
	ChainID    uint64
	Digest     [32]byte
	ApprovalID string
}

type TransactionApprovalVerifier interface {
	VerifyTransactionApproval(context.Context, TransactionApprovalBinding) error
}

type MessageApprovalBinding struct {
	AccountID  string
	Scheme     MessageSigningScheme
	ChainID    uint64
	Digest     [32]byte
	IntentHash [32]byte
	ApprovalID string
}

type MessageApprovalVerifier interface {
	VerifyMessageApproval(context.Context, MessageApprovalBinding) error
}

type SoftwareSigningResult struct {
	AccountID     string
	Purpose       SigningPurpose
	MessageScheme MessageSigningScheme
	ChainID       uint64
	Digest        [32]byte
	IntentHash    [32]byte
	Signature     []byte
}

type SoftwareSigner struct {
	vault                       *WalletVault
	transactionApprovalVerifier TransactionApprovalVerifier
	messageApprovalVerifier     MessageApprovalVerifier
}

func NewSoftwareSigner(vault *WalletVault) (*SoftwareSigner, error) {
	if vault == nil {
		return nil, fmt.Errorf("wallet vault is required")
	}
	return &SoftwareSigner{vault: vault}, nil
}

func NewSoftwareSignerWithApprovalVerifier(vault *WalletVault, verifier TransactionApprovalVerifier) (*SoftwareSigner, error) {
	if verifier == nil {
		return nil, fmt.Errorf("transaction approval verifier is required")
	}
	signer, err := NewSoftwareSigner(vault)
	if err != nil {
		return nil, err
	}
	signer.transactionApprovalVerifier = verifier
	if messageVerifier, ok := verifier.(MessageApprovalVerifier); ok {
		signer.messageApprovalVerifier = messageVerifier
	}
	return signer, nil
}

func (signer *SoftwareSigner) Sign(ctx context.Context, handle CapabilityHandle, request SoftwareSigningRequest) (SoftwareSigningResult, error) {
	if request.AccountID == "" || request.AccountID != handle.AccountID() {
		return SoftwareSigningResult{}, fmt.Errorf("signing request account mismatch")
	}
	if request.ApprovalID == "" {
		return SoftwareSigningResult{}, fmt.Errorf("signing request requires bound approval")
	}
	if request.Digest == ([32]byte{}) {
		return SoftwareSigningResult{}, fmt.Errorf("signing request requires a non-zero digest")
	}
	var requiredCapability AccountCapability
	switch request.Purpose {
	case SigningPurposeTransaction:
		if request.ChainID == 0 {
			return SoftwareSigningResult{}, fmt.Errorf("transaction signing requires validated chain ID")
		}
		requiredCapability = CapabilitySignTransaction
	case SigningPurposeMessage:
		if request.IntentHash == ([32]byte{}) {
			return SoftwareSigningResult{}, fmt.Errorf("message signing requires an intent hash")
		}
		switch request.MessageScheme {
		case MessageSigningEIP191Personal:
			if request.ChainID != 0 {
				return SoftwareSigningResult{}, fmt.Errorf("EIP-191 personal signing cannot claim chain binding")
			}
		case MessageSigningEIP712:
			if request.ChainID == 0 {
				return SoftwareSigningResult{}, fmt.Errorf("EIP-712 signing requires validated chain ID")
			}
		default:
			return SoftwareSigningResult{}, fmt.Errorf("unsupported message signing scheme")
		}
		requiredCapability = CapabilitySignMessage
	default:
		return SoftwareSigningResult{}, fmt.Errorf("unsupported signing purpose")
	}
	var signature []byte
	err := signer.vault.withPrivateKey(ctx, handle, func(privateKeyBytes []byte, account *Account) error {
		if account.Capabilities&requiredCapability == 0 {
			return ErrCapabilityDenied
		}
		switch request.Purpose {
		case SigningPurposeTransaction:
			if signer.transactionApprovalVerifier == nil {
				return fmt.Errorf("transaction approval verifier is unavailable")
			}
			if err := signer.transactionApprovalVerifier.VerifyTransactionApproval(ctx, TransactionApprovalBinding{
				AccountID: request.AccountID, ChainID: request.ChainID, Digest: request.Digest, ApprovalID: request.ApprovalID,
			}); err != nil {
				return fmt.Errorf("transaction approval verification failed: %w", err)
			}
		case SigningPurposeMessage:
			if signer.messageApprovalVerifier == nil {
				return fmt.Errorf("message approval verifier is unavailable")
			}
			if err := signer.messageApprovalVerifier.VerifyMessageApproval(ctx, MessageApprovalBinding{
				AccountID: request.AccountID, Scheme: request.MessageScheme, ChainID: request.ChainID,
				Digest: request.Digest, IntentHash: request.IntentHash, ApprovalID: request.ApprovalID,
			}); err != nil {
				return fmt.Errorf("message approval verification failed: %w", err)
			}
		}
		privateKey, err := crypto.ToECDSA(privateKeyBytes)
		if err != nil {
			return fmt.Errorf("load software signing key: %w", err)
		}
		defer privateKey.D.SetInt64(0)
		signed, err := crypto.Sign(request.Digest[:], privateKey)
		if err != nil {
			return fmt.Errorf("sign approved request: %w", err)
		}
		signature = signed
		return nil
	})
	if err != nil {
		return SoftwareSigningResult{}, err
	}
	return SoftwareSigningResult{
		AccountID:     request.AccountID,
		Purpose:       request.Purpose,
		MessageScheme: request.MessageScheme,
		ChainID:       request.ChainID,
		Digest:        request.Digest,
		IntentHash:    request.IntentHash,
		Signature:     signature,
	}, nil
}
