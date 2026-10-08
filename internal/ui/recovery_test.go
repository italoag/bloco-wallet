package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const recoveryUITestPassword = "jk recovery password 1!"

func newRecoveryTestModel(t *testing.T, summary wallet.AccountSummary) *CLIModel {
	t.Helper()
	vault, _, cfg := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	account := summary
	model := &CLIModel{
		Vault:         vault,
		styles:        createStyles(),
		balanceConfig: cfg,
		menuItems:     NewMenu(),
	}
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model.selectedAccount = &account
	model.currentView = constants.WalletDetailsView
	return model
}

func importRecoveryUIAccount(t *testing.T, model *CLIModel) wallet.AccountSummary {
	t.Helper()
	summary, err := model.Vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name: "Recovery UI", Mnemonic: "test test test test test test test test test test test junk",
		StoragePassword: []byte(recoveryUITestPassword), ConfirmStoragePassword: []byte(recoveryUITestPassword),
	})
	require.NoError(t, err)
	return summary
}

func recoveryDriveToConfirm(t *testing.T, model *CLIModel, word string) tea.Cmd {
	t.Helper()
	state := model.recovery
	require.NotNil(t, state)
	if state.stage == recoveryStageMenu {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	require.Equal(t, recoveryStagePassword, state.stage)
	state.password.SetValue(recoveryUITestPassword)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStageConfirmation, state.stage)
	state.confirmation.SetValue(word)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	return cmd
}

func recoveryDeliverResult(t *testing.T, model *CLIModel, cmd tea.Cmd) {
	t.Helper()
	feedCmdResult(model, cmd, 0)
}

func TestRecoveryKeyDisabledForWatchOnly(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	watch, err := model.Vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name: "Watch", Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	require.NoError(t, err)
	model.selectedAccount = &watch
	model.initWalletDetailsComponents()
	assert.False(t, model.walletDetailsKeys.Recovery.Enabled())
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	assert.Nil(t, model.recovery)
	assert.Equal(t, constants.WalletDetailsView, model.currentView)
}

func TestRecoveryKeyOpensMenuWithExpectedActions(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initWalletDetailsComponents()
	assert.True(t, model.walletDetailsKeys.Recovery.Enabled())
	assert.Contains(t, model.walletDetailsHelp.View(model.walletDetailsKeys), "Recovery")

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	require.Equal(t, constants.RecoveryView, model.currentView)
	require.NotNil(t, model.recovery)
	view := model.View()
	assert.Contains(t, view, "Show recovery phrase")
	assert.Contains(t, view, "Export recovery phrase")
	assert.Contains(t, view, "Show private key")
	assert.Contains(t, view, "Export private key")
	assert.NotContains(t, view, "test test test", "no secret may render before authentication")
}

func TestRecoveryRevealFlowRequiresPasswordAndTypedWord(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	require.NotNil(t, state)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStagePassword, state.stage)
	assert.NotContains(t, model.View(), "test test test")

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, recoveryStagePassword, state.stage)
	assert.Contains(t, model.View(), "password")

	state.password.SetValue("wrong password")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.confirmation.SetValue("REVEAL")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "", state.password.Value(), "password input cleared before dispatch")
	recoveryDeliverResult(t, model, cmd)
	assert.Equal(t, recoveryStagePassword, state.stage)
	assert.Contains(t, model.View(), "Incorrect password")
	assert.NotContains(t, model.View(), "test test test")

	state.password.SetValue(recoveryUITestPassword)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStageConfirmation, state.stage)
	state.confirmation.SetValue("reveal")
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd, "lowercase reveal must not dispatch")
	state.confirmation.SetValue("REVEAL")
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	recoveryDeliverResult(t, model, cmd)

	require.Equal(t, recoveryStageRevealed, state.stage)
	require.NotNil(t, state.material)
	view := model.View()
	for index, word := range strings.Fields("test test test test test test test test test test test junk") {
		assert.Contains(t, view, fmt.Sprintf("%d. ", index+1))
		assert.Contains(t, view, word)
	}
	assert.Contains(t, view, fmt.Sprintf("%d.", 12))
}

