package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func responsiveTableAccounts() []wallet.AccountSummary {
	return []wallet.AccountSummary{
		{
			AccountID: "60f52053-13d7-4517-8289-2d3d8212d342", Name: "Alpha Vault",
			Address: "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", SignerKind: wallet.SignerKindSoftware,
			State: wallet.AccountStateActive, CreatedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		},
		{
			AccountID: "71111111-1111-4111-8111-111111111111", Name: "Beta Watch",
			Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94", SignerKind: wallet.SignerKindWatchOnly,
			State: wallet.AccountStateActive, CreatedAt: time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC),
		},
	}
}

func newResponsiveTableModel(t *testing.T, width, height int) (*CLIModel, []wallet.AccountSummary) {
	t.Helper()
	previousLanguage := localization.GetCurrentLanguage()
	localization.SetCurrentLanguage("en")
	t.Cleanup(func() { localization.SetCurrentLanguage(previousLanguage) })
	accounts := responsiveTableAccounts()
	model := &CLIModel{
		currentView:  constants.ListWalletsView,
		menuItems:    NewMenu(),
		selectedMenu: 2,
		styles:       createStyles(),
	}
	model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model.applyAccountList(accounts)
	return model, accounts
}

func TestAccountTableLayoutFillsAvailableWidth(t *testing.T) {
	accounts := responsiveTableAccounts()
	for _, available := range []int{76, 96, 124, 156, 212} {
		columns, _ := accountTableLayout(available, accounts)
		require.Len(t, columns, 4)
		total := 0
		visible := 0
		for _, column := range columns {
			if column.Width > 0 {
				visible++
			}
			total += column.Width
		}
		assert.Equal(t, available, total+2*visible, "widths must fill available width %d", available)
		assert.Equal(t, 42, columns[3].Width)
	}
}

func TestAccountTableHidesOptionalColumnsNarrowly(t *testing.T) {
	accounts := responsiveTableAccounts()
	columns, rows := accountTableLayout(96, accounts)
	require.Len(t, columns, 4)
	assert.Equal(t, 0, columns[2].Width)
	assert.Equal(t, 20, columns[1].Width)
	require.Len(t, rows[0], 4)
	assert.Equal(t, accounts[0].Address, rows[0][3])
	columns, _ = accountTableLayout(80, accounts)
	assert.Equal(t, 0, columns[1].Width)
	assert.Equal(t, 0, columns[2].Width)
	assert.Equal(t, 42, columns[3].Width)
}

func TestAccountTableNameColumnExpandsWithWidth(t *testing.T) {
	narrow, _ := accountTableLayout(120, nil)
	wide, _ := accountTableLayout(200, nil)
	assert.Greater(t, wide[0].Width, narrow[0].Width)
	assert.Equal(t, 42, narrow[3].Width)
	assert.Equal(t, 42, wide[3].Width)
}

func TestWalletListHidesUUIDAndShowsNameAndAddress(t *testing.T) {
	model, accounts := newResponsiveTableModel(t, 160, 50)
	view := model.View()
	assert.NotContains(t, view, accounts[0].AccountID)
	assert.NotContains(t, view, accounts[1].AccountID)
	assert.Contains(t, view, "Alpha Vault")
	assert.Contains(t, view, accounts[0].Address)
	for _, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), model.width, "line exceeds terminal width")
	}
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	assert.LessOrEqual(t, len(lines), model.height)
}

