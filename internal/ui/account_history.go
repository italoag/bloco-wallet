package ui

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ethereum/go-ethereum/common"
)

const accountHistoryPageSize = 20

type accountHistoryKeyMap struct {
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Next       key.Binding
	Previous   key.Binding
	Refresh    key.Binding
	ToggleHelp key.Binding
	Back       key.Binding
}

func newAccountHistoryKeyMap() accountHistoryKeyMap {
	return accountHistoryKeyMap{
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
		PageUp:     key.NewBinding(key.WithKeys("pgup", "u"), key.WithHelp("pgup/u", "page up")),
		PageDown:   key.NewBinding(key.WithKeys("pgdown", "d"), key.WithHelp("pgdn/d", "page down")),
		Next:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next results")),
		Previous:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous results")),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		ToggleHelp: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more help")),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (keys accountHistoryKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Next, keys.Previous, keys.Refresh, keys.ToggleHelp, keys.Back}
}

func (keys accountHistoryKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Up, keys.Down, keys.PageUp, keys.PageDown}, {keys.Next, keys.Previous, keys.Refresh, keys.ToggleHelp, keys.Back}}
}

type accountHistoryState struct {
	accountID       string
	sender          common.Address
	page            evm.HistoryPage
	analytics       evm.AnalyticsSnapshot
	cursor          *evm.HistoryCursor
	previousCursors []*evm.HistoryCursor
	pageNumber      int
	generation      uint64
	loading         bool
	cancel          context.CancelFunc
	err             string
	viewport        viewport.Model
	help            help.Model
	keys            accountHistoryKeyMap
}

type accountHistoryLoadedMsg struct {
	accountID  string
	generation uint64
	page       evm.HistoryPage
	analytics  evm.AnalyticsSnapshot
	err        error
}

func (model *CLIModel) ConfigureHistoryReader(reader evm.HistoryReader) {
	model.clearAccountHistory()
	model.historyReader = reader
	if model.selectedAccount != nil {
		model.refreshWalletDetailsComponents()
	}
}

func (model *CLIModel) initAccountHistory() tea.Cmd {
	if model.historyReader == nil || model.selectedAccount == nil || !common.IsHexAddress(model.selectedAccount.Address) {
		return nil
	}
	width := max(40, model.width-6)
	height := max(8, model.height-12)
	state := &accountHistoryState{
		accountID:  model.selectedAccount.AccountID,
		sender:     common.HexToAddress(model.selectedAccount.Address),
		pageNumber: 1,
		viewport:   viewport.New(width, height),
		help:       help.New(),
		keys:       newAccountHistoryKeyMap(),
	}
	state.viewport.Style = lipgloss.NewStyle().Padding(0, 1)
	state.help.Width = width
	model.accountHistory = state
	model.currentView = constants.AccountHistoryView
	return model.startAccountHistoryLoad()
}

func (model *CLIModel) startAccountHistoryLoad() tea.Cmd {
	state := model.accountHistory
	if state == nil || model.historyReader == nil {
		return nil
	}
	if state.cancel != nil {
		state.cancel()
	}
	model.historyGeneration++
	state.generation = model.historyGeneration
	generation := state.generation
	accountID := state.accountID
	sender := state.sender
	cursor := cloneHistoryCursor(state.cursor)
	reader := model.historyReader
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	state.cancel = cancel
	state.loading = true
	state.err = ""
	model.refreshAccountHistoryContent()
	return func() tea.Msg {
		defer cancel()
		page, err := reader.ListTransactions(ctx, evm.HistoryQuery{Sender: sender, Cursor: cursor, Limit: accountHistoryPageSize})
		if err != nil {
			return accountHistoryLoadedMsg{accountID: accountID, generation: generation, err: err}
		}
		analytics, err := reader.Analytics(ctx, evm.AnalyticsQuery{Sender: sender})
		return accountHistoryLoadedMsg{accountID: accountID, generation: generation, page: page, analytics: analytics, err: err}
	}
}

