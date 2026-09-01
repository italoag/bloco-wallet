package storage

import (
	"context"
	"fmt"
	"math"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

type messageApprovalRow struct {
	ApprovalID         string `gorm:"column:approval_id;primaryKey"`
	AccountID          string `gorm:"column:account_id"`
	SignerAddress      []byte `gorm:"column:signer_address"`
	Scheme             string `gorm:"column:scheme"`
	ChainID            int64  `gorm:"column:chain_id"`
	Digest             []byte `gorm:"column:digest"`
	IntentHash         []byte `gorm:"column:intent_hash"`
	PayloadSize        int64  `gorm:"column:payload_size"`
	AuthorizationEpoch int64  `gorm:"column:authorization_epoch"`
	ConfirmationLevel  string `gorm:"column:confirmation_level"`
	State              string `gorm:"column:state"`
	CreatedAtMS        int64  `gorm:"column:created_at_ms"`
	ConfirmedAtMS      int64  `gorm:"column:confirmed_at_ms"`
	ExpiresAtMS        int64  `gorm:"column:expires_at_ms"`
	ConsumedAtMS       *int64 `gorm:"column:consumed_at_ms"`
	InvalidatedAtMS    *int64 `gorm:"column:invalidated_at_ms"`
	InvalidationReason string `gorm:"column:invalidation_reason"`
	Revision           int64  `gorm:"column:revision"`
}

func (messageApprovalRow) TableName() string { return "message_signing_approvals" }

type messageSigningRow struct {
	SigningID      string `gorm:"column:signing_id;primaryKey"`
	ApprovalID     string `gorm:"column:approval_id"`
	AccountID      string `gorm:"column:account_id"`
	SignerAddress  []byte `gorm:"column:signer_address"`
	Scheme         string `gorm:"column:scheme"`
	ChainID        int64  `gorm:"column:chain_id"`
	Digest         []byte `gorm:"column:digest"`
	IntentHash     []byte `gorm:"column:intent_hash"`
	State          string `gorm:"column:state"`
	SignatureHash  []byte `gorm:"column:signature_hash"`
	LastResultCode string `gorm:"column:last_result_code"`
	CreatedAtMS    int64  `gorm:"column:created_at_ms"`
	CompletedAtMS  *int64 `gorm:"column:completed_at_ms"`
	Revision       int64  `gorm:"column:revision"`
}

func (messageSigningRow) TableName() string { return "message_signing_records" }

var _ evm.MessageSigningRepository = (*GORMRepository)(nil)
var _ wallet.MessageApprovalVerifier = (*GORMRepository)(nil)

func (repository *GORMRepository) VerifyMessageApproval(ctx context.Context, binding wallet.MessageApprovalBinding) error {
	if binding.AccountID == "" || binding.ApprovalID == "" || binding.Digest == ([32]byte{}) || binding.IntentHash == ([32]byte{}) || !validStoredMessageScheme(binding.Scheme, binding.ChainID) {
		return fmt.Errorf("message approval binding is invalid")
	}
	var count int64
	err := repository.db.WithContext(ctx).Table("message_signing_approvals AS approval").
		Joins("JOIN message_signing_records AS signing_record ON signing_record.approval_id = approval.approval_id").
		Joins("JOIN accounts AS account ON account.account_id = approval.account_id").
		Where("approval.approval_id = ? AND approval.account_id = ? AND approval.scheme = ? AND approval.chain_id = ? AND approval.digest = ? AND approval.intent_hash = ? AND approval.state = ? AND approval.consumed_at_ms IS NOT NULL AND approval.consumed_at_ms >= approval.created_at_ms AND approval.consumed_at_ms < approval.expires_at_ms", binding.ApprovalID, binding.AccountID, string(binding.Scheme), int64(binding.ChainID), binding.Digest[:], binding.IntentHash[:], string(evm.MessageApprovalConsumed)).
		Where("signing_record.account_id = ? AND signing_record.signer_address = approval.signer_address AND signing_record.scheme = ? AND signing_record.chain_id = ? AND signing_record.digest = ? AND signing_record.intent_hash = ? AND signing_record.state = ?", binding.AccountID, string(binding.Scheme), int64(binding.ChainID), binding.Digest[:], binding.IntentHash[:], string(evm.MessageSigningInProgress)).
		Where("account.authorization_epoch = approval.authorization_epoch AND account.state IN ? AND account.signer_kind IN ? AND (account.capabilities & ?) != 0", []string{string(wallet.AccountStateActive), string(wallet.AccountStateLocked)}, []string{string(wallet.SignerKindSoftware), string(wallet.SignerKindCloud), string(wallet.SignerKindHardware)}, int64(wallet.CapabilitySignMessage)).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("verify message approval: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("message approval is not consumable")
	}
	return nil
}

func (repository *GORMRepository) IssueMessageApproval(ctx context.Context, approval evm.MessageApproval) error {
	if err := evm.ValidateMessageApproval(approval); err != nil {
		return err
	}
	if approval.PayloadSize > math.MaxInt64 || approval.AuthorizationEpoch > math.MaxInt64 || approval.ChainID > math.MaxInt64 {
		return &evm.EngineError{Code: evm.ErrorInvalidIntent, Field: "message approval storage bounds"}
	}
	row := messageApprovalRow{
		ApprovalID: approval.ApprovalID, AccountID: approval.AccountID, SignerAddress: approval.Signer.Bytes(),
		Scheme: string(approval.Scheme), ChainID: int64(approval.ChainID), Digest: append([]byte(nil), approval.Digest[:]...),
		IntentHash: append([]byte(nil), approval.IntentHash[:]...), PayloadSize: int64(approval.PayloadSize), AuthorizationEpoch: int64(approval.AuthorizationEpoch),
		ConfirmationLevel: string(approval.ConfirmationLevel), State: string(approval.State),
		CreatedAtMS: approval.CreatedAt.UTC().UnixMilli(), ConfirmedAtMS: approval.ConfirmedAt.UTC().UnixMilli(), ExpiresAtMS: approval.ExpiresAt.UTC().UnixMilli(), Revision: int64(approval.Revision),
	}
	if err := repository.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("issue message approval: %w", err)
	}
	return nil
}

