package ui

import (
	"bytes"
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
	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestKeystore(t *testing.T, path, password string) []byte {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	encoded, err := keystore.EncryptKey(&keystore.Key{
		Id:         uuid.New(),
		Address:    crypto.PubkeyToAddress(key.PublicKey),
		PrivateKey: key,
	}, password, keystore.LightScryptN, keystore.LightScryptP)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encoded, 0600))
	return encoded
}

func newCanonicalTestVault(t *testing.T) (*wallet.WalletVault, *storage.GORMRepository, *config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		WalletsDir:   filepath.Join(root, "keystore"),
		DatabasePath: filepath.Join(root, "vault.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64,
			Argon2Threads: 1,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
		Language:  "en",
		LocaleDir: "../../pkg/localization/locales",
	}
	require.NoError(t, localization.InitLocalization(cfg))
	repository, err := storage.NewVaultRepository(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repository.Close()) })
	codec, err := wallet.NewSecretEnvelopeCodec(wallet.Argon2idPolicy{
		Time: 1, MemoryKiB: 64, Parallelism: 1, KeyLength: 32, SaltLength: 16,
		MaxTime: 4, MaxMemoryKiB: 256 * 1024, MaxParallelism: 8, MaxKeyLength: 32, MaxSaltLength: 64,
	})
	require.NoError(t, err)
	vault, err := wallet.NewWalletVault(repository, codec, wallet.VaultOptions{ChallengeWords: 3, SourceIdentityKey: bytes.Repeat([]byte{0x42}, 32)})
	require.NoError(t, err)
	return vault, repository, cfg
}

func TestReadCanonicalKeystoreBatchUsesRegularJSONFiles(t *testing.T) {
	root := t.TempDir()
	password := []byte("source password")
	writeTestKeystore(t, filepath.Join(root, "a.json"), string(password))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.pwd"), append(append([]byte(nil), password...), '\n'), 0600))
	writeTestKeystore(t, filepath.Join(root, "b.json"), "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("ignored"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.password"), []byte("wrong"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.json.pwd"), []byte("wrong"), 0600))
	items, err := readCanonicalKeystoreBatch(root)
	require.NoError(t, err)
	defer clearCanonicalBatchItems(items)
	require.Len(t, items, 2)
	assert.Equal(t, "a", items[0].Name)
	assert.Equal(t, filepath.Join(root, "a.json"), items[0].SourcePath)
	assert.Equal(t, password, items[0].SourcePassword)
	assert.Equal(t, "b", items[1].Name)
	assert.Empty(t, items[1].SourcePassword)
	for _, item := range items {
		require.Nil(t, item.PreflightErr)
		_, err := wallet.PreviewKeystoreImport(item.KeystoreJSON, item.SourcePassword)
		assert.NoError(t, err)
	}
}

func TestReadCanonicalKeystoreBatchFlagsMalformedAndEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "bad.json"), []byte("{not json"), 0600))
	items, err := readCanonicalKeystoreBatch(root)
	require.NoError(t, err)
	defer clearCanonicalBatchItems(items)
	require.Len(t, items, 1)
	require.Nil(t, items[0].PreflightErr)
	_, err = wallet.PreviewKeystoreImport(items[0].KeystoreJSON, items[0].SourcePassword)
	assert.Error(t, err)

	empty := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(empty, "notes.txt"), []byte("x"), 0600))
	_, err = readCanonicalKeystoreBatch(empty)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no JSON keystore files")
}

