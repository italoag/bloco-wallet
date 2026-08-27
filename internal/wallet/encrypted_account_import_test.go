package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestEncryptedAccountImportRejectsInvalidArtifacts(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	vault, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x24}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := vault.openEncryptedAccountExport(context.Background(), nil, nil); err == nil {
		t.Fatal("empty encrypted account export was accepted")
	}
	if _, _, err := vault.openEncryptedAccountExport(context.Background(), make([]byte, maxKeystoreImportSize+1), nil); err == nil {
		t.Fatal("oversized encrypted account export was accepted")
	}
	invalid := [][]byte{
		[]byte("not-json"),
		[]byte(`{"version":1}{"version":1}`),
		[]byte(`{"version":2,"signer_kind":"software","secret_envelope":"AQ=="}`),
		[]byte(`{"version":1,"signer_kind":"watch_only","secret_envelope":"AQ=="}`),
		[]byte(`{"version":1,"signer_kind":"software"}`),
	}
	for _, data := range invalid {
		if _, _, err := vault.openEncryptedAccountExport(context.Background(), data, []byte("password")); err == nil {
			t.Fatal("invalid encrypted account artifact was accepted")
		}
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := crypto.FromECDSA(key)
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	key.D.SetInt64(0)
	secretJSON, err := encodeCanonicalSecret(canonicalSecretV1{Version: canonicalSecretVersion, Kind: SecretTypePrivateKey, PrivateKey: privateKey})
	clear(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(secretJSON)
	exported := EncryptedAccountExportV1{
		Version:            encryptedAccountExportVersion,
		AccountID:          "018f76c1-04e7-4d55-8db4-f57c7ff9e3b2",
		Address:            address,
		SignerKind:         SignerKindSoftware,
		SecretType:         SecretTypeMnemonic,
		DerivationScheme:   "bip44",
		DerivationPath:     "m/44'/60'/0'/0/0",
		BIP39Language:      string(BIP39English),
		EnvelopeGeneration: 1,
	}
	exportPassword := []byte("Strong export password 1!")
	exported.SecretEnvelope, err = codec.Seal(exportPassword, exported.Metadata(), secretJSON)
	if err != nil {
		t.Fatal(err)
	}
	mismatched, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := vault.openEncryptedAccountExport(context.Background(), mismatched, exportPassword); err == nil {
		t.Fatal("encrypted account secret type mismatch was accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := vault.openEncryptedAccountExport(cancelled, []byte("{}"), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled encrypted import returned %v", err)
	}
	vault.Close()
	if _, err := vault.PreviewEncryptedAccountImport(context.Background(), []byte("{}"), nil); !errors.Is(err, ErrVaultClosed) {
		t.Fatalf("closed vault preview returned %v", err)
	}
	if _, err := vault.ImportEncryptedAccount(context.Background(), EncryptedAccountImportRequest{}); !errors.Is(err, ErrVaultClosed) {
		t.Fatalf("closed vault import returned %v", err)
	}
}

func TestPhaseOneEnvelopeExportsAsCanonicalBackup(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	repository := newMemoryAccountRepository()
	identityKey := bytes.Repeat([]byte{0x42}, 32)
	vault, err := NewWalletVault(repository, codec, VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		t.Fatal(err)
	}
	mnemonic := "test test test test test test test test test test test junk"
	path, _ := ParseDerivationPath("m/44'/60'/0'/0/0")
	privateKey, address, err := deriveEVMAccount(mnemonic, "", BIP39English, path)
	if err != nil {
		t.Fatal(err)
	}
	clear(privateKey)
	account := &Account{
		AccountID:          "018f76c1-04e7-4d55-8db4-f57c7ff9e3b2",
		Name:               "Phase one",
		Address:            address,
		SignerKind:         SignerKindSoftware,
		SignerReference:    "phase-one",
		SecretType:         SecretTypeMnemonic,
		DerivationScheme:   "bip44",
		DerivationPath:     path.String(),
		BIP39Language:      string(BIP39English),
		Capabilities:       CapabilitySignMessage | CapabilityExportSecret,
		State:              AccountStateActive,
		EnvelopeGeneration: 1,
		AuthorizationEpoch: 1,
		BackupGeneration:   1,
		SourceIdentity:     "phase-one-source",
		Revision:           1,
	}
	storagePassword := []byte("Strong phase one password 1!")
	account.SecretEnvelope, err = codec.Seal(storagePassword, metadataForAccount(account), []byte(mnemonic))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	handle, err := vault.Unlock(context.Background(), account.AccountID, storagePassword)
	if err != nil {
		t.Fatal(err)
	}
	exportPassword := []byte("Strong canonical backup 2!")
	destination := filepath.Join(t.TempDir(), "phase-one.bloco")
	if err := vault.ExportEncryptedAccount(context.Background(), EncryptedAccountExportRequest{
		Handle:             handle,
		Destination:        destination,
		CurrentPassword:    storagePassword,
		NewPassword:        exportPassword,
		ConfirmNewPassword: append([]byte(nil), exportPassword...),
	}); err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	destinationVault, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x24}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	newPassword := []byte("Strong restored password 3!")
	imported, err := destinationVault.ImportEncryptedAccount(context.Background(), EncryptedAccountImportRequest{
		Name:                   "Restored phase one",
		ExportJSON:             exported,
		ExportPassword:         exportPassword,
		StoragePassword:        newPassword,
		ConfirmStoragePassword: append([]byte(nil), newPassword...),
	})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Address != address {
		t.Fatal("phase one canonical export changed address")
	}
}

