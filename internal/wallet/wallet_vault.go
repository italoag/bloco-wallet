package wallet

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	ErrBackupChallengeNotFound      = errors.New("backup challenge not found")
	ErrBackupChallengeExpired       = errors.New("backup challenge expired")
	ErrBackupConfirmationFailed     = errors.New("backup confirmation failed")
	ErrBackupConfirmationInProgress = errors.New("backup confirmation in progress")
	ErrAccountPendingBackup         = errors.New("account backup is pending")
	ErrCapabilityNotFound           = errors.New("capability handle not found")
	ErrCapabilityExpired            = errors.New("capability handle expired")
	ErrCapabilityDenied             = errors.New("capability is not allowed")
	ErrCapabilitySerialization      = errors.New("capability handles cannot be serialized")
	ErrVaultClosed                  = errors.New("wallet vault is closed")
)

type VaultOptions struct {
	Now               func() time.Time
	Random            io.Reader
	MnemonicGenerator func() (string, error)
	BackupTTL         time.Duration
	SessionTTL        time.Duration
	InactivityTTL     time.Duration
	ChallengeWords    int
}

type CreateAccountRequest struct {
	Name     string
	Password []byte
}

type AccountSummary struct {
	AccountID        string
	Name             string
	Address          string
	SignerKind       SignerKind
	DerivationScheme string
	DerivationPath   string
	Capabilities     AccountCapability
	State            AccountState
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type BackupChallenge struct {
	ChallengeID         string
	AccountID           string
	BackupGeneration    uint64
	Words               []string
	RequiredWordIndices []int
	ExpiresAt           time.Time
}

type CapabilityHandle struct {
	token     string
	accountID string
	expiresAt time.Time
}

func (handle CapabilityHandle) AccountID() string {
	return handle.accountID
}

func (handle CapabilityHandle) ExpiresAt() time.Time {
	return handle.expiresAt
}

func (CapabilityHandle) MarshalJSON() ([]byte, error) {
	return nil, ErrCapabilitySerialization
}

func (*CapabilityHandle) UnmarshalJSON([]byte) error {
	return ErrCapabilitySerialization
}

type backupChallengeState struct {
	accountID        string
	backupGeneration uint64
	words            []string
	required         []int
	expiresAt        time.Time
	consuming        bool
}

type vaultSession struct {
	accountID          string
	privateKey         []byte
	capabilities       AccountCapability
	authorizationEpoch uint64
	expiresAt          time.Time
	lastUsedAt         time.Time
	timer              *time.Timer
	timerGeneration    uint64
}

type WalletVault struct {
	repository AccountRepository
	codec      SecretEnvelope
	options    VaultOptions

	lifecycle  sync.RWMutex
	mu         sync.Mutex
	closed     bool
	challenges map[string]*backupChallengeState
	sessions   map[string]*vaultSession
}

func NewWalletVault(repository AccountRepository, codec SecretEnvelope, options VaultOptions) (*WalletVault, error) {
	if repository == nil || codec == nil {
		return nil, fmt.Errorf("vault repository and envelope codec are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.MnemonicGenerator == nil {
		options.MnemonicGenerator = GenerateMnemonic
	}
	if options.BackupTTL <= 0 {
		options.BackupTTL = 10 * time.Minute
	}
	if options.SessionTTL <= 0 {
		options.SessionTTL = 30 * time.Minute
	}
	if options.InactivityTTL <= 0 || options.InactivityTTL > options.SessionTTL {
		options.InactivityTTL = 5 * time.Minute
	}
	if options.ChallengeWords <= 0 {
		options.ChallengeWords = 3
	}
	return &WalletVault{
		repository: repository,
		codec:      codec,
		options:    options,
		challenges: make(map[string]*backupChallengeState),
		sessions:   make(map[string]*vaultSession),
	}, nil
}

func (vault *WalletVault) beginOperation() error {
	vault.lifecycle.RLock()
	if vault.closed {
		vault.lifecycle.RUnlock()
		return ErrVaultClosed
	}
	return nil
}

func (vault *WalletVault) endOperation() {
	vault.lifecycle.RUnlock()
}

func (vault *WalletVault) Create(ctx context.Context, request CreateAccountRequest) (AccountSummary, BackupChallenge, error) {
	if err := vault.beginOperation(); err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	defer vault.endOperation()
	if err := ctx.Err(); err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("account name is required")
	}
	if err := validateNewStoragePassword(request.Password); err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	mnemonic, err := vault.options.MnemonicGenerator()
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("generate mnemonic: %w", err)
	}
	words := strings.Fields(mnemonic)
	if len(words) != 12 {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("unexpected mnemonic length")
	}
	accountID, err := newUUID(vault.options.Random)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	privateKey, address, err := deriveSecretIdentity(SecretTypeMnemonic, []byte(mnemonic))
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	clear(privateKey)
	metadata := EnvelopeMetadata{
		AccountID:          accountID,
		SecretType:         SecretTypeMnemonic,
		Address:            address,
		EnvelopeGeneration: 1,
		Derivation: DerivationMetadata{
			Scheme:   "bip44",
			Path:     "m/44'/60'/0'/0/0",
			Language: "english",
		},
	}
	envelope, err := vault.codec.Seal(request.Password, metadata, []byte(mnemonic))
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	now := vault.options.Now().UTC()
	account := &Account{
		AccountID:          accountID,
		Name:               name,
		Address:            address,
		SignerKind:         SignerKindSoftware,
		SignerReference:    accountID,
		SecretType:         SecretTypeMnemonic,
		DerivationScheme:   metadata.Derivation.Scheme,
		DerivationPath:     metadata.Derivation.Path,
		BIP39Language:      metadata.Derivation.Language,
		Capabilities:       CapabilitySignTransaction | CapabilitySignMessage | CapabilityExportSecret,
		State:              AccountStatePendingBackup,
		SecretEnvelope:     envelope,
		EnvelopeGeneration: 1,
		AuthorizationEpoch: 1,
		BackupGeneration:   1,
		SourceIdentity:     "generated:" + accountID,
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	challenge, err := vault.prepareBackupChallenge(accountID, account.BackupGeneration, words)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	if err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		if err := transaction.CreateAccount(ctx, account); err != nil {
			return err
		}
		persisted, err := transaction.GetAccount(ctx, account.AccountID)
		if err != nil {
			return err
		}
		reopened, err := vault.codec.Open(request.Password, metadataForAccount(persisted), persisted.SecretEnvelope)
		if err != nil {
			return fmt.Errorf("reopen pending envelope: %w", err)
		}
		defer clear(reopened)
		if !bytes.Equal(reopened, []byte(mnemonic)) {
			return fmt.Errorf("pending envelope plaintext mismatch")
		}
		verificationKey, verificationAddress, err := deriveSecretIdentity(persisted.SecretType, reopened)
		if err != nil {
			return err
		}
		clear(verificationKey)
		if !addressesEqual(verificationAddress, persisted.Address) {
			return fmt.Errorf("pending envelope identity mismatch")
		}
		account = persisted
		return ctx.Err()
	}); err != nil {
		clearWords(challenge.Words)
		return AccountSummary{}, BackupChallenge{}, err
	}
	if err := vault.storeBackupChallenge(challenge); err != nil {
		clearWords(challenge.Words)
		return summaryFromAccount(account), BackupChallenge{}, err
	}
	return summaryFromAccount(account), challenge, nil
}

