package ui

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/constants"
	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type createFlowRepository struct {
	wallets []wallet.Wallet
}

func (r *createFlowRepository) AddWallet(w *wallet.Wallet) error {
	w.ID = len(r.wallets) + 1
	r.wallets = append(r.wallets, *w)
	return nil
}

func (r *createFlowRepository) GetAllWallets() ([]wallet.Wallet, error) {
	return append([]wallet.Wallet(nil), r.wallets...), nil
}

func (r *createFlowRepository) DeleteWallet(int) error {
	return nil
}

func (r *createFlowRepository) FindBySourceHash(sourceHash string) (*wallet.Wallet, error) {
	for i := range r.wallets {
		if r.wallets[i].SourceHash == sourceHash {
			w := r.wallets[i]
			return &w, nil
		}
	}
	return nil, nil
}

func (r *createFlowRepository) FindByAddress(string) ([]wallet.Wallet, error) {
	return nil, nil
}

func (r *createFlowRepository) FindByAddressAndMethod(string, string) ([]wallet.Wallet, error) {
	return nil, nil
}

func (r *createFlowRepository) Close() error {
	return nil
}

func TestDisplayedMnemonicControlsPersistedAccount(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	globalConfigManager = nil
	globalNetworkManager = nil
	t.Cleanup(func() {
		globalConfigManager = nil
		globalNetworkManager = nil
	})

	cfg := &config.Config{
		AppDir:     homeDir,
		WalletsDir: filepath.Join(homeDir, "wallets"),
		Language:   "en",
		LocaleDir:  "../../pkg/localization/locales",
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64 * 1024,
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
	}
	require.NoError(t, localization.InitLocalization(cfg))
	wallet.InitCryptoService(cfg)

	keystoreDir := filepath.Join(homeDir, "keystore")
	repository := &createFlowRepository{}
	service := wallet.NewWalletService(
		repository,
		keystore.NewKeyStore(keystoreDir, keystore.LightScryptN, keystore.LightScryptP),
	)
	model := &CLIModel{Service: service}
	model.initCreateWallet()
	displayedMnemonic := model.mnemonic

	model.nameInput.SetValue("Recovery Test")
	_, _ = model.updateCreateWalletName(tea.KeyMsg{Type: tea.KeyEnter})
	model.backupConfirmationInput.SetValue(displayedMnemonic)
	_, _ = model.updateCreateWalletBackup(tea.KeyMsg{Type: tea.KeyEnter})
	model.passwordInput.SetValue("StrongPassword1!")
	_, _ = model.updateCreateWalletPassword(tea.KeyMsg{Type: tea.KeyEnter})

	require.NotNil(t, model.walletDetails)
	require.NotNil(t, model.walletDetails.Mnemonic)
	if displayedMnemonic != *model.walletDetails.Mnemonic {
		t.Fatal("displayed mnemonic does not control persisted account")
	}
	preview, err := wallet.PreviewMnemonicImport(wallet.MnemonicImportRequest{Mnemonic: displayedMnemonic})
	require.NoError(t, err)
	assert.Equal(t, model.walletDetails.Wallet.Address, preview.Address)

	restartedService := wallet.NewWalletService(
		repository,
		keystore.NewKeyStore(keystoreDir, keystore.LightScryptN, keystore.LightScryptP),
	)
	loaded, err := restartedService.LoadWallet(model.walletDetails.Wallet, "StrongPassword1!")
	require.NoError(t, err)
	assert.Equal(t, model.walletDetails.Wallet.Address, loaded.Wallet.Address)
}

func TestNewCLIModelRequiresVault(t *testing.T) {
	model, err := NewCLIModel(nil)
	assert.Error(t, err)
	assert.Nil(t, model)
}

