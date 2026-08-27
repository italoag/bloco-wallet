package wallet

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type faultEnvelope struct {
	base           SecretEnvelope
	sealErr        error
	openErr        error
	failOpenAt     int
	overrideOpenAt int
	overrideOpen   []byte
	openCalls      int
}

func (envelope *faultEnvelope) Seal(password []byte, metadata EnvelopeMetadata, plaintext []byte) ([]byte, error) {
	if envelope.sealErr != nil {
		return nil, envelope.sealErr
	}
	return envelope.base.Seal(password, metadata, plaintext)
}

func (envelope *faultEnvelope) Open(password []byte, metadata EnvelopeMetadata, encoded []byte) ([]byte, error) {
	envelope.openCalls++
	if envelope.overrideOpenAt > 0 && envelope.openCalls == envelope.overrideOpenAt {
		return append([]byte(nil), envelope.overrideOpen...), nil
	}
	if envelope.openErr != nil || (envelope.failOpenAt > 0 && envelope.openCalls == envelope.failOpenAt) {
		if envelope.openErr != nil {
			return nil, envelope.openErr
		}
		return nil, errors.New("envelope open fault")
	}
	return envelope.base.Open(password, metadata, encoded)
}

type faultAccountRepository struct {
	base      AccountRepository
	createErr error
	getErr    error
	failGetAt int
	getCalls  int
	listErr   error
	updateErr error
	deleteErr error
	txErr     error
}

func (repository *faultAccountRepository) CreateAccount(ctx context.Context, account *Account) error {
	if repository.createErr != nil {
		return repository.createErr
	}
	return repository.base.CreateAccount(ctx, account)
}

func (repository *faultAccountRepository) GetAccount(ctx context.Context, accountID string) (*Account, error) {
	repository.getCalls++
	if repository.getErr != nil && (repository.failGetAt == 0 || repository.getCalls == repository.failGetAt) {
		return nil, repository.getErr
	}
	return repository.base.GetAccount(ctx, accountID)
}

func (repository *faultAccountRepository) FindAccountBySourceIdentity(ctx context.Context, sourceIdentity string) (*Account, error) {
	return repository.base.FindAccountBySourceIdentity(ctx, sourceIdentity)
}

func (repository *faultAccountRepository) FindAccountsByAddress(ctx context.Context, address string) ([]Account, error) {
	return repository.base.FindAccountsByAddress(ctx, address)
}

func (repository *faultAccountRepository) GetVaultMetadata(ctx context.Context, key string) (string, error) {
	return repository.base.GetVaultMetadata(ctx, key)
}

func (repository *faultAccountRepository) PutVaultMetadata(ctx context.Context, key, value string) error {
	return repository.base.PutVaultMetadata(ctx, key, value)
}

func (repository *faultAccountRepository) ListAccounts(ctx context.Context) ([]Account, error) {
	if repository.listErr != nil {
		return nil, repository.listErr
	}
	return repository.base.ListAccounts(ctx)
}

func (repository *faultAccountRepository) UpdateAccount(ctx context.Context, account *Account) error {
	if repository.updateErr != nil {
		return repository.updateErr
	}
	return repository.base.UpdateAccount(ctx, account)
}

func (repository *faultAccountRepository) DeletePendingAccount(ctx context.Context, accountID string, backupGeneration uint64) error {
	if repository.deleteErr != nil {
		return repository.deleteErr
	}
	return repository.base.DeletePendingAccount(ctx, accountID, backupGeneration)
}

func (repository *faultAccountRepository) WithAccountTransaction(ctx context.Context, operation func(AccountRepository) error) error {
	if repository.txErr != nil {
		return repository.txErr
	}
	return operation(repository)
}

