package storage

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"blocowallet/internal/wallet"
)

const (
	ledgerTarget  = "11111111-1111-4111-8111-111111111111"
	ledgerTarget2 = "55555555-5555-4555-8555-555555555555"
	ledgerVault   = "22222222-2222-4222-8222-222222222222"
	ledgerAccount = "33333333-3333-4333-8333-333333333333"
	ledgerOperID  = "44444444-4444-4444-8444-444444444444"
	ledgerOperID2 = "66666666-6666-4666-8666-666666666666"
)

func ledgerState(operation, state string, generation uint64) wallet.CredentialBackupState {
	return wallet.CredentialBackupState{
		TargetID:    ledgerTarget,
		VaultID:     ledgerVault,
		AccountID:   ledgerAccount,
		ItemID:      "account",
		OperationID: ledgerOperID,
		Operation:   operation,
		State:       state,
		Generation:  generation,
		Revision:    1,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

func ledgerKey() wallet.CredentialBackupKey {
	return wallet.CredentialBackupKey{
		TargetID:  ledgerTarget,
		VaultID:   ledgerVault,
		AccountID: ledgerAccount,
		ItemID:    "account",
	}
}

func TestCredentialBackupPutGetConfirm(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	if _, err := repository.GetCredentialBackup(ctx, ledgerKey()); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
	state := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 3)
	if err := repository.PutCredentialBackup(ctx, state, 0); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetCredentialBackup(ctx, ledgerKey())
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != wallet.CredentialBackupStatePending || stored.Generation != 3 || stored.Revision != 1 {
		t.Fatalf("unexpected stored state: %+v", stored)
	}
	if err := repository.PutCredentialBackup(ctx, state, 0); !errors.Is(err, wallet.ErrAccountConflict) {
		t.Fatalf("expected duplicate insert conflict, got %v", err)
	}
	if err := repository.ConfirmCredentialBackup(ctx, ledgerKey(), ledgerOperID2, 3); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("expected stale operation ack refusal, got %v", err)
	}
	if err := repository.ConfirmCredentialBackup(ctx, ledgerKey(), ledgerOperID, 2); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("expected stale generation ack refusal, got %v", err)
	}
	if err := repository.ConfirmCredentialBackup(ctx, ledgerKey(), ledgerOperID, 3); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	stored, err = repository.GetCredentialBackup(ctx, ledgerKey())
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != wallet.CredentialBackupStateSynced || stored.SyncedGeneration != 3 || stored.Revision != 2 {
		t.Fatalf("unexpected confirmed state: %+v", stored)
	}
}

func TestCredentialBackupRevisionCAS(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	state := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 3)
	if err := repository.PutCredentialBackup(ctx, state, 0); err != nil {
		t.Fatal(err)
	}
	updated := state
	updated.OperationID = ledgerOperID2
	updated.Generation = 4
	if err := repository.PutCredentialBackup(ctx, updated, 99); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("expected stale revision refusal, got %v", err)
	}
	if err := repository.PutCredentialBackup(ctx, updated, 1); err != nil {
		t.Fatalf("cas update: %v", err)
	}
	regressed := updated
	regressed.Generation = 2
	regressed.OperationID = ledgerOperID
	if err := repository.PutCredentialBackup(ctx, regressed, 2); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("expected generation rollback refusal, got %v", err)
	}
	stored, err := repository.GetCredentialBackup(ctx, ledgerKey())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Generation != 4 || stored.Revision != 2 || stored.OperationID != ledgerOperID2 {
		t.Fatalf("unexpected stored row: %+v", stored)
	}
}

func TestCredentialBackupNamespaceIsolationAndDelete(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	other := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 1)
	other.TargetID = ledgerTarget2
	if err := repository.PutCredentialBackup(ctx, ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := repository.PutCredentialBackup(ctx, other, 0); err != nil {
		t.Fatal(err)
	}
	list, err := repository.ListCredentialBackups(ctx, ledgerTarget, ledgerVault)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("namespace isolation broken: %d rows", len(list))
	}
	deleteIntent := ledgerState(wallet.CredentialBackupOperationDelete, wallet.CredentialBackupStateDeletePending, 2)
	deleteIntent.OperationID = ledgerOperID2
	if err := repository.PutCredentialBackup(ctx, deleteIntent, 1); err != nil {
		t.Fatalf("delete intent: %v", err)
	}
	if err := repository.ConfirmCredentialBackup(ctx, ledgerKey(), ledgerOperID2, 2); err != nil {
		t.Fatalf("delete confirm: %v", err)
	}
	stored, err := repository.GetCredentialBackup(ctx, ledgerKey())
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != wallet.CredentialBackupStateSynced || stored.Operation != wallet.CredentialBackupOperationDelete {
		t.Fatalf("delete tombstone not retained: %+v", stored)
	}
	list, err = repository.ListCredentialBackups(ctx, ledgerTarget, ledgerVault)
	if err != nil || len(list) != 1 {
		t.Fatalf("tombstone not listed: %v %d", err, len(list))
	}
}