func TestVaultBackedCreateFlowPersistsOnlyEncryptedSecret(t *testing.T) {
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
	model := &CLIModel{Vault: vault, styles: createStyles()}

	model.initCreateWallet()
	assert.Empty(t, model.mnemonic)
	model.nameInput.SetValue("Vault UI")
	_, _ = model.updateCreateWalletName(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, constants.CreateWalletOptionsView, model.currentView)
	for range 4 {
		_, _ = model.updateCreateWalletOptions(tea.KeyMsg{Type: tea.KeyEnter})
	}
	assert.Equal(t, constants.CreateWalletView, model.currentView)
	model.passwordInput.SetValue("Strong vault password 1!")
	_, _ = model.updateCreateWalletPassword(tea.KeyMsg{Type: tea.KeyEnter})
	model.createPasswordConfirmationInput.SetValue("Strong vault password 1!")
	_, _ = model.updateCreateWalletPassword(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, model.backupChallenge)
	mnemonic := strings.Join(model.backupChallenge.Words, " ")
	answers := make([]string, 0, len(model.backupChallenge.RequiredWordIndices))
	for _, index := range model.backupChallenge.RequiredWordIndices {
		answers = append(answers, model.backupChallenge.Words[index])
	}
	model.backupConfirmationInput.SetValue(strings.Join(answers, " "))
	_, _ = model.updateCreateWalletBackup(tea.KeyMsg{Type: tea.KeyEnter})

	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, wallet.AccountStateActive, model.selectedAccount.State)
	assert.Nil(t, model.walletDetails)
	stored, err := repository.GetAccount(context.Background(), model.selectedAccount.AccountID)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(stored.SecretEnvelope, []byte(mnemonic)))
	assert.NotContains(t, model.viewWalletDetails(), mnemonic)
	countMessage, ok := walletCountCmd(nil, vault)().(walletCountMsg)
	require.True(t, ok)
	assert.NoError(t, countMessage.err)
	assert.Equal(t, 1, countMessage.count)

	newStoragePassword := "Different vault password 2!"
	model.initVaultAction(false)
	model.currentPasswordInput.SetValue("Strong vault password 1!")
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, false)
	model.newPasswordInput.SetValue(newStoragePassword)
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, false)
	model.confirmPasswordInput.SetValue(newStoragePassword)
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, false)
	assert.Equal(t, constants.WalletDetailsView, model.currentView)
	if _, err := vault.Unlock(context.Background(), model.selectedAccount.AccountID, []byte("Strong vault password 1!")); err == nil {
		t.Fatal("old password unlocked after UI rotation")
	}
	handle, err := vault.Unlock(context.Background(), model.selectedAccount.AccountID, []byte(newStoragePassword))
	require.NoError(t, err)
	require.NoError(t, vault.Lock(handle))

	exportPassword := "Strong export password 3!"
	exportPath := filepath.Join(t.TempDir(), "account.bloco")
	model.initVaultAction(true)
	model.currentPasswordInput.SetValue(newStoragePassword)
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, true)
	model.newPasswordInput.SetValue(exportPassword)
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, true)
	model.confirmPasswordInput.SetValue(exportPassword)
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, true)
	model.exportDestinationInput.SetValue(exportPath)
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, true)
	assert.True(t, model.vaultActionPreview)
	_, _ = model.updateVaultAction(tea.KeyMsg{Type: tea.KeyEnter}, true)
	assert.Equal(t, constants.WalletDetailsView, model.currentView)
	assert.FileExists(t, exportPath)

	pending, suspended, err := vault.Create(context.Background(), wallet.CreateAccountRequest{
		Name:     "Resume UI",
		Password: []byte(newStoragePassword),
	})
	require.NoError(t, err)
	require.NoError(t, vault.SuspendBackup(suspended.ChallengeID))
	model.selectedAccount = &pending
	model.currentView = constants.WalletDetailsView
	_, _ = model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	assert.Equal(t, constants.CreateWalletView, model.currentView)
	model.passwordInput.SetValue(newStoragePassword)
	_, _ = model.updateCreateWalletPassword(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, model.backupChallenge)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	storedPending, err := repository.GetAccount(context.Background(), pending.AccountID)
	require.NoError(t, err)
	assert.Equal(t, wallet.AccountStatePendingBackup, storedPending.State)

	custom, customChallenge, err := vault.Create(context.Background(), wallet.CreateAccountRequest{
		Name:            "Custom backup UI",
		Password:        []byte(newStoragePassword),
		WordCount:       15,
		BIP39Language:   wallet.BIP39Spanish,
		BIP39Passphrase: "contraseña",
		DerivationPath:  "m/44'/60'/4'/1/2",
	})
	require.NoError(t, err)
	model.pendingAccount = &custom
	model.backupChallenge = &customChallenge
	model.initBackupMaterialInputs()
	model.currentView = constants.CreateWalletBackupView
	requestedWords := make([]string, 0, len(customChallenge.RequiredWordIndices))
	for _, index := range customChallenge.RequiredWordIndices {
		requestedWords = append(requestedWords, customChallenge.Words[index])
	}
	model.backupConfirmationInput.SetValue(strings.Join(requestedWords, " "))
	_, _ = model.updateCreateWalletBackup(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, constants.WalletDetailsView, model.currentView)
	assert.Nil(t, model.backupChallenge)
	activatedCustom, err := repository.GetAccount(context.Background(), custom.AccountID)
	require.NoError(t, err)
	assert.Equal(t, wallet.AccountStateActive, activatedCustom.State)

	model.initCanonicalImport(wallet.ImportMethodMnemonic)
	for index := range model.canonicalImport.fields {
		field := &model.canonicalImport.fields[index]
		switch field.key {
		case "name":
			field.input.SetValue("Canonical UI import")
		case "mnemonic":
			field.input.SetValue("test test test test test test test test test test test junk")
		case "language":
			field.input.SetValue("english")
		case "path":
			field.input.SetValue("m/44'/60'/9'/0/0")
		case "storage_password", "confirm_password":
			field.input.SetValue("Canonical storage password 4!")
		}
	}
	var importCommand tea.Cmd
	for range model.canonicalImport.fields {
		_, importCommand = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	}
	require.NotNil(t, importCommand)
	_, _ = model.Update(importCommand())
	require.NotNil(t, model.canonicalImport.preview)
	_, importCommand = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, importCommand)
	_, _ = model.Update(importCommand())
	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, wallet.AccountStateActive, model.selectedAccount.State)
	assert.Equal(t, constants.WalletDetailsView, model.currentView)

	model.initCanonicalImport(wallet.ImportMethodWatchOnly)
	model.canonicalImport.fields[0].input.SetValue("Watch-only UI")
	model.canonicalImport.fields[1].input.SetValue("0x0000000000000000000000000000000000000001")
	for range model.canonicalImport.fields {
		_, importCommand = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	}
	require.NotNil(t, importCommand)
	_, _ = model.Update(importCommand())
	require.NotNil(t, model.canonicalImport.preview)
	_, importCommand = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, importCommand)
	_, _ = model.Update(importCommand())
	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, wallet.SignerKindWatchOnly, model.selectedAccount.SignerKind)
	assert.Zero(t, model.selectedAccount.Capabilities)
	storedWatchOnly, err := repository.GetAccount(context.Background(), model.selectedAccount.AccountID)
	require.NoError(t, err)
	assert.Empty(t, storedWatchOnly.SecretEnvelope)
}

