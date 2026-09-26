package ui

import (
	"context"
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func footerRows(t *testing.T, model *CLIModel) string {
	t.Helper()
	view := model.View()
	lines := strings.Split(view, "\n")
	footerHeight := lipgloss.Height(model.renderStatusBar())
	require.LessOrEqual(t, footerHeight, len(lines))
	return strings.Join(lines[len(lines)-footerHeight:], "\n")
}

func assertViewFitsTerminal(t *testing.T, model *CLIModel, width, height int) string {
	t.Helper()
	view := model.View()
	for _, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), width, "line overflows %d columns", width)
	}
	assert.LessOrEqual(t, lipgloss.Height(view), height, "view exceeds %d rows", height)
	return view
}

func TestStatusBarWalletListHintsOnlyInFooter(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.applyAccountList([]wallet.AccountSummary{summary})
	model.currentView = constants.ListWalletsView

	body := model.viewListWallets()
	for _, stale := range []string{"Enter: open", "d/Delete", "Press", "up/down", "Esc: back"} {
		assert.NotContains(t, body, stale, "list body must not carry shortcut hints")
	}

	footer := ansi.Strip(footerRows(t, model))
	for _, token := range []string{"Open", "Delete", "Esc", "Ctrl+Q", "View: My Wallets"} {
		assert.Contains(t, footer, token)
	}
	assert.NotContains(t, ansi.Strip(model.View()), "Press 'q'")
	assert.Equal(t, 1, strings.Count(ansi.Strip(model.View()), "d/Delete Delete"), "delete hint must appear exactly once")
	assert.Equal(t, 1, strings.Count(ansi.Strip(model.View()), "Enter Open"), "open hint must appear exactly once")
}

func TestStatusBarWalletListEmptyHasNoRowActions(t *testing.T) {
	model, _ := newResponsiveTableModel(t, 100, 30)
	model.applyAccountList(nil)
	model.currentView = constants.ListWalletsView
	footer := ansi.Strip(model.renderStatusBar())
	assert.NotContains(t, footer, "Open")
	assert.NotContains(t, footer, "Delete")
	assert.NotContains(t, footer, "Navigate")
	assert.Contains(t, footer, "Esc")
	assert.Contains(t, footer, "Ctrl+Q")
}

func TestStatusBarAccountDeletionHints(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.applyAccountList([]wallet.AccountSummary{summary})
	model.currentView = constants.ListWalletsView

	model.initAccountDeletion(summary)
	body := model.viewAccountDeletion()
	assert.NotContains(t, body, "Press Enter")
	assert.NotContains(t, body, "Esc to cancel")
	assert.Contains(t, body, "Confirm account ID")
	assert.Contains(t, body, "backup is required")
	footer := ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "View: Delete Wallet")
	assert.Contains(t, footer, "Enter Continue")
	assert.Contains(t, footer, "Esc Cancel")
	assert.Contains(t, footer, "Ctrl+Q Quit")

	model.accountDeletion.stage = 1
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Delete")

	watchOnly := wallet.AccountSummary{AccountID: "w", SignerKind: wallet.SignerKindWatchOnly, State: wallet.AccountStateActive}
	model.initAccountDeletion(watchOnly)
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Delete", "non-software deletion confirms in one step")

	model.accountDeletion.busy = true
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Esc Cancel")
	assert.Contains(t, footer, "Ctrl+Q Cancel")
	assert.NotContains(t, footer, "Enter")
	assert.NotContains(t, footer, "Enter Delete")
	busyBody := model.viewAccountDeletion()
	assert.NotContains(t, busyBody, "Press Esc")
	assert.Contains(t, busyBody, "Deleting account")
}

