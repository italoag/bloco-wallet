package storage

import (
	"context"
	"math"
	"time"

	"blocowallet/internal/safe"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

var (
	_ wallet.AccountDeletionRepository           = (*GORMRepository)(nil)
	_ wallet.AccountCredentialDeletionRepository = (*GORMRepository)(nil)
)

func (repo *GORMRepository) DeleteAccount(ctx context.Context, accountID string, expectedRevision uint64, deletedAt time.Time) error {
	return repo.deleteAccount(ctx, accountID, expectedRevision, deletedAt, nil)
}

func (repo *GORMRepository) DeleteAccountWithCredentialBackup(ctx context.Context, accountID string, expectedRevision uint64, deletedAt time.Time, intent wallet.CredentialBackupDeleteIntent) error {
	return repo.deleteAccount(ctx, accountID, expectedRevision, deletedAt, &intent)
}

func (repo *GORMRepository) deleteAccount(ctx context.Context, accountID string, expectedRevision uint64, deletedAt time.Time, intent *wallet.CredentialBackupDeleteIntent) error {
	return repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		transactionRepo := &GORMRepository{db: tx}
		account, err := transactionRepo.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if account.Revision != expectedRevision || account.State == wallet.AccountStateTombstoned {
			return wallet.ErrAccountRevisionConflict
		}
		if account.AuthorizationEpoch >= math.MaxInt64 || account.Revision >= math.MaxInt64 || account.BackupGeneration >= math.MaxInt64 {
			return wallet.ErrAccountRevisionConflict
		}
		nowMS := deletedAt.UnixMilli()
		guards := []func() (int64, error){
			func() (int64, error) {
				var count int64
				err := tx.Model(&evmTransactionRow{}).Where("account_id = ? AND state NOT IN ?", accountID, []string{"confirmed", "reverted", "signing_failed"}).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&messageSigningRow{}).Where("account_id = ? AND state = ?", accountID, "signing").Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&evmNonceReservationRow{}).Where("account_id = ? AND (state = ? OR (state = ? AND expires_at_ms > ?))", accountID, "committed", "reserved", nowMS).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&evmApprovalRow{}).Where("account_id = ? AND state = ? AND expires_at_ms > ? AND authorization_epoch = ?", accountID, "pending", nowMS, account.AuthorizationEpoch).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&messageApprovalRow{}).Where("account_id = ? AND state = ? AND expires_at_ms > ? AND authorization_epoch = ?", accountID, "pending", nowMS, account.AuthorizationEpoch).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&safeProposalRow{}).Where("account_id = ? AND status = ?", accountID, "pending").Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&wallet.Account{}).Where("related_account_id = ? AND state <> ?", accountID, wallet.AccountStateTombstoned).Count(&count).Error
				return count, err
			},
		}
		for _, guard := range guards {
			count, err := guard()
			if err != nil {
				return err
			}
			if count > 0 {
				return wallet.ErrAccountDeletionPending
			}
		}
		if account.SignerKind.SupportsEOASigning() {
			ownerAddress := common.HexToAddress(account.Address)
			var encodings [][]byte
			if err := tx.Model(&safeProposalRow{}).Where("status = ? AND account_id <> ?", "pending", accountID).Select("encoding").Find(&encodings).Error; err != nil {
				return err
			}
			for _, encoded := range encodings {
				proposal, err := safe.DecodeProposal(encoded)
				if err != nil {
					return err
				}
				for _, owner := range proposal.Owners {
					if owner.Address == ownerAddress {
						return wallet.ErrAccountDeletionPending
					}
				}
			}
		}
		if intent != nil {
			if err := markCredentialBackupDeletion(ctx, tx, *intent, account); err != nil {
				return err
			}
		}
		hasHistory := false
		historyChecks := []func() (int64, error){
			func() (int64, error) {
				var count int64
				err := tx.Model(&evmNonceReservationRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&evmApprovalRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&evmTransactionRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&messageApprovalRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&messageSigningRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&safeProposalRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&wcSessionRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&fido2ChallengeRow{}).Where("account_id = ?", accountID).Count(&count).Error
				return count, err
			},
			func() (int64, error) {
				var count int64
				err := tx.Model(&wallet.Account{}).Where("related_account_id = ?", accountID).Count(&count).Error
				return count, err
			},
		}
		for _, check := range historyChecks {
			count, err := check()
			if err != nil {
				return err
			}
			if count > 0 {
				hasHistory = true
				break
			}
		}
		if !hasHistory {
			result := tx.Where("account_id = ? AND revision = ?", accountID, expectedRevision).Delete(&wallet.Account{})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return wallet.ErrAccountRevisionConflict
			}
			return ctx.Err()
		}
		account.State = wallet.AccountStateTombstoned
		clear(account.SecretEnvelope)
		account.SecretEnvelope = nil
		account.Capabilities = 0
		account.AuthorizationEpoch++
		account.BackupGeneration++
		account.UpdatedAt = deletedAt
		if err := transactionRepo.UpdateAccount(ctx, account); err != nil {
			return err
		}
		if err := tx.Model(&wcSessionRow{}).Where("account_id = ? AND revoked = ?", accountID, false).Updates(map[string]any{"revoked": true, "last_used_at_ms": nowMS}).Error; err != nil {
			return err
		}
		if err := tx.Model(&fido2ChallengeRow{}).Where("account_id = ? AND used = ?", accountID, false).Update("used", true).Error; err != nil {
			return err
		}
		return ctx.Err()
	})
}

func markCredentialBackupDeletion(ctx context.Context, tx *gorm.DB, intent wallet.CredentialBackupDeleteIntent, account *wallet.Account) error {
	if !credentialBackupUUIDPattern.MatchString(intent.TargetID) ||
		!credentialBackupUUIDPattern.MatchString(intent.VaultID) ||
		!credentialBackupUUIDPattern.MatchString(intent.OperationID) {
		return wallet.ErrCredentialBackupConflict
	}
	transactionRepo := &GORMRepository{db: tx}
	rows, err := transactionRepo.ListCredentialBackups(ctx, intent.TargetID, intent.VaultID)
	if err != nil {
		return err
	}
	hasAccountRow := false
	for _, row := range rows {
		if row.AccountID != account.AccountID {
			continue
		}
		updated := row
		updated.State = wallet.CredentialBackupStateDeletePending
		updated.Operation = wallet.CredentialBackupOperationDelete
		updated.OperationID = intent.OperationID
		updated.UpdatedAt = time.Now().UTC()
		if row.ItemID == "account" {
			hasAccountRow = true
			updated.Generation = account.EnvelopeGeneration
		}
		if err := transactionRepo.PutCredentialBackup(ctx, updated, row.Revision); err != nil {
			return err
		}
	}
	if hasAccountRow {
		return nil
	}
	return transactionRepo.PutCredentialBackup(ctx, wallet.CredentialBackupState{
		TargetID:    intent.TargetID,
		VaultID:     intent.VaultID,
		AccountID:   account.AccountID,
		ItemID:      "account",
		OperationID: intent.OperationID,
		Operation:   wallet.CredentialBackupOperationDelete,
		State:       wallet.CredentialBackupStateDeletePending,
		Generation:  account.EnvelopeGeneration,
		Revision:    1,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}, 0)
}
