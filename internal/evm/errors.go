package evm

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	ErrorInvalidIntent       ErrorCode = "invalid_intent"
	ErrorPolicyDenied        ErrorCode = "policy_denied"
	ErrorSimulationFailed    ErrorCode = "simulation_failed"
	ErrorProviderUnavailable ErrorCode = "provider_unavailable"
	ErrorPlanStale           ErrorCode = "plan_stale"
	ErrorApprovalExpired     ErrorCode = "approval_expired"
	ErrorApprovalConsumed    ErrorCode = "approval_consumed"
	ErrorNonceConflict       ErrorCode = "nonce_conflict"
	ErrorSigningFailed       ErrorCode = "signing_failed"
	ErrorBroadcastRejected   ErrorCode = "broadcast_rejected"
	ErrorReceiptReverted     ErrorCode = "receipt_reverted"
	ErrorReorgDetected       ErrorCode = "reorg_detected"
	ErrorTransactionNotFound ErrorCode = "transaction_not_found"
)

type EngineError struct {
	Code  ErrorCode
	Field string
	Cause error
}

func (engineError *EngineError) Error() string {
	if engineError == nil {
		return "EVM engine error"
	}
	if engineError.Field != "" {
		return fmt.Sprintf("EVM %s: invalid %s", engineError.Code, engineError.Field)
	}
	return fmt.Sprintf("EVM %s", engineError.Code)
}

func (engineError *EngineError) Unwrap() error {
	if engineError == nil {
		return nil
	}
	return engineError.Cause
}

func IsErrorCode(err error, code ErrorCode) bool {
	var engineError *EngineError
	return errors.As(err, &engineError) && engineError.Code == code
}

func invalidIntent(field string) error {
	return &EngineError{Code: ErrorInvalidIntent, Field: field}
}
