package evm

import (
	"context"
	"math/big"
	"time"
)

type TrackingResult struct {
	TransactionID string
	State         TransactionState
	Confirmations uint64
	Receipt       *Receipt
}

type ReceiptTracker struct {
	repository TransactionRepository
	rpc        RPC
}

func NewReceiptTracker(repository TransactionRepository, rpc RPC) *ReceiptTracker {
	return &ReceiptTracker{repository: repository, rpc: rpc}
}

func (tracker *ReceiptTracker) TrackOnce(ctx context.Context, transactionID string, confirmationTarget uint64, observedAt time.Time) (TrackingResult, error) {
	if tracker == nil || tracker.repository == nil || tracker.rpc == nil {
		return TrackingResult{}, invalidIntent("receipt tracker")
	}
	if err := ValidateTransactionID(transactionID); err != nil {
		return TrackingResult{}, err
	}
	if confirmationTarget == 0 || confirmationTarget > 10_000 || observedAt.IsZero() {
		return TrackingResult{}, invalidIntent("confirmation target")
	}
	record, err := tracker.repository.GetTransaction(ctx, transactionID)
	if err != nil {
		return TrackingResult{}, err
	}
	if record.TransactionHash == ([32]byte{}) {
		return TrackingResult{}, &EngineError{Code: ErrorInvalidIntent, Field: "transaction hash"}
	}
	if record.ChainID != tracker.rpc.ChainID() {
		return TrackingResult{}, &EngineError{Code: ErrorProviderUnavailable, Field: "receipt chain identity"}
	}
	if record.ConfirmationTarget != 0 && record.ConfirmationTarget != confirmationTarget {
		return TrackingResult{}, &EngineError{Code: ErrorPolicyDenied, Field: "confirmation target"}
	}
	receipt, found, err := tracker.rpc.TransactionReceipt(ctx, record.TransactionHash)
	if err != nil {
		return TrackingResult{}, &EngineError{Code: ErrorProviderUnavailable, Field: "transaction receipt", Cause: err}
	}
	if !found {
		if record.State == TransactionConfirming || record.State == TransactionConfirmed || record.State == TransactionReverted {
			return tracker.markReorged(ctx, record, "receipt_disappeared", observedAt)
		}
		return TrackingResult{TransactionID: transactionID, State: record.State}, nil
	}
	if receipt.TransactionHash != record.TransactionHash || receipt.Block.Number == 0 || receipt.Block.Hash == ([32]byte{}) {
		return TrackingResult{}, &EngineError{Code: ErrorProviderUnavailable, Field: "receipt identity"}
	}
	if record.State != TransactionReorged && record.Receipt != nil && (record.Receipt.Block != receipt.Block || record.Receipt.Status != receipt.Status || record.Receipt.TransactionIndex != receipt.TransactionIndex || record.Receipt.GasUsed != receipt.GasUsed) {
		return tracker.markReorged(ctx, record, "receipt_changed", observedAt)
	}
	canonical, found, err := tracker.rpc.HeaderByNumber(ctx, receipt.Block.Number)
	if err != nil {
		return TrackingResult{}, &EngineError{Code: ErrorProviderUnavailable, Field: "canonical receipt block", Cause: err}
	}
	if !found || canonical.Hash != receipt.Block.Hash {
		return tracker.markReorged(ctx, record, "canonical_hash_changed", observedAt)
	}
	head, err := tracker.rpc.LatestHeader(ctx)
	if err != nil {
		return TrackingResult{}, &EngineError{Code: ErrorProviderUnavailable, Field: "confirmation head", Cause: err}
	}
	if head.Number < receipt.Block.Number {
		return tracker.markReorged(ctx, record, "head_behind_receipt", observedAt)
	}
	confirmations := head.Number - receipt.Block.Number + 1
	state := TransactionConfirming
	if confirmations >= confirmationTarget {
		if receipt.Status == 0 {
			state = TransactionReverted
		} else if (record.Operation == OperationERC20Transfer || record.Operation == OperationERC20Approve || record.Operation == OperationERC721SafeTransfer || record.Operation == OperationERC1155SafeTransfer || record.Operation == OperationERC1155BatchTransfer) && !HasExpectedEffect(record, receipt) {
			state = TransactionEffectUnverified
		} else {
			state = TransactionConfirmed
		}
	}
	observation := ReceiptObservation{
		TransactionID: transactionID, Receipt: receipt, Confirmations: confirmations,
		ConfirmationTarget: confirmationTarget, ObservedAt: observedAt.UTC(), State: state,
	}
	if err := tracker.repository.RecordReceipt(ctx, observation); err != nil {
		return TrackingResult{}, err
	}
	copyReceipt := receipt
	if receipt.EffectiveGasPrice != nil {
		copyReceipt.EffectiveGasPrice = new(big.Int).Set(receipt.EffectiveGasPrice)
	}
	return TrackingResult{TransactionID: transactionID, State: state, Confirmations: confirmations, Receipt: &copyReceipt}, nil
}

func (tracker *ReceiptTracker) markReorged(ctx context.Context, record TransactionRecord, reason string, observedAt time.Time) (TrackingResult, error) {
	if err := tracker.repository.MarkReorged(ctx, ReorgObservation{TransactionID: record.TransactionID, Reason: reason, ObservedAt: observedAt.UTC()}); err != nil {
		return TrackingResult{}, err
	}
	return TrackingResult{TransactionID: record.TransactionID, State: TransactionReorged}, nil
}