func TestQIsAcceptedInSecretInput(t *testing.T) {
	cfg := &config.Config{Language: "en", LocaleDir: "../../pkg/localization/locales"}
	require.NoError(t, localization.InitLocalization(cfg))
	model := &CLIModel{}
	model.initCreateWallet()
	model.currentView = constants.CreateWalletBackupView
	model.backupConfirmationInput.Focus()

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	assert.Equal(t, "q", model.backupConfirmationInput.Value())
	assert.Equal(t, constants.CreateWalletBackupView, model.currentView)
	if cmd != nil {
		_, quitting := cmd().(tea.QuitMsg)
		assert.False(t, quitting)
	}
}

func TestWalletCreationRequiresMnemonicConfirmation(t *testing.T) {
	cfg := &config.Config{Language: "en", LocaleDir: "../../pkg/localization/locales"}
	require.NoError(t, localization.InitLocalization(cfg))
	model := &CLIModel{}
	model.initCreateWallet()
	model.nameInput.SetValue("Unconfirmed")
	_, _ = model.updateCreateWalletName(tea.KeyMsg{Type: tea.KeyEnter})
	model.backupConfirmationInput.SetValue("wrong recovery phrase")

	_, _ = model.updateCreateWalletBackup(tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, constants.CreateWalletBackupView, model.currentView)
	assert.NotEmpty(t, model.backupError)
	assert.Nil(t, model.walletDetails)
}

