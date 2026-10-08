package storage

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/fido2"
	"blocowallet/internal/safe"
	"blocowallet/internal/wallet"
	"blocowallet/internal/walletconnect"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

const (
	testDeletionMnemonic = "test test test test test test test test test test test junk"
	testDeletionPassword = "deletion-test-password-0!"
)

func testDeletionCodecPolicy() wallet.Argon2idPolicy {
	return wallet.Argon2idPolicy{
		Time: 1, MemoryKiB: 64, Parallelism: 1, KeyLength: 32, SaltLength: 16,
		MaxTime: 4, MaxMemoryKiB: 256 * 1024, MaxParallelism: 8, MaxKeyLength: 32, MaxSaltLength: 64,
	}
}

func newDeletionTestVault(t *testing.T) (*wallet.WalletVault, *GORMRepository) {
	t.Helper()
	repository := newAccountTestRepository(t)
	codec, err := wallet.NewSecretEnvelopeCodec(testDeletionCodecPolicy())
	if err != nil {
		t.Fatal(err)
	}
	vault, err := wallet.NewWalletVault(repository, codec, wallet.VaultOptions{SourceIdentityKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vault.Close)
	return vault, repository
}

func importDeletionAccount(t *testing.T, vault *wallet.WalletVault) wallet.AccountSummary {
	t.Helper()
	summary, err := vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name:                   "Delete Me",
		Mnemonic:               testDeletionMnemonic,
		StoragePassword:        []byte(testDeletionPassword),
		ConfirmStoragePassword: []byte(testDeletionPassword),
	})
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func deleteRequest(summary wallet.AccountSummary, password string) wallet.DeleteAccountRequest {
	return wallet.DeleteAccountRequest{
		AccountID:        summary.AccountID,
		ConfirmAccountID: summary.AccountID,
		Password:         []byte(password),
	}
}

func rawAccountRow(t *testing.T, repository *GORMRepository, accountID string) *wallet.Account {
	t.Helper()
	var account wallet.Account
	if err := repository.db.WithContext(context.Background()).Where("account_id = ?", accountID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	return &account
}

func TestDeleteAccountRemovesUnreferencedWallet(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)

	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repository.GetAccount(context.Background(), summary.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
	reimported, err := vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name:                   "Reimported",
		Mnemonic:               testDeletionMnemonic,
		StoragePassword:        []byte(testDeletionPassword),
		ConfirmStoragePassword: []byte(testDeletionPassword),
	})
	if err != nil {
		t.Fatalf("reimport after clean delete: %v", err)
	}
	if reimported.AccountID == summary.AccountID {
		t.Fatal("expected new account ID on reimport")
	}
}

func TestDeleteAccountTombstonesWhenHistoryExists(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	past := time.Now().UTC().Add(-time.Hour)
	reservation, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID:  "11111111-1111-4111-8111-111111111111",
		OperationID:    "21111111-1111-4111-8111-111111111111",
		AccountID:      summary.AccountID,
		Sender:         common.HexToAddress(summary.Address),
		ChainID:        1,
		PendingNonce:   0,
		PlanGeneration: 1,
		ReservedAt:     past,
		ExpiresAt:      past.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	before := rawAccountRow(t, repository, summary.AccountID)

	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := repository.GetAccount(context.Background(), summary.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("tombstone must be hidden from GetAccount, got %v", err)
	}
	listed, err := repository.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range listed {
		if account.AccountID == summary.AccountID {
			t.Fatal("tombstone must be hidden from ListAccounts")
		}
	}
	found, err := repository.FindAccountsByAddress(context.Background(), summary.Address)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range found {
		if account.AccountID == summary.AccountID {
			t.Fatal("tombstone must be hidden from FindAccountsByAddress")
		}
	}

	row := rawAccountRow(t, repository, summary.AccountID)
	if row.State != wallet.AccountStateTombstoned {
		t.Fatalf("expected tombstone state, got %q", row.State)
	}
	if len(row.SecretEnvelope) != 0 {
		t.Fatal("tombstone must zero secret envelope")
	}
	if row.Capabilities != 0 {
		t.Fatal("tombstone must zero capabilities")
	}
	if row.AuthorizationEpoch != before.AuthorizationEpoch+1 || row.BackupGeneration != before.BackupGeneration+1 {
		t.Fatal("tombstone must bump authorization epoch and backup generation")
	}
	if row.SourceIdentity != before.SourceIdentity || row.Address != before.Address || row.Name != before.Name {
		t.Fatal("tombstone must retain source identity, address, and name")
	}

	var historyRows []evmNonceReservationRow
	if err := repository.db.WithContext(context.Background()).
		Where("reservation_id = ?", reservation.ReservationID).Find(&historyRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(historyRows) != 1 {
		t.Fatal("history reservation row must be retained")
	}
	_, err = vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name:                   "Reimported",
		Mnemonic:               testDeletionMnemonic,
		StoragePassword:        []byte(testDeletionPassword),
		ConfirmStoragePassword: []byte(testDeletionPassword),
	})
	if !errors.Is(err, wallet.ErrAccountDeleted) {
		t.Fatalf("reimport over tombstone must return ErrAccountDeleted, got %v", err)
	}
}

