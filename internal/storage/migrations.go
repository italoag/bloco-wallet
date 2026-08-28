package storage

import (
	"fmt"
	"strings"
	"time"

	"blocowallet/internal/wallet"

	"gorm.io/gorm"
)

const latestSchemaVersion uint = 12

type schemaMigration struct {
	Version   uint      `gorm:"primaryKey"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string {
	return "schema_migrations"
}

func configureSQLite(database *gorm.DB, disk bool) error {
	sqlDatabase, err := database.DB()
	if err != nil {
		return err
	}
	sqlDatabase.SetMaxOpenConns(1)
	sqlDatabase.SetMaxIdleConns(1)
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = FULL",
	}
	if disk {
		pragmas = append(pragmas, "PRAGMA journal_mode = WAL")
	}
	for _, pragma := range pragmas {
		if err := database.Exec(pragma).Error; err != nil {
			return fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	return nil
}

func runMigrations(database *gorm.DB, includeLegacy bool) error {
	if err := database.AutoMigrate(&schemaMigration{}); err != nil {
		return fmt.Errorf("create schema migration table: %w", err)
	}
	return database.Transaction(func(transaction *gorm.DB) error {
		var appliedRecords []schemaMigration
		if err := transaction.Find(&appliedRecords).Error; err != nil {
			return err
		}
		applied := make(map[uint]struct{}, len(appliedRecords))
		for _, record := range appliedRecords {
			if record.Version > latestSchemaVersion {
				return fmt.Errorf("database schema version %d is newer than supported version %d", record.Version, latestSchemaVersion)
			}
			applied[record.Version] = struct{}{}
		}
		migrations := []struct {
			version uint
			apply   func(*gorm.DB) error
		}{
			{version: 1, apply: func(tx *gorm.DB) error {
				if !includeLegacy {
					return nil
				}
				return tx.AutoMigrate(&wallet.Wallet{})
			}},
			{version: 2, apply: func(tx *gorm.DB) error { return tx.AutoMigrate(&wallet.Account{}) }},
			{version: 3, apply: func(tx *gorm.DB) error {
				if tx.Migrator().HasColumn(&wallet.Account{}, "HasBIP39Passphrase") {
					return nil
				}
				return tx.Migrator().AddColumn(&wallet.Account{}, "HasBIP39Passphrase")
			}},
			{version: 4, apply: func(tx *gorm.DB) error {
				if !tx.Migrator().HasColumn(&wallet.Account{}, "RelatedAccountID") {
					if err := tx.Migrator().AddColumn(&wallet.Account{}, "RelatedAccountID"); err != nil {
						return err
					}
				}
				return tx.AutoMigrate(&wallet.VaultMetadata{})
			}},
			{version: 5, apply: migrateEVMNonceReservations},
			{version: 6, apply: migrateEVMApprovalsAndTransactions},
			{version: 7, apply: migrateEVMHistoryIndexes},
			{version: 8, apply: migrateWatchOnlyCustodyGuards},
			{version: 9, apply: migrateMessageSigningApprovals},
			{version: 10, apply: migrateERC721Operation},
			{version: 11, apply: migrateERC1155Effects},
			{version: 12, apply: migrateContractCallOperation},
		}
		for _, migration := range migrations {
			if _, exists := applied[migration.version]; exists {
				continue
			}
			if err := migration.apply(transaction); err != nil {
				return fmt.Errorf("apply schema migration %d: %w", migration.version, err)
			}
			if err := transaction.Create(&schemaMigration{Version: migration.version, AppliedAt: time.Now().UTC()}).Error; err != nil {
				return fmt.Errorf("record schema migration %d: %w", migration.version, err)
			}
		}
		return nil
	})
}

func migrateEVMApprovalsAndTransactions(transaction *gorm.DB) error {
	statements := []string{
		`CREATE TABLE evm_approvals (
			approval_id TEXT PRIMARY KEY NOT NULL CHECK(length(approval_id) = 36),
			reservation_id TEXT NOT NULL UNIQUE,
			account_id TEXT NOT NULL,
			sender_address BLOB NOT NULL CHECK(typeof(sender_address) = 'blob' AND length(sender_address) = 20),
			chain_id INTEGER NOT NULL CHECK(chain_id > 0),
			nonce INTEGER NOT NULL CHECK(nonce >= 0),
			authorization_epoch INTEGER NOT NULL CHECK(authorization_epoch > 0),
			plan_hash BLOB NOT NULL CHECK(typeof(plan_hash) = 'blob' AND length(plan_hash) = 32),
			transaction_digest BLOB NOT NULL CHECK(typeof(transaction_digest) = 'blob' AND length(transaction_digest) = 32),
			risk_level TEXT NOT NULL CHECK(risk_level IN ('normal', 'warning', 'critical')),
			confirmation_level TEXT NOT NULL CHECK(confirmation_level IN ('standard', 'reinforced')),
			confirmation_target INTEGER NOT NULL CHECK(confirmation_target BETWEEN 1 AND 10000),
			state TEXT NOT NULL CHECK(state IN ('pending', 'consumed', 'invalidated')),
			created_at_ms INTEGER NOT NULL,
			confirmed_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms > confirmed_at_ms),
			consumed_at_ms INTEGER,
			invalidated_at_ms INTEGER,
			invalidation_reason TEXT,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(reservation_id) REFERENCES evm_nonce_reservations(reservation_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			CHECK(risk_level <> 'critical' OR confirmation_level = 'reinforced'),
			CHECK((state = 'pending' AND consumed_at_ms IS NULL AND invalidated_at_ms IS NULL) OR
			      (state = 'consumed' AND consumed_at_ms IS NOT NULL AND invalidated_at_ms IS NULL) OR
			      (state = 'invalidated' AND consumed_at_ms IS NULL AND invalidated_at_ms IS NOT NULL))
		) STRICT`,
		`CREATE INDEX ix_evm_approval_expiry ON evm_approvals(state, expires_at_ms)`,
		`CREATE TABLE evm_transactions (
			transaction_id TEXT PRIMARY KEY NOT NULL CHECK(length(transaction_id) = 36),
			approval_id TEXT NOT NULL UNIQUE,
			reservation_id TEXT NOT NULL UNIQUE,
			account_id TEXT NOT NULL,
			sender_address BLOB NOT NULL CHECK(typeof(sender_address) = 'blob' AND length(sender_address) = 20),
			chain_id INTEGER NOT NULL CHECK(chain_id > 0),
			nonce INTEGER NOT NULL CHECK(nonce >= 0),
			operation TEXT NOT NULL CHECK(operation IN ('native_transfer', 'erc20_transfer', 'erc20_approve')),
			counterparty_address BLOB NOT NULL CHECK(typeof(counterparty_address) = 'blob' AND length(counterparty_address) = 20),
			asset_contract BLOB NOT NULL CHECK(typeof(asset_contract) = 'blob' AND length(asset_contract) = 20),
			asset_amount BLOB NOT NULL CHECK(typeof(asset_amount) = 'blob' AND length(asset_amount) = 32),
			plan_hash BLOB NOT NULL CHECK(typeof(plan_hash) = 'blob' AND length(plan_hash) = 32),
			transaction_digest BLOB NOT NULL CHECK(typeof(transaction_digest) = 'blob' AND length(transaction_digest) = 32),
			state TEXT NOT NULL CHECK(state IN ('signing', 'signing_failed', 'broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified')),
			signed_payload BLOB,
			transaction_hash BLOB,
			broadcast_attempts INTEGER NOT NULL DEFAULT 0 CHECK(broadcast_attempts >= 0),
			first_broadcast_at_ms INTEGER,
			last_broadcast_at_ms INTEGER,
			last_result_code TEXT CHECK(last_result_code IS NULL OR length(last_result_code) <= 64),
			receipt_status INTEGER CHECK(receipt_status IS NULL OR receipt_status IN (0, 1)),
			receipt_block_number INTEGER CHECK(receipt_block_number IS NULL OR receipt_block_number >= 0),
			receipt_block_hash BLOB CHECK(receipt_block_hash IS NULL OR (typeof(receipt_block_hash) = 'blob' AND length(receipt_block_hash) = 32)),
			receipt_tx_index INTEGER CHECK(receipt_tx_index IS NULL OR receipt_tx_index >= 0),
			receipt_gas_used INTEGER CHECK(receipt_gas_used IS NULL OR receipt_gas_used > 0),
			effective_gas_price BLOB CHECK(effective_gas_price IS NULL OR (typeof(effective_gas_price) = 'blob' AND length(effective_gas_price) = 32)),
			confirmations INTEGER NOT NULL DEFAULT 0 CHECK(confirmations >= 0),
			confirmation_target INTEGER NOT NULL DEFAULT 1 CHECK(confirmation_target >= 1),
			reorg_count INTEGER NOT NULL DEFAULT 0 CHECK(reorg_count >= 0),
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(approval_id) REFERENCES evm_approvals(approval_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(reservation_id) REFERENCES evm_nonce_reservations(reservation_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			CHECK((state IN ('signing', 'signing_failed') AND signed_payload IS NULL AND transaction_hash IS NULL AND broadcast_attempts = 0) OR
			      (state IN ('broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified') AND typeof(signed_payload) = 'blob' AND length(signed_payload) BETWEEN 1 AND 131072 AND typeof(transaction_hash) = 'blob' AND length(transaction_hash) = 32 AND broadcast_attempts >= 1 AND first_broadcast_at_ms IS NOT NULL AND last_broadcast_at_ms IS NOT NULL)),
			CHECK(state <> 'confirmed' OR receipt_status = 1),
			CHECK(state <> 'reverted' OR receipt_status = 0)
		) STRICT`,
		`CREATE INDEX ix_evm_transaction_state ON evm_transactions(state, updated_at_ms)`,
		`CREATE TRIGGER trg_evm_approval_binding_immutable
			BEFORE UPDATE OF reservation_id, account_id, sender_address, chain_id, nonce, authorization_epoch, plan_hash, transaction_digest, risk_level, confirmation_level, confirmation_target, created_at_ms, confirmed_at_ms, expires_at_ms
			ON evm_approvals BEGIN SELECT RAISE(ABORT, 'immutable EVM approval binding'); END`,
		`CREATE TRIGGER trg_evm_signed_payload_write_once
			BEFORE UPDATE OF signed_payload, transaction_hash ON evm_transactions
			WHEN OLD.signed_payload IS NOT NULL
			BEGIN SELECT RAISE(ABORT, 'signed EVM payload is immutable'); END`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateEVMHistoryIndexes(transaction *gorm.DB) error {
	statements := []string{
		`CREATE INDEX ix_evm_history_account ON evm_transactions(account_id, created_at_ms DESC, transaction_id DESC)`,
		`CREATE INDEX ix_evm_history_sender ON evm_transactions(sender_address, created_at_ms DESC, transaction_id DESC)`,
		`CREATE TRIGGER trg_evm_history_key_immutable
			BEFORE UPDATE OF transaction_id, created_at_ms ON evm_transactions
			BEGIN SELECT RAISE(ABORT, 'immutable EVM transaction history key'); END`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateWatchOnlyCustodyGuards(transaction *gorm.DB) error {
	invalidCondition := `signer_kind = 'watch_only' AND (
		capabilities <> 0 OR COALESCE(length(secret_envelope), 0) <> 0 OR COALESCE(secret_type, '') <> '' OR
		COALESCE(derivation_scheme, '') <> '' OR COALESCE(derivation_path, '') <> '' OR account_index <> 0 OR
		change_index <> 0 OR address_index <> 0 OR COALESCE(b_ip39_language, '') <> '' OR has_b_ip39_passphrase <> 0)`
	var invalidAccounts int64
	if err := transaction.Table("accounts").Where(invalidCondition).Count(&invalidAccounts).Error; err != nil {
		return err
	}
	if invalidAccounts != 0 {
		return fmt.Errorf("database contains invalid watch-only custody material")
	}
	statements := []string{
		`CREATE TRIGGER trg_account_identity_immutable
			BEFORE UPDATE OF address, signer_kind, signer_reference, secret_type, derivation_scheme, derivation_path, account_index, change_index, address_index, b_ip39_language, has_b_ip39_passphrase, source_identity, related_account_id
			ON accounts BEGIN SELECT RAISE(ABORT, 'immutable account identity metadata'); END`,
		`CREATE TRIGGER trg_watch_only_custody_insert
			BEFORE INSERT ON accounts
			WHEN NEW.signer_kind = 'watch_only' AND (
				NEW.capabilities <> 0 OR COALESCE(length(NEW.secret_envelope), 0) <> 0 OR COALESCE(NEW.secret_type, '') <> '' OR
				COALESCE(NEW.derivation_scheme, '') <> '' OR COALESCE(NEW.derivation_path, '') <> '' OR NEW.account_index <> 0 OR
				NEW.change_index <> 0 OR NEW.address_index <> 0 OR COALESCE(NEW.b_ip39_language, '') <> '' OR NEW.has_b_ip39_passphrase <> 0)
			BEGIN SELECT RAISE(ABORT, 'watch-only account cannot contain custody material'); END`,
		`CREATE TRIGGER trg_watch_only_custody_update
			BEFORE UPDATE ON accounts
			WHEN NEW.signer_kind = 'watch_only' AND (
				NEW.capabilities <> 0 OR COALESCE(length(NEW.secret_envelope), 0) <> 0 OR COALESCE(NEW.secret_type, '') <> '' OR
				COALESCE(NEW.derivation_scheme, '') <> '' OR COALESCE(NEW.derivation_path, '') <> '' OR NEW.account_index <> 0 OR
				NEW.change_index <> 0 OR NEW.address_index <> 0 OR COALESCE(NEW.b_ip39_language, '') <> '' OR NEW.has_b_ip39_passphrase <> 0)
			BEGIN SELECT RAISE(ABORT, 'watch-only account cannot contain custody material'); END`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateMessageSigningApprovals(transaction *gorm.DB) error {
	statements := []string{
		`CREATE TABLE message_signing_approvals (
			approval_id TEXT PRIMARY KEY NOT NULL CHECK(length(approval_id) = 36),
			account_id TEXT NOT NULL,
			signer_address BLOB NOT NULL CHECK(typeof(signer_address) = 'blob' AND length(signer_address) = 20),
			scheme TEXT NOT NULL CHECK(scheme IN ('eip191_personal', 'eip712')),
			chain_id INTEGER NOT NULL CHECK(chain_id >= 0),
			digest BLOB NOT NULL CHECK(typeof(digest) = 'blob' AND length(digest) = 32),
			intent_hash BLOB NOT NULL CHECK(typeof(intent_hash) = 'blob' AND length(intent_hash) = 32),
			payload_size INTEGER NOT NULL CHECK(payload_size BETWEEN 0 AND 65536),
			authorization_epoch INTEGER NOT NULL CHECK(authorization_epoch > 0),
			confirmation_level TEXT NOT NULL CHECK(confirmation_level IN ('standard', 'reinforced')),
			state TEXT NOT NULL CHECK(state IN ('pending', 'consumed', 'invalidated')),
			created_at_ms INTEGER NOT NULL,
			confirmed_at_ms INTEGER NOT NULL,
			expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms > confirmed_at_ms),
			consumed_at_ms INTEGER,
			invalidated_at_ms INTEGER,
			invalidation_reason TEXT,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			CHECK((scheme = 'eip191_personal' AND chain_id = 0) OR (scheme = 'eip712' AND chain_id > 0)),
			CHECK((state = 'pending' AND consumed_at_ms IS NULL AND invalidated_at_ms IS NULL) OR
			      (state = 'consumed' AND consumed_at_ms IS NOT NULL AND invalidated_at_ms IS NULL) OR
			      (state = 'invalidated' AND consumed_at_ms IS NULL AND invalidated_at_ms IS NOT NULL))
		) STRICT`,
		`CREATE INDEX ix_message_approval_expiry ON message_signing_approvals(state, expires_at_ms)`,
		`CREATE TABLE message_signing_records (
			signing_id TEXT PRIMARY KEY NOT NULL CHECK(length(signing_id) = 36),
			approval_id TEXT NOT NULL UNIQUE,
			account_id TEXT NOT NULL,
			signer_address BLOB NOT NULL CHECK(typeof(signer_address) = 'blob' AND length(signer_address) = 20),
			scheme TEXT NOT NULL CHECK(scheme IN ('eip191_personal', 'eip712')),
			chain_id INTEGER NOT NULL CHECK(chain_id >= 0),
			digest BLOB NOT NULL CHECK(typeof(digest) = 'blob' AND length(digest) = 32),
			intent_hash BLOB NOT NULL CHECK(typeof(intent_hash) = 'blob' AND length(intent_hash) = 32),
			state TEXT NOT NULL CHECK(state IN ('signing', 'signed', 'signing_failed')),
			signature_hash BLOB,
			last_result_code TEXT CHECK(last_result_code IS NULL OR length(last_result_code) <= 64),
			created_at_ms INTEGER NOT NULL,
			completed_at_ms INTEGER,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(approval_id) REFERENCES message_signing_approvals(approval_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			CHECK((scheme = 'eip191_personal' AND chain_id = 0) OR (scheme = 'eip712' AND chain_id > 0)),
			CHECK((state = 'signing' AND signature_hash IS NULL AND completed_at_ms IS NULL) OR
			      (state = 'signed' AND typeof(signature_hash) = 'blob' AND length(signature_hash) = 32 AND completed_at_ms IS NOT NULL) OR
			      (state = 'signing_failed' AND signature_hash IS NULL AND completed_at_ms IS NOT NULL AND last_result_code IS NOT NULL))
		) STRICT`,
		`CREATE INDEX ix_message_signing_account ON message_signing_records(account_id, created_at_ms DESC, signing_id DESC)`,
		`CREATE TRIGGER trg_message_approval_binding_immutable
			BEFORE UPDATE OF approval_id, account_id, signer_address, scheme, chain_id, digest, intent_hash, payload_size, authorization_epoch, confirmation_level, created_at_ms, confirmed_at_ms, expires_at_ms
			ON message_signing_approvals BEGIN SELECT RAISE(ABORT, 'immutable message approval binding'); END`,
		`CREATE TRIGGER trg_message_signing_binding_immutable
			BEFORE UPDATE OF signing_id, approval_id, account_id, signer_address, scheme, chain_id, digest, intent_hash, created_at_ms
			ON message_signing_records BEGIN SELECT RAISE(ABORT, 'immutable message signing binding'); END`,
		`CREATE TRIGGER trg_message_signature_hash_write_once
			BEFORE UPDATE OF signature_hash ON message_signing_records
			WHEN OLD.signature_hash IS NOT NULL
			BEGIN SELECT RAISE(ABORT, 'message signature hash is immutable'); END`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

// repairLegacyEVMSchema brings databases created by the original migration 6
// (which predated the operation/counterparty/asset columns and the approval
// confirmation target) up to the schema assumed by migrations 7+.
func repairLegacyEVMSchema(transaction *gorm.DB) error {
	var transactionColumns []tableColumn
	if err := transaction.Raw("PRAGMA table_info(evm_transactions)").Scan(&transactionColumns).Error; err != nil {
		return err
	}
	hasColumn := func(columns []tableColumn, name string) bool {
		for _, column := range columns {
			if column.Name == name {
				return true
			}
		}
		return false
	}
	if !hasColumn(transactionColumns, "operation") {
		zeroAddress := "X'" + strings.Repeat("00", 20) + "'"
		zeroAmount := "X'" + strings.Repeat("00", 32) + "'"
		statements := []string{
			`ALTER TABLE evm_transactions ADD COLUMN operation TEXT NOT NULL DEFAULT 'native_transfer' CHECK(operation IN ('native_transfer', 'erc20_transfer', 'erc20_approve'))`,
			`ALTER TABLE evm_transactions ADD COLUMN counterparty_address BLOB NOT NULL DEFAULT ` + zeroAddress + ` CHECK(typeof(counterparty_address) = 'blob' AND length(counterparty_address) = 20)`,
			`ALTER TABLE evm_transactions ADD COLUMN asset_contract BLOB NOT NULL DEFAULT ` + zeroAddress + ` CHECK(typeof(asset_contract) = 'blob' AND length(asset_contract) = 20)`,
			`ALTER TABLE evm_transactions ADD COLUMN asset_amount BLOB NOT NULL DEFAULT ` + zeroAmount + ` CHECK(typeof(asset_amount) = 'blob' AND length(asset_amount) = 32)`,
		}
		for _, statement := range statements {
			if err := transaction.Exec(statement).Error; err != nil {
				return err
			}
		}
	}
	var approvalColumns []tableColumn
	if err := transaction.Raw("PRAGMA table_info(evm_approvals)").Scan(&approvalColumns).Error; err != nil {
		return err
	}
	if !hasColumn(approvalColumns, "confirmation_target") {
		if err := transaction.Exec(`ALTER TABLE evm_approvals ADD COLUMN confirmation_target INTEGER NOT NULL DEFAULT 1 CHECK(confirmation_target BETWEEN 1 AND 10000)`).Error; err != nil {
			return err
		}
		if err := transaction.Exec(`DROP TRIGGER trg_evm_approval_binding_immutable`).Error; err != nil {
			return err
		}
		if err := transaction.Exec(`CREATE TRIGGER trg_evm_approval_binding_immutable
			BEFORE UPDATE OF reservation_id, account_id, sender_address, chain_id, nonce, authorization_epoch, plan_hash, transaction_digest, risk_level, confirmation_level, confirmation_target, created_at_ms, confirmed_at_ms, expires_at_ms
			ON evm_approvals BEGIN SELECT RAISE(ABORT, 'immutable EVM approval binding'); END`).Error; err != nil {
			return err
		}
	}
	return nil
}

type tableColumn struct {
	Name string `gorm:"column:name"`
}

func migrateERC721Operation(transaction *gorm.DB) error {
	if err := repairLegacyEVMSchema(transaction); err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE evm_transactions_v10 (
			transaction_id TEXT PRIMARY KEY NOT NULL CHECK(length(transaction_id) = 36),
			approval_id TEXT NOT NULL UNIQUE,
			reservation_id TEXT NOT NULL UNIQUE,
			account_id TEXT NOT NULL,
			sender_address BLOB NOT NULL CHECK(typeof(sender_address) = 'blob' AND length(sender_address) = 20),
			chain_id INTEGER NOT NULL CHECK(chain_id > 0),
			nonce INTEGER NOT NULL CHECK(nonce >= 0),
			operation TEXT NOT NULL CHECK(operation IN ('native_transfer', 'erc20_transfer', 'erc20_approve', 'erc721_safe_transfer')),
			counterparty_address BLOB NOT NULL CHECK(typeof(counterparty_address) = 'blob' AND length(counterparty_address) = 20),
			asset_contract BLOB NOT NULL CHECK(typeof(asset_contract) = 'blob' AND length(asset_contract) = 20),
			asset_amount BLOB NOT NULL CHECK(typeof(asset_amount) = 'blob' AND length(asset_amount) = 32),
			plan_hash BLOB NOT NULL CHECK(typeof(plan_hash) = 'blob' AND length(plan_hash) = 32),
			transaction_digest BLOB NOT NULL CHECK(typeof(transaction_digest) = 'blob' AND length(transaction_digest) = 32),
			state TEXT NOT NULL CHECK(state IN ('signing', 'signing_failed', 'broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified')),
			signed_payload BLOB,
			transaction_hash BLOB,
			broadcast_attempts INTEGER NOT NULL DEFAULT 0 CHECK(broadcast_attempts >= 0),
			first_broadcast_at_ms INTEGER,
			last_broadcast_at_ms INTEGER,
			last_result_code TEXT CHECK(last_result_code IS NULL OR length(last_result_code) <= 64),
			receipt_status INTEGER CHECK(receipt_status IS NULL OR receipt_status IN (0, 1)),
			receipt_block_number INTEGER CHECK(receipt_block_number IS NULL OR receipt_block_number >= 0),
			receipt_block_hash BLOB CHECK(receipt_block_hash IS NULL OR (typeof(receipt_block_hash) = 'blob' AND length(receipt_block_hash) = 32)),
			receipt_tx_index INTEGER CHECK(receipt_tx_index IS NULL OR receipt_tx_index >= 0),
			receipt_gas_used INTEGER CHECK(receipt_gas_used IS NULL OR receipt_gas_used > 0),
			effective_gas_price BLOB CHECK(effective_gas_price IS NULL OR (typeof(effective_gas_price) = 'blob' AND length(effective_gas_price) = 32)),
			confirmations INTEGER NOT NULL DEFAULT 0 CHECK(confirmations >= 0),
			confirmation_target INTEGER NOT NULL DEFAULT 1 CHECK(confirmation_target >= 1),
			reorg_count INTEGER NOT NULL DEFAULT 0 CHECK(reorg_count >= 0),
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(approval_id) REFERENCES evm_approvals(approval_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(reservation_id) REFERENCES evm_nonce_reservations(reservation_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			CHECK((state IN ('signing', 'signing_failed') AND signed_payload IS NULL AND transaction_hash IS NULL AND broadcast_attempts = 0) OR
			      (state IN ('broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified') AND typeof(signed_payload) = 'blob' AND length(signed_payload) BETWEEN 1 AND 131072 AND typeof(transaction_hash) = 'blob' AND length(transaction_hash) = 32 AND broadcast_attempts >= 1 AND first_broadcast_at_ms IS NOT NULL AND last_broadcast_at_ms IS NOT NULL)),
			CHECK(state <> 'confirmed' OR receipt_status = 1),
			CHECK(state <> 'reverted' OR receipt_status = 0)
		) STRICT`,
		`INSERT INTO evm_transactions_v10 (transaction_id, approval_id, reservation_id, account_id, sender_address, chain_id, nonce, operation, counterparty_address, asset_contract, asset_amount, plan_hash, transaction_digest, state, signed_payload, transaction_hash, broadcast_attempts, first_broadcast_at_ms, last_broadcast_at_ms, last_result_code, receipt_status, receipt_block_number, receipt_block_hash, receipt_tx_index, receipt_gas_used, effective_gas_price, confirmations, confirmation_target, reorg_count, created_at_ms, updated_at_ms, revision)
			SELECT transaction_id, approval_id, reservation_id, account_id, sender_address, chain_id, nonce, operation, counterparty_address, asset_contract, asset_amount, plan_hash, transaction_digest, state, signed_payload, transaction_hash, broadcast_attempts, first_broadcast_at_ms, last_broadcast_at_ms, last_result_code, receipt_status, receipt_block_number, receipt_block_hash, receipt_tx_index, receipt_gas_used, effective_gas_price, confirmations, confirmation_target, reorg_count, created_at_ms, updated_at_ms, revision FROM evm_transactions`,
		`DROP TABLE evm_transactions`,
		`ALTER TABLE evm_transactions_v10 RENAME TO evm_transactions`,
		`CREATE INDEX ix_evm_transaction_state ON evm_transactions(state, updated_at_ms)`,
		`CREATE INDEX ix_evm_history_account ON evm_transactions(account_id, created_at_ms DESC, transaction_id DESC)`,
		`CREATE INDEX ix_evm_history_sender ON evm_transactions(sender_address, created_at_ms DESC, transaction_id DESC)`,
		`CREATE TRIGGER trg_evm_signed_payload_write_once
			BEFORE UPDATE OF signed_payload, transaction_hash ON evm_transactions
			WHEN OLD.signed_payload IS NOT NULL
			BEGIN SELECT RAISE(ABORT, 'signed EVM payload is immutable'); END`,
		`CREATE TRIGGER trg_evm_history_key_immutable
			BEFORE UPDATE OF transaction_id, created_at_ms ON evm_transactions
			BEGIN SELECT RAISE(ABORT, 'immutable EVM transaction history key'); END`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateERC1155Effects(transaction *gorm.DB) error {
	statements := []string{
		`CREATE TABLE evm_transactions_v11 (
			transaction_id TEXT PRIMARY KEY NOT NULL CHECK(length(transaction_id) = 36),
			approval_id TEXT NOT NULL UNIQUE,
			reservation_id TEXT NOT NULL UNIQUE,
			account_id TEXT NOT NULL,
			sender_address BLOB NOT NULL CHECK(typeof(sender_address) = 'blob' AND length(sender_address) = 20),
			chain_id INTEGER NOT NULL CHECK(chain_id > 0),
			nonce INTEGER NOT NULL CHECK(nonce >= 0),
			operation TEXT NOT NULL CHECK(operation IN ('native_transfer', 'erc20_transfer', 'erc20_approve', 'erc721_safe_transfer', 'erc1155_safe_transfer', 'erc1155_batch_transfer')),
			counterparty_address BLOB NOT NULL CHECK(typeof(counterparty_address) = 'blob' AND length(counterparty_address) = 20),
			asset_contract BLOB NOT NULL CHECK(typeof(asset_contract) = 'blob' AND length(asset_contract) = 20),
			asset_amount BLOB NOT NULL CHECK(typeof(asset_amount) = 'blob' AND length(asset_amount) = 32),
			token_id BLOB CHECK(token_id IS NULL OR (typeof(token_id) = 'blob' AND length(token_id) = 32)),
			plan_hash BLOB NOT NULL CHECK(typeof(plan_hash) = 'blob' AND length(plan_hash) = 32),
			transaction_digest BLOB NOT NULL CHECK(typeof(transaction_digest) = 'blob' AND length(transaction_digest) = 32),
			state TEXT NOT NULL CHECK(state IN ('signing', 'signing_failed', 'broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified')),
			signed_payload BLOB,
			transaction_hash BLOB,
			broadcast_attempts INTEGER NOT NULL DEFAULT 0 CHECK(broadcast_attempts >= 0),
			first_broadcast_at_ms INTEGER,
			last_broadcast_at_ms INTEGER,
			last_result_code TEXT CHECK(last_result_code IS NULL OR length(last_result_code) <= 64),
			receipt_status INTEGER CHECK(receipt_status IS NULL OR receipt_status IN (0, 1)),
			receipt_block_number INTEGER CHECK(receipt_block_number IS NULL OR receipt_block_number >= 0),
			receipt_block_hash BLOB CHECK(receipt_block_hash IS NULL OR (typeof(receipt_block_hash) = 'blob' AND length(receipt_block_hash) = 32)),
			receipt_tx_index INTEGER CHECK(receipt_tx_index IS NULL OR receipt_tx_index >= 0),
			receipt_gas_used INTEGER CHECK(receipt_gas_used IS NULL OR receipt_gas_used > 0),
			effective_gas_price BLOB CHECK(effective_gas_price IS NULL OR (typeof(effective_gas_price) = 'blob' AND length(effective_gas_price) = 32)),
			confirmations INTEGER NOT NULL DEFAULT 0 CHECK(confirmations >= 0),
			confirmation_target INTEGER NOT NULL DEFAULT 1 CHECK(confirmation_target >= 1),
			reorg_count INTEGER NOT NULL DEFAULT 0 CHECK(reorg_count >= 0),
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(approval_id) REFERENCES evm_approvals(approval_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(reservation_id) REFERENCES evm_nonce_reservations(reservation_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			CHECK((state IN ('signing', 'signing_failed') AND signed_payload IS NULL AND transaction_hash IS NULL AND broadcast_attempts = 0) OR
			      (state IN ('broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified') AND typeof(signed_payload) = 'blob' AND length(signed_payload) BETWEEN 1 AND 131072 AND typeof(transaction_hash) = 'blob' AND length(transaction_hash) = 32 AND broadcast_attempts >= 1 AND first_broadcast_at_ms IS NOT NULL AND last_broadcast_at_ms IS NOT NULL)),
			CHECK(state <> 'confirmed' OR receipt_status = 1),
			CHECK(state <> 'reverted' OR receipt_status = 0)
		) STRICT`,
		`INSERT INTO evm_transactions_v11 (transaction_id, approval_id, reservation_id, account_id, sender_address, chain_id, nonce, operation, counterparty_address, asset_contract, asset_amount, plan_hash, transaction_digest, state, signed_payload, transaction_hash, broadcast_attempts, first_broadcast_at_ms, last_broadcast_at_ms, last_result_code, receipt_status, receipt_block_number, receipt_block_hash, receipt_tx_index, receipt_gas_used, effective_gas_price, confirmations, confirmation_target, reorg_count, created_at_ms, updated_at_ms, revision)
			SELECT transaction_id, approval_id, reservation_id, account_id, sender_address, chain_id, nonce, operation, counterparty_address, asset_contract, asset_amount, plan_hash, transaction_digest, state, signed_payload, transaction_hash, broadcast_attempts, first_broadcast_at_ms, last_broadcast_at_ms, last_result_code, receipt_status, receipt_block_number, receipt_block_hash, receipt_tx_index, receipt_gas_used, effective_gas_price, confirmations, confirmation_target, reorg_count, created_at_ms, updated_at_ms, revision FROM evm_transactions`,
		`DROP TABLE evm_transactions`,
		`ALTER TABLE evm_transactions_v11 RENAME TO evm_transactions`,
		`CREATE INDEX ix_evm_transaction_state ON evm_transactions(state, updated_at_ms)`,
		`CREATE INDEX ix_evm_history_account ON evm_transactions(account_id, created_at_ms DESC, transaction_id DESC)`,
		`CREATE INDEX ix_evm_history_sender ON evm_transactions(sender_address, created_at_ms DESC, transaction_id DESC)`,
		`CREATE TRIGGER trg_evm_signed_payload_write_once
			BEFORE UPDATE OF signed_payload, transaction_hash ON evm_transactions
			WHEN OLD.signed_payload IS NOT NULL
			BEGIN SELECT RAISE(ABORT, 'signed EVM payload is immutable'); END`,
		`CREATE TRIGGER trg_evm_history_key_immutable
			BEFORE UPDATE OF transaction_id, created_at_ms ON evm_transactions
			BEGIN SELECT RAISE(ABORT, 'immutable EVM transaction history key'); END`,
		`CREATE TABLE evm_transaction_effects (
			transaction_id TEXT NOT NULL,
			effect_index INTEGER NOT NULL CHECK(effect_index >= 0),
			token_id BLOB NOT NULL CHECK(typeof(token_id) = 'blob' AND length(token_id) = 32),
			amount BLOB NOT NULL CHECK(typeof(amount) = 'blob' AND length(amount) = 32),
			PRIMARY KEY (transaction_id, effect_index),
			FOREIGN KEY(transaction_id) REFERENCES evm_transactions(transaction_id) ON UPDATE RESTRICT ON DELETE RESTRICT
		) STRICT`,
		`CREATE TRIGGER trg_evm_effect_immutable
			BEFORE UPDATE OF transaction_id, effect_index, token_id, amount ON evm_transaction_effects
			BEGIN SELECT RAISE(ABORT, 'immutable EVM transaction effect'); END`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateContractCallOperation(transaction *gorm.DB) error {
	statements := []string{
		`CREATE TABLE evm_transactions_v12 (
			transaction_id TEXT PRIMARY KEY NOT NULL CHECK(length(transaction_id) = 36),
			approval_id TEXT NOT NULL UNIQUE,
			reservation_id TEXT NOT NULL UNIQUE,
			account_id TEXT NOT NULL,
			sender_address BLOB NOT NULL CHECK(typeof(sender_address) = 'blob' AND length(sender_address) = 20),
			chain_id INTEGER NOT NULL CHECK(chain_id > 0),
			nonce INTEGER NOT NULL CHECK(nonce >= 0),
			operation TEXT NOT NULL CHECK(operation IN ('native_transfer', 'erc20_transfer', 'erc20_approve', 'erc721_safe_transfer', 'erc1155_safe_transfer', 'erc1155_batch_transfer', 'contract_call')),
			counterparty_address BLOB NOT NULL CHECK(typeof(counterparty_address) = 'blob' AND length(counterparty_address) = 20),
			asset_contract BLOB NOT NULL CHECK(typeof(asset_contract) = 'blob' AND length(asset_contract) = 20),
			asset_amount BLOB NOT NULL CHECK(typeof(asset_amount) = 'blob' AND length(asset_amount) = 32),
			token_id BLOB CHECK(token_id IS NULL OR (typeof(token_id) = 'blob' AND length(token_id) = 32)),
			plan_hash BLOB NOT NULL CHECK(typeof(plan_hash) = 'blob' AND length(plan_hash) = 32),
			transaction_digest BLOB NOT NULL CHECK(typeof(transaction_digest) = 'blob' AND length(transaction_digest) = 32),
			state TEXT NOT NULL CHECK(state IN ('signing', 'signing_failed', 'broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified')),
			signed_payload BLOB,
			transaction_hash BLOB,
			broadcast_attempts INTEGER NOT NULL DEFAULT 0 CHECK(broadcast_attempts >= 0),
			first_broadcast_at_ms INTEGER,
			last_broadcast_at_ms INTEGER,
			last_result_code TEXT CHECK(last_result_code IS NULL OR length(last_result_code) <= 64),
			receipt_status INTEGER CHECK(receipt_status IS NULL OR receipt_status IN (0, 1)),
			receipt_block_number INTEGER CHECK(receipt_block_number IS NULL OR receipt_block_number >= 0),
			receipt_block_hash BLOB CHECK(receipt_block_hash IS NULL OR (typeof(receipt_block_hash) = 'blob' AND length(receipt_block_hash) = 32)),
			receipt_tx_index INTEGER CHECK(receipt_tx_index IS NULL OR receipt_tx_index >= 0),
			receipt_gas_used INTEGER CHECK(receipt_gas_used IS NULL OR receipt_gas_used > 0),
			effective_gas_price BLOB CHECK(effective_gas_price IS NULL OR (typeof(effective_gas_price) = 'blob' AND length(effective_gas_price) = 32)),
			confirmations INTEGER NOT NULL DEFAULT 0 CHECK(confirmations >= 0),
			confirmation_target INTEGER NOT NULL DEFAULT 1 CHECK(confirmation_target >= 1),
			reorg_count INTEGER NOT NULL DEFAULT 0 CHECK(reorg_count >= 0),
			created_at_ms INTEGER NOT NULL,
			updated_at_ms INTEGER NOT NULL,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(approval_id) REFERENCES evm_approvals(approval_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(reservation_id) REFERENCES evm_nonce_reservations(reservation_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			CHECK((state IN ('signing', 'signing_failed') AND signed_payload IS NULL AND transaction_hash IS NULL AND broadcast_attempts = 0) OR
			      (state IN ('broadcasting', 'broadcast_failed', 'submitted', 'confirming', 'confirmed', 'reverted', 'reorged', 'effect_unverified') AND typeof(signed_payload) = 'blob' AND length(signed_payload) BETWEEN 1 AND 131072 AND typeof(transaction_hash) = 'blob' AND length(transaction_hash) = 32 AND broadcast_attempts >= 1 AND first_broadcast_at_ms IS NOT NULL AND last_broadcast_at_ms IS NOT NULL)),
			CHECK(state <> 'confirmed' OR receipt_status = 1),
			CHECK(state <> 'reverted' OR receipt_status = 0)
		) STRICT`,
		`INSERT INTO evm_transactions_v12 (transaction_id, approval_id, reservation_id, account_id, sender_address, chain_id, nonce, operation, counterparty_address, asset_contract, asset_amount, token_id, plan_hash, transaction_digest, state, signed_payload, transaction_hash, broadcast_attempts, first_broadcast_at_ms, last_broadcast_at_ms, last_result_code, receipt_status, receipt_block_number, receipt_block_hash, receipt_tx_index, receipt_gas_used, effective_gas_price, confirmations, confirmation_target, reorg_count, created_at_ms, updated_at_ms, revision)
			SELECT transaction_id, approval_id, reservation_id, account_id, sender_address, chain_id, nonce, operation, counterparty_address, asset_contract, asset_amount, token_id, plan_hash, transaction_digest, state, signed_payload, transaction_hash, broadcast_attempts, first_broadcast_at_ms, last_broadcast_at_ms, last_result_code, receipt_status, receipt_block_number, receipt_block_hash, receipt_tx_index, receipt_gas_used, effective_gas_price, confirmations, confirmation_target, reorg_count, created_at_ms, updated_at_ms, revision FROM evm_transactions`,
		`CREATE TABLE evm_transaction_effects_v12 (
			transaction_id TEXT NOT NULL,
			effect_index INTEGER NOT NULL CHECK(effect_index >= 0),
			token_id BLOB NOT NULL CHECK(typeof(token_id) = 'blob' AND length(token_id) = 32),
			amount BLOB NOT NULL CHECK(typeof(amount) = 'blob' AND length(amount) = 32),
			PRIMARY KEY (transaction_id, effect_index),
			FOREIGN KEY(transaction_id) REFERENCES evm_transactions_v12(transaction_id) ON UPDATE RESTRICT ON DELETE RESTRICT
		) STRICT`,
		`INSERT INTO evm_transaction_effects_v12 (transaction_id, effect_index, token_id, amount)
			SELECT transaction_id, effect_index, token_id, amount FROM evm_transaction_effects`,
		`DROP TABLE evm_transaction_effects`,
		`DROP TABLE evm_transactions`,
		`ALTER TABLE evm_transactions_v12 RENAME TO evm_transactions`,
		`ALTER TABLE evm_transaction_effects_v12 RENAME TO evm_transaction_effects`,
		`CREATE INDEX ix_evm_transaction_state ON evm_transactions(state, updated_at_ms)`,
		`CREATE INDEX ix_evm_history_account ON evm_transactions(account_id, created_at_ms DESC, transaction_id DESC)`,
		`CREATE INDEX ix_evm_history_sender ON evm_transactions(sender_address, created_at_ms DESC, transaction_id DESC)`,
		`CREATE TRIGGER trg_evm_signed_payload_write_once
			BEFORE UPDATE OF signed_payload, transaction_hash ON evm_transactions
			WHEN OLD.signed_payload IS NOT NULL
			BEGIN SELECT RAISE(ABORT, 'signed EVM payload is immutable'); END`,
		`CREATE TRIGGER trg_evm_history_key_immutable
			BEFORE UPDATE OF transaction_id, created_at_ms ON evm_transactions
			BEGIN SELECT RAISE(ABORT, 'immutable EVM transaction history key'); END`,
		`CREATE TRIGGER trg_evm_effect_immutable
			BEFORE UPDATE OF transaction_id, effect_index, token_id, amount ON evm_transaction_effects
			BEGIN SELECT RAISE(ABORT, 'immutable EVM transaction effect'); END`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateEVMNonceReservations(transaction *gorm.DB) error {
	statements := []string{
		`CREATE TABLE evm_nonce_reservations (
			reservation_id TEXT PRIMARY KEY NOT NULL CHECK(length(reservation_id) = 36),
			operation_id TEXT NOT NULL CHECK(length(operation_id) = 36),
			account_id TEXT NOT NULL,
			sender_address BLOB NOT NULL CHECK(typeof(sender_address) = 'blob' AND length(sender_address) = 20),
			chain_id INTEGER NOT NULL CHECK(chain_id > 0),
			nonce INTEGER NOT NULL CHECK(nonce >= 0),
			plan_generation INTEGER NOT NULL CHECK(plan_generation > 0),
			state TEXT NOT NULL CHECK(state IN ('reserved', 'committed', 'finalized', 'invalidated')),
			reserved_at_ms INTEGER NOT NULL CHECK(reserved_at_ms >= 0),
			expires_at_ms INTEGER NOT NULL CHECK(expires_at_ms > reserved_at_ms),
			committed_at_ms INTEGER,
			finalized_at_ms INTEGER,
			invalidated_at_ms INTEGER,
			invalidation_reason TEXT,
			revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
			FOREIGN KEY(account_id) REFERENCES accounts(account_id) ON UPDATE RESTRICT ON DELETE RESTRICT,
			UNIQUE(reservation_id, account_id, sender_address, chain_id, nonce),
			UNIQUE(account_id, chain_id, operation_id, plan_generation)
		) STRICT`,
		`CREATE UNIQUE INDEX ux_evm_nonce_live
			ON evm_nonce_reservations(chain_id, sender_address, nonce)
			WHERE state IN ('reserved', 'committed', 'finalized')`,
		`CREATE INDEX ix_evm_nonce_recovery ON evm_nonce_reservations(state, expires_at_ms)`,
		`CREATE INDEX ix_evm_nonce_sender ON evm_nonce_reservations(chain_id, sender_address, nonce)`,
	}
	for _, statement := range statements {
		if err := transaction.Exec(statement).Error; err != nil {
			return err
		}
	}
	return nil
}