func TestReadCanonicalPasswordFileSemantics(t *testing.T) {
	root := t.TempDir()
	dir, err := openPathNoFollow(root, true)
	require.NoError(t, err)
	defer func() { _ = dir.Close() }()

	write := func(name string, content []byte) {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), content, 0600))
	}

	write("lf.pwd", []byte("secret\n"))
	password, found, err := readCanonicalPasswordFile(dir, "lf.json")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("secret"), password)

	write("crlf.pwd", []byte("secret\r\n"))
	password, found, err = readCanonicalPasswordFile(dir, "crlf.json")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("secret"), password)

	write("spaces.pwd", []byte("  padded  \n"))
	password, found, err = readCanonicalPasswordFile(dir, "spaces.json")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("  padded  "), password)

	write("nonewline.pwd", []byte("exact"))
	password, found, err = readCanonicalPasswordFile(dir, "nonewline.json")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("exact"), password)

	write("empty.pwd", nil)
	password, found, err = readCanonicalPasswordFile(dir, "empty.json")
	require.NoError(t, err)
	require.True(t, found)
	assert.Empty(t, password)

	write("twolines.pwd", []byte("first\nsecond\n"))
	password, found, err = readCanonicalPasswordFile(dir, "twolines.json")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("first\nsecond"), password)

	password, found, err = readCanonicalPasswordFile(dir, "absent.json")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, password)

	write("aliasonly.json.pwd", []byte("alias"))
	write("aliasonly.password", []byte("alias"))
	_, found, err = readCanonicalPasswordFile(dir, "aliasonly.json")
	require.NoError(t, err)
	assert.False(t, found)

	write("oversize.pwd", bytes.Repeat([]byte("x"), 4097))
	_, found, err = readCanonicalPasswordFile(dir, "oversize.json")
	require.Error(t, err)
	assert.True(t, found)

	require.NoError(t, os.Mkdir(filepath.Join(root, "nonregular.pwd"), 0700))
	_, _, err = readCanonicalPasswordFile(dir, "nonregular.json")
	assert.Error(t, err)

	target := filepath.Join(root, "lf.pwd")
	require.NoError(t, os.WriteFile(filepath.Join(root, "real.pwd"), []byte("x"), 0600))
	link := filepath.Join(root, "linked.pwd")
	if err := os.Symlink(target, link); err == nil {
		_, _, err = readCanonicalPasswordFile(dir, "linked.json")
		assert.Error(t, err)
	}
}

func TestCanonicalImportViewSanitizesUntrustedResults(t *testing.T) {
	model := &CLIModel{canonicalImport: &canonicalImportState{
		resultLines: []string{"evil\x1b]52;c;secret\x07\r\nnext"},
	}}
	view := model.viewCanonicalImport()
	if strings.ContainsAny(view, "\x1b\a\r") || strings.Contains(view, "\u009b") {
		t.Fatalf("canonical import view retained terminal controls: %q", view)
	}
}

func TestReadCanonicalKeystoreBatchRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(root, link); err == nil {
		if _, err := readCanonicalKeystoreBatch(link); err == nil {
			t.Fatal("symlink batch directory was accepted")
		}
	}
}

func TestCanonicalBatchFieldsAskOnlyDirectoryAndVaultPassword(t *testing.T) {
	state := newCanonicalImportState(canonicalBatchMethod)
	keys := make([]string, 0, len(state.fields))
	for _, field := range state.fields {
		keys = append(keys, field.key)
	}
	assert.Equal(t, []string{"directory", "storage_password", "confirm_password"}, keys)
}

func TestCanonicalKeystorePathSidecarSkipsSourcePassword(t *testing.T) {
	root := t.TempDir()
	encoded := writeTestKeystore(t, filepath.Join(root, "key.json"), "sidecar secret")
	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), []byte("sidecar secret\n"), 0600))
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("From file")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(filepath.Join(root, "key.json"))
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.True(t, state.busy)
	msg := cmd()
	result, ok := msg.(canonicalSourcePasswordMsg)
	require.True(t, ok)
	require.NoError(t, result.err)
	require.True(t, result.found)
	_, _ = model.updateCanonicalImport(result)
	require.False(t, state.busy)
	assert.True(t, state.sourcePasswordFromFile)
	assert.Equal(t, 3, state.stage)
	assert.Equal(t, "storage_password", state.fields[state.stage].key)
	assert.Equal(t, "", state.fields[2].input.Value())

	password := canonicalSourcePassword(state)
	preview, err := wallet.PreviewKeystoreImport(encoded, password)
	require.NoError(t, err)
	assert.NotEmpty(t, preview.Address)
}

