package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"gorm.io/gorm"
)

type evmApprovalRow struct {
	ApprovalID         string `gorm:"column:approval_id;primaryKey"`
	ReservationID      string `gorm:"column:reservation_id"`
	AccountID          string `gorm:"column:account_id"`
	SenderAddress      []byte `gorm:"column:sender_address"`
	ChainID            int64  `gorm:"column:chain_id"`
	Nonce              int64  `gorm:"column:nonce"`
	AuthorizationEpoch int64  `gorm:"column:authorization_epoch"`
	PlanHash           []byte `gorm:"column:plan_hash"`
	TransactionDigest  []byte `gorm:"column:transaction_digest"`
	RiskLevel          string `gorm:"column:risk_level"`
	ConfirmationLevel  string `gorm:"column:confirmation_level"`
	ConfirmationTarget int64  `gorm:"column:confirmation_target"`
	State              string `gorm:"column:state"`
	CreatedAtMS        int64  `gorm:"column:created_at_ms"`
	ConfirmedAtMS      int64  `gorm:"column:confirmed_at_ms"`
	ExpiresAtMS        int64  `gorm:"column:expires_at_ms"`
	ConsumedAtMS       *int64 `gorm:"column:consumed_at_ms"`
	InvalidatedAtMS    *int64 `gorm:"column:invalidated_at_ms"`
	Revision           int64  `gorm:"column:revision"`
}

func (evmApprovalRow) TableName() string { return "evm_approvals" }

type evmTransactionRow struct {
	TransactionID       string `gorm:"column:transaction_id;primaryKey"`
	ApprovalID          string `gorm:"column:approval_id"`
	ReservationID       string `gorm:"column:reservation_id"`
	AccountID           string `gorm:"column:account_id"`
	SenderAddress       []byte `gorm:"column:sender_address"`
	ChainID             int64  `gorm:"column:chain_id"`
	Nonce               int64  `gorm:"column:nonce"`
	Operation           string `gorm:"column:operation"`
	CounterpartyAddress []byte `gorm:"column:counterparty_address"`
	AssetContract       []byte `gorm:"column:asset_contract"`
	AssetAmount         []byte `gorm:"column:asset_amount"`
	TokenID             []byte `gorm:"column:token_id"`
	PlanHash            []byte `gorm:"column:plan_hash"`
	TransactionDigest   []byte `gorm:"column:transaction_digest"`
	State               string `gorm:"column:state"`
	SignedPayload       []byte `gorm:"column:signed_payload"`
	TransactionHash     []byte `gorm:"column:transaction_hash"`
	BroadcastAttempts   int64  `gorm:"column:broadcast_attempts"`
	FirstBroadcastAtMS  *int64 `gorm:"column:first_broadcast_at_ms"`
	LastBroadcastAtMS   *int64 `gorm:"column:last_broadcast_at_ms"`
	LastResultCode      string `gorm:"column:last_result_code"`
	ReceiptStatus       *int64 `gorm:"column:receipt_status"`
	ReceiptBlockNumber  *int64 `gorm:"column:receipt_block_number"`
	ReceiptBlockHash    []byte `gorm:"column:receipt_block_hash"`
	ReceiptTxIndex      *int64 `gorm:"column:receipt_tx_index"`
	ReceiptGasUsed      *int64 `gorm:"column:receipt_gas_used"`
	EffectiveGasPrice   []byte `gorm:"column:effective_gas_price"`
	Confirmations       int64  `gorm:"column:confirmations"`
	ConfirmationTarget  int64  `gorm:"column:confirmation_target"`
	ReorgCount          int64  `gorm:"column:reorg_count"`
	CreatedAtMS         int64  `gorm:"column:created_at_ms"`
	UpdatedAtMS         int64  `gorm:"column:updated_at_ms"`
	Revision            int64  `gorm:"column:revision"`
}

func (evmTransactionRow) TableName() string { return "evm_transactions" }

type evmNonceReservationRow struct {
	ReservationID  string `gorm:"column:reservation_id;primaryKey"`
	OperationID    string `gorm:"column:operation_id"`
	AccountID      string `gorm:"column:account_id"`
	SenderAddress  []byte `gorm:"column:sender_address"`
	ChainID        int64  `gorm:"column:chain_id"`
	Nonce          int64  `gorm:"column:nonce"`
	PlanGeneration int64  `gorm:"column:plan_generation"`
	State          string `gorm:"column:state"`
	ReservedAtMS   int64  `gorm:"column:reserved_at_ms"`
	ExpiresAtMS    int64  `gorm:"column:expires_at_ms"`
	Revision       int64  `gorm:"column:revision"`
}

func (evmNonceReservationRow) TableName() string { return "evm_nonce_reservations" }

var _ evm.TransactionRepository = (*GORMRepository)(nil)
var _ wallet.TransactionApprovalVerifier = (*GORMRepository)(nil)

