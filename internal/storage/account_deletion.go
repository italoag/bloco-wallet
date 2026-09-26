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

var _ wallet.AccountDeletionRepository = (*GORMRepository)(nil)

func (repo *GORMRepository) DeleteAccount(ctx context.Context, accountID string, expectedRevision uint64, deletedAt time.Time) error {
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