func (vault *WalletVault) ResumeBackup(ctx context.Context, accountID string, password []byte) (AccountSummary, BackupChallenge, error) {
	if err := vault.beginOperation(); err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	defer vault.endOperation()
	account, err := vault.repository.GetAccount(ctx, accountID)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	if account.State != AccountStatePendingBackup {
		return AccountSummary{}, BackupChallenge{}, ErrBackupChallengeNotFound
	}
	plaintext, err := vault.codec.Open(password, metadataForAccount(account), account.SecretEnvelope)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("resume backup: %w", err)
	}
	defer clear(plaintext)
	privateKey, address, err := deriveSecretIdentity(account.SecretType, plaintext)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	clear(privateKey)
	if !addressesEqual(address, account.Address) {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("pending account identity mismatch")
	}
	words := strings.Fields(string(plaintext))
	if len(words) != 12 {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("unexpected mnemonic length")
	}
	var resumed *Account
	if err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		latest, err := transaction.GetAccount(ctx, account.AccountID)
		if err != nil {
			return err
		}
		if latest.State != AccountStatePendingBackup || latest.Revision != account.Revision || latest.BackupGeneration != account.BackupGeneration {
			return ErrBackupChallengeNotFound
		}
		latest.BackupGeneration++
		latest.UpdatedAt = vault.options.Now().UTC()
		if err := transaction.UpdateAccount(ctx, latest); err != nil {
			return err
		}
		resumed = latest
		return nil
	}); err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	challenge, err := vault.issueBackupChallenge(resumed.AccountID, resumed.BackupGeneration, words)
	if err != nil {
		return summaryFromAccount(resumed), BackupChallenge{}, err
	}
	return summaryFromAccount(resumed), challenge, nil
}

