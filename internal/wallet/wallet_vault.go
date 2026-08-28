package wallet

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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
	SourceIdentityKey []byte
	BackupTTL         time.Duration
	SessionTTL        time.Duration
	InactivityTTL     time.Duration
	ChallengeWords    int
}

type CreateAccountRequest struct {
	Name            string
	Password        []byte
	WordCount       int
	BIP39Language   BIP39Language
	BIP39Passphrase string
	DerivationPath  string
}

type AccountSummary struct {
	AccountID          string
	Name               string
	Address            string
	SignerKind         SignerKind
	DerivationScheme   string
	DerivationPath     string
	BIP39Language      string
	HasBIP39Passphrase bool
	RelatedAccountID   string
	Capabilities       AccountCapability
	State              AccountState
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type BackupChallenge struct {
	ChallengeID                  string
	AccountID                    string
	BackupGeneration             uint64
	Words                        []string
	RequiredWordIndices          []int
	DerivationPath               string
	BIP39Language                BIP39Language
	RequiresMaterialConfirmation bool
	ExpiresAt                    time.Time
	passphraseMAC                []byte
}

type BackupMaterialConfirmation struct {
	WordAnswers     map[int]string
	BIP39Passphrase string
	DerivationPath  string
	BIP39Language   BIP39Language
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
	passphraseMAC    []byte
	derivationPath   string
	language         BIP39Language
	requiresMaterial bool
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
	repository        AccountRepository
	codec             SecretEnvelope
	options           VaultOptions
	sourceIdentityKey []byte

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
	if len(options.SourceIdentityKey) != 0 && len(options.SourceIdentityKey) != 32 {
		return nil, fmt.Errorf("source identity key must contain 32 bytes")
	}
	sourceIdentityKey := append([]byte(nil), options.SourceIdentityKey...)
	options.SourceIdentityKey = nil
	if len(sourceIdentityKey) == 32 {
		fingerprintBytes := sha256.Sum256(sourceIdentityKey)
		fingerprint := hex.EncodeToString(fingerprintBytes[:])
		stored, err := repository.GetVaultMetadata(context.Background(), "source_identity_key_sha256")
		if errors.Is(err, ErrAccountNotFound) {
			err = repository.PutVaultMetadata(context.Background(), "source_identity_key_sha256", fingerprint)
		}
		if err != nil {
			clear(sourceIdentityKey)
			return nil, fmt.Errorf("bind source identity key: %w", err)
		}
		if stored != "" && (len(stored) != len(fingerprint) || subtle.ConstantTimeCompare([]byte(stored), []byte(fingerprint)) != 1) {
			clear(sourceIdentityKey)
			return nil, fmt.Errorf("source identity key does not match vault metadata")
		}
	}
	return &WalletVault{
		repository:        repository,
		codec:             codec,
		options:           options,
		sourceIdentityKey: sourceIdentityKey,
		challenges:        make(map[string]*backupChallengeState),
		sessions:          make(map[string]*vaultSession),
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
	wordCount := request.WordCount
	if wordCount == 0 {
		wordCount = 12
	}
	language := request.BIP39Language
	if language == "" {
		language = BIP39English
	}
	pathValue := request.DerivationPath
	if pathValue == "" {
		pathValue = "m/44'/60'/0'/0/0"
	}
	path, err := ParseDerivationPath(pathValue)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	var mnemonic string
	if wordCount == 12 && language == BIP39English {
		mnemonic, err = vault.options.MnemonicGenerator()
	} else {
		mnemonic, err = generateMnemonicForLanguage(wordCount, language)
	}
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("generate mnemonic: %w", err)
	}
	if err := ValidateBIP39Mnemonic(mnemonic, language); err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	secret := canonicalSecretV1{
		Version:         canonicalSecretVersion,
		Kind:            SecretTypeMnemonic,
		Mnemonic:        mnemonic,
		BIP39Passphrase: request.BIP39Passphrase,
		BIP39Language:   language,
		DerivationPath:  path.String(),
	}
	canonicalSecret, err := encodeCanonicalSecret(secret)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	defer clear(canonicalSecret)
	secret, err = decodeCanonicalSecret(canonicalSecret)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	words := strings.Fields(secret.Mnemonic)
	accountID, err := newUUID(vault.options.Random)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	privateKey, address, err := deriveCanonicalSecretIdentity(secret)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	clear(privateKey)
	metadata := EnvelopeMetadata{
		AccountID:          accountID,
		SecretType:         SecretTypeMnemonic,
		Address:            address,
		EnvelopeGeneration: 1,
		PassphrasePresent:  secret.BIP39Passphrase != "",
		Derivation:         derivationMetadataForPath(path, language),
	}
	envelope, err := vault.codec.Seal(request.Password, metadata, canonicalSecret)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	sourceIdentity := "generated:" + accountID
	if len(vault.sourceIdentityKey) == 32 {
		sourceIdentity = vault.deriveSourceIdentity(canonicalSecret)
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
		AccountIndex:       metadata.Derivation.AccountIndex,
		ChangeIndex:        metadata.Derivation.ChangeIndex,
		AddressIndex:       metadata.Derivation.AddressIndex,
		BIP39Language:      metadata.Derivation.Language,
		HasBIP39Passphrase: secret.BIP39Passphrase != "",
		Capabilities:       CapabilitySignTransaction | CapabilitySignMessage | CapabilityExportSecret,
		State:              AccountStatePendingBackup,
		SecretEnvelope:     envelope,
		EnvelopeGeneration: 1,
		AuthorizationEpoch: 1,
		BackupGeneration:   1,
		SourceIdentity:     sourceIdentity,
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	challenge, err := vault.prepareBackupChallenge(accountID, account.BackupGeneration, mnemonicBackupMaterial{
		words:      words,
		passphrase: secret.BIP39Passphrase,
		path:       secret.DerivationPath,
		language:   secret.BIP39Language,
	})
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
		if !bytes.Equal(reopened, canonicalSecret) {
			return fmt.Errorf("pending envelope plaintext mismatch")
		}
		verificationKey, verificationAddress, err := deriveStoredSecretIdentity(persisted, reopened)
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
		clear(challenge.passphraseMAC)
		return AccountSummary{}, BackupChallenge{}, err
	}
	if err := vault.storeBackupChallenge(challenge); err != nil {
		clearWords(challenge.Words)
		clear(challenge.passphraseMAC)
		return summaryFromAccount(account), BackupChallenge{}, err
	}
	clear(challenge.passphraseMAC)
	challenge.passphraseMAC = nil
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
	privateKey, address, err := deriveStoredSecretIdentity(account, plaintext)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
	}
	clear(privateKey)
	if !addressesEqual(address, account.Address) {
		return AccountSummary{}, BackupChallenge{}, fmt.Errorf("pending account identity mismatch")
	}
	backupMaterial, err := mnemonicBackupMaterialFromStoredSecret(account, plaintext)
	if err != nil {
		return AccountSummary{}, BackupChallenge{}, err
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
	challenge, err := vault.issueBackupChallenge(resumed.AccountID, resumed.BackupGeneration, backupMaterial)
	if err != nil {
		return summaryFromAccount(resumed), BackupChallenge{}, err
	}
	return summaryFromAccount(resumed), challenge, nil
}

func (vault *WalletVault) ConfirmBackup(ctx context.Context, challengeID string, answers map[int]string) (AccountSummary, error) {
	return vault.confirmBackup(ctx, challengeID, BackupMaterialConfirmation{WordAnswers: answers}, false)
}

func (vault *WalletVault) ConfirmBackupWithMaterial(ctx context.Context, challengeID string, confirmation BackupMaterialConfirmation) (AccountSummary, error) {
	return vault.confirmBackup(ctx, challengeID, confirmation, true)
}

func (vault *WalletVault) confirmBackup(ctx context.Context, challengeID string, confirmation BackupMaterialConfirmation, materialProvided bool) (AccountSummary, error) {
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
					clear(stale.passphraseMAC)
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
			clear(challenge.passphraseMAC)
			delete(vault.challenges, challengeID)
		}
		vault.mu.Unlock()
		return AccountSummary{}, ErrBackupChallengeExpired
	}
	if !backupAnswersMatch(challenge, confirmation.WordAnswers) {
		vault.mu.Unlock()
		return AccountSummary{}, ErrBackupConfirmationFailed
	}
	confirmedAddress := ""
	if materialProvided {
		if !backupMaterialMatches(challengeID, challenge, confirmation) {
			vault.mu.Unlock()
			return AccountSummary{}, ErrBackupConfirmationFailed
		}
		path, _ := ParseDerivationPath(confirmation.DerivationPath)
		privateKey, address, err := deriveEVMAccount(strings.Join(challenge.words, " "), confirmation.BIP39Passphrase, confirmation.BIP39Language, path)
		if err != nil {
			vault.mu.Unlock()
			return AccountSummary{}, ErrBackupConfirmationFailed
		}
		clear(privateKey)
		confirmedAddress = address
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
		if confirmedAddress != "" && !addressesEqual(confirmedAddress, account.Address) {
			return ErrBackupConfirmationFailed
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
		clear(challenge.passphraseMAC)
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
				clear(stale.passphraseMAC)
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
		clear(challenge.passphraseMAC)
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
	clear(challenge.passphraseMAC)
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
	if account.SignerKind != SignerKindSoftware {
		return CapabilityHandle{}, ErrCapabilityDenied
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
	privateKey, address, err := deriveStoredSecretIdentity(account, plaintext)
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
		if account.SignerKind != SignerKindSoftware {
			return ErrCapabilityDenied
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
		privateKey, address, err := deriveStoredSecretIdentity(persisted, reopened)
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
		if account.SignerKind != SignerKindSoftware {
			return ErrCapabilityDenied
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

func (vault *WalletVault) AuthorizationEpoch(ctx context.Context, handle CapabilityHandle) (uint64, error) {
	var epoch uint64
	err := vault.withPrivateKey(ctx, handle, func(_ []byte, account *Account) error {
		epoch = account.AuthorizationEpoch
		return nil
	})
	if err != nil {
		return 0, err
	}
	return epoch, nil
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
		clear(challenge.passphraseMAC)
		delete(vault.challenges, challengeID)
	}
	clear(vault.sourceIdentityKey)
	vault.sourceIdentityKey = nil
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
		AccountID:          account.AccountID,
		Name:               account.Name,
		Address:            account.Address,
		SignerKind:         account.SignerKind,
		DerivationScheme:   account.DerivationScheme,
		DerivationPath:     account.DerivationPath,
		BIP39Language:      account.BIP39Language,
		HasBIP39Passphrase: account.HasBIP39Passphrase,
		RelatedAccountID:   account.RelatedAccountID,
		Capabilities:       account.Capabilities,
		State:              account.State,
		CreatedAt:          account.CreatedAt,
		UpdatedAt:          account.UpdatedAt,
	}
}

func metadataForAccount(account *Account) EnvelopeMetadata {
	return EnvelopeMetadata{
		AccountID:          account.AccountID,
		SecretType:         account.SecretType,
		Address:            account.Address,
		EnvelopeGeneration: account.EnvelopeGeneration,
		PassphrasePresent:  account.HasBIP39Passphrase,
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
		privateKeyHex, err := derivePrivateKeyLegacy(string(plaintext))
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

func backupMaterialMatches(challengeID string, challenge *backupChallengeState, confirmation BackupMaterialConfirmation) bool {
	path, err := ParseDerivationPath(confirmation.DerivationPath)
	if err != nil || path.String() != challenge.derivationPath || confirmation.BIP39Language != challenge.language {
		return false
	}
	if len(confirmation.BIP39Passphrase) > maxBIP39PassphraseLength {
		return false
	}
	passphraseMAC := hmac.New(sha256.New, []byte(challengeID))
	_, _ = passphraseMAC.Write([]byte(normalizeBIP39Passphrase(confirmation.BIP39Passphrase)))
	return hmac.Equal(challenge.passphraseMAC, passphraseMAC.Sum(nil))
}

func (vault *WalletVault) prepareBackupChallenge(accountID string, backupGeneration uint64, material mnemonicBackupMaterial) (BackupChallenge, error) {
	challengeID, err := newRandomToken(vault.options.Random, 32)
	if err != nil {
		return BackupChallenge{}, err
	}
	required, err := randomWordIndices(vault.options.Random, len(material.words), vault.options.ChallengeWords)
	if err != nil {
		return BackupChallenge{}, err
	}
	passphraseMAC := hmac.New(sha256.New, []byte(challengeID))
	_, _ = passphraseMAC.Write([]byte(normalizeBIP39Passphrase(material.passphrase)))
	path := material.path
	if path == "" {
		path = "m/44'/60'/0'/0/0"
	}
	language := material.language
	if language == "" {
		language = BIP39English
	}
	requiresMaterial := material.passphrase != "" || path != "m/44'/60'/0'/0/0" || language != BIP39English
	return BackupChallenge{
		ChallengeID:                  challengeID,
		AccountID:                    accountID,
		BackupGeneration:             backupGeneration,
		Words:                        append([]string(nil), material.words...),
		RequiredWordIndices:          append([]int(nil), required...),
		DerivationPath:               path,
		BIP39Language:                language,
		RequiresMaterialConfirmation: requiresMaterial,
		ExpiresAt:                    vault.options.Now().UTC().Add(vault.options.BackupTTL),
		passphraseMAC:                passphraseMAC.Sum(nil),
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
			clear(existing.passphraseMAC)
			delete(vault.challenges, existingID)
		}
	}
	vault.challenges[challenge.ChallengeID] = &backupChallengeState{
		accountID:        challenge.AccountID,
		backupGeneration: challenge.BackupGeneration,
		words:            append([]string(nil), challenge.Words...),
		required:         append([]int(nil), challenge.RequiredWordIndices...),
		passphraseMAC:    append([]byte(nil), challenge.passphraseMAC...),
		derivationPath:   challenge.DerivationPath,
		language:         challenge.BIP39Language,
		requiresMaterial: challenge.RequiresMaterialConfirmation,
		expiresAt:        challenge.ExpiresAt,
	}
	return nil
}

func (vault *WalletVault) issueBackupChallenge(accountID string, backupGeneration uint64, material mnemonicBackupMaterial) (BackupChallenge, error) {
	challenge, err := vault.prepareBackupChallenge(accountID, backupGeneration, material)
	if err != nil {
		return BackupChallenge{}, err
	}
	if err := vault.storeBackupChallenge(challenge); err != nil {
		clearWords(challenge.Words)
		clear(challenge.passphraseMAC)
		return BackupChallenge{}, err
	}
	clear(challenge.passphraseMAC)
	challenge.passphraseMAC = nil
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