func (repository *GORMRepository) VerifyTransactionApproval(ctx context.Context, binding wallet.TransactionApprovalBinding) error {
	if binding.AccountID == "" || binding.ChainID == 0 || binding.ApprovalID == "" || binding.Digest == ([32]byte{}) {
		return fmt.Errorf("transaction approval binding is invalid")
	}
	var count int64
	err := repository.db.WithContext(ctx).Table("evm_approvals AS approval").
		Joins("JOIN evm_transactions AS transaction_record ON transaction_record.approval_id = approval.approval_id").
		Where("approval.approval_id = ? AND approval.account_id = ? AND approval.chain_id = ? AND approval.transaction_digest = ? AND approval.state = ?", binding.ApprovalID, binding.AccountID, int64(binding.ChainID), binding.Digest[:], string(evm.ApprovalConsumed)).
		Where("transaction_record.account_id = ? AND transaction_record.chain_id = ? AND transaction_record.transaction_digest = ? AND transaction_record.state = ?", binding.AccountID, int64(binding.ChainID), binding.Digest[:], string(evm.TransactionSigning)).
		Count(&count).Error
	if err != nil {
		return fmt.Errorf("verify transaction approval")
	}
	if count != 1 {
		return fmt.Errorf("transaction approval is not consumable")
	}
	return nil
}

func (repository *GORMRepository) ListRecoverableTransactions(ctx context.Context, limit int) ([]evm.TransactionRecord, error) {
	if limit <= 0 || limit > 100 {
		return nil, &evm.EngineError{Code: evm.ErrorInvalidIntent, Field: "recovery limit"}
	}
	var rows []evmTransactionRow
	if err := repository.db.WithContext(ctx).Where("state IN ?", []string{
		string(evm.TransactionSigning), string(evm.TransactionBroadcasting), string(evm.TransactionBroadcastFailed), string(evm.TransactionSubmitted),
		string(evm.TransactionConfirming), string(evm.TransactionConfirmed), string(evm.TransactionReverted), string(evm.TransactionEffectUnverified), string(evm.TransactionReorged),
	}).Order("updated_at_ms ASC, transaction_id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list recoverable EVM transactions: %w", err)
	}
	result := make([]evm.TransactionRecord, 0, len(rows))
	for _, row := range rows {
		record := transactionRecordFromRow(row)
		effects, err := repository.loadTransactionEffects(ctx, row.TransactionID)
		if err != nil {
			return nil, err
		}
		record.Effects = effects
		result = append(result, record)
	}
	return result, nil
}

func (repository *GORMRepository) RecordReceipt(ctx context.Context, observation evm.ReceiptObservation) error {
	if err := evm.ValidateReceiptObservation(observation); err != nil {
		return err
	}
	if observation.Receipt.Block.Number > math.MaxInt64 || observation.Receipt.TransactionIndex > math.MaxInt64 || observation.Receipt.GasUsed > math.MaxInt64 || observation.Confirmations > math.MaxInt64 || observation.ConfirmationTarget > math.MaxInt64 {
		return &evm.EngineError{Code: evm.ErrorInvalidIntent, Field: "receipt storage bounds"}
	}
	return repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var row evmTransactionRow
		if err := transaction.Where("transaction_id = ?", observation.TransactionID).First(&row).Error; err != nil {
			return err
		}
		if row.ConfirmationTarget != int64(observation.ConfirmationTarget) || common.BytesToHash(row.TransactionHash) != observation.Receipt.TransactionHash || (row.State != string(evm.TransactionBroadcasting) && row.State != string(evm.TransactionBroadcastFailed) && row.State != string(evm.TransactionSubmitted) && row.State != string(evm.TransactionConfirming) && row.State != string(evm.TransactionReorged) && row.State != string(evm.TransactionConfirmed) && row.State != string(evm.TransactionReverted) && row.State != string(evm.TransactionEffectUnverified)) {
			return &evm.EngineError{Code: evm.ErrorReorgDetected, Field: "receipt transaction state"}
		}
		status := int64(observation.Receipt.Status)
		blockNumber := int64(observation.Receipt.Block.Number)
		transactionIndex := int64(observation.Receipt.TransactionIndex)
		gasUsed := int64(observation.Receipt.GasUsed)
		result := transaction.Model(&evmTransactionRow{}).Where("transaction_id = ? AND revision = ?", row.TransactionID, row.Revision).Updates(map[string]any{
			"state":                string(observation.State),
			"receipt_status":       status,
			"receipt_block_number": blockNumber,
			"receipt_block_hash":   observation.Receipt.Block.Hash.Bytes(),
			"receipt_tx_index":     transactionIndex,
			"receipt_gas_used":     gasUsed,
			"effective_gas_price":  uint256Bytes(observation.Receipt.EffectiveGasPrice),
			"confirmations":        int64(observation.Confirmations),
			"confirmation_target":  int64(observation.ConfirmationTarget),
			"updated_at_ms":        observation.ObservedAt.UTC().UnixMilli(),
			"revision":             row.Revision + 1,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return &evm.EngineError{Code: evm.ErrorReorgDetected, Field: "receipt revision"}
		}
		if observation.State == evm.TransactionConfirmed || observation.State == evm.TransactionReverted || observation.State == evm.TransactionEffectUnverified {
			reservation := transaction.Model(&evmNonceReservationRow{}).Where("reservation_id = ? AND state = ?", row.ReservationID, string(evm.NonceCommitted)).Updates(map[string]any{
				"state":           string(evm.NonceFinalized),
				"finalized_at_ms": observation.ObservedAt.UTC().UnixMilli(),
				"revision":        gorm.Expr("revision + 1"),
			})
			if reservation.Error != nil {
				return reservation.Error
			}
		}
		return nil
	})
}