func (vault *WalletVault) ConfirmBackup(ctx context.Context, challengeID string, answers map[int]string) (AccountSummary, error) {
	if err := vault.beginOperation(); err != nil {
		return AccountSummary{}, err
	}
	defer vault.endOperation()
	vault.mu.Lock()
	challenge, exists := vault.challenges[challengeID]
	if !exists {
		vault.mu.Unlock()
		return AccountSummary{}, ErrBackupChallengeNotFound
	}
	if challenge.consuming {
		vault.mu.Unlock()
		return AccountSummary{}, ErrBackupConfirmationInProgress
	}
	if !vault.options.Now().Before(challenge.expiresAt) {
		accountID := challenge.accountID
		vault.mu.Unlock()
		if err := vault.repository.DeletePendingAccount(context.Background(), accountID, challenge.backupGeneration); err != nil {
			if errors.Is(err, ErrAccountNotFound) {
				vault.mu.Lock()
				if stale, exists := vault.challenges[challengeID]; exists {
					clearWords(stale.words)
					delete(vault.challenges, challengeID)
				}
				vault.mu.Unlock()
				return AccountSummary{}, ErrBackupChallengeNotFound
			}
			return AccountSummary{}, fmt.Errorf("expire backup challenge: %w", err)
		}
		vault.mu.Lock()
		if challenge, exists := vault.challenges[challengeID]; exists {
			clearWords(challenge.words)
			delete(vault.challenges, challengeID)
		}
		vault.mu.Unlock()
		return AccountSummary{}, ErrBackupChallengeExpired
	}
	if !backupAnswersMatch(challenge, answers) {
		vault.mu.Unlock()
		return AccountSummary{}, ErrBackupConfirmationFailed
	}
	challenge.consuming = true
	accountID := challenge.accountID
	vault.mu.Unlock()

	var activated *Account
	if err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		account, err := transaction.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if account.State != AccountStatePendingBackup || account.BackupGeneration != challenge.backupGeneration {
			return ErrBackupChallengeNotFound
		}
		account.State = AccountStateActive
		account.UpdatedAt = vault.options.Now().UTC()
		if err := transaction.UpdateAccount(ctx, account); err != nil {
			return err
		}
		activated = account
		return nil
	}); err != nil {
		vault.mu.Lock()
		if challenge, exists := vault.challenges[challengeID]; exists {
			challenge.consuming = false
		}
		vault.mu.Unlock()
		return AccountSummary{}, err
	}
	vault.mu.Lock()
	if challenge, exists := vault.challenges[challengeID]; exists {
		clearWords(challenge.words)
		delete(vault.challenges, challengeID)
	}
	vault.mu.Unlock()
	return summaryFromAccount(activated), nil
}

func (vault *WalletVault) CancelBackup(ctx context.Context, challengeID string) error {
	if err := vault.beginOperation(); err != nil {
		return err
	}
	defer vault.endOperation()
	vault.mu.Lock()
	challenge, exists := vault.challenges[challengeID]
	if !exists {
		vault.mu.Unlock()
		return ErrBackupChallengeNotFound
	}
	if challenge.consuming {
		vault.mu.Unlock()
		return ErrBackupConfirmationInProgress
	}
	accountID := challenge.accountID
	vault.mu.Unlock()
	if err := vault.repository.DeletePendingAccount(ctx, accountID, challenge.backupGeneration); err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			vault.mu.Lock()
			if stale, exists := vault.challenges[challengeID]; exists {
				clearWords(stale.words)
				delete(vault.challenges, challengeID)
			}
			vault.mu.Unlock()
			return ErrBackupChallengeNotFound
		}
		return err
	}
	vault.mu.Lock()
	if challenge, exists := vault.challenges[challengeID]; exists {
		clearWords(challenge.words)
		delete(vault.challenges, challengeID)
	}
	vault.mu.Unlock()
	return nil
}

