package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/keepass"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/common"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCredentialUITestModel(t *testing.T, enabled bool) (*CLIModel, *wallet.WalletVault, *keepass.Store, keepass.Binding) {
	t.Helper()
	vault, _, cfg := newCanonicalTestVault(t)
	store := keepass.NewStore(keepass.Options{})
	service, err := wallet.NewCredentialBackupService(vault, store)
	require.NoError(t, err)
	t.Cleanup(service.Close)
	var binding keepass.Binding
	if enabled {
		master := []byte("test master password 1234")
		binding, err = store.Create(context.Background(), filepath.Join(cfg.AppDir, "vault.kdbx"), mustCredentialVaultID(t, vault), master)
		clear(master)
		require.NoError(t, err)
	}
	require.NoError(t, service.Configure(context.Background(), wallet.CredentialBackupPolicy{Enabled: enabled, Binding: binding}))
	model, err := NewCLIModel(vault)
	require.NoError(t, err)
	model.ConfigureCredentialBackups(service, store)
	model.balanceConfig = cfg
	model.currentConfig = cfg
	return model, vault, store, binding
}

func mustCredentialVaultID(t *testing.T, vault *wallet.WalletVault) string {
	t.Helper()
	id, err := vault.CredentialVaultID(context.Background())
	require.NoError(t, err)
	return id
}

