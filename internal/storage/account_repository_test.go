package storage

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/evm"
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

func TestAccountRepositoryRejectsImmutableSignerDiscriminatorConfusion(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := testAccount("11111111-1111-4111-8111-111111111111", "watch-only-immutable")
	account.SignerKind = wallet.SignerKindWatchOnly
	account.SignerReference = "watch-only:v1:" + account.Address
	account.SecretType = ""
	account.DerivationScheme = ""
	account.DerivationPath = ""
	account.BIP39Language = ""
	account.Capabilities = 0
	account.State = wallet.AccountStateActive
	account.SecretEnvelope = nil
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	mutated := *account
	mutated.SignerKind = wallet.SignerKindHardware
	mutated.Capabilities = wallet.CapabilitySignTransaction
	if err := repository.UpdateAccount(context.Background(), &mutated); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("signer discriminator confusion returned %v", err)
	}
	mutated = *account
	mutated.SignerKind = wallet.SignerKindSoftware
	mutated.SecretType = wallet.SecretTypePrivateKey
	mutated.SecretEnvelope = []byte("secret")
	if err := repository.UpdateAccount(context.Background(), &mutated); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("secret discriminator confusion returned %v", err)
	}
	if err := repository.db.Model(&wallet.Account{}).Where("account_id = ?", account.AccountID).Update("capabilities", wallet.CapabilitySignTransaction).Error; err == nil {
		t.Fatal("database trigger allowed watch-only signing capability")
	}
	stored, err := repository.GetAccount(context.Background(), account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SignerKind != wallet.SignerKindWatchOnly || stored.Capabilities != 0 || len(stored.SecretEnvelope) != 0 {
		t.Fatalf("watch-only account was corrupted: %+v", stored)
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
	if migrationCount != int64(latestSchemaVersion) {
		t.Fatalf("expected %d schema migrations, got %d", latestSchemaVersion, migrationCount)
	}
	for _, index := range []string{"ix_evm_history_account", "ix_evm_history_sender"} {
		if !repository.db.Migrator().HasIndex("evm_transactions", index) {
			t.Fatalf("history index %s is missing", index)
		}
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

func TestVaultRepositoryAppliesHistoryAndWatchGuardMigrations(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{AppDir: root, DatabasePath: filepath.Join(root, "history-upgrade.db"), Database: config.DatabaseConfig{Type: "sqlite"}}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	record := createAuthorizedTestTransaction(t, repository, time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC))
	for _, statement := range []string{
		"DROP INDEX ix_evm_history_account",
		"DROP INDEX ix_evm_history_sender",
		"DROP TRIGGER trg_evm_history_key_immutable",
		"DROP TRIGGER trg_account_identity_immutable",
		"DROP TRIGGER trg_watch_only_custody_insert",
		"DROP TRIGGER trg_watch_only_custody_update",
		"DROP TRIGGER trg_message_approval_binding_immutable",
		"DROP TRIGGER trg_message_signing_binding_immutable",
		"DROP TRIGGER trg_message_signature_hash_write_once",
		"DROP TABLE message_signing_records",
		"DROP TABLE message_signing_approvals",
		"DROP TRIGGER trg_evm_effect_immutable",
		"DROP TABLE evm_transaction_effects",
		"ALTER TABLE evm_transactions DROP COLUMN token_id",
	} {
		if err := repository.db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.db.Where("version >= ?", 7).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	for _, name := range []string{"ix_evm_history_account", "ix_evm_history_sender"} {
		if !reopened.db.Migrator().HasIndex("evm_transactions", name) {
			t.Fatalf("upgrade omitted history index %s", name)
		}
	}
	var triggerCount int64
	if err := reopened.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN ?", []string{"trg_evm_history_key_immutable", "trg_account_identity_immutable", "trg_watch_only_custody_insert", "trg_watch_only_custody_update"}).Scan(&triggerCount).Error; err != nil {
		t.Fatal(err)
	}
	if triggerCount != 4 {
		t.Fatalf("upgrade omitted security triggers: %d", triggerCount)
	}
	if !reopened.db.Migrator().HasTable("message_signing_approvals") || !reopened.db.Migrator().HasTable("message_signing_records") {
		t.Fatal("upgrade omitted message signing tables")
	}
	if !reopened.db.Migrator().HasTable("evm_transaction_effects") || !reopened.db.Migrator().HasColumn("evm_transactions", "token_id") {
		t.Fatal("upgrade omitted ERC-1155 effect storage")
	}
	var messageTriggerCount int64
	if err := reopened.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN ?", []string{"trg_message_approval_binding_immutable", "trg_message_signing_binding_immutable", "trg_message_signature_hash_write_once"}).Scan(&messageTriggerCount).Error; err != nil {
		t.Fatal(err)
	}
	if messageTriggerCount != 3 {
		t.Fatalf("upgrade omitted message signing triggers: %d", messageTriggerCount)
	}
	preserved, err := reopened.GetTransaction(context.Background(), record.TransactionID)
	if err != nil || preserved.TransactionID != record.TransactionID {
		t.Fatalf("history migration changed existing transaction: %+v %v", preserved, err)
	}
}

func TestWatchOnlyGuardMigrationRejectsExistingCorruption(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{AppDir: root, DatabasePath: filepath.Join(root, "watch-corrupt.db"), Database: config.DatabaseConfig{Type: "sqlite"}}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("11111111-1111-4111-8111-111111111111", "watch-corrupt")
	account.SignerKind = wallet.SignerKindWatchOnly
	account.SignerReference = "watch-only:v1:" + account.Address
	account.SecretType = ""
	account.DerivationScheme = ""
	account.DerivationPath = ""
	account.BIP39Language = ""
	account.Capabilities = 0
	account.State = wallet.AccountStateActive
	account.SecretEnvelope = nil
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{"trg_watch_only_custody_insert", "trg_watch_only_custody_update"} {
		if err := repository.db.Exec("DROP TRIGGER " + trigger).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.db.Delete(&schemaMigration{}, 8).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Model(&wallet.Account{}).Where("account_id = ?", account.AccountID).Update("capabilities", wallet.CapabilitySignTransaction).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := NewVaultRepository(cfg); err == nil {
		_ = reopened.Close()
		t.Fatal("watch-only guard migration accepted existing custody corruption")
	}
}

func TestVaultRepositoryAppliesERC721MigrationPreservingRecords(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{AppDir: root, DatabasePath: filepath.Join(root, "erc721-upgrade.db"), Database: config.DatabaseConfig{Type: "sqlite"}}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	record := createAuthorizedTestTransaction(t, repository, now)
	raw := []byte{1, 2, 3, 4}
	if _, err := repository.BeginFirstBroadcast(context.Background(), evm.FirstBroadcastRequest{
		TransactionID: record.TransactionID, SignedPayload: raw, StartedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Exec("DROP TRIGGER trg_evm_effect_immutable").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Exec("DROP TABLE evm_transaction_effects").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Exec("ALTER TABLE evm_transactions DROP COLUMN token_id").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Where("version >= ?", 10).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	for _, index := range []string{"ix_evm_transaction_state", "ix_evm_history_account", "ix_evm_history_sender"} {
		if !reopened.db.Migrator().HasIndex("evm_transactions", index) {
			t.Fatalf("ERC-721 upgrade omitted index %s", index)
		}
	}
	if !reopened.db.Migrator().HasTable("evm_transaction_effects") || !reopened.db.Migrator().HasColumn("evm_transactions", "token_id") {
		t.Fatal("ERC-1155 migration was not reapplied after ERC-721 rebuild")
	}
	var triggerCount int64
	if err := reopened.db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN ?", []string{"trg_evm_signed_payload_write_once", "trg_evm_history_key_immutable"}).Scan(&triggerCount).Error; err != nil {
		t.Fatal(err)
	}
	if triggerCount != 2 {
		t.Fatalf("ERC-721 upgrade omitted triggers: %d", triggerCount)
	}
	preserved, err := reopened.GetTransaction(context.Background(), record.TransactionID)
	if err != nil || preserved.TransactionID != record.TransactionID || string(preserved.SignedPayload) != string(raw) {
		t.Fatalf("ERC-721 migration changed existing transaction: %+v %v", preserved, err)
	}
	var operationCheck string
	if err := reopened.db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'evm_transactions'").Scan(&operationCheck).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(operationCheck, "erc721_safe_transfer") {
		t.Fatalf("ERC-721 upgrade omitted the new operation: %s", operationCheck)
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
	if err := repository.UpdateAccount(ctx, account); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("immutable metadata mutation returned %v", err)
	}
	loaded, err := repository.GetAccount(ctx, account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name == "Renamed" || loaded.Address == account.Address || loaded.DerivationPath == account.DerivationPath {
		t.Fatal("rejected immutable metadata update changed the account")
	}
	loaded.Name = "Renamed"
	if err := repository.UpdateAccount(ctx, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err = repository.GetAccount(ctx, account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "Renamed" {
		t.Fatal("mutable name was not updated")
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