func TestCanonicalKeystoreWithoutSidecarKeepsManualPrompt(t *testing.T) {
	root := t.TempDir()
	writeTestKeystore(t, filepath.Join(root, "key.json"), "manual")
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("Manual")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(filepath.Join(root, "key.json"))
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	result, ok := msg.(canonicalSourcePasswordMsg)
	require.True(t, ok)
	require.False(t, result.found)
	_, _ = model.updateCanonicalImport(result)
	assert.Equal(t, 2, state.stage)
	assert.Equal(t, "source_password", state.fields[state.stage].key)
	assert.False(t, state.sourcePasswordFromFile)
}

func TestCanonicalKeystoreEmptySidecarSkipsSourcePassword(t *testing.T) {
	root := t.TempDir()
	writeTestKeystore(t, filepath.Join(root, "key.json"), "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), nil, 0600))
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("Empty sidecar")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(filepath.Join(root, "key.json"))
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = model.updateCanonicalImport(cmd())
	assert.Equal(t, 3, state.stage)
	assert.True(t, state.sourcePasswordFromFile)
}

func TestCanonicalKeystoreSidecarErrorStaysOnPath(t *testing.T) {
	root := t.TempDir()
	writeTestKeystore(t, filepath.Join(root, "key.json"), "x")
	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), bytes.Repeat([]byte("x"), 5000), 0600))
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("Broken")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(filepath.Join(root, "key.json"))
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = model.updateCanonicalImport(cmd())
	assert.Equal(t, 1, state.stage)
	assert.Equal(t, "keystore_path", state.fields[state.stage].key)
	assert.NotEmpty(t, state.err)
	assert.NotContains(t, model.viewCanonicalImport(), "x")
}

func TestCanonicalKeystorePathRejectsNonAbsolute(t *testing.T) {
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("Name")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue("relative/key.json")
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.Equal(t, 1, state.stage)
	assert.NotEmpty(t, state.err)
}

func TestCanonicalBatchFlowImportsAndLogsFailures(t *testing.T) {
	vault, repository, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	writeTestKeystore(t, filepath.Join(source, "one.json"), "batch secret")
	require.NoError(t, os.WriteFile(filepath.Join(source, "one.pwd"), []byte("batch secret\n"), 0600))
	writeTestKeystore(t, filepath.Join(source, "two.json"), "")
	require.NoError(t, os.WriteFile(filepath.Join(source, "broken.json"), []byte("{corrupt ZZ_SENTINEL_9x"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "notes.txt"), []byte("nope"), 0600))

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.canonicalImport = newCanonicalImportState(canonicalBatchMethod)
	state := model.canonicalImport
	require.Len(t, state.fields, 3)
	state.fields[0].input.SetValue(source)
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue("Strong batch password 1!")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[2].input.SetValue("Strong batch password 1!")
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	require.True(t, state.busy)

	previewProgress := drainCanonicalCmd(model, cmd)
	require.NotEmpty(t, previewProgress)
	assert.Equal(t, "Validating", previewProgress[0].stage)
	assert.Equal(t, 3, previewProgress[0].progress.Total)
	require.False(t, state.busy)
	require.NotNil(t, state.preview)
	assert.Len(t, state.batchPreviews, 3)
	view := model.viewCanonicalImport()
	assert.Contains(t, view, "Authenticated batch preview")
	assert.Contains(t, view, "keystore validation or decryption failed")

	_, cmd = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	commitProgress := drainCanonicalCmd(model, cmd)
	require.NotEmpty(t, commitProgress)
	assert.Equal(t, "Importing", commitProgress[0].stage)
	assert.Equal(t, 3, commitProgress[len(commitProgress)-1].progress.Completed)
	require.False(t, state.busy)
	require.NotEmpty(t, state.resultLines)
	joined := strings.Join(state.resultLines, "\n")
	assert.Contains(t, joined, "imported")
	assert.Contains(t, joined, "keystore validation or decryption failed")
	assert.Contains(t, joined, "Failure log: ")
	assert.Contains(t, joined, "Summary: 3 found, 3 processed, 2 imported, 0 already imported, 1 failed")
	assert.NotContains(t, joined, "batch secret")
	assert.NotContains(t, joined, "ZZ_SENTINEL_9x")

	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 2)

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
	assert.True(t, info.Mode().IsRegular())
	content, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.NotContains(t, string(content), "batch secret")
	assert.NotContains(t, string(content), "ZZ_SENTINEL_9x")
	var summary map[string]any
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.GreaterOrEqual(t, len(lines), 2)
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &summary))
	assert.Equal(t, "summary", summary["type"])
	var record map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &record))
	assert.Equal(t, "failure", record["type"])
	assert.Equal(t, filepath.Join(source, "broken.json"), record["path"])
	assert.Equal(t, "keystore validation or decryption failed", record["reason"])
}

