package storage

import (
	"context"
	"errors"
	"math"
	"regexp"
	"time"

	"blocowallet/internal/wallet"

	"gorm.io/gorm"
)

var credentialBackupUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func credentialBackupScope(ctx context.Context, db *gorm.DB, key wallet.CredentialBackupKey) *gorm.DB {
	return db.WithContext(ctx).
		Model(&wallet.CredentialBackupState{}).
		Where("target_id = ? AND vault_id = ? AND account_id = ? AND item_id = ?",
			key.TargetID, key.VaultID, key.AccountID, key.ItemID)
}

func (repo *GORMRepository) GetCredentialBackup(ctx context.Context, key wallet.CredentialBackupKey) (wallet.CredentialBackupState, error) {
	if err := key.Validate(); err != nil {
		return wallet.CredentialBackupState{}, err
	}
	var state wallet.CredentialBackupState
	result := credentialBackupScope(ctx, repo.db, key).First(&state)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return wallet.CredentialBackupState{}, wallet.ErrAccountNotFound
	}
	if result.Error != nil {
		return wallet.CredentialBackupState{}, result.Error
	}
	return state, nil
}

func (repo *GORMRepository) ListCredentialBackups(ctx context.Context, targetID, vaultID string) ([]wallet.CredentialBackupState, error) {
	var states []wallet.CredentialBackupState
	result := repo.db.WithContext(ctx).
		Where("target_id = ? AND vault_id = ?", targetID, vaultID).
		Order("account_id ASC, item_id ASC").
		Find(&states)
	return states, result.Error
}

func (repo *GORMRepository) PutCredentialBackup(ctx context.Context, state wallet.CredentialBackupState, expectedRevision uint64) error {
	if expectedRevision > math.MaxInt64-1 {
		return wallet.ErrAccountRevisionConflict
	}
	if expectedRevision == 0 {
		if state.Revision != 1 {
			return wallet.ErrAccountRevisionConflict
		}
		if err := state.Validate(); err != nil {
			return err
		}
		if err := repo.db.WithContext(ctx).Create(&state).Error; err != nil {
			return normalizeAccountError(err)
		}
		return nil
	}
	state.Revision = expectedRevision + 1
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	if err := state.Validate(); err != nil {
		return err
	}
	query := credentialBackupScope(ctx, repo.db, wallet.CredentialBackupKey{
		TargetID:  state.TargetID,
		VaultID:   state.VaultID,
		AccountID: state.AccountID,
		ItemID:    state.ItemID,
	}).Where("revision = ?", expectedRevision)
	if state.Operation == wallet.CredentialBackupOperationUpsert {
		query = query.
			Where("generation <= ?", state.Generation).
			Where("state <> ?", wallet.CredentialBackupStateDeletePending).
			Where("NOT (operation = ? AND state = ?)",
				wallet.CredentialBackupOperationDelete, wallet.CredentialBackupStateSynced)
	}
	result := query.Updates(map[string]interface{}{
		"operation_id":    state.OperationID,
		"operation":       state.Operation,
		"state":           state.State,
		"generation":      state.Generation,
		"revision":        state.Revision,
		"artifact_kind":   state.ArtifactKind,
		"artifact_name":   state.ArtifactName,
		"artifact_path":   state.ArtifactPath,
		"artifact_digest": state.ArtifactDigest,
		"updated_at":      state.UpdatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return wallet.ErrAccountRevisionConflict
	}
	return nil
}

func (repo *GORMRepository) ConfirmCredentialBackup(ctx context.Context, key wallet.CredentialBackupKey, operationID string, generation uint64) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if !credentialBackupUUIDPattern.MatchString(operationID) || generation > math.MaxInt64 {
		return wallet.ErrAccountRevisionConflict
	}
	result := credentialBackupScope(ctx, repo.db, key).
		Where("operation_id = ? AND generation = ?", operationID, generation).
		Where("(state = ? AND operation = ?) OR (state = ? AND operation = ?)",
			wallet.CredentialBackupStatePending, wallet.CredentialBackupOperationUpsert,
			wallet.CredentialBackupStateDeletePending, wallet.CredentialBackupOperationDelete).
		Where("revision < ?", math.MaxInt64).
		Updates(map[string]interface{}{
			"state":             wallet.CredentialBackupStateSynced,
			"synced_generation": generation,
			"revision":          gorm.Expr("revision + ?", 1),
			"updated_at":        time.Now().UTC(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return wallet.ErrAccountRevisionConflict
	}
	return nil
}
