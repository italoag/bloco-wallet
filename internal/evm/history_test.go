package evm

import (
	"math"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestValidateHistoryQuery(t *testing.T) {
	valid := HistoryQuery{Sender: common.HexToAddress("0x1111111111111111111111111111111111111111"), Limit: 25}
	if err := ValidateHistoryQuery(valid); err != nil {
		t.Fatalf("valid history query was rejected: %v", err)
	}
	valid.AccountID = "11111111-1111-4111-8111-111111111111"
	valid.ChainID = 1
	valid.States = []TransactionState{TransactionConfirmed}
	valid.Operations = []Operation{OperationNativeTransfer}
	valid.Cursor = &HistoryCursor{CreatedAt: time.Now().UTC(), TransactionID: "61111111-1111-4111-8111-111111111111"}
	if err := ValidateHistoryQuery(valid); err != nil {
		t.Fatalf("fully filtered history query was rejected: %v", err)
	}
	invalid := []HistoryQuery{
		{Limit: 25},
		{Sender: valid.Sender, Limit: 0},
		{Sender: valid.Sender, Limit: MaxHistoryPageSize + 1},
		{AccountID: "invalid", Limit: 1},
		{Sender: valid.Sender, ChainID: uint64(math.MaxInt64) + 1, Limit: 1},
		{Sender: valid.Sender, Limit: 1, States: []TransactionState{"unknown"}},
		{Sender: valid.Sender, Limit: 1, States: []TransactionState{TransactionConfirmed, TransactionConfirmed}},
		{Sender: valid.Sender, Limit: 1, Operations: []Operation{"unknown"}},
		{Sender: valid.Sender, Limit: 1, Operations: []Operation{OperationNativeTransfer, OperationNativeTransfer}},
		{Sender: valid.Sender, Limit: 1, Cursor: &HistoryCursor{}},
	}
	for _, query := range invalid {
		if err := ValidateHistoryQuery(query); err == nil {
			t.Fatalf("invalid history query was accepted: %+v", query)
		}
	}
}

func TestValidateAnalyticsQuery(t *testing.T) {
	valid := AnalyticsQuery{Sender: common.HexToAddress("0x1111111111111111111111111111111111111111")}
	if err := ValidateAnalyticsQuery(valid); err != nil {
		t.Fatalf("valid analytics query was rejected: %v", err)
	}
	for _, query := range []AnalyticsQuery{{}, {AccountID: "invalid"}, {Sender: valid.Sender, ChainID: uint64(math.MaxInt64) + 1}} {
		if err := ValidateAnalyticsQuery(query); err == nil {
			t.Fatalf("invalid analytics query was accepted: %+v", query)
		}
	}
}
