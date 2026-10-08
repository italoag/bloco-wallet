package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"blocowallet/internal/keepass"

	"github.com/ethereum/go-ethereum/crypto"
	kp "github.com/tobischo/gokeepasslib/v3"
)

func testKeystoreFixture(t *testing.T) ([]byte, []byte) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ethers-v5-ethers1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data, []byte("password")
}

const credentialTestMaster = "test-master-password-42"

type credentialTestRepository struct {
	*memoryAccountRepository
	muBackups sync.Mutex
	backups   map[string]CredentialBackupState
	failPut   *atomic.Int32
}

func newCredentialTestRepository() *credentialTestRepository {
	return &credentialTestRepository{
		memoryAccountRepository: newMemoryAccountRepository(),
		backups:                 make(map[string]CredentialBackupState),
		failPut:                 &atomic.Int32{},
	}
}

func credentialKeyString(key CredentialBackupKey) string {
	return key.TargetID + "|" + key.VaultID + "|" + key.AccountID + "|" + key.ItemID
}

func (repository *credentialTestRepository) GetCredentialBackup(_ context.Context, key CredentialBackupKey) (CredentialBackupState, error) {
	if err := key.Validate(); err != nil {
		return CredentialBackupState{}, err
	}
	repository.muBackups.Lock()
	defer repository.muBackups.Unlock()
	state, exists := repository.backups[credentialKeyString(key)]
	if !exists {
		return CredentialBackupState{}, ErrAccountNotFound
	}
	return state, nil
}

func (repository *credentialTestRepository) ListCredentialBackups(_ context.Context, targetID, vaultID string) ([]CredentialBackupState, error) {
	repository.muBackups.Lock()
	defer repository.muBackups.Unlock()
	states := make([]CredentialBackupState, 0)
	for _, state := range repository.backups {
		if state.TargetID == targetID && state.VaultID == vaultID {
			states = append(states, state)
		}
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].AccountID != states[j].AccountID {
			return states[i].AccountID < states[j].AccountID
		}
		return states[i].ItemID < states[j].ItemID
	})
	return states, nil
}

func (repository *credentialTestRepository) PutCredentialBackup(_ context.Context, state CredentialBackupState, expectedRevision uint64) error {
	for {
		remaining := repository.failPut.Load()
		if remaining <= 0 {
			break
		}
		if repository.failPut.CompareAndSwap(remaining, remaining-1) {
			return fmt.Errorf("injected credential ledger failure")
		}
	}
	repository.muBackups.Lock()
	defer repository.muBackups.Unlock()
	key := credentialKeyString(CredentialBackupKey{
		TargetID:  state.TargetID,
		VaultID:   state.VaultID,
		AccountID: state.AccountID,
		ItemID:    state.ItemID,
	})
	existing, exists := repository.backups[key]
	if expectedRevision == 0 {
		if state.Revision != 1 || exists {
			return ErrAccountRevisionConflict
		}
		if err := state.Validate(); err != nil {
			return err
		}
		repository.backups[key] = state
		return nil
	}
	if !exists || existing.Revision != expectedRevision {
		return ErrAccountRevisionConflict
	}
	if state.Operation == CredentialBackupOperationUpsert &&
		(existing.State == CredentialBackupStateDeletePending ||
			(existing.Operation == CredentialBackupOperationDelete && existing.State == CredentialBackupStateSynced) ||
			existing.Generation > state.Generation) {
		return ErrAccountRevisionConflict
	}
	state.Revision = expectedRevision + 1
	if err := state.Validate(); err != nil {
		return err
	}
	repository.backups[key] = state
	return nil
}

func (repository *credentialTestRepository) ConfirmCredentialBackup(_ context.Context, key CredentialBackupKey, operationID string, generation uint64) error {
	if err := key.Validate(); err != nil {
		return err
	}
	repository.muBackups.Lock()
	defer repository.muBackups.Unlock()
	existing, exists := repository.backups[credentialKeyString(key)]
	if !exists || existing.OperationID != operationID || existing.Generation != generation {
		return ErrAccountRevisionConflict
	}
	pairOK := (existing.State == CredentialBackupStatePending && existing.Operation == CredentialBackupOperationUpsert) ||
		(existing.State == CredentialBackupStateDeletePending && existing.Operation == CredentialBackupOperationDelete)
	if !pairOK {
		return ErrAccountRevisionConflict
	}
	existing.State = CredentialBackupStateSynced
	existing.SyncedGeneration = generation
	existing.Revision++
	repository.backups[credentialKeyString(key)] = existing
	return nil
}

func (repository *credentialTestRepository) DeleteAccount(_ context.Context, accountID string, expectedRevision uint64, deletedAt time.Time) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	account, exists := repository.accounts[accountID]
	if !exists || account.Revision != expectedRevision || account.State == AccountStateTombstoned {
		return ErrAccountRevisionConflict
	}
	delete(repository.accounts, accountID)
	return nil
}

func (repository *credentialTestRepository) DeleteAccountWithCredentialBackup(ctx context.Context, accountID string, expectedRevision uint64, deletedAt time.Time, intent CredentialBackupDeleteIntent) error {
	account, accountErr := repository.GetAccount(ctx, accountID)
	if err := repository.DeleteAccount(ctx, accountID, expectedRevision, deletedAt); err != nil {
		return err
	}
	repository.muBackups.Lock()
	defer repository.muBackups.Unlock()
	hasAccountRow := false
	for key, state := range repository.backups {
		if state.TargetID != intent.TargetID || state.VaultID != intent.VaultID || state.AccountID != accountID {
			continue
		}
		state.State = CredentialBackupStateDeletePending
		state.Operation = CredentialBackupOperationDelete
		state.OperationID = intent.OperationID
		repository.backups[key] = state
		if state.ItemID == credentialItemAccount {
			hasAccountRow = true
		}
	}
	if hasAccountRow {
		return nil
	}
	var generation uint64 = 1
	if accountErr == nil {
		generation = account.EnvelopeGeneration
	}
	repository.backups[credentialKeyString(CredentialBackupKey{
		TargetID: intent.TargetID, VaultID: intent.VaultID, AccountID: accountID, ItemID: credentialItemAccount,
	})] = CredentialBackupState{
		TargetID:    intent.TargetID,
		VaultID:     intent.VaultID,
		AccountID:   accountID,
		ItemID:      credentialItemAccount,
		OperationID: intent.OperationID,
		Operation:   CredentialBackupOperationDelete,
		State:       CredentialBackupStateDeletePending,
		Generation:  generation,
		Revision:    1,
	}
	return nil
}

