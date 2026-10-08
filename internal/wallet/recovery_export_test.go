package wallet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

const (
	recoveryTestMnemonic = "test test test test test test test test test test test junk"
	recoveryTestKey      = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	recoveryTestAddress  = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"
	recoveryTestPassword = "recovery test password 1!"
)

func importRecoveryMnemonic(t *testing.T, vault *WalletVault, request MnemonicImportRequest) AccountSummary {
	t.Helper()
	if request.Name == "" {
		request.Name = "Recovery Test"
	}
	if len(request.StoragePassword) == 0 {
		request.StoragePassword = []byte(recoveryTestPassword)
	}
	if len(request.ConfirmStoragePassword) == 0 {
		request.ConfirmStoragePassword = append([]byte(nil), request.StoragePassword...)
	}
	summary, err := vault.ImportMnemonic(context.Background(), request)
	require.NoError(t, err)
	return summary
}

func recoveryRequest(summary AccountSummary, kind RecoverySecretKind) RecoverySecretRequest {
	return RecoverySecretRequest{
		AccountID:        summary.AccountID,
		ConfirmAccountID: summary.AccountID,
		Password:         []byte(recoveryTestPassword),
		Kind:             kind,
	}
}

func TestRecoveryRevealMnemonicWordsAndKey(t *testing.T) {
	vault, _, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})
	require.Equal(t, recoveryTestAddress, summary.Address)

	material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	require.NoError(t, err)
	words := strings.Fields(string(material.Bytes()))
	assert.Equal(t, strings.Fields(recoveryTestMnemonic), words)
	assert.Len(t, words, 12)
	assert.Equal(t, summary.AccountID, material.AccountID)
	assert.Equal(t, summary.Address, material.Address)
	assert.Equal(t, "m/44'/60'/0'/0/0", material.DerivationPath)
	assert.Equal(t, BIP39English, material.BIP39Language)
	assert.False(t, material.HasBIP39Passphrase)
	material.Destroy()
	assert.Empty(t, material.Bytes())
	material.Destroy()

	keyMaterial, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryPrivateKey))
	require.NoError(t, err)
	defer keyMaterial.Destroy()
	assert.Equal(t, "0x"+recoveryTestKey, string(keyMaterial.Bytes()))
	decoded, err := crypto.HexToECDSA(recoveryTestKey)
	require.NoError(t, err)
	assert.Equal(t, recoveryTestAddress, crypto.PubkeyToAddress(decoded.PublicKey).Hex())
}

func TestRecoveryPrivateKeyAccountHasNoMnemonic(t *testing.T) {
	vault, _, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary, err := vault.ImportPrivateKey(context.Background(), PrivateKeyImportRequest{
		Name: "Key Only", PrivateKey: recoveryTestKey,
		StoragePassword: []byte(recoveryTestPassword), ConfirmStoragePassword: []byte(recoveryTestPassword),
	})
	require.NoError(t, err)

	_, err = vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, ErrRecoveryUnavailable)
	_, err = vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryPassphrase))
	assert.ErrorIs(t, err, ErrRecoveryUnavailable)

	keyMaterial, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryPrivateKey))
	require.NoError(t, err)
	defer keyMaterial.Destroy()
	assert.Equal(t, "0x"+recoveryTestKey, string(keyMaterial.Bytes()))

	destination := filepath.Join(t.TempDir(), "out.key")
	require.NoError(t, vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryPrivateKey), Destination: destination,
	}))
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, "0x"+recoveryTestKey+"\n", string(content))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
}

func TestRecoveryKeystoreAccountKeyExport(t *testing.T) {
	vault, _, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	key, err := crypto.HexToECDSA(recoveryTestKey)
	require.NoError(t, err)
	keystoreJSON, err := keystore.EncryptKey(&keystore.Key{
		Id:         uuid.New(),
		Address:    crypto.PubkeyToAddress(key.PublicKey),
		PrivateKey: key,
	}, "source password", keystore.LightScryptN, keystore.LightScryptP)
	require.NoError(t, err)
	summary, err := vault.ImportKeystore(context.Background(), KeystoreImportRequest{
		Name: "Keystore", KeystoreJSON: keystoreJSON, SourcePassword: []byte("source password"),
		StoragePassword: []byte(recoveryTestPassword), ConfirmStoragePassword: []byte(recoveryTestPassword),
	})
	require.NoError(t, err)
	material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryPrivateKey))
	require.NoError(t, err)
	defer material.Destroy()
	assert.Equal(t, "0x"+recoveryTestKey, string(material.Bytes()))
}