func driveCmds(model *CLIModel, cmd tea.Cmd) {
	var walk func(c tea.Cmd, depth int)
	walk = func(c tea.Cmd, depth int) {
		if c == nil {
			return
		}
		if depth > 1024 {
			panic("driveCmds exceeded maximum command depth")
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
		if rv := reflect.ValueOf(msg); rv.IsValid() && rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Func {
			for i := 0; i < rv.Len(); i++ {
				if inner, ok := rv.Index(i).Interface().(tea.Cmd); ok {
					walk(inner, depth+1)
				}
			}
			return
		}
		_, next := model.Update(msg)
		walk(next, depth+1)
	}
	walk(cmd, 0)
}

func keyMsg(value string) tea.KeyMsg {
	if value == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func typeCredentialKeys(model *CLIModel, value string) {
	for _, r := range value {
		if model.credentialPrompt == nil {
			return
		}
		model.updateCredentialPrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestKeePassPromptUnlockFlow(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	ready := false
	cmd := model.requestCredentialOperation(func(op *wallet.CredentialBackupOperation) tea.Cmd {
		ready = true
		return nil
	})
	require.Nil(t, cmd)
	require.NotNil(t, model.credentialPrompt)
	assert.False(t, ready)
	typeCredentialKeys(model, "test master password 1234")
	_, cmd = model.updateCredentialPrompt(keyMsg("enter"))
	require.NotNil(t, cmd)
	msg := cmd()
	_, ok := msg.(credentialOpenedMsg)
	require.True(t, ok)
	_, _ = model.handleCredentialOpened(msg.(credentialOpenedMsg))
	require.NotNil(t, model.credentialOperation)
	assert.True(t, ready)
}

func TestKeePassPromptWrongMaster(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	model.requestCredentialOperation(nil)
	require.NotNil(t, model.credentialPrompt)
	typeCredentialKeys(model, "wrong master password!!")
	_, cmd := model.updateCredentialPrompt(keyMsg("enter"))
	require.NotNil(t, cmd)
	msg := cmd().(credentialOpenedMsg)
	require.Error(t, msg.err)
	_, _ = model.handleCredentialOpened(msg)
	require.Nil(t, model.credentialOperation)
	require.NotNil(t, model.credentialPrompt)
	assert.NotEmpty(t, model.credentialPrompt.err)
}

func TestKeePassPromptStaleResultClosed(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	model.requestCredentialOperation(nil)
	typeCredentialKeys(model, "test master password 1234")
	_, cmd := model.updateCredentialPrompt(keyMsg("enter"))
	msg := cmd().(credentialOpenedMsg)
	require.NoError(t, msg.err)
	require.NotNil(t, msg.operation)
	model.clearCredentialPrompt()
	_, _ = model.handleCredentialOpened(msg)
	assert.Error(t, msg.operation.Context().Err())
	require.Nil(t, model.credentialOperation)
}

func TestKeePassDisabledFlowBypass(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, false)
	called := false
	model.requestCredentialOperation(func(op *wallet.CredentialBackupOperation) tea.Cmd {
		called = true
		assert.Nil(t, op)
		return nil
	})
	assert.True(t, called)
	assert.Nil(t, model.credentialPrompt)
	assert.False(t, model.credentialBackupEnabled())
}

func TestKeePassBlurClearsOperation(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	model.requestCredentialOperation(nil)
	typeCredentialKeys(model, "test master password 1234")
	_, cmd := model.updateCredentialPrompt(keyMsg("enter"))
	_, _ = model.handleCredentialOpened(cmd().(credentialOpenedMsg))
	require.NotNil(t, model.credentialOperation)
	op := model.credentialOperation
	_, _ = model.Update(tea.BlurMsg{})
	assert.Error(t, op.Context().Err())
	assert.Nil(t, model.credentialOperation)
}

func TestKeePassSettingsPendingList(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)
	service := model.credentialService
	op, err := service.Begin(context.Background(), []byte("test master password 1234"))
	require.NoError(t, err)
	defer op.Close()
	_, _, err = vault.Create(op.Context(), wallet.CreateAccountRequest{
		Name: "acct", Password: []byte("storage password 1"), WordCount: 12,
	})
	require.NoError(t, err)
	model.initKeePassSettings()
	require.NotNil(t, model.keepassSettings)
	_, cmd := model.runKeePassMenuAction("pending")
	require.NotNil(t, cmd)
	msg := cmd()
	pending, ok := msg.(keepassPendingMsg)
	require.True(t, ok)
	require.NoError(t, pending.err)
	require.NotEmpty(t, pending.rows)
	_, _ = model.updateKeePassSettings(msg)
	require.Equal(t, keepassStagePendingList, model.keepassSettings.stage)
	require.NotEmpty(t, model.keepassSettings.pending)
}

func TestKeePassSubmitWithCredentialDefersUntilPrompt(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	ran := false
	submit := func() tea.Cmd {
		ran = true
		return nil
	}
	cmd := model.submitWithCredential(true, submit)
	require.Nil(t, cmd)
	assert.False(t, ran)
	require.NotNil(t, model.credentialPrompt)
	typeCredentialKeys(model, "test master password 1234")
	_, openCmd := model.updateCredentialPrompt(keyMsg("enter"))
	_, _ = model.handleCredentialOpened(openCmd().(credentialOpenedMsg))
	assert.True(t, ran)
}

func TestRunWithAccountCredentialManual(t *testing.T) {
	called := false
	err := runWithAccountCredential(context.Background(), nil, "id", []byte("pw"), false, func(pw []byte) error {
		called = true
		assert.Equal(t, []byte("pw"), pw)
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	err = runWithAccountCredential(context.Background(), nil, "id", nil, true, func(pw []byte) error { return nil })
	assert.ErrorIs(t, err, wallet.ErrCredentialBackupRequired)
}

func pressKey(model *CLIModel, value string) tea.Cmd {
	_, cmd := model.Update(keyMsg(value))
	return cmd
}

func typeRunesViaUpdate(model *CLIModel, value string) {
	for _, r := range value {
		_, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func unlockMasterViaUpdate(t *testing.T, model *CLIModel, master string) {
	t.Helper()
	require.NotNil(t, model.credentialPrompt)
	typeRunesViaUpdate(model, master)
	cmd := pressKey(model, "enter")
	driveCmds(model, cmd)
	require.Nil(t, model.credentialPrompt)
}

func createActiveAccount(t *testing.T, model *CLIModel, name, password string) wallet.AccountSummary {
	t.Helper()
	vault := model.Vault
	ctx := context.Background()
	var op *wallet.CredentialBackupOperation
	if model.credentialBackupEnabled() {
		var err error
		op, err = model.credentialService.Begin(ctx, []byte("test master password 1234"))
		require.NoError(t, err)
		defer op.Close()
		ctx = op.Context()
	}
	summary, challenge, err := vault.Create(ctx, wallet.CreateAccountRequest{
		Name: name, Password: []byte(password), WordCount: 12,
	})
	require.NoError(t, err)
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	_, err = vault.ConfirmBackup(ctx, challenge.ChallengeID, answers)
	require.NoError(t, err)
	if op != nil {
		_, err = op.Sync(ctx)
		require.NoError(t, err)
	}
	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	for _, account := range accounts {
		if account.AccountID == summary.AccountID {
			return account
		}
	}
	t.Fatalf("account %s not listed after confirmation", summary.AccountID)
	return wallet.AccountSummary{}
}

func seedKeePassFileCredential(t *testing.T, store *keepass.Store, binding keepass.Binding, master, kind string, ciphertext []byte, password string) {
	t.Helper()
	digest := sha256.Sum256(ciphertext)
	digestHex := hex.EncodeToString(digest[:])
	op, err := store.Open(context.Background(), binding, []byte(master))
	require.NoError(t, err)
	defer op.Close()
	record := keepass.Record{
		Ref:      keepass.Ref{VaultID: binding.VaultID, AccountID: uuid.NewString(), ItemID: "file:" + kind + ":" + digestHex},
		Title:    "seed",
		Kind:     kind,
		FileName: "seed.json",
		Digest:   digestHex,
		Password: []byte(password),
	}
	_, err = op.Upsert(context.Background(), []keepass.Record{record})
	require.NoError(t, err)
}

func TestKeePassUIConfigureAndBackupExisting(t *testing.T) {
	model, _, store, _ := newCredentialUITestModel(t, false)
	summary := createActiveAccount(t, model, "pre-integration", "storage pass phrase 1")

	model.loadConfigFn = func() (*config.Config, error) { return model.currentConfig, nil }
	model.saveConfigFn = func(cfg *config.Config) error {
		model.currentConfig = cfg
		model.balanceConfig = cfg
		return nil
	}

	model.currentView = constants.ConfigurationView
	model.selectedMenu = 0
	pressKey(model, "down")
	pressKey(model, "down")
	pressKey(model, "enter")
	require.Equal(t, constants.KeePassSettingsView, model.currentView)
	state := model.keepassSettings
	require.NotNil(t, state)

	pressKey(model, "enter")
	require.Equal(t, keepassStagePath, state.stage)
	state.pathInput.SetValue(filepath.Join(model.balanceConfig.AppDir, "ui-vault.kdbx"))
	pressKey(model, "enter")
	require.Equal(t, keepassStageMaster, state.stage)
	state.masterInput.SetValue("ui master password 9")
	pressKey(model, "enter")
	state.confirmInput.SetValue("ui master password 9")
	pressKey(model, "enter")
	state.consentInput.SetValue("ENABLE")
	driveCmds(model, pressKey(model, "enter"))
	require.True(t, model.credentialBackupEnabled())
	require.NotNil(t, model.currentConfig)
	assert.True(t, model.currentConfig.KeePass.Enabled)
	assert.FileExists(t, model.currentConfig.KeePass.Path)

	model.selectedAccount = &summary
	model.currentView = constants.WalletDetailsView
	driveCmds(model, pressKey(model, "K"))
	require.Equal(t, constants.KeePassAccountView, model.currentView)
	require.NotNil(t, model.keepassAccount)
	assert.False(t, model.keepassAccount.hasBackup)

	pressKey(model, "enter")
	require.Equal(t, keepassAccountStagePassword, model.keepassAccount.stage)
	model.keepassAccount.password.SetValue("storage pass phrase 1")
	driveCmds(model, pressKey(model, "enter"))
	require.NotNil(t, model.credentialPrompt)

	typeRunesViaUpdate(model, "ui master password 9")
	driveCmds(model, pressKey(model, "enter"))
	require.Nil(t, model.credentialPrompt)
	driveCmds(model, nil)
	assert.Empty(t, model.keepassAccount.err)

	rows, err := model.credentialService.Status(context.Background(), summary.AccountID)
	require.NoError(t, err)
	synced := false
	for _, row := range rows {
		if row.ItemID == "account" && row.State == wallet.CredentialBackupStateSynced {
			synced = true
		}
	}
	assert.True(t, synced, "expected synced account backup row, got %+v", rows)

	binding := keepass.Binding{
		Path:     model.currentConfig.KeePass.Path,
		TargetID: model.currentConfig.KeePass.TargetID,
		VaultID:  model.currentConfig.KeePass.VaultID,
	}
	verifyOp, err := store.Open(context.Background(), binding, []byte("ui master password 9"))
	require.NoError(t, err)
	defer verifyOp.Close()
	metadata, err := verifyOp.List(context.Background())
	require.NoError(t, err)
	foundAccount := false
	for _, meta := range metadata {
		if meta.Ref.AccountID == summary.AccountID && meta.Ref.ItemID == "account" {
			foundAccount = true
		}
	}
	assert.True(t, foundAccount, "expected account record inside KDBX")
}

func TestKeePassUIConfigureCancelThenRetry(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, false)
	model.loadConfigFn = func() (*config.Config, error) { return model.currentConfig, nil }
	model.saveConfigFn = func(cfg *config.Config) error {
		model.currentConfig = cfg
		model.balanceConfig = cfg
		return nil
	}
	model.initKeePassSettings()
	state := model.keepassSettings
	pressKey(model, "enter")
	state.pathInput.SetValue(filepath.Join(model.balanceConfig.AppDir, "cancel.kdbx"))
	pressKey(model, "enter")
	state.masterInput.SetValue("master password 55")
	pressKey(model, "esc")
	require.Equal(t, keepassStageMenu, state.stage)
	require.Empty(t, state.masterInput.Value())

	pressKey(model, "enter")
	state.pathInput.SetValue(filepath.Join(model.balanceConfig.AppDir, "cancel.kdbx"))
	pressKey(model, "enter")
	state.masterInput.SetValue("master password 55")
	pressKey(model, "enter")
	state.confirmInput.SetValue("master password 55")
	pressKey(model, "enter")
	state.consentInput.SetValue("WRONG")
	pressKey(model, "enter")
	require.Equal(t, keepassStageConsent, state.stage)
	assert.NotEmpty(t, state.errText())
	assert.NotEmpty(t, state.masterInput.Value())
	state.consentInput.SetValue("ENABLE")
	driveCmds(model, pressKey(model, "enter"))
	require.True(t, model.credentialBackupEnabled())
}

func TestKeePassUIDisabledTestRejected(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, false)
	model.initKeePassSettings()
	state := model.keepassSettings
	for i, action := range model.keepassMenuActions() {
		if action == "test" {
			state.menuIndex = i
		}
	}
	pressKey(model, "enter")
	assert.NotEmpty(t, state.errText())
	assert.False(t, state.busy)
	assert.Empty(t, state.notice)
}

func TestKeePassUISourcePreviewAndCommit(t *testing.T) {
	model, vault, store, binding := newCredentialUITestModel(t, true)
	root := t.TempDir()
	sourcePassword := "kdbx stored source pw"
	keystorePath := filepath.Join(root, "key.json")
	ciphertext := writeTestKeystore(t, keystorePath, sourcePassword)
	seedKeePassFileCredential(t, store, binding, "test master password 1234", "keystore_v3", ciphertext, sourcePassword)

	model.initCanonicalImport(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("KDBX import")
	pressKey(model, "enter")
	state.fields[1].input.SetValue(keystorePath)
	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, "source_password", state.fields[state.stage].key)
	pressKey(model, "ctrl+k")
	require.True(t, model.credentialUseKeePass)
	pressKey(model, "enter")
	require.Equal(t, "storage_password", state.fields[state.stage].key)
	state.fields[3].input.SetValue("new storage password 1")
	pressKey(model, "enter")
	require.Equal(t, "confirm_password", state.fields[state.stage].key)
	state.fields[4].input.SetValue("new storage password 1")
	driveCmds(model, pressKey(model, "enter"))
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, "test master password 1234")

	require.NotNil(t, state.preview)
	assert.True(t, state.kdbxSource)
	assert.NotContains(t, model.viewCanonicalImport(), sourcePassword)

	driveCmds(model, pressKey(model, "enter"))
	require.Nil(t, model.canonicalImport)
	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, "KDBX import", model.selectedAccount.Name)
	assert.NotContains(t, model.View(), sourcePassword)
	assert.NotContains(t, model.View(), "new storage password 1")

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	var imported *wallet.AccountSummary
	for i := range accounts {
		if accounts[i].Name == "KDBX import" {
			imported = &accounts[i]
		}
	}
	require.NotNil(t, imported)
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestKeePassUISourcePwdSidecarPriority(t *testing.T) {
	model, vault, store, binding := newCredentialUITestModel(t, true)
	root := t.TempDir()
	realPassword := "actual sidecar secret"
	keystorePath := filepath.Join(root, "key.json")
	ciphertext := writeTestKeystore(t, keystorePath, realPassword)
	require.NoError(t, os.WriteFile(filepath.Join(root, "key.pwd"), []byte(realPassword+"\n"), 0600))
	seedKeePassFileCredential(t, store, binding, "test master password 1234", "keystore_v3", ciphertext, "conflicting kdbx password")

	model.initCanonicalImport(wallet.ImportMethodKeystore)
	state := model.canonicalImport
	state.fields[0].input.SetValue("Sidecar wins")
	pressKey(model, "enter")
	state.fields[1].input.SetValue(keystorePath)
	driveCmds(model, pressKey(model, "enter"))
	require.True(t, state.sourcePasswordFromFile)
	state.fields[3].input.SetValue("storage password zz")
	pressKey(model, "enter")
	state.fields[4].input.SetValue("storage password zz")
	driveCmds(model, pressKey(model, "enter"))
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, "test master password 1234")
	require.NotNil(t, state.preview)
	driveCmds(model, pressKey(model, "enter"))
	require.Nil(t, model.canonicalImport)
	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, "Sidecar wins", model.selectedAccount.Name)

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	found := false
	for i := range accounts {
		if accounts[i].Name == "Sidecar wins" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestKeePassUIBatchSources(t *testing.T) {
	model, vault, store, binding := newCredentialUITestModel(t, true)
	root := t.TempDir()
	writeTestKeystore(t, filepath.Join(root, "a.json"), "pw a")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.pwd"), []byte("pw a\n"), 0600))
	bBytes := writeTestKeystore(t, filepath.Join(root, "b.json"), "pw b")
	seedKeePassFileCredential(t, store, binding, "test master password 1234", "keystore_v3", bBytes, "pw b")
	writeTestKeystore(t, filepath.Join(root, "c.json"), "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "bad.json"), []byte("{not json"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.txt"), []byte("ignored"), 0600))

	model.initCanonicalImport(canonicalBatchMethod)
	state := model.canonicalImport
	require.True(t, state.lookupMissingSidecars)
	assert.Contains(t, model.viewCanonicalImport(), "KeePass")

	state.fields[0].input.SetValue(root)
	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, "storage_password", state.fields[state.stage].key)
	state.fields[1].input.SetValue("batch storage pw 1")
	pressKey(model, "enter")
	state.fields[2].input.SetValue("batch storage pw 1")
	driveCmds(model, pressKey(model, "enter"))
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, "test master password 1234")

	require.NotEmpty(t, state.batchPreviews)
	kdbxSeen := false
	for _, item := range state.batchPreviews {
		if item.kdbx {
			kdbxSeen = true
		}
	}
	assert.True(t, kdbxSeen, "expected a batch preview marked as KDBX sourced")

	driveCmds(model, pressKey(model, "enter"))
	require.NotEmpty(t, state.resultLines)
	joined := strings.Join(state.resultLines, "\n")
	assert.NotContains(t, joined, "pw a")
	assert.NotContains(t, joined, "pw b")
	assert.NotContains(t, joined, "batch storage pw 1")

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 3)
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)
	for _, account := range accounts {
		rows, err := model.credentialService.Status(context.Background(), account.AccountID)
		require.NoError(t, err)
		synced := false
		for _, row := range rows {
			if row.ItemID == "account" && row.State == wallet.CredentialBackupStateSynced {
				synced = true
			}
		}
		assert.True(t, synced, "account %s missing synced backup", account.AccountID)
	}
}

