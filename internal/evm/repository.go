package evm

import (
	"context"
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type NonceReservationState string

const (
	NonceReserved    NonceReservationState = "reserved"
	NonceCommitted   NonceReservationState = "committed"
	NonceFinalized   NonceReservationState = "finalized"
	NonceInvalidated NonceReservationState = "invalidated"
)

type ReserveNonceRequest struct {
	ReservationID  string
	OperationID    string
	AccountID      string
	Sender         common.Address
	ChainID        uint64
	PendingNonce   uint64
	PlanGeneration uint64
	ReservedAt     time.Time
	ExpiresAt      time.Time
}

type NonceReservation struct {
	ReservationID  string
	OperationID    string
	AccountID      string
	Sender         common.Address
	ChainID        uint64
	Nonce          uint64
	PlanGeneration uint64
	State          NonceReservationState
	ReservedAt     time.Time
	ExpiresAt      time.Time
	Revision       uint64
}

type RiskLevel string

const (
	RiskNormal   RiskLevel = "normal"
	RiskWarning  RiskLevel = "warning"
	RiskCritical RiskLevel = "critical"
)

type ConfirmationLevel string

const (
	ConfirmationStandard   ConfirmationLevel = "standard"
	ConfirmationReinforced ConfirmationLevel = "reinforced"
)

type ApprovalState string

const (
	ApprovalPending     ApprovalState = "pending"
	ApprovalConsumed    ApprovalState = "consumed"
	ApprovalInvalidated ApprovalState = "invalidated"
)

type SigningApproval struct {
	ApprovalID         string
	ReservationID      string
	AccountID          string
	Sender             common.Address
	ChainID            uint64
	Nonce              uint64
	AuthorizationEpoch uint64
	PlanHash           [32]byte
	TransactionDigest  [32]byte
	RiskLevel          RiskLevel
	ConfirmationLevel  ConfirmationLevel
	ConfirmationTarget uint64
	CreatedAt          time.Time
	ConfirmedAt        time.Time
	ExpiresAt          time.Time
	State              ApprovalState
	Revision           uint64
}

type AuthorizeSigningRequest struct {
	TransactionID      string
	ApprovalID         string
	ReservationID      string
	AccountID          string
	Sender             common.Address
	ChainID            uint64
	Nonce              uint64
	AuthorizationEpoch uint64
	PlanHash           [32]byte
	TransactionDigest  [32]byte
	Operation          Operation
	Counterparty       common.Address
	AssetContract      common.Address
	AssetAmount        *big.Int
	TokenID            *big.Int
	Effects            []EffectEntry
	ConfirmationTarget uint64
	AuthorizedAt       time.Time
}

type TransactionState string

const (
	TransactionSigning          TransactionState = "signing"
	TransactionSigningFailed    TransactionState = "signing_failed"
	TransactionBroadcasting     TransactionState = "broadcasting"
	TransactionBroadcastFailed  TransactionState = "broadcast_failed"
	TransactionSubmitted        TransactionState = "submitted"
	TransactionConfirming       TransactionState = "confirming"
	TransactionConfirmed        TransactionState = "confirmed"
	TransactionReverted         TransactionState = "reverted"
	TransactionReorged          TransactionState = "reorged"
	TransactionEffectUnverified TransactionState = "effect_unverified"
)

type SigningFailureRequest struct {
	TransactionID string
	FailedAt      time.Time
	ResultCode    string
}

type FirstBroadcastRequest struct {
	TransactionID string
	SignedPayload []byte
	StartedAt     time.Time
}

type BroadcastAttempt struct {
	TransactionID string
	ChainID       uint64
	SignedPayload []byte
	Hash          common.Hash
	Attempt       uint64
	StartedAt     time.Time
}

type BroadcastResult struct {
	TransactionID string
	Hash          common.Hash
	Accepted      bool
	ResultCode    string
	CompletedAt   time.Time
}

type ReceiptObservation struct {
	TransactionID      string
	Receipt            Receipt
	Confirmations      uint64
	ConfirmationTarget uint64
	ObservedAt         time.Time
	State              TransactionState
}

type ReorgObservation struct {
	TransactionID string
	Reason        string
	ObservedAt    time.Time
}

type TransactionRecord struct {
	TransactionID      string
	ApprovalID         string
	ReservationID      string
	AccountID          string
	Sender             common.Address
	ChainID            uint64
	Nonce              uint64
	PlanHash           [32]byte
	TransactionDigest  [32]byte
	Operation          Operation
	Counterparty       common.Address
	AssetContract      common.Address
	AssetAmount        *big.Int
	TokenID            *big.Int
	Effects            []EffectEntry
	State              TransactionState
	LastResultCode     string
	SignedPayload      []byte
	TransactionHash    common.Hash
	BroadcastAttempts  uint64
	Receipt            *Receipt
	Confirmations      uint64
	ConfirmationTarget uint64
	ReorgCount         uint64
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Revision           uint64
}

type InvalidateReservationRequest struct {
	ReservationID  string
	AccountID      string
	PlanGeneration uint64
	InvalidatedAt  time.Time
	Reason         string
}

type TransactionRepository interface {
	ReserveNonce(context.Context, ReserveNonceRequest) (NonceReservation, error)
	InvalidateUnsignedReservation(context.Context, InvalidateReservationRequest) error
	IssueApproval(context.Context, SigningApproval) error
	AuthorizeSigning(context.Context, AuthorizeSigningRequest) (TransactionRecord, error)
	RecordSigningFailure(context.Context, SigningFailureRequest) error
	GetTransaction(context.Context, string) (TransactionRecord, error)
	BeginFirstBroadcast(context.Context, FirstBroadcastRequest) (BroadcastAttempt, error)
	BeginRebroadcast(context.Context, string, time.Time) (BroadcastAttempt, error)
	RecordBroadcastResult(context.Context, BroadcastResult) error
	RecordReceipt(context.Context, ReceiptObservation) error
	MarkReorged(context.Context, ReorgObservation) error
	ListRecoverableTransactions(context.Context, int) ([]TransactionRecord, error)
}

func ValidateReceiptObservation(observation ReceiptObservation) error {
	if err := ValidateTransactionID(observation.TransactionID); err != nil {
		return err
	}
	if observation.Receipt.TransactionHash == (common.Hash{}) || observation.Receipt.Block.Number == 0 || observation.Receipt.Block.Hash == (common.Hash{}) || (observation.Receipt.Status != 0 && observation.Receipt.Status != 1) || observation.Receipt.GasUsed == 0 || observation.Receipt.EffectiveGasPrice == nil || observation.Receipt.EffectiveGasPrice.Sign() <= 0 || observation.Receipt.EffectiveGasPrice.BitLen() > 256 {
		return invalidIntent("receipt observation")
	}
	if observation.ConfirmationTarget == 0 || observation.Confirmations == 0 || observation.ObservedAt.IsZero() {
		return invalidIntent("receipt confirmations")
	}
	if observation.State != TransactionConfirming && observation.State != TransactionConfirmed && observation.State != TransactionReverted && observation.State != TransactionEffectUnverified {
		return invalidIntent("receipt state")
	}
	if observation.State == TransactionConfirmed && (observation.Receipt.Status != 1 || observation.Confirmations < observation.ConfirmationTarget) {
		return invalidIntent("confirmed receipt")
	}
	if observation.State == TransactionReverted && observation.Receipt.Status != 0 {
		return invalidIntent("reverted receipt")
	}
	if observation.State == TransactionEffectUnverified && (observation.Receipt.Status != 1 || observation.Confirmations < observation.ConfirmationTarget) {
		return invalidIntent("unverified transaction effect")
	}
	return nil
}

func ValidateReorgObservation(observation ReorgObservation) error {
	if err := ValidateTransactionID(observation.TransactionID); err != nil {
		return err
	}
	if observation.ObservedAt.IsZero() || (observation.Reason != "receipt_disappeared" && observation.Reason != "canonical_hash_changed" && observation.Reason != "head_behind_receipt" && observation.Reason != "receipt_changed") {
		return invalidIntent("reorg observation")
	}
	return nil
}

func ValidateBroadcastResult(result BroadcastResult) error {
	if err := ValidateTransactionID(result.TransactionID); err != nil {
		return err
	}
	if result.Hash == (common.Hash{}) || result.CompletedAt.IsZero() {
		return invalidIntent("broadcast result binding")
	}
	if result.Accepted {
		if result.ResultCode != "accepted" && result.ResultCode != "already_known" {
			return invalidIntent("accepted broadcast result code")
		}
	} else if result.ResultCode != "remote_rejected" && result.ResultCode != "transport_unknown" {
		return invalidIntent("failed broadcast result code")
	}
	return nil
}

func ValidateFirstBroadcastRequest(request FirstBroadcastRequest) error {
	if err := ValidateTransactionID(request.TransactionID); err != nil {
		return err
	}
	if len(request.SignedPayload) == 0 || len(request.SignedPayload) > 128<<10 || request.StartedAt.IsZero() {
		return invalidIntent("first broadcast payload")
	}
	return nil
}

func ValidateTransactionID(transactionID string) error {
	if !accountIDPattern.MatchString(transactionID) {
		return invalidIntent("transaction ID")
	}
	return nil
}

func ValidateSigningFailureRequest(request SigningFailureRequest) error {
	if !accountIDPattern.MatchString(request.TransactionID) || request.FailedAt.IsZero() {
		return invalidIntent("signing failure identity")
	}
	if request.ResultCode != "signer_rejected" && request.ResultCode != "capability_expired" && request.ResultCode != "invalid_signature" && request.ResultCode != "cancelled" && request.ResultCode != "persistence_failed" {
		return invalidIntent("signing failure code")
	}
	return nil
}

func ValidateInvalidateReservationRequest(request InvalidateReservationRequest) error {
	if !accountIDPattern.MatchString(request.ReservationID) || !accountIDPattern.MatchString(request.AccountID) {
		return invalidIntent("reservation invalidation identity")
	}
	if request.PlanGeneration == 0 || request.PlanGeneration > math.MaxInt64 || request.InvalidatedAt.IsZero() {
		return invalidIntent("reservation invalidation binding")
	}
	if request.Reason != "user_cancelled" && request.Reason != "expired" && request.Reason != "plan_stale" {
		return invalidIntent("reservation invalidation reason")
	}
	return nil
}

func ValidateSigningApproval(approval SigningApproval) error {
	if !accountIDPattern.MatchString(approval.ApprovalID) || !accountIDPattern.MatchString(approval.ReservationID) || !accountIDPattern.MatchString(approval.AccountID) {
		return invalidIntent("approval identity")
	}
	if approval.Sender == (common.Address{}) || approval.ChainID == 0 || approval.ChainID > math.MaxInt64 || approval.Nonce > math.MaxInt64 || approval.AuthorizationEpoch == 0 || approval.AuthorizationEpoch > math.MaxInt64 || approval.ConfirmationTarget > 10_000 {
		return invalidIntent("approval binding")
	}
	if approval.PlanHash == ([32]byte{}) || approval.TransactionDigest == ([32]byte{}) {
		return invalidIntent("approval digest")
	}
	if approval.RiskLevel != RiskNormal && approval.RiskLevel != RiskWarning && approval.RiskLevel != RiskCritical {
		return invalidIntent("risk level")
	}
	if approval.ConfirmationLevel != ConfirmationStandard && approval.ConfirmationLevel != ConfirmationReinforced {
		return invalidIntent("confirmation level")
	}
	if approval.RiskLevel == RiskCritical && approval.ConfirmationLevel != ConfirmationReinforced {
		return &EngineError{Code: ErrorPolicyDenied, Field: "reinforced confirmation"}
	}
	if approval.CreatedAt.IsZero() || approval.ConfirmedAt.Before(approval.CreatedAt) || !approval.ExpiresAt.After(approval.ConfirmedAt) {
		return invalidIntent("approval expiry")
	}
	return nil
}

func ValidateAuthorizeSigningRequest(request AuthorizeSigningRequest) error {
	if !accountIDPattern.MatchString(request.TransactionID) || !accountIDPattern.MatchString(request.ApprovalID) || !accountIDPattern.MatchString(request.ReservationID) || !accountIDPattern.MatchString(request.AccountID) {
		return invalidIntent("signing authorization identity")
	}
	if request.Sender == (common.Address{}) || request.ChainID == 0 || request.ChainID > math.MaxInt64 || request.Nonce > math.MaxInt64 || request.AuthorizationEpoch == 0 || request.AuthorizationEpoch > math.MaxInt64 || request.ConfirmationTarget > 10_000 {
		return invalidIntent("signing authorization binding")
	}
	if request.PlanHash == ([32]byte{}) || request.TransactionDigest == ([32]byte{}) || request.AuthorizedAt.IsZero() {
		return invalidIntent("signing authorization digest")
	}
	if request.Counterparty == (common.Address{}) || request.AssetAmount == nil || request.AssetAmount.Sign() < 0 || request.AssetAmount.BitLen() > 256 {
		return invalidIntent("signing authorization operation")
	}
	if request.Operation != OperationNativeTransfer && request.Operation != OperationERC20Transfer && request.Operation != OperationERC20Approve && request.Operation != OperationERC721SafeTransfer && request.Operation != OperationERC1155SafeTransfer && request.Operation != OperationERC1155BatchTransfer && request.Operation != OperationContractCall {
		return invalidIntent("signing authorization operation")
	}
	if request.Operation == OperationNativeTransfer && request.AssetContract != (common.Address{}) {
		return invalidIntent("native asset contract")
	}
	if request.Operation != OperationNativeTransfer && request.AssetContract == (common.Address{}) {
		return invalidIntent("token asset contract")
	}
	if request.TokenID != nil && (request.TokenID.Sign() < 0 || request.TokenID.BitLen() > 256) {
		return invalidIntent("signing authorization token ID")
	}
	if len(request.Effects) > 64 {
		return invalidIntent("signing authorization effects")
	}
	for _, effect := range request.Effects {
		if effect.TokenID == nil || effect.Amount == nil || effect.TokenID.Sign() < 0 || effect.TokenID.BitLen() > 256 || effect.Amount.Sign() < 0 || effect.Amount.BitLen() > 256 {
			return invalidIntent("signing authorization effect")
		}
	}
	return nil
}

func ValidateReserveNonceRequest(request ReserveNonceRequest) error {
	if !accountIDPattern.MatchString(request.ReservationID) {
		return invalidIntent("reservation ID")
	}
	if !accountIDPattern.MatchString(request.OperationID) {
		return invalidIntent("operation ID")
	}
	if !accountIDPattern.MatchString(request.AccountID) {
		return invalidIntent("account ID")
	}
	if request.Sender == (common.Address{}) {
		return invalidIntent("sender")
	}
	if request.ChainID == 0 || request.ChainID > math.MaxInt64 {
		return invalidIntent("chain ID")
	}
	if request.PendingNonce > math.MaxInt64 || request.PlanGeneration == 0 || request.PlanGeneration > math.MaxInt64 {
		return invalidIntent("nonce reservation bounds")
	}
	if request.ReservedAt.IsZero() || !request.ExpiresAt.After(request.ReservedAt) {
		return invalidIntent("reservation expiry")
	}
	return nil
}