func TestDeleteAccountBlockedByActiveReservation(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	now := time.Now().UTC()
	_, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID:  "31111111-1111-4111-8111-111111111111",
		OperationID:    "41111111-1111-4111-8111-111111111111",
		AccountID:      summary.AccountID,
		Sender:         common.HexToAddress(summary.Address),
		ChainID:        1,
		PendingNonce:   0,
		PlanGeneration: 1,
		ReservedAt:     now,
		ExpiresAt:      now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); !errors.Is(err, wallet.ErrAccountDeletionPending) {
		t.Fatalf("expected ErrAccountDeletionPending, got %v", err)
	}
	row := rawAccountRow(t, repository, summary.AccountID)
	if len(row.SecretEnvelope) == 0 || row.State == wallet.AccountStateTombstoned {
		t.Fatal("blocked delete must leave account intact")
	}
}

func TestDeleteAccountRejectsInvalidRequests(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	before := rawAccountRow(t, repository, summary.AccountID)

	cases := []struct {
		name    string
		request wallet.DeleteAccountRequest
		want    error
	}{
		{"empty account id", wallet.DeleteAccountRequest{ConfirmAccountID: "x", Password: []byte(testDeletionPassword)}, wallet.ErrAccountDeleteConfirmation},
		{"mismatched confirmation", wallet.DeleteAccountRequest{AccountID: summary.AccountID, ConfirmAccountID: "other", Password: []byte(testDeletionPassword)}, wallet.ErrAccountDeleteConfirmation},
		{"empty confirmation", wallet.DeleteAccountRequest{AccountID: summary.AccountID, Password: []byte(testDeletionPassword)}, wallet.ErrAccountDeleteConfirmation},
		{"unknown account", wallet.DeleteAccountRequest{AccountID: "51111111-1111-4111-8111-111111111111", ConfirmAccountID: "51111111-1111-4111-8111-111111111111", Password: []byte(testDeletionPassword)}, wallet.ErrAccountNotFound},
		{"empty password", deleteRequest(summary, ""), nil},
		{"wrong password", deleteRequest(summary, "definitely-wrong-password"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := vault.DeleteAccount(context.Background(), tc.request)
			if tc.want == nil {
				if err == nil {
					t.Fatal("expected error")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}

	after := rawAccountRow(t, repository, summary.AccountID)
	if after.Revision != before.Revision || string(after.SecretEnvelope) != string(before.SecretEnvelope) || after.State != before.State {
		t.Fatal("failed deletes must leave account row unchanged")
	}
	if _, err := vault.Unlock(context.Background(), summary.AccountID, []byte(testDeletionPassword)); err != nil {
		t.Fatalf("unlock must still work after failed deletes: %v", err)
	}
}

func TestDeleteAccountRejectsCancelledContext(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := vault.DeleteAccount(ctx, deleteRequest(summary, testDeletionPassword)); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	row := rawAccountRow(t, repository, summary.AccountID)
	if len(row.SecretEnvelope) == 0 {
		t.Fatal("cancelled delete must leave envelope intact")
	}
}

type noDeletionExtensionRepository struct {
	wallet.AccountRepository
}

func TestDeleteAccountFailsClosedWithoutRepositorySupport(t *testing.T) {
	inner := newAccountTestRepository(t)
	codec, err := wallet.NewSecretEnvelopeCodec(testDeletionCodecPolicy())
	if err != nil {
		t.Fatal(err)
	}
	wrapped := noDeletionExtensionRepository{AccountRepository: inner}
	vault, err := wallet.NewWalletVault(wrapped, codec, wallet.VaultOptions{SourceIdentityKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vault.Close)
	summary := importDeletionAccount(t, vault)
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); !errors.Is(err, wallet.ErrAccountDeletionUnsupported) {
		t.Fatalf("expected ErrAccountDeletionUnsupported, got %v", err)
	}
	if _, err := vault.Unlock(context.Background(), summary.AccountID, []byte(testDeletionPassword)); err != nil {
		t.Fatalf("account must remain usable: %v", err)
	}
}

func TestDeleteAccountClosedVaultRejects(t *testing.T) {
	vault, _ := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	vault.Close()
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); !errors.Is(err, wallet.ErrVaultClosed) {
		t.Fatalf("expected ErrVaultClosed, got %v", err)
	}
}

func TestDeleteAccountInvalidatesSessionsAndBackup(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	pending, challenge, err := vault.Create(context.Background(), wallet.CreateAccountRequest{
		Name:     "Pending Backup",
		Password: []byte(testDeletionPassword),
	})
	if err != nil {
		t.Fatal(err)
	}
	summary := importDeletionAccount(t, vault)
	other, err := vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
		Name:                   "Survivor",
		Mnemonic:               "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
		StoragePassword:        []byte(testDeletionPassword),
		ConfirmStoragePassword: []byte(testDeletionPassword),
	})
	if err != nil {
		t.Fatal(err)
	}
	handle, err := vault.Unlock(context.Background(), summary.AccountID, []byte(testDeletionPassword))
	if err != nil {
		t.Fatal(err)
	}
	otherHandle, err := vault.Unlock(context.Background(), other.AccountID, []byte(testDeletionPassword))
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); err != nil {
		t.Fatal(err)
	}
	if err := vault.Lock(handle); err == nil {
		t.Fatal("session handle must be invalidated after delete")
	}
	if err := vault.DeleteAccount(context.Background(), wallet.DeleteAccountRequest{
		AccountID:        pending.AccountID,
		ConfirmAccountID: pending.AccountID,
		Password:         []byte(testDeletionPassword),
	}); err != nil {
		t.Fatal(err)
	}
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers); err == nil {
		t.Fatal("backup challenge must be invalidated after delete")
	}
	if _, err := vault.AuthorizationEpoch(context.Background(), otherHandle); err != nil {
		t.Fatal("other account session must survive")
	}
	if _, err := repository.GetAccount(context.Background(), other.AccountID); err != nil {
		t.Fatal("other account row must survive")
	}
}