func (repository *credentialTestRepository) DeletePendingAccount(ctx context.Context, accountID string, backupGeneration uint64) error {
	if err := repository.memoryAccountRepository.DeletePendingAccount(ctx, accountID, backupGeneration); err != nil {
		return err
	}
	repository.muBackups.Lock()
	defer repository.muBackups.Unlock()
	for key, state := range repository.backups {
		if state.AccountID == accountID && state.State == CredentialBackupStatePending && state.Operation == CredentialBackupOperationUpsert {
			delete(repository.backups, key)
		}
	}
	return nil
}

func (repository *credentialTestRepository) WithAccountTransaction(ctx context.Context, operation func(AccountRepository) error) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	snapshot := make(map[string]*Account, len(repository.accounts))
	for id, account := range repository.accounts {
		snapshot[id] = cloneAccount(account)
	}
	metadata := make(map[string]string, len(repository.metadata))
	for key, value := range repository.metadata {
		metadata[key] = value
	}
	repository.muBackups.Lock()
	backupSnapshot := make(map[string]CredentialBackupState, len(repository.backups))
	for key, value := range repository.backups {
		backupSnapshot[key] = value
	}
	repository.muBackups.Unlock()
	tx := &credentialTestRepository{
		memoryAccountRepository: &memoryAccountRepository{accounts: snapshot, metadata: metadata},
		backups:                 backupSnapshot,
		failPut:                 repository.failPut,
	}
	if err := operation(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	repository.accounts = tx.accounts
	repository.metadata = tx.metadata
	repository.muBackups.Lock()
	repository.backups = tx.backups
	repository.muBackups.Unlock()
	return nil
}

func (repository *credentialTestRepository) pendingRows(t *testing.T, targetID, vaultID string) []CredentialBackupState {
	t.Helper()
	rows, err := repository.ListCredentialBackups(context.Background(), targetID, vaultID)
	if err != nil {
		t.Fatal(err)
	}
	pending := make([]CredentialBackupState, 0, len(rows))
	for _, row := range rows {
		if row.State != CredentialBackupStateSynced {
			pending = append(pending, row)
		}
	}
	return pending
}

type credentialTestFixture struct {
	vault   *WalletVault
	repo    *credentialTestRepository
	store   *keepass.Store
	service *CredentialBackupService
	binding keepass.Binding
	master  []byte
}