func TestRecoveryRevealPrivateKeyShowsFullHex(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryPrivateKey && !action.export {
			state.selected = i
		}
	}
	cmd := recoveryDriveToConfirm(t, model, "REVEAL")
	recoveryDeliverResult(t, model, cmd)
	require.Equal(t, recoveryStageRevealed, state.stage)
	assert.Contains(t, model.View(), "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
}

func TestRecoveryEscBlurExpiryAndResizeHideSecret(t *testing.T) {
	assertZeroed := func(t *testing.T, raw []byte) {
		t.Helper()
		require.NotEmpty(t, raw)
		for _, b := range raw {
			assert.Zero(t, b, "revealed buffer must be zeroed")
		}
	}

	t.Run("esc exits and clears", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		require.Equal(t, recoveryStageRevealed, model.recovery.stage)
		raw := model.recovery.material.Bytes()
		model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		assert.Nil(t, model.recovery)
		assert.Equal(t, constants.WalletDetailsView, model.currentView)
		assertZeroed(t, raw)
	})

	t.Run("blur hides secret without exit", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		raw := model.recovery.material.Bytes()
		model.Update(tea.BlurMsg{})
		require.NotNil(t, model.recovery)
		assert.Nil(t, model.recovery.material)
		assertZeroed(t, raw)
		assert.Equal(t, recoveryStageMenu, model.recovery.stage)
		assert.NotContains(t, model.View(), "test test test")
		assert.Contains(t, model.View(), "focus lost")
		model.Update(tea.FocusMsg{})
		assert.NotContains(t, model.View(), "test test test", "focus must not auto reveal")
	})

	t.Run("expired msg hides current reveal", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		opID := model.recovery.operationID
		model.Update(recoveryExpiredMsg{operationID: opID + 99})
		assert.Equal(t, recoveryStageRevealed, model.recovery.stage, "stale expiry must not hide current reveal")
		assert.NotNil(t, model.recovery.material)
		raw := model.recovery.material.Bytes()
		model.Update(recoveryExpiredMsg{operationID: opID})
		assert.Nil(t, model.recovery.material)
		assertZeroed(t, raw)
		assert.Equal(t, recoveryStageMenu, model.recovery.stage)
		assert.Contains(t, model.View(), "expired")
	})

	t.Run("too small resize wipes", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		raw := model.recovery.material.Bytes()
		model.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
		assert.Nil(t, model.recovery.material)
		assertZeroed(t, raw)
		assert.NotContains(t, model.View(), "test test test")
	})

	t.Run("past expiresAt view hides secret", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		model.recovery.expiresAt = time.Now().Add(-time.Second)
		model.displayTime = time.Now()
		assert.NotContains(t, model.View(), "test test test")
		assert.Contains(t, model.View(), "Secret hidden")
	})

	t.Run("clock tick expires reveal and destroys buffer", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		require.NotNil(t, model.recovery.material)
		raw := model.recovery.material.Bytes()
		require.NotEmpty(t, raw)
		model.recovery.expiresAt = time.Now().Add(-time.Second)
		model.Update(clockTickMsg(time.Now()))
		assert.Nil(t, model.recovery.material)
		for _, b := range raw {
			assert.Zero(t, b, "revealed buffer must be zeroed on expiry")
		}
		assert.NotContains(t, model.View(), "test test test")
	})

	t.Run("zero clock never shows secret", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		model.displayTime = time.Time{}
		assert.NotContains(t, model.View(), "test test test")
	})
}