func TestEncryptedAccountExportImportRoundTrip(t *testing.T) {
	sourceVault, _, _ := newTestVault(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := hex.EncodeToString(crypto.FromECDSA(key))
	expectedAddress := crypto.PubkeyToAddress(key.PublicKey).Hex()
	key.D.SetInt64(0)
	storagePassword := []byte("Strong source storage password 1!")
	source, err := sourceVault.ImportPrivateKey(context.Background(), PrivateKeyImportRequest{
		Name:                   "Source",
		PrivateKey:             privateKey,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := sourceVault.Unlock(context.Background(), source.AccountID, storagePassword)
	if err != nil {
		t.Fatal(err)
	}
	exportPassword := []byte("Strong backup password 2!")
	destination := filepath.Join(t.TempDir(), "account.bloco")
	if err := sourceVault.ExportEncryptedAccount(context.Background(), EncryptedAccountExportRequest{
		Handle:             handle,
		Destination:        destination,
		CurrentPassword:    storagePassword,
		NewPassword:        exportPassword,
		ConfirmNewPassword: append([]byte(nil), exportPassword...),
	}); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	destinationVault, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x24}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destinationVault.PreviewEncryptedAccountImport(context.Background(), encoded, []byte("wrong backup password")); err == nil {
		t.Fatal("wrong encrypted export password was accepted")
	}
	preview, err := destinationVault.PreviewEncryptedAccountImport(context.Background(), encoded, exportPassword)
	if err != nil {
		t.Fatal(err)
	}
	newStoragePassword := []byte("Strong destination password 3!")
	importRequest := EncryptedAccountImportRequest{
		Name:                   "Restored",
		ExportJSON:             encoded,
		ExportPassword:         exportPassword,
		StoragePassword:        newStoragePassword,
		ConfirmStoragePassword: append([]byte(nil), newStoragePassword...),
	}
	imported, err := destinationVault.ImportEncryptedAccount(context.Background(), importRequest)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Address != expectedAddress || imported.Address != expectedAddress {
		t.Fatal("encrypted account round-trip changed address")
	}
	if _, err := destinationVault.ImportEncryptedAccount(context.Background(), importRequest); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("duplicate encrypted account import returned %v", err)
	}
	if _, err := destinationVault.Unlock(context.Background(), imported.AccountID, newStoragePassword); err != nil {
		t.Fatal(err)
	}
}
