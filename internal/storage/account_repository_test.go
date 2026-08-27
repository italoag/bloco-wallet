package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
)

func newAccountTestRepository(t *testing.T) *GORMRepository {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "accounts.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Error(err)
		}
	})
	return repository
}

func testAccount(id, sourceIdentity string) *wallet.Account {
	return &wallet.Account{
		AccountID:          id,
		Name:               "Test Account",
		Address:            "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		SignerKind:         wallet.SignerKindSoftware,
		SignerReference:    id,
		SecretType:         wallet.SecretTypeMnemonic,
		DerivationScheme:   "bip44",
		DerivationPath:     "m/44'/60'/0'/0/0",
		BIP39Language:      "english",
		Capabilities:       wallet.CapabilitySignTransaction | wallet.CapabilitySignMessage,
		State:              wallet.AccountStatePendingBackup,
		SecretEnvelope:     []byte("envelope-" + id),
		EnvelopeGeneration: 1,
		AuthorizationEpoch: 1,
		BackupGeneration:   1,
		SourceIdentity:     sourceIdentity,
		Revision:           1,
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
}

func TestAccountRepositoryAppliesSecurityPragmasAndMigrations(t *testing.T) {
	repository := newAccountTestRepository(t)
	var foreignKeys int
	if err := repository.db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error; err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatal("SQLite foreign keys are disabled")
	}
	var synchronous int
	if err := repository.db.Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil {
		t.Fatal(err)
	}
	if synchronous != 2 {
		t.Fatalf("SQLite synchronous mode is %d", synchronous)
	}
	var journalMode string
	if err := repository.db.Raw("PRAGMA journal_mode").Scan(&journalMode).Error; err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("SQLite journal mode is %s", journalMode)
	}
	var migrationCount int64
	if err := repository.db.Model(&schemaMigration{}).Count(&migrationCount).Error; err != nil {
		t.Fatal(err)
	}
	if migrationCount != 4 {
		t.Fatalf("expected four schema migrations, got %d", migrationCount)
	}
	if repository.db.Migrator().HasTable(&wallet.Wallet{}) {
		t.Fatal("fresh vault database created legacy wallet table")
	}
	if !repository.db.Migrator().HasTable(&wallet.VaultMetadata{}) {
		t.Fatal("fresh vault database omitted metadata table")
	}
	if err := repository.PutVaultMetadata(context.Background(), "identity", "fingerprint"); err != nil {
		t.Fatal(err)
	}
	if value, err := repository.GetVaultMetadata(context.Background(), "identity"); err != nil || value != "fingerprint" {
		t.Fatal("vault metadata did not round-trip")
	}
	if err := repository.PutVaultMetadata(context.Background(), "identity", "different"); !errors.Is(err, wallet.ErrAccountConflict) {
		t.Fatal("vault metadata conflict was accepted")
	}
}

func TestVaultRepositoryAppliesPhaseTwoAccountMigration(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "upgrade.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("018f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "upgrade-source")
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Migrator().DropColumn(&wallet.Account{}, "HasBIP39Passphrase"); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Delete(&schemaMigration{}, 3).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	if !reopened.db.Migrator().HasColumn(&wallet.Account{}, "HasBIP39Passphrase") {
		t.Fatal("phase two migration did not restore BIP39 passphrase metadata")
	}
	preserved, err := reopened.GetAccount(context.Background(), account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Address != account.Address || preserved.SourceIdentity != account.SourceIdentity {
		t.Fatal("phase two migration changed existing account data")
	}
}

func TestVaultRepositoryRejectsLegacyWalletData(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "legacy.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	legacy, err := NewWalletRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.AddWallet(&wallet.Wallet{
		Name:         "Legacy",
		Address:      "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		KeyStorePath: filepath.Join(root, "legacy.json"),
		ImportMethod: string(wallet.ImportMethodKeystore),
		SourceHash:   "legacy-source",
	}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	if repository, err := NewVaultRepository(cfg); err == nil {
		_ = repository.Close()
		t.Fatal("legacy custody data was accepted without migration")
	}
}

func TestVaultRepositoryRejectsUnknownSchemaVersion(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "future.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Create(&schemaMigration{Version: 99, AppliedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := NewVaultRepository(cfg); err == nil {
		_ = reopened.Close()
		t.Fatal("future schema version was accepted")
	}
}

func TestAccountRepositoryTransactionCommitAndRollback(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	rolledBack := testAccount("018f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-a")
	sentinel := errors.New("rollback")

	err := repository.WithAccountTransaction(ctx, func(tx wallet.AccountRepository) error {
		if err := tx.CreateAccount(ctx, rolledBack); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected rollback error, got %v", err)
	}
	if _, err := repository.GetAccount(ctx, rolledBack.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("rolled back account is visible: %v", err)
	}

	committed := testAccount("028f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-b")
	if err := repository.WithAccountTransaction(ctx, func(tx wallet.AccountRepository) error {
		return tx.CreateAccount(ctx, committed)
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetAccount(ctx, committed.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccountID != committed.AccountID {
		t.Fatal("committed account mismatch")
	}
}

func TestAccountRepositoryAllowsSameAddressWithIndependentEnvelopes(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	first := testAccount("118f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-first")
	second := testAccount("218f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-second")
	second.SecretEnvelope = []byte("different-envelope")

	if err := repository.CreateAccount(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAccount(ctx, second); err != nil {
		t.Fatal(err)
	}
	accounts, err := repository.ListAccounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected two accounts, got %d", len(accounts))
	}
	if accounts[0].AccountID == accounts[1].AccountID {
		t.Fatal("account IDs were shared")
	}
	if bytes.Equal(accounts[0].SecretEnvelope, accounts[1].SecretEnvelope) {
		t.Fatal("encrypted envelopes were shared")
	}
}

func TestAccountRepositoryUpdatesStateAndEnvelope(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := testAccount("318f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-update")
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	account.State = wallet.AccountStateActive
	account.SecretEnvelope = []byte("rotated-envelope")
	if err := repository.UpdateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetAccount(ctx, account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != wallet.AccountStateActive || string(loaded.SecretEnvelope) != "rotated-envelope" {
		t.Fatal("account update did not persist")
	}
}

func TestAccountRepositoryProtectsEnvelopeMetadataAndRevision(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := testAccount("618f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-revision")
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	stale, err := repository.GetAccount(ctx, account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Name = "Renamed"
	account.Address = "0x0000000000000000000000000000000000000001"
	account.DerivationPath = "m/44'/60'/9'/0/0"
	if err := repository.UpdateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetAccount(ctx, account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "Renamed" {
		t.Fatal("mutable name was not updated")
	}
	if loaded.Address == account.Address || loaded.DerivationPath == account.DerivationPath {
		t.Fatal("AAD-bound account metadata was mutated")
	}
	stale.Name = "Stale update"
	if err := repository.UpdateAccount(ctx, stale); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
}

func TestAccountRepositoryRejectsDuplicateIdentity(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	first := testAccount("418f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-duplicate")
	second := testAccount("518f76c1-04e7-4d55-8db4-f57c7ff9e3b2", "source-duplicate")
	if err := repository.CreateAccount(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAccount(ctx, second); !errors.Is(err, wallet.ErrAccountConflict) {
		t.Fatalf("expected account conflict, got %v", err)
	}
}