func TestRecoveryMnemonicWordCountsAndLanguages(t *testing.T) {
	for _, wordCount := range []int{12, 15, 18, 21, 24} {
		vault, _, _ := newTestVault(t)
		mnemonic, err := generateMnemonicForLanguage(wordCount, BIP39English)
		require.NoError(t, err)
		summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Name: fmt.Sprintf("wc%d", wordCount), Mnemonic: mnemonic})
		material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
		require.NoError(t, err)
		assert.Len(t, strings.Fields(string(material.Bytes())), wordCount)
		material.Destroy()
		vault.Close()
	}

	vault, _, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	mnemonic, err := generateMnemonicForLanguage(12, BIP39Spanish)
	require.NoError(t, err)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{
		Name: "Spanish", Mnemonic: mnemonic, BIP39Language: BIP39Spanish, DerivationPath: "m/44'/60'/1'/0/5",
	})
	material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	require.NoError(t, err)
	defer material.Destroy()
	assert.Equal(t, normalizedMnemonic(mnemonic), string(material.Bytes()))
	assert.Equal(t, BIP39Spanish, material.BIP39Language)
	assert.Equal(t, "m/44'/60'/1'/0/5", material.DerivationPath)
}

func TestRecoveryPassphraseExactBytesAndPathDerivedKey(t *testing.T) {
	vault, _, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	passphrase := "  pass phrase\ncafé 日本語  "
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{
		Name: "Passphrase", Mnemonic: recoveryTestMnemonic,
		BIP39Passphrase: passphrase, DerivationPath: "m/44'/60'/2'/0/7",
	})
	require.True(t, summary.HasBIP39Passphrase)

	material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryPassphrase))
	require.NoError(t, err)
	defer material.Destroy()
	assert.Equal(t, norm.NFKD.String(passphrase), string(material.Bytes()), "passphrase must be the canonical stored bytes")

	seedDestination := filepath.Join(t.TempDir(), "seed.mnemonic")
	require.NoError(t, vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: seedDestination,
	}))
	seedContent, err := os.ReadFile(seedDestination)
	require.NoError(t, err)
	assert.Equal(t, normalizedMnemonic(recoveryTestMnemonic)+"\n", string(seedContent))
	assert.NotContains(t, string(seedContent), "pass phrase")

	passDestination := filepath.Join(t.TempDir(), "seed.passphrase")
	require.NoError(t, vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryPassphrase), Destination: passDestination,
	}))
	passContent, err := os.ReadFile(passDestination)
	require.NoError(t, err)
	assert.Equal(t, norm.NFKD.String(passphrase), string(passContent), "passphrase file must be exact bytes with no appended newline")

	keyMaterial, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryPrivateKey))
	require.NoError(t, err)
	defer keyMaterial.Destroy()
	raw := keyMaterial.Bytes()
	require.Len(t, raw, 66)
	decoded, err := crypto.HexToECDSA(string(raw[2:]))
	require.NoError(t, err)
	assert.Equal(t, summary.Address, crypto.PubkeyToAddress(decoded.PublicKey).Hex(), "exported key must derive the stored address for the account's own path")
}

func TestRecoveryLegacyRawMnemonicEnvelope(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	accountID, err := newUUID(vault.options.Random)
	require.NoError(t, err)
	account := &Account{
		AccountID: accountID, Name: "Legacy", Address: recoveryTestAddress,
		SignerKind: SignerKindSoftware, SignerReference: accountID,
		SecretType: SecretTypeMnemonic, DerivationPath: "m/44'/60'/0'/0/0",
		BIP39Language: string(BIP39English),
		Capabilities:  CapabilitySignTransaction | CapabilitySignMessage | CapabilityExportSecret,
		State:         AccountStateActive, EnvelopeGeneration: 1, AuthorizationEpoch: 1,
		BackupGeneration: 1, SourceIdentity: "legacy:" + accountID, Revision: 1,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	envelope, err := vault.codec.Seal([]byte(recoveryTestPassword), metadataForAccount(account), []byte(recoveryTestMnemonic))
	require.NoError(t, err)
	account.SecretEnvelope = envelope
	require.NoError(t, repository.CreateAccount(context.Background(), account))

	material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summaryFromAccount(account), RecoveryMnemonic))
	require.NoError(t, err)
	defer material.Destroy()
	assert.Equal(t, recoveryTestMnemonic, string(material.Bytes()))
}