func (repository *GORMRepository) MarkReorged(ctx context.Context, observation evm.ReorgObservation) error {
	if err := evm.ValidateReorgObservation(observation); err != nil {
		return err
	}
	return repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var row evmTransactionRow
		if err := transaction.Where("transaction_id = ?", observation.TransactionID).First(&row).Error; err != nil {
			return err
		}
		if row.State != string(evm.TransactionConfirming) && row.State != string(evm.TransactionConfirmed) && row.State != string(evm.TransactionReverted) && row.State != string(evm.TransactionEffectUnverified) {
			return &evm.EngineError{Code: evm.ErrorReorgDetected, Field: "transaction state"}
		}
		result := transaction.Model(&evmTransactionRow{}).Where("transaction_id = ? AND revision = ?", row.TransactionID, row.Revision).Updates(map[string]any{
			"state":            string(evm.TransactionReorged),
			"confirmations":    0,
			"last_result_code": observation.Reason,
			"reorg_count":      row.ReorgCount + 1,
			"updated_at_ms":    observation.ObservedAt.UTC().UnixMilli(),
			"revision":         row.Revision + 1,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return &evm.EngineError{Code: evm.ErrorReorgDetected, Field: "reorg revision"}
		}
		reservation := transaction.Model(&evmNonceReservationRow{}).Where("reservation_id = ? AND state = ?", row.ReservationID, string(evm.NonceFinalized)).Updates(map[string]any{
			"state":           string(evm.NonceCommitted),
			"finalized_at_ms": nil,
			"revision":        gorm.Expr("revision + 1"),
		})
		return reservation.Error
	})
}