func TestStatusBarCanonicalImportHints(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	model.currentView = constants.CanonicalImportView
	model.initCanonicalImport(wallet.ImportMethodKeystore)

	footer := ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "View: Import Wallet")
	assert.Contains(t, footer, "Enter Continue")
	assert.Contains(t, footer, "Esc Cancel")
	body := model.viewCanonicalImport()
	assert.NotContains(t, body, "Press Enter")
	assert.NotContains(t, body, "Esc to cancel")
	assert.NotContains(t, body, "Tab:")

	model.canonicalImport.stage = 1
	require.Equal(t, "keystore_path", model.canonicalImport.fields[1].key)
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Tab Complete")

	model.canonicalImport.stage = 2
	require.Equal(t, "source_password", model.canonicalImport.fields[2].key)
	footer = ansi.Strip(model.renderStatusBar())
	assert.NotContains(t, footer, "Tab", "password fields must not offer path completion")

	model.canonicalImport.stage = len(model.canonicalImport.fields) - 1
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Preview")

	model.canonicalImport.preview = &wallet.ImportPreview{}
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Import")
	previewBody := model.viewCanonicalImport()
	assert.NotContains(t, previewBody, "Press Enter")

	model.canonicalImport.preview = nil
	model.canonicalImport.resultLines = []string{"imported 1 account"}
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Wallets")

	model.canonicalImport.resultLines = nil
	model.canonicalImport.busy = true
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Esc Cancel")
	assert.Contains(t, footer, "Ctrl+Q Cancel")
	assert.NotContains(t, footer, "Enter")
	busyBody := model.viewCanonicalImport()
	assert.NotContains(t, busyBody, "Press Esc")
}

func TestStatusBarWalletListUnmappedSelectionHints(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.applyAccountList([]wallet.AccountSummary{summary})
	model.currentView = constants.ListWalletsView

	model.accountTableIDs = nil
	model.wallets = nil
	require.NotEmpty(t, model.walletTable.Rows())
	require.Nil(t, model.selectedAccountFromTable())
	require.Nil(t, model.selectedWalletFromTable())

	footer := ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Up/Down Navigate")
	assert.NotContains(t, footer, "Enter Open")
	assert.NotContains(t, footer, "d/Delete")
	assert.Contains(t, footer, "Esc Back")
	assert.Contains(t, footer, "Ctrl+Q Quit")
}

func TestStatusBarCanonicalImportEncryptedPathNoCompletion(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	model.currentView = constants.CanonicalImportView
	model.initCanonicalImport(canonicalEncryptedMethod)

	model.canonicalImport.stage = 1
	require.Equal(t, "encrypted_path", model.canonicalImport.fields[1].key)
	footer := ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Continue")
	assert.NotContains(t, footer, "Tab", "encrypted_path has no completion handler")
	assert.NotContains(t, footer, "Suggestions")
}

func TestStatusBarWalletDetailsHintsFollowBindings(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initWalletDetailsComponents()
	model.currentView = constants.WalletDetailsView

	footer := ansi.Strip(model.renderStatusBar())
	for _, token := range []string{"R Recovery", "e Export Keystore", "x Encrypted backup", "esc Back", "Ctrl+Q Quit"} {
		assert.Contains(t, footer, token)
	}
	assert.NotContains(t, model.viewWalletDetails(), "R Recovery", "body must not render the help model")
	assert.NotContains(t, model.viewWalletDetails(), "esc Back", "body must not render the help model")

	watch, err := model.Vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name: "Watch", Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	require.NoError(t, err)
	model.selectedAccount = &watch
	model.initWalletDetailsComponents()
	footer = ansi.Strip(model.renderStatusBar())
	assert.NotContains(t, footer, "Recovery")
	assert.NotContains(t, footer, "Export")
	assert.Contains(t, footer, "esc Back")
}

func TestStatusBarVaultActionHints(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary

	model.currentView = constants.RotatePasswordView
	model.vaultActionStage = 0
	assert.Contains(t, ansi.Strip(model.renderStatusBar()), "Enter Continue")
	model.vaultActionStage = 2
	footer := ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Change password")
	assert.Contains(t, footer, "View: Change Password")

	model.currentView = constants.ExportAccountView
	model.vaultActionStage = 0
	model.vaultActionPreview = false
	assert.Contains(t, ansi.Strip(model.renderStatusBar()), "Enter Continue")
	model.vaultActionPreview = true
	assert.Contains(t, ansi.Strip(model.renderStatusBar()), "Enter Export")
	assert.NotContains(t, model.viewVaultAction(true), "Press Enter")
	assert.NotContains(t, model.viewVaultAction(false), "Press Esc")
}

