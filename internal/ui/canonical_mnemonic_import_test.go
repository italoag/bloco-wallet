package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testMnemonicJunk    = "test test test test test test test test test test test junk"
	testMnemonicAbandon = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testAddressJunk     = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	testAddressAbandon  = "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
)

func writeMnemonicFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), content, 0600))
}

func TestImportMenuRoutesMnemonicBatch(t *testing.T) {
	menu := NewImportMenu()
	require.Len(t, menu, 8)

	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.currentView = constants.ImportMethodSelectionView
	for index, want := range []wallet.ImportMethod{
		wallet.ImportMethodMnemonic,
		wallet.ImportMethodPrivateKey,
		wallet.ImportMethodKeystore,
		canonicalBatchMethod,
		canonicalEncryptedMethod,
		wallet.ImportMethodWatchOnly,
		wallet.ImportMethod("mnemonic_batch"),
	} {
		model.selectedMenu = index
		_, _ = model.updateImportMethodSelection(tea.KeyMsg{Type: tea.KeyEnter})
		require.NotNil(t, model.canonicalImport, "index %d", index)
		assert.Equal(t, want, model.canonicalImport.method, "index %d", index)
		assert.Equal(t, constants.CanonicalImportView, model.currentView, "index %d", index)
		model.canonicalImport = nil
		model.currentView = constants.ImportMethodSelectionView
	}
	model.selectedMenu = 7
	_, _ = model.updateImportMethodSelection(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, constants.DefaultView, model.currentView)
}

func TestReadCanonicalMnemonicBatchFiltersAndSorts(t *testing.T) {
	root := t.TempDir()
	writeMnemonicFile(t, root, "b.mnemonic", []byte(testMnemonicJunk+"\n"))
	writeMnemonicFile(t, root, "a.seedphrase", []byte(testMnemonicAbandon))
	writeMnemonicFile(t, root, "C.MNEMONIC", []byte(testMnemonicJunk))
	writeMnemonicFile(t, root, "ignored.json", []byte("{}"))
	writeMnemonicFile(t, root, "ignored.pwd", []byte("nope"))
	writeMnemonicFile(t, root, "ignored.txt", []byte("nope"))
	require.NoError(t, os.Mkdir(filepath.Join(root, "subdir"), 0755))
	writeMnemonicFile(t, filepath.Join(root, "subdir"), "nested.mnemonic", []byte(testMnemonicJunk))
	if err := os.Symlink(filepath.Join(root, "b.mnemonic"), filepath.Join(root, "linked.mnemonic")); err != nil {
		t.Log("symlink unavailable", err)
	}

	items, err := readCanonicalMnemonicBatch(root)
	require.NoError(t, err)
	defer clearCanonicalMnemonicItems(items)
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	require.GreaterOrEqual(t, len(names), 3)
	assert.Equal(t, []string{"C", "a", "b"}, names[:3])
	assert.Equal(t, filepath.Join(root, "a.seedphrase"), items[1].SourcePath)
	if len(names) == 4 {
		assert.Equal(t, "linked", names[3])
		assert.ErrorIs(t, items[3].PreflightErr, errCanonicalBatchSourceRead)
	}
}

func TestReadCanonicalMnemonicBatchEmptyAndOversize(t *testing.T) {
	empty := t.TempDir()
	writeMnemonicFile(t, empty, "notes.txt", []byte("x"))
	_, err := readCanonicalMnemonicBatch(empty)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no mnemonic files")

	root := t.TempDir()
	writeMnemonicFile(t, root, "big.mnemonic", []byte(strings.Repeat("ab ", 7000)))
	items, err := readCanonicalMnemonicBatch(root)
	require.NoError(t, err)
	defer clearCanonicalMnemonicItems(items)
	require.Len(t, items, 1)
	assert.ErrorIs(t, items[0].PreflightErr, errCanonicalBatchSourceRead)

	_, err = readCanonicalMnemonicBatch("relative/dir")
	assert.Error(t, err)
	if link := filepath.Join(t.TempDir(), "linkdir"); os.Symlink(root, link) == nil {
		_, err = readCanonicalMnemonicBatch(link)
		assert.Error(t, err)
	}
}

func TestPreviewMnemonicFileImportNormalizes(t *testing.T) {
	preview, err := wallet.PreviewMnemonicFileImport([]byte("\xef\xbb\xbf  " + strings.ReplaceAll(testMnemonicJunk, " ", "  \r\n\t") + "\r\n"))
	require.NoError(t, err)
	assert.Equal(t, testAddressJunk, preview.Address)
	assert.Equal(t, "m/44'/60'/0'/0/0", preview.DerivationPath)

	preview, err = wallet.PreviewMnemonicFileImport([]byte(testMnemonicAbandon + "\r\n"))
	require.NoError(t, err)
	assert.Equal(t, testAddressAbandon, preview.Address)

	_, err = wallet.PreviewMnemonicFileImport(bytes0xFF())
	assert.Error(t, err)
	_, err = wallet.PreviewMnemonicFileImport(nil)
	assert.Error(t, err)
	_, err = wallet.PreviewMnemonicFileImport([]byte(strings.Repeat("a", wallet.MaxMnemonicImportBytes+1)))
	assert.Error(t, err)
	_, err = wallet.PreviewMnemonicFileImport([]byte(testMnemonicJunk + " " + testMnemonicJunk))
	assert.Error(t, err)
}