func TestWalletListRendersAtSupportedSizes(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 24}, {120, 30}, {160, 50}, {220, 60}} {
		model, _ := newResponsiveTableModel(t, size[0], size[1])
		view := model.View()
		for _, line := range strings.Split(view, "\n") {
			assert.LessOrEqual(t, lipgloss.Width(line), size[0], "line overflows at %dx%d", size[0], size[1])
		}
		lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
		assert.LessOrEqual(t, len(lines), size[1], "view overflows height at %dx%d", size[0], size[1])
		assert.Contains(t, view, "Alpha Vault")
		assert.Contains(t, view, "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	}
}

func TestWalletListFallsBackBelowMinimum(t *testing.T) {
	model, _ := newResponsiveTableModel(t, 70, 20)
	view := model.View()
	assert.Contains(t, view, "Terminal too small")
	assert.Contains(t, view, "80 × 16")
}

func TestWalletListCompactHeaderAtMediumSize(t *testing.T) {
	model, _ := newResponsiveTableModel(t, 100, 24)
	view := model.View()
	assert.Contains(t, view, "BLOCO Wallet | Version:")
}

func TestWalletSelectionTracksAccountIDAcrossRefresh(t *testing.T) {
	model, accounts := newResponsiveTableModel(t, 160, 50)
	model.walletTable.MoveDown(1)
	selected := model.selectedAccountFromTable()
	require.NotNil(t, selected)
	assert.Equal(t, accounts[1].AccountID, selected.AccountID)

	reordered := []wallet.AccountSummary{accounts[1], accounts[0]}
	model.applyAccountList(reordered)
	selected = model.selectedAccountFromTable()
	require.NotNil(t, selected)
	assert.Equal(t, accounts[1].AccountID, selected.AccountID, "selection must track the same account ID after reorder")
}

func TestWalletSelectionFallsBackToClampedCursor(t *testing.T) {
	model, accounts := newResponsiveTableModel(t, 160, 50)
	model.walletTable.MoveDown(1)
	model.applyAccountList(accounts[:1])
	selected := model.selectedAccountFromTable()
	require.NotNil(t, selected)
	assert.Equal(t, accounts[0].AccountID, selected.AccountID)
}

func TestWalletSelectionEmptyList(t *testing.T) {
	model, _ := newResponsiveTableModel(t, 160, 50)
	model.applyAccountList(nil)
	assert.Nil(t, model.selectedAccountFromTable())
	assert.Empty(t, model.accountTableIDs)
}

func TestWalletSelectionStableAcrossResize(t *testing.T) {
	model, accounts := newResponsiveTableModel(t, 160, 50)
	model.walletTable.MoveDown(1)
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	selected := model.selectedAccountFromTable()
	require.NotNil(t, selected)
	assert.Equal(t, accounts[1].AccountID, selected.AccountID)
	model.Update(tea.WindowSizeMsg{Width: 220, Height: 60})
	selected = model.selectedAccountFromTable()
	require.NotNil(t, selected)
	assert.Equal(t, accounts[1].AccountID, selected.AccountID)
}

func TestWalletListDuplicateAddressSelectionUsesCursor(t *testing.T) {
	accounts := responsiveTableAccounts()
	accounts[1].Address = accounts[0].Address
	model := &CLIModel{currentView: constants.ListWalletsView, width: 160, height: 50, styles: createStyles()}
	model.applyAccountList(accounts)
	model.walletTable.MoveDown(1)
	selected := model.selectedAccountFromTable()
	require.NotNil(t, selected)
	assert.Equal(t, accounts[1].AccountID, selected.AccountID)
}

func TestAccountTableTinyWidthsStayBounded(t *testing.T) {
	for available := 0; available <= 5; available++ {
		columns, _ := accountTableLayout(available, responsiveTableAccounts())
		require.Len(t, columns, 4)
		total := 0
		visible := 0
		for _, column := range columns {
			assert.GreaterOrEqual(t, column.Width, 0)
			if column.Width > 0 {
				visible++
			}
			total += column.Width
		}
		assert.LessOrEqual(t, total+2*visible, available, "tiny width %d overshoots", available)
	}
}

func TestWalletListRenderedTableFillsInnerWidth(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 30}, {160, 50}, {220, 60}} {
		model, accounts := newResponsiveTableModel(t, size[0], size[1])
		inner := model.accountTableWidth()
		tableView := model.walletTable.View()
		assert.Equal(t, inner, lipgloss.Width(tableView), "table must fill inner width at %dx%d", size[0], size[1])
		for _, line := range strings.Split(tableView, "\n") {
			assert.Equal(t, inner, lipgloss.Width(line), "table line must span inner width at %dx%d", size[0], size[1])
		}
		view := model.View()
		for _, line := range strings.Split(view, "\n") {
			assert.LessOrEqual(t, lipgloss.Width(line), size[0])
		}
		assert.LessOrEqual(t, lipgloss.Height(view), size[1])
		assert.NotContains(t, view, accounts[0].AccountID)
	}
}