func (model *CLIModel) updateAccountHistory(message tea.Msg) (tea.Model, tea.Cmd) {
	state := model.accountHistory
	if state == nil {
		model.currentView = constants.WalletDetailsView
		return model, nil
	}
	switch message := message.(type) {
	case accountHistoryLoadedMsg:
		if message.accountID != state.accountID || message.generation != state.generation {
			return model, nil
		}
		state.loading = false
		state.cancel = nil
		if message.err != nil {
			state.err = safeError(message.err)
		} else {
			state.page = message.page
			state.analytics = message.analytics
			state.err = ""
		}
		model.refreshAccountHistoryContent()
		return model, nil
	case tea.KeyMsg:
		if key.Matches(message, state.keys.ToggleHelp) {
			state.help.ShowAll = !state.help.ShowAll
			return model, nil
		}
		if key.Matches(message, state.keys.Up) || key.Matches(message, state.keys.Down) || key.Matches(message, state.keys.PageUp) || key.Matches(message, state.keys.PageDown) {
			var command tea.Cmd
			state.viewport, command = state.viewport.Update(message)
			return model, command
		}
		if state.loading {
			return model, nil
		}
		switch {
		case key.Matches(message, state.keys.Next):
			if state.page.NextCursor == nil {
				return model, nil
			}
			state.previousCursors = append(state.previousCursors, cloneHistoryCursor(state.cursor))
			state.cursor = cloneHistoryCursor(state.page.NextCursor)
			state.pageNumber++
			state.viewport.GotoTop()
			return model, model.startAccountHistoryLoad()
		case key.Matches(message, state.keys.Previous):
			if len(state.previousCursors) == 0 {
				return model, nil
			}
			last := len(state.previousCursors) - 1
			state.cursor = cloneHistoryCursor(state.previousCursors[last])
			state.previousCursors = state.previousCursors[:last]
			state.pageNumber = max(1, state.pageNumber-1)
			state.viewport.GotoTop()
			return model, model.startAccountHistoryLoad()
		case key.Matches(message, state.keys.Refresh):
			state.viewport.GotoTop()
			return model, model.startAccountHistoryLoad()
		}
	}
	return model, nil
}

func (model *CLIModel) clearAccountHistory() {
	model.historyGeneration++
	if model.accountHistory != nil && model.accountHistory.cancel != nil {
		model.accountHistory.cancel()
	}
	model.accountHistory = nil
}

func (model *CLIModel) refreshAccountHistoryContent() {
	if model.accountHistory == nil {
		return
	}
	model.accountHistory.keys.Next.SetEnabled(!model.accountHistory.loading && model.accountHistory.page.NextCursor != nil)
	model.accountHistory.keys.Previous.SetEnabled(!model.accountHistory.loading && len(model.accountHistory.previousCursors) > 0)
	model.accountHistory.keys.Refresh.SetEnabled(!model.accountHistory.loading)
	model.accountHistory.viewport.SetContent(model.accountHistoryContent())
}