func bytes0xFF() []byte { return []byte{0xff, 0xfe, 0xfd} }

func driveMnemonicBatchFields(t *testing.T, model *CLIModel, state *canonicalImportState, source, password string) tea.Cmd {
	t.Helper()
	require.Len(t, state.fields, 3)
	state.fields[0].input.SetValue(source)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(password)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[2].input.SetValue(password)
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return cmd
}

func TestCanonicalMnemonicBatchFlowImportsAndLogs(t *testing.T) {
	vault, repository, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	writeMnemonicFile(t, source, "alpha.mnemonic", []byte(testMnemonicJunk))
	writeMnemonicFile(t, source, "beta.seedphrase", []byte(testMnemonicAbandon+"\r\n"))
	writeMnemonicFile(t, source, "gamma.mnemonic", []byte(testMnemonicJunk))
	writeMnemonicFile(t, source, "broken.mnemonic", []byte("zzsentinel9x not a phrase"))
	writeMnemonicFile(t, source, "alpha.mnemonic.pwd", []byte("malicious"))
	writeMnemonicFile(t, source, "alpha.pwd", []byte("malicious sibling"))
	writeMnemonicFile(t, source, "ignored.json", []byte("{}"))

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.currentView = constants.CanonicalImportView
	model.canonicalImport = newCanonicalImportState(canonicalMnemonicBatchMethod)
	state := model.canonicalImport
	assert.Equal(t, []string{"directory", "storage_password", "confirm_password"},
		[]string{state.fields[0].key, state.fields[1].key, state.fields[2].key})

	cmd := driveMnemonicBatchFields(t, model, state, source, "Strong vault pass 1!")
	require.NotNil(t, cmd)
	progress := drainCanonicalCmd(model, cmd)
	require.NotEmpty(t, progress)
	assert.Equal(t, "canonical_stage_validating", progress[0].stage)
	assert.Equal(t, 4, progress[0].progress.Total)
	require.NotNil(t, state.preview)
	require.Len(t, state.mnemonicItems, 4)
	require.Len(t, state.batchPreviews, 4)
	assert.Equal(t, testAddressJunk, state.batchPreviews[0].address)
	assert.Equal(t, testAddressAbandon, state.batchPreviews[1].address)
	assert.Equal(t, "Mnemonic validation failed", state.batchPreviews[2].err)
	assert.Equal(t, testAddressJunk, state.batchPreviews[3].address)

	require.Equal(t, []byte(testMnemonicJunk), state.mnemonicItems[0].Mnemonic)

	view := model.viewCanonicalImport()
	assert.Contains(t, view, "Authenticated batch preview")
	assert.Contains(t, view, "Derivation path: m/44'/60'/0'/0/0")
	assert.Contains(t, view, testAddressJunk)
	assert.Contains(t, view, testAddressAbandon)
	assert.NotContains(t, view, "sha256:")
	assert.NotContains(t, view, testMnemonicJunk)
	assert.NotContains(t, view, "zzsentinel9x")

	writeMnemonicFile(t, source, "alpha.mnemonic", []byte(testMnemonicAbandon))

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	commitProgress := drainCanonicalCmd(model, cmd)
	require.NotEmpty(t, commitProgress)
	assert.Equal(t, "canonical_stage_importing", commitProgress[0].stage)
	require.NotEmpty(t, state.resultLines)
	joined := strings.Join(state.resultLines, "\n")
	assert.Contains(t, joined, "Summary: 4 found, 4 processed, 2 imported, 1 already imported, 1 failed")
	assert.Contains(t, joined, "Mnemonic validation failed")
	assert.Contains(t, joined, "Failure log: ")
	assert.NotContains(t, joined, testMnemonicJunk)
	assert.NotContains(t, joined, "zzsentinel9x")
	assert.NotContains(t, joined, "Strong vault pass")

	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	var accountID string
	for _, account := range accounts {
		if account.Address == testAddressJunk {
			accountID = account.AccountID
		}
	}
	require.NotEmpty(t, accountID)
	handle, err := vault.Unlock(context.Background(), accountID, []byte("Strong vault pass 1!"))
	require.NoError(t, err)
	require.NoError(t, vault.Lock(handle))

	var logPath string
	for _, line := range state.resultLines {
		if strings.HasPrefix(line, "Failure log: ") {
			logPath = strings.TrimPrefix(line, "Failure log: ")
		}
	}
	require.NotEmpty(t, logPath)
	assert.True(t, strings.HasPrefix(logPath, cfg.AppDir))
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "zzsentinel9x")
	assert.NotContains(t, string(content), testMnemonicJunk)
	assert.True(t, strings.HasPrefix(filepath.Base(logPath), "mnemonic-import-failures-"))
	logLines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.GreaterOrEqual(t, len(logLines), 2)
	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(logLines[1]), &record))
	assert.Equal(t, "failure", record["type"])
	assert.Equal(t, filepath.Join(source, "broken.mnemonic"), record["path"])
	assert.Equal(t, "Mnemonic validation failed", record["reason"])
}

