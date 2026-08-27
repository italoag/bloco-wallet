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

type SoftwareSigningRequest struct {
	AccountID  string
	Purpose    SigningPurpose
	ChainID    uint64
	Digest     [32]byte
	ApprovalID string
}

type SoftwareSigningResult struct {
	AccountID string
	Purpose   SigningPurpose
	ChainID   uint64
	Digest    [32]byte
	Signature []byte
}

type SoftwareSigner struct {
	vault *WalletVault
}

func NewSoftwareSigner(vault *WalletVault) (*SoftwareSigner, error) {
	if vault == nil {
		return nil, fmt.Errorf("wallet vault is required")
	}
	return &SoftwareSigner{vault: vault}, nil
}

func (signer *SoftwareSigner) Sign(ctx context.Context, handle CapabilityHandle, request SoftwareSigningRequest) (SoftwareSigningResult, error) {
	if request.AccountID == "" || request.AccountID != handle.AccountID() {
		return SoftwareSigningResult{}, fmt.Errorf("signing request account mismatch")
	}
	if request.ApprovalID == "" {
		return SoftwareSigningResult{}, fmt.Errorf("signing request requires bound approval")
	}
	var requiredCapability AccountCapability
	switch request.Purpose {
	case SigningPurposeTransaction:
		if request.ChainID == 0 {
			return SoftwareSigningResult{}, fmt.Errorf("transaction signing requires validated chain ID")
		}
		requiredCapability = CapabilitySignTransaction
	case SigningPurposeMessage:
		requiredCapability = CapabilitySignMessage
	default:
		return SoftwareSigningResult{}, fmt.Errorf("unsupported signing purpose")
	}
	var signature []byte
	err := signer.vault.withPrivateKey(ctx, handle, func(privateKeyBytes []byte, account *Account) error {
		if account.Capabilities&requiredCapability == 0 {
			return ErrCapabilityDenied
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
		AccountID: request.AccountID,
		Purpose:   request.Purpose,
		ChainID:   request.ChainID,
		Digest:    request.Digest,
		Signature: signature,
	}, nil
}
