package ui

import (
	"context"
	"math/big"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/common"
)

type fakeHistoryReader struct {
	queries        []evm.HistoryQuery
	analyticsCalls int
}

func (reader *fakeHistoryReader) ListTransactions(_ context.Context, query evm.HistoryQuery) (evm.HistoryPage, error) {
	reader.queries = append(reader.queries, query)
	entry := evm.HistoryEntry{
		TransactionID: "61111111-1111-4111-8111-111111111111", AccountID: "11111111-1111-4111-8111-111111111111",
		Sender: common.HexToAddress("0x1111111111111111111111111111111111111111"), ChainID: 1, Nonce: 7,
		Operation: evm.OperationNativeTransfer, Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"),
		AssetAmount: big.NewInt(5), State: evm.TransactionConfirmed, TransactionHash: common.HexToHash("0x01"),
		Receipt: &evm.HistoryReceipt{BlockNumber: 10, ActualFee: big.NewInt(42_000)}, Confirmations: 2, ConfirmationTarget: 2,
		CreatedAt: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
	if query.Cursor != nil {
		entry.TransactionID = "62222222-2222-4222-8222-222222222222"
		entry.CreatedAt = entry.CreatedAt.Add(-time.Second)
		return evm.HistoryPage{Entries: []evm.HistoryEntry{entry}}, nil
	}
	return evm.HistoryPage{
		Entries:    []evm.HistoryEntry{entry},
		NextCursor: &evm.HistoryCursor{CreatedAt: entry.CreatedAt, TransactionID: entry.TransactionID},
	}, nil
}

func (reader *fakeHistoryReader) Analytics(context.Context, evm.AnalyticsQuery) (evm.AnalyticsSnapshot, error) {
	reader.analyticsCalls++
	return evm.AnalyticsSnapshot{
		TransactionCount: 1,
		States:           []evm.AnalyticsCount{{Key: string(evm.TransactionConfirmed), Count: 1}},
		Operations:       []evm.AnalyticsCount{{Key: string(evm.OperationNativeTransfer), Count: 1}},
		Assets:           []evm.AssetAnalytics{{ChainID: 1, Operation: evm.OperationNativeTransfer, TransactionCount: 1, Amount: big.NewInt(5)}},
		Fees:             []evm.ChainFeeAnalytics{{ChainID: 1, TransactionCount: 1, ActualFee: big.NewInt(42_000)}},
	}, nil
}

func TestAccountHistoryLoadsPaginatesAndKeepsViewPure(t *testing.T) {
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet", "version": "0.2.0", "main_menu_title": "Main Menu"}
	t.Cleanup(func() { localization.Labels = previousLabels })
	reader := &fakeHistoryReader{}
	model := &CLIModel{
		width: 120, height: 30, styles: createStyles(),
		selectedAccount: &wallet.AccountSummary{
			AccountID: "11111111-1111-4111-8111-111111111111", Name: "Observer",
			Address: "0x1111111111111111111111111111111111111111", SignerKind: wallet.SignerKindWatchOnly, State: wallet.AccountStateActive,
		},
	}
	model.ConfigureHistoryReader(reader)
	if !model.walletDetailsKeys.History.Enabled() {
		t.Fatal("local history was not enabled for watch-only account")
	}
	_, command := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if command == nil || model.currentView != constants.AccountHistoryView {
		t.Fatal("history key did not open account history")
	}
	before := len(reader.queries)
	_ = model.viewAccountHistory()
	if len(reader.queries) != before {
		t.Fatal("account history View performed repository I/O")
	}
	_, _ = model.Update(command())
	view := model.viewAccountHistory()
	for _, expected := range []string{"not complete on-chain history", "confirmed=1", "42000 wei", "0x2222222222222222222222222222222222222222"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("history view omitted %q: %q", expected, view)
		}
	}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if command == nil {
		t.Fatal("next history page did not start a query")
	}
	_, _ = model.Update(command())
	if len(reader.queries) != 2 || reader.queries[1].Cursor == nil || model.accountHistory.pageNumber != 2 {
		t.Fatalf("history pagination did not bind cursor: %+v", reader.queries)
	}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if command == nil {
		t.Fatal("previous history page did not start a query")
	}
	_, _ = model.Update(command())
	if model.accountHistory.pageNumber != 1 || len(reader.queries) != 3 || reader.queries[2].Cursor != nil {
		t.Fatalf("history previous-page cursor stack is incorrect: %+v", reader.queries)
	}
}

func TestAccountHistoryIgnoresStaleResultAfterBack(t *testing.T) {
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet"}
	t.Cleanup(func() { localization.Labels = previousLabels })
	reader := &fakeHistoryReader{}
	model := &CLIModel{width: 120, height: 30, styles: createStyles(), historyReader: reader, currentView: constants.WalletDetailsView, selectedAccount: &wallet.AccountSummary{
		AccountID: "11111111-1111-4111-8111-111111111111", Address: "0x1111111111111111111111111111111111111111", State: wallet.AccountStateActive,
	}}
	command := model.initAccountHistory()
	if command == nil {
		t.Fatal("history load did not start")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.currentView != constants.WalletDetailsView || model.accountHistory != nil {
		t.Fatal("history back navigation did not restore wallet details")
	}
	_, _ = model.Update(command())
	if model.currentView != constants.WalletDetailsView || model.accountHistory != nil {
		t.Fatal("stale history result changed the restored view")
	}
}