func TestCredentialBackupTransactionRollback(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	err := repository.WithAccountTransaction(ctx, func(transaction wallet.AccountRepository) error {
		ledger, ok := transaction.(wallet.CredentialBackupRepository)
		if !ok {
			t.Fatal("transaction repository does not implement CredentialBackupRepository")
		}
		if err := ledger.PutCredentialBackup(ctx, ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 1), 0); err != nil {
			return err
		}
		return errors.New("injected rollback")
	})
	if err == nil {
		t.Fatal("transaction did not propagate rollback error")
	}
	if _, err := repository.GetCredentialBackup(ctx, ledgerKey()); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("rolled back row persisted: %v", err)
	}
	err = repository.WithAccountTransaction(ctx, func(transaction wallet.AccountRepository) error {
		ledger := transaction.(wallet.CredentialBackupRepository)
		return ledger.PutCredentialBackup(ctx, ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 1), 0)
	})
	if err != nil {
		t.Fatalf("transactional put: %v", err)
	}
	stored, err := repository.GetCredentialBackup(ctx, ledgerKey())
	if err != nil || stored.Revision != 1 {
		t.Fatalf("transactional row missing: %v %+v", err, stored)
	}
}

func TestCredentialBackupGuards(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	if _, err := repository.GetCredentialBackup(ctx, wallet.CredentialBackupKey{}); err == nil {
		t.Fatal("empty key accepted")
	}
	stored := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 1)
	if err := repository.PutCredentialBackup(ctx, stored, 0); err != nil {
		t.Fatal(err)
	}
	mismatched := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 2)
	mismatched.ItemID = "file:keystore_v3:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mismatched.ArtifactKind = "keystore_v3"
	mismatched.ArtifactName = "k.json"
	mismatched.ArtifactDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := repository.PutCredentialBackup(ctx, mismatched, 0); err == nil {
		t.Fatal("file row with mismatched digest accepted")
	}
	if err := repository.PutCredentialBackup(ctx, stored, math.MaxInt64); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("expected revision overflow refusal, got %v", err)
	}
	digest := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	prepared := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePrepared, 0)
	prepared.ItemID = "file:keystore_v3:" + digest
	prepared.ArtifactKind = "keystore_v3"
	prepared.ArtifactName = "k.json"
	prepared.ArtifactDigest = digest
	if err := repository.PutCredentialBackup(ctx, prepared, 0); err != nil {
		t.Fatal(err)
	}
	key := wallet.CredentialBackupKey{TargetID: ledgerTarget, VaultID: ledgerVault, AccountID: ledgerAccount, ItemID: prepared.ItemID}
	if err := repository.ConfirmCredentialBackup(ctx, key, ledgerOperID, 0); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("prepared row confirmed early: %v", err)
	}
	next := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 5)
	next.OperationID = ledgerOperID2
	if err := repository.PutCredentialBackup(ctx, next, 1); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfirmCredentialBackup(ctx, ledgerKey(), ledgerOperID, 3); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("stale ack accepted after new operation, got %v", err)
	}
	deleted := ledgerState(wallet.CredentialBackupOperationDelete, wallet.CredentialBackupStateDeletePending, 6)
	deleted.OperationID = ledgerOperID
	if err := repository.PutCredentialBackup(ctx, deleted, 2); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfirmCredentialBackup(ctx, ledgerKey(), ledgerOperID, 6); err != nil {
		t.Fatalf("delete confirm: %v", err)
	}
	revive := ledgerState(wallet.CredentialBackupOperationUpsert, wallet.CredentialBackupStatePending, 7)
	if err := repository.PutCredentialBackup(ctx, revive, 4); !errors.Is(err, wallet.ErrAccountRevisionConflict) {
		t.Fatalf("upsert over delete tombstone accepted: %v", err)
	}
}

func TestCredentialBackupSchema(t *testing.T) {
	repository := newAccountTestRepository(t)
	var version uint
	if err := repository.db.Raw("SELECT MAX(version) FROM schema_migrations").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version != latestSchemaVersion {
		t.Fatalf("schema version %d, want %d", version, latestSchemaVersion)
	}
	if !repository.db.Migrator().HasTable(&wallet.CredentialBackupState{}) {
		t.Fatal("account_credential_backups table missing")
	}
	model := reflect.TypeOf(wallet.CredentialBackupState{})
	for index := 0; index < model.NumField(); index++ {
		field := model.Field(index)
		if field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Map {
			t.Fatalf("credential backup schema field %s could carry opaque payloads", field.Name)
		}
	}
}
