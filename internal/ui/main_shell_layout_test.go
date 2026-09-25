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

func TestMainShellVersionFallsBackToDev(t *testing.T) {
	model := newMainShellLayoutModel(t, constants.DefaultView)
	view := model.View()
	if !strings.Contains(view, "Version: dev") {
		t.Fatalf("default view did not render dev version: %q", view)
	}
	if strings.Contains(view, "0.2.0") {
		t.Fatalf("stale locale version leaked into view: %q", view)
	}
}

func TestMainShellVersionTable(t *testing.T) {
	cases := map[string]string{
		"v0.5.0":                 "v0.5.0",
		"v0.6.0-rc.1":            "v0.6.0-rc.1",
		"v0.6.0-2-gabcdef-dirty": "v0.6.0-2-gabcdef-dirty",
		"0.6.0":                  "0.6.0",
		"dev":                    "dev",
		"":                       "dev",
		"   ":                    "dev",
	}
	for input, expected := range cases {
		model := newMainShellLayoutModel(t, constants.DefaultView)
		model.ConfigureVersion(input)
		main := model.View()
		if !strings.Contains(main, "Version: "+expected) {
			t.Fatalf("main view for %q missing %q: %q", input, expected, main)
		}
		if strings.Contains(main, "0.2.0") || strings.Contains(main, "vv") {
			t.Fatalf("main view for %q leaked stale or double-prefixed version: %q", input, main)
		}
		model.currentView = constants.ListWalletsView
		list := model.View()
		if !strings.Contains(list, "Version: "+expected) {
			t.Fatalf("list view for %q missing %q: %q", input, expected, list)
		}
	}
}

func TestMainShellVersionIgnoresLocaleMutation(t *testing.T) {
	model := newMainShellLayoutModel(t, constants.DefaultView)
	model.ConfigureVersion("v0.6.0")
	_ = model.View()
	localization.Labels["version"] = "999.999.999"
	view := model.View()
	if !strings.Contains(view, "Version: v0.6.0") || strings.Contains(view, "999.999.999") {
		t.Fatalf("locale mutation changed rendered version: %q", view)
	}
}

func TestSplashRendersInjectedVersion(t *testing.T) {
	model := newMainShellLayoutModel(t, constants.SplashView)
	model.ConfigureVersion("v0.6.0")
	fonts := buildFontsList("")
	if len(fonts) == 0 {
		t.Fatal("no embedded fonts available")
	}
	if err := loadSelectedFont(model, fonts[0]); err != nil {
		t.Fatalf("embedded font failed to load: %v", err)
	}
	view := model.View()
	if !strings.Contains(view, "BLOCO Wallet v0.6.0") {
		t.Fatalf("splash missing injected version: %q", view)
	}
	if strings.Contains(view, "v0.2.0") || strings.Contains(view, "vv") {
		t.Fatalf("splash leaked stale or double-prefixed version: %q", view)
	}
}