func TestRecoveryRejectsInvalidRequests(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})
	handle, err := vault.Unlock(context.Background(), summary.AccountID, []byte(recoveryTestPassword))
	require.NoError(t, err)
	_ = handle

	cases := map[string]RecoverySecretRequest{
		"wrong password":  {AccountID: summary.AccountID, ConfirmAccountID: summary.AccountID, Password: []byte("wrong password"), Kind: RecoveryMnemonic},
		"empty password":  {AccountID: summary.AccountID, ConfirmAccountID: summary.AccountID, Kind: RecoveryMnemonic},
		"empty account":   {ConfirmAccountID: "", Password: []byte(recoveryTestPassword), Kind: RecoveryMnemonic},
		"wrong confirm":   {AccountID: summary.AccountID, ConfirmAccountID: "other-id", Password: []byte(recoveryTestPassword), Kind: RecoveryMnemonic},
		"unknown kind":    {AccountID: summary.AccountID, ConfirmAccountID: summary.AccountID, Password: []byte(recoveryTestPassword), Kind: RecoverySecretKind("nope")},
		"missing account": {AccountID: "00000000-0000-4000-8000-000000000000", ConfirmAccountID: "00000000-0000-4000-8000-000000000000", Password: []byte(recoveryTestPassword), Kind: RecoveryMnemonic},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			material, err := vault.RevealRecoverySecret(context.Background(), request)
			assert.Error(t, err)
			assert.Nil(t, material)
		})
	}
	wrongPass := cases["wrong password"]
	_, err = vault.RevealRecoverySecret(context.Background(), wrongPass)
	assert.ErrorIs(t, err, ErrRecoveryAuthentication)
	assert.NotContains(t, err.Error(), "test test")

	_, err = vault.RevealRecoverySecret(context.Background(), cases["empty password"])
	assert.ErrorIs(t, err, ErrRecoveryAuthentication)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = vault.RevealRecoverySecret(cancelled, recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, context.Canceled)

	record, err := repository.GetAccount(context.Background(), summary.AccountID)
	require.NoError(t, err)
	record.Capabilities &^= CapabilityExportSecret
	require.NoError(t, repository.UpdateAccount(context.Background(), record))
	_, err = vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, ErrCapabilityDenied)
	record, err = repository.GetAccount(context.Background(), summary.AccountID)
	require.NoError(t, err)
	record.Capabilities |= CapabilityExportSecret
	require.NoError(t, repository.UpdateAccount(context.Background(), record))

	record.State = AccountStateUnavailable
	require.NoError(t, repository.UpdateAccount(context.Background(), record))
	_, err = vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, ErrRecoveryUnavailable)

	record, err = repository.GetAccount(context.Background(), summary.AccountID)
	require.NoError(t, err)
	record.State = AccountStateActive
	require.NoError(t, repository.UpdateAccount(context.Background(), record))

	watch, err := vault.ImportWatchOnly(context.Background(), WatchOnlyImportRequest{Name: "Watch", Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"})
	require.NoError(t, err)
	for _, kind := range []RecoverySecretKind{RecoveryMnemonic, RecoveryPrivateKey, RecoveryPassphrase} {
		_, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(watch, kind))
		assert.ErrorIs(t, err, ErrRecoveryUnavailable)
	}

	pending := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Name: "Pending", Mnemonic: "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"})
	pendingRecord, err := repository.GetAccount(context.Background(), pending.AccountID)
	require.NoError(t, err)
	pendingRecord.State = AccountStatePendingBackup
	require.NoError(t, repository.UpdateAccount(context.Background(), pendingRecord))
	_, err = vault.RevealRecoverySecret(context.Background(), recoveryRequest(pending, RecoveryMnemonic))
	assert.ErrorIs(t, err, ErrRecoveryUnavailable)
}

func TestRecoveryRejectsTamperedAccountAndClosedVault(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})
	record, err := repository.GetAccount(context.Background(), summary.AccountID)
	require.NoError(t, err)
	record.SecretType = SecretType("bogus")
	require.NoError(t, repository.UpdateAccount(context.Background(), record))
	_, err = vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, ErrRecoveryUnavailable)

	vault.Close()
	_, err = vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, ErrVaultClosed)
}