func TestCanonicalPathSuggestions(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "sub dir"), 0755))
	writeTestKeystore(t, filepath.Join(root, "one.json"), "x")
	require.NoError(t, os.WriteFile(filepath.Join(root, "two.txt"), []byte("x"), 0600))
	if err := os.Symlink(filepath.Join(root, "one.json"), filepath.Join(root, "link.json")); err != nil {
		t.Log("symlink unavailable", err)
	}
	if runtime.GOOS != "windows" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "ctl\x01.json"), []byte("x"), 0600))
	}

	suggestions, err := canonicalPathSuggestions(root+string(os.PathSeparator), false)
	require.NoError(t, err)
	joined := strings.Join(suggestions, "|")
	assert.Contains(t, joined, "sub dir"+string(os.PathSeparator))
	assert.Contains(t, joined, "one.json")
	assert.NotContains(t, joined, "two.txt")
	assert.NotContains(t, joined, "link.json")
	assert.NotContains(t, joined, "ctl")

	suggestions, err = canonicalPathSuggestions(filepath.Join(root, "o"), false)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(root, "one.json")}, suggestions)

	suggestions, err = canonicalPathSuggestions(root+string(os.PathSeparator), true)
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(root, "sub dir") + string(os.PathSeparator)}, suggestions)

	homeRoot := t.TempDir()
	writeTestKeystore(t, filepath.Join(homeRoot, "home.json"), "x")
	t.Setenv("HOME", homeRoot)
	t.Setenv("USERPROFILE", homeRoot)
	suggestions, err = canonicalPathSuggestions("~/h", false)
	require.NoError(t, err)
	require.Equal(t, []string{"~/home.json"}, suggestions)
}

func TestCanonicalSuggestionsStaleGenerationIgnored(t *testing.T) {
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	state := newCanonicalImportState(canonicalBatchMethod)
	model.canonicalImport = state
	state.fields[0].input.SetValue("/tmp")
	cmd := state.canonicalSuggestCmd()
	require.NotNil(t, cmd)
	msg := cmd()
	stale := msg.(canonicalPathSuggestionsMsg)
	stale.generation++
	_, _ = model.updateCanonicalImport(stale)
	assert.Empty(t, state.fields[0].input.AvailableSuggestions())
	fresh := msg.(canonicalPathSuggestionsMsg)
	_, _ = model.updateCanonicalImport(fresh)
	assert.NotPanics(t, func() { _ = state.fields[0].input.AvailableSuggestions() })
}

func TestCanonicalPathSuggestionsTildeDrillsIntoHome(t *testing.T) {
	homeRoot := t.TempDir()
	writeTestKeystore(t, filepath.Join(homeRoot, "alpha.json"), "x")
	require.NoError(t, os.Mkdir(filepath.Join(homeRoot, "nested"), 0755))
	writeTestKeystore(t, filepath.Join(homeRoot, "nested", "inner.json"), "x")
	t.Setenv("HOME", homeRoot)
	t.Setenv("USERPROFILE", homeRoot)

	suggestions, err := canonicalPathSuggestions("~/", false)
	require.NoError(t, err)
	assert.Contains(t, suggestions, "~/alpha.json")
	assert.Contains(t, suggestions, "~/nested/")

	suggestions, err = canonicalPathSuggestions("~/nested/", false)
	require.NoError(t, err)
	assert.Equal(t, []string{"~/nested/inner.json"}, suggestions)

	suggestions, err = canonicalPathSuggestions("~/nested/", true)
	require.NoError(t, err)
	assert.Empty(t, suggestions)
}

