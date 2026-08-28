package ui

import (
	"strings"
	"testing"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWatchOnlyWalletDetailsDisableCustodyActions(t *testing.T) {
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet"}
	t.Cleanup(func() { localization.Labels = previousLabels })
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
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet"}
	t.Cleanup(func() { localization.Labels = previousLabels })
	model := &CLIModel{width: 100, height: 30, styles: createStyles(), Vault: &wallet.WalletVault{}, selectedAccount: &wallet.AccountSummary{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: "Primary",
		Address: "0x1111111111111111111111111111111111111111", State: wallet.AccountStateActive,
		SignerKind: wallet.SignerKindSoftware, Capabilities: wallet.CapabilitySignTransaction,
		DerivationPath: "m/44'/60'/0'/0/0", BIP39Language: "english",
	}}
	model.initWalletDetailsComponents()
	view := model.viewWalletDetails()
	if model.walletDetailsViewport.Width == 0 || !strings.Contains(view, "Primary") || !strings.Contains(view, "esc") || !strings.Contains(view, "back") {
		t.Fatalf("wallet details did not render viewport and help: %q", view)
	}
	if strings.Count(view, "Lock") != 1 {
		t.Fatalf("wallet action help is missing or duplicated: %q", view)
	}
	previous := model.walletDetailsHelp.ShowAll
	_, _ = model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if model.walletDetailsHelp.ShowAll == previous {
		t.Fatal("wallet details help did not toggle")
	}
}