func (repository *GORMRepository) AuthorizeMessageSigning(ctx context.Context, request evm.AuthorizeMessageSigningRequest) (evm.MessageSigningRecord, error) {
	if err := evm.ValidateAuthorizeMessageSigningRequest(request); err != nil {
		return evm.MessageSigningRecord{}, err
	}
	var created messageSigningRow
	err := repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var account wallet.Account
		if err := transaction.Select("account_id", "address", "signer_kind", "state", "capabilities", "authorization_epoch").Where("account_id = ?", request.AccountID).First(&account).Error; err != nil {
			return fmt.Errorf("load message signing account: %w", err)
		}
		if common.HexToAddress(account.Address) != request.Signer || !account.SignerKind.SupportsEOASigning() || (account.State != wallet.AccountStateActive && account.State != wallet.AccountStateLocked) || account.Capabilities&wallet.CapabilitySignMessage == 0 || account.AuthorizationEpoch != request.AuthorizationEpoch {
			return &evm.EngineError{Code: evm.ErrorPolicyDenied, Field: "message signing account"}
		}
		var approval messageApprovalRow
		if err := transaction.Where("approval_id = ?", request.ApprovalID).First(&approval).Error; err != nil {
			return fmt.Errorf("load message approval: %w", err)
		}
		authorizedAt := request.AuthorizedAt.UTC().UnixMilli()
		if approval.AccountID != request.AccountID || common.BytesToAddress(approval.SignerAddress) != request.Signer || approval.Scheme != string(request.Scheme) || approval.ChainID != int64(request.ChainID) || !equal32(approval.Digest, request.Digest) || !equal32(approval.IntentHash, request.IntentHash) || approval.AuthorizationEpoch != int64(request.AuthorizationEpoch) || approval.State != string(evm.MessageApprovalPending) || approval.ExpiresAtMS <= authorizedAt {
			return &evm.EngineError{Code: evm.ErrorApprovalConsumed, Field: "message approval binding"}
		}
		updated := transaction.Model(&messageApprovalRow{}).
			Where("approval_id = ? AND state = ? AND revision = ? AND expires_at_ms > ?", approval.ApprovalID, string(evm.MessageApprovalPending), approval.Revision, authorizedAt).
			Updates(map[string]any{"state": string(evm.MessageApprovalConsumed), "consumed_at_ms": authorizedAt, "revision": approval.Revision + 1})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return &evm.EngineError{Code: evm.ErrorApprovalConsumed, Field: "message approval state"}
		}
		created = messageSigningRow{
			SigningID: request.SigningID, ApprovalID: request.ApprovalID, AccountID: request.AccountID,
			SignerAddress: request.Signer.Bytes(), Scheme: string(request.Scheme), ChainID: int64(request.ChainID),
			Digest: append([]byte(nil), request.Digest[:]...), IntentHash: append([]byte(nil), request.IntentHash[:]...),
			State: string(evm.MessageSigningInProgress), CreatedAtMS: authorizedAt, Revision: 1,
		}
		return transaction.Create(&created).Error
	})
	if err != nil {
		return evm.MessageSigningRecord{}, fmt.Errorf("authorize message signing: %w", err)
	}
	return messageSigningRecordFromRow(created), nil
}

