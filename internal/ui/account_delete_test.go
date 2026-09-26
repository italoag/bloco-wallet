package ui

import (
	"context"
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDeleteMnemonic = "test test test test test test test test test test test junk"
	testDeletePassword = "del-pass-0!xx-longer"
)

func newDeletionTestModel(t *testing.T) (*CLIModel, *wallet.WalletVault, wallet.AccountSummary) {
	t.Helper()
	vault, repository, _ := newCanonicalTestVault(t)
	summary, err := vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name:                   "Delete Target",
		Mnemonic:               testDeleteMnemonic,
		StoragePassword:        []byte(testDeletePassword),
		ConfirmStoragePassword: []byte(testDeletePassword),
	})
	require.NoError(t, err)
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	_ = repository
	t.Cleanup(vault.Close)
	model := &CLIModel{
		Vault:       vault,
		currentView: constants.ListWalletsView,
		width:       120,
		height:      40,
		styles:      createStyles(),
	}
	model.applyAccountList(accounts)
	return model, vault, summary
}

func typeText(model *CLIModel, text string) {
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func drainDeletionCmd(model *CLIModel, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, inner := range batch {
			drainDeletionCmd(model, inner)
		}
		return
	}
	if _, next := model.Update(msg); next != nil {
		drainDeletionCmd(model, next)
	}
}

func TestListWalletsDeleteOpensDialog(t *testing.T) {
	model, vault, summary := newDeletionTestModel(t)
	_ = vault
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.Nil(t, cmd)
	require.NotNil(t, model.accountDeletion)
	view := model.View()
	assert.Contains(t, view, summary.AccountID)
	assert.Contains(t, view, summary.Address)
	assert.Contains(t, view, summary.Name)
}

func TestAccountDeletionRejectsWrongConfirmation(t *testing.T) {
	model, vault, _ := newDeletionTestModel(t)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, model.accountDeletion)

	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, 0, model.accountDeletion.stage)
	assert.NotEmpty(t, model.accountDeletion.err)

	typeText(model, "wrong-id")
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, 0, model.accountDeletion.stage)

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
}

func TestAccountDeletionEscCancels(t *testing.T) {
	model, vault, _ := newDeletionTestModel(t)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, model.accountDeletion)
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, model.accountDeletion)
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
}

func TestAccountDeletionSoftwareWrongPasswordThenSuccess(t *testing.T) {
	model, vault, summary := newDeletionTestModel(t)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, model.accountDeletion)

	typeText(model, summary.AccountID)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, 1, model.accountDeletion.stage)

	typeText(model, "wrong-password")
	cmd := lastDeletionCmd(model)
	drainDeletionCmd(model, cmd)
	require.NotNil(t, model.accountDeletion)
	assert.NotEmpty(t, model.accountDeletion.err)
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)

	if model.accountDeletion.stage == 0 {
		typeText(model, summary.AccountID)
		model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		require.Equal(t, 1, model.accountDeletion.stage)
	}
	model.accountDeletion.password.SetValue("")
	typeText(model, testDeletePassword)
	cmd = lastDeletionCmd(model)
	drainDeletionCmd(model, cmd)
	assert.Nil(t, model.accountDeletion)
	assert.Contains(t, model.lastOperationNotice, "deleted")
	accounts, err = vault.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Empty(t, accounts)
	assert.Contains(t, model.View(), "Account deleted")
}

func lastDeletionCmd(model *CLIModel) tea.Cmd {
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return cmd
}

func openPendingDeletion(t *testing.T, model *CLIModel, summary wallet.AccountSummary, password string) tea.Cmd {
	t.Helper()
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, model.accountDeletion)
	typeText(model, summary.AccountID)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, 1, model.accountDeletion.stage)
	typeText(model, password)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.True(t, model.accountDeletion.busy)
	return cmd
}

func TestAccountDeletionStaleMessageIgnored(t *testing.T) {
	model, _, summary := newDeletionTestModel(t)
	cmd := openPendingDeletion(t, model, summary, testDeletePassword)
	state := model.accountDeletion
	model.Update(accountDeletedMsg{operationID: state.operationID + 99, accountID: summary.AccountID})
	assert.True(t, state.busy)
	model.Update(accountDeletedMsg{operationID: state.operationID, accountID: "a9999999-9999-4999-8999-999999999999"})
	assert.True(t, state.busy)
	drainDeletionCmd(model, cmd)
	assert.Nil(t, model.accountDeletion)
}

func TestAccountDeletionDuplicateEnterWhileBusy(t *testing.T) {
	model, _, summary := newDeletionTestModel(t)
	cmd := openPendingDeletion(t, model, summary, testDeletePassword)
	_, dup := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, dup)
	drainDeletionCmd(model, cmd)
}

func TestAccountDeletionEscWhileBusyCancels(t *testing.T) {
	model, vault, summary := newDeletionTestModel(t)
	cmd := openPendingDeletion(t, model, summary, testDeletePassword)
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, model.accountDeletion)
	assert.True(t, model.accountDeletion.cancelling)
	drainDeletionCmd(model, cmd)
	require.NotNil(t, model.accountDeletion)
	assert.False(t, model.accountDeletion.busy)
	assert.NotEmpty(t, model.accountDeletion.err)
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
}

func TestAccountDeletionCommittedSuccessWinsOverEsc(t *testing.T) {
	model, vault, summary := newDeletionTestModel(t)
	cmd := openPendingDeletion(t, model, summary, testDeletePassword)
	msg := cmd()
	require.NotNil(t, msg)
	if batch, ok := msg.(tea.BatchMsg); ok {
		msg = batch[0]()
	}
	deleted, ok := msg.(accountDeletedMsg)
	require.True(t, ok)
	require.NoError(t, deleted.err)
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model.Update(deleted)
	assert.Nil(t, model.accountDeletion)
	assert.Contains(t, model.lastOperationNotice, "deleted")
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Empty(t, accounts)
}

func TestAccountDeletionClearsOldNotice(t *testing.T) {
	model, _, _ := newDeletionTestModel(t)
	model.lastOperationNotice = "Account deleted"
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, model.accountDeletion)
	assert.Empty(t, model.lastOperationNotice)
}

func TestAccountDeletionWatchOnlyNoPasswordFlow(t *testing.T) {
	vault, repository, _ := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	watch, err := vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name: "Watcher", Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	require.NoError(t, err)
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	model := &CLIModel{Vault: vault, currentView: constants.ListWalletsView, width: 120, height: 40, styles: createStyles()}
	model.applyAccountList(accounts)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	require.NotNil(t, model.accountDeletion)
	typeText(model, watch.AccountID)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	drainDeletionCmd(model, cmd)
	assert.Nil(t, model.accountDeletion)
	if _, err := repository.GetAccount(context.Background(), watch.AccountID); err == nil {
		t.Fatal("watch-only account must be deleted")
	}
}

func TestAccountDeletionEmptySelectionNoOp(t *testing.T) {
	vault, _, _ := newCanonicalTestVault(t)
	model := &CLIModel{Vault: vault, currentView: constants.ListWalletsView, width: 120, height: 40, styles: createStyles()}
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.Nil(t, model.accountDeletion)
	vault.Close()
}

func TestAccountDeletionHidesPassword(t *testing.T) {
	model, _, summary := newDeletionTestModel(t)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	typeText(model, summary.AccountID)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, 1, model.accountDeletion.stage)
	typeText(model, testDeletePassword)
	view := model.View()
	assert.NotContains(t, view, testDeletePassword)
	assert.Contains(t, view, strings.Repeat("•", 3)[:1])
}