func (vault *WalletVault) SuspendBackup(challengeID string) error {
	if err := vault.beginOperation(); err != nil {
		return err
	}
	defer vault.endOperation()
	vault.mu.Lock()
	defer vault.mu.Unlock()
	challenge, exists := vault.challenges[challengeID]
	if !exists {
		return ErrBackupChallengeNotFound
	}
	if challenge.consuming {
		return ErrBackupConfirmationInProgress
	}
	clearWords(challenge.words)
	delete(vault.challenges, challengeID)
	return nil
}

func (vault *WalletVault) Unlock(ctx context.Context, accountID string, password []byte) (CapabilityHandle, error) {
	if err := vault.beginOperation(); err != nil {
		return CapabilityHandle{}, err
	}
	defer vault.endOperation()
	if err := ctx.Err(); err != nil {
		return CapabilityHandle{}, err
	}
	account, err := vault.repository.GetAccount(ctx, accountID)
	if err != nil {
		return CapabilityHandle{}, err
	}
	if account.State == AccountStatePendingBackup {
		return CapabilityHandle{}, ErrAccountPendingBackup
	}
	if account.State != AccountStateActive && account.State != AccountStateLocked {
		return CapabilityHandle{}, fmt.Errorf("account is unavailable")
	}
	plaintext, err := vault.codec.Open(password, metadataForAccount(account), account.SecretEnvelope)
	if err != nil {
		return CapabilityHandle{}, fmt.Errorf("unlock account: %w", err)
	}
	defer clear(plaintext)
	privateKey, address, err := deriveSecretIdentity(account.SecretType, plaintext)
	if err != nil {
		return CapabilityHandle{}, err
	}
	if !addressesEqual(address, account.Address) {
		clear(privateKey)
		return CapabilityHandle{}, fmt.Errorf("unlocked account identity mismatch")
	}
	latest, err := vault.repository.GetAccount(ctx, accountID)
	if err != nil {
		clear(privateKey)
		return CapabilityHandle{}, err
	}
	if latest.AuthorizationEpoch != account.AuthorizationEpoch || (latest.State != AccountStateActive && latest.State != AccountStateLocked) {
		clear(privateKey)
		return CapabilityHandle{}, ErrCapabilityExpired
	}
	now := vault.options.Now().UTC()
	if latest.State == AccountStateLocked {
		latest.State = AccountStateActive
		latest.UpdatedAt = now
		if err := vault.repository.UpdateAccount(ctx, latest); err != nil {
			clear(privateKey)
			return CapabilityHandle{}, err
		}
	}
	token, err := newRandomToken(vault.options.Random, 32)
	if err != nil {
		clear(privateKey)
		return CapabilityHandle{}, err
	}
	handle := CapabilityHandle{token: token, accountID: accountID, expiresAt: now.Add(vault.options.SessionTTL)}
	vault.mu.Lock()
	session := &vaultSession{
		accountID:          accountID,
		privateKey:         privateKey,
		capabilities:       latest.Capabilities,
		authorizationEpoch: latest.AuthorizationEpoch,
		expiresAt:          handle.expiresAt,
		lastUsedAt:         now,
	}
	vault.sessions[token] = session
	vault.scheduleSessionExpiryLocked(token, session, now)
	vault.mu.Unlock()
	return handle, nil
}

