package ui

import (
	"strings"
	"testing"

	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCreateWalletOptionsStartWithSelectableWordCountList(t *testing.T) {
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles(), width: 100, height: 30}
	model.initCreateWallet()
	if model.createOptionList.Items() == nil || len(model.createOptionList.Items()) != 5 {
		t.Fatalf("word count selector is not a Bubble list: %+v", model.createOptionList.Items())
	}
	selected, ok := model.createOptionList.SelectedItem().(createOptionItem)
	if !ok || selected.value != "12" {
		t.Fatalf("word count default is not 12: %#v", model.createOptionList.SelectedItem())
	}
}

func TestCreateWalletOptionListsAdvanceThroughDefaultsAndCustomPath(t *testing.T) {
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles(), width: 100, height: 30}
	model.initCreateWallet()
	_, _ = model.updateCreateWalletOptions(tea.KeyMsg{Type: tea.KeyEnter})
	if model.createOptionsStage != 1 || model.createWordCountInput.Value() != "12" {
		t.Fatal("word count list did not persist its default")
	}
	_, _ = model.updateCreateWalletOptions(tea.KeyMsg{Type: tea.KeyEnter})
	if model.createOptionsStage != 2 || model.createLanguageInput.Value() != "english" {
		t.Fatal("language list did not persist its default")
	}
	_, _ = model.updateCreateWalletOptions(tea.KeyMsg{Type: tea.KeyEnter})
	if model.createOptionsStage != 3 {
		t.Fatal("passphrase step did not advance to path list")
	}
	model.createOptionList.Select(3)
	_, _ = model.updateCreateWalletOptions(tea.KeyMsg{Type: tea.KeyEnter})
	if !model.createCustomPath || !model.createDerivationPathInput.Focused() {
		t.Fatal("custom derivation path option did not open validated input")
	}
}

func TestMnemonicCardsShowNumberAboveEveryWord(t *testing.T) {
	words := []string{"alpha", "beta", "gamma", "delta"}
	view := renderMnemonicCards(words, 80)
	for index, word := range words {
		number := []string{"01", "02", "03", "04"}[index]
		if !strings.Contains(view, number) || !strings.Contains(view, word) || strings.Index(view, number) > strings.Index(view, word) {
			t.Fatalf("card does not show %s above %q: %q", number, word, view)
		}
	}
	if strings.Count(view, "╭") != len(words) {
		t.Fatalf("expected one bordered card per word: %q", view)
	}
}