func TestRecoveryStaleResultsDestroyed(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery

	stale, err := model.Vault.RevealRecoverySecret(context.Background(), wallet.RecoverySecretRequest{
		AccountID: summary.AccountID, ConfirmAccountID: summary.AccountID,
		Password: []byte(recoveryUITestPassword), Kind: wallet.RecoveryMnemonic,
	})
	require.NoError(t, err)
	staleRaw := stale.Bytes()
	require.NotEmpty(t, staleRaw)
	model.Update(recoveryResultMsg{operationID: 999, accountID: summary.AccountID, material: stale})
	for _, b := range staleRaw {
		assert.Zero(t, b, "stale material buffer must be zeroed")
	}
	assert.NotContains(t, model.View(), "test test test")

	cmd := recoveryDriveToConfirm(t, model, "REVEAL")
	recoveryDeliverResult(t, model, cmd)
	require.Equal(t, recoveryStageRevealed, state.stage)

	other, err := model.Vault.RevealRecoverySecret(context.Background(), wallet.RecoverySecretRequest{
		AccountID: summary.AccountID, ConfirmAccountID: summary.AccountID,
		Password: []byte(recoveryUITestPassword), Kind: wallet.RecoveryMnemonic,
	})
	require.NoError(t, err)
	otherRaw := other.Bytes()
	model.Update(recoveryResultMsg{operationID: state.operationID, accountID: "different", material: other})
	for _, b := range otherRaw {
		assert.Zero(t, b, "mismatched-account material buffer must be zeroed")
	}
	assert.Equal(t, recoveryStageRevealed, state.stage)

	errResult, err := model.Vault.RevealRecoverySecret(context.Background(), wallet.RecoverySecretRequest{
		AccountID: summary.AccountID, ConfirmAccountID: summary.AccountID,
		Password: []byte(recoveryUITestPassword), Kind: wallet.RecoveryMnemonic,
	})
	require.NoError(t, err)
	errRaw := errResult.Bytes()
	model.Update(recoveryResultMsg{operationID: 999, accountID: summary.AccountID, material: errResult, err: context.Canceled})
	for _, b := range errRaw {
		assert.Zero(t, b, "material carried with an error result must be zeroed")
	}
}

func TestRecoveryCancelAfterCompletedRevealDestroysResult(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	cmd := recoveryDriveToConfirm(t, model, "REVEAL")
	msgs := cmdResultMsgs(t, cmd)
	require.NotEmpty(t, msgs)
	result, ok := msgs[0].(recoveryResultMsg)
	require.True(t, ok)
	require.Nil(t, result.err)
	require.NotNil(t, result.material)
	raw := result.material.Bytes()

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, state.cancelling && state.exitAfterResult)
	for _, resultMsg := range msgs {
		model.Update(resultMsg)
	}
	for _, b := range raw {
		assert.Zero(t, b, "cancelled reveal result must be destroyed")
	}
	assert.Nil(t, model.recovery, "esc while busy exits once the result arrives")
	assert.Equal(t, constants.WalletDetailsView, model.currentView)
}