func TestDeleteAccountSecondDeleteFails(t *testing.T) {
	vault, _ := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); err != nil {
		t.Fatal(err)
	}
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound on second delete, got %v", err)
	}
}

func TestDeleteAccountRepositoryRejectsStaleRevision(t *testing.T) {
	repository := newAccountTestRepository(t)
	extension, ok := interface{}(repository).(wallet.AccountDeletionRepository)
	if !ok {
		t.Fatal("GORM repository must implement AccountDeletionRepository")
	}
	account := testAccount("61111111-1111-4111-8111-111111111111", "source-stale")
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	err := extension.DeleteAccount(context.Background(), account.AccountID, account.Revision+9, time.Now().UTC())
	if err == nil {
		t.Fatal("expected stale revision rejection")
	}
	row := rawAccountRow(t, repository, account.AccountID)
	if row.State == wallet.AccountStateTombstoned || len(row.SecretEnvelope) == 0 {
		t.Fatal("stale revision delete must leave account intact")
	}
}

func TestDeleteAccountKeepsSiblingWithSameAddress(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	watch, err := vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name:    "Watcher",
		Address: summary.Address,
	})
	if err != nil {
		t.Fatal(err)
	}
	if watch.AccountID == summary.AccountID {
		t.Fatal("expected distinct IDs")
	}
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); !errors.Is(err, wallet.ErrAccountDeletionPending) {
		t.Fatalf("dependent watch-only must block signer delete, got %v", err)
	}
	if err := vault.DeleteAccount(context.Background(), wallet.DeleteAccountRequest{
		AccountID: watch.AccountID, ConfirmAccountID: watch.AccountID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetAccount(context.Background(), summary.AccountID); err != nil {
		t.Fatal("software account must survive sibling delete")
	}
	if _, err := vault.Unlock(context.Background(), summary.AccountID, []byte(testDeletionPassword)); err != nil {
		t.Fatalf("software account must still unlock after sibling delete: %v", err)
	}
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetAccount(context.Background(), watch.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("watch-only must be gone, got %v", err)
	}
}

func TestDeleteAccountWatchOnlyNeedsNoPassword(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	watch, err := vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name:    "Watcher",
		Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := wallet.DeleteAccountRequest{AccountID: watch.AccountID, ConfirmAccountID: watch.AccountID}
	if err := vault.DeleteAccount(context.Background(), request); err != nil {
		t.Fatalf("watch-only delete without password: %v", err)
	}
	if _, err := repository.GetAccount(context.Background(), watch.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("expected gone, got %v", err)
	}
}

func TestDeleteAccountRejectsPasswordForNonSoftware(t *testing.T) {
	vault, _ := newDeletionTestVault(t)
	watch, err := vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name:    "Watcher",
		Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = vault.DeleteAccount(context.Background(), wallet.DeleteAccountRequest{
		AccountID:        watch.AccountID,
		ConfirmAccountID: watch.AccountID,
		Password:         []byte("should-be-rejected"),
	})
	if err == nil {
		t.Fatal("non-software delete must reject a supplied password")
	}
}

func TestDeleteAccountRejectsUnknownState(t *testing.T) {
	repository := newAccountTestRepository(t)
	codec, err := wallet.NewSecretEnvelopeCodec(testDeletionCodecPolicy())
	if err != nil {
		t.Fatal(err)
	}
	vault, err := wallet.NewWalletVault(repository, codec, wallet.VaultOptions{SourceIdentityKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vault.Close)
	account := testAccount("71111111-1111-4111-8111-111111111111", "source-unknown")
	account.State = wallet.AccountState("bogus")
	if err := repository.db.WithContext(context.Background()).Create(account).Error; err != nil {
		t.Fatal(err)
	}
	err = vault.DeleteAccount(context.Background(), wallet.DeleteAccountRequest{
		AccountID: account.AccountID, ConfirmAccountID: account.AccountID, Password: []byte(testDeletionPassword),
	})
	if err == nil || errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("unknown state must be rejected before delete, got %v", err)
	}
}

func TestDeleteAccountRevokesWalletConnectAndFIDO2(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	watch, err := vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name:    "DApp Watcher",
		Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	if err != nil {
		t.Fatal(err)
	}
	nowMS := time.Now().UTC().UnixMilli()
	session := &walletconnect.Session{
		Topic: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PeerName: "Peer", PeerMetadata: map[string]any{"name": "peer"},
		AccountID: watch.AccountID,
		Namespaces: walletconnect.Namespaces{
			"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign"}, Events: []string{"chainChanged"}},
		},
		ExpiresAt: nowMS + 60_000, CreatedAt: nowMS,
	}
	if err := repository.SaveSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	challenge := &fido2.Challenge{
		ChallengeID: "81111111-1111-4111-8111-111111111111", Kind: fido2.ChallengeAuthenticate,
		RPID: "example.com", Origin: "https://example.com", AccountID: watch.AccountID,
		Challenge: []byte("0123456789abcdef0123456789abcdef"), ExpiresAt: nowMS + 60_000, CreatedAt: nowMS,
	}
	if err := repository.SaveChallenge(context.Background(), challenge); err != nil {
		t.Fatal(err)
	}
	before := rawAccountRow(t, repository, watch.AccountID)

	err = vault.DeleteAccount(context.Background(), wallet.DeleteAccountRequest{
		AccountID: watch.AccountID, ConfirmAccountID: watch.AccountID,
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	stored, err := repository.GetSession(context.Background(), session.Topic)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Revoked {
		t.Fatal("WalletConnect session must be revoked after delete")
	}
	storedChallenge, err := repository.GetChallenge(context.Background(), challenge.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	if !storedChallenge.Used {
		t.Fatal("FIDO2 challenge must be marked used after delete")
	}
	row := rawAccountRow(t, repository, watch.AccountID)
	if row.State != wallet.AccountStateTombstoned || len(row.SecretEnvelope) != 0 {
		t.Fatal("expected tombstoned account with cleared envelope")
	}
	if row.SourceIdentity != before.SourceIdentity {
		t.Fatal("source identity must be retained for history")
	}
}

func TestDeleteAccountRollsBackWhenRevocationFails(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	watch, err := vault.ImportWatchOnly(context.Background(), wallet.WatchOnlyImportRequest{
		Name:    "DApp Watcher",
		Address: "0x9858EfFD232B4033E47d90003D41EC34EcaEda94",
	})
	if err != nil {
		t.Fatal(err)
	}
	nowMS := time.Now().UTC().UnixMilli()
	session := &walletconnect.Session{
		Topic: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", PeerName: "Peer", PeerMetadata: map[string]any{"name": "peer"},
		AccountID: watch.AccountID,
		Namespaces: walletconnect.Namespaces{
			"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign"}},
		},
		ExpiresAt: nowMS + 60_000, CreatedAt: nowMS,
	}
	if err := repository.SaveSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	challenge := &fido2.Challenge{
		ChallengeID: "91111111-1111-4111-8111-111111111111", Kind: fido2.ChallengeAuthenticate,
		RPID: "example.com", Origin: "https://example.com", AccountID: watch.AccountID,
		Challenge: []byte("0123456789abcdef0123456789abcdef"), ExpiresAt: nowMS + 60_000, CreatedAt: nowMS,
	}
	if err := repository.SaveChallenge(context.Background(), challenge); err != nil {
		t.Fatal(err)
	}
	before := rawAccountRow(t, repository, watch.AccountID)

	sentinel := errors.New("test injected wc revoke failure")
	if err := repository.db.Callback().Update().Before("gorm:update").Register("test_fail_wc_revoke", func(db *gorm.DB) {
		if db.Statement != nil && db.Statement.Table == "wc_sessions" {
			_ = db.AddError(sentinel)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = repository.db.Callback().Update().Remove("test_fail_wc_revoke")
	})

	err = vault.DeleteAccount(context.Background(), wallet.DeleteAccountRequest{
		AccountID: watch.AccountID, ConfirmAccountID: watch.AccountID,
	})
	if err == nil {
		t.Fatal("expected injected failure")
	}
	row := rawAccountRow(t, repository, watch.AccountID)
	if row.State == wallet.AccountStateTombstoned || row.Revision != before.Revision ||
		row.AuthorizationEpoch != before.AuthorizationEpoch || len(row.SecretEnvelope) != len(before.SecretEnvelope) {
		t.Fatal("rolled back delete must leave account row untouched")
	}
	stored, err := repository.GetSession(context.Background(), session.Topic)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revoked {
		t.Fatal("revocation must be rolled back")
	}
	storedChallenge, err := repository.GetChallenge(context.Background(), challenge.ChallengeID)
	if err != nil {
		t.Fatal(err)
	}
	if storedChallenge.Used {
		t.Fatal("challenge consume must be rolled back")
	}
}

func newGuardFixtureAccount(t *testing.T, repository *GORMRepository, id, sourceIdentity string) *wallet.Account {
	t.Helper()
	account := testAccount(id, sourceIdentity)
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	return account
}

func expectDeletionBlocked(t *testing.T, repository *GORMRepository, account *wallet.Account) {
	t.Helper()
	ext, ok := interface{}(repository).(wallet.AccountDeletionRepository)
	if !ok {
		t.Fatal("repository must implement deletion")
	}
	err := ext.DeleteAccount(context.Background(), account.AccountID, 1, time.Now().UTC())
	if !errors.Is(err, wallet.ErrAccountDeletionPending) {
		t.Fatalf("expected ErrAccountDeletionPending, got %v", err)
	}
	row := rawAccountRow(t, repository, account.AccountID)
	if row.State == wallet.AccountStateTombstoned || len(row.SecretEnvelope) == 0 {
		t.Fatal("blocked delete must leave account intact")
	}
}

func TestDeleteAccountGuardActiveTransaction(t *testing.T) {
	repository := newAccountTestRepository(t)
	record := createAuthorizedTestTransaction(t, repository, time.Now().UTC())
	expectDeletionBlocked(t, repository, &wallet.Account{AccountID: record.AccountID})
}

func TestDeleteAccountGuardMessageSigning(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := newGuardFixtureAccount(t, repository, "a1111111-1111-4111-8111-111111111111", "msg-signing")
	now := time.Now().UTC()
	approval := testMessageApproval(account, now)
	if err := repository.IssueMessageApproval(context.Background(), approval); err != nil {
		t.Fatal(err)
	}
	_, err := repository.AuthorizeMessageSigning(context.Background(), evm.AuthorizeMessageSigningRequest{
		SigningID: "b1111111-1111-4111-8111-111111111111", ApprovalID: approval.ApprovalID,
		AccountID: account.AccountID, Signer: approval.Signer, Scheme: approval.Scheme,
		Digest: approval.Digest, IntentHash: approval.IntentHash,
		AuthorizationEpoch: approval.AuthorizationEpoch, AuthorizedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	expectDeletionBlocked(t, repository, account)
}

func TestDeleteAccountGuardPendingEVMApproval(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := newGuardFixtureAccount(t, repository, "c1111111-1111-4111-8111-111111111111", "evm-approval")
	now := time.Now().UTC()
	reservation, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID: "d1111111-1111-4111-8111-111111111111", OperationID: "e1111111-1111-4111-8111-111111111111",
		AccountID: account.AccountID, Sender: common.HexToAddress(account.Address), ChainID: 1,
		PendingNonce: 0, PlanGeneration: 1, ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.IssueApproval(context.Background(), evm.SigningApproval{
		ApprovalID: "f1111111-1111-4111-8111-111111111111", ReservationID: reservation.ReservationID,
		AccountID: account.AccountID, Sender: common.HexToAddress(account.Address), ChainID: 1,
		Nonce: reservation.Nonce, AuthorizationEpoch: account.AuthorizationEpoch,
		PlanHash: [32]byte{1}, TransactionDigest: [32]byte{2},
		RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard, ConfirmationTarget: 2,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.InvalidateUnsignedReservation(context.Background(), evm.InvalidateReservationRequest{
		ReservationID: reservation.ReservationID, AccountID: account.AccountID,
		PlanGeneration: 1, Reason: "plan_stale", InvalidatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	expectDeletionBlocked(t, repository, account)
}

func TestDeleteAccountGuardPendingMessageApproval(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := newGuardFixtureAccount(t, repository, "01111111-1111-4111-8111-111111111111", "msg-approval")
	if err := repository.IssueMessageApproval(context.Background(), testMessageApproval(account, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	expectDeletionBlocked(t, repository, account)
}

func createPendingSafeProposal(t *testing.T, repository *GORMRepository, id, accountID string, owners []safe.OwnerSnapshot) {
	t.Helper()
	now := time.Now().UTC()
	proposal := &safe.Proposal{
		ProposalID: id, SafeAddress: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		AccountID: accountID, ChainID: 1, Nonce: big.NewInt(0),
		Transaction: safe.SafeTransaction{
			To: common.HexToAddress("0x4444444444444444444444444444444444444444"), Value: big.NewInt(0),
			SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0), GasPrice: big.NewInt(0), Nonce: big.NewInt(0),
		},
		Owners: owners, Threshold: 1, Status: safe.ProposalPending,
		CreatedAt: now, UpdatedAt: now,
	}
	encoded, err := safe.EncodeProposal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.db.WithContext(context.Background()).Exec(
		"INSERT INTO safe_proposals (proposal_id, account_id, safe_address, chain_id, status, encoding, revision, created_at_ms, updated_at_ms) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		proposal.ProposalID, proposal.AccountID, proposal.SafeAddress.Hex(), int64(proposal.ChainID),
		string(proposal.Status), encoded, 1, now.UnixMilli(), now.UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
}

func TestDeleteAccountGuardOwnPendingSafeProposal(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := newGuardFixtureAccount(t, repository, "12111111-1111-4111-8111-111111111111", "safe-own")
	createPendingSafeProposal(t, repository, "22111111-1111-4111-8111-111111111111", account.AccountID, nil)
	expectDeletionBlocked(t, repository, account)
}

func TestDeleteAccountGuardSafeOwnerDependency(t *testing.T) {
	repository := newAccountTestRepository(t)
	ownerAccount := newGuardFixtureAccount(t, repository, "32111111-1111-4111-8111-111111111111", "safe-owner")
	safeAccount := newGuardFixtureAccount(t, repository, "42111111-1111-4111-8111-111111111111", "safe-account")
	createPendingSafeProposal(t, repository, "52111111-1111-4111-8111-111111111111", safeAccount.AccountID,
		[]safe.OwnerSnapshot{{Address: common.HexToAddress(ownerAccount.Address)}})
	expectDeletionBlocked(t, repository, ownerAccount)
}

func TestDeleteAccountSafeProposalUnrelatedOwnerDoesNotBlock(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := newGuardFixtureAccount(t, repository, "62111111-1111-4111-8111-111111111111", "safe-unrelated")
	safeAccount := newGuardFixtureAccount(t, repository, "72111111-1111-4111-8111-111111111111", "safe-account-2")
	createPendingSafeProposal(t, repository, "82111111-1111-4111-8111-111111111111", safeAccount.AccountID,
		[]safe.OwnerSnapshot{{Address: common.HexToAddress("0x9858EfFD232B4033E47d90003D41EC34EcaEda94")}})
	ext := interface{}(repository).(wallet.AccountDeletionRepository)
	if err := ext.DeleteAccount(context.Background(), account.AccountID, 1, time.Now().UTC()); err != nil {
		t.Fatalf("unrelated owner must not block, got %v", err)
	}
	if _, err := repository.GetAccount(context.Background(), account.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("expected account removed, got %v", err)
	}
}

func TestDeleteAccountConcurrentSameID(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- vault.DeleteAccount(ctx, deleteRequest(summary, testDeletionPassword))
		}()
	}
	wg.Wait()
	close(results)
	var succeeded, notFound int
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, wallet.ErrAccountNotFound) || errors.Is(err, wallet.ErrAccountRevisionConflict) {
			notFound++
		} else {
			t.Fatalf("unexpected concurrent delete error: %v", err)
		}
	}
	if succeeded != 1 || notFound != 1 {
		t.Fatalf("expected exactly one success and one failure, got %d/%d", succeeded, notFound)
	}
	if _, err := repository.GetAccount(context.Background(), summary.AccountID); !errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("expected account gone, got %v", err)
	}
}

func TestDeleteAccountBatchImportReportsDeletedSource(t *testing.T) {
	vault, repository := newDeletionTestVault(t)
	summary := importDeletionAccount(t, vault)
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID: "a2111111-1111-4111-8111-111111111111", OperationID: "b2111111-1111-4111-8111-111111111111",
		AccountID: summary.AccountID, Sender: common.HexToAddress(summary.Address), ChainID: 1,
		PendingNonce: 0, PlanGeneration: 1, ReservedAt: past, ExpiresAt: past.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); err != nil {
		t.Fatal(err)
	}
	results := vault.ImportMnemonicBatch(context.Background(), wallet.MnemonicBatchImportRequest{
		Items:                  []wallet.MnemonicBatchItem{{Name: "Again", SourcePath: "again.mnemonic", Mnemonic: []byte(testDeletionMnemonic)}},
		StoragePassword:        []byte(testDeletionPassword),
		ConfirmStoragePassword: []byte(testDeletionPassword),
	})
	if len(results) != 1 || !errors.Is(results[0].Err, wallet.ErrAccountDeleted) {
		t.Fatalf("batch reimport of tombstoned source must fail with ErrAccountDeleted, got %+v", results)
	}
}

func TestDeleteAccountLeavesSourceFileUntouched(t *testing.T) {
	vault, _ := newDeletionTestVault(t)
	source := filepath.Join(t.TempDir(), "keep.mnemonic")
	original := []byte(testDeletionMnemonic)
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	summary := importDeletionAccount(t, vault)
	if err := vault.DeleteAccount(context.Background(), deleteRequest(summary, testDeletionPassword)); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("source mnemonic file must remain byte-identical after delete")
	}
}

func TestDeleteAccountRejectsUnknownSignerKind(t *testing.T) {
	repository := newAccountTestRepository(t)
	codec, err := wallet.NewSecretEnvelopeCodec(testDeletionCodecPolicy())
	if err != nil {
		t.Fatal(err)
	}
	vault, err := wallet.NewWalletVault(repository, codec, wallet.VaultOptions{SourceIdentityKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vault.Close)
	account := testAccount("c2111111-1111-4111-8111-111111111111", "source-bogus-kind")
	account.SignerKind = wallet.SignerKind("bogus_kind")
	if err := repository.db.WithContext(context.Background()).Create(account).Error; err != nil {
		t.Fatal(err)
	}
	err = vault.DeleteAccount(context.Background(), wallet.DeleteAccountRequest{
		AccountID: account.AccountID, ConfirmAccountID: account.AccountID,
	})
	if err == nil || errors.Is(err, wallet.ErrAccountNotFound) {
		t.Fatalf("unknown signer kind must be rejected before passwordless delete, got %v", err)
	}
	row := rawAccountRow(t, repository, account.AccountID)
	if len(row.SecretEnvelope) == 0 {
		t.Fatal("rejected delete must leave record untouched")
	}
}