func TestWalletVaultRejectsInvalidRequests(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWalletVault(nil, codec, VaultOptions{}); err == nil {
		t.Fatal("nil repository was accepted")
	}
	if _, err := NewWalletVault(newMemoryAccountRepository(), nil, VaultOptions{}); err == nil {
		t.Fatal("nil codec was accepted")
	}
	if _, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{}); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("repository fault")
	faultRepository := &faultAccountRepository{base: newMemoryAccountRepository(), txErr: fault}
	faultVault, err := NewWalletVault(faultRepository, codec, VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := faultVault.Create(context.Background(), CreateAccountRequest{Name: "Fault", Password: []byte("Strong vault password 1!")}); !errors.Is(err, fault) {
		t.Fatalf("repository transaction error was lost: %v", err)
	}
	faultRepository.txErr = nil
	faultRepository.createErr = fault
	if _, _, err := faultVault.Create(context.Background(), CreateAccountRequest{Name: "Create Fault", Password: []byte("Strong vault password 1!")}); !errors.Is(err, fault) {
		t.Fatalf("repository create error was lost: %v", err)
	}
	faultRepository.createErr = nil
	faultRepository.listErr = fault
	if _, err := faultVault.ListAccounts(context.Background()); !errors.Is(err, fault) {
		t.Fatalf("repository list error was lost: %v", err)
	}
	overflowRepository := newMemoryAccountRepository()
	overflowVault, err := NewWalletVault(overflowRepository, codec, VaultOptions{ChallengeWords: 13})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := overflowVault.Create(context.Background(), CreateAccountRequest{Name: "Overflow", Password: []byte("Strong vault password 1!")}); err == nil {
		t.Fatal("oversized backup challenge was accepted")
	}
	if accounts, err := overflowRepository.ListAccounts(context.Background()); err != nil || len(accounts) != 0 {
		t.Fatal("failed challenge left a pending account")
	}
	vault, _, _ := newTestVault(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := vault.Create(cancelled, CreateAccountRequest{Name: "Cancelled", Password: []byte("Strong vault password 1!")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancelled create, got %v", err)
	}
	if _, _, err := vault.Create(context.Background(), CreateAccountRequest{Password: []byte("Strong vault password 1!")}); err == nil {
		t.Fatal("empty account name was accepted")
	}
	if _, _, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Short", Password: []byte("short")}); err == nil {
		t.Fatal("short password was accepted")
	}
	if _, err := vault.ConfirmBackup(context.Background(), "missing", nil); !errors.Is(err, ErrBackupChallengeNotFound) {
		t.Fatalf("unexpected missing confirmation error: %v", err)
	}
	if err := vault.CancelBackup(context.Background(), "missing"); !errors.Is(err, ErrBackupChallengeNotFound) {
		t.Fatalf("unexpected missing cancellation error: %v", err)
	}
	if _, err := vault.Unlock(context.Background(), "missing", []byte("Strong vault password 1!")); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("unexpected missing unlock error: %v", err)
	}
	if _, err := NewSoftwareSigner(nil); err == nil {
		t.Fatal("nil vault signer was accepted")
	}
	handle := CapabilityHandle{accountID: "account", expiresAt: time.Now()}
	if handle.AccountID() != "account" || handle.ExpiresAt().IsZero() {
		t.Fatal("capability handle metadata mismatch")
	}
	if err := json.Unmarshal([]byte(`{}`), &handle); !errors.Is(err, ErrCapabilitySerialization) {
		t.Fatalf("capability handle deserialized: %v", err)
	}
}