func TestWalletSelectionVisibleAcrossResizeCycle(t *testing.T) {
	accounts := make([]wallet.AccountSummary, 40)
	for index := range accounts {
		accounts[index] = wallet.AccountSummary{
			AccountID:  fmt.Sprintf("%08x-1111-4111-8111-%012x", index+1, index+1),
			Name:       fmt.Sprintf("UniqueName%02d", index),
			Address:    fmt.Sprintf("0x%040x", index+1),
			SignerKind: wallet.SignerKindWatchOnly, State: wallet.AccountStateActive,
			CreatedAt: time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC),
		}
	}
	model, _ := newResponsiveTableModel(t, 220, 60)
	model.applyAccountList(accounts)
	target := 36
	model.walletTable.GotoTop()
	model.walletTable.MoveDown(target)
	require.NotNil(t, model.selectedAccountFromTable())
	require.Equal(t, accounts[target].AccountID, model.selectedAccountFromTable().AccountID)

	assertVisible := func(width, height int) {
		tableView := model.walletTable.View()
		assert.Contains(t, tableView, accounts[target].Name, "selected row must render at %dx%d", width, height)
		assert.Contains(t, tableView, accounts[target].Address, "selected address must render at %dx%d", width, height)
	}
	assertVisible(220, 60)
	for _, size := range [][2]int{{80, 24}, {120, 30}, {220, 60}} {
		model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		_ = model.View()
		selected := model.selectedAccountFromTable()
		require.NotNil(t, selected)
		assert.Equal(t, accounts[target].AccountID, selected.AccountID)
		assertVisible(size[0], size[1])
	}
}

func TestWalletListDeleteDialogTargetsSelectedAccount(t *testing.T) {
	vault, _, _ := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	summary, err := vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name: "Listed", Mnemonic: "test test test test test test test test test test test junk",
		StoragePassword: []byte("del-pass-0!xx-longer"), ConfirmStoragePassword: []byte("del-pass-0!xx-longer"),
	})
	require.NoError(t, err)
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	model := &CLIModel{Vault: vault, currentView: constants.ListWalletsView, width: 160, height: 50, styles: createStyles()}
	model.applyAccountList(accounts)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, model.accountDeletion)
	assert.Equal(t, summary.AccountID, model.accountDeletion.account.AccountID)
	assert.Contains(t, model.View(), summary.AccountID)
}

func TestWalletListRerenderDoesNotResetTableScroll(t *testing.T) {
	accs := make([]wallet.AccountSummary, 0, 40)
	for i := 0; i < 40; i++ {
		accs = append(accs, wallet.AccountSummary{
			AccountID: fmt.Sprintf("scroll-id-%02d", i), Name: fmt.Sprintf("ScrollName%02d", i),
			Address: fmt.Sprintf("0x%040x", i+1), CreatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	m, _ := newResponsiveTableModel(t, 120, 30)
	m.applyAccountList(accs)
	m.walletTable.GotoTop()
	m.walletTable.MoveDown(36)
	m.View()
	m.walletTable.MoveUp(3)
	before := m.walletTable.View()
	selectedID := m.selectedAccountFromTable().AccountID
	m.View()
	assert.Equal(t, before, m.walletTable.View(), "same-height rerender must not reset the table viewport")
	assert.Equal(t, selectedID, m.selectedAccountFromTable().AccountID)
}