func TestCanonicalTabAcceptanceDrillsDirectories(t *testing.T) {
	homeRoot := t.TempDir()
	writeTestKeystore(t, filepath.Join(homeRoot, "aardvark.json"), "x")
	writeTestKeystore(t, filepath.Join(homeRoot, "wallet.json"), "x")
	require.NoError(t, os.Mkdir(filepath.Join(homeRoot, "vaults"), 0755))
	writeTestKeystore(t, filepath.Join(homeRoot, "vaults", "deep.json"), "x")
	t.Setenv("HOME", homeRoot)
	t.Setenv("USERPROFILE", homeRoot)

	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	state := newCanonicalImportState(wallet.ImportMethodKeystore)
	model.canonicalImport = state
	state.stage = 1
	pathField := &state.fields[state.stage]
	require.Equal(t, "keystore_path", pathField.key)
	pathField.input.Focus()
	pathField.input.SetValue("~/")

	suggestCmd := state.canonicalSuggestCmd()
	require.NotNil(t, suggestCmd)
	_, _ = model.updateCanonicalImport(suggestCmd())
	suggestions := pathField.input.AvailableSuggestions()
	require.Contains(t, suggestions, "~/aardvark.json")
	require.Contains(t, suggestions, "~/wallet.json")
	require.Contains(t, suggestions, "~/vaults/")

	for range len(suggestions) {
		if pathField.input.CurrentSuggestion() == "~/vaults/" {
			break
		}
		_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyDown})
	}
	require.Equal(t, "~/vaults/", pathField.input.CurrentSuggestion())
	_, tabCmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "~/vaults/", pathField.input.Value())

	require.NotNil(t, tabCmd)
	delivered := false
	switch produced := tabCmd().(type) {
	case tea.BatchMsg:
		for _, inner := range produced {
			if inner == nil {
				continue
			}
			if suggestionMsg, ok := inner().(canonicalPathSuggestionsMsg); ok {
				delivered = true
				_, _ = model.updateCanonicalImport(suggestionMsg)
			}
		}
	case canonicalPathSuggestionsMsg:
		delivered = true
		_, _ = model.updateCanonicalImport(produced)
	}
	require.True(t, delivered)
	assert.Equal(t, []string{"~/vaults/deep.json"}, pathField.input.AvailableSuggestions())
}

func TestCanonicalSourcePasswordCancelBeforeExec(t *testing.T) {
	root := t.TempDir()
	writeTestKeystore(t, filepath.Join(root, "key.json"), "x")
	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), []byte("top secret"), 0600))
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("Name")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(filepath.Join(root, "key.json"))
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	state.cancel()
	msg := cmd()
	result, ok := msg.(canonicalSourcePasswordMsg)
	require.True(t, ok)
	require.Error(t, result.err)
	assert.Nil(t, result.password)
}

func TestCanonicalSourcePasswordCancelledMessageDoesNotAdvance(t *testing.T) {
	root := t.TempDir()
	writeTestKeystore(t, filepath.Join(root, "key.json"), "x")
	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), []byte("captured secret"), 0600))
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.currentView = constants.CanonicalImportView
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("Name")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(filepath.Join(root, "key.json"))
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	captured := cmd().(canonicalSourcePasswordMsg)
	require.True(t, captured.found)
	require.NotEmpty(t, captured.password)

	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.True(t, state.cancelling)
	_, _ = model.Update(captured)
	assert.Equal(t, 1, state.stage)
	assert.Equal(t, "keystore_path", state.fields[state.stage].key)
	assert.False(t, state.sourcePasswordFromFile)
	assert.Nil(t, state.sourcePassword)
	for _, b := range captured.password {
		assert.Zero(t, b)
	}
	assert.NotContains(t, model.viewCanonicalImport(), "captured secret")
}

func drainCanonicalCmd(model *CLIModel, cmd tea.Cmd) []canonicalImportProgressMsg {
	var progress []canonicalImportProgressMsg
	var walk func(c tea.Cmd, depth int)
	walk = func(c tea.Cmd, depth int) {
		if c == nil || depth > 1024 {
			return
		}
		msg := c()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, inner := range batch {
				walk(inner, depth+1)
			}
			return
		}
		if progressMsg, ok := msg.(canonicalImportProgressMsg); ok {
			progress = append(progress, progressMsg)
		}
		_, next := model.updateCanonicalImport(msg)
		walk(next, depth+1)
	}
	walk(cmd, 0)
	return progress
}

