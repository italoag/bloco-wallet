package wallet

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrAccountDeleteConfirmation  = errors.New("account deletion confirmation does not match")
	ErrAccountDeletionPending     = errors.New("account has pending operations or active dependents")
	ErrAccountDeletionUnsupported = errors.New("account repository does not support protected deletion")
	ErrAccountDeleted             = errors.New("account source belongs to a deleted record retained for history")
)

type DeleteAccountRequest struct {
	AccountID              string
	ConfirmAccountID       string
	Password               []byte
	RemoveCredentialBackup bool
	ConfirmBackupAccountID string
}

type AccountDeletionRepository interface {
	DeleteAccount(ctx context.Context, accountID string, expectedRevision uint64, deletedAt time.Time) error
}

func (vault *WalletVault) DeleteAccount(ctx context.Context, request DeleteAccountRequest) error {
	var credentialOp *CredentialBackupOperation
	if request.RemoveCredentialBackup {
		if request.AccountID == "" || request.ConfirmBackupAccountID != request.AccountID {
			return ErrAccountDeleteConfirmation
		}
		op, done, err := vault.beginCredentialMutation(ctx)
		if err != nil {
			return err
		}
		defer done()
		if op == nil {
			return ErrCredentialBackupRequired
		}
		credentialOp = op
	}
	vault.lifecycle.Lock()
	defer vault.lifecycle.Unlock()
	if vault.closed {
		return ErrVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.AccountID == "" || request.AccountID != request.ConfirmAccountID {
		return ErrAccountDeleteConfirmation
	}
	deleter, ok := vault.repository.(AccountDeletionRepository)
	if !ok {
		return ErrAccountDeletionUnsupported
	}
	account, err := vault.repository.GetAccount(ctx, request.AccountID)
	if err != nil {
		return err
	}
	switch account.State {
	case AccountStatePendingBackup, AccountStateActive, AccountStateLocked, AccountStateUnavailable:
	default:
		return fmt.Errorf("account state does not allow deletion")
	}
	if err := account.Validate(); err != nil {
		return fmt.Errorf("account record is invalid")
	}
	if account.SignerKind == SignerKindSoftware {
		if len(request.Password) == 0 {
			return fmt.Errorf("vault password is required for software accounts")
		}
		plaintext, err := vault.codec.Open(request.Password, metadataForAccount(account), account.SecretEnvelope)
		if err != nil {
			return err
		}
		defer clear(plaintext)
		verificationKey, verificationAddress, err := deriveStoredSecretIdentity(account, plaintext)
		if err != nil {
			return err
		}
		clear(verificationKey)
		if !addressesEqual(verificationAddress, account.Address) {
			return fmt.Errorf("storage password is incorrect")
		}
	} else if len(request.Password) != 0 {
		return fmt.Errorf("external and watch-only accounts do not use a vault password")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if credentialOp != nil {
		deleterWithBackup, ok := vault.repository.(AccountCredentialDeletionRepository)
		if !ok {
			return ErrAccountDeletionUnsupported
		}
		intent := CredentialBackupDeleteIntent{
			TargetID:    credentialOp.binding.TargetID,
			VaultID:     credentialOp.binding.VaultID,
			OperationID: credentialOp.id,
		}
		if err := deleterWithBackup.DeleteAccountWithCredentialBackup(ctx, account.AccountID, account.Revision, vault.options.Now().UTC(), intent); err != nil {
			return err
		}
		credentialOp.queueDeletion(account.AccountID)
	} else if err := deleter.DeleteAccount(ctx, account.AccountID, account.Revision, vault.options.Now().UTC()); err != nil {
		return err
	}
	vault.mu.Lock()
	defer vault.mu.Unlock()
	for token, session := range vault.sessions {
		if session.accountID == account.AccountID {
			vault.deleteSessionLocked(token, session)
		}
	}
	for challengeID, challenge := range vault.challenges {
		if challenge.accountID == account.AccountID {
			clearWords(challenge.words)
			clear(challenge.passphraseMAC)
			delete(vault.challenges, challengeID)
		}
	}
	return nil
}