func (repository *GORMRepository) RecordBroadcastResult(ctx context.Context, broadcastResult evm.BroadcastResult) error {
	if err := evm.ValidateBroadcastResult(broadcastResult); err != nil {
		return err
	}
	state := evm.TransactionBroadcastFailed
	if broadcastResult.Accepted {
		state = evm.TransactionSubmitted
	}
	result := repository.db.WithContext(ctx).Model(&evmTransactionRow{}).Where(
		"transaction_id = ? AND state = ? AND transaction_hash = ?",
		broadcastResult.TransactionID, string(evm.TransactionBroadcasting), broadcastResult.Hash.Bytes(),
	).Updates(map[string]any{
		"state":            string(state),
		"last_result_code": broadcastResult.ResultCode,
		"updated_at_ms":    broadcastResult.CompletedAt.UTC().UnixMilli(),
		"revision":         gorm.Expr("revision + 1"),
	})
	if result.Error != nil {
		return fmt.Errorf("record EVM broadcast result: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return &evm.EngineError{Code: evm.ErrorBroadcastRejected, Field: "broadcast result state"}
	}
	return nil
}

func (repository *GORMRepository) BeginFirstBroadcast(ctx context.Context, request evm.FirstBroadcastRequest) (evm.BroadcastAttempt, error) {
	if err := evm.ValidateFirstBroadcastRequest(request); err != nil {
		return evm.BroadcastAttempt{}, err
	}
	hash := crypto.Keccak256Hash(request.SignedPayload)
	startedAt := request.StartedAt.UTC().UnixMilli()
	result := repository.db.WithContext(ctx).Model(&evmTransactionRow{}).Where(
		"transaction_id = ? AND state = ? AND signed_payload IS NULL AND broadcast_attempts = 0",
		request.TransactionID, string(evm.TransactionSigning),
	).Updates(map[string]any{
		"state":                 string(evm.TransactionBroadcasting),
		"signed_payload":        append([]byte(nil), request.SignedPayload...),
		"transaction_hash":      append([]byte(nil), hash.Bytes()...),
		"broadcast_attempts":    1,
		"first_broadcast_at_ms": startedAt,
		"last_broadcast_at_ms":  startedAt,
		"updated_at_ms":         startedAt,
		"revision":              gorm.Expr("revision + 1"),
	})
	if result.Error != nil {
		return evm.BroadcastAttempt{}, fmt.Errorf("begin first EVM broadcast: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return evm.BroadcastAttempt{}, &evm.EngineError{Code: evm.ErrorSigningFailed, Field: "first broadcast state"}
	}
	return evm.BroadcastAttempt{TransactionID: request.TransactionID, SignedPayload: append([]byte(nil), request.SignedPayload...), Hash: hash, Attempt: 1, StartedAt: request.StartedAt.UTC()}, nil
}

func (repository *GORMRepository) BeginRebroadcast(ctx context.Context, transactionID string, startedAt time.Time) (evm.BroadcastAttempt, error) {
	if err := evm.ValidateTransactionID(transactionID); err != nil {
		return evm.BroadcastAttempt{}, err
	}
	if startedAt.IsZero() {
		return evm.BroadcastAttempt{}, &evm.EngineError{Code: evm.ErrorInvalidIntent, Field: "rebroadcast time"}
	}
	var row evmTransactionRow
	err := repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := transaction.Where("transaction_id = ?", transactionID).First(&row).Error; err != nil {
			return err
		}
		if len(row.SignedPayload) == 0 || len(row.TransactionHash) != common.HashLength || row.BroadcastAttempts < 1 || (row.State != string(evm.TransactionBroadcasting) && row.State != string(evm.TransactionBroadcastFailed) && row.State != string(evm.TransactionSubmitted) && row.State != string(evm.TransactionReorged)) {
			return &evm.EngineError{Code: evm.ErrorBroadcastRejected, Field: "rebroadcast state"}
		}
		timestamp := startedAt.UTC().UnixMilli()
		result := transaction.Model(&evmTransactionRow{}).Where("transaction_id = ? AND revision = ?", row.TransactionID, row.Revision).Updates(map[string]any{
			"state":                string(evm.TransactionBroadcasting),
			"broadcast_attempts":   row.BroadcastAttempts + 1,
			"last_broadcast_at_ms": timestamp,
			"updated_at_ms":        timestamp,
			"revision":             row.Revision + 1,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return &evm.EngineError{Code: evm.ErrorBroadcastRejected, Field: "rebroadcast revision"}
		}
		row.BroadcastAttempts++
		row.Revision++
		return nil
	})
	if err != nil {
		return evm.BroadcastAttempt{}, fmt.Errorf("begin EVM rebroadcast: %w", err)
	}
	return evm.BroadcastAttempt{
		TransactionID: row.TransactionID,
		ChainID:       uint64(row.ChainID),
		SignedPayload: append([]byte(nil), row.SignedPayload...),
		Hash:          common.BytesToHash(row.TransactionHash),
		Attempt:       uint64(row.BroadcastAttempts),
		StartedAt:     startedAt.UTC(),
	}, nil
}

func (repository *GORMRepository) RecordSigningFailure(ctx context.Context, request evm.SigningFailureRequest) error {
	if err := evm.ValidateSigningFailureRequest(request); err != nil {
		return err
	}
	return repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		var row evmTransactionRow
		if err := transaction.Where("transaction_id = ? AND state = ?", request.TransactionID, string(evm.TransactionSigning)).First(&row).Error; err != nil {
			return &evm.EngineError{Code: evm.ErrorSigningFailed, Field: "transaction state", Cause: err}
		}
		failedAt := request.FailedAt.UTC().UnixMilli()
		result := transaction.Model(&evmTransactionRow{}).Where(
			"transaction_id = ? AND state = ? AND revision = ?", request.TransactionID, string(evm.TransactionSigning), row.Revision,
		).Updates(map[string]any{
			"state": string(evm.TransactionSigningFailed), "last_result_code": request.ResultCode,
			"updated_at_ms": failedAt, "revision": row.Revision + 1,
		})
		if result.Error != nil || result.RowsAffected != 1 {
			return &evm.EngineError{Code: evm.ErrorSigningFailed, Field: "transaction state", Cause: result.Error}
		}
		reservation := transaction.Model(&evmNonceReservationRow{}).Where(
			"reservation_id = ? AND state = ?", row.ReservationID, string(evm.NonceCommitted),
		).Updates(map[string]any{
			"state": string(evm.NonceInvalidated), "invalidated_at_ms": failedAt,
			"invalidation_reason": "signing_failed", "committed_at_ms": nil,
			"revision": gorm.Expr("revision + 1"),
		})
		return reservation.Error
	})
}

func (repository *GORMRepository) GetTransaction(ctx context.Context, transactionID string) (evm.TransactionRecord, error) {
	if err := evm.ValidateTransactionID(transactionID); err != nil {
		return evm.TransactionRecord{}, err
	}
	var row evmTransactionRow
	if err := repository.db.WithContext(ctx).Where("transaction_id = ?", transactionID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return evm.TransactionRecord{}, &evm.EngineError{Code: evm.ErrorTransactionNotFound, Field: "transaction ID"}
		}
		return evm.TransactionRecord{}, fmt.Errorf("load EVM transaction: %w", err)
	}
	record := transactionRecordFromRow(row)
	effects, err := repository.loadTransactionEffects(ctx, transactionID)
	if err != nil {
		return evm.TransactionRecord{}, err
	}
	record.Effects = effects
	return record, nil
}

