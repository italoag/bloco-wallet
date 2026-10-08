package wallet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

func testSecretBatchPassword() []byte {
	return []byte("Strong batch storage password 1!")
}

func TestCanonicalMnemonicBatchImportSupportsPartialSuccess(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	validMnemonic := "test test test test test test test test test test test junk"
	storagePassword := testSecretBatchPassword()
	items := []SecretBatchItem{
		{Name: "wallet-one", SecretData: []byte(validMnemonic)},
		{Name: "wallet-two", SecretData: []byte("not a valid mnemonic phrase at all")},
		{Name: "wallet-dupe", SecretData: []byte(validMnemonic)},
	}
	results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{
		Kind:                   SecretBatchMnemonic,
		Items:                  items,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
		MaxConcurrency:         2,
	})
	if len(results) != 3 {
		t.Fatalf("expected three results, got %d", len(results))
	}
	// items[0] and items[2] hold the same mnemonic; under concurrency either
	// one may win the insert while the other is flagged as already imported.
	successes, alreadyImported, failures := 0, 0, 0
	for _, result := range results {
		switch {
		case result.Err != nil:
			failures++
		case result.AlreadyImported:
			alreadyImported++
		case result.Summary != nil:
			successes++
			if result.Summary.Address != "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" {
				t.Fatalf("unexpected address: %s", result.Summary.Address)
			}
		}
	}
	if successes != 1 || alreadyImported != 1 || failures != 1 {
		t.Fatalf("unexpected batch result: %d successes, %d existing, %d failures", successes, alreadyImported, failures)
	}
	if results[1].Err == nil {
		t.Fatal("invalid mnemonic was imported")
	}
	accounts, err := repository.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected one imported account, got %d", len(accounts))
	}
}

func TestCanonicalMnemonicBatchImportItemPassphraseOverridesGlobal(t *testing.T) {
	vault, _, _ := newTestVault(t)
	storagePassword := testSecretBatchPassword()
	mnemonic := "legal winner thank year wave sausage worth useful legal winner thank yellow"
	items := []SecretBatchItem{
		{Name: "no-passphrase", SecretData: []byte(mnemonic)},
		{Name: "with-passphrase", SecretData: []byte(mnemonic), Passphrase: []byte("item passphrase")},
	}
	results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{
		Kind:                   SecretBatchMnemonic,
		Items:                  items,
		BIP39Language:          BIP39English,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	})
	if results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("unexpected failures: %v / %v", results[0].Err, results[1].Err)
	}
	if results[0].Summary.Address == results[1].Summary.Address {
		t.Fatal("per-item passphrase did not change the derived address")
	}
}

func TestCanonicalPrivateKeyBatchImportSupportsPartialSuccess(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	storagePassword := testSecretBatchPassword()
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatal(err)
	}
	keyBytes[0] |= 0x01
	validHex := hex.EncodeToString(keyBytes)
	items := []SecretBatchItem{
		{Name: "key-one", SecretData: []byte("0x" + validHex)},
		{Name: "key-short", SecretData: []byte("0xdeadbeef")},
		{Name: "key-dupe", SecretData: []byte(validHex)},
	}
	results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{
		Kind:                   SecretBatchPrivateKey,
		Items:                  items,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	})
	if len(results) != 3 {
		t.Fatalf("expected three results, got %d", len(results))
	}
	// items[0] and items[2] hold the same key; under concurrency either one may
	// win the insert while the other is flagged as already imported.
	successes, alreadyImported, failures := 0, 0, 0
	for _, result := range results {
		switch {
		case result.Err != nil:
			failures++
		case result.AlreadyImported:
			alreadyImported++
		case result.Summary != nil:
			successes++
		}
	}
	if successes != 1 || alreadyImported != 1 || failures != 1 {
		t.Fatalf("unexpected batch result: %d successes, %d existing, %d failures", successes, alreadyImported, failures)
	}
	if results[1].Err == nil {
		t.Fatal("short private key was imported")
	}
	accounts, err := repository.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected one imported account, got %d", len(accounts))
	}
}

func TestCanonicalSecretBatchImportValidatesBatchPolicy(t *testing.T) {
	vault, _, _ := newTestVault(t)
	storagePassword := testSecretBatchPassword()
	items := []SecretBatchItem{{Name: "one", SecretData: []byte("test test test test test test test test test test test junk")}}

	if results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{Kind: SecretBatchMnemonic}); len(results) != 0 {
		t.Fatal("empty batch returned results")
	}
	tooMany := make([]SecretBatchItem, maxCanonicalBatchItems+1)
	if results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{Kind: SecretBatchMnemonic, Items: tooMany}); len(results) != 1 || results[0].Err == nil {
		t.Fatal("oversized batch was accepted")
	}
	if results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{
		Kind:                   SecretBatchKind("unknown"),
		Items:                  items,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	}); len(results) != 1 || results[0].Err == nil {
		t.Fatal("unknown batch kind was accepted")
	}
	if results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{
		Kind:                   SecretBatchMnemonic,
		Items:                  items,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: []byte("different password"),
	}); len(results) != 1 || !errors.Is(results[0].Err, ErrStoragePasswordConfirmation) {
		t.Fatal("storage password mismatch was accepted")
	}
	if results := vault.ImportSecretBatch(context.Background(), SecretBatchImportRequest{
		Kind:                   SecretBatchMnemonic,
		Items:                  []SecretBatchItem{{Name: "one", SecretData: []byte("x"), PreflightErr: fmt.Errorf("preflight")}},
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	}); len(results) != 1 || results[0].Err == nil || results[0].Err.Error() != "preflight" {
		t.Fatal("preflight error was not propagated")
	}
}