func TestRecoveryCtrlQWhileBusyQuitsAfterResult(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryMnemonic && action.export {
			state.selected = i
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	destination := filepath.Join(t.TempDir(), "quit.mnemonic")
	state.destination.SetValue(destination)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cmd := recoveryDriveToConfirm(t, model, "EXPORT")
	msgs := cmdResultMsgs(t, cmd)
	require.NotEmpty(t, msgs)
	_, quitCmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	assert.Nil(t, quitCmd, "busy export must wait for the result instead of quitting")
	assert.True(t, state.cancelling && state.quitAfterResult)
	_, final := model.Update(msgs[0])
	for _, rest := range msgs[1:] {
		model.Update(rest)
	}
	require.NotNil(t, final)
	assert.Nil(t, model.recovery)
	assert.Contains(t, model.lastOperationNotice, destination, "completed export must be reported even after ctrl+q")
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestRecoveryBlurDuringBusyExportKeepsOutcome(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryPrivateKey && action.export {
			state.selected = i
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	destination := filepath.Join(t.TempDir(), "blur.key")
	state.destination.SetValue(destination)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cmd := recoveryDriveToConfirm(t, model, "EXPORT")
	msgs := cmdResultMsgs(t, cmd)
	require.NotEmpty(t, msgs)
	model.Update(tea.BlurMsg{})
	require.True(t, state.busy, "blur must not drop the in-flight export")
	for _, resultMsg := range msgs {
		model.Update(resultMsg)
	}
	assert.False(t, state.busy)
	assert.Equal(t, recoveryStageMenu, state.stage)
	assert.Contains(t, model.View(), "Export completed")
	assert.Contains(t, model.View(), "blur.key")
	assert.Nil(t, state.material)
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80\n", string(data))
}

func TestRecoveryCommittedWarningReportedAsCreated(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryPrivateKey && action.export {
			state.selected = i
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	destination := filepath.Join(t.TempDir(), "warn.key")
	state.destination.SetValue(destination)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cmd := recoveryDriveToConfirm(t, model, "EXPORT")
	msgs := cmdResultMsgs(t, cmd)
	require.NotEmpty(t, msgs)
	result, ok := msgs[0].(recoveryResultMsg)
	require.True(t, ok)
	require.NoError(t, result.err, "real export must succeed before wrapping")
	require.FileExists(t, destination)

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, state.cancelling && state.exitAfterResult)
	model.Update(recoveryResultMsg{
		operationID: result.operationID, accountID: result.accountID, export: true,
		destination: result.destination, err: &wallet.ExportCommittedWarning{Cause: errors.New("dirsync failed")},
	})
	assert.Nil(t, model.recovery, "esc while busy exits once the result arrives")
	assert.Equal(t, constants.WalletDetailsView, model.currentView)
	assert.Contains(t, model.lastOperationNotice, "was created")
	assert.Contains(t, model.lastOperationNotice, "warning")
	assert.NotContains(t, model.lastOperationNotice, "cancelled")
	data, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80\n", string(data), "committed file contents untouched")
}

func TestRecoveryTypesJKIntoInputs(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStagePassword, state.stage)
	for _, r := range recoveryUITestPassword {
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	assert.Equal(t, recoveryUITestPassword, state.password.Value(), "password containing j/k must reach the input")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStageConfirmation, state.stage)
	for _, r := range "REVEAL" {
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	assert.Equal(t, "REVEAL", state.confirmation.Value())
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	recoveryDeliverResult(t, model, cmd)
	require.Equal(t, recoveryStageRevealed, state.stage)

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model.initRecovery()
	state = model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryPrivateKey && action.export {
			state.selected = i
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStageDestination, state.stage)
	destination := filepath.Join(t.TempDir(), "jk", "out.key")
	require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0700))
	for _, r := range destination {
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	assert.Equal(t, destination, state.destination.Value(), "destination containing j/k must reach the input")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range recoveryUITestPassword {
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "EXPORT" {
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	recoveryDeliverResult(t, model, cmd)
	assert.Contains(t, model.View(), "Export completed")
}

func TestRecoveryExportFlowWritesFile(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryMnemonic && action.export {
			state.selected = i
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStageDestination, state.stage)
	assert.Contains(t, model.View(), ".mnemonic")

	state.destination.SetValue("relative/path.mnemonic")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, recoveryStageDestination, state.stage)
	assert.Contains(t, model.View(), "absolute")

	destination := filepath.Join(t.TempDir(), "seed.mnemonic")
	state.destination.SetValue(destination)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStagePassword, state.stage)
	state.password.SetValue(recoveryUITestPassword)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStageConfirmation, state.stage)
	preview := model.View()
	assert.Contains(t, preview, "UNENCRYPTED")
	assert.Contains(t, strings.ReplaceAll(preview, "\n", ""), destination, "wrapped destination must reconstruct the full path")
	state.confirmation.SetValue("EXPORT")
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	recoveryDeliverResult(t, model, cmd)
	assert.Equal(t, recoveryStageMenu, state.stage)
	assert.Contains(t, model.View(), "Export completed")
	assert.Nil(t, state.material, "export must not route plaintext into the view")
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "test test test test test test test test test test test junk\n", string(content))
}

func TestRecoveryExportExistingDestinationRejected(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryPrivateKey && action.export {
			state.selected = i
		}
	}
	destination := filepath.Join(t.TempDir(), "existing.key")
	require.NoError(t, os.WriteFile(destination, []byte("keep"), 0600))
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.destination.SetValue(destination)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	cmd := recoveryDriveToConfirm(t, model, "EXPORT")
	recoveryDeliverResult(t, model, cmd)
	assert.Equal(t, recoveryStageDestination, state.stage, "existing destination must return to the destination field")
	assert.Contains(t, model.View(), "Destination exists")
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "keep", string(content))
}