func TestRecoveryMaterialRedaction(t *testing.T) {
	material := &RecoveryMaterial{AccountID: "id", Address: "addr", Kind: RecoveryPrivateKey, data: []byte("0xdeadbeef")}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		rendered := fmt.Sprintf(format, material)
		assert.Equal(t, "[redacted recovery material]", rendered)
		renderedValue := fmt.Sprintf(format, *material)
		assert.Equal(t, "[redacted recovery material]", renderedValue)
	}
	encoded, err := json.Marshal(material)
	assert.ErrorIs(t, err, ErrRecoverySerialization)
	assert.Nil(t, encoded)
	encoded, err = json.Marshal(*material)
	assert.ErrorIs(t, err, ErrRecoverySerialization)
	assert.Nil(t, encoded)
	assert.Equal(t, "0xdeadbeef", string(material.Bytes()))
	var nilMaterial *RecoveryMaterial
	assert.Nil(t, nilMaterial.Bytes())
	nilMaterial.Destroy()
}

func TestRecoveryExportFileSemantics(t *testing.T) {
	vault, _, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})

	relative := RecoveryExportRequest{RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: "relative/out.mnemonic"}
	err := vault.ExportRecoverySecret(context.Background(), relative)
	assert.Error(t, err)

	destination := filepath.Join(t.TempDir(), "backup.mnemonic")
	require.NoError(t, vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: destination,
	}))
	content, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, normalizedMnemonic(recoveryTestMnemonic)+"\n", string(content))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}

	err = vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: destination,
	})
	assert.ErrorIs(t, err, os.ErrExist)
	after, err := os.ReadFile(destination)
	require.NoError(t, err)
	assert.Equal(t, content, after)

	empty := filepath.Join(t.TempDir(), "empty.mnemonic")
	require.NoError(t, os.WriteFile(empty, nil, 0600))
	err = vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: empty,
	})
	assert.ErrorIs(t, err, os.ErrExist)
	data, err := os.ReadFile(empty)
	require.NoError(t, err)
	assert.Empty(t, data)

	symlinkDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "real")
	require.NoError(t, os.MkdirAll(target, 0700))
	linkParent := filepath.Join(symlinkDir, "linked")
	if err := os.Symlink(target, linkParent); err == nil {
		err = vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
			RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: filepath.Join(linkParent, "x.mnemonic"),
		})
		assert.Error(t, err)
	}

	sentinel := filepath.Join(t.TempDir(), "sentinel.mnemonic")
	require.NoError(t, os.WriteFile(sentinel, []byte("keep"), 0600))
	linkDest := filepath.Join(t.TempDir(), "link.mnemonic")
	if err := os.Symlink(sentinel, linkDest); err == nil {
		err = vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
			RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: linkDest,
		})
		assert.Error(t, err, "existing destination symlink must be refused")
		data, readErr := os.ReadFile(sentinel)
		require.NoError(t, readErr)
		assert.Equal(t, "keep", string(data), "symlink target must remain untouched")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cancelDest := filepath.Join(t.TempDir(), "cancelled.mnemonic")
	err = vault.ExportRecoverySecret(cancelled, RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: cancelDest,
	})
	assert.Error(t, err)
	_, statErr := os.Stat(cancelDest)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRecoveryNonSoftwareSignersDenied(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	kinds := map[SignerKind]string{
		SignerKindWatchOnly: "watch",
		SignerKindHardware:  "hardware",
		SignerKindCloud:     "cloud",
		SignerKindMultisig:  "multisig",
	}
	for signerKind, suffix := range kinds {
		accountID, err := newUUID(vault.options.Random)
		require.NoError(t, err)
		capabilities := AccountCapability(0)
		if signerKind != SignerKindWatchOnly {
			capabilities = CapabilitySignTransaction | CapabilitySignMessage
		}
		account := &Account{
			AccountID: accountID, Name: "Ext " + suffix, Address: recoveryTestAddress,
			SignerKind: signerKind, SignerReference: "ref-" + suffix,
			Capabilities: capabilities, State: AccountStateActive,
			EnvelopeGeneration: 1, AuthorizationEpoch: 1, BackupGeneration: 1,
			SourceIdentity: "external:" + suffix + ":" + accountID, Revision: 1,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		require.NoError(t, repository.CreateAccount(context.Background(), account))
		summary := summaryFromAccount(account)
		for _, kind := range []RecoverySecretKind{RecoveryMnemonic, RecoveryPrivateKey, RecoveryPassphrase} {
			material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, kind))
			assert.ErrorIs(t, err, ErrRecoveryUnavailable, "%s account must not yield %s", signerKind, kind)
			assert.Nil(t, material)
		}
	}
}

