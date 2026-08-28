package storage

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

type historyFixture struct {
	account *wallet.Account
	rows    []evmTransactionRow
}

func createHistoryFixture(t *testing.T, repository *GORMRepository) historyFixture {
	t.Helper()
	ctx := context.Background()
	account := testAccount("11111111-1111-4111-8111-111111111111", "history-source")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	sender := common.HexToAddress(account.Address)
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	type fixtureValue struct {
		chainID   int64
		operation evm.Operation
		amount    int64
		state     evm.TransactionState
		contract  common.Address
		gasUsed   int64
		gasPrice  int64
		reorgs    int64
	}
	values := []fixtureValue{
		{chainID: 2, operation: evm.OperationERC20Approve, amount: 13, state: evm.TransactionSigningFailed, contract: contract},
		{chainID: 1, operation: evm.OperationERC20Transfer, amount: 11, state: evm.TransactionConfirmed, contract: contract, gasUsed: 50_000, gasPrice: 3},
		{chainID: 1, operation: evm.OperationERC20Transfer, amount: 7, state: evm.TransactionSubmitted, contract: contract},
		{chainID: 1, operation: evm.OperationNativeTransfer, amount: 5, state: evm.TransactionConfirmed, gasUsed: 21_000, gasPrice: 2, reorgs: 1},
		{chainID: 1, operation: evm.OperationERC721SafeTransfer, amount: 42, state: evm.TransactionConfirmed, contract: contract},
	}
	rows := make([]evmTransactionRow, 0, len(values))
	for index, value := range values {
		number := index + 1
		reservationID := fmt.Sprintf("3%07d-1111-4111-8111-%012d", number, number)
		operationID := fmt.Sprintf("4%07d-1111-4111-8111-%012d", number, number)
		approvalID := fmt.Sprintf("5%07d-1111-4111-8111-%012d", number, number)
		transactionID := fmt.Sprintf("6%07d-1111-4111-8111-%012d", number, number)
		timeOffset := index
		if index == 2 {
			timeOffset = 1
		}
		createdAt := now.Add(time.Duration(timeOffset) * time.Second).UnixMilli()
		committedAt := createdAt
		consumedAt := createdAt
		reservation := evmNonceReservationRow{
			ReservationID: reservationID, OperationID: operationID, AccountID: account.AccountID,
			SenderAddress: sender.Bytes(), ChainID: value.chainID, Nonce: int64(index), PlanGeneration: 1,
			State: string(evm.NonceCommitted), ReservedAtMS: createdAt, ExpiresAtMS: createdAt + 60_000, Revision: 1,
		}
		if err := repository.db.Create(&reservation).Error; err != nil {
			t.Fatal(err)
		}
		if err := repository.db.Exec("UPDATE evm_nonce_reservations SET committed_at_ms = ? WHERE reservation_id = ?", committedAt, reservationID).Error; err != nil {
			t.Fatal(err)
		}
		approval := evmApprovalRow{
			ApprovalID: approvalID, ReservationID: reservationID, AccountID: account.AccountID,
			SenderAddress: sender.Bytes(), ChainID: value.chainID, Nonce: int64(index), AuthorizationEpoch: 1,
			PlanHash: bytes32(byte(number)), TransactionDigest: bytes32(byte(number + 10)), RiskLevel: string(evm.RiskNormal),
			ConfirmationLevel: string(evm.ConfirmationStandard), ConfirmationTarget: 1, State: string(evm.ApprovalConsumed),
			CreatedAtMS: createdAt, ConfirmedAtMS: createdAt, ExpiresAtMS: createdAt + 60_000, ConsumedAtMS: &consumedAt, Revision: 1,
		}
		if err := repository.db.Create(&approval).Error; err != nil {
			t.Fatal(err)
		}
		row := evmTransactionRow{
			TransactionID: transactionID, ApprovalID: approvalID, ReservationID: reservationID, AccountID: account.AccountID,
			SenderAddress: sender.Bytes(), ChainID: value.chainID, Nonce: int64(index), Operation: string(value.operation),
			CounterpartyAddress: common.HexToAddress("0x2222222222222222222222222222222222222222").Bytes(), AssetContract: value.contract.Bytes(),
			AssetAmount: uint256Bytes(big.NewInt(value.amount)), PlanHash: bytes32(byte(number)), TransactionDigest: bytes32(byte(number + 10)),
			State: string(value.state), ConfirmationTarget: 1, ReorgCount: value.reorgs, CreatedAtMS: createdAt, UpdatedAtMS: createdAt, Revision: 1,
		}
		if value.state != evm.TransactionSigningFailed {
			broadcastAt := createdAt
			row.SignedPayload = []byte{byte(number)}
			row.TransactionHash = common.BigToHash(big.NewInt(int64(number))).Bytes()
			row.BroadcastAttempts = 1
			row.FirstBroadcastAtMS = &broadcastAt
			row.LastBroadcastAtMS = &broadcastAt
		}
		if value.gasUsed > 0 {
			status := int64(1)
			blockNumber := int64(100 + index)
			txIndex := int64(index)
			row.ReceiptStatus = &status
			row.ReceiptBlockNumber = &blockNumber
			row.ReceiptBlockHash = common.BigToHash(big.NewInt(blockNumber)).Bytes()
			row.ReceiptTxIndex = &txIndex
			row.ReceiptGasUsed = &value.gasUsed
			row.EffectiveGasPrice = uint256Bytes(big.NewInt(value.gasPrice))
			row.Confirmations = 1
		}
		if err := repository.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return historyFixture{account: account, rows: rows}
}

func bytes32(value byte) []byte {
	result := make([]byte, 32)
	result[31] = value
	return result
}

func TestHistoryReaderPaginatesFiltersAndNeverExposesSignedPayload(t *testing.T) {
	repository := newAccountTestRepository(t)
	fixture := createHistoryFixture(t, repository)
	if err := repository.db.Model(&evmTransactionRow{}).Where("transaction_id = ?", fixture.rows[0].TransactionID).Update("created_at_ms", fixture.rows[0].CreatedAtMS+1).Error; err == nil {
		t.Fatal("database allowed immutable history cursor key mutation")
	}
	sender := common.HexToAddress(fixture.account.Address)
	page, err := repository.ListTransactions(context.Background(), evm.HistoryQuery{Sender: sender, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].TransactionID != fixture.rows[4].TransactionID || page.Entries[1].TransactionID != fixture.rows[3].TransactionID || page.NextCursor == nil {
		t.Fatalf("unexpected first history page: %+v", page)
	}
	if page.Entries[1].Receipt == nil || page.Entries[1].Receipt.ActualFee.Cmp(big.NewInt(42_000)) != 0 {
		t.Fatalf("actual fee was not derived safely: %+v", page.Entries[1].Receipt)
	}
	second, err := repository.ListTransactions(context.Background(), evm.HistoryQuery{Sender: sender, Limit: 2, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 2 || second.Entries[0].TransactionID != fixture.rows[2].TransactionID || second.Entries[1].TransactionID != fixture.rows[1].TransactionID || second.NextCursor == nil {
		t.Fatalf("unexpected second history page: %+v", second)
	}
	third, err := repository.ListTransactions(context.Background(), evm.HistoryQuery{Sender: sender, Limit: 2, Cursor: second.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(third.Entries) != 1 || third.Entries[0].TransactionID != fixture.rows[0].TransactionID || third.NextCursor != nil {
		t.Fatalf("unexpected final history page: %+v", third)
	}
	filtered, err := repository.ListTransactions(context.Background(), evm.HistoryQuery{AccountID: fixture.account.AccountID, Sender: sender, ChainID: 1, States: []evm.TransactionState{evm.TransactionConfirmed}, Limit: 10})
	if err != nil || len(filtered.Entries) != 3 {
		t.Fatalf("history filters failed: %+v %v", filtered, err)
	}
	alias := testAccount("22222222-2222-4222-8222-222222222222", "history-alias")
	alias.State = wallet.AccountStateActive
	if err := repository.CreateAccount(context.Background(), alias); err != nil {
		t.Fatal(err)
	}
	aliasOnly, err := repository.ListTransactions(context.Background(), evm.HistoryQuery{AccountID: alias.AccountID, Limit: 10})
	if err != nil || len(aliasOnly.Entries) != 0 {
		t.Fatalf("account-scoped history leaked same-address alias records: %+v %v", aliasOnly, err)
	}
	if _, exists := reflect.TypeOf(evm.HistoryEntry{}).FieldByName("SignedPayload"); exists {
		t.Fatal("history DTO exposes signed payload")
	}
	if strings.Contains(historySelectColumns, "signed_payload") || strings.Contains(historySelectColumns, "transaction_digest") || strings.Contains(historySelectColumns, "plan_hash") {
		t.Fatal("history projection selects signing material")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.ListTransactions(cancelled, evm.HistoryQuery{Sender: sender, Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled history query returned %v", err)
	}
}

func TestHistoryAnalyticsSeparatesAssetsOperationsAndChainFees(t *testing.T) {
	repository := newAccountTestRepository(t)
	fixture := createHistoryFixture(t, repository)
	snapshot, err := repository.Analytics(context.Background(), evm.AnalyticsQuery{Sender: common.HexToAddress(fixture.account.Address)})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TransactionCount != 5 || snapshot.ReorgCount != 1 {
		t.Fatalf("unexpected analytics totals: %+v", snapshot)
	}
	if len(snapshot.Assets) != 4 || snapshot.Assets[0].Amount == nil || len(snapshot.Fees) != 1 || snapshot.Fees[0].ChainID != 1 || snapshot.Fees[0].TransactionCount != 2 || snapshot.Fees[0].ActualFee.Cmp(big.NewInt(192_000)) != 0 {
		t.Fatalf("analytics mixed assets or fees: %+v", snapshot)
	}
	var transferAmount *big.Int
	var nftAsset *evm.AssetAnalytics
	for _, asset := range snapshot.Assets {
		if asset.Operation == evm.OperationERC20Transfer {
			transferAmount = asset.Amount
		}
		if asset.Operation == evm.OperationERC721SafeTransfer {
			nftAsset = &asset
		}
	}
	if transferAmount == nil || transferAmount.Cmp(big.NewInt(18)) != 0 {
		t.Fatalf("ERC-20 transfer aggregate is incorrect: %v", transferAmount)
	}
	if nftAsset == nil || nftAsset.TransactionCount != 1 || nftAsset.Amount.Sign() != 0 {
		t.Fatalf("ERC-721 analytics summed token IDs as amounts: %+v", nftAsset)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repository.Analytics(cancelled, evm.AnalyticsQuery{Sender: common.HexToAddress(fixture.account.Address)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled analytics query returned %v", err)
	}
}