func TestKeystoreImportUsesCanonicalMenu(t *testing.T) {
	cfg := &config.Config{Language: "en", LocaleDir: "../../pkg/localization/locales"}
	require.NoError(t, localization.InitLocalization(cfg))
	found := false
	for _, item := range NewImportMenu() {
		if item.title == localization.Labels["import_keystore"] {
			found = true
		}
	}
	assert.True(t, found)
	model := &CLIModel{selectedMenu: 2, styles: createStyles()}
	_, _ = model.updateImportMethodSelection(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, model.canonicalImport)
	assert.Equal(t, wallet.ImportMethodKeystore, model.canonicalImport.method)
	assert.Equal(t, constants.CanonicalImportView, model.currentView)
}

func TestWatchOnlyImportUsesCanonicalAddressOnlyFlow(t *testing.T) {
	cfg := &config.Config{Language: "en", LocaleDir: "../../pkg/localization/locales"}
	require.NoError(t, localization.InitLocalization(cfg))
	found := false
	for _, item := range NewImportMenu() {
		if item.title == "Watch-only Address" {
			found = true
		}
	}
	assert.True(t, found)
	model := &CLIModel{selectedMenu: 5, styles: createStyles()}
	_, _ = model.updateImportMethodSelection(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, model.canonicalImport)
	assert.Equal(t, wallet.ImportMethodWatchOnly, model.canonicalImport.method)
	require.Len(t, model.canonicalImport.fields, 2)
	assert.Equal(t, "name", model.canonicalImport.fields[0].key)
	assert.Equal(t, "address", model.canonicalImport.fields[1].key)
	model.canonicalImport.fields[0].input.SetValue("Observer")
	model.canonicalImport.fields[1].input.SetValue("f39fd6e51aad88f6f4ce6ab8827279cfffb92266")
	require.NoError(t, prepareCanonicalImportPreview(context.Background(), nil, model.canonicalImport))
	require.NotNil(t, model.canonicalImport.preview)
	assert.Equal(t, wallet.SignerKindWatchOnly, model.canonicalImport.preview.SignerKind)
	approvedAddress := model.canonicalImport.fields[1].input.Value()
	_, _ = model.updateCanonicalImport(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, approvedAddress, model.canonicalImport.fields[1].input.Value())
	assert.Equal(t, model.canonicalImport.preview.Address, "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	assert.Contains(t, model.viewCanonicalImport(), "watch_only")
	assert.NotContains(t, model.viewCanonicalImport(), "storage password")
}

func TestSecretImportInputsAreMasked(t *testing.T) {
	cfg := &config.Config{Language: "en", LocaleDir: "../../pkg/localization/locales"}
	require.NoError(t, localization.InitLocalization(cfg))

	mnemonicModel := &CLIModel{styles: createStyles()}
	mnemonicModel.initCanonicalImport(wallet.ImportMethodMnemonic)
	mnemonicModel.canonicalImport.stage = 1
	mnemonicModel.canonicalImport.fields[1].input.SetValue("abandon")
	assert.NotContains(t, mnemonicModel.viewCanonicalImport(), "abandon")

	privateKeyModel := &CLIModel{styles: createStyles()}
	privateKeyModel.initCanonicalImport(wallet.ImportMethodPrivateKey)
	privateKeyModel.canonicalImport.stage = 1
	privateKey := strings.Repeat("a", 64)
	privateKeyModel.canonicalImport.fields[1].input.SetValue(privateKey)
	assert.NotContains(t, privateKeyModel.viewCanonicalImport(), privateKey)
}

func TestMnemonicImportIgnoresStalePrivateKeyInput(t *testing.T) {
	homeDir := t.TempDir()
	cfg := &config.Config{
		AppDir:     homeDir,
		WalletsDir: filepath.Join(homeDir, "keystore"),
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64 * 1024,
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
	}
	wallet.InitCryptoService(cfg)
	repository := &createFlowRepository{}
	service := wallet.NewWalletService(repository, keystore.NewKeyStore(cfg.WalletsDir, keystore.LightScryptN, keystore.LightScryptP), cfg.WalletsDir)
	mnemonic := "test test test test test test test test test test test junk"
	staleKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	model := &CLIModel{
		Service:             service,
		currentView:         constants.ImportWalletPasswordView,
		pendingImportMethod: wallet.ImportMethodMnemonic,
		importWords:         strings.Fields(mnemonic),
	}
	model.privateKeyInput.SetValue(hex.EncodeToString(crypto.FromECDSA(staleKey)))
	model.passwordInput.SetValue("StrongPassword1!")

	_, _ = model.updateImportWalletPassword(tea.KeyMsg{Type: tea.KeyEnter})

	require.NotNil(t, model.walletDetails)
	preview, err := wallet.PreviewMnemonicImport(wallet.MnemonicImportRequest{Mnemonic: mnemonic})
	require.NoError(t, err)
	assert.Equal(t, preview.Address, model.walletDetails.Wallet.Address)
	assert.Empty(t, model.privateKeyInput.Value())
}

func TestWalletDetailsFetchesBalancesOnlyAfterExplicitAction(t *testing.T) {
	cfg := &config.Config{Language: "en", LocaleDir: "../../pkg/localization/locales", Networks: make(map[string]config.Network)}
	require.NoError(t, localization.InitLocalization(cfg))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if len(payload) > 0 && payload[0] == '[' {
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x2a"}]`)
			return
		}
		_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	provider := blockchain.NewMultiProvider(gateway, config.EnvironmentCredentialProvider{})
	defer provider.Close()
	cfg.Networks["test"] = config.Network{Name: "Test", RPCEndpoint: server.URL, ChainID: 1, Symbol: "ETH", NativeDecimals: 18, NativeDecimalsSet: true, IsActive: true}
	model := &CLIModel{
		currentView:     constants.WalletDetailsView,
		selectedAccount: &wallet.AccountSummary{Address: "0x0000000000000000000000000000000000000001", State: wallet.AccountStateActive},
		balanceProvider: provider,
		balanceConfig:   cfg,
		styles:          createStyles(),
	}
	_ = model.viewWalletDetails()
	assert.Equal(t, int32(0), calls.Load())
	_, command := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	require.NotNil(t, command)
	assert.Equal(t, int32(0), calls.Load())
	_, _ = model.Update(command())
	assert.Greater(t, calls.Load(), int32(0))
	assert.Contains(t, model.viewWalletDetails(), "0.000000000000000042 ETH")
	callsAfterFirstFetch := calls.Load()
	_, cachedCommand := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	require.NotNil(t, cachedCommand)
	_, _ = model.Update(cachedCommand())
	assert.Equal(t, callsAfterFirstFetch, calls.Load())
	_, staleCommand := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	require.NotNil(t, staleCommand)
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, model.selectedAccount)
	assert.Empty(t, model.networkBalances)
	_, _ = model.Update(staleCommand())
	assert.Empty(t, model.networkBalances)
}