func TestKeePassUIDeletePreservesBackupByDefault(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "delete-me", "delete storage pw 9")

	model.initAccountDeletion(summary)
	model.currentView = constants.ListWalletsView
	state := model.accountDeletion
	require.False(t, state.removeBackup)
	state.confirmation.SetValue(summary.AccountID)
	pressKey(model, "enter")
	require.Equal(t, 1, state.stage)
	state.password.SetValue("delete storage pw 9")
	driveCmds(model, pressKey(model, "enter"))
	assert.Equal(t, constants.ListWalletsView, model.currentView)
	assert.Nil(t, model.accountDeletion)

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	for _, account := range accounts {
		assert.NotEqual(t, summary.AccountID, account.AccountID)
	}
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestKeePassUIDeleteRemoveRequiresTypedConfirmation(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "delete-backup", "delete storage pw 8")

	model.initAccountDeletion(summary)
	model.currentView = constants.ListWalletsView
	state := model.accountDeletion
	pressKey(model, "ctrl+b")
	require.True(t, state.removeBackup)
	state.confirmation.SetValue(summary.AccountID)
	pressKey(model, "enter")
	require.Equal(t, 2, state.stage)
	state.backupConfirm.SetValue("wrong-id")
	pressKey(model, "enter")
	require.Equal(t, 2, state.stage)
	assert.NotEmpty(t, state.err)
	state.backupConfirm.SetValue(summary.AccountID)
	pressKey(model, "enter")
	require.Equal(t, 1, state.stage)
	state.password.SetValue("delete storage pw 8")
	driveCmds(model, pressKey(model, "enter"))
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, "test master password 1234")
	assert.Equal(t, constants.ListWalletsView, model.currentView)

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	for _, account := range accounts {
		assert.NotEqual(t, summary.AccountID, account.AccountID)
	}
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestKeePassUIPromptRendersOverDeleteView(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "overlay-delete", "overlay storage pw")
	model.initAccountDeletion(summary)
	model.currentView = constants.ListWalletsView
	state := model.accountDeletion
	pressKey(model, "ctrl+b")
	state.confirmation.SetValue(summary.AccountID)
	pressKey(model, "enter")
	state.backupConfirm.SetValue(summary.AccountID)
	pressKey(model, "enter")
	state.password.SetValue("overlay storage pw")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	model.width = 100
	model.height = 30
	view := model.View()
	assert.Contains(t, view, localization.Get("keepass_master_title"))
	assert.NotContains(t, view, "overlay storage pw")
	assert.NotContains(t, view, summary.AccountID[:20])
}

