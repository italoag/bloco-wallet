package storage

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/wallet"
)

func credentialDeletionIntent() wallet.CredentialBackupDeleteIntent {
	return wallet.CredentialBackupDeleteIntent{
		TargetID:    "c1111111-1111-4111-8111-111111111111",
		VaultID:     "c2222222-2222-4222-8222-222222222222",
		OperationID: "c3333333-3333-4333-8333-333333333333",
	}
}

func TestCredentialBackupDeleteBlockedLeavesNoIntent(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := newGuardFixtureAccount(t, repository, "a1111111-1111-4111-8111-111111111111", "cred-blocked")
	related := testAccount("a2222222-2222-4222-8222-222222222222", "cred-related")
	related.State = wallet.AccountStateActive
	related.RelatedAccountID = account.AccountID
	if err := repository.CreateAccount(ctx, related); err != nil {
		t.Fatal(err)
	}
	intent := credentialDeletionIntent()
	err := repository.DeleteAccountWithCredentialBackup(ctx, account.AccountID, account.Revision, time.Now().UTC(), intent)
	if !errors.Is(err, wallet.ErrAccountDeletionPending) {
		t.Fatalf("expected ErrAccountDeletionPending, got %v", err)
	}
	rows, err := repository.ListCredentialBackups(ctx, intent.TargetID, intent.VaultID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("blocked delete must leave no credential intent, got %+v", rows)
	}
	stored, err := repository.GetAccount(ctx, account.AccountID)
	if err != nil || stored.State != wallet.AccountStateActive {
		t.Fatalf("account must remain intact: %v %v", stored, err)
	}
}

func TestCredentialBackupDeleteLedgerFailureRollsBack(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := newGuardFixtureAccount(t, repository, "a3333333-3333-4333-8333-333333333333", "cred-rollback")
	intent := credentialDeletionIntent()
	state := wallet.CredentialBackupState{
		TargetID:    intent.TargetID,
		VaultID:     intent.VaultID,
		AccountID:   account.AccountID,
		ItemID:      "account",
		OperationID: "c4444444-4444-4444-8444-444444444444",
		Operation:   wallet.CredentialBackupOperationUpsert,
		State:       wallet.CredentialBackupStatePending,
		Generation:  account.EnvelopeGeneration,
		Revision:    1,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := repository.PutCredentialBackup(ctx, state, 0); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.WithContext(ctx).Model(&wallet.CredentialBackupState{}).
		Where("target_id = ? AND vault_id = ? AND account_id = ? AND item_id = ?",
			intent.TargetID, intent.VaultID, account.AccountID, "account").
		Update("revision", uint64(math.MaxInt64)).Error; err != nil {
		t.Fatal(err)
	}
	err := repository.DeleteAccountWithCredentialBackup(ctx, account.AccountID, account.Revision, time.Now().UTC(), intent)
	if err == nil {
		t.Fatal("ledger failure must abort the delete")
	}
	stored, getErr := repository.GetAccount(ctx, account.AccountID)
	if getErr != nil || stored.State != wallet.AccountStateActive || len(stored.SecretEnvelope) == 0 {
		t.Fatalf("ledger failure must roll back the account delete: %v %v", stored, getErr)
	}
	row, getErr := repository.GetCredentialBackup(ctx, wallet.CredentialBackupKey{
		TargetID: intent.TargetID, VaultID: intent.VaultID,
		AccountID: account.AccountID, ItemID: "account",
	})
	if getErr != nil {
		t.Fatal(getErr)
	}
	if row.State != wallet.CredentialBackupStatePending || row.Revision != uint64(math.MaxInt64) {
		t.Fatalf("ledger row must be unchanged after rollback: %+v", row)
	}
}

func TestCredentialBackupDeletePhysicalPreservesPendingDelete(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := newGuardFixtureAccount(t, repository, "a5555555-5555-4555-8555-555555555555", "cred-physical")
	intent := credentialDeletionIntent()
	digest := strings.Repeat("ab", 32)
	now := time.Now().UTC()
	rows := []wallet.CredentialBackupState{
		{
			TargetID: intent.TargetID, VaultID: intent.VaultID, AccountID: account.AccountID,
			ItemID:      "account",
			OperationID: "c6666666-6666-4666-8666-666666666666",
			Operation:   wallet.CredentialBackupOperationUpsert,
			State:       wallet.CredentialBackupStatePending,
			Generation:  account.EnvelopeGeneration,
			Revision:    1, CreatedAt: now, UpdatedAt: now,
		},
		{
			TargetID: intent.TargetID, VaultID: intent.VaultID, AccountID: account.AccountID,
			ItemID:         "file:keystore_v3:" + digest,
			OperationID:    "c6666666-6666-4666-8666-666666666666",
			Operation:      wallet.CredentialBackupOperationUpsert,
			State:          wallet.CredentialBackupStatePending,
			Generation:     0,
			ArtifactKind:   "keystore_v3",
			ArtifactName:   "source.json",
			ArtifactPath:   "/tmp/source.json",
			ArtifactDigest: digest,
			Revision:       1, CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, row := range rows {
		if err := repository.PutCredentialBackup(ctx, row, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.DeleteAccountWithCredentialBackup(ctx, account.AccountID, account.Revision, now, intent); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetAccount(ctx, account.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("expected physical delete, got %v", err)
	}
	for _, itemID := range []string{"account", "file:keystore_v3:" + digest} {
		row, err := repository.GetCredentialBackup(ctx, wallet.CredentialBackupKey{
			TargetID: intent.TargetID, VaultID: intent.VaultID,
			AccountID: account.AccountID, ItemID: itemID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if row.State != wallet.CredentialBackupStateDeletePending ||
			row.Operation != wallet.CredentialBackupOperationDelete ||
			row.OperationID != intent.OperationID ||
			row.Revision != 2 {
			t.Fatalf("row must be delete_pending under the new operation: %+v", row)
		}
	}
	accountRow, err := repository.GetCredentialBackup(ctx, wallet.CredentialBackupKey{
		TargetID: intent.TargetID, VaultID: intent.VaultID,
		AccountID: account.AccountID, ItemID: "account",
	})
	if err != nil {
		t.Fatal(err)
	}
	if accountRow.Generation != account.EnvelopeGeneration {
		t.Fatalf("account row generation mismatch: %d", accountRow.Generation)
	}
}