func newCredentialFixture(t *testing.T) *credentialTestFixture {
	t.Helper()
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	repository := newCredentialTestRepository()
	vault, err := NewWalletVault(repository, codec, VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x42}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	vaultID, err := vault.CredentialVaultID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store := keepass.NewStore(keepass.Options{})
	master := []byte(credentialTestMaster)
	binding, err := store.Create(ctx, filepath.Join(t.TempDir(), "credentials.kdbx"), vaultID, master)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewCredentialBackupService(vault, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Configure(ctx, CredentialBackupPolicy{Enabled: true, Binding: binding}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return &credentialTestFixture{vault: vault, repo: repository, store: store, service: service, binding: binding, master: master}
}

func (fixture *credentialTestFixture) begin(t *testing.T) (*CredentialBackupOperation, context.Context) {
	t.Helper()
	op, err := fixture.service.Begin(context.Background(), fixture.master)
	if err != nil {
		t.Fatal(err)
	}
	return op, op.Context()
}

func confirmChallenge(t *testing.T, vault *WalletVault, ctx context.Context, challenge BackupChallenge) AccountSummary {
	t.Helper()
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	summary, err := vault.ConfirmBackup(ctx, challenge.ChallengeID, answers)
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func createConfirmedAccount(t *testing.T, fixture *credentialTestFixture, ctx context.Context, password []byte) AccountSummary {
	t.Helper()
	_, challenge, err := fixture.vault.Create(ctx, CreateAccountRequest{Name: "managed", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	return confirmChallenge(t, fixture.vault, ctx, challenge)
}

func keystoreEntryPassword(t *testing.T, fixture *credentialTestFixture, op *CredentialBackupOperation, accountID string) []byte {
	t.Helper()
	var captured []byte
	err := op.WithAccountPassword(context.Background(), accountID, func(password []byte) error {
		captured = append([]byte(nil), password...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return captured
}

func TestCredentialBackupCreateConfirmSync(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	password := []byte("storage-password-1")
	summary, challenge, err := fixture.vault.Create(ctx, CreateAccountRequest{Name: "acct", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID)); got != 1 {
		t.Fatalf("expected one pending row, got %d", got)
	}
	listing, err := op.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 0 {
		t.Fatalf("expected empty vault before sync, got %d entries", len(listing))
	}
	activated := confirmChallenge(t, fixture.vault, ctx, challenge)
	if activated.State != AccountStateActive {
		t.Fatalf("expected active account, got %s", activated.State)
	}
	report, err := op.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.AccountsSaved != 1 || report.FilesSaved != 0 || report.Deleted != 0 || report.Pending != 0 {
		t.Fatalf("unexpected report %+v", report)
	}
	if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 0 {
		t.Fatalf("expected all rows synced, got %d pending", len(rows))
	}
	op2, _ := fixture.begin(t)
	defer op2.Close()
	if got := keystoreEntryPassword(t, fixture, op2, summary.AccountID); !bytes.Equal(got, password) {
		t.Fatalf("stored credential password mismatch")
	}
	listing, err = op2.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 1 || listing[0].Ref.ItemID != "account" || listing[0].Ref.AccountID != summary.AccountID {
		t.Fatalf("unexpected vault listing %+v", listing)
	}
}

func TestCredentialBackupRequiresManagedContext(t *testing.T) {
	fixture := newCredentialFixture(t)
	_, _, err := fixture.vault.Create(context.Background(), CreateAccountRequest{Name: "acct", Password: []byte("storage-password-1")})
	if !errors.Is(err, ErrCredentialBackupRequired) {
		t.Fatalf("expected ErrCredentialBackupRequired, got %v", err)
	}
	if accounts, err := fixture.vault.ListAccounts(context.Background()); err != nil || len(accounts) != 0 {
		t.Fatalf("expected no persisted account, got %v err %v", accounts, err)
	}
}

func TestCredentialBackupDisabledUnchanged(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	_ = repository
	service, err := NewCredentialBackupService(vault, keepass.NewStore(keepass.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	summary, challenge, err := vault.Create(context.Background(), CreateAccountRequest{Name: "acct", Password: []byte("storage-password-1")})
	if err != nil {
		t.Fatal(err)
	}
	confirmChallenge(t, vault, context.Background(), challenge)
	if summary.State != AccountStatePendingBackup {
		t.Fatalf("unexpected state %s", summary.State)
	}
	if _, err := service.Begin(context.Background(), []byte(credentialTestMaster)); !errors.Is(err, ErrCredentialBackupUnavailable) {
		t.Fatalf("expected ErrCredentialBackupUnavailable, got %v", err)
	}
}

func TestCredentialBackupBeginWrongMaster(t *testing.T) {
	fixture := newCredentialFixture(t)
	if _, err := fixture.service.Begin(context.Background(), []byte("wrong-master-password")); !errors.Is(err, keepass.ErrAuthentication) {
		t.Fatalf("expected ErrAuthentication, got %v", err)
	}
}

func TestCredentialBackupCancelDiscardsLedger(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	_, challenge, err := fixture.vault.Create(ctx, CreateAccountRequest{Name: "acct", Password: []byte("storage-password-1")})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.vault.CancelBackup(ctx, challenge.ChallengeID); err != nil {
		t.Fatal(err)
	}
	if got := len(fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID)); got != 0 {
		t.Fatalf("expected discarded ledger rows, got %d", got)
	}
	report, err := op.Sync(context.Background())
	if err != nil || report.AccountsSaved != 0 || report.Pending != 0 {
		t.Fatalf("unexpected sync %+v err %v", report, err)
	}
}

func TestCredentialBackupKeystoreImportSync(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	keystoreJSON, sourcePassword := testKeystoreFixture(t)
	sourcePath := filepath.Join(t.TempDir(), "ethers.json")
	summary, err := fixture.vault.ImportKeystore(ctx, KeystoreImportRequest{
		Name:                   "imported",
		KeystoreJSON:           keystoreJSON,
		SourcePassword:         sourcePassword,
		SourcePath:             sourcePath,
		StoragePassword:        []byte("storage-password-1"),
		ConfirmStoragePassword: []byte("storage-password-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	pending := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID)
	if len(pending) != 2 {
		t.Fatalf("expected account+file rows, got %d", len(pending))
	}
	report, err := op.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.AccountsSaved != 1 || report.FilesSaved != 1 || report.Pending != 0 {
		t.Fatalf("unexpected report %+v", report)
	}
	op2, _ := fixture.begin(t)
	defer op2.Close()
	var captured []byte
	err = op2.WithFilePassword(context.Background(), credentialKindKeystoreV3, keystoreJSON, func(password []byte) error {
		captured = append([]byte(nil), password...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(captured, sourcePassword) {
		t.Fatalf("file password mismatch")
	}
	account, err := fixture.vault.repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil || account.State != AccountStateActive {
		t.Fatalf("unexpected account state %v", account)
	}
}

func TestCredentialBackupRotatePassword(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	oldPassword := []byte("storage-password-1")
	newPassword := []byte("storage-password-2")
	summary := createConfirmedAccount(t, fixture, ctx, oldPassword)
	if report, err := op.Sync(context.Background()); err != nil || report.AccountsSaved != 1 {
		t.Fatalf("sync failed %+v %v", report, err)
	}
	op2, ctx2 := fixture.begin(t)
	defer op2.Close()
	if err := fixture.vault.RotatePassword(ctx2, summary.AccountID, oldPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	report, err := op2.Sync(context.Background())
	if err != nil || report.AccountsSaved != 1 {
		t.Fatalf("sync failed %+v %v", report, err)
	}
	op3, _ := fixture.begin(t)
	defer op3.Close()
	if got := keystoreEntryPassword(t, fixture, op3, summary.AccountID); !bytes.Equal(got, newPassword) {
		t.Fatalf("rotated password mismatch")
	}
}

func TestCredentialBackupDeleteRemoval(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	password := []byte("storage-password-1")
	summary := createConfirmedAccount(t, fixture, ctx, password)
	if report, err := op.Sync(context.Background()); err != nil || report.AccountsSaved != 1 {
		t.Fatalf("sync failed %+v %v", report, err)
	}
	op2, ctx2 := fixture.begin(t)
	request := DeleteAccountRequest{
		AccountID:              summary.AccountID,
		ConfirmAccountID:       summary.AccountID,
		Password:               password,
		RemoveCredentialBackup: true,
		ConfirmBackupAccountID: summary.AccountID,
	}
	if err := fixture.vault.DeleteAccount(ctx2, request); err != nil {
		t.Fatal(err)
	}
	report, err := op2.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Deleted != 1 {
		t.Fatalf("expected one deletion, got %+v", report)
	}
	op3, _ := fixture.begin(t)
	defer op3.Close()
	listing, err := op3.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 0 {
		t.Fatalf("expected empty vault after deletion, got %d entries", len(listing))
	}
}

func TestCredentialBackupDefaultDeletePreservesVault(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	password := []byte("storage-password-1")
	summary := createConfirmedAccount(t, fixture, ctx, password)
	if report, err := op.Sync(context.Background()); err != nil || report.AccountsSaved != 1 {
		t.Fatalf("sync failed %+v %v", report, err)
	}
	request := DeleteAccountRequest{
		AccountID:        summary.AccountID,
		ConfirmAccountID: summary.AccountID,
		Password:         password,
	}
	if err := fixture.vault.DeleteAccount(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	op2, _ := fixture.begin(t)
	defer op2.Close()
	listing, err := op2.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 1 {
		t.Fatalf("expected vault entry preserved, got %d", len(listing))
	}
}

func TestCredentialBackupLedgerFailureRollsBackAccount(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	defer op.Close()
	fixture.repo.failPut.Store(1)
	_, _, err := fixture.vault.Create(ctx, CreateAccountRequest{Name: "acct", Password: []byte("storage-password-1")})
	if err == nil {
		t.Fatal("expected create to fail")
	}
	if accounts, listErr := fixture.vault.ListAccounts(context.Background()); listErr != nil || len(accounts) != 0 {
		t.Fatalf("expected rollback without account, got %v err %v", accounts, listErr)
	}
	if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 0 {
		t.Fatalf("expected no ledger rows, got %d", len(rows))
	}
}

func TestCredentialBackupQueueOnClosedOperation(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, _ := fixture.begin(t)
	account := &Account{AccountID: "00000000-0000-4000-8000-000000000001", EnvelopeGeneration: 1, Revision: 1, AuthorizationEpoch: 1}
	op.Close()
	err := queueCommittedAccountCredential(op, account, []byte("password"), nil)
	var pendingErr *CredentialBackupPendingError
	if !errors.As(err, &pendingErr) {
		t.Fatalf("expected CredentialBackupPendingError, got %v", err)
	}
}

func TestCredentialBackupBatchImport(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	keystoreJSON, sourcePassword := testKeystoreFixture(t)
	results := fixture.vault.ImportKeystoreBatch(ctx, KeystoreBatchImportRequest{
		Items: []KeystoreBatchItem{
			{Name: "one", KeystoreJSON: keystoreJSON, SourcePassword: sourcePassword},
			{Name: "two", KeystoreJSON: keystoreJSON, SourcePassword: []byte("wrong-password")},
		},
		StoragePassword:        []byte("storage-password-1"),
		ConfirmStoragePassword: []byte("storage-password-1"),
	})
	if len(results) != 2 {
		t.Fatalf("expected two results, got %d", len(results))
	}
	imported, failed := 0, 0
	for _, result := range results {
		if result.Err != nil {
			failed++
			continue
		}
		imported++
	}
	if imported != 1 || failed != 1 {
		t.Fatalf("expected 1 import 1 failure, got %d/%d (%+v)", imported, failed, results)
	}
	report, err := op.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.AccountsSaved != 1 || report.FilesSaved != 1 {
		t.Fatalf("unexpected report %+v", report)
	}
}

func TestCredentialBackupSyncInflightBusy(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	_ = ctx
	op.mu.Lock()
	op.inflight++
	op.mu.Unlock()
	if _, err := op.Sync(context.Background()); !errors.Is(err, keepass.ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
	op.mu.Lock()
	op.inflight--
	op.mu.Unlock()
	op.Close()
}

func TestCredentialBackupPendingErrorString(t *testing.T) {
	err := &CredentialBackupPendingError{Cause: keepass.ErrBusy}
	if !strings.Contains(err.Error(), "pending") || !errors.Is(err, keepass.ErrBusy) {
		t.Fatalf("unexpected pending error %v", err)
	}
	var artifact credentialArtifact
	if serialized, marshalErr := artifact.MarshalJSON(); marshalErr == nil || serialized != nil {
		t.Fatalf("artifact serialization must fail")
	}
}

func readCredentialVaultValues(t *testing.T, fixture *credentialTestFixture, accountID, itemID string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(fixture.binding.Path)
	if err != nil {
		t.Fatal(err)
	}
	decoded := kp.NewDatabase()
	decoded.Credentials = kp.NewPasswordCredentials(credentialTestMaster)
	if err := kp.NewDecoderWithLimits(bytes.NewReader(raw), kp.DefaultDecodeLimits()).Decode(decoded); err != nil {
		t.Fatalf("vault decode failed: %v", err)
	}
	if err := decoded.UnlockProtectedEntries(); err != nil {
		t.Fatalf("unlock protected entries: %v", err)
	}
	defer func() {
		if err := decoded.LockProtectedEntries(); err != nil {
			t.Errorf("lock protected entries: %v", err)
		}
	}()
	values := make(map[string]string)
	var walk func(groups []kp.Group)
	walk = func(groups []kp.Group) {
		for groupIndex := range groups {
			for entryIndex := range groups[groupIndex].Entries {
				entry := &groups[groupIndex].Entries[entryIndex]
				if entry.GetContent("Bloco.AccountID") != accountID || entry.GetContent("Bloco.ItemID") != itemID {
					continue
				}
				for _, value := range entry.Values {
					values[value.Key] = value.Value.Content
				}
			}
			walk(groups[groupIndex].Groups)
		}
	}
	walk(decoded.Content.Root.Groups)
	return values
}

func TestCredentialBackupMnemonicExportFields(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	mnemonic := testBatchMnemonicJunk
	passphrase := "custom bip39 passphrase"
	path := "m/44'/60'/2'/0/0"
	storagePassword := []byte("storage-password-1")
	summary, err := fixture.vault.ImportMnemonic(ctx, MnemonicImportRequest{
		Name:                   "mnemonic",
		Mnemonic:               mnemonic,
		BIP39Passphrase:        passphrase,
		BIP39Language:          BIP39English,
		DerivationPath:         path,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := op.Sync(context.Background())
	if err != nil || report.AccountsSaved != 1 {
		t.Fatalf("sync failed %+v %v", report, err)
	}
	parsedPath, err := ParseDerivationPath(path)
	if err != nil {
		t.Fatal(err)
	}
	expectedKey, expectedAddress, _, err := deriveEVMAccount(mnemonic, passphrase, BIP39English, parsedPath)
	if err != nil {
		t.Fatal(err)
	}
	values := readCredentialVaultValues(t, fixture, summary.AccountID, "account")
	if len(values) == 0 {
		t.Fatal("account entry missing from credential vault")
	}
	if values["Bloco.Mnemonic"] != normalizedMnemonic(mnemonic) {
		t.Fatalf("mnemonic mismatch")
	}
	if values["Bloco.BIP39Passphrase"] != passphrase {
		t.Fatalf("passphrase mismatch")
	}
	if values["Bloco.PrivateKey"] != "0x"+hex.EncodeToString(expectedKey) {
		t.Fatalf("private key mismatch")
	}
	if values["Password"] != string(storagePassword) {
		t.Fatalf("password mismatch")
	}
	if !strings.EqualFold(values["UserName"], summary.Address) {
		t.Fatalf("address mismatch: vault %q account %q expected %q", values["UserName"], summary.Address, expectedAddress)
	}
	clear(expectedKey)
}

func TestCredentialBackupRevokedCapabilityPending(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	summary := createConfirmedAccount(t, fixture, ctx, []byte("storage-password-1"))
	account, err := fixture.repo.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Capabilities &^= CapabilityExportSecret
	if err := fixture.repo.UpdateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	report, syncErr := op.Sync(context.Background())
	var pendingErr *CredentialBackupPendingError
	if !errors.As(syncErr, &pendingErr) {
		t.Fatalf("expected CredentialBackupPendingError, got %v", syncErr)
	}
	if report.Pending == 0 || report.AccountsSaved != 0 {
		t.Fatalf("unexpected report %+v", report)
	}
	op2, _ := fixture.begin(t)
	defer op2.Close()
	listing, err := op2.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 0 {
		t.Fatalf("expected nothing written, got %d entries", len(listing))
	}
	if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 1 {
		t.Fatalf("expected ledger still pending, got %d rows", len(rows))
	}
}

func TestCredentialBackupForeignPendingDeletePreserved(t *testing.T) {
	fixture := newCredentialFixture(t)
	op1, ctx1 := fixture.begin(t)
	password := []byte("storage-password-1")
	summaryA := createConfirmedAccount(t, fixture, ctx1, password)
	if _, err := op1.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	op2, ctx2 := fixture.begin(t)
	if err := fixture.vault.DeleteAccount(ctx2, DeleteAccountRequest{
		AccountID:              summaryA.AccountID,
		ConfirmAccountID:       summaryA.AccountID,
		Password:               password,
		RemoveCredentialBackup: true,
		ConfirmBackupAccountID: summaryA.AccountID,
	}); err != nil {
		t.Fatal(err)
	}
	op2.Close()
	op3, ctx3 := fixture.begin(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(key))
	storagePassword := []byte("storage-password-2")
	summaryB, err := fixture.vault.ImportPrivateKey(ctx3, PrivateKeyImportRequest{
		Name:                   "unrelated",
		PrivateKey:             privateKeyHex,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := op3.Sync(context.Background())
	if err != nil {
		t.Fatalf("unrelated import sync failed %+v %v", report, err)
	}
	if report.AccountsSaved != 1 || report.Deleted != 0 || report.Pending != 0 {
		t.Fatalf("unexpected report %+v", report)
	}
	op4, _ := fixture.begin(t)
	listing, err := op4.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 2 {
		t.Fatalf("old pending backup must be preserved, got %d entries", len(listing))
	}
	deleted := 0
	for _, row := range fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID) {
		if row.AccountID == summaryA.AccountID && row.State == CredentialBackupStateDeletePending {
			deleted++
		}
	}
	if deleted == 0 {
		t.Fatal("pending delete rows were not preserved")
	}
	if err := op4.RetryDeletion(context.Background(), summaryA.AccountID, summaryA.AccountID); err != nil {
		t.Fatal(err)
	}
	report, err = op4.Sync(context.Background())
	if err != nil || report.Deleted != 1 {
		t.Fatalf("retry deletion failed %+v %v", report, err)
	}
	op5, _ := fixture.begin(t)
	defer op5.Close()
	listing, err = op5.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing) != 1 || listing[0].Ref.AccountID != summaryB.AccountID {
		t.Fatalf("expected only the imported account entry, got %+v", listing)
	}
}

func TestCredentialBackupSupersededIntentNotAcked(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	summary := createConfirmedAccount(t, fixture, ctx, []byte("storage-password-1"))
	key := CredentialBackupKey{
		TargetID: fixture.binding.TargetID, VaultID: fixture.binding.VaultID,
		AccountID: summary.AccountID, ItemID: credentialItemAccount,
	}
	row, err := fixture.repo.GetCredentialBackup(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	replaced := row
	replaced.OperationID = "11111111-2222-4333-8444-555555555555"
	if err := fixture.repo.PutCredentialBackup(context.Background(), replaced, row.Revision); err != nil {
		t.Fatal(err)
	}
	report, syncErr := op.Sync(context.Background())
	var pendingErr *CredentialBackupPendingError
	if !errors.As(syncErr, &pendingErr) {
		t.Fatalf("expected CredentialBackupPendingError, got %v", syncErr)
	}
	if report.AccountsSaved != 0 {
		t.Fatalf("superseded intent must not be acked, got %+v", report)
	}
	row, err = fixture.repo.GetCredentialBackup(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != CredentialBackupStatePending || row.OperationID != replaced.OperationID {
		t.Fatalf("ledger row was wrongly acked: %+v", row)
	}
	op2, _ := fixture.begin(t)
	summary2 := createConfirmedAccount(t, fixture, op2.Context(), []byte("storage-password-1"))
	key2 := CredentialBackupKey{
		TargetID: fixture.binding.TargetID, VaultID: fixture.binding.VaultID,
		AccountID: summary2.AccountID, ItemID: credentialItemAccount,
	}
	row2, err := fixture.repo.GetCredentialBackup(context.Background(), key2)
	if err != nil {
		t.Fatal(err)
	}
	bumped := row2
	bumped.Generation++
	if err := fixture.repo.PutCredentialBackup(context.Background(), bumped, row2.Revision); err != nil {
		t.Fatal(err)
	}
	report2, syncErr2 := op2.Sync(context.Background())
	if !errors.As(syncErr2, &pendingErr) {
		t.Fatalf("expected CredentialBackupPendingError for generation mismatch, got %v", syncErr2)
	}
	if report2.AccountsSaved != 0 {
		t.Fatalf("generation-mismatched intent must not be acked, got %+v", report2)
	}
	row2, err = fixture.repo.GetCredentialBackup(context.Background(), key2)
	if err != nil {
		t.Fatal(err)
	}
	if row2.State != CredentialBackupStatePending || row2.Generation != bumped.Generation {
		t.Fatalf("generation-bumped row was wrongly acked: %+v", row2)
	}
}

func TestCredentialBackupStoreCommitFailureRetry(t *testing.T) {
	fixture := newCredentialFixture(t)
	op1, ctx1 := fixture.begin(t)
	password := []byte("storage-password-1")
	summary := createConfirmedAccount(t, fixture, ctx1, password)
	moved := fixture.binding.Path + ".moved"
	if err := os.Rename(fixture.binding.Path, moved); err != nil {
		t.Fatal(err)
	}
	report, syncErr := op1.Sync(context.Background())
	if syncErr == nil {
		t.Fatal("expected sync failure against missing vault file")
	}
	if report.AccountsSaved != 0 {
		t.Fatalf("unexpected report %+v", report)
	}
	if _, err := fixture.vault.repository.GetAccount(context.Background(), summary.AccountID); err != nil {
		t.Fatalf("account must survive backup failure: %v", err)
	}
	if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 1 {
		t.Fatalf("expected pending ledger row, got %d", len(rows))
	}
	if err := os.Rename(moved, fixture.binding.Path); err != nil {
		t.Fatal(err)
	}
	op2, _ := fixture.begin(t)
	if err := op2.BackupAccount(context.Background(), summary.AccountID, password); err != nil {
		t.Fatal(err)
	}
	report, err := op2.Sync(context.Background())
	if err != nil || report.AccountsSaved != 1 {
		t.Fatalf("retry sync failed %+v %v", report, err)
	}
}

func TestCredentialBackupRetryArtifact(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	keystoreJSON, sourcePassword := testKeystoreFixture(t)
	sourcePath := filepath.Join(t.TempDir(), "ethers.json")
	summary, err := fixture.vault.ImportKeystore(ctx, KeystoreImportRequest{
		Name:                   "imported",
		KeystoreJSON:           keystoreJSON,
		SourcePassword:         sourcePassword,
		SourcePath:             sourcePath,
		StoragePassword:        []byte("storage-password-1"),
		ConfirmStoragePassword: []byte("storage-password-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var fileKey CredentialBackupKey
	for _, row := range fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID) {
		if row.AccountID == summary.AccountID && strings.HasPrefix(row.ItemID, "file:") {
			fileKey = CredentialBackupKey{TargetID: row.TargetID, VaultID: row.VaultID, AccountID: row.AccountID, ItemID: row.ItemID}
		}
	}
	if fileKey.ItemID == "" {
		t.Fatal("file ledger row missing")
	}
	if err := op.RetryArtifact(context.Background(), fileKey, keystoreJSON, []byte("wrong-source-password"), ""); err == nil {
		t.Fatal("wrong artifact password was accepted")
	}
	row, err := fixture.repo.GetCredentialBackup(context.Background(), fileKey)
	if err != nil {
		t.Fatal(err)
	}
	prepared := row
	prepared.State = CredentialBackupStatePrepared
	if err := fixture.repo.PutCredentialBackup(context.Background(), prepared, row.Revision); err != nil {
		t.Fatal(err)
	}
	if err := op.RetryArtifact(context.Background(), fileKey, keystoreJSON, sourcePassword, sourcePath); err != nil {
		t.Fatal(err)
	}
	row, err = fixture.repo.GetCredentialBackup(context.Background(), fileKey)
	if err != nil {
		t.Fatal(err)
	}
	if row.State != CredentialBackupStatePending || row.OperationID != op.id {
		t.Fatalf("prepared row must transition to pending under this operation: %+v", row)
	}
	report, err := op.Sync(context.Background())
	if err != nil || report.FilesSaved != 1 || report.AccountsSaved != 1 {
		t.Fatalf("sync failed %+v %v", report, err)
	}
}

func TestCredentialBackupEncryptedArtifacts(t *testing.T) {
	sourceVault, _, _ := newTestVault(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKeyHex := hex.EncodeToString(crypto.FromECDSA(key))
	storagePassword := []byte("storage-password-1")
	sourceSummary, err := sourceVault.ImportPrivateKey(context.Background(), PrivateKeyImportRequest{
		Name:                   "source",
		PrivateKey:             privateKeyHex,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceHandle, err := sourceVault.Unlock(context.Background(), sourceSummary.AccountID, storagePassword)
	if err != nil {
		t.Fatal(err)
	}
	exportPassword := []byte("export-password-distinct-1")
	exportPath := filepath.Join(t.TempDir(), "export.bloco")
	if err := sourceVault.ExportEncryptedAccount(context.Background(), EncryptedAccountExportRequest{
		Handle:             sourceHandle,
		Destination:        exportPath,
		CurrentPassword:    storagePassword,
		NewPassword:        exportPassword,
		ConfirmNewPassword: append([]byte(nil), exportPassword...),
	}); err != nil {
		t.Fatal(err)
	}
	exportJSON, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	managedPassword := []byte("storage-password-1")
	summary := createConfirmedAccount(t, fixture, ctx, managedPassword)
	handle, err := fixture.vault.Unlock(ctx, summary.AccountID, managedPassword)
	if err != nil {
		t.Fatal(err)
	}
	keystoreExportPassword := []byte("keystore-export-2")
	keystoreDestination := filepath.Join(t.TempDir(), "acct.json")
	if err := fixture.vault.ExportKeystoreV3(ctx, KeystoreV3ExportRequest{
		Handle:          handle,
		Destination:     keystoreDestination,
		Password:        keystoreExportPassword,
		ConfirmPassword: append([]byte(nil), keystoreExportPassword...),
	}); err != nil {
		t.Fatal(err)
	}
	encryptedExportPassword := []byte("encrypted-export-3")
	encryptedDestination := filepath.Join(t.TempDir(), "acct.bloco")
	if err := fixture.vault.ExportEncryptedAccount(ctx, EncryptedAccountExportRequest{
		Handle:             handle,
		Destination:        encryptedDestination,
		CurrentPassword:    managedPassword,
		NewPassword:        encryptedExportPassword,
		ConfirmNewPassword: append([]byte(nil), encryptedExportPassword...),
	}); err != nil {
		t.Fatal(err)
	}
	importSummary, err := fixture.vault.ImportEncryptedAccount(ctx, EncryptedAccountImportRequest{
		Name:                   "imported-encrypted",
		ExportJSON:             exportJSON,
		ExportPassword:         exportPassword,
		SourcePath:             exportPath,
		StoragePassword:        managedPassword,
		ConfirmStoragePassword: append([]byte(nil), managedPassword...),
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := op.Sync(context.Background())
	if err != nil {
		t.Fatalf("sync failed %+v %v", report, err)
	}
	if report.AccountsSaved != 2 || report.FilesSaved != 3 {
		t.Fatalf("unexpected report %+v", report)
	}
	op2, _ := fixture.begin(t)
	defer op2.Close()
	keystoreBytes, err := os.ReadFile(keystoreDestination)
	if err != nil {
		t.Fatal(err)
	}
	var captured []byte
	if err := op2.WithFilePassword(context.Background(), credentialKindKeystoreV3, keystoreBytes, func(password []byte) error {
		captured = append([]byte(nil), password...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(captured, keystoreExportPassword) {
		t.Fatal("keystore export password mismatch")
	}
	captured = nil
	encryptedBytes, err := os.ReadFile(encryptedDestination)
	if err != nil {
		t.Fatal(err)
	}
	if err := op2.WithFilePassword(context.Background(), credentialKindEncryptedFile, encryptedBytes, func(password []byte) error {
		captured = append([]byte(nil), password...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(captured, encryptedExportPassword) {
		t.Fatal("encrypted export password mismatch")
	}
	captured = nil
	if err := op2.WithFilePassword(context.Background(), credentialKindEncryptedFile, exportJSON, func(password []byte) error {
		captured = append([]byte(nil), password...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(captured, exportPassword) {
		t.Fatal("imported source password mismatch")
	}
	_ = importSummary
}

func TestCredentialBackupPreparedReported(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	password := []byte("storage-password-1")
	summary := createConfirmedAccount(t, fixture, ctx, password)
	handle, err := fixture.vault.Unlock(ctx, summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "missing", "acct.json")
	exportPassword := []byte("keystore-export-2")
	if err := fixture.vault.ExportKeystoreV3(ctx, KeystoreV3ExportRequest{
		Handle:          handle,
		Destination:     destination,
		Password:        exportPassword,
		ConfirmPassword: append([]byte(nil), exportPassword...),
	}); err == nil {
		t.Fatal("export to missing directory must fail")
	}
	report, syncErr := op.Sync(context.Background())
	var pendingErr *CredentialBackupPendingError
	if !errors.As(syncErr, &pendingErr) {
		t.Fatalf("expected CredentialBackupPendingError, got %v", syncErr)
	}
	prepared := 0
	for _, failure := range report.Failures {
		if failure.Code == "prepared" {
			prepared++
		}
	}
	if prepared != 1 {
		t.Fatalf("expected one prepared failure, got %+v", report)
	}
}

func TestCredentialBackupWithAccountPasswordStaleEntry(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	oldPassword := []byte("storage-password-1")
	summary := createConfirmedAccount(t, fixture, ctx, oldPassword)
	if _, err := op.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	op2, ctx2 := fixture.begin(t)
	if err := fixture.vault.RotatePassword(ctx2, summary.AccountID, oldPassword, []byte("storage-password-2")); err != nil {
		t.Fatal(err)
	}
	op2.Close()
	op3, _ := fixture.begin(t)
	defer op3.Close()
	called := false
	err := op3.WithAccountPassword(context.Background(), summary.AccountID, func([]byte) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected stale stored password to fail verification")
	}
	if called {
		t.Fatal("callback must not run for a stale stored password")
	}
}

func TestCredentialBackupConcurrentLifecycle(t *testing.T) {
	fixture := newCredentialFixture(t)
	op, ctx := fixture.begin(t)
	var group sync.WaitGroup
	group.Add(3)
	go func() {
		defer group.Done()
		_, _, _ = fixture.vault.Create(ctx, CreateAccountRequest{Name: "racer", Password: []byte("storage-password-1")})
	}()
	go func() {
		defer group.Done()
		_, _ = op.Sync(context.Background())
	}()
	go func() {
		defer group.Done()
		op.Close()
	}()
	group.Wait()
}

func TestCredentialBackupCanceledRootClosesOperation(t *testing.T) {
	fixture := newCredentialFixture(t)
	parent, cancel := context.WithCancel(context.Background())
	op, err := fixture.service.Begin(parent, fixture.master)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		op.mu.Lock()
		closed := op.closed
		op.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("operation was not closed after root cancellation")
		}
		time.Sleep(time.Millisecond)
	}
	if err := op.WithFilePassword(context.Background(), credentialKindKeystoreV3, []byte("data"), func([]byte) error { return nil }); err == nil {
		t.Fatal("closed operation must reject use")
	}
}

func TestCredentialBackupStatus(t *testing.T) {
	fixture := newCredentialFixture(t)
	_, ctx := fixture.begin(t)
	summary, _, err := fixture.vault.Create(ctx, CreateAccountRequest{Name: "acct", Password: []byte("storage-password-1")})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := fixture.service.Status(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].AccountID != summary.AccountID || rows[0].ItemID != "account" {
		t.Fatalf("unexpected status rows %+v", rows)
	}
	other := "00000000-0000-4000-8000-000000000000"
	rows, err = fixture.service.Status(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no rows for unrelated account, got %+v", rows)
	}
	if _, err := fixture.service.Status(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("invalid account id must be rejected")
	}
	plainVault, _, _ := newTestVault(t)
	disabled, err := NewCredentialBackupService(plainVault, keepass.NewStore(keepass.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	if _, err := disabled.Status(context.Background(), summary.AccountID); !errors.Is(err, ErrCredentialBackupUnavailable) {
		t.Fatalf("expected ErrCredentialBackupUnavailable, got %v", err)
	}
}

func TestCredentialBackupServiceGuards(t *testing.T) {
	fixture := newCredentialFixture(t)
	if _, err := NewCredentialBackupService(fixture.vault, fixture.store); err == nil {
		t.Fatal("second live service attachment must be rejected")
	}
	fixture.service.Close()
	if err := fixture.service.Configure(context.Background(), CredentialBackupPolicy{Enabled: true, Binding: fixture.binding}); !errors.Is(err, ErrCredentialBackupUnavailable) {
		t.Fatalf("expected ErrCredentialBackupUnavailable, got %v", err)
	}
	if _, err := fixture.service.Begin(context.Background(), fixture.master); !errors.Is(err, ErrCredentialBackupUnavailable) {
		t.Fatalf("expected ErrCredentialBackupUnavailable, got %v", err)
	}
}

func TestCredentialBackupFreshConfirmationRequiresQueuedAccount(t *testing.T) {
	fixture := newCredentialFixture(t)
	password := []byte("storage-password-1")
	op1, ctx1 := fixture.begin(t)
	summary, challenge, err := fixture.vault.Create(ctx1, CreateAccountRequest{Name: "acct", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	op1.Close()

	op2, ctx2 := fixture.begin(t)
	defer op2.Close()
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	if _, err := fixture.vault.ConfirmBackup(ctx2, challenge.ChallengeID, answers); err == nil {
		t.Fatal("confirmation without a queued credential must fail")
	}
	account, err := fixture.repo.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.State != AccountStatePendingBackup {
		t.Fatalf("account must remain pending, got %s", account.State)
	}
	if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 1 {
		t.Fatalf("expected the queued ledger row preserved, got %d pending", len(rows))
	}
	if _, _, err := fixture.vault.ResumeBackup(ctx2, summary.AccountID, []byte("wrong-storage-password")); err == nil {
		t.Fatal("resume with a wrong password must fail")
	}
	account, err = fixture.repo.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.State != AccountStatePendingBackup {
		t.Fatalf("wrong-password resume must not activate, got %s", account.State)
	}
	if err := fixture.vault.SuspendBackup(challenge.ChallengeID); err != nil {
		t.Fatal(err)
	}
	resumed, challenge2, err := fixture.vault.ResumeBackup(ctx2, summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.AccountID != summary.AccountID {
		t.Fatalf("resumed wrong account %+v", resumed)
	}
	activated := confirmChallenge(t, fixture.vault, ctx2, challenge2)
	if activated.AccountID != summary.AccountID || activated.State != AccountStateActive {
		t.Fatalf("unexpected activation %+v", activated)
	}
	report, err := op2.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.AccountsSaved != 1 || report.Pending != 0 {
		t.Fatalf("unexpected report %+v", report)
	}
	if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 0 {
		t.Fatalf("expected all rows synced, got %d pending", len(rows))
	}
	op3, _ := fixture.begin(t)
	defer op3.Close()
	if got := keystoreEntryPassword(t, fixture, op3, summary.AccountID); !bytes.Equal(got, password) {
		t.Fatal("stored credential password mismatch")
	}
}

type committedWarningStore struct {
	credentialStoreOperation
	cause        error
	onUpsert     bool
	onRemove     bool
	upsertCalls  int
	lastRecordLn int
}

func (store *committedWarningStore) Upsert(ctx context.Context, records []keepass.Record) (keepass.CommitResult, error) {
	store.upsertCalls++
	store.lastRecordLn = len(records)
	result, err := store.credentialStoreOperation.Upsert(ctx, records)
	if err == nil && result.Committed && store.onUpsert {
		return result, &keepass.CommittedWarning{Cause: store.cause}
	}
	return result, err
}

func (store *committedWarningStore) RemoveAccount(ctx context.Context, accountID string) (keepass.CommitResult, error) {
	result, err := store.credentialStoreOperation.RemoveAccount(ctx, accountID)
	if err == nil && result.Committed && store.onRemove {
		return result, &keepass.CommittedWarning{Cause: store.cause}
	}
	return result, err
}

func installWarningStore(op *CredentialBackupOperation, store *committedWarningStore) {
	op.mu.Lock()
	defer op.mu.Unlock()
	store.credentialStoreOperation = op.store
	op.store = store
}

func TestCredentialBackupCommittedWarningsRemainRecoverable(t *testing.T) {
	sentinel := errors.New("synthetic durability detail")

	t.Run("upsert", func(t *testing.T) {
		fixture := newCredentialFixture(t)
		op1, ctx1 := fixture.begin(t)
		summaryA := createConfirmedAccount(t, fixture, ctx1, []byte("storage-password-1"))
		if _, err := op1.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}

		op2, ctx2 := fixture.begin(t)
		passwordB := []byte("storage-password-2")
		summaryB := createConfirmedAccount(t, fixture, ctx2, passwordB)
		wrapper := &committedWarningStore{cause: sentinel, onUpsert: true}
		installWarningStore(op2, wrapper)
		report, syncErr := op2.Sync(context.Background())
		var pendingErr *CredentialBackupPendingError
		if !errors.As(syncErr, &pendingErr) {
			t.Fatalf("expected CredentialBackupPendingError, got %v", syncErr)
		}
		var warning *keepass.CommittedWarning
		if !errors.As(syncErr, &warning) {
			t.Fatalf("expected CommittedWarning, got %v", syncErr)
		}
		code := ""
		for _, failure := range report.Failures {
			if failure.AccountID == summaryB.AccountID {
				code = failure.Code
			}
		}
		if code != "durability_unconfirmed" {
			t.Fatalf("expected durability_unconfirmed, got %q (%+v)", code, report.Failures)
		}
		if len(report.BackupPaths) == 0 {
			t.Fatal("committed backup path must be preserved in the report")
		}
		if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 1 || rows[0].AccountID != summaryB.AccountID {
			t.Fatalf("ledger must retain the pending row, got %+v", rows)
		}

		inspect, _ := fixture.begin(t)
		listing, err := inspect.store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		installed := 0
		for _, meta := range listing {
			if meta.Ref.AccountID == summaryB.AccountID {
				installed++
			}
		}
		if installed != 1 {
			t.Fatalf("upserted entry must exist despite warning, got %+v", listing)
		}
		inspect.Close()

		op3, _ := fixture.begin(t)
		if err := op3.BackupAccount(context.Background(), summaryB.AccountID, passwordB); err != nil {
			t.Fatal(err)
		}
		report3, err := op3.Sync(context.Background())
		if err != nil {
			t.Fatalf("retry sync must succeed, got %+v %v", report3, err)
		}
		if report3.AccountsSaved != 1 || report3.Pending != 0 {
			t.Fatalf("unexpected retry report %+v", report3)
		}
		op4, _ := fixture.begin(t)
		defer op4.Close()
		listing, err = op4.store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(listing) != 2 {
			t.Fatalf("retry must not duplicate or orphan entries, got %+v", listing)
		}
		for _, meta := range listing {
			if meta.Ref.AccountID != summaryA.AccountID && meta.Ref.AccountID != summaryB.AccountID {
				t.Fatalf("unexpected vault entry %+v", meta)
			}
		}
	})

	t.Run("delete", func(t *testing.T) {
		fixture := newCredentialFixture(t)
		op1, ctx1 := fixture.begin(t)
		password := []byte("storage-password-1")
		summary := createConfirmedAccount(t, fixture, ctx1, password)
		if _, err := op1.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}

		op2, ctx2 := fixture.begin(t)
		if err := fixture.vault.DeleteAccount(ctx2, DeleteAccountRequest{
			AccountID:              summary.AccountID,
			ConfirmAccountID:       summary.AccountID,
			Password:               password,
			RemoveCredentialBackup: true,
			ConfirmBackupAccountID: summary.AccountID,
		}); err != nil {
			t.Fatal(err)
		}
		wrapper := &committedWarningStore{cause: sentinel, onRemove: true}
		installWarningStore(op2, wrapper)
		report, syncErr := op2.Sync(context.Background())
		var pendingErr *CredentialBackupPendingError
		if !errors.As(syncErr, &pendingErr) {
			t.Fatalf("expected CredentialBackupPendingError, got %v", syncErr)
		}
		var warning *keepass.CommittedWarning
		if !errors.As(syncErr, &warning) {
			t.Fatalf("expected CommittedWarning, got %v", syncErr)
		}
		code := ""
		for _, failure := range report.Failures {
			if failure.AccountID == summary.AccountID {
				code = failure.Code
			}
		}
		if code != "durability_unconfirmed" {
			t.Fatalf("expected durability_unconfirmed, got %q (%+v)", code, report.Failures)
		}
		if len(report.BackupPaths) == 0 {
			t.Fatal("committed backup path must be preserved in the report")
		}
		pending := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID)
		if len(pending) == 0 {
			t.Fatal("ledger must retain the delete_pending rows")
		}
		for _, row := range pending {
			if row.State != CredentialBackupStateDeletePending {
				t.Fatalf("expected delete_pending row, got %+v", row)
			}
		}

		inspect, _ := fixture.begin(t)
		listing, err := inspect.store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, meta := range listing {
			if meta.Ref.AccountID == summary.AccountID {
				t.Fatalf("deleted entry must be absent despite warning, got %+v", listing)
			}
		}
		inspect.Close()

		opRetry, _ := fixture.begin(t)
		retryWrapper := &committedWarningStore{cause: sentinel, onUpsert: true}
		installWarningStore(opRetry, retryWrapper)
		if err := opRetry.RetryDeletion(context.Background(), summary.AccountID, summary.AccountID); err != nil {
			t.Fatal(err)
		}
		reportRetry, retryErr := opRetry.Sync(context.Background())
		if !errors.As(retryErr, &pendingErr) {
			t.Fatalf("unconfirmed durable rewrite must stay pending, got %v", retryErr)
		}
		if !errors.As(retryErr, &warning) {
			t.Fatalf("expected CommittedWarning from the rewrite, got %v", retryErr)
		}
		if retryWrapper.upsertCalls == 0 || retryWrapper.lastRecordLn != 0 {
			t.Fatalf("not-found removal must trigger an empty durable rewrite, calls=%d records=%d", retryWrapper.upsertCalls, retryWrapper.lastRecordLn)
		}
		if reportRetry.Pending == 0 || reportRetry.Deleted != 0 {
			t.Fatalf("unconfirmed rewrite must not be acked, got %+v", reportRetry)
		}
		if len(reportRetry.BackupPaths) == 0 {
			t.Fatal("rewrite backup path must be preserved in the report")
		}
		stillPending := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID)
		if len(stillPending) == 0 {
			t.Fatal("ledger must retain the delete_pending rows after unconfirmed rewrite")
		}
		for _, row := range stillPending {
			if row.State != CredentialBackupStateDeletePending {
				t.Fatalf("expected delete_pending row, got %+v", row)
			}
		}

		op3, _ := fixture.begin(t)
		if err := op3.RetryDeletion(context.Background(), summary.AccountID, summary.AccountID); err != nil {
			t.Fatal(err)
		}
		report3, err := op3.Sync(context.Background())
		if err != nil {
			t.Fatalf("retry sync must succeed, got %+v %v", report3, err)
		}
		if report3.Deleted != 1 || report3.Pending != 0 {
			t.Fatalf("unexpected retry report %+v", report3)
		}
		if rows := fixture.repo.pendingRows(t, fixture.binding.TargetID, fixture.binding.VaultID); len(rows) != 0 {
			t.Fatalf("expected ledger acked, got %+v", rows)
		}
		op4, _ := fixture.begin(t)
		defer op4.Close()
		listing, err = op4.store.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(listing) != 0 {
			t.Fatalf("expected empty vault after deletion retry, got %+v", listing)
		}
	})
}
