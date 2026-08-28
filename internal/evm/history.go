package evm

import (
	"context"
	"math"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

const MaxHistoryPageSize = 100

type HistoryCursor struct {
	CreatedAt     time.Time
	TransactionID string
}

type HistoryQuery struct {
	AccountID  string
	Sender     common.Address
	ChainID    uint64
	States     []TransactionState
	Operations []Operation
	Cursor     *HistoryCursor
	Limit      int
}

type HistoryReceipt struct {
	Status            uint64
	BlockNumber       uint64
	GasUsed           uint64
	EffectiveGasPrice *big.Int
	ActualFee         *big.Int
}

type HistoryEntry struct {
	TransactionID      string
	AccountID          string
	Sender             common.Address
	ChainID            uint64
	Nonce              uint64
	Operation          Operation
	Counterparty       common.Address
	AssetContract      common.Address
	AssetAmount        *big.Int
	TokenID            *big.Int
	State              TransactionState
	LastResultCode     string
	TransactionHash    common.Hash
	BroadcastAttempts  uint64
	Receipt            *HistoryReceipt
	Confirmations      uint64
	ConfirmationTarget uint64
	ReorgCount         uint64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type HistoryPage struct {
	Entries    []HistoryEntry
	NextCursor *HistoryCursor
}

type AnalyticsQuery struct {
	AccountID string
	Sender    common.Address
	ChainID   uint64
}

type AnalyticsCount struct {
	Key   string
	Count uint64
}

type AssetAnalytics struct {
	ChainID          uint64
	Operation        Operation
	AssetContract    common.Address
	TransactionCount uint64
	Amount           *big.Int
}

type ChainFeeAnalytics struct {
	ChainID          uint64
	TransactionCount uint64
	ActualFee        *big.Int
}

type AnalyticsSnapshot struct {
	TransactionCount uint64
	ReorgCount       uint64
	States           []AnalyticsCount
	Operations       []AnalyticsCount
	Assets           []AssetAnalytics
	Fees             []ChainFeeAnalytics
}

type HistoryReader interface {
	ListTransactions(context.Context, HistoryQuery) (HistoryPage, error)
	Analytics(context.Context, AnalyticsQuery) (AnalyticsSnapshot, error)
}

func ValidateHistoryQuery(query HistoryQuery) error {
	if err := validateHistoryScope(query.AccountID, query.Sender, query.ChainID); err != nil {
		return err
	}
	if query.Limit <= 0 || query.Limit > MaxHistoryPageSize {
		return invalidIntent("history limit")
	}
	if len(query.States) > 10 || len(query.Operations) > 7 {
		return invalidIntent("history filters")
	}
	states := make(map[TransactionState]struct{}, len(query.States))
	for _, state := range query.States {
		if !validTransactionState(state) {
			return invalidIntent("history state")
		}
		if _, exists := states[state]; exists {
			return invalidIntent("duplicate history state")
		}
		states[state] = struct{}{}
	}
	operations := make(map[Operation]struct{}, len(query.Operations))
	for _, operation := range query.Operations {
		if !validHistoryOperation(operation) {
			return invalidIntent("history operation")
		}
		if _, exists := operations[operation]; exists {
			return invalidIntent("duplicate history operation")
		}
		operations[operation] = struct{}{}
	}
	if query.Cursor != nil && (query.Cursor.CreatedAt.IsZero() || ValidateTransactionID(query.Cursor.TransactionID) != nil) {
		return invalidIntent("history cursor")
	}
	return nil
}

func ValidateAnalyticsQuery(query AnalyticsQuery) error {
	return validateHistoryScope(query.AccountID, query.Sender, query.ChainID)
}

func validateHistoryScope(accountID string, sender common.Address, chainID uint64) error {
	if accountID == "" && sender == (common.Address{}) {
		return invalidIntent("history scope")
	}
	if accountID != "" && !accountIDPattern.MatchString(accountID) {
		return invalidIntent("history account ID")
	}
	if chainID > math.MaxInt64 {
		return invalidIntent("history chain ID")
	}
	return nil
}

func validTransactionState(state TransactionState) bool {
	switch state {
	case TransactionSigning, TransactionSigningFailed, TransactionBroadcasting, TransactionBroadcastFailed, TransactionSubmitted, TransactionConfirming, TransactionConfirmed, TransactionReverted, TransactionReorged, TransactionEffectUnverified:
		return true
	default:
		return false
	}
}

func validHistoryOperation(operation Operation) bool {
	switch operation {
	case OperationNativeTransfer, OperationERC20Transfer, OperationERC20Approve, OperationERC721SafeTransfer, OperationERC1155SafeTransfer, OperationERC1155BatchTransfer, OperationContractCall:
		return true
	default:
		return false
	}
}