func TestStatusBarRecoveryHintsPerStage(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery

	footer := ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "View: Recovery")
	assert.Contains(t, footer, "Enter Continue")
	assert.Contains(t, footer, "Esc Back")
	assert.Contains(t, footer, "Ctrl+Q Quit")
	assert.NotContains(t, footer, "test test test")

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStagePassword, state.stage)
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Enter Continue")
	assert.Contains(t, footer, "Esc Cancel")

	for i, action := range state.actions {
		if action.kind == wallet.RecoveryMnemonic && action.export {
			state.selected = i
		}
	}
	state.stage = recoveryStageConfirmation
	assert.Contains(t, ansi.Strip(model.renderStatusBar()), "Enter Export")
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryMnemonic && !action.export {
			state.selected = i
		}
	}
	assert.Contains(t, ansi.Strip(model.renderStatusBar()), "Enter Reveal")

	state.stage = recoveryStageRevealed
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Esc Hide")
	assert.NotContains(t, footer, "Enter")

	state.stage = recoveryStagePassword
	state.busy = true
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Esc Cancel")
	assert.Contains(t, footer, "Ctrl+Q Cancel & Quit")
}

func TestStatusBarRootAndFallbackHints(t *testing.T) {
	model, _ := newResponsiveTableModel(t, 120, 30)
	model.currentView = constants.DefaultView
	footer := ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Up/Down Select")
	assert.Contains(t, footer, "Enter Open")
	assert.Contains(t, footer, "Ctrl+Q Quit")
	assert.NotContains(t, footer, "Press 'q'")

	model.currentView = constants.ConfigurationView
	assert.Contains(t, ansi.Strip(model.renderStatusBar()), "Esc Back")

	model.currentView = constants.WalletConnectView
	footer = ansi.Strip(model.renderStatusBar())
	assert.Contains(t, footer, "Esc Back")
	assert.Contains(t, footer, "Ctrl+Q Quit")
}

func TestGlobalQStillTypesIntoInputs(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary

	model.initRecovery()
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStagePassword, model.recovery.stage)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.Equal(t, "q", model.recovery.password.Value(), "q must reach the password input, not quit")
	require.NotNil(t, model.recovery)

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Nil(t, model.recovery)

	model.currentView = constants.CanonicalImportView
	model.initCanonicalImport(wallet.ImportMethodKeystore)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.Equal(t, "q", model.canonicalImport.fields[0].input.Value(), "q must reach canonical fields")
}

func TestStatusBarLayoutsFitAcrossSizes(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary

	for _, accounts := range []int{1, 14, 40} {
		list := make([]wallet.AccountSummary, 0, accounts)
		for i := 0; i < accounts; i++ {
			entry := summary
			entry.Name = strings.Repeat("W", 8) + "-" + string(rune('a'+i%26))
			list = append(list, entry)
		}
		for _, size := range [][2]int{{80, 24}, {100, 24}, {120, 30}, {160, 50}, {220, 60}} {
			model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			model.applyAccountList(list)
			model.currentView = constants.ListWalletsView
			assertViewFitsTerminal(t, model, size[0], size[1])
			footer := ansi.Strip(footerRows(t, model))
			assert.Contains(t, footer, "Ctrl+Q", "hint tokens must not be cut mid-token")
			assert.Contains(t, footer, "Esc")

			model.initAccountDeletion(summary)
			assertViewFitsTerminal(t, model, size[0], size[1])
			model.clearAccountDeletion()

			model.currentView = constants.WalletDetailsView
			model.initWalletDetailsComponents()
			assertViewFitsTerminal(t, model, size[0], size[1])

			model.initRecovery()
			assertViewFitsTerminal(t, model, size[0], size[1])
			assert.Contains(t, ansi.Strip(footerRows(t, model)), "View: Recovery")
			model.clearRecovery()
			model.currentView = constants.ListWalletsView
		}
	}
}