func TestRecoveryRequiresReauthenticationPerOperation(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	cmd := recoveryDriveToConfirm(t, model, "REVEAL")
	recoveryDeliverResult(t, model, cmd)
	require.Equal(t, recoveryStageRevealed, state.stage)

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.Nil(t, model.recovery)
	model.initRecovery()
	state = model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryPrivateKey && action.export {
			state.selected = i
		}
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, recoveryStageDestination, state.stage)
	destination := filepath.Join(t.TempDir(), "out.key")
	state.destination.SetValue(destination)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, recoveryStagePassword, state.stage)
	state.password.SetValue("")
	state.confirmation.SetValue("EXPORT")
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd, "empty password must not dispatch even with prior reveal")
	_, statErr := os.Stat(destination)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRecoveryRendersBounded(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary, challenge, err := model.Vault.Create(context.Background(), wallet.CreateAccountRequest{
		Name:            strings.Repeat("N", 128),
		Password:        []byte(recoveryUITestPassword),
		WordCount:       24,
		BIP39Passphrase: "bounded test passphrase",
	})
	require.NoError(t, err)
	answers := make(map[int]string)
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	activated, err := model.Vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers)
	require.NoError(t, err)
	summary = activated
	model.selectedAccount = &summary
	longDestination := filepath.Join(t.TempDir(), strings.Repeat("segment-", 45), "out.mnemonic")
	require.Greater(t, len(longDestination), 350)

	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model.initRecovery()
		assertRecoveryViewFits(t, model, size[0], size[1])

		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		assertRecoveryViewFits(t, model, size[0], size[1])
		model.recovery.password.SetValue(recoveryUITestPassword)
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		assertRecoveryViewFits(t, model, size[0], size[1])
		model.recovery.confirmation.SetValue("REVEAL")
		_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		require.NotNil(t, cmd)
		recoveryDeliverResult(t, model, cmd)
		require.Equal(t, recoveryStageRevealed, model.recovery.stage)
		paged := recoveryPagedLines(t, model, size[0], size[1])
		flattened := strings.Join(paged, "\n")
		assert.Contains(t, flattened, "BIP39 passphrase is also required")
		previous := -1
		for index := 1; index <= 24; index++ {
			position := strings.Index(flattened, fmt.Sprintf("%d. ", index))
			require.GreaterOrEqual(t, position, 0, "word %d must be reachable", index)
			assert.Greater(t, position, previous, "word %d must appear after word %d", index, index-1)
			previous = position
		}
		model.Update(tea.KeyMsg{Type: tea.KeyEsc})

		model.initRecovery()
		state := model.recovery
		for i, action := range state.actions {
			if action.kind == wallet.RecoveryMnemonic && action.export {
				state.selected = i
			}
		}
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		assertRecoveryViewFits(t, model, size[0], size[1])
		state.destination.SetValue(longDestination)
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		assertRecoveryViewFits(t, model, size[0], size[1])
		state.password.SetValue(recoveryUITestPassword)
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		require.Equal(t, recoveryStageConfirmation, state.stage)

		paged = recoveryPagedLines(t, model, size[0], size[1])
		flattened = strings.ReplaceAll(strings.Join(paged, "\n"), "\n", "")
		assert.Contains(t, flattened, "UNENCRYPTED")
		assert.Contains(t, flattened, longDestination, "full destination path must be reachable via paging")
		model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	}
}

