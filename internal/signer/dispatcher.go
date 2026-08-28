package signer

import (
	"context"
	"fmt"

	"blocowallet/internal/wallet"
)

// SignerDispatcher routes signing intents to the adapter matching the
// account signer kind.
type SignerDispatcher struct {
	software ApprovedDigestSigner
	cloud    ApprovedDigestSigner
	accounts AccountLookup
}

// ApprovedDigestSigner mirrors the evm signer contract.
type ApprovedDigestSigner interface {
	Sign(context.Context, wallet.CapabilityHandle, wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error)
}

// NewSignerDispatcher builds the dispatcher. At least the software signer
// must be provided.
func NewSignerDispatcher(software ApprovedDigestSigner, cloud ApprovedDigestSigner, accounts AccountLookup) (*SignerDispatcher, error) {
	if software == nil || accounts == nil {
		return nil, fmt.Errorf("signer dispatcher: software signer and accounts are required")
	}
	return &SignerDispatcher{software: software, cloud: cloud, accounts: accounts}, nil
}

// Sign implements the signer contract by dispatching on the account kind.
func (dispatcher *SignerDispatcher) Sign(ctx context.Context, handle wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	if dispatcher == nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("signer dispatcher: nil")
	}
	account, err := dispatcher.accounts.GetAccount(ctx, request.AccountID)
	if err != nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("signer dispatcher: account: %w", err)
	}
	switch account.SignerKind {
	case wallet.SignerKindSoftware:
		return dispatcher.software.Sign(ctx, handle, request)
	case wallet.SignerKindCloud:
		if dispatcher.cloud == nil {
			return wallet.SoftwareSigningResult{}, fmt.Errorf("signer dispatcher: cloud signer not configured")
		}
		return dispatcher.cloud.Sign(ctx, handle, request)
	default:
		return wallet.SoftwareSigningResult{}, fmt.Errorf("signer dispatcher: unsupported signer kind %q", account.SignerKind)
	}
}