func TestRecoveryRejectsTombstonedAccount(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})
	record, err := repository.GetAccount(context.Background(), summary.AccountID)
	require.NoError(t, err)
	record.State = AccountStateTombstoned
	record.SecretEnvelope = nil
	record.Capabilities = 0
	record.EnvelopeGeneration++
	record.AuthorizationEpoch++
	require.NoError(t, repository.UpdateAccount(context.Background(), record))
	for _, kind := range []RecoverySecretKind{RecoveryMnemonic, RecoveryPrivateKey, RecoveryPassphrase} {
		material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, kind))
		assert.Error(t, err)
		assert.Nil(t, material)
	}
}

type recoveryMutatingRepository struct {
	AccountRepository
	calls  int
	mutate func(*Account)
}

func (repository *recoveryMutatingRepository) GetAccount(ctx context.Context, accountID string) (*Account, error) {
	repository.calls++
	account, err := repository.AccountRepository.GetAccount(ctx, accountID)
	if err == nil && repository.calls == 2 && repository.mutate != nil {
		repository.mutate(account)
	}
	return account, err
}

func TestRecoveryRejectsRevisionChangeBetweenReads(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})
	wrapped := &recoveryMutatingRepository{AccountRepository: repository, mutate: func(account *Account) {
		account.Revision++
	}}
	vault.repository = wrapped
	material, err := vault.RevealRecoverySecret(context.Background(), recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, ErrCapabilityExpired)
	assert.Nil(t, material)

	wrapped.calls = 0
	destination := filepath.Join(t.TempDir(), "out.mnemonic")
	err = vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: destination,
	})
	assert.ErrorIs(t, err, ErrCapabilityExpired)
	_, statErr := os.Stat(destination)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRecoveryCancelledContextAfterSecondRead(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})
	ctx, cancel := context.WithCancel(context.Background())
	wrapped := &recoveryMutatingRepository{AccountRepository: repository, mutate: func(*Account) { cancel() }}
	vault.repository = wrapped
	material, err := vault.RevealRecoverySecret(ctx, recoveryRequest(summary, RecoveryMnemonic))
	assert.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, material)

	wrapped.calls = 0
	exportCtx, exportCancel := context.WithCancel(context.Background())
	wrapped.mutate = func(*Account) { exportCancel() }
	destination := filepath.Join(t.TempDir(), "cancelled.mnemonic")
	err = vault.ExportRecoverySecret(exportCtx, RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: destination,
	})
	assert.ErrorIs(t, err, context.Canceled)
	assertNoRecoveryFile(t, destination)
}

func TestRecoveryExportFailuresLeaveNoFile(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	t.Cleanup(vault.Close)
	summary := importRecoveryMnemonic(t, vault, MnemonicImportRequest{Mnemonic: recoveryTestMnemonic})
	destination := filepath.Join(t.TempDir(), "never.mnemonic")

	badConfirm := recoveryRequest(summary, RecoveryMnemonic)
	badConfirm.ConfirmAccountID = "0"
	err := vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{RecoverySecretRequest: badConfirm, Destination: destination})
	assert.ErrorIs(t, err, ErrRecoveryConfirmation)
	assertNoRecoveryFile(t, destination)

	badPassword := recoveryRequest(summary, RecoveryMnemonic)
	badPassword.Password = []byte("nope")
	err = vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{RecoverySecretRequest: badPassword, Destination: destination})
	assert.ErrorIs(t, err, ErrRecoveryAuthentication)
	assert.NotContains(t, err.Error(), "test test")
	assertNoRecoveryFile(t, destination)

	record, err := repository.GetAccount(context.Background(), summary.AccountID)
	require.NoError(t, err)
	record.Capabilities &^= CapabilityExportSecret
	require.NoError(t, repository.UpdateAccount(context.Background(), record))
	err = vault.ExportRecoverySecret(context.Background(), RecoveryExportRequest{
		RecoverySecretRequest: recoveryRequest(summary, RecoveryMnemonic), Destination: destination,
	})
	assert.ErrorIs(t, err, ErrCapabilityDenied)
	assertNoRecoveryFile(t, destination)
}

func assertNoRecoveryFile(t *testing.T, destination string) {
	t.Helper()
	_, err := os.Stat(destination)
	assert.True(t, os.IsNotExist(err))
	matches, globErr := filepath.Glob(filepath.Join(filepath.Dir(destination), ".bloco-export-*"))
	require.NoError(t, globErr)
	assert.Empty(t, matches, "no temporary residue after failed export")
}
