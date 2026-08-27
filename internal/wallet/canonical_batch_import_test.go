package wallet

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

func TestCanonicalKeystoreBatchImportSupportsPartialSuccess(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	sourcePassword := []byte("External batch password")
	items := make([]KeystoreBatchItem, 3)
	for index := range items {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := keystore.EncryptKey(&keystore.Key{
			Id:         uuid.New(),
			Address:    crypto.PubkeyToAddress(key.PublicKey),
			PrivateKey: key,
		}, string(sourcePassword), keystore.LightScryptN, keystore.LightScryptP)
		key.D.SetInt64(0)
		if err != nil {
			t.Fatal(err)
		}
		items[index] = KeystoreBatchItem{Name: fmt.Sprintf("Batch %d", index), KeystoreJSON: encoded, SourcePassword: sourcePassword}
	}
	items[2].KeystoreJSON = append([]byte(nil), items[0].KeystoreJSON...)
	storagePassword := []byte("Strong batch storage password 1!")
	results := vault.ImportKeystoreBatch(context.Background(), KeystoreBatchImportRequest{
		Items:                  items,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
		MaxConcurrency:         2,
	})
	if len(results) != 3 {
		t.Fatalf("expected three results, got %d", len(results))
	}
	successes := 0
	alreadyImported := 0
	failures := 0
	for _, result := range results {
		if result.Err != nil {
			failures++
		} else if result.AlreadyImported {
			alreadyImported++
		} else if result.Summary != nil {
			successes++
		}
	}
	if successes != 2 || alreadyImported != 1 || failures != 0 {
		t.Fatalf("unexpected batch result: %d successes, %d existing, %d failures", successes, alreadyImported, failures)
	}
	accounts, err := repository.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected two imported accounts, got %d", len(accounts))
	}
}

func TestCanonicalKeystoreBatchImportValidatesBatchPolicy(t *testing.T) {
	vault, _, _ := newTestVault(t)
	if results := vault.ImportKeystoreBatch(context.Background(), KeystoreBatchImportRequest{}); len(results) != 0 {
		t.Fatal("empty batch returned results")
	}
	tooMany := make([]KeystoreBatchItem, maxCanonicalBatchItems+1)
	if results := vault.ImportKeystoreBatch(context.Background(), KeystoreBatchImportRequest{Items: tooMany}); len(results) != 1 || results[0].Err == nil {
		t.Fatal("oversized batch item count was accepted")
	}
	item := KeystoreBatchItem{Name: "Invalid", KeystoreJSON: []byte("{}"), SourcePassword: []byte("source")}
	password := []byte("Strong batch storage password 1!")
	mismatch := vault.ImportKeystoreBatch(context.Background(), KeystoreBatchImportRequest{
		Items:                  []KeystoreBatchItem{item},
		StoragePassword:        password,
		ConfirmStoragePassword: []byte("Different batch password 2!"),
	})
	if len(mismatch) != 1 || !errors.Is(mismatch[0].Err, ErrStoragePasswordConfirmation) {
		t.Fatal("batch accepted mismatched password confirmation")
	}
	short := vault.ImportKeystoreBatch(context.Background(), KeystoreBatchImportRequest{
		Items:                  []KeystoreBatchItem{item},
		StoragePassword:        []byte("short"),
		ConfirmStoragePassword: []byte("short"),
	})
	if len(short) != 1 || short[0].Err == nil {
		t.Fatal("batch accepted short storage password")
	}
	for _, concurrency := range []int{0, 99} {
		results := vault.ImportKeystoreBatch(context.Background(), KeystoreBatchImportRequest{
			Items:                  []KeystoreBatchItem{item},
			StoragePassword:        password,
			ConfirmStoragePassword: append([]byte(nil), password...),
			MaxConcurrency:         concurrency,
		})
		if len(results) != 1 || results[0].Err == nil {
			t.Fatal("invalid batch item did not report an error")
		}
	}
}

func TestCanonicalKeystoreBatchImportHonorsCancellation(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := vault.ImportKeystoreBatch(ctx, KeystoreBatchImportRequest{
		Items:                  []KeystoreBatchItem{{Name: "Cancelled", KeystoreJSON: []byte("{}"), SourcePassword: []byte("source")}},
		StoragePassword:        []byte("Strong batch storage password 1!"),
		ConfirmStoragePassword: []byte("Strong batch storage password 1!"),
	})
	if len(results) != 1 || results[0].Err == nil {
		t.Fatal("cancelled batch did not report cancellation")
	}
	accounts, err := repository.ListAccounts(context.Background())
	if err != nil || len(accounts) != 0 {
		t.Fatal("cancelled batch persisted an account")
	}
}