type evmTransactionEffectRow struct {
	TransactionID string `gorm:"column:transaction_id"`
	EffectIndex   int64  `gorm:"column:effect_index"`
	TokenID       []byte `gorm:"column:token_id"`
	Amount        []byte `gorm:"column:amount"`
}

func (evmTransactionEffectRow) TableName() string { return "evm_transaction_effects" }

func (repository *GORMRepository) loadTransactionEffects(ctx context.Context, transactionID string) ([]evm.EffectEntry, error) {
	var rows []evmTransactionEffectRow
	if err := repository.db.WithContext(ctx).Where("transaction_id = ?", transactionID).Order("effect_index ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load EVM transaction effects: %w", err)
	}
	effects := make([]evm.EffectEntry, 0, len(rows))
	for _, row := range rows {
		if len(row.TokenID) != 32 || len(row.Amount) != 32 {
			return nil, fmt.Errorf("EVM transaction effect row is invalid")
		}
		effects = append(effects, evm.EffectEntry{
			TokenID: new(big.Int).SetBytes(row.TokenID),
			Amount:  new(big.Int).SetBytes(row.Amount),
		})
	}
	return effects, nil
}

func (repository *GORMRepository) InvalidateUnsignedReservation(ctx context.Context, request evm.InvalidateReservationRequest) error {
	if err := evm.ValidateInvalidateReservationRequest(request); err != nil {
		return err
	}
	result := repository.db.WithContext(ctx).Model(&evmNonceReservationRow{}).Where(
		"reservation_id = ? AND account_id = ? AND plan_generation = ? AND state = ?",
		request.ReservationID, request.AccountID, int64(request.PlanGeneration), string(evm.NonceReserved),
	).Updates(map[string]any{
		"state":               string(evm.NonceInvalidated),
		"invalidated_at_ms":   request.InvalidatedAt.UTC().UnixMilli(),
		"invalidation_reason": request.Reason,
		"revision":            gorm.Expr("revision + 1"),
	})
	if result.Error != nil {
		return fmt.Errorf("invalidate EVM nonce reservation: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return &evm.EngineError{Code: evm.ErrorNonceConflict, Field: "reservation state"}
	}
	return nil
}

func (repository *GORMRepository) IssueApproval(ctx context.Context, approval evm.SigningApproval) error {
	if err := evm.ValidateSigningApproval(approval); err != nil {
		return err
	}
	return repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := validateTransactionAccount(transaction, approval.AccountID, approval.Sender, approval.AuthorizationEpoch); err != nil {
			return err
		}
		var reservation evmNonceReservationRow
		if err := transaction.Where("reservation_id = ?", approval.ReservationID).First(&reservation).Error; err != nil {
			return fmt.Errorf("load nonce reservation: %w", err)
		}
		if reservation.State != string(evm.NonceReserved) || reservation.ExpiresAtMS <= approval.ConfirmedAt.UTC().UnixMilli() || reservation.AccountID != approval.AccountID || reservation.ChainID != int64(approval.ChainID) || reservation.Nonce != int64(approval.Nonce) || common.BytesToAddress(reservation.SenderAddress) != approval.Sender {
			return &evm.EngineError{Code: evm.ErrorNonceConflict, Field: "approval reservation binding"}
		}
		confirmationTarget := approval.ConfirmationTarget
		if confirmationTarget == 0 {
			confirmationTarget = 1
		}
		row := evmApprovalRow{
			ApprovalID:         approval.ApprovalID,
			ReservationID:      approval.ReservationID,
			AccountID:          approval.AccountID,
			SenderAddress:      append([]byte(nil), approval.Sender.Bytes()...),
			ChainID:            int64(approval.ChainID),
			Nonce:              int64(approval.Nonce),
			AuthorizationEpoch: int64(approval.AuthorizationEpoch),
			PlanHash:           append([]byte(nil), approval.PlanHash[:]...),
			TransactionDigest:  append([]byte(nil), approval.TransactionDigest[:]...),
			RiskLevel:          string(approval.RiskLevel),
			ConfirmationLevel:  string(approval.ConfirmationLevel),
			ConfirmationTarget: int64(confirmationTarget),
			State:              string(evm.ApprovalPending),
			CreatedAtMS:        approval.CreatedAt.UTC().UnixMilli(),
			ConfirmedAtMS:      approval.ConfirmedAt.UTC().UnixMilli(),
			ExpiresAtMS:        approval.ExpiresAt.UTC().UnixMilli(),
			Revision:           1,
		}
		if err := transaction.Create(&row).Error; err != nil {
			return fmt.Errorf("create signing approval: %w", err)
		}
		return nil
	})
}

