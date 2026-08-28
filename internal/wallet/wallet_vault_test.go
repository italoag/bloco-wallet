package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

type memoryAccountRepository struct {
	mu       sync.Mutex
	accounts map[string]*Account
	metadata map[string]string
}

type blockingGetAccountRepository struct {
	AccountRepository
	calls   atomic.Int32
	blockAt int32
	started chan struct{}
	release chan struct{}
}

func (repository *blockingGetAccountRepository) GetAccount(ctx context.Context, accountID string) (*Account, error) {
	if repository.calls.Add(1) == repository.blockAt {
		close(repository.started)
		select {
		case <-repository.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return repository.AccountRepository.GetAccount(ctx, accountID)
}

func newMemoryAccountRepository() *memoryAccountRepository {
	return &memoryAccountRepository{accounts: make(map[string]*Account), metadata: make(map[string]string)}
}

func cloneAccount(account *Account) *Account {
	if account == nil {
		return nil
	}
	cloned := *account
	cloned.SecretEnvelope = append([]byte(nil), account.SecretEnvelope...)
	return &cloned
}

func (repository *memoryAccountRepository) CreateAccount(_ context.Context, account *Account) error {
	if err := account.Validate(); err != nil {
		return err
	}
	if account.Revision == 0 {
		account.Revision = 1
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.accounts[account.AccountID]; exists {
		return ErrAccountConflict
	}
	for _, existing := range repository.accounts {
		if existing.SourceIdentity == account.SourceIdentity {
			return ErrAccountConflict
		}
	}
	repository.accounts[account.AccountID] = cloneAccount(account)
	return nil
}

func (repository *memoryAccountRepository) GetAccount(_ context.Context, accountID string) (*Account, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	account, exists := repository.accounts[accountID]
	if !exists {
		return nil, ErrAccountNotFound
	}
	return cloneAccount(account), nil
}

func (repository *memoryAccountRepository) FindAccountBySourceIdentity(_ context.Context, sourceIdentity string) (*Account, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for _, account := range repository.accounts {
		if account.SourceIdentity == sourceIdentity {
			return cloneAccount(account), nil
		}
	}
	return nil, ErrAccountNotFound
}

func (repository *memoryAccountRepository) FindAccountsByAddress(_ context.Context, address string) ([]Account, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	accounts := make([]Account, 0)
	for _, account := range repository.accounts {
		if account.Address == address {
			accounts = append(accounts, *cloneAccount(account))
		}
	}
	return accounts, nil
}

func (repository *memoryAccountRepository) GetVaultMetadata(_ context.Context, key string) (string, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	value, exists := repository.metadata[key]
	if !exists {
		return "", ErrAccountNotFound
	}
	return value, nil
}

func (repository *memoryAccountRepository) PutVaultMetadata(_ context.Context, key, value string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.metadata[key]; exists && existing != value {
		return ErrAccountConflict
	}
	repository.metadata[key] = value
	return nil
}

func (repository *memoryAccountRepository) ListAccounts(_ context.Context) ([]Account, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	accounts := make([]Account, 0, len(repository.accounts))
	for _, account := range repository.accounts {
		accounts = append(accounts, *cloneAccount(account))
	}
	return accounts, nil
}

func (repository *memoryAccountRepository) UpdateAccount(_ context.Context, account *Account) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	existing, exists := repository.accounts[account.AccountID]
	if !exists {
		return ErrAccountNotFound
	}
	if account.Revision != existing.Revision {
		return ErrAccountRevisionConflict
	}
	account.Revision++
	repository.accounts[account.AccountID] = cloneAccount(account)
	return nil
}

func (repository *memoryAccountRepository) DeletePendingAccount(_ context.Context, accountID string, backupGeneration uint64) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	account, exists := repository.accounts[accountID]
	if !exists || account.State != AccountStatePendingBackup || account.BackupGeneration != backupGeneration {
		return ErrAccountNotFound
	}
	delete(repository.accounts, accountID)
	return nil
}

func (repository *memoryAccountRepository) WithAccountTransaction(ctx context.Context, operation func(AccountRepository) error) error {
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
	tx := &memoryAccountRepository{accounts: snapshot, metadata: metadata}
	if err := operation(tx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	repository.accounts = tx.accounts
	repository.metadata = tx.metadata
	return nil
}

type fakeVaultClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *fakeVaultClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeVaultClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func newTestVault(t *testing.T) (*WalletVault, *memoryAccountRepository, *fakeVaultClock) {
	t.Helper()
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	repository := newMemoryAccountRepository()
	clock := &fakeVaultClock{now: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)}
	vault, err := NewWalletVault(repository, codec, VaultOptions{
		Now:               clock.Now,
		BackupTTL:         10 * time.Minute,
		SessionTTL:        30 * time.Minute,
		InactivityTTL:     5 * time.Minute,
		ChallengeWords:    3,
		SourceIdentityKey: bytes.Repeat([]byte{0x42}, 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	return vault, repository, clock
}

func messageSigningRequest(accountID string, digest []byte) SoftwareSigningRequest {
	var value [32]byte
	copy(value[:], digest)
	return SoftwareSigningRequest{
		AccountID:     accountID,
		Purpose:       SigningPurposeMessage,
		MessageScheme: MessageSigningEIP191Personal,
		Digest:        value,
		IntentHash:    crypto.Keccak256Hash([]byte("test-message-intent"), digest),
		ApprovalID:    "test-approval",
	}
}

func activateTestAccount(t *testing.T, vault *WalletVault, password []byte) AccountSummary {
	t.Helper()
	summary, challenge, err := vault.Create(context.Background(), CreateAccountRequest{
		Name:     "Vault Account",
		Password: password,
	})
	if err != nil {
		t.Fatal(err)
	}
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	activated, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers)
	if err != nil {
		t.Fatal(err)
	}
	if activated.AccountID != summary.AccountID {
		t.Fatal("activated account changed identity")
	}
	return activated
}

func TestTransactionAuthorizerLocksPasswordPerTransactionSession(t *testing.T) {
	vault, _, _ := newTestVault(t)
	password := []byte("Strong one-shot password 1!")
	account := activateTestAccount(t, vault, password)
	authorizer, err := NewTransactionAuthorizer(vault, TransactionAuthorizationPerTransaction)
	if err != nil {
		t.Fatal(err)
	}
	defer authorizer.Close()
	var handle CapabilityHandle
	if err := authorizer.Authorize(context.Background(), account.AccountID, password, func(value CapabilityHandle, _ uint64) error {
		handle = value
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if authorizer.HasActiveSession(context.Background(), account.AccountID) {
		t.Fatal("password-per-transaction authorizer retained session")
	}
	if _, err := vault.AuthorizationEpoch(context.Background(), handle); err == nil {
		t.Fatal("password-per-transaction capability remained usable")
	}
}

func TestTransactionAuthorizerSupportsConfiguredTemporarySession(t *testing.T) {
	vault, _, _ := newTestVault(t)
	password := []byte("Strong authorizer password 1!")
	account := activateTestAccount(t, vault, password)
	authorizer, err := NewTransactionAuthorizer(vault, TransactionAuthorizationTemporarySession)
	if err != nil {
		t.Fatal(err)
	}
	defer authorizer.Close()
	var firstHandle CapabilityHandle
	if err := authorizer.Authorize(context.Background(), account.AccountID, password, func(handle CapabilityHandle, epoch uint64) error {
		firstHandle = handle
		if epoch == 0 {
			t.Fatal("authorizer returned zero epoch")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Authorize(context.Background(), account.AccountID, nil, func(handle CapabilityHandle, _ uint64) error {
		if handle != firstHandle {
			t.Fatal("temporary authorization did not reuse active session")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	authorizer.Close()
	if _, err := vault.AuthorizationEpoch(context.Background(), firstHandle); err == nil {
		t.Fatal("closing authorizer left capability session active")
	}
}

func TestWalletVaultCreateRequiresBackupConfirmation(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	summary, challenge, err := vault.Create(context.Background(), CreateAccountRequest{
		Name:     "Vault Account",
		Password: password,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.State != AccountStatePendingBackup || len(challenge.Words) != 12 || len(challenge.RequiredWordIndices) != 3 {
		t.Fatal("invalid pending backup challenge")
	}
	account, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.State != AccountStatePendingBackup || len(account.SecretEnvelope) == 0 {
		t.Fatal("pending encrypted account was not persisted")
	}
	mnemonic := []byte(joinMnemonicWords(challenge.Words))
	if bytes.Contains(account.SecretEnvelope, mnemonic) {
		t.Fatal("plaintext mnemonic appears in persisted envelope")
	}
	privateKeyHex, err := derivePrivateKeyLegacy(string(mnemonic))
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := hexToECDSALegacy(privateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(privateKey.PublicKey).Hex() != summary.Address {
		t.Fatal("backup words do not derive persisted address")
	}

	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, map[int]string{}); !errors.Is(err, ErrBackupConfirmationFailed) {
		t.Fatalf("expected failed confirmation, got %v", err)
	}
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	active, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers)
	if err != nil {
		t.Fatal(err)
	}
	if active.State != AccountStateActive {
		t.Fatal("account was not activated")
	}
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers); !errors.Is(err, ErrBackupChallengeNotFound) {
		t.Fatalf("backup challenge remained reusable: %v", err)
	}
}

func TestWalletVaultCreatesConfiguredBIP39Account(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	summary, challenge, err := vault.Create(context.Background(), CreateAccountRequest{
		Name:            "Spanish 24",
		Password:        password,
		WordCount:       24,
		BIP39Language:   BIP39Spanish,
		BIP39Passphrase: "contraseña",
		DerivationPath:  "m/44'/60'/5'/1/7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(challenge.Words) != 24 {
		t.Fatalf("expected 24 backup words, got %d", len(challenge.Words))
	}
	if err := vault.SuspendBackup(challenge.ChallengeID); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewWalletVault(repository, vault.codec, vault.options)
	if err != nil {
		t.Fatal(err)
	}
	_, challenge, err = restarted.ResumeBackup(context.Background(), summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	vault = restarted
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers); err != nil {
		t.Fatalf("custom BIP39 account was not activated by mnemonic confirmation: %v", err)
	}
	stored, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.BIP39Language != string(BIP39Spanish) || !stored.HasBIP39Passphrase || stored.AccountIndex != 5 || stored.ChangeIndex != 1 || stored.AddressIndex != 7 {
		t.Fatal("configured BIP39 metadata was not persisted")
	}
	if _, err := vault.Unlock(context.Background(), summary.AccountID, password); err != nil {
		t.Fatal(err)
	}
}

func TestWalletVaultResumesPendingBackupAfterRestart(t *testing.T) {
	vault, repository, clock := newTestVault(t)
	password := []byte("Strong vault password 1!")
	summary, _, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Resume", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	codec := vault.codec
	options := vault.options
	vault.Close()
	restarted, err := NewWalletVault(repository, codec, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := restarted.ResumeBackup(context.Background(), summary.AccountID, []byte("Wrong vault password 1!")); err == nil {
		t.Fatal("incorrect password resumed backup")
	}
	resumed, challenge, err := restarted.ResumeBackup(context.Background(), summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.AccountID != summary.AccountID || len(challenge.Words) != 12 {
		t.Fatal("resumed backup changed account")
	}
	firstGeneration := challenge.BackupGeneration
	_, challenge, err = restarted.ResumeBackup(context.Background(), summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	if challenge.BackupGeneration != firstGeneration+1 {
		t.Fatal("resuming backup did not advance generation")
	}
	if err := restarted.SuspendBackup(challenge.ChallengeID); err != nil {
		t.Fatal(err)
	}
	if stored, err := repository.GetAccount(context.Background(), summary.AccountID); err != nil || stored.State != AccountStatePendingBackup {
		t.Fatal("suspending backup removed pending account")
	}
	_, challenge, err = restarted.ResumeBackup(context.Background(), summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	clock.Advance(time.Minute)
	active, err := restarted.ConfirmBackup(context.Background(), challenge.ChallengeID, answers)
	if err != nil {
		t.Fatal(err)
	}
	if active.State != AccountStateActive {
		t.Fatal("resumed backup did not activate account")
	}
}

func TestResumedBackupInvalidatesChallengesAcrossVaultInstances(t *testing.T) {
	vaultA, repository, clock := newTestVault(t)
	password := []byte("Strong vault password 1!")
	summary, challengeA, err := vaultA.Create(context.Background(), CreateAccountRequest{Name: "Cross Vault Backup", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	answersA := make(map[int]string, len(challengeA.RequiredWordIndices))
	for _, index := range challengeA.RequiredWordIndices {
		answersA[index] = challengeA.Words[index]
	}
	vaultA.mu.Lock()
	originalChallenge := vaultA.challenges[challengeA.ChallengeID]
	vaultA.challenges["stale-cancel"] = &backupChallengeState{
		accountID:        originalChallenge.accountID,
		backupGeneration: originalChallenge.backupGeneration,
		words:            append([]string(nil), originalChallenge.words...),
		required:         append([]int(nil), originalChallenge.required...),
		expiresAt:        originalChallenge.expiresAt,
	}
	vaultA.mu.Unlock()
	clock.Advance(9 * time.Minute)
	vaultB, err := NewWalletVault(repository, vaultA.codec, vaultA.options)
	if err != nil {
		t.Fatal(err)
	}
	_, challengeB, err := vaultB.ResumeBackup(context.Background(), summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	answersB := make(map[int]string, len(challengeB.RequiredWordIndices))
	for _, index := range challengeB.RequiredWordIndices {
		answersB[index] = challengeB.Words[index]
	}
	if err := vaultA.CancelBackup(context.Background(), "stale-cancel"); !errors.Is(err, ErrBackupChallengeNotFound) {
		t.Fatalf("stale cancellation affected resumed backup: %v", err)
	}
	clock.Advance(2 * time.Minute)
	if _, err := vaultA.ConfirmBackup(context.Background(), challengeA.ChallengeID, answersA); !errors.Is(err, ErrBackupChallengeNotFound) {
		t.Fatalf("stale challenge affected resumed backup: %v", err)
	}
	stored, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != AccountStatePendingBackup || stored.BackupGeneration != challengeB.BackupGeneration {
		t.Fatal("stale challenge changed pending account")
	}
	active, err := vaultB.ConfirmBackup(context.Background(), challengeB.ChallengeID, answersB)
	if err != nil {
		t.Fatal(err)
	}
	if active.State != AccountStateActive {
		t.Fatal("resumed challenge did not activate account")
	}
}

func TestWalletVaultCancelBackupDeletesPendingAccount(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	summary, challenge, err := vault.Create(context.Background(), CreateAccountRequest{
		Name:     "Cancelled",
		Password: []byte("Strong vault password 1!"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.CancelBackup(context.Background(), challenge.ChallengeID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetAccount(context.Background(), summary.AccountID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("pending account survived cancellation: %v", err)
	}
}

func TestWalletVaultUnlockSignRotateAndAutoLock(t *testing.T) {
	vault, repository, clock := newTestVault(t)
	oldPassword := []byte("Strong vault password 1!")
	newPassword := []byte("Different vault password 2!")
	account := activateTestAccount(t, vault, oldPassword)
	handle, err := vault.Unlock(context.Background(), account.AccountID, oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	serializedHandle, err := json.Marshal(handle)
	if !errors.Is(err, ErrCapabilitySerialization) || len(serializedHandle) != 0 {
		t.Fatal("capability handle serialized across the trust boundary")
	}
	signer, err := NewSoftwareSignerWithApprovalVerifier(vault, &approvalVerifierStub{})
	if err != nil {
		t.Fatal(err)
	}
	digest := crypto.Keccak256([]byte("intent"))
	result, err := signer.Sign(context.Background(), handle, messageSigningRequest(account.AccountID, digest))
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := crypto.SigToPub(digest, result.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey).Hex() != account.Address {
		t.Fatal("signature does not belong to account")
	}

	clock.Advance(6 * time.Minute)
	if _, err := signer.Sign(context.Background(), handle, messageSigningRequest(account.AccountID, digest)); !errors.Is(err, ErrCapabilityExpired) {
		t.Fatalf("inactive handle remained usable: %v", err)
	}
	handle, err = vault.Unlock(context.Background(), account.AccountID, oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	beforeRotation, err := repository.GetAccount(context.Background(), account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.RotatePassword(context.Background(), account.AccountID, oldPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	afterRotation, err := repository.GetAccount(context.Background(), account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if afterRotation.EnvelopeGeneration != beforeRotation.EnvelopeGeneration+1 || afterRotation.AuthorizationEpoch != beforeRotation.AuthorizationEpoch+1 {
		t.Fatal("rotation did not advance envelope and authorization generations")
	}
	if bytes.Equal(afterRotation.SecretEnvelope, beforeRotation.SecretEnvelope) {
		t.Fatal("rotation did not replace encrypted envelope")
	}
	if _, err := signer.Sign(context.Background(), handle, messageSigningRequest(account.AccountID, digest)); !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatalf("rotation did not revoke old handle: %v", err)
	}
	if _, err := vault.Unlock(context.Background(), account.AccountID, oldPassword); err == nil {
		t.Fatal("old password still unlocks after rotation")
	}
	newHandle, err := vault.Unlock(context.Background(), account.AccountID, newPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Lock(newHandle); err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(context.Background(), newHandle, messageSigningRequest(account.AccountID, digest)); !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatalf("locked handle remained usable: %v", err)
	}
	accountHandle, err := vault.Unlock(context.Background(), account.AccountID, newPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.LockAccount(context.Background(), account.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(context.Background(), accountHandle, messageSigningRequest(account.AccountID, digest)); !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatalf("account lock did not revoke handle: %v", err)
	}
	stored, err := repository.GetAccount(context.Background(), account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != AccountStateLocked {
		t.Fatal("account lock state was not persisted")
	}
}

func TestAuthorizationEpochRevokesHandlesAcrossVaultInstances(t *testing.T) {
	vaultA, repository, _ := newTestVault(t)
	oldPassword := []byte("Strong vault password 1!")
	newPassword := []byte("Different vault password 2!")
	account := activateTestAccount(t, vaultA, oldPassword)
	handleA, err := vaultA.Unlock(context.Background(), account.AccountID, oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	vaultB, err := NewWalletVault(repository, vaultA.codec, vaultA.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := vaultB.RotatePassword(context.Background(), account.AccountID, oldPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	signerA, err := NewSoftwareSigner(vaultA)
	if err != nil {
		t.Fatal(err)
	}
	digest := crypto.Keccak256([]byte("cross-vault"))
	if _, err := signerA.Sign(context.Background(), handleA, messageSigningRequest(account.AccountID, digest)); !errors.Is(err, ErrCapabilityExpired) {
		t.Fatalf("rotation in second vault did not revoke handle: %v", err)
	}
	handleB, err := vaultB.Unlock(context.Background(), account.AccountID, newPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := vaultA.LockAccount(context.Background(), account.AccountID); err != nil {
		t.Fatal(err)
	}
	signerB, err := NewSoftwareSigner(vaultB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signerB.Sign(context.Background(), handleB, messageSigningRequest(account.AccountID, digest)); !errors.Is(err, ErrCapabilityExpired) {
		t.Fatalf("lock in first vault did not revoke second vault handle: %v", err)
	}
}

func TestConcurrentUnlockCannotPublishHandleAfterLock(t *testing.T) {
	baseVault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	account := activateTestAccount(t, baseVault, password)
	blockingRepository := &blockingGetAccountRepository{
		AccountRepository: repository,
		blockAt:           2,
		started:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	unlockVault, err := NewWalletVault(blockingRepository, baseVault.codec, baseVault.options)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := unlockVault.Unlock(context.Background(), account.AccountID, password)
		result <- err
	}()
	select {
	case <-blockingRepository.started:
	case <-time.After(time.Second):
		t.Fatal("unlock did not reach authorization revalidation")
	}
	if err := baseVault.LockAccount(context.Background(), account.AccountID); err != nil {
		t.Fatal(err)
	}
	close(blockingRepository.release)
	select {
	case err := <-result:
		if !errors.Is(err, ErrCapabilityExpired) {
			t.Fatalf("concurrent unlock published handle after lock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent unlock did not finish")
	}
}

func TestWalletVaultExportsEncryptedAccountAtomically(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	newPassword := []byte("Strong export password 2!")
	account := activateTestAccount(t, vault, password)
	handle, err := vault.Unlock(context.Background(), account.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "account.bloco")
	exportRequest := EncryptedAccountExportRequest{
		Handle:             handle,
		Destination:        destination,
		CurrentPassword:    password,
		NewPassword:        newPassword,
		ConfirmNewPassword: append([]byte(nil), newPassword...),
	}

	if err := vault.ExportEncryptedAccount(context.Background(), exportRequest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("export mode is %o", info.Mode().Perm())
	}
	exportedJSON, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var exported EncryptedAccountExportV1
	if err := json.Unmarshal(exportedJSON, &exported); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetAccount(context.Background(), account.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(exported.SecretEnvelope, stored.SecretEnvelope) {
		t.Fatal("export reused the storage envelope")
	}
	if _, err := vault.codec.Open(password, exported.Metadata(), exported.SecretEnvelope); err == nil {
		t.Fatal("storage password opened exported envelope")
	}
	plaintext, err := vault.codec.Open(newPassword, exported.Metadata(), exported.SecretEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(plaintext)
	if bytes.Contains(exportedJSON, plaintext) {
		t.Fatal("export contains plaintext secret")
	}

	original := []byte("do-not-overwrite")
	if err := os.WriteFile(destination, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := vault.ExportEncryptedAccount(context.Background(), exportRequest); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected no-overwrite error, got %v", err)
	}
	current, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("existing export was overwritten")
	}
}

func TestWalletVaultExpiresBackupChallenge(t *testing.T) {
	vault, repository, clock := newTestVault(t)
	summary, challenge, err := vault.Create(context.Background(), CreateAccountRequest{
		Name:     "Expired",
		Password: []byte("Strong vault password 1!"),
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(11 * time.Minute)
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, nil); !errors.Is(err, ErrBackupChallengeExpired) {
		t.Fatalf("expected expired challenge, got %v", err)
	}
	if _, err := repository.GetAccount(context.Background(), summary.AccountID); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expired pending account survived: %v", err)
	}
}