func TestCanonicalKeystoreSidecarEndToEndCommit(t *testing.T) {
	vault, repository, _ := newCanonicalTestVault(t)
	root := t.TempDir()
	writeTestKeystore(t, filepath.Join(root, "key.json"), "sidecar secret")
	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), []byte("sidecar secret\n"), 0600))

	model := &CLIModel{Vault: vault, styles: createStyles()}
	model.currentView = constants.CanonicalImportView
	model.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("End to end")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue(filepath.Join(root, "key.json"))
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = model.Update(cmd())
	require.Equal(t, 3, state.stage)
	assert.NotContains(t, model.viewCanonicalImport(), "sidecar secret")

	state.fields[3].input.SetValue("New vault password 1!")
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[4].input.SetValue("New vault password 1!")
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	_, _ = model.Update(cmd())
	require.NotNil(t, state.preview)
	previewAddress := state.preview.Address

	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), []byte("changed"), 0600))
	require.NoError(t, os.Remove(filepath.Join(root, "key.pwd")))
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	_, _ = model.Update(cmd())

	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, previewAddress, model.selectedAccount.Address)
	stored, err := repository.GetAccount(context.Background(), model.selectedAccount.AccountID)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.SecretEnvelope)
	handle, err := vault.Unlock(context.Background(), model.selectedAccount.AccountID, []byte("New vault password 1!"))
	require.NoError(t, err)
	require.NoError(t, vault.Lock(handle))
}

func TestCanonicalBatchFailureLogWriteFailureIsNonFatal(t *testing.T) {
	vault, repository, cfg := newCanonicalTestVault(t)
	blockingFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(blockingFile, []byte("x"), 0600))
	cfg.AppDir = blockingFile

	source := t.TempDir()
	writeTestKeystore(t, filepath.Join(source, "ok.json"), "")
	require.NoError(t, os.WriteFile(filepath.Join(source, "bad.json"), []byte("{broken"), 0600))

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.canonicalImport = newCanonicalImportState(canonicalBatchMethod)
	state := model.canonicalImport
	state.fields[0].input.SetValue(source)
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue("Strong batch password 1!")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[2].input.SetValue("Strong batch password 1!")
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	require.NotNil(t, state.preview)
	_, cmd = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	joined := strings.Join(state.resultLines, "\n")
	assert.Contains(t, joined, "WARNING: failure log could not be written")
	assert.Contains(t, joined, "1 imported")
	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 1)
}

func TestCanonicalBatchReadOnlySourceStillWritesLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on Windows")
	}
	vault, _, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "bad.json"), []byte("{broken"), 0600))
	require.NoError(t, os.Chmod(source, 0o555))
	t.Cleanup(func() { _ = os.Chmod(source, 0o755) })

	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.canonicalImport = newCanonicalImportState(canonicalBatchMethod)
	state := model.canonicalImport
	state.fields[0].input.SetValue(source)
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue("Strong batch password 1!")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[2].input.SetValue("Strong batch password 1!")
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	_, cmd = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	joined := strings.Join(state.resultLines, "\n")
	assert.Contains(t, joined, "Failure log: ")
	matches, err := filepath.Glob(filepath.Join(cfg.AppDir, "keystore-import-failures-*.log"))
	require.NoError(t, err)
	assert.NotEmpty(t, matches)
}

func TestCanonicalBatchAllSuccessWritesNoFailureLog(t *testing.T) {
	vault, _, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	writeTestKeystore(t, filepath.Join(source, "ok.json"), "")
	model := &CLIModel{Vault: vault, styles: createStyles(), balanceConfig: cfg}
	model.canonicalImport = newCanonicalImportState(canonicalBatchMethod)
	state := model.canonicalImport
	state.fields[0].input.SetValue(source)
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[1].input.SetValue("Strong batch password 1!")
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	state.fields[2].input.SetValue("Strong batch password 1!")
	_, cmd := model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	_, cmd = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	drainCanonicalCmd(model, cmd)
	joined := strings.Join(state.resultLines, "\n")
	assert.NotContains(t, joined, "Failure log")
	matches, err := filepath.Glob(filepath.Join(cfg.AppDir, "keystore-import-failures-*.log"))
	require.NoError(t, err)
	assert.Empty(t, matches)
}