func (repository *GORMRepository) AuthorizeSigning(ctx context.Context, request evm.AuthorizeSigningRequest) (evm.TransactionRecord, error) {
	if err := evm.ValidateAuthorizeSigningRequest(request); err != nil {
		return evm.TransactionRecord{}, err
	}
	var record evmTransactionRow
	err := repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := validateTransactionAccount(transaction, request.AccountID, request.Sender, request.AuthorizationEpoch); err != nil {
			return err
		}
		var approval evmApprovalRow
		if err := transaction.Where("approval_id = ?", request.ApprovalID).First(&approval).Error; err != nil {
			return fmt.Errorf("load signing approval: %w", err)
		}
		if approval.State == string(evm.ApprovalConsumed) {
			return &evm.EngineError{Code: evm.ErrorApprovalConsumed, Field: "approval state"}
		}
		if approval.State != string(evm.ApprovalPending) {
			return &evm.EngineError{Code: evm.ErrorPolicyDenied, Field: "approval state"}
		}
		if request.AuthorizedAt.UTC().UnixMilli() >= approval.ExpiresAtMS {
			return &evm.EngineError{Code: evm.ErrorApprovalExpired, Field: "approval expiry"}
		}
		if !approvalMatchesRequest(approval, request) {
			return &evm.EngineError{Code: evm.ErrorPolicyDenied, Field: "approval binding"}
		}
		consumedAt := request.AuthorizedAt.UTC().UnixMilli()
		approvalUpdate := transaction.Model(&evmApprovalRow{}).Where(
			"approval_id = ? AND state = ? AND revision = ?", approval.ApprovalID, string(evm.ApprovalPending), approval.Revision,
		).Updates(map[string]any{"state": string(evm.ApprovalConsumed), "consumed_at_ms": consumedAt, "revision": approval.Revision + 1})
		if approvalUpdate.Error != nil {
			return approvalUpdate.Error
		}
		if approvalUpdate.RowsAffected != 1 {
			return &evm.EngineError{Code: evm.ErrorApprovalConsumed, Field: "approval state"}
		}
		reservationUpdate := transaction.Model(&evmNonceReservationRow{}).Where(
			"reservation_id = ? AND state = ? AND account_id = ? AND chain_id = ? AND nonce = ? AND sender_address = ? AND expires_at_ms > ?",
			request.ReservationID, string(evm.NonceReserved), request.AccountID, int64(request.ChainID), int64(request.Nonce), request.Sender.Bytes(), consumedAt,
		).Updates(map[string]any{"state": string(evm.NonceCommitted), "committed_at_ms": consumedAt, "revision": gorm.Expr("revision + 1")})
		if reservationUpdate.Error != nil {
			return reservationUpdate.Error
		}
		if reservationUpdate.RowsAffected != 1 {
			return &evm.EngineError{Code: evm.ErrorNonceConflict, Field: "reservation state"}
		}
		confirmationTarget := request.ConfirmationTarget
		if confirmationTarget == 0 {
			confirmationTarget = 1
		}
		record = evmTransactionRow{
			TransactionID:       request.TransactionID,
			ApprovalID:          request.ApprovalID,
			ReservationID:       request.ReservationID,
			AccountID:           request.AccountID,
			SenderAddress:       append([]byte(nil), request.Sender.Bytes()...),
			ChainID:             int64(request.ChainID),
			Nonce:               int64(request.Nonce),
			Operation:           string(request.Operation),
			CounterpartyAddress: append([]byte(nil), request.Counterparty.Bytes()...),
			AssetContract:       append([]byte(nil), request.AssetContract.Bytes()...),
			AssetAmount:         uint256Bytes(request.AssetAmount),
			TokenID:             optionalUint256Bytes(request.TokenID),
			PlanHash:            append([]byte(nil), request.PlanHash[:]...),
			TransactionDigest:   append([]byte(nil), request.TransactionDigest[:]...),
			State:               string(evm.TransactionSigning),
			ConfirmationTarget:  int64(confirmationTarget),
			CreatedAtMS:         consumedAt,
			UpdatedAtMS:         consumedAt,
			Revision:            1,
		}
		if err := transaction.Create(&record).Error; err != nil {
			return err
		}
		for index, effect := range request.Effects {
			effectRow := evmTransactionEffectRow{
				TransactionID: request.TransactionID, EffectIndex: int64(index),
				TokenID: uint256Bytes(effect.TokenID), Amount: uint256Bytes(effect.Amount),
			}
			if err := transaction.Create(&effectRow).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return evm.TransactionRecord{}, fmt.Errorf("authorize EVM signing: %w", err)
	}
	return transactionRecordFromRow(record), nil
}

