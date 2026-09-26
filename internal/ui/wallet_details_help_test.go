package ui

import (
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWatchOnlyWalletDetailsDisableCustodyActions(t *testing.T) {
	previousLanguage := localization.GetCurrentLanguage()
	localization.SetCurrentLanguage("en")
	t.Cleanup(func() { localization.SetCurrentLanguage(previousLanguage) })
	model := &CLIModel{width: 100, height: 30, styles: createStyles(), Vault: &wallet.WalletVault{}, selectedAccount: &wallet.AccountSummary{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: "Observer",
		Address: "0x1111111111111111111111111111111111111111", State: wallet.AccountStateActive,
		SignerKind: wallet.SignerKindWatchOnly,
	}}
	model.initWalletDetailsComponents()
	if model.walletDetailsKeys.Lock.Enabled() || model.walletDetailsKeys.Rotate.Enabled() || model.walletDetailsKeys.Export.Enabled() || model.walletDetailsKeys.EncryptedExport.Enabled() || model.walletDetailsKeys.SendNative.Enabled() || model.walletDetailsKeys.SendToken.Enabled() || model.walletDetailsKeys.ApproveToken.Enabled() {
		t.Fatal("watch-only account exposed custody or signing actions")
	}
	if !strings.Contains(model.walletDetailsContent(), "No signing secrets are stored") {
		t.Fatalf("watch-only details omitted custody status: %q", model.walletDetailsContent())
	}
}

func TestWalletDetailsUsesViewportAndDynamicBubbleHelp(t *testing.T) {
	previousLanguage := localization.GetCurrentLanguage()
	localization.SetCurrentLanguage("en")
	t.Cleanup(func() { localization.SetCurrentLanguage(previousLanguage) })
	model := &CLIModel{width: 100, height: 30, styles: createStyles(), Vault: &wallet.WalletVault{}, selectedAccount: &wallet.AccountSummary{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: "Primary",
		Address: "0x1111111111111111111111111111111111111111", State: wallet.AccountStateActive,
		SignerKind: wallet.SignerKindSoftware, Capabilities: wallet.CapabilitySignTransaction,
		DerivationPath: "m/44'/60'/0'/0/0", BIP39Language: "english",
	}}
	model.initWalletDetailsComponents()
	view := model.viewWalletDetails()
	if model.walletDetailsViewport.Width == 0 || !strings.Contains(view, "Primary") {
		t.Fatalf("wallet details did not render viewport content: %q", view)
	}
	if strings.Contains(view, "esc") || strings.Contains(view, "Lock") {
		t.Fatalf("shortcut hints must not appear in the details body: %q", view)
	}
	model.currentView = constants.WalletDetailsView
	footer := model.renderStatusBar()
	if !strings.Contains(footer, "esc") || !strings.Contains(footer, "Back") {
		t.Fatalf("status bar is missing navigation hints: %q", footer)
	}
	if strings.Count(footer, "Lock") != 1 {
		t.Fatalf("wallet action help is missing or duplicated in the status bar: %q", footer)
	}
	previous := model.walletDetailsHelp.ShowAll
	_, _ = model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.walletDetailsHelp.ShowAll == previous {
		t.Fatal("wallet details help did not toggle")
	}
	expanded := model.renderStatusBar()
	if !strings.Contains(expanded, "scroll up") {
		t.Fatalf("expanded help must list additional bindings in the status bar: %q", expanded)
	}
}