func (model *CLIModel) accountHistoryContent() string {
	state := model.accountHistory
	if state == nil {
		return "Local history is unavailable."
	}
	var content strings.Builder
	content.WriteString(lipgloss.NewStyle().Bold(true).Render("Local EVM Activity"))
	content.WriteString("\nOutgoing transactions created by this wallet for the selected address. This is not complete on-chain history.\n")
	_, _ = fmt.Fprintf(&content, "Address: %s\nPage: %d", state.sender.Hex(), state.pageNumber)
	if state.loading {
		content.WriteString("\n\nLoading local history and analytics...")
		return content.String()
	}
	if state.err != "" {
		content.WriteString("\n\nHistory unavailable: " + safeInline(state.err) + "\nPress r to retry.")
		return content.String()
	}
	_, _ = fmt.Fprintf(&content, "\n\nAnalytics\nTransactions: %d | Reorgs observed: %d", state.analytics.TransactionCount, state.analytics.ReorgCount)
	if len(state.analytics.States) > 0 {
		content.WriteString("\nStates: ")
		content.WriteString(formatAnalyticsCounts(state.analytics.States))
	}
	if len(state.analytics.Operations) > 0 {
		content.WriteString("\nOperations: ")
		content.WriteString(formatAnalyticsCounts(state.analytics.Operations))
	}
	for _, fee := range state.analytics.Fees {
		_, _ = fmt.Fprintf(&content, "\nChain %d receipt fees: %s wei across %d transactions", fee.ChainID, historyAmountString(fee.ActualFee), fee.TransactionCount)
	}
	if len(state.analytics.Assets) > 0 {
		content.WriteString("\nIntent amounts by chain, operation, and asset:")
		for _, asset := range state.analytics.Assets {
			assetName := asset.AssetContract.Hex()
			if asset.AssetContract == (common.Address{}) {
				assetName = "native"
			}
			_, _ = fmt.Fprintf(&content, "\n  Chain %d | %s | %s | amount %s | %d records", asset.ChainID, safeShort(string(asset.Operation)), assetName, historyAmountString(asset.Amount), asset.TransactionCount)
		}
	}
	content.WriteString("\n\nTransactions")
	if len(state.page.Entries) == 0 {
		content.WriteString("\nNo local outgoing transactions were found for this address.")
		return content.String()
	}
	for _, entry := range state.page.Entries {
		_, _ = fmt.Fprintf(&content, "\n\n%s | Chain %d | %s | %s", entry.CreatedAt.Format("2006-01-02 15:04:05"), entry.ChainID, safeShort(string(entry.Operation)), safeShort(string(entry.State)))
		if entry.Operation == evm.OperationERC721SafeTransfer {
			_, _ = fmt.Fprintf(&content, "\n  Token ID: %s | Contract: %s", historyAmountString(entry.AssetAmount), historyAssetLabel(entry.AssetContract))
		} else {
			_, _ = fmt.Fprintf(&content, "\n  Amount: %s | Asset: %s", historyAmountString(entry.AssetAmount), historyAssetLabel(entry.AssetContract))
		}
		_, _ = fmt.Fprintf(&content, "\n  To: %s | Nonce: %d", entry.Counterparty.Hex(), entry.Nonce)
		if entry.TransactionHash != (common.Hash{}) {
			content.WriteString("\n  Tx: " + entry.TransactionHash.Hex())
		}
		if entry.Receipt != nil {
			_, _ = fmt.Fprintf(&content, "\n  Receipt: block %d | fee %s wei | confirmations %d/%d", entry.Receipt.BlockNumber, historyAmountString(entry.Receipt.ActualFee), entry.Confirmations, entry.ConfirmationTarget)
		}
		if entry.LastResultCode != "" {
			content.WriteString("\n  Result: " + safeInline(entry.LastResultCode))
		}
		if entry.ReorgCount > 0 {
			_, _ = fmt.Fprintf(&content, "\n  Reorg observations: %d", entry.ReorgCount)
		}
	}
	return content.String()
}

func (model *CLIModel) viewAccountHistory() string {
	if model.accountHistory == nil {
		return "Local history is unavailable."
	}
	return model.accountHistory.viewport.View() + "\n" + model.accountHistory.help.View(model.accountHistory.keys)
}

func cloneHistoryCursor(cursor *evm.HistoryCursor) *evm.HistoryCursor {
	if cursor == nil {
		return nil
	}
	cloned := *cursor
	return &cloned
}

func formatAnalyticsCounts(counts []evm.AnalyticsCount) string {
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", safeShort(count.Key), count.Count))
	}
	return strings.Join(parts, ", ")
}

func historyAmountString(amount *big.Int) string {
	if amount == nil {
		return "unavailable"
	}
	return amount.String()
}

func historyAssetLabel(contract common.Address) string {
	if contract == (common.Address{}) {
		return "native"
	}
	return contract.Hex()
}
