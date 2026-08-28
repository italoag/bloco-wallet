package ui

import (
	"strings"
	"testing"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

func newMainShellLayoutModel(t *testing.T, view string) *CLIModel {
	t.Helper()
	previousLabels := localization.Labels
	localization.Labels = map[string]string{
		"version":              "0.2.0",
		"main_menu_title":      "Main Menu",
		"list_wallets":         "My Wallets",
		"list_wallets_title":   "My Wallets",
		"wallet_details_title": "Wallet Details",
	}
	t.Cleanup(func() { localization.Labels = previousLabels })
	styles := createStyles()
	styles.Header = styles.Header.Width(160)
	styles.Content = styles.Content.Width(160)
	styles.Footer = styles.Footer.Width(160)
	model := &CLIModel{
		currentView: view,
		menuItems: []menuItem{
			{title: "Create New", description: "Generate a wallet"},
			{title: "Import Wallet", description: "Import a wallet"},
			{title: "My Wallets", description: "View and manage wallets"},
			{title: "Configuration", description: "Configure settings"},
			{title: "Exit", description: "Exit the application"},
		},
		selectedMenu: 2,
		walletCount:  1,
		width:        160,
		height:       50,
		displayTime:  time.Date(2026, 8, 27, 19, 0, 0, 0, time.UTC),
		styles:       styles,
	}
	return model
}

func TestWalletListRendersInsideMainShellWithoutVerticalOverflow(t *testing.T) {
	model := newMainShellLayoutModel(t, constants.ListWalletsView)
	model.accounts = []wallet.AccountSummary{{Name: "DAB"}}
	model.walletTable = table.New(
		table.WithColumns([]table.Column{{Title: "Name", Width: 20}, {Title: "Address", Width: 42}}),
		table.WithRows([]table.Row{{"DAB", "0x63E8328D2aED963Ae1407EfDF1125aF2DCE3A92B"}}),
		table.WithHeight(45),
	)
	view := model.View()
	if lipgloss.Height(view) > model.height {
		t.Fatalf("wallet list overflowed terminal: rendered=%d terminal=%d", lipgloss.Height(view), model.height)
	}
	headerIndex := strings.Index(view, "Create New")
	tableIndex := strings.Index(view, "DAB")
	if headerIndex < 0 || tableIndex < 0 || headerIndex > tableIndex {
		t.Fatalf("wallet table did not replace main content below the persistent header: %q", view)
	}
}

func TestMainShellUsesBoundedCompactFallback(t *testing.T) {
	model := newMainShellLayoutModel(t, constants.ListWalletsView)
	model.width = 80
	model.height = 24
	model.styles.Header = model.styles.Header.Width(model.width)
	model.styles.Content = model.styles.Content.Width(model.width)
	model.styles.Footer = model.styles.Footer.Width(model.width)
	view := model.View()
	if lipgloss.Height(view) > model.height || lipgloss.Width(view) > model.width || !strings.Contains(view, "Terminal too small") {
		t.Fatalf("compact shell is not bounded to 80x24: %dx%d %q", lipgloss.Width(view), lipgloss.Height(view), view)
	}
}

func TestAccountTablePreservesFullAddressAtCommonWidths(t *testing.T) {
	account := wallet.AccountSummary{
		AccountID: "60f52053-13d7-4517-8289-2d3d8212d342", Name: "DAB",
		Address: "0x63E8328D2aED963Ae1407EfDF1125aF2DCE3A92B", SignerKind: wallet.SignerKindWatchOnly, State: wallet.AccountStateActive,
	}
	for _, width := range []int{80, 120} {
		columns, rows := accountTableLayout(width, []wallet.AccountSummary{account})
		if len(columns) < 4 || columns[len(columns)-1].Width != 42 || len(rows) != 1 || rows[0][len(rows[0])-1] != account.Address || rows[0][0] != account.AccountID {
			t.Fatalf("account table lost identity or address at width %d: columns=%+v rows=%+v", width, columns, rows)
		}
	}
}

func TestWalletDetailsRenderInsideMainShellWithoutVerticalOverflow(t *testing.T) {
	model := newMainShellLayoutModel(t, constants.WalletDetailsView)
	model.selectedAccount = &wallet.AccountSummary{
		AccountID:  "60f52053-13d7-4517-8289-2d3d8212d342",
		Name:       "DAB",
		Address:    "0x63E8328D2aED963Ae1407EfDF1125aF2DCE3A92B",
		SignerKind: wallet.SignerKindSoftware,
		State:      wallet.AccountStateActive,
	}
	model.initWalletDetailsComponents()
	view := model.View()
	if lipgloss.Height(view) > model.height {
		t.Fatalf("wallet details overflowed terminal: rendered=%d terminal=%d", lipgloss.Height(view), model.height)
	}
	headerIndex := strings.Index(view, "Create New")
	detailsIndex := strings.Index(view, "Wallet Details")
	if headerIndex < 0 || detailsIndex < 0 || headerIndex > detailsIndex {
		t.Fatalf("wallet details did not remain below the persistent header: %q", view)
	}
}

func TestWalletDetailsRemainBoundedAt120x30(t *testing.T) {
	model := newMainShellLayoutModel(t, constants.WalletDetailsView)
	model.width = 120
	model.height = 30
	model.styles.Header = model.styles.Header.Width(model.width)
	model.styles.Content = model.styles.Content.Width(model.width)
	model.styles.Footer = model.styles.Footer.Width(model.width)
	model.selectedAccount = &wallet.AccountSummary{
		AccountID: "60f52053-13d7-4517-8289-2d3d8212d342", Name: "DAB",
		Address: "0x63E8328D2aED963Ae1407EfDF1125aF2DCE3A92B", SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
	}
	model.initWalletDetailsComponents()
	view := model.View()
	if lipgloss.Height(view) > model.height || lipgloss.Width(view) > model.width || !strings.Contains(view, "Create New") || !strings.Contains(view, "DAB") {
		t.Fatalf("120x30 wallet details overflowed or lost shell content: %dx%d %q", lipgloss.Width(view), lipgloss.Height(view), view)
	}
}