func (vault *WalletVault) RotatePassword(ctx context.Context, accountID string, oldPassword, newPassword []byte) error {
	if err := vault.beginOperation(); err != nil {
		return err
	}
	defer vault.endOperation()
	if err := validateNewStoragePassword(newPassword); err != nil {
		return err
	}
	if len(oldPassword) == len(newPassword) && subtle.ConstantTimeCompare(oldPassword, newPassword) == 1 {
		return fmt.Errorf("new storage password must differ from old password")
	}
	if err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		account, err := transaction.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if account.State == AccountStatePendingBackup || account.State == AccountStateTombstoned {
			return fmt.Errorf("account cannot rotate password in state %s", account.State)
		}
		oldMetadata := metadataForAccount(account)
		plaintext, err := vault.codec.Open(oldPassword, oldMetadata, account.SecretEnvelope)
		if err != nil {
			return fmt.Errorf("open account for rotation: %w", err)
		}
		defer clear(plaintext)
		account.EnvelopeGeneration++
		account.AuthorizationEpoch++
		newMetadata := metadataForAccount(account)
		rotated, err := vault.codec.Seal(newPassword, newMetadata, plaintext)
		if err != nil {
			return err
		}
		account.SecretEnvelope = rotated
		account.UpdatedAt = vault.options.Now().UTC()
		if err := transaction.UpdateAccount(ctx, account); err != nil {
			return err
		}
		persisted, err := transaction.GetAccount(ctx, account.AccountID)
		if err != nil {
			return err
		}
		reopened, err := vault.codec.Open(newPassword, metadataForAccount(persisted), persisted.SecretEnvelope)
		if err != nil {
			return fmt.Errorf("verify rotated envelope: %w", err)
		}
		defer clear(reopened)
		if !bytes.Equal(reopened, plaintext) {
			return fmt.Errorf("rotated envelope mismatch")
		}
		privateKey, address, err := deriveSecretIdentity(persisted.SecretType, reopened)
		if err != nil {
			return err
		}
		clear(privateKey)
		if !addressesEqual(address, persisted.Address) {
			return fmt.Errorf("rotated envelope identity mismatch")
		}
		return nil
	}); err != nil {
		return err
	}
	vault.lockAccountSessions(accountID)
	return nil
}

func (vault *WalletVault) LockAccount(ctx context.Context, accountID string) error {
	if err := vault.beginOperation(); err != nil {
		return err
	}
	defer vault.endOperation()
	if err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		account, err := transaction.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if account.State != AccountStateActive && account.State != AccountStateLocked {
			return fmt.Errorf("account cannot be locked in state %s", account.State)
		}
		account.State = AccountStateLocked
		account.AuthorizationEpoch++
		account.UpdatedAt = vault.options.Now().UTC()
		return transaction.UpdateAccount(ctx, account)
	}); err != nil {
		return err
	}
	vault.lockAccountSessions(accountID)
	return nil
}

func (vault *WalletVault) Lock(handle CapabilityHandle) error {
	if err := vault.beginOperation(); err != nil {
		return err
	}
	defer vault.endOperation()
	vault.mu.Lock()
	defer vault.mu.Unlock()
	session, exists := vault.sessions[handle.token]
	if !exists || session.accountID != handle.accountID {
		return ErrCapabilityNotFound
	}
	vault.deleteSessionLocked(handle.token, session)
	return nil
}

func (vault *WalletVault) LockAll() {
	if err := vault.beginOperation(); err != nil {
		return
	}
	defer vault.endOperation()
	vault.mu.Lock()
	defer vault.mu.Unlock()
	for token, session := range vault.sessions {
		vault.deleteSessionLocked(token, session)
	}
}

func (vault *WalletVault) Close() {
	vault.lifecycle.Lock()
	defer vault.lifecycle.Unlock()
	if vault.closed {
		return
	}
	vault.closed = true
	vault.mu.Lock()
	defer vault.mu.Unlock()
	for token, session := range vault.sessions {
		vault.deleteSessionLocked(token, session)
	}
	for challengeID, challenge := range vault.challenges {
		clearWords(challenge.words)
		delete(vault.challenges, challengeID)
	}
}

func (vault *WalletVault) ListAccounts(ctx context.Context) ([]AccountSummary, error) {
	if err := vault.beginOperation(); err != nil {
		return nil, err
	}
	defer vault.endOperation()
	accounts, err := vault.repository.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]AccountSummary, 0, len(accounts))
	for index := range accounts {
		summaries = append(summaries, summaryFromAccount(&accounts[index]))
	}
	return summaries, nil
}

func (vault *WalletVault) withPrivateKey(ctx context.Context, handle CapabilityHandle, operation func([]byte, *Account) error) error {
	if err := vault.beginOperation(); err != nil {
		return err
	}
	defer vault.endOperation()
	if err := ctx.Err(); err != nil {
		return err
	}
	vault.mu.Lock()
	defer vault.mu.Unlock()
	session, exists := vault.sessions[handle.token]
	if !exists || session.accountID != handle.accountID {
		return ErrCapabilityNotFound
	}
	now := vault.options.Now().UTC()
	if !now.Before(session.expiresAt) || now.Sub(session.lastUsedAt) >= vault.options.InactivityTTL {
		vault.deleteSessionLocked(handle.token, session)
		return ErrCapabilityExpired
	}
	account, err := vault.repository.GetAccount(ctx, session.accountID)
	if err != nil || account.State != AccountStateActive || account.AuthorizationEpoch != session.authorizationEpoch {
		vault.deleteSessionLocked(handle.token, session)
		return ErrCapabilityExpired
	}
	session.lastUsedAt = now
	vault.scheduleSessionExpiryLocked(handle.token, session, now)
	return operation(session.privateKey, account)
}