func TestCanonicalMnemonicBatchCommitsSnapshotAfterEdit(t *testing.T) {
	vault, repository, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	writeMnemonicFile(t, source, "only.mnemonic", []byte(testMnemonicJunk))

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.currentView = constants.CanonicalImportView
	model.canonicalImport = newCanonicalImportState(canonicalMnemonicBatchMethod)
	state := model.canonicalImport
	cmd := driveMnemonicBatchFields(t, model, state, source, "Strong vault pass 1!")
	drainCanonicalCmd(model, cmd)
	require.NotNil(t, state.preview)
	require.Len(t, state.batchPreviews, 1)
	assert.Equal(t, testAddressJunk, state.batchPreviews[0].address)

	writeMnemonicFile(t, source, "only.mnemonic", []byte(testMnemonicAbandon))

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	joined := strings.Join(state.resultLines, "\n")
	assert.Contains(t, joined, "1 imported, 0 already imported, 0 failed")
	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, testAddressJunk, accounts[0].Address)
}

func TestCanonicalMnemonicBatchAllSuccessNoLog(t *testing.T) {
	vault, repository, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	writeMnemonicFile(t, source, "one.mnemonic", []byte(testMnemonicJunk))
	writeMnemonicFile(t, source, "two.seedphrase", []byte(testMnemonicAbandon))

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.currentView = constants.CanonicalImportView
	model.canonicalImport = newCanonicalImportState(canonicalMnemonicBatchMethod)
	state := model.canonicalImport
	cmd := driveMnemonicBatchFields(t, model, state, source, "Strong vault pass 1!")
	drainCanonicalCmd(model, cmd)
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	joined := strings.Join(state.resultLines, "\n")
	assert.Contains(t, joined, "2 imported, 0 already imported, 0 failed")
	assert.NotContains(t, joined, "Failure log")
	matches, err := filepath.Glob(filepath.Join(cfg.AppDir, "mnemonic-import-failures-*.log"))
	require.NoError(t, err)
	assert.Empty(t, matches)
	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
}

func TestCanonicalMnemonicBatchCancelledCommitLogsFailures(t *testing.T) {
	vault, _, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	writeMnemonicFile(t, source, "one.mnemonic", []byte(testMnemonicJunk))
	items, err := readCanonicalMnemonicBatch(source)
	require.NoError(t, err)
	defer clearCanonicalMnemonicItems(items)
	digest := sha256.Sum256(items[0].Mnemonic)
	state := &canonicalImportState{
		method:        canonicalMnemonicBatchMethod,
		mnemonicItems: items,
		logDirectory:  cfg.AppDir,
		fields: []canonicalImportField{
			{key: "storage_password", input: textinput.New()},
			{key: "confirm_password", input: textinput.New()},
		},
	}
	state.fields[0].input.SetValue("Strong vault pass 1!")
	state.fields[1].input.SetValue("Strong vault pass 1!")
	state.batchPreviews = []canonicalBatchPreview{{name: items[0].Name, digest: hex.EncodeToString(digest[:])}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, lines, err := executeCanonicalImport(ctx, vault, state, nil, false)
	require.NoError(t, err)
	assert.Empty(t, summary.AccountID)
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "Import cancelled")
	assert.Contains(t, joined, "Failure log: ")
	assert.NotContains(t, joined, testMnemonicJunk)
	matches, globErr := filepath.Glob(filepath.Join(cfg.AppDir, "mnemonic-import-failures-*.log"))
	require.NoError(t, globErr)
	require.Len(t, matches, 1)
}

func TestCanonicalMnemonicBatchStaleAndZeroedBuffers(t *testing.T) {
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.currentView = constants.CanonicalImportView
	model.canonicalImport = newCanonicalImportState(canonicalMnemonicBatchMethod)
	state := model.canonicalImport
	secret := []byte(testMnemonicJunk)
	msg := canonicalPreviewResultMsg{
		operationID:   state.operationID + 99,
		mnemonicItems: []wallet.MnemonicBatchItem{{Name: "x", Mnemonic: secret}},
	}
	_, _ = model.Update(msg)
	for _, b := range secret {
		assert.Zero(t, b)
	}
	assert.Nil(t, state.mnemonicItems)

	model.canonicalImport = nil
	secret2 := []byte(testMnemonicAbandon)
	_, _ = model.Update(canonicalPreviewResultMsg{mnemonicItems: []wallet.MnemonicBatchItem{{Mnemonic: secret2}}})
	for _, b := range secret2 {
		assert.Zero(t, b)
	}
}