type spyTransactionAuthorizer struct {
	accountID string
	password  []byte
	calls     int
}

func (spy *spyTransactionAuthorizer) Authorize(_ context.Context, accountID string, password []byte, operation wallet.TransactionAuthorizationOperation) error {
	spy.accountID = accountID
	spy.password = append([]byte(nil), password...)
	spy.calls++
	return operation(wallet.CapabilityHandle{}, 1)
}

func (spy *spyTransactionAuthorizer) HasActiveSession(context.Context, string) bool { return false }

func TestKeePassUIPersonalSignResolvesBackupPassword(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "signer", "sign storage pass 4")
	spy := &spyTransactionAuthorizer{}
	model.transactionAuthorizer = spy
	model.selectedAccount = &summary
	signer := common.HexToAddress(summary.Address)
	model.ConfigureMessageSigningFactory(func(context.Context) (MessageSigningService, error) {
		return &personalSignServiceStub{signer: signer}, nil
	})
	service, err := model.messageSigningFactory(context.Background())
	require.NoError(t, err)
	model.initPersonalSign(service)

	signOnce := func() {
		model.personalSign.message.SetValue("hello")
		pressKey(model, "n")
		pressKey(model, "a")
		require.Equal(t, personalSignPassword, model.personalSign.phase)
		pressKey(model, "ctrl+k")
		pressKey(model, "enter")
		require.NotNil(t, model.credentialPrompt)
		unlockMasterViaUpdate(t, model, "test master password 1234")
		require.Equal(t, personalSignComplete, model.personalSign.phase)
	}
	signOnce()
	require.Equal(t, 1, spy.calls)
	assert.Equal(t, summary.AccountID, spy.accountID)
	assert.Equal(t, []byte("sign storage pass 4"), spy.password)
	assert.Nil(t, model.credentialOperation)

	model.initPersonalSign(service)
	signOnce()
	require.Equal(t, 2, spy.calls)
	assert.Equal(t, []byte("sign storage pass 4"), spy.password)
}