func approvalMatchesRequest(approval evmApprovalRow, request evm.AuthorizeSigningRequest) bool {
	return approval.ReservationID == request.ReservationID &&
		approval.AccountID == request.AccountID &&
		common.BytesToAddress(approval.SenderAddress) == request.Sender &&
		approval.ChainID == int64(request.ChainID) && approval.Nonce == int64(request.Nonce) &&
		approval.AuthorizationEpoch == int64(request.AuthorizationEpoch) &&
		approval.ConfirmationTarget == int64(effectiveConfirmationTarget(request.ConfirmationTarget)) &&
		bytes.Equal(approval.PlanHash, request.PlanHash[:]) && bytes.Equal(approval.TransactionDigest, request.TransactionDigest[:])
}

func transactionRecordFromRow(row evmTransactionRow) evm.TransactionRecord {
	var planHash [32]byte
	var digest [32]byte
	copy(planHash[:], row.PlanHash)
	copy(digest[:], row.TransactionDigest)
	record := evm.TransactionRecord{
		TransactionID: row.TransactionID, ApprovalID: row.ApprovalID, ReservationID: row.ReservationID,
		AccountID: row.AccountID, Sender: common.BytesToAddress(row.SenderAddress), ChainID: uint64(row.ChainID), Nonce: uint64(row.Nonce),
		PlanHash: planHash, TransactionDigest: digest,
		Operation: evm.Operation(row.Operation), Counterparty: common.BytesToAddress(row.CounterpartyAddress), AssetContract: common.BytesToAddress(row.AssetContract), AssetAmount: new(big.Int).SetBytes(row.AssetAmount),
		TokenID: optionalBytesToBigInt(row.TokenID),
		State:   evm.TransactionState(row.State), LastResultCode: row.LastResultCode,
		SignedPayload: append([]byte(nil), row.SignedPayload...), TransactionHash: common.BytesToHash(row.TransactionHash), BroadcastAttempts: uint64(row.BroadcastAttempts),
		Confirmations: uint64(row.Confirmations), ConfirmationTarget: uint64(row.ConfirmationTarget), ReorgCount: uint64(row.ReorgCount),
		CreatedAt: unixMilliTime(row.CreatedAtMS), UpdatedAt: unixMilliTime(row.UpdatedAtMS), Revision: uint64(row.Revision),
	}
	if row.ReceiptStatus != nil && row.ReceiptBlockNumber != nil && len(row.ReceiptBlockHash) == common.HashLength && row.ReceiptTxIndex != nil && row.ReceiptGasUsed != nil && len(row.EffectiveGasPrice) == 32 {
		record.Receipt = &evm.Receipt{
			TransactionHash:  record.TransactionHash,
			Block:            evm.BlockIdentity{Number: uint64(*row.ReceiptBlockNumber), Hash: common.BytesToHash(row.ReceiptBlockHash)},
			TransactionIndex: uint64(*row.ReceiptTxIndex), Status: uint64(*row.ReceiptStatus), GasUsed: uint64(*row.ReceiptGasUsed),
			EffectiveGasPrice: new(big.Int).SetBytes(row.EffectiveGasPrice),
		}
	}
	return record
}