func TestWalletVaultReportsChallengeStoreConflictAfterCommit(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	repository := newMemoryAccountRepository()
	vault, err := NewWalletVault(repository, codec, VaultOptions{
		Random:            bytes.NewReader(make([]byte, 256)),
		MnemonicGenerator: func() (string, error) { return "test test test test test test test test test test test junk", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	accountID := "00000000-0000-4000-8000-000000000000"
	vault.challenges["existing"] = &backupChallengeState{accountID: accountID, consuming: true}
	summary, _, err := vault.Create(context.Background(), CreateAccountRequest{
		Name:     "Challenge conflict",
		Password: []byte("Strong vault password 1!"),
	})
	if !errors.Is(err, ErrBackupConfirmationInProgress) || summary.AccountID != accountID {
		t.Fatalf("challenge store conflict returned %v for %s", err, summary.AccountID)
	}
	if err := repository.DeletePendingAccount(context.Background(), accountID, 1); err != nil {
		t.Fatal(err)
	}
}

func TestWalletVaultPropagatesGeneratorAndRandomErrors(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	fault := errors.New("entropy fault")
	newVault := func(options VaultOptions) *WalletVault {
		vault, err := NewWalletVault(newMemoryAccountRepository(), codec, options)
		if err != nil {
			t.Fatal(err)
		}
		return vault
	}
	generatorFault := newVault(VaultOptions{MnemonicGenerator: func() (string, error) { return "", fault }})
	if _, _, err := generatorFault.Create(context.Background(), CreateAccountRequest{Name: "Generator", Password: []byte("Strong vault password 1!")}); !errors.Is(err, fault) {
		t.Fatalf("mnemonic generator error was lost: %v", err)
	}
	malformed := newVault(VaultOptions{MnemonicGenerator: func() (string, error) { return "too few words", nil }})
	if _, _, err := malformed.Create(context.Background(), CreateAccountRequest{Name: "Malformed", Password: []byte("Strong vault password 1!")}); err == nil {
		t.Fatal("malformed generated mnemonic was accepted")
	}
	randomFault := newVault(VaultOptions{Random: errorReader{err: fault}})
	if _, _, err := randomFault.Create(context.Background(), CreateAccountRequest{Name: "Random", Password: []byte("Strong vault password 1!")}); !errors.Is(err, fault) {
		t.Fatalf("account ID entropy error was lost: %v", err)
	}
	challengeIDFault := newVault(VaultOptions{Random: bytes.NewReader(make([]byte, 16))})
	if _, _, err := challengeIDFault.Create(context.Background(), CreateAccountRequest{Name: "Challenge", Password: []byte("Strong vault password 1!")}); err == nil {
		t.Fatal("challenge ID entropy failure was ignored")
	}
	challengeWordsFault := newVault(VaultOptions{Random: bytes.NewReader(make([]byte, 48))})
	if _, _, err := challengeWordsFault.Create(context.Background(), CreateAccountRequest{Name: "Indices", Password: []byte("Strong vault password 1!")}); err == nil {
		t.Fatal("challenge index entropy failure was ignored")
	}

	baseVault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	active := activateTestAccount(t, baseVault, password)
	unlockFault, err := NewWalletVault(repository, baseVault.codec, VaultOptions{Random: errorReader{err: fault}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unlockFault.Unlock(context.Background(), active.AccountID, password); !errors.Is(err, fault) {
		t.Fatalf("session entropy error was lost: %v", err)
	}
}

func TestWalletVaultRollsBackEnvelopeFailures(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	fault := errors.New("envelope fault")
	sealRepository := newMemoryAccountRepository()
	sealVault, err := NewWalletVault(sealRepository, &faultEnvelope{base: codec, sealErr: fault}, VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sealVault.Create(context.Background(), CreateAccountRequest{Name: "Seal", Password: []byte("Strong vault password 1!")}); !errors.Is(err, fault) {
		t.Fatalf("seal error was lost: %v", err)
	}
	openRepository := newMemoryAccountRepository()
	openVault, err := NewWalletVault(openRepository, &faultEnvelope{base: codec, openErr: fault}, VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := openVault.Create(context.Background(), CreateAccountRequest{Name: "Open", Password: []byte("Strong vault password 1!")}); !errors.Is(err, fault) {
		t.Fatalf("reopen error was lost: %v", err)
	}
	if accounts, _ := openRepository.ListAccounts(context.Background()); len(accounts) != 0 {
		t.Fatal("failed envelope verification committed account")
	}

	baseVault, repository, _ := newTestVault(t)
	oldPassword := []byte("Strong vault password 1!")
	newPassword := []byte("Different vault password 2!")
	active := activateTestAccount(t, baseVault, oldPassword)
	before, err := repository.GetAccount(context.Background(), active.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	rotateSealVault, err := NewWalletVault(repository, &faultEnvelope{base: baseVault.codec, sealErr: fault}, baseVault.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := rotateSealVault.RotatePassword(context.Background(), active.AccountID, oldPassword, newPassword); !errors.Is(err, fault) {
		t.Fatalf("rotation seal error was lost: %v", err)
	}
	verifyFault := &faultEnvelope{base: baseVault.codec, failOpenAt: 2}
	rotateVerifyVault, err := NewWalletVault(repository, verifyFault, baseVault.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := rotateVerifyVault.RotatePassword(context.Background(), active.AccountID, oldPassword, newPassword); err == nil {
		t.Fatal("rotation verification fault was ignored")
	}
	after, err := repository.GetAccount(context.Background(), active.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if after.EnvelopeGeneration != before.EnvelopeGeneration || !bytes.Equal(after.SecretEnvelope, before.SecretEnvelope) {
		t.Fatal("failed rotation modified persisted envelope")
	}
}

func TestWalletVaultPreservesChallengeOnRepositoryFailures(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	base := newMemoryAccountRepository()
	fault := errors.New("repository fault")
	repository := &faultAccountRepository{base: base}
	vault, err := NewWalletVault(repository, codec, VaultOptions{ChallengeWords: 3})
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("Strong vault password 1!")
	_, challenge, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Update Fault", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	repository.updateErr = fault
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers); !errors.Is(err, fault) {
		t.Fatalf("update error was lost: %v", err)
	}
	repository.updateErr = nil
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers); err != nil {
		t.Fatal(err)
	}
	_, cancellation, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Delete Fault", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	repository.deleteErr = fault
	if err := vault.CancelBackup(context.Background(), cancellation.ChallengeID); !errors.Is(err, fault) {
		t.Fatalf("delete error was lost: %v", err)
	}
	repository.deleteErr = nil
	if err := vault.CancelBackup(context.Background(), cancellation.ChallengeID); err != nil {
		t.Fatal(err)
	}
}

func TestWalletVaultRejectsBackupMaterialThatCannotDerive(t *testing.T) {
	vault, _, _ := newTestVault(t)
	_, challenge, err := vault.Create(context.Background(), CreateAccountRequest{
		Name:            "Invalid derivation material",
		Password:        []byte("Strong vault password 1!"),
		BIP39Passphrase: "passphrase",
	})
	if err != nil {
		t.Fatal(err)
	}
	vault.mu.Lock()
	state := vault.challenges[challenge.ChallengeID]
	for index := range state.words {
		state.words[index] = "invalid"
	}
	vault.mu.Unlock()
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = "invalid"
	}
	if _, err := vault.ConfirmBackupWithMaterial(context.Background(), challenge.ChallengeID, BackupMaterialConfirmation{
		WordAnswers:     answers,
		BIP39Passphrase: "passphrase",
		DerivationPath:  challenge.DerivationPath,
		BIP39Language:   challenge.BIP39Language,
	}); !errors.Is(err, ErrBackupConfirmationFailed) {
		t.Fatalf("non-derivable backup material returned %v", err)
	}
}

func TestWalletVaultRejectsConcurrentBackupConsumption(t *testing.T) {
	vault, _, _ := newTestVault(t)
	_, challenge, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Consuming", Password: []byte("Strong vault password 1!")})
	if err != nil {
		t.Fatal(err)
	}
	vault.mu.Lock()
	vault.challenges[challenge.ChallengeID].consuming = true
	vault.mu.Unlock()
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, nil); !errors.Is(err, ErrBackupConfirmationInProgress) {
		t.Fatalf("concurrent confirmation was accepted: %v", err)
	}
	if err := vault.CancelBackup(context.Background(), challenge.ChallengeID); !errors.Is(err, ErrBackupConfirmationInProgress) {
		t.Fatalf("concurrent cancellation was accepted: %v", err)
	}
	if err := vault.SuspendBackup(challenge.ChallengeID); !errors.Is(err, ErrBackupConfirmationInProgress) {
		t.Fatalf("concurrent suspension was accepted: %v", err)
	}
	if _, err := vault.issueBackupChallenge(challenge.AccountID, challenge.BackupGeneration, mnemonicBackupMaterial{
		words:    challenge.Words,
		path:     challenge.DerivationPath,
		language: challenge.BIP39Language,
	}); !errors.Is(err, ErrBackupConfirmationInProgress) {
		t.Fatalf("concurrent challenge replacement was accepted: %v", err)
	}
	vault.mu.Lock()
	vault.challenges[challenge.ChallengeID].consuming = false
	vault.mu.Unlock()
	if err := vault.CancelBackup(context.Background(), challenge.ChallengeID); err != nil {
		t.Fatal(err)
	}
}

func TestWalletVaultRejectsInvalidAccountStatesAndPasswords(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	pending, challenge, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Pending", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Unlock(context.Background(), pending.AccountID, password); !errors.Is(err, ErrAccountPendingBackup) {
		t.Fatalf("pending account unlocked: %v", err)
	}
	if err := vault.RotatePassword(context.Background(), pending.AccountID, password, []byte("Different vault password 2!")); err == nil {
		t.Fatal("pending account rotated password")
	}
	if _, _, err := vault.ResumeBackup(context.Background(), pending.AccountID, []byte("Wrong vault password 1!")); err == nil {
		t.Fatal("wrong password resumed backup")
	}
	if err := vault.CancelBackup(context.Background(), challenge.ChallengeID); err != nil {
		t.Fatal(err)
	}

	active := activateTestAccount(t, vault, password)
	if _, _, err := vault.ResumeBackup(context.Background(), active.AccountID, password); !errors.Is(err, ErrBackupChallengeNotFound) {
		t.Fatalf("active account resumed backup: %v", err)
	}
	if _, err := vault.Unlock(context.Background(), active.AccountID, []byte("Wrong vault password 1!")); err == nil {
		t.Fatal("wrong password unlocked account")
	}
	if err := vault.RotatePassword(context.Background(), active.AccountID, password, password); err == nil {
		t.Fatal("rotation accepted identical password")
	}
	if err := vault.RotatePassword(context.Background(), active.AccountID, password, []byte("short")); err == nil {
		t.Fatal("rotation accepted short password")
	}
	account, err := repository.GetAccount(context.Background(), active.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	account.State = AccountStateUnavailable
	if err := repository.UpdateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Unlock(context.Background(), active.AccountID, password); err == nil {
		t.Fatal("unavailable account unlocked")
	}
	if err := vault.LockAccount(context.Background(), active.AccountID); err == nil {
		t.Fatal("unavailable account was locked")
	}
}

func TestWalletVaultUnlockRevalidationFailures(t *testing.T) {
	baseVault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	active := activateTestAccount(t, baseVault, password)
	fault := errors.New("repository fault")
	secondGetFault := &faultAccountRepository{base: repository, getErr: fault, failGetAt: 2}
	vault, err := NewWalletVault(secondGetFault, baseVault.codec, baseVault.options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Unlock(context.Background(), active.AccountID, password); !errors.Is(err, fault) {
		t.Fatalf("authorization revalidation error was lost: %v", err)
	}
	if err := baseVault.LockAccount(context.Background(), active.AccountID); err != nil {
		t.Fatal(err)
	}
	updateFault := &faultAccountRepository{base: repository, updateErr: fault}
	vault, err = NewWalletVault(updateFault, baseVault.codec, baseVault.options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Unlock(context.Background(), active.AccountID, password); !errors.Is(err, fault) {
		t.Fatalf("locked account update error was lost: %v", err)
	}
}

func TestSoftwareSignerEnforcesCapabilitiesContextAndTTL(t *testing.T) {
	vault, repository, clock := newTestVault(t)
	password := []byte("Strong vault password 1!")
	active := activateTestAccount(t, vault, password)
	account, err := repository.GetAccount(context.Background(), active.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Capabilities = CapabilityExportSecret
	if err := repository.UpdateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	handle, err := vault.Unlock(context.Background(), active.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSoftwareSigner(vault)
	if err != nil {
		t.Fatal(err)
	}
	digest := crypto.Keccak256([]byte("capability"))
	if _, err := signer.Sign(context.Background(), handle, messageSigningRequest(active.AccountID, digest)); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("signer ignored capabilities: %v", err)
	}
	invalidRequest := messageSigningRequest("other-account", digest)
	if _, err := signer.Sign(context.Background(), handle, invalidRequest); err == nil {
		t.Fatal("signer accepted mismatched account")
	}
	invalidRequest = messageSigningRequest(active.AccountID, digest)
	invalidRequest.ApprovalID = ""
	if _, err := signer.Sign(context.Background(), handle, invalidRequest); err == nil {
		t.Fatal("signer accepted missing approval")
	}
	invalidRequest = messageSigningRequest(active.AccountID, digest)
	invalidRequest.Purpose = SigningPurposeTransaction
	if _, err := signer.Sign(context.Background(), handle, invalidRequest); err == nil {
		t.Fatal("signer accepted transaction without chain identity")
	}
	invalidRequest.Purpose = "unknown"
	if _, err := signer.Sign(context.Background(), handle, invalidRequest); err == nil {
		t.Fatal("signer accepted unknown purpose")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := signer.Sign(cancelled, handle, messageSigningRequest(active.AccountID, digest)); !errors.Is(err, context.Canceled) {
		t.Fatalf("signer ignored context: %v", err)
	}

	account, err = repository.GetAccount(context.Background(), active.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Capabilities = CapabilitySignMessage
	if err := repository.UpdateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if err := vault.Lock(handle); err != nil {
		t.Fatal(err)
	}
	handle, err = vault.Unlock(context.Background(), active.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	transactionRequest := messageSigningRequest(active.AccountID, digest)
	transactionRequest.Purpose = SigningPurposeTransaction
	transactionRequest.ChainID = 1
	if _, err := signer.Sign(context.Background(), handle, transactionRequest); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("message capability signed transaction: %v", err)
	}
	for range 2 {
		clock.Advance(4 * time.Minute)
		if _, err := signer.Sign(context.Background(), handle, messageSigningRequest(active.AccountID, digest)); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(22 * time.Minute)
	if _, err := signer.Sign(context.Background(), handle, messageSigningRequest(active.AccountID, digest)); !errors.Is(err, ErrCapabilityExpired) {
		t.Fatalf("absolute TTL did not expire handle: %v", err)
	}
	if err := vault.Lock(handle); !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatalf("expired handle remained lockable: %v", err)
	}
}

func TestWalletVaultProactivelyExpiresInactiveSession(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	repository := newMemoryAccountRepository()
	vault, err := NewWalletVault(repository, codec, VaultOptions{
		Now:            time.Now,
		BackupTTL:      time.Minute,
		SessionTTL:     time.Second,
		InactivityTTL:  20 * time.Millisecond,
		ChallengeWords: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("Strong vault password 1!")
	active := activateTestAccount(t, vault, password)
	handle, err := vault.Unlock(context.Background(), active.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSoftwareSigner(vault)
	if err != nil {
		t.Fatal(err)
	}
	request := messageSigningRequest(active.AccountID, crypto.Keccak256([]byte("proactive")))
	deadline := time.Now().Add(time.Second)
	for {
		_, err = signer.Sign(context.Background(), handle, request)
		if errors.Is(err, ErrCapabilityNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not expire proactively: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		stored, err := repository.GetAccount(context.Background(), active.AccountID)
		if err == nil && stored.State == AccountStateLocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("proactive expiry did not persist locked state")
}

func TestWalletVaultHelpersAndShutdown(t *testing.T) {
	vault, _, _ := newTestVault(t)
	if summaries, err := vault.ListAccounts(context.Background()); err != nil || len(summaries) != 0 {
		t.Fatalf("unexpected account list: %v", err)
	}
	if summaryFromAccount(nil) != (AccountSummary{}) {
		t.Fatal("nil account summary was not empty")
	}
	if _, _, err := deriveSecretIdentity("unknown", []byte("secret")); err == nil {
		t.Fatal("unknown secret type was accepted")
	}
	if _, _, err := deriveSecretIdentity(SecretTypePrivateKey, []byte("short")); err == nil {
		t.Fatal("short private key was accepted")
	}
	if _, _, err := deriveSecretIdentity(SecretTypePrivateKey, make([]byte, 32)); err == nil {
		t.Fatal("invalid private key scalar was accepted")
	}
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKeyBytes := crypto.FromECDSA(privateKey)
	derived, address, err := deriveSecretIdentity(SecretTypePrivateKey, privateKeyBytes)
	if err != nil {
		t.Fatal(err)
	}
	clear(derived)
	if address != crypto.PubkeyToAddress(privateKey.PublicKey).Hex() {
		t.Fatal("private key identity mismatch")
	}
	if _, err := randomWordIndices(crand.Reader, 0, 1); err == nil {
		t.Fatal("invalid challenge size was accepted")
	}
	if _, err := newRandomToken(crand.Reader, 0); err == nil {
		t.Fatal("empty random token was accepted")
	}
	active := activateTestAccount(t, vault, []byte("Strong vault password 1!"))
	handle, err := vault.Unlock(context.Background(), active.AccountID, []byte("Strong vault password 1!"))
	if err != nil {
		t.Fatal(err)
	}
	vault.LockAll()
	signer, err := NewSoftwareSigner(vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(context.Background(), handle, messageSigningRequest(active.AccountID, crypto.Keccak256([]byte("locked")))); !errors.Is(err, ErrCapabilityNotFound) {
		t.Fatal("LockAll left a usable session")
	}
	handle, err = vault.Unlock(context.Background(), active.AccountID, []byte("Strong vault password 1!"))
	if err != nil {
		t.Fatal(err)
	}
	vault.Close()
	if _, err := signer.Sign(context.Background(), handle, messageSigningRequest(active.AccountID, crypto.Keccak256([]byte("closed")))); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("Close left a usable session")
	}
	if _, _, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Closed", Password: []byte("Strong vault password 1!")}); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted create")
	}
	if _, _, err := vault.ResumeBackup(context.Background(), active.AccountID, []byte("Strong vault password 1!")); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted backup resume")
	}
	if _, err := vault.ConfirmBackup(context.Background(), "closed", nil); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted backup confirmation")
	}
	if err := vault.CancelBackup(context.Background(), "closed"); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted backup cancellation")
	}
	if err := vault.SuspendBackup("closed"); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted backup suspension")
	}
	if _, err := vault.Unlock(context.Background(), active.AccountID, []byte("Strong vault password 1!")); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted unlock")
	}
	if err := vault.RotatePassword(context.Background(), active.AccountID, []byte("Strong vault password 1!"), []byte("Different vault password 2!")); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted rotation")
	}
	if err := vault.LockAccount(context.Background(), active.AccountID); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted account lock")
	}
	if err := vault.Lock(CapabilityHandle{}); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault accepted handle lock")
	}
	if _, err := vault.ListAccounts(context.Background()); !errors.Is(err, ErrVaultClosed) {
		t.Fatal("closed vault listed accounts")
	}
	vault.Close()
}