func TestRecoveryRevealedScrollingReachesAllContent(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	var builder strings.Builder
	for index := 0; index < 40; index++ {
		fmt.Fprintf(&builder, "\x01\x02\x03\x04\x05\x06\x07\x08tok%03d", index)
	}
	for builder.Len() < 980 {
		builder.WriteByte(0x10)
	}
	longPassphrase := builder.String() + "UNIQUE-TAIL-MARKER"
	require.LessOrEqual(t, len(longPassphrase), 1024, "fixture must stay inside the passphrase policy")
	summary, err := model.Vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name: strings.Repeat("P", 128), Mnemonic: "test test test test test test test test test test test junk",
		BIP39Passphrase: longPassphrase,
		StoragePassword: []byte(recoveryUITestPassword), ConfirmStoragePassword: []byte(recoveryUITestPassword),
	})
	require.NoError(t, err)
	model.selectedAccount = &summary
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model.initRecovery()
	state := model.recovery
	for i, action := range state.actions {
		if action.kind == wallet.RecoveryPassphrase && !action.export {
			state.selected = i
		}
	}
	cmd := recoveryDriveToConfirm(t, model, "REVEAL")
	recoveryDeliverResult(t, model, cmd)
	require.Equal(t, recoveryStageRevealed, state.stage)

	full := model.recoveryPageLayout()
	require.Greater(t, len(full.lines), 2*full.bodyHeight, "fixture must force at least 3 pages")

	covered := make([]bool, len(full.lines))
	previousEnd := 0
	for page := 0; page < 64; page++ {
		layout := model.recoveryPageLayout()
		assert.Equal(t, full.lines, layout.lines, "lines must be stable across paging")
		assertRecoveryViewFits(t, model, 80, 24)
		end := min(len(layout.lines), layout.offset+layout.bodyHeight)
		assert.LessOrEqual(t, layout.offset, previousEnd, "page %d must not skip lines", page)
		for index := layout.offset; index < end; index++ {
			covered[index] = true
		}
		previousEnd = end
		if end == len(layout.lines) {
			break
		}
		model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	for index, seen := range covered {
		assert.True(t, seen, "line %d must be reachable via paging", index)
	}
	paged := full.lines
	reconstructed := strings.ReplaceAll(strings.Join(paged, "\n"), "\n", "")
	assert.Contains(t, reconstructed, strings.ReplaceAll(strconv.Quote(longPassphrase), "\n", ""), "quoted passphrase must survive paging intact")
	assert.Contains(t, reconstructed, "UNIQUE-TAIL-MARKER")

	for state.scrollOffset > 0 {
		model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	}
	assert.Equal(t, 0, model.recoveryPageLayout().offset)
}

func recoveryPagedLines(t *testing.T, model *CLIModel, width, height int) []string {
	t.Helper()
	layout := model.recoveryPageLayout()
	covered := make([]bool, len(layout.lines))
	previousEnd := 0
	for page := 0; page < 64; page++ {
		layout := model.recoveryPageLayout()
		assertRecoveryViewFits(t, model, width, height)
		end := min(len(layout.lines), layout.offset+layout.bodyHeight)
		assert.LessOrEqual(t, layout.offset, previousEnd, "page %d must not skip lines", page)
		for index := layout.offset; index < end; index++ {
			covered[index] = true
		}
		previousEnd = end
		if end == len(layout.lines) {
			break
		}
		model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}
	for index, seen := range covered {
		assert.True(t, seen, "line %d must be reachable via paging", index)
	}
	for model.recovery.scrollOffset > 0 {
		model.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	}
	return layout.lines
}

func assertRecoveryViewFits(t *testing.T, model *CLIModel, width, height int) string {
	t.Helper()
	view := model.View()
	for _, line := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), width, "line overflows %d columns", width)
	}
	assert.LessOrEqual(t, lipgloss.Height(view), height, "view exceeds %d rows", height)
	return view
}

func TestRecoveryLeavesViewWipesMaterial(t *testing.T) {
	t.Run("view changed away", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		raw := model.recovery.material.Bytes()
		model.currentView = constants.ListWalletsView
		model.Update(clockTickMsg(time.Now()))
		assert.Nil(t, model.recovery)
		for _, b := range raw {
			assert.Zero(t, b, "material must be zeroed when the view navigates away")
		}
	})

	t.Run("selected account changed", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		raw := model.recovery.material.Bytes()
		other, err := model.Vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
			Name: "Other", Mnemonic: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			StoragePassword: []byte(recoveryUITestPassword), ConfirmStoragePassword: []byte(recoveryUITestPassword),
		})
		require.NoError(t, err)
		model.selectedAccount = &other
		model.Update(clockTickMsg(time.Now()))
		assert.Nil(t, model.recovery)
		for _, b := range raw {
			assert.Zero(t, b, "material must be zeroed when the selected account changes")
		}
	})

	t.Run("model error wipes", func(t *testing.T) {
		model := newRecoveryTestModel(t, wallet.AccountSummary{})
		summary := importRecoveryUIAccount(t, model)
		model.selectedAccount = &summary
		model.initRecovery()
		cmd := recoveryDriveToConfirm(t, model, "REVEAL")
		recoveryDeliverResult(t, model, cmd)
		raw := model.recovery.material.Bytes()
		model.err = errors.New("synthetic failure")
		model.Update(clockTickMsg(time.Now()))
		assert.Nil(t, model.recovery)
		for _, b := range raw {
			assert.Zero(t, b, "material must be zeroed when the view shows an error")
		}
	})
}