func TestCanonicalBatchCancelledCommitLogsFailures(t *testing.T) {
	_, _, cfg := newCanonicalTestVault(t)
	source := t.TempDir()
	encoded := writeTestKeystore(t, filepath.Join(source, "one.json"), "")
	digest := sha256.Sum256(encoded)
	items, err := readCanonicalKeystoreBatch(source)
	require.NoError(t, err)
	defer clearCanonicalBatchItems(items)
	state := &canonicalImportState{
		method:       canonicalBatchMethod,
		batchItems:   items,
		logDirectory: cfg.AppDir,
		fields: []canonicalImportField{
			{key: "storage_password", input: textinput.New()},
			{key: "confirm_password", input: textinput.New()},
		},
	}
	state.fields[0].input.SetValue("Strong batch password 1!")
	state.fields[1].input.SetValue("Strong batch password 1!")
	state.batchPreviews = []canonicalBatchPreview{{name: items[0].Name, digest: hex.EncodeToString(digest[:])}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	summary, lines, err := executeCanonicalImport(ctx, nil, state)
	require.NoError(t, err)
	assert.Empty(t, summary.AccountID)
	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "import cancelled")
	assert.Contains(t, joined, "Failure log: ")
	matches, globErr := filepath.Glob(filepath.Join(cfg.AppDir, "keystore-import-failures-*.log"))
	require.NoError(t, globErr)
	require.Len(t, matches, 1)
	content, readErr := os.ReadFile(matches[0])
	require.NoError(t, readErr)
	assert.Contains(t, string(content), "import cancelled")
}

func TestCanonicalBatchBusyViewRendersProgress(t *testing.T) {
	model := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	model.canonicalImport = newCanonicalImportState(canonicalBatchMethod)
	state := model.canonicalImport
	state.busy = true
	state.events = make(chan tea.Msg, 4)
	state.operationID = 7
	_, next := model.updateCanonicalImport(canonicalImportProgressMsg{
		operationID: 7,
		stage:       "Importing",
		progress:    wallet.KeystoreBatchProgress{Total: 5, Completed: 2, Imported: 1, AlreadyImported: 1},
	})
	require.NotNil(t, next)
	view := model.viewCanonicalImport()
	assert.Contains(t, view, "Found 5")
	assert.Contains(t, view, "Processed 2/5")
	assert.Contains(t, view, "Imported 1")
	assert.Contains(t, view, "Already imported 1")
	assert.Contains(t, view, "Failed 0")
}

func TestCanonicalSourcePasswordMsgDiscardedCleanup(t *testing.T) {
	secret := []byte("discard me")

	nilState := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	nilState.currentView = constants.CanonicalImportView
	_, _ = nilState.Update(canonicalSourcePasswordMsg{operationID: 1, password: secret, found: true})
	for _, b := range secret {
		assert.Zero(t, b)
	}

	secret = []byte("discard me")
	otherView := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	otherView.currentView = constants.WalletDetailsView
	otherView.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	otherView.canonicalImport.operationID = 1
	_, _ = otherView.Update(canonicalSourcePasswordMsg{operationID: 1, password: secret, found: true})
	for _, b := range secret {
		assert.Zero(t, b)
	}
	assert.Equal(t, 0, otherView.canonicalImport.stage)
	assert.Nil(t, otherView.canonicalImport.sourcePassword)

	secret = []byte("discard me")
	stale := &CLIModel{Vault: &wallet.WalletVault{}, styles: createStyles()}
	stale.currentView = constants.CanonicalImportView
	stale.canonicalImport = newCanonicalImportState(wallet.ImportMethodKeystore)
	stale.canonicalImport.operationID = 99
	_, _ = stale.Update(canonicalSourcePasswordMsg{operationID: 1, password: secret, found: true})
	for _, b := range secret {
		assert.Zero(t, b)
	}
	assert.Equal(t, 0, stale.canonicalImport.stage)
	assert.Nil(t, stale.canonicalImport.sourcePassword)
	assert.False(t, stale.canonicalImport.sourcePasswordFromFile)
}