func (repository *GORMRepository) CompleteMessageSigning(ctx context.Context, request evm.CompleteMessageSigningRequest) error {
	if err := evm.ValidateCompleteMessageSigningRequest(request); err != nil {
		return err
	}
	completedAt := request.CompletedAt.UTC().UnixMilli()
	result := repository.db.WithContext(ctx).Model(&messageSigningRow{}).
		Where("signing_id = ? AND state = ?", request.SigningID, string(evm.MessageSigningInProgress)).
		Where("EXISTS (SELECT 1 FROM message_signing_approvals approval JOIN accounts account ON account.account_id = approval.account_id WHERE approval.approval_id = message_signing_records.approval_id AND account.authorization_epoch = approval.authorization_epoch AND account.state IN ? AND (account.capabilities & ?) != 0)", []string{string(wallet.AccountStateActive), string(wallet.AccountStateLocked)}, int64(wallet.CapabilitySignMessage)).
		Updates(map[string]any{
			"state": string(evm.MessageSigningSigned), "signature_hash": request.SignatureHash.Bytes(),
			"last_result_code": "signed", "completed_at_ms": completedAt, "revision": gorm.Expr("revision + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("complete message signing: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return &evm.EngineError{Code: evm.ErrorApprovalConsumed, Field: "message signing completion state"}
	}
	return nil
}

func (repository *GORMRepository) FailMessageSigning(ctx context.Context, request evm.FailMessageSigningRequest) error {
	if err := evm.ValidateFailMessageSigningRequest(request); err != nil {
		return err
	}
	result := repository.db.WithContext(ctx).Model(&messageSigningRow{}).
		Where("signing_id = ? AND state = ?", request.SigningID, string(evm.MessageSigningInProgress)).
		Updates(map[string]any{
			"state": string(evm.MessageSigningFailed), "last_result_code": request.ResultCode,
			"completed_at_ms": request.CompletedAt.UTC().UnixMilli(), "revision": gorm.Expr("revision + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("fail message signing: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return &evm.EngineError{Code: evm.ErrorApprovalConsumed, Field: "message signing failure state"}
	}
	return nil
}

func messageSigningRecordFromRow(row messageSigningRow) evm.MessageSigningRecord {
	var digest [32]byte
	var intentHash [32]byte
	copy(digest[:], row.Digest)
	copy(intentHash[:], row.IntentHash)
	record := evm.MessageSigningRecord{
		SigningID: row.SigningID, ApprovalID: row.ApprovalID, AccountID: row.AccountID,
		Signer: common.BytesToAddress(row.SignerAddress), Scheme: wallet.MessageSigningScheme(row.Scheme), ChainID: uint64(row.ChainID),
		Digest: digest, IntentHash: intentHash, State: evm.MessageSigningState(row.State), SignatureHash: common.BytesToHash(row.SignatureHash),
		LastResultCode: row.LastResultCode, CreatedAt: time.UnixMilli(row.CreatedAtMS).UTC(), Revision: uint64(row.Revision),
	}
	if row.CompletedAtMS != nil {
		record.CompletedAt = time.UnixMilli(*row.CompletedAtMS).UTC()
	}
	return record
}

func validStoredMessageScheme(scheme wallet.MessageSigningScheme, chainID uint64) bool {
	return (scheme == wallet.MessageSigningEIP191Personal && chainID == 0) || (scheme == wallet.MessageSigningEIP712 && chainID > 0 && chainID <= math.MaxInt64)
}

func equal32(encoded []byte, expected [32]byte) bool {
	return len(encoded) == len(expected) && common.BytesToHash(encoded) == common.Hash(expected)
}
