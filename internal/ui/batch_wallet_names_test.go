package ui

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextBatchWalletNameDeterministicOffset(t *testing.T) {
	used := map[string]struct{}{}
	name, err := nextBatchWalletName(used, bytes.NewReader(make([]byte, 64)))
	require.NoError(t, err)
	assert.Equal(t, "Block-White", name)
	name, err = nextBatchWalletName(used, bytes.NewReader(make([]byte, 64)))
	require.NoError(t, err)
	assert.Equal(t, "Block-Black", name, "reserved name must be skipped")
}

func TestNextBatchWalletNameCasefoldReservation(t *testing.T) {
	used := map[string]struct{}{"block-white": {}}
	name, err := nextBatchWalletName(used, bytes.NewReader(make([]byte, 64)))
	require.NoError(t, err)
	assert.Equal(t, "Block-Black", name)
}

func TestNextBatchWalletNameExhaustion(t *testing.T) {
	used := make(map[string]struct{}, len(batchNameNouns)*len(batchNameAdjectives))
	for _, noun := range batchNameNouns {
		for _, adjective := range batchNameAdjectives {
			used[strings.ToLower(noun+"-"+adjective)] = struct{}{}
		}
	}
	_, err := nextBatchWalletName(used, bytes.NewReader(make([]byte, 64)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no unique")
}

func TestNextBatchWalletNameReaderFailure(t *testing.T) {
	_, err := nextBatchWalletName(map[string]struct{}{}, errReader{})
	require.Error(t, err)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, assert.AnError }

func TestBatchWalletNamesUniqueAcrossBatch(t *testing.T) {
	vault, _, _ := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	pattern := regexp.MustCompile(`^[A-Z][a-z]+-[A-Z][a-z]+$`)

	mnemonicState := &canonicalImportState{method: canonicalMnemonicBatchMethod}
	for index := 0; index < 100; index++ {
		mnemonicState.mnemonicItems = append(mnemonicState.mnemonicItems, wallet.MnemonicBatchItem{Name: "file.mnemonic", SourcePath: "file.mnemonic"})
	}
	require.NoError(t, assignCanonicalBatchNames(context.Background(), vault, mnemonicState))
	seen := make(map[string]struct{}, 100)
	for _, item := range mnemonicState.mnemonicItems {
		assert.Regexp(t, pattern, item.Name)
		_, exists := seen[item.Name]
		assert.False(t, exists, "duplicate generated name %q", item.Name)
		seen[item.Name] = struct{}{}
	}

	keystoreState := &canonicalImportState{method: canonicalBatchMethod}
	for index := 0; index < 100; index++ {
		keystoreState.batchItems = append(keystoreState.batchItems, wallet.KeystoreBatchItem{Name: "file.json", SourcePath: "file.json"})
	}
	require.NoError(t, assignCanonicalBatchNames(context.Background(), vault, keystoreState))
	seen = make(map[string]struct{}, 100)
	for _, item := range keystoreState.batchItems {
		assert.Regexp(t, pattern, item.Name)
		_, exists := seen[item.Name]
		assert.False(t, exists, "duplicate generated name %q", item.Name)
		seen[item.Name] = struct{}{}
	}
}

func TestBatchWalletNamesSkipExistingAccountNames(t *testing.T) {
	vault, _, _ := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	_, err := vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name: "Block-White", Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	require.NoError(t, err)
	state := &canonicalImportState{method: canonicalMnemonicBatchMethod}
	state.mnemonicItems = []wallet.MnemonicBatchItem{{Name: "a.mnemonic", SourcePath: "a.mnemonic"}}
	for i := 0; i < 20; i++ {
		state.mnemonicItems = append(state.mnemonicItems, wallet.MnemonicBatchItem{Name: "n.mnemonic", SourcePath: "n.mnemonic"})
	}
	require.NoError(t, assignCanonicalBatchNames(context.Background(), vault, state))
	for _, item := range state.mnemonicItems {
		assert.NotEqual(t, "Block-White", item.Name)
	}
}

func TestCanonicalBatchPreviewUsesFriendlyNames(t *testing.T) {
	vault, repository, cfg := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	source := t.TempDir()
	writeMnemonicFile(t, source, "one.mnemonic", []byte(testMnemonicJunk))
	writeMnemonicFile(t, source, "two.mnemonic", []byte(testMnemonicAbandon))

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.currentView = constants.CanonicalImportView
	model.canonicalImport = newCanonicalImportState(canonicalMnemonicBatchMethod)
	state := model.canonicalImport
	cmd := driveMnemonicBatchFields(t, model, state, source, "Strong vault pass 1!")
	drainCanonicalCmd(model, cmd)
	require.Len(t, state.batchPreviews, 2)
	pattern := regexp.MustCompile(`^[A-Z][a-z]+-[A-Z][a-z]+$`)
	previewNames := map[string]bool{}
	for _, preview := range state.batchPreviews {
		assert.Regexp(t, pattern, preview.name)
		assert.NotContains(t, preview.name, ".mnemonic")
		previewNames[preview.name] = true
	}
	require.Len(t, previewNames, 2)

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	for _, account := range accounts {
		assert.True(t, previewNames[account.Name], "stored name %q must come from preview names %v", account.Name, previewNames)
	}
}

func TestCanonicalKeystoreBatchPreviewUsesFriendlyNames(t *testing.T) {
	vault, repository, cfg := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	source := t.TempDir()
	writeTestKeystore(t, filepath.Join(source, "one.json"), "batch secret")
	require.NoError(t, os.WriteFile(filepath.Join(source, "one.pwd"), []byte("batch secret\n"), 0600))

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.canonicalImport = newCanonicalImportState(canonicalBatchMethod)
	state := model.canonicalImport
	state.fields[0].input.SetValue(source)
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue("Strong batch password 1!")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[2].input.SetValue("Strong batch password 1!")
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	drainCanonicalCmd(model, cmd)

	require.Len(t, state.batchPreviews, 1)
	pattern := regexp.MustCompile(`^[A-Z][a-z]+-[A-Z][a-z]+$`)
	previewName := state.batchPreviews[0].name
	previewAddress := state.batchPreviews[0].address
	assert.Regexp(t, pattern, previewName)
	assert.NotEqual(t, "one", previewName)
	assert.NotEmpty(t, previewAddress)

	_, cmd = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, previewAddress, accounts[0].Address)
	assert.Equal(t, previewName, accounts[0].Name)

	state = newCanonicalImportState(canonicalBatchMethod)
	model.canonicalImport = state
	state.fields[0].input.SetValue(source)
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue("Strong batch password 1!")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[2].input.SetValue("Strong batch password 1!")
	_, cmd = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	_, cmd = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	joined := strings.Join(state.resultLines, "\n")
	assert.Contains(t, joined, "already imported")
	accounts, err = repository.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, previewName, accounts[0].Name, "reimport must not rename the existing account")
}

func TestManualImportNameUntouchedByFriendlyNames(t *testing.T) {
	vault, _, _ := newCanonicalTestVault(t)
	t.Cleanup(vault.Close)
	summary, err := vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name: "My Explicit Wallet", Mnemonic: testMnemonicJunk,
		StoragePassword: []byte("manual-import-pass-1!"), ConfirmStoragePassword: []byte("manual-import-pass-1!"),
	})
	require.NoError(t, err)
	assert.Equal(t, "My Explicit Wallet", summary.Name)
}