func TestWalletDetailsViewHidesSecrets(t *testing.T) {
	cfg := &config.Config{Language: "en", LocaleDir: "../../pkg/localization/locales"}
	require.NoError(t, localization.InitLocalization(cfg))
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	model := &CLIModel{walletDetails: &wallet.WalletDetails{
		Wallet: &wallet.Wallet{
			Address:      crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
			ImportMethod: string(wallet.ImportMethodMnemonic),
		},
		Mnemonic:     &mnemonic,
		ImportMethod: wallet.ImportMethodMnemonic,
		HasMnemonic:  true,
	}}

	view := model.viewWalletDetails()

	assert.NotContains(t, view, mnemonic)
	assert.NotContains(t, view, hex.EncodeToString(crypto.FromECDSA(privateKey)))
	assert.Contains(t, view, localization.GetWalletImportMessage("sensitive_data_hidden"))
}

func TestWalletDeletionIsDisabledInUI(t *testing.T) {
	model := &CLIModel{currentView: constants.ListWalletsView}

	_, _ = model.updateListWallets(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	assert.NotNil(t, model.err)
	assert.Nil(t, model.deletingWallet)
}

func TestWalletSelectionUsesIDWhenAddressesMatch(t *testing.T) {
	wallets := []wallet.Wallet{
		{ID: 1, Address: "0x1234567890123456789012345678901234567890"},
		{ID: 2, Address: "0x1234567890123456789012345678901234567890"},
	}
	walletTable := table.New(
		table.WithColumns([]table.Column{
			{Title: "ID", Width: 4},
			{Title: "Name", Width: 10},
			{Title: "Type", Width: 10},
			{Title: "Created", Width: 10},
			{Title: "Address", Width: 42},
		}),
		table.WithRows([]table.Row{
			{"1", "First", "Type", "Created", wallets[0].Address},
			{"2", "Second", "Type", "Created", wallets[1].Address},
		}),
	)
	walletTable.SetCursor(1)
	model := &CLIModel{wallets: wallets, walletTable: walletTable}

	selected := model.selectedWalletFromTable()

	require.NotNil(t, selected)
	assert.Equal(t, 2, selected.ID)
}

func TestWalletDetailsViewConsistency(t *testing.T) {
	// Initialize localization for tests
	cfg := &config.Config{
		Language:  "en",
		LocaleDir: "../../pkg/localization/locales",
	}
	err := localization.InitLocalization(cfg)
	require.NoError(t, err)

	tests := []struct {
		name             string
		importMethod     wallet.ImportMethod
		hasMnemonic      bool
		expectedMethod   string
		expectedMnemonic string
	}{
		{
			name:             "Keystore import shows correct method and mnemonic message",
			importMethod:     wallet.ImportMethodKeystore,
			hasMnemonic:      false,
			expectedMethod:   "Keystore File",
			expectedMnemonic: "Mnemonic not available - imported from keystore file",
		},
		{
			name:             "Private key import shows correct method and mnemonic message",
			importMethod:     wallet.ImportMethodPrivateKey,
			hasMnemonic:      false,
			expectedMethod:   "Private Key",
			expectedMnemonic: "Mnemonic not available (imported via private key)",
		},
		{
			name:             "Mnemonic import shows correct method",
			importMethod:     wallet.ImportMethodMnemonic,
			hasMnemonic:      true,
			expectedMethod:   "Mnemonic Phrase",
			expectedMnemonic: "test mnemonic phrase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock wallet details
			mockWallet := &wallet.Wallet{
				Name:         "Test Wallet",
				Address:      "0x1234567890123456789012345678901234567890",
				ImportMethod: string(tt.importMethod),
			}

			var mnemonicPtr *string
			if tt.hasMnemonic {
				mnemonic := "test mnemonic phrase"
				mnemonicPtr = &mnemonic
			}

			walletDetails := &wallet.WalletDetails{
				Wallet:       mockWallet,
				Mnemonic:     mnemonicPtr,
				ImportMethod: tt.importMethod,
				HasMnemonic:  tt.hasMnemonic,
			}

			// Create CLI model with wallet details
			model := &CLIModel{
				walletDetails: walletDetails,
			}

			// Get the wallet details view
			view := model.viewWalletDetails()

			// Verify the view contains expected information
			assert.Contains(t, view, tt.expectedMethod, "View should contain correct import method")

			if !tt.hasMnemonic {
				assert.Contains(t, view, tt.expectedMnemonic, "View should contain correct mnemonic message for non-mnemonic imports")
			}

			// Verify consistent terminology
			if tt.importMethod == wallet.ImportMethodKeystore {
				assert.Contains(t, view, "Keystore", "View should use consistent keystore terminology")
				assert.NotContains(t, view, "private key)", "View should not show private key message for keystore imports")
			}
		})
	}
}