func (vault *WalletVault) scheduleSessionExpiryLocked(token string, session *vaultSession, now time.Time) {
	if session.timer != nil {
		session.timer.Stop()
	}
	duration := vault.options.InactivityTTL
	if absolute := session.expiresAt.Sub(now); absolute < duration {
		duration = absolute
	}
	if duration <= 0 {
		duration = time.Nanosecond
	}
	session.timerGeneration++
	generation := session.timerGeneration
	session.timer = time.AfterFunc(duration, func() {
		vault.expireSession(token, session, generation)
	})
}

func (vault *WalletVault) expireSession(token string, expected *vaultSession, generation uint64) {
	if err := vault.beginOperation(); err != nil {
		return
	}
	defer vault.endOperation()
	vault.mu.Lock()
	session, exists := vault.sessions[token]
	if !exists || session != expected || session.timerGeneration != generation {
		vault.mu.Unlock()
		return
	}
	accountID := session.accountID
	epoch := session.authorizationEpoch
	vault.deleteSessionLocked(token, session)
	lastSession := true
	for _, remaining := range vault.sessions {
		if remaining.accountID == accountID {
			lastSession = false
			break
		}
	}
	vault.mu.Unlock()
	if lastSession {
		_ = vault.markAccountLockedIfEpoch(context.Background(), accountID, epoch)
	}
}

func (vault *WalletVault) markAccountLockedIfEpoch(ctx context.Context, accountID string, epoch uint64) error {
	return vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		account, err := transaction.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if account.State != AccountStateActive || account.AuthorizationEpoch != epoch {
			return nil
		}
		account.State = AccountStateLocked
		account.AuthorizationEpoch++
		account.UpdatedAt = vault.options.Now().UTC()
		return transaction.UpdateAccount(ctx, account)
	})
}

func (vault *WalletVault) deleteSessionLocked(token string, session *vaultSession) {
	if session.timer != nil {
		session.timer.Stop()
		session.timer = nil
	}
	clear(session.privateKey)
	delete(vault.sessions, token)
}

func (vault *WalletVault) lockAccountSessions(accountID string) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	for token, session := range vault.sessions {
		if session.accountID == accountID {
			vault.deleteSessionLocked(token, session)
		}
	}
}

func summaryFromAccount(account *Account) AccountSummary {
	if account == nil {
		return AccountSummary{}
	}
	return AccountSummary{
		AccountID:        account.AccountID,
		Name:             account.Name,
		Address:          account.Address,
		SignerKind:       account.SignerKind,
		DerivationScheme: account.DerivationScheme,
		DerivationPath:   account.DerivationPath,
		Capabilities:     account.Capabilities,
		State:            account.State,
		CreatedAt:        account.CreatedAt,
		UpdatedAt:        account.UpdatedAt,
	}
}

func metadataForAccount(account *Account) EnvelopeMetadata {
	return EnvelopeMetadata{
		AccountID:          account.AccountID,
		SecretType:         account.SecretType,
		Address:            account.Address,
		EnvelopeGeneration: account.EnvelopeGeneration,
		Derivation: DerivationMetadata{
			Scheme:       account.DerivationScheme,
			Path:         account.DerivationPath,
			AccountIndex: account.AccountIndex,
			ChangeIndex:  account.ChangeIndex,
			AddressIndex: account.AddressIndex,
			Language:     account.BIP39Language,
		},
	}
}

