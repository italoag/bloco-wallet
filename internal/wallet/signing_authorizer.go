package wallet

import (
	"context"
	"fmt"
)

type signingAccountLookup interface {
	GetAccount(context.Context, string) (*Account, error)
}

// SigningAuthorizer delegates software accounts to password authorization and
// authorizes active external EOA signers from their persisted epoch.
type SigningAuthorizer struct {
	software *TransactionAuthorizer
	accounts signingAccountLookup
}

// NewSigningAuthorizer creates a signer-kind-aware authorizer.
func NewSigningAuthorizer(software *TransactionAuthorizer, accounts signingAccountLookup) (*SigningAuthorizer, error) {
	if software == nil || accounts == nil {
		return nil, fmt.Errorf("signing authorizer dependencies are required")
	}
	return &SigningAuthorizer{software: software, accounts: accounts}, nil
}

// HasActiveSession reports software sessions and treats available external
// signers as not requiring a storage password.
func (authorizer *SigningAuthorizer) HasActiveSession(ctx context.Context, accountID string) bool {
	if authorizer == nil || accountID == "" {
		return false
	}
	account, err := authorizer.accounts.GetAccount(ctx, accountID)
	if err != nil || account == nil || account.AccountID != accountID {
		return false
	}
	if account.Capabilities&(CapabilitySignTransaction|CapabilitySignMessage) == 0 {
		return false
	}
	if account.SignerKind == SignerKindSoftware {
		return authorizer.software.HasActiveSession(ctx, accountID)
	}
	return account.SignerKind.SupportsEOASigning() && account.State == AccountStateActive && account.AuthorizationEpoch != 0
}

// Authorize supplies a local capability for software or an empty capability
// plus repository-controlled epoch for cloud/hardware EOA signers.
func (authorizer *SigningAuthorizer) Authorize(ctx context.Context, accountID string, password []byte, operation TransactionAuthorizationOperation) error {
	if authorizer == nil || operation == nil || accountID == "" {
		return fmt.Errorf("signing authorization request is invalid")
	}
	account, err := authorizer.accounts.GetAccount(ctx, accountID)
	if err != nil {
		return fmt.Errorf("signing authorization account: %w", err)
	}
	if account == nil || account.AccountID != accountID || !account.SignerKind.SupportsEOASigning() || account.AuthorizationEpoch == 0 || account.Capabilities&(CapabilitySignTransaction|CapabilitySignMessage) == 0 {
		return fmt.Errorf("signing authorization account binding mismatch")
	}
	if account.SignerKind == SignerKindSoftware {
		return authorizer.software.Authorize(ctx, accountID, password, operation)
	}
	if account.State != AccountStateActive {
		return fmt.Errorf("external signer account is unavailable")
	}
	if len(password) != 0 {
		return fmt.Errorf("storage password is not accepted for external signers")
	}
	return operation(CapabilityHandle{}, account.AuthorizationEpoch)
}

// Close releases software authorization sessions.
func (authorizer *SigningAuthorizer) Close() {
	if authorizer != nil && authorizer.software != nil {
		authorizer.software.Close()
	}
}