func TestWalletDetailsViewMnemonicHandling(t *testing.T) {
	// Initialize localization for tests
	cfg := &config.Config{
		Language:  "en",
		LocaleDir: "../../pkg/localization/locales",
	}
	err := localization.InitLocalization(cfg)
	require.NoError(t, err)

	t.Run("Keystore import without mnemonic shows appropriate message", func(t *testing.T) {
		mockWallet := &wallet.Wallet{
			Name:         "Keystore Wallet",
			Address:      "0x1234567890123456789012345678901234567890",
			ImportMethod: string(wallet.ImportMethodKeystore),
		}

		walletDetails := &wallet.WalletDetails{
			Wallet:       mockWallet,
			Mnemonic:     nil,
			ImportMethod: wallet.ImportMethodKeystore,
			HasMnemonic:  false,
		}

		model := &CLIModel{
			walletDetails: walletDetails,
		}

		view := model.viewWalletDetails()

		// Should show keystore-specific message
		assert.Contains(t, view, "imported from keystore file", "Should show keystore-specific mnemonic message")
		assert.NotContains(t, view, "imported via private key", "Should not show private key message")
	})

	t.Run("Private key import without mnemonic shows appropriate message", func(t *testing.T) {
		mockWallet := &wallet.Wallet{
			Name:         "Private Key Wallet",
			Address:      "0x1234567890123456789012345678901234567890",
			ImportMethod: string(wallet.ImportMethodPrivateKey),
		}

		walletDetails := &wallet.WalletDetails{
			Wallet:       mockWallet,
			Mnemonic:     nil,
			ImportMethod: wallet.ImportMethodPrivateKey,
			HasMnemonic:  false,
		}

		model := &CLIModel{
			walletDetails: walletDetails,
		}

		view := model.viewWalletDetails()

		// Should show private key-specific message
		assert.Contains(t, view, "imported via private key", "Should show private key-specific mnemonic message")
		assert.NotContains(t, view, "imported from keystore file", "Should not show keystore message")
	})

	t.Run("Mnemonic import hides mnemonic by default", func(t *testing.T) {
		testMnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

		mockWallet := &wallet.Wallet{
			Name:         "Mnemonic Wallet",
			Address:      "0x1234567890123456789012345678901234567890",
			ImportMethod: string(wallet.ImportMethodMnemonic),
		}

		walletDetails := &wallet.WalletDetails{
			Wallet:       mockWallet,
			Mnemonic:     &testMnemonic,
			ImportMethod: wallet.ImportMethodMnemonic,
			HasMnemonic:  true,
		}

		model := &CLIModel{
			walletDetails: walletDetails,
		}

		view := model.viewWalletDetails()

		// Should hide the mnemonic until an explicit export flow is used
		assert.NotContains(t, view, testMnemonic)
		assert.Contains(t, view, localization.GetWalletImportMessage("sensitive_data_hidden"))
		assert.NotContains(t, view, "not available", "Should not show 'not available' message when mnemonic exists")
	})
}

