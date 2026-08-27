package wallet

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestExportKeystoreV3InteroperatesWithGoEthereum(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(key))
	expectedAddress := crypto.PubkeyToAddress(key.PublicKey).Hex()
	key.D.SetInt64(0)
	storagePassword := []byte("Strong storage password 1!")
	summary, err := vault.ImportPrivateKey(context.Background(), PrivateKeyImportRequest{
		Name:                   "Exported",
		PrivateKey:             privateKeyHex,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := vault.Unlock(context.Background(), summary.AccountID, storagePassword)
	if err != nil {
		t.Fatal(err)
	}
	exportPassword := []byte("External export password 2!")
	destination := filepath.Join(t.TempDir(), "account.json")
	request := KeystoreV3ExportRequest{
		Handle:          handle,
		Destination:     destination,
		Password:        exportPassword,
		ConfirmPassword: append([]byte(nil), exportPassword...),
	}
	if err := vault.ExportKeystoreV3(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := keystore.DecryptKey(encoded, string(exportPassword))
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(decrypted.PrivateKey.PublicKey).Hex()
	decrypted.PrivateKey.D.SetInt64(0)
	if address != expectedAddress {
		t.Fatalf("exported address mismatch: %s", address)
	}
	if err := vault.ExportKeystoreV3(context.Background(), request); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing destination was overwritten: %v", err)
	}
	account, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Capabilities &^= CapabilityExportSecret
	if err := repository.UpdateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	request.Destination = filepath.Join(t.TempDir(), "denied.json")
	if err := vault.ExportKeystoreV3(context.Background(), request); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("export ignored capability policy: %v", err)
	}
}

func TestVerifyKeystoreV3ExportRejectsInvalidArtifacts(t *testing.T) {
	password := []byte("Strong export password 1!")
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	encoded, err := keystore.EncryptKey(&keystore.Key{
		Address:    crypto.PubkeyToAddress(key.PublicKey),
		PrivateKey: key,
	}, string(password), keystore.LightScryptN, keystore.LightScryptP)
	key.D.SetInt64(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyKeystoreV3Export([]byte("invalid"), password, address); err == nil {
		t.Fatal("invalid export artifact was accepted")
	}
	if err := verifyKeystoreV3Export(encoded, password, "0x0000000000000000000000000000000000000001"); err == nil {
		t.Fatal("wrong expected address was accepted")
	}
	if err := verifyKeystoreV3Export(encoded, []byte("wrong password"), address); err == nil {
		t.Fatal("wrong verification password was accepted")
	}
	var artifact map[string]any
	if err := json.Unmarshal(encoded, &artifact); err != nil {
		t.Fatal(err)
	}
	otherAddress := "0000000000000000000000000000000000000001"
	artifact["address"] = otherAddress
	tampered, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyKeystoreV3Export(tampered, password, "0x"+otherAddress); err == nil {
		t.Fatal("artifact with mismatched decrypted key was accepted")
	}
}

func TestExportKeystoreV3RequiresConfirmedNewPassword(t *testing.T) {
	vault, _, _ := newTestVault(t)
	if err := vault.ExportKeystoreV3(context.Background(), KeystoreV3ExportRequest{Destination: "relative.json"}); err == nil {
		t.Fatal("relative Keystore export destination was accepted")
	}
	destination := filepath.Join(t.TempDir(), "account.json")
	if err := vault.ExportKeystoreV3(context.Background(), KeystoreV3ExportRequest{
		Destination:     destination,
		Password:        []byte("Strong export password 1!"),
		ConfirmPassword: []byte("Different export password 2!"),
	}); !errors.Is(err, ErrStoragePasswordConfirmation) {
		t.Fatalf("mismatched export confirmation returned %v", err)
	}
	if err := vault.ExportKeystoreV3(context.Background(), KeystoreV3ExportRequest{
		Destination:     destination,
		Password:        []byte("short"),
		ConfirmPassword: []byte("short"),
	}); err == nil {
		t.Fatal("short export password was accepted")
	}
}
