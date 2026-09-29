package wallet

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

const (
	CredentialBackupStatePending       = "pending"
	CredentialBackupStateSynced        = "synced"
	CredentialBackupStateDeletePending = "delete_pending"
	CredentialBackupStatePrepared      = "prepared"

	CredentialBackupOperationUpsert = "upsert"
	CredentialBackupOperationDelete = "delete"

	credentialVaultIDMetadataKey = "credential_backup_vault_id"
)

var credentialBackupItemIDPattern = regexp.MustCompile(`^(account|file:(keystore_v3|bloco_encrypted):[0-9a-f]{64})$`)

type CredentialBackupState struct {
	TargetID         string `gorm:"primaryKey;size:36"`
	VaultID          string `gorm:"primaryKey;size:36"`
	AccountID        string `gorm:"primaryKey;size:36"`
	ItemID           string `gorm:"primaryKey;size:160"`
	OperationID      string `gorm:"size:36;not null"`
	Operation        string `gorm:"size:16;not null"`
	State            string `gorm:"size:24;not null"`
	Generation       uint64 `gorm:"not null"`
	SyncedGeneration uint64 `gorm:"not null;default:0"`
	Revision         uint64 `gorm:"not null;default:1"`
	ArtifactKind     string
	ArtifactName     string
	ArtifactPath     string
	ArtifactDigest   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (CredentialBackupState) TableName() string {
	return "account_credential_backups"
}

type CredentialBackupKey struct {
	TargetID  string
	VaultID   string
	AccountID string
	ItemID    string
}

type CredentialBackupRepository interface {
	GetCredentialBackup(context.Context, CredentialBackupKey) (CredentialBackupState, error)
	ListCredentialBackups(context.Context, string, string) ([]CredentialBackupState, error)
	PutCredentialBackup(context.Context, CredentialBackupState, uint64) error
	ConfirmCredentialBackup(context.Context, CredentialBackupKey, string, uint64) error
}

func (key CredentialBackupKey) Validate() error {
	if !accountUUIDPattern.MatchString(key.TargetID) ||
		!accountUUIDPattern.MatchString(key.VaultID) ||
		!accountUUIDPattern.MatchString(key.AccountID) {
		return fmt.Errorf("credential backup identifiers must be canonical UUIDv4")
	}
	if !credentialBackupItemIDPattern.MatchString(key.ItemID) {
		return fmt.Errorf("credential backup item identifier is outside policy")
	}
	return nil
}

func (state *CredentialBackupState) Validate() error {
	if state == nil {
		return fmt.Errorf("credential backup state is required")
	}
	key := CredentialBackupKey{
		TargetID:  state.TargetID,
		VaultID:   state.VaultID,
		AccountID: state.AccountID,
		ItemID:    state.ItemID,
	}
	if err := key.Validate(); err != nil {
		return err
	}
	if !accountUUIDPattern.MatchString(state.OperationID) {
		return fmt.Errorf("credential backup operation identifier must be a canonical UUIDv4")
	}
	switch state.Operation {
	case CredentialBackupOperationUpsert, CredentialBackupOperationDelete:
	default:
		return fmt.Errorf("credential backup operation is outside policy")
	}
	switch state.State {
	case CredentialBackupStatePending, CredentialBackupStatePrepared:
		if state.Operation != CredentialBackupOperationUpsert {
			return fmt.Errorf("credential backup pending state requires an upsert operation")
		}
	case CredentialBackupStateDeletePending:
		if state.Operation != CredentialBackupOperationDelete {
			return fmt.Errorf("credential backup delete_pending state requires a delete operation")
		}
	case CredentialBackupStateSynced:
	default:
		return fmt.Errorf("credential backup state is outside policy")
	}
	if state.State == CredentialBackupStatePrepared && state.ItemID == "account" {
		return fmt.Errorf("credential backup prepared state requires a file item")
	}
	if state.SyncedGeneration > state.Generation {
		return fmt.Errorf("credential backup synced generation cannot exceed generation")
	}
	if state.State == CredentialBackupStateSynced && state.SyncedGeneration != state.Generation {
		return fmt.Errorf("credential backup synced rows must carry their applied generation")
	}
	if state.ItemID == "account" && state.Generation == 0 {
		return fmt.Errorf("credential backup accounts require a positive generation")
	}
	if state.Generation > math.MaxInt64 || state.Revision > math.MaxInt64 || state.Revision == 0 {
		return fmt.Errorf("credential backup counters exceed policy bounds")
	}
	if len(state.ArtifactKind) > 32 || len(state.ArtifactName) > 255 || len(state.ArtifactPath) > 1024 ||
		strings.ContainsAny(state.ArtifactKind+state.ArtifactName+state.ArtifactPath+state.ArtifactDigest, "\x00") {
		return fmt.Errorf("credential backup artifact metadata is outside policy")
	}
	if state.ItemID == "account" {
		if state.ArtifactKind != "" || state.ArtifactName != "" || state.ArtifactPath != "" || state.ArtifactDigest != "" {
			return fmt.Errorf("credential backup accounts cannot carry artifact metadata")
		}
		return nil
	}
	if state.ArtifactKind != "keystore_v3" && state.ArtifactKind != "bloco_encrypted" {
		return fmt.Errorf("credential backup artifact kind is outside policy")
	}
	matched, _ := regexp.MatchString(`^[0-9a-f]{64}$`, state.ArtifactDigest)
	if !matched {
		return fmt.Errorf("credential backup artifact digest must be a lowercase SHA-256 hex")
	}
	if state.ArtifactName == "" {
		return fmt.Errorf("credential backup artifacts require a file name")
	}
	if state.ItemID != "file:"+state.ArtifactKind+":"+state.ArtifactDigest {
		return fmt.Errorf("credential backup item identifier must match the artifact identity")
	}
	return nil
}

var accountUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func (vault *WalletVault) CredentialVaultID(ctx context.Context) (string, error) {
	if err := vault.beginOperation(); err != nil {
		return "", err
	}
	defer vault.endOperation()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	stored, err := vault.repository.GetVaultMetadata(ctx, credentialVaultIDMetadataKey)
	if err == nil {
		if !accountUUIDPattern.MatchString(stored) {
			return "", fmt.Errorf("stored credential vault identifier is invalid")
		}
		return stored, nil
	}
	if !errors.Is(err, ErrAccountNotFound) {
		return "", err
	}
	identifier, err := newUUID(vault.options.Random)
	if err != nil {
		return "", err
	}
	if err := vault.repository.PutVaultMetadata(ctx, credentialVaultIDMetadataKey, identifier); err != nil {
		if !errors.Is(err, ErrAccountConflict) {
			return "", err
		}
		stored, err = vault.repository.GetVaultMetadata(ctx, credentialVaultIDMetadataKey)
		if err != nil {
			return "", err
		}
		if !accountUUIDPattern.MatchString(stored) {
			return "", fmt.Errorf("stored credential vault identifier is invalid")
		}
		return stored, nil
	}
	return identifier, nil
}