func TestWalletDetailsViewImportMethodDisplay(t *testing.T) {
	// Initialize localization for tests
	cfg := &config.Config{
		Language:  "en",
		LocaleDir: "../../pkg/localization/locales",
	}
	err := localization.InitLocalization(cfg)
	require.NoError(t, err)

	t.Run("Import method labels are displayed correctly", func(t *testing.T) {
		testCases := []struct {
			importMethod wallet.ImportMethod
			expectedText string
		}{
			{wallet.ImportMethodKeystore, "Keystore"},
			{wallet.ImportMethodPrivateKey, "Private Key"},
			{wallet.ImportMethodMnemonic, "Mnemonic"},
		}

		for _, tc := range testCases {
			mockWallet := &wallet.Wallet{
				Name:         "Test Wallet",
				Address:      "0x1234567890123456789012345678901234567890",
				ImportMethod: string(tc.importMethod),
			}

			walletDetails := &wallet.WalletDetails{
				Wallet:       mockWallet,
				ImportMethod: tc.importMethod,
				HasMnemonic:  tc.importMethod == wallet.ImportMethodMnemonic,
			}

			model := &CLIModel{
				walletDetails: walletDetails,
			}

			view := model.viewWalletDetails()
			assert.Contains(t, view, tc.expectedText, "View should contain correct import method label for %s", tc.importMethod)
		}
	})
}