func deriveSecretIdentity(secretType SecretType, plaintext []byte) ([]byte, string, error) {
	var privateKey []byte
	switch secretType {
	case SecretTypeMnemonic:
		privateKeyHex, err := DerivePrivateKey(string(plaintext))
		if err != nil {
			return nil, "", err
		}
		decoded, err := hex.DecodeString(privateKeyHex)
		if err != nil {
			return nil, "", err
		}
		privateKey = decoded
	case SecretTypePrivateKey:
		if len(plaintext) != 32 {
			return nil, "", fmt.Errorf("private key secret must contain 32 bytes")
		}
		privateKey = append([]byte(nil), plaintext...)
	default:
		return nil, "", fmt.Errorf("unsupported secret type")
	}
	key, err := crypto.ToECDSA(privateKey)
	if err != nil {
		clear(privateKey)
		return nil, "", err
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	key.D.SetInt64(0)
	return privateKey, address, nil
}

func backupAnswersMatch(challenge *backupChallengeState, answers map[int]string) bool {
	if len(answers) != len(challenge.required) {
		return false
	}
	for _, index := range challenge.required {
		answer, exists := answers[index]
		if !exists || index < 0 || index >= len(challenge.words) {
			return false
		}
		expected := []byte(challenge.words[index])
		provided := []byte(strings.TrimSpace(answer))
		if len(expected) != len(provided) || subtle.ConstantTimeCompare(expected, provided) != 1 {
			return false
		}
	}
	return true
}

func (vault *WalletVault) prepareBackupChallenge(accountID string, backupGeneration uint64, words []string) (BackupChallenge, error) {
	challengeID, err := newRandomToken(vault.options.Random, 32)
	if err != nil {
		return BackupChallenge{}, err
	}
	required, err := randomWordIndices(vault.options.Random, len(words), vault.options.ChallengeWords)
	if err != nil {
		return BackupChallenge{}, err
	}
	return BackupChallenge{
		ChallengeID:         challengeID,
		AccountID:           accountID,
		BackupGeneration:    backupGeneration,
		Words:               append([]string(nil), words...),
		RequiredWordIndices: append([]int(nil), required...),
		ExpiresAt:           vault.options.Now().UTC().Add(vault.options.BackupTTL),
	}, nil
}

func (vault *WalletVault) storeBackupChallenge(challenge BackupChallenge) error {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	for existingID, existing := range vault.challenges {
		if existing.accountID == challenge.AccountID {
			if existing.consuming {
				return ErrBackupConfirmationInProgress
			}
			clearWords(existing.words)
			delete(vault.challenges, existingID)
		}
	}
	vault.challenges[challenge.ChallengeID] = &backupChallengeState{
		accountID:        challenge.AccountID,
		backupGeneration: challenge.BackupGeneration,
		words:            append([]string(nil), challenge.Words...),
		required:         append([]int(nil), challenge.RequiredWordIndices...),
		expiresAt:        challenge.ExpiresAt,
	}
	return nil
}

func (vault *WalletVault) issueBackupChallenge(accountID string, backupGeneration uint64, words []string) (BackupChallenge, error) {
	challenge, err := vault.prepareBackupChallenge(accountID, backupGeneration, words)
	if err != nil {
		return BackupChallenge{}, err
	}
	if err := vault.storeBackupChallenge(challenge); err != nil {
		clearWords(challenge.Words)
		return BackupChallenge{}, err
	}
	return challenge, nil
}

func randomWordIndices(random io.Reader, wordCount, requiredCount int) ([]int, error) {
	if wordCount <= 0 || requiredCount <= 0 || requiredCount > wordCount {
		return nil, fmt.Errorf("invalid backup challenge size")
	}
	indices := make([]int, wordCount)
	for index := range indices {
		indices[index] = index
	}
	for index := wordCount - 1; index > 0; index-- {
		selected, err := rand.Int(random, big.NewInt(int64(index+1)))
		if err != nil {
			return nil, err
		}
		swap := int(selected.Int64())
		indices[index], indices[swap] = indices[swap], indices[index]
	}
	return append([]int(nil), indices[:requiredCount]...), nil
}

func newUUID(random io.Reader) (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func newRandomToken(random io.Reader, byteCount int) (string, error) {
	if byteCount < 16 {
		return "", fmt.Errorf("random token must contain at least 16 bytes")
	}
	value := make([]byte, byteCount)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func clearWords(words []string) {
	for index := range words {
		words[index] = ""
	}
}

func joinMnemonicWords(words []string) string {
	return strings.Join(words, " ")
}

func addressesEqual(first, second string) bool {
	return common.IsHexAddress(first) && common.IsHexAddress(second) && common.HexToAddress(first) == common.HexToAddress(second)
}
