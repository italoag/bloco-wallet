package ui

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/pkg/localization"

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
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", localization.Get("hist_scroll_up"))),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", localization.Get("hist_scroll_down"))),
		PageUp:     key.NewBinding(key.WithKeys("pgup", "u"), key.WithHelp("pgup/u", localization.Get("hist_page_up"))),
		PageDown:   key.NewBinding(key.WithKeys("pgdown", "d"), key.WithHelp("pgdn/d", localization.Get("hist_page_down"))),
		Next:       key.NewBinding(key.WithKeys("n"), key.WithHelp("n", localization.Get("hist_next"))),
		Previous:   key.NewBinding(key.WithKeys("p"), key.WithHelp("p", localization.Get("hist_previous"))),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", localization.Get("hist_refresh"))),
		ToggleHelp: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", localization.Get("hist_more_help"))),
		Back:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", localization.Get("hist_back"))),
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
		return localization.Get("hist_unavailable")
	}
	var content strings.Builder
	content.WriteString(lipgloss.NewStyle().Bold(true).Render(localization.Get("hist_title")))
	content.WriteString("\n" + localization.Get("hist_subtitle") + "\n")
	_, _ = content.WriteString(localization.T("hist_address_page", map[string]interface{}{"Address": state.sender.Hex(), "Page": state.pageNumber}))
	if state.loading {
		content.WriteString("\n\n" + localization.Get("hist_loading"))
		return content.String()
	}
	if state.err != "" {
		content.WriteString("\n\n" + localization.T("hist_unavailable_detail", map[string]interface{}{"Error": safeInline(state.err)}))
		return content.String()
	}
	_, _ = content.WriteString("\n\n" + localization.T("hist_analytics_line", map[string]interface{}{"Tx": state.analytics.TransactionCount, "Reorgs": state.analytics.ReorgCount}))
	if len(state.analytics.States) > 0 {
		content.WriteString("\n" + localization.Get("hist_states_prefix"))
		content.WriteString(formatAnalyticsCounts(state.analytics.States))
	}
	if len(state.analytics.Operations) > 0 {
		content.WriteString("\n" + localization.Get("hist_operations_prefix"))
		content.WriteString(formatAnalyticsCounts(state.analytics.Operations))
	}
	for _, fee := range state.analytics.Fees {
		_, _ = content.WriteString("\n" + localization.T("hist_fee_line", map[string]interface{}{"Chain": fee.ChainID, "Fee": historyAmountString(fee.ActualFee), "Count": fee.TransactionCount}))
	}
	if len(state.analytics.Assets) > 0 {
		content.WriteString("\n" + localization.Get("hist_assets_header"))
		for _, asset := range state.analytics.Assets {
			assetName := asset.AssetContract.Hex()
			if asset.AssetContract == (common.Address{}) {
				assetName = localization.Get("hist_asset_native")
			}
			_, _ = content.WriteString("\n" + localization.T("hist_asset_line", map[string]interface{}{"Chain": asset.ChainID, "Op": safeShort(string(asset.Operation)), "Asset": assetName, "Amount": historyAmountString(asset.Amount), "Count": asset.TransactionCount}))
		}
	}
	content.WriteString("\n\n" + localization.Get("hist_tx_header"))
	if len(state.page.Entries) == 0 {
		content.WriteString("\n" + localization.Get("hist_no_transactions"))
		return content.String()
	}
	for _, entry := range state.page.Entries {
		_, _ = fmt.Fprintf(&content, "\n\n%s", localization.T("hist_entry_line", map[string]interface{}{"When": entry.CreatedAt.Format("2006-01-02 15:04:05"), "Chain": entry.ChainID, "Op": safeShort(string(entry.Operation)), "State": safeShort(string(entry.State))}))
		if entry.Operation == evm.OperationERC721SafeTransfer {
			_, _ = content.WriteString("\n" + localization.T("hist_token_line", map[string]interface{}{"ID": historyAmountString(entry.AssetAmount), "Contract": historyAssetLabel(entry.AssetContract)}))
		} else {
			_, _ = content.WriteString("\n" + localization.T("hist_amount_line", map[string]interface{}{"Amount": historyAmountString(entry.AssetAmount), "Asset": historyAssetLabel(entry.AssetContract)}))
		}
		_, _ = content.WriteString("\n" + localization.T("hist_to_line", map[string]interface{}{"To": entry.Counterparty.Hex(), "Nonce": entry.Nonce}))
		if entry.TransactionHash != (common.Hash{}) {
			content.WriteString("\n" + localization.Get("hist_tx_prefix") + entry.TransactionHash.Hex())
		}
		if entry.Receipt != nil {
			_, _ = content.WriteString("\n" + localization.T("hist_receipt_line", map[string]interface{}{"Block": entry.Receipt.BlockNumber, "Fee": historyAmountString(entry.Receipt.ActualFee), "Conf": entry.Confirmations, "Target": entry.ConfirmationTarget}))
		}
		if entry.LastResultCode != "" {
			content.WriteString("\n" + localization.Get("hist_result_prefix") + safeInline(entry.LastResultCode))
		}
		if entry.ReorgCount > 0 {
			_, _ = content.WriteString("\n" + localization.T("hist_reorg_line", map[string]interface{}{"Count": entry.ReorgCount}))
		}
	}
	return content.String()
}

func (model *CLIModel) viewAccountHistory() string {
	if model.accountHistory == nil {
		return localization.Get("hist_unavailable")
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
		return localization.Get("hist_amount_unavailable")
	}
	return amount.String()
}

func historyAssetLabel(contract common.Address) string {
	if contract == (common.Address{}) {
		return localization.Get("hist_asset_native")
	}
	return contract.Hex()
}
