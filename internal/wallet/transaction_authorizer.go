package wallet

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type TransactionAuthorizationMode string

const (
	TransactionAuthorizationPerTransaction   TransactionAuthorizationMode = "password_per_transaction"
	TransactionAuthorizationTemporarySession TransactionAuthorizationMode = "temporary_session"
)

type TransactionAuthorizationOperation func(CapabilityHandle, uint64) error

type TransactionAuthorizer struct {
	vault   *WalletVault
	mode    TransactionAuthorizationMode
	mu      sync.Mutex
	handles map[string]CapabilityHandle
	closed  bool
}

func NewTransactionAuthorizer(vault *WalletVault, mode TransactionAuthorizationMode) (*TransactionAuthorizer, error) {
	if vault == nil {
		return nil, fmt.Errorf("wallet vault is required")
	}
	if mode != TransactionAuthorizationPerTransaction && mode != TransactionAuthorizationTemporarySession {
		return nil, fmt.Errorf("unsupported transaction authorization mode")
	}
	return &TransactionAuthorizer{vault: vault, mode: mode, handles: make(map[string]CapabilityHandle)}, nil
}

func (authorizer *TransactionAuthorizer) HasActiveSession(_ context.Context, accountID string) bool {
	if authorizer == nil || accountID == "" {
		return false
	}
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	if authorizer.closed || authorizer.mode != TransactionAuthorizationTemporarySession {
		return false
	}
	_, exists := authorizer.handles[accountID]
	return exists
}

func (authorizer *TransactionAuthorizer) Authorize(ctx context.Context, accountID string, password []byte, operation TransactionAuthorizationOperation) error {
	if authorizer == nil || operation == nil || accountID == "" {
		return fmt.Errorf("transaction authorization request is invalid")
	}
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	if authorizer.closed {
		return fmt.Errorf("transaction authorizer is closed")
	}
	if authorizer.mode == TransactionAuthorizationTemporarySession {
		if handle, exists := authorizer.handles[accountID]; exists {
			epoch, err := authorizer.vault.AuthorizationEpoch(ctx, handle)
			if err == nil {
				return operation(handle, epoch)
			}
			delete(authorizer.handles, accountID)
			_ = authorizer.vault.Lock(handle)
		}
	}
	if len(password) == 0 {
		return fmt.Errorf("storage password is required")
	}
	handle, err := authorizer.vault.Unlock(ctx, accountID, password)
	if err != nil {
		return err
	}
	epoch, err := authorizer.vault.AuthorizationEpoch(ctx, handle)
	if err != nil {
		_ = authorizer.vault.Lock(handle)
		return err
	}
	if authorizer.mode == TransactionAuthorizationTemporarySession {
		authorizer.handles[accountID] = handle
		return operation(handle, epoch)
	}
	operationErr := operation(handle, epoch)
	lockErr := authorizer.vault.Lock(handle)
	return errors.Join(operationErr, lockErr)
}

func (authorizer *TransactionAuthorizer) Close() {
	if authorizer == nil {
		return
	}
	authorizer.mu.Lock()
	defer authorizer.mu.Unlock()
	if authorizer.closed {
		return
	}
	authorizer.closed = true
	for accountID, handle := range authorizer.handles {
		_ = authorizer.vault.Lock(handle)
		delete(authorizer.handles, accountID)
	}
}