func TestRecoveryMismatchedMaterialRejected(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	cmd := recoveryDriveToConfirm(t, model, "REVEAL")
	msgs := cmdResultMsgs(t, cmd)
	require.NotEmpty(t, msgs)
	result, ok := msgs[0].(recoveryResultMsg)
	require.True(t, ok)
	require.NotNil(t, result.material)
	raw := result.material.Bytes()
	result.material.AccountID = "forged-account-id"
	model.Update(result)
	assert.Equal(t, recoveryStageMenu, state.stage)
	assert.Nil(t, state.material)
	for _, b := range raw {
		assert.Zero(t, b, "material with mismatched account id must be destroyed")
	}
	assert.NotContains(t, model.View(), "test test test")

	cmd = recoveryDriveToConfirm(t, model, "REVEAL")
	msgs = cmdResultMsgs(t, cmd)
	require.NotEmpty(t, msgs)
	result, ok = msgs[0].(recoveryResultMsg)
	require.True(t, ok)
	require.NotNil(t, result.material)
	raw = result.material.Bytes()
	result.material.Kind = wallet.RecoveryPrivateKey
	model.Update(result)
	assert.Equal(t, recoveryStageMenu, state.stage)
	assert.Nil(t, state.material)
	for _, b := range raw {
		assert.Zero(t, b, "material with mismatched kind must be destroyed")
	}
	assert.NotContains(t, model.View(), "test test test")
}

func TestRecoveryNilMaterialRevealResultHandled(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	summary := importRecoveryUIAccount(t, model)
	model.selectedAccount = &summary
	model.initRecovery()
	state := model.recovery
	recoveryDriveToConfirm(t, model, "REVEAL")
	require.True(t, state.busy)
	model.Update(recoveryResultMsg{operationID: state.operationID, accountID: summary.AccountID})
	assert.Equal(t, recoveryStageMenu, state.stage)
	assert.False(t, state.busy)
	assert.Contains(t, model.View(), "unavailable")
}

func TestRecoveryUnavailableForNonSoftwareSummaries(t *testing.T) {
	model := newRecoveryTestModel(t, wallet.AccountSummary{})
	for _, kind := range []wallet.SignerKind{wallet.SignerKindWatchOnly, wallet.SignerKindHardware, wallet.SignerKindCloud, wallet.SignerKindMultisig} {
		summary := wallet.AccountSummary{
			SignerKind: kind, State: wallet.AccountStateActive,
			Capabilities: wallet.CapabilityExportSecret | wallet.CapabilitySignTransaction,
		}
		assert.False(t, recoveryAvailable(&summary, model.Vault), "%s must not offer recovery", kind)
		model.selectedAccount = &summary
		model.initRecovery()
		assert.Nil(t, model.recovery, "%s must not enter recovery", kind)
	}
}

func TestRecoveryResultMessageRedaction(t *testing.T) {
	msg := recoveryResultMsg{operationID: 1, accountID: "acc", material: &wallet.RecoveryMaterial{AccountID: "acc"}}
	assert.NotContains(t, fmt.Sprintf("%v", msg), "test test")
	assert.Equal(t, "[redacted recovery result]", fmt.Sprintf("%v", msg))
	assert.Equal(t, "[redacted recovery result]", fmt.Sprintf("%#v", msg))
}