func (repository *GORMRepository) ReserveNonce(ctx context.Context, request evm.ReserveNonceRequest) (evm.NonceReservation, error) {
	if err := evm.ValidateReserveNonceRequest(request); err != nil {
		return evm.NonceReservation{}, err
	}
	var reserved evmNonceReservationRow
	err := repository.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := validateTransactionAccount(transaction, request.AccountID, request.Sender, 0); err != nil {
			return err
		}
		now := request.ReservedAt.UTC().UnixMilli()
		if err := transaction.Model(&evmNonceReservationRow{}).Where(
			"chain_id = ? AND sender_address = ? AND state = ? AND expires_at_ms <= ?",
			int64(request.ChainID), request.Sender.Bytes(), string(evm.NonceReserved), now,
		).Updates(map[string]any{
			"state": string(evm.NonceInvalidated), "invalidated_at_ms": now,
			"invalidation_reason": "expired", "revision": gorm.Expr("revision + 1"),
		}).Error; err != nil {
			return err
		}
		var signingCount int64
		if err := transaction.Table("evm_nonce_reservations AS reservation").
			Joins("JOIN evm_transactions AS transaction_record ON transaction_record.reservation_id = reservation.reservation_id").
			Where("reservation.chain_id = ? AND reservation.sender_address = ? AND reservation.state = ? AND transaction_record.state = ?", int64(request.ChainID), request.Sender.Bytes(), string(evm.NonceCommitted), string(evm.TransactionSigning)).
			Count(&signingCount).Error; err != nil {
			return err
		}
		if signingCount > 0 {
			return &evm.EngineError{Code: evm.ErrorNonceConflict, Field: "signing recovery required"}
		}
		var existing evmNonceReservationRow
		err := transaction.Where(
			"account_id = ? AND chain_id = ? AND operation_id = ? AND plan_generation = ?",
			request.AccountID, int64(request.ChainID), request.OperationID, int64(request.PlanGeneration),
		).First(&existing).Error
		if err == nil {
			if existing.State != string(evm.NonceReserved) || existing.ExpiresAtMS <= now || !sameReservationRequest(existing, request) {
				return &evm.EngineError{Code: evm.ErrorNonceConflict, Field: "operation binding"}
			}
			reserved = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var rows []evmNonceReservationRow
		if err := transaction.Select("nonce").Where(
			"chain_id = ? AND sender_address = ? AND state IN ? AND nonce >= ?",
			int64(request.ChainID), request.Sender.Bytes(), []string{string(evm.NonceReserved), string(evm.NonceCommitted), string(evm.NonceFinalized)}, int64(request.PendingNonce),
		).Order("nonce ASC").Find(&rows).Error; err != nil {
			return err
		}
		nonce := request.PendingNonce
		for _, row := range rows {
			if uint64(row.Nonce) == nonce {
				nonce++
			}
		}
		reserved = evmNonceReservationRow{
			ReservationID:  request.ReservationID,
			OperationID:    request.OperationID,
			AccountID:      request.AccountID,
			SenderAddress:  append([]byte(nil), request.Sender.Bytes()...),
			ChainID:        int64(request.ChainID),
			Nonce:          int64(nonce),
			PlanGeneration: int64(request.PlanGeneration),
			State:          string(evm.NonceReserved),
			ReservedAtMS:   request.ReservedAt.UTC().UnixMilli(),
			ExpiresAtMS:    request.ExpiresAt.UTC().UnixMilli(),
			Revision:       1,
		}
		if err := transaction.Create(&reserved).Error; err != nil {
			if isSQLiteConstraintError(err) {
				return &evm.EngineError{Code: evm.ErrorNonceConflict, Field: "nonce reservation", Cause: err}
			}
			return err
		}
		return nil
	})
	if err != nil {
		return evm.NonceReservation{}, fmt.Errorf("reserve EVM nonce: %w", err)
	}
	return nonceReservationFromRow(reserved), nil
}

func sameReservationRequest(row evmNonceReservationRow, request evm.ReserveNonceRequest) bool {
	return row.AccountID == request.AccountID &&
		row.ChainID == int64(request.ChainID) &&
		common.BytesToAddress(row.SenderAddress) == request.Sender
}

func nonceReservationFromRow(row evmNonceReservationRow) evm.NonceReservation {
	return evm.NonceReservation{
		ReservationID:  row.ReservationID,
		OperationID:    row.OperationID,
		AccountID:      row.AccountID,
		Sender:         common.BytesToAddress(row.SenderAddress),
		ChainID:        uint64(row.ChainID),
		Nonce:          uint64(row.Nonce),
		PlanGeneration: uint64(row.PlanGeneration),
		State:          evm.NonceReservationState(row.State),
		ReservedAt:     unixMilliTime(row.ReservedAtMS),
		ExpiresAt:      unixMilliTime(row.ExpiresAtMS),
		Revision:       uint64(row.Revision),
	}
}

func effectiveConfirmationTarget(target uint64) uint64 {
	if target == 0 {
		return 1
	}
	return target
}

func uint256Bytes(value *big.Int) []byte {
	encoded := make([]byte, 32)
	value.FillBytes(encoded)
	return encoded
}

func optionalUint256Bytes(value *big.Int) []byte {
	if value == nil {
		return nil
	}
	return uint256Bytes(value)
}

func optionalBytesToBigInt(value []byte) *big.Int {
	if len(value) == 0 {
		return nil
	}
	return new(big.Int).SetBytes(value)
}

func validateTransactionAccount(transaction *gorm.DB, accountID string, sender common.Address, authorizationEpoch uint64) error {
	var account wallet.Account
	if err := transaction.Select("account_id", "address", "signer_kind", "state", "capabilities", "authorization_epoch").Where("account_id = ?", accountID).First(&account).Error; err != nil {
		return fmt.Errorf("load transaction account: %w", err)
	}
	if common.HexToAddress(account.Address) != sender || account.SignerKind != wallet.SignerKindSoftware || (account.State != wallet.AccountStateActive && account.State != wallet.AccountStateLocked) || account.Capabilities&wallet.CapabilitySignTransaction == 0 {
		return &evm.EngineError{Code: evm.ErrorPolicyDenied, Field: "transaction account"}
	}
	if authorizationEpoch != 0 && account.AuthorizationEpoch != authorizationEpoch {
		return &evm.EngineError{Code: evm.ErrorPolicyDenied, Field: "authorization epoch"}
	}
	return nil
}

func unixMilliTime(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}

func isSQLiteConstraintError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "constraint failed") || strings.Contains(message, "unique constraint")
}
