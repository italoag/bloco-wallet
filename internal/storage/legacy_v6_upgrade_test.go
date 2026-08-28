package storage

import (
	"context"
	"path/filepath"
	"testing"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
)

// TestVaultRepositoryUpgradesLegacyV6Schema simulates a database created by
// the original migration 6, which predated the operation/counterparty/asset
// columns on evm_transactions and the approval confirmation target. The
// upgrade path must repair the schema before rebuilding.
func TestVaultRepositoryUpgradesLegacyV6Schema(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "legacy.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("11111111-1111-4111-8111-111111111111", "legacy-v6")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	// The approval and reservation rows exist with their modern shape; only
	// the evm_transactions row will be legacy.
	if err := repository.db.Exec(`INSERT INTO evm_nonce_reservations
		(reservation_id, operation_id, account_id, sender_address, chain_id, nonce, plan_generation, state, reserved_at_ms, expires_at_ms)
		VALUES (?, ?, ?, zeroblob(20), 1, 7, 1, 'reserved', 1, 2)`,
		"31111111-1111-4111-8111-111111111111", "41111111-1111-4111-8111-111111111111", account.AccountID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Exec(`INSERT INTO evm_approvals
		(approval_id, reservation_id, account_id, sender_address, chain_id, nonce, authorization_epoch, plan_hash, transaction_digest, risk_level, confirmation_level, confirmation_target, state, created_at_ms, confirmed_at_ms, expires_at_ms)
		VALUES (?, ?, ?, zeroblob(20), 1, 7, 1, zeroblob(32), zeroblob(32), 'normal', 'standard', 1, 'pending', 1, 1, 2)`,
		"51111111-1111-4111-8111-111111111111", "31111111-1111-4111-8111-111111111111", account.AccountID).Error; err != nil {
		t.Fatal(err)
	}
	// Regress the schema to the legacy v6 shape.
	if err := repository.db.Exec(`DROP TRIGGER trg_evm_approval_binding_immutable`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Exec(`CREATE TRIGGER trg_evm_approval_binding_immutable
		BEFORE UPDATE OF reservation_id, account_id, sender_address, chain_id, nonce, authorization_epoch, plan_hash, transaction_digest, risk_level, confirmation_level, created_at_ms, confirmed_at_ms, expires_at_ms
		ON evm_approvals BEGIN SELECT RAISE(ABORT, 'immutable EVM approval binding'); END`).Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE evm_approvals DROP COLUMN confirmation_target",
		"ALTER TABLE evm_transactions DROP COLUMN operation",
		"ALTER TABLE evm_transactions DROP COLUMN counterparty_address",
		"ALTER TABLE evm_transactions DROP COLUMN asset_contract",
		"ALTER TABLE evm_transactions DROP COLUMN asset_amount",
		"ALTER TABLE evm_transactions DROP COLUMN token_id",
		"DROP INDEX ix_evm_history_account",
		"DROP INDEX ix_evm_history_sender",
		"DROP TRIGGER trg_evm_history_key_immutable",
		"DROP TRIGGER trg_evm_effect_immutable",
		"DROP TABLE evm_transaction_effects",
		"DROP TRIGGER trg_account_identity_immutable",
		"DROP TRIGGER trg_watch_only_custody_insert",
		"DROP TRIGGER trg_watch_only_custody_update",
		"DROP TRIGGER trg_message_approval_binding_immutable",
		"DROP TRIGGER trg_message_signing_binding_immutable",
		"DROP TRIGGER trg_message_signature_hash_write_once",
		"DROP TABLE message_signing_approvals",
		"DROP TABLE message_signing_records",
	} {
		if err := repository.db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Insert a legacy transaction row with the pre-v7 column set (state
	// signing, no operation/counterparty/asset columns exist yet).
	if err := repository.db.Exec(`INSERT INTO evm_transactions
		(transaction_id, approval_id, reservation_id, account_id, sender_address, chain_id, nonce, plan_hash, transaction_digest, state, confirmation_target, created_at_ms, updated_at_ms, revision)
		VALUES (?, ?, ?, ?, zeroblob(20), 1, 7, zeroblob(32), zeroblob(32), 'signing', 1, 1, 1, 1)`,
		"61111111-1111-4111-8111-111111111111", "51111111-1111-4111-8111-111111111111", "31111111-1111-4111-8111-111111111111",
		account.AccountID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Where("version >= ?", 7).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatalf("legacy v6 database failed to upgrade: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	for _, version := range []uint{7, 8, 9, 10, 11, 12} {
		var count int64
		if err := reopened.db.Model(&schemaMigration{}).Where("version = ?", version).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("migration %d was not applied after legacy repair: %v", version, err)
		}
	}
	if !reopened.db.Migrator().HasColumn("evm_transactions", "token_id") || !reopened.db.Migrator().HasColumn("evm_approvals", "confirmation_target") {
		t.Fatal("legacy repair did not restore the modern schema")
	}
	var operation string
	if err := reopened.db.Raw("SELECT operation FROM evm_transactions WHERE transaction_id = ?", "61111111-1111-4111-8111-111111111111").Scan(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if operation != "native_transfer" {
		t.Fatalf("legacy transaction row was not attributed as native_transfer: %q", operation)
	}
	var approvals int64
	if err := reopened.db.Model(&evmApprovalRow{}).Count(&approvals).Error; err != nil {
		t.Fatal(err)
	}
	_ = approvals
	var transactionCount int64
	if err := reopened.db.Table("evm_transactions").Count(&transactionCount).Error; err != nil {
		t.Fatal(err)
	}
	if transactionCount != 1 {
		t.Fatalf("legacy upgrade lost transaction rows: %d", transactionCount)
	}
}
