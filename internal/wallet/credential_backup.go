package wallet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"blocowallet/internal/keepass"
)

var (
	ErrCredentialBackupRequired    = errors.New("credential backup requires an active managed operation context")
	ErrCredentialBackupUnavailable = errors.New("credential backup is unavailable")
	ErrCredentialBackupConflict    = errors.New("credential backup state conflict")
)

const (
	credentialItemAccount       = "account"
	credentialKindKeystoreV3    = "keystore_v3"
	credentialKindEncryptedFile = "bloco_encrypted"
	credentialOperationTTL      = 10 * time.Minute
)

type CredentialBackupPolicy struct {
	Enabled bool
	Binding keepass.Binding
}

type CredentialBackupReport struct {
	AccountsSaved int
	FilesSaved    int
	Deleted       int
	Pending       int
	BackupPaths   []string
	Failures      []CredentialBackupFailure
}

type CredentialBackupFailure struct {
	AccountID string
	ItemID    string
	Code      string
}

type CredentialBackupPendingError struct {
	Cause error
}

func (e *CredentialBackupPendingError) Error() string {
	return "wallet operation completed; credential backup is pending"
}

func (e *CredentialBackupPendingError) Unwrap() error {
	return e.Cause
}

type CredentialBackupDeleteIntent struct {
	TargetID    string
	VaultID     string
	OperationID string
}

type AccountCredentialDeletionRepository interface {
	DeleteAccountWithCredentialBackup(ctx context.Context, accountID string, expectedRevision uint64, deletedAt time.Time, intent CredentialBackupDeleteIntent) error
}

type credentialArtifact struct {
	Kind       string
	Name       string
	Path       string
	Ciphertext []byte
	Password   []byte
}

func (credentialArtifact) String() string   { return "credentialArtifact{redacted}" }
func (credentialArtifact) GoString() string { return "credentialArtifact{redacted}" }
func (credentialArtifact) MarshalJSON() ([]byte, error) {
	return nil, errCredentialSerialization
}

var errCredentialSerialization = errors.New("credential backup material cannot be serialized")

type queuedAccount struct {
	accountID  string
	generation uint64
	revision   uint64
	epoch      uint64
	password   []byte
}

func (q *queuedAccount) clear() {
	if q == nil {
		return
	}
	clear(q.password)
	q.password = nil
}

type queuedFile struct {
	accountID string
	itemID    string
	kind      string
	name      string
	path      string
	digest    string
	password  []byte
}

func (q *queuedFile) clear() {
	if q == nil {
		return
	}
	clear(q.password)
	q.password = nil
}

type credentialOperationContextKey struct{}

type CredentialBackupService struct {
	vault      *WalletVault
	repository CredentialBackupRepository
	store      *keepass.Store

	mu         sync.Mutex
	policy     CredentialBackupPolicy
	generation uint64
	ops        map[*CredentialBackupOperation]struct{}
	closed     bool
}

func NewCredentialBackupService(vault *WalletVault, store *keepass.Store) (*CredentialBackupService, error) {
	if vault == nil || store == nil {
		return nil, fmt.Errorf("credential backup requires a vault and a store")
	}
	service := &CredentialBackupService{
		vault: vault,
		store: store,
		ops:   make(map[*CredentialBackupOperation]struct{}),
	}
	if repository, ok := vault.repository.(CredentialBackupRepository); ok {
		service.repository = repository
	}
	vault.credentialMu.Lock()
	if existing := vault.credentialService; existing != nil {
		existing.mu.Lock()
		existingClosed := existing.closed
		existing.mu.Unlock()
		if !existingClosed {
			vault.credentialMu.Unlock()
			return nil, fmt.Errorf("credential backup service already attached")
		}
	}
	vault.credentialService = service
	vault.credentialMu.Unlock()
	return service, nil
}

func (s *CredentialBackupService) Configure(ctx context.Context, policy CredentialBackupPolicy) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrCredentialBackupUnavailable
	}
	if policy.Enabled {
		if s.repository == nil {
			return ErrCredentialBackupUnavailable
		}
		if !filepath.IsAbs(policy.Binding.Path) || !strings.EqualFold(filepath.Ext(policy.Binding.Path), ".kdbx") ||
			!uuidV4(policy.Binding.TargetID) || !uuidV4(policy.Binding.VaultID) {
			return ErrCredentialBackupConflict
		}
		vaultID, err := s.vault.CredentialVaultID(ctx)
		if err != nil {
			return err
		}
		if vaultID != policy.Binding.VaultID {
			return ErrCredentialBackupConflict
		}
	} else if policy.Binding != (keepass.Binding{}) &&
		(!filepath.IsAbs(policy.Binding.Path) || !strings.EqualFold(filepath.Ext(policy.Binding.Path), ".kdbx") ||
			!uuidV4(policy.Binding.TargetID) || !uuidV4(policy.Binding.VaultID)) {
		return ErrCredentialBackupConflict
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrCredentialBackupUnavailable
	}
	s.policy = policy
	s.generation++
	ops := make([]*CredentialBackupOperation, 0, len(s.ops))
	for op := range s.ops {
		ops = append(ops, op)
	}
	s.ops = make(map[*CredentialBackupOperation]struct{})
	s.mu.Unlock()
	for _, op := range ops {
		op.Close()
	}
	return nil
}

func (s *CredentialBackupService) Policy() CredentialBackupPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy
}

func (s *CredentialBackupService) Status(ctx context.Context, accountID string) ([]CredentialBackupState, error) {
	if !uuidV4(accountID) {
		return nil, fmt.Errorf("account id must be a canonical UUIDv4")
	}
	s.mu.Lock()
	policy := s.policy
	repository := s.repository
	closed := s.closed
	s.mu.Unlock()
	if closed || !policy.Enabled || repository == nil {
		return nil, ErrCredentialBackupUnavailable
	}
	rows, err := repository.ListCredentialBackups(ctx, policy.Binding.TargetID, policy.Binding.VaultID)
	if err != nil {
		return nil, err
	}
	filtered := rows[:0]
	for _, row := range rows {
		if row.AccountID == accountID {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func (s *CredentialBackupService) Begin(ctx context.Context, master []byte) (*CredentialBackupOperation, error) {
	s.mu.Lock()
	policy := s.policy
	generation := s.generation
	closed := s.closed
	s.mu.Unlock()
	if closed || !policy.Enabled {
		return nil, ErrCredentialBackupUnavailable
	}
	storeOp, err := s.store.Open(ctx, policy.Binding, master)
	if err != nil {
		return nil, err
	}
	if err := storeOp.Preflight(ctx); err != nil {
		storeOp.Close()
		return nil, err
	}
	operationID, err := newUUID(s.vault.options.Random)
	if err != nil {
		storeOp.Close()
		return nil, err
	}
	child, cancel := context.WithTimeout(ctx, credentialOperationTTL)
	op := &CredentialBackupOperation{
		service:    s,
		id:         operationID,
		generation: generation,
		cancel:     cancel,
		store:      storeOp,
		binding:    policy.Binding,
		accounts:   make(map[string]*queuedAccount),
		files:      make(map[string]*queuedFile),
		deletions:  make(map[string]struct{}),
	}
	op.ctx = context.WithValue(child, credentialOperationContextKey{}, op)
	op.mu.Lock()
	op.stopAfterFunc = context.AfterFunc(op.ctx, func() {
		op.Close()
	})
	ctxErr := op.ctx.Err()
	op.mu.Unlock()
	if ctxErr != nil {
		op.Close()
		return nil, ErrCredentialBackupUnavailable
	}
	s.mu.Lock()
	if s.closed || s.generation != generation {
		s.mu.Unlock()
		op.Close()
		return nil, ErrCredentialBackupUnavailable
	}
	s.ops[op] = struct{}{}
	s.mu.Unlock()
	return op, nil
}

func (s *CredentialBackupService) Pending(ctx context.Context) ([]CredentialBackupState, error) {
	s.mu.Lock()
	policy := s.policy
	repository := s.repository
	s.mu.Unlock()
	if !policy.Enabled || repository == nil {
		return nil, ErrCredentialBackupUnavailable
	}
	rows, err := repository.ListCredentialBackups(ctx, policy.Binding.TargetID, policy.Binding.VaultID)
	if err != nil {
		return nil, err
	}
	pending := make([]CredentialBackupState, 0, len(rows))
	for _, row := range rows {
		if row.State != CredentialBackupStateSynced {
			pending = append(pending, row)
		}
	}
	return pending, nil
}

func (s *CredentialBackupService) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	ops := make([]*CredentialBackupOperation, 0, len(s.ops))
	for op := range s.ops {
		ops = append(ops, op)
	}
	s.ops = make(map[*CredentialBackupOperation]struct{})
	s.mu.Unlock()
	for _, op := range ops {
		op.Close()
	}
}

func (s *CredentialBackupService) String() string {
	return "wallet.CredentialBackupService"
}

func (s *CredentialBackupService) GoString() string {
	return s.String()
}

func (s *CredentialBackupService) MarshalJSON() ([]byte, error) {
	return nil, errCredentialSerialization
}

type credentialStoreOperation interface {
	Preflight(context.Context) error
	Upsert(context.Context, []keepass.Record) (keepass.CommitResult, error)
	RemoveAccount(context.Context, string) (keepass.CommitResult, error)
	WithPassword(context.Context, keepass.Ref, uint64, func([]byte) error) error
	WithFilePassword(context.Context, string, string, func([]byte) error) error
	List(context.Context) ([]keepass.Metadata, error)
	Close()
}

type CredentialBackupOperation struct {
	service    *CredentialBackupService
	id         string
	generation uint64
	binding    keepass.Binding

	ctx    context.Context
	cancel context.CancelFunc
	store  credentialStoreOperation

	stopAfterFunc func() bool

	mu        sync.Mutex
	closed    bool
	sealed    bool
	syncing   bool
	inflight  int
	accounts  map[string]*queuedAccount
	files     map[string]*queuedFile
	deletions map[string]struct{}
}

func (op *CredentialBackupOperation) Context() context.Context {
	if op == nil {
		return context.Background()
	}
	return op.ctx
}

func (op *CredentialBackupOperation) ID() string {
	if op == nil {
		return ""
	}
	return op.id
}

func (op *CredentialBackupOperation) String() string {
	return fmt.Sprintf("wallet.CredentialBackupOperation{target:%s}", op.binding.TargetID)
}

func (op *CredentialBackupOperation) GoString() string {
	return op.String()
}

func (op *CredentialBackupOperation) MarshalJSON() ([]byte, error) {
	return nil, errCredentialSerialization
}

func (op *CredentialBackupOperation) alive() error {
	if op == nil {
		return ErrCredentialBackupUnavailable
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.aliveLocked()
}

func (op *CredentialBackupOperation) aliveLocked() error {
	if op.closed {
		return ErrCredentialBackupUnavailable
	}
	if err := op.ctx.Err(); err != nil {
		return err
	}
	op.service.mu.Lock()
	defer op.service.mu.Unlock()
	if op.service.closed || op.service.generation != op.generation {
		return ErrCredentialBackupUnavailable
	}
	return nil
}

func (op *CredentialBackupOperation) aliveMutable() error {
	if op == nil {
		return ErrCredentialBackupUnavailable
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	return op.aliveMutableLocked()
}

func (op *CredentialBackupOperation) aliveMutableLocked() error {
	if err := op.aliveLocked(); err != nil {
		return err
	}
	if op.sealed {
		return ErrCredentialBackupUnavailable
	}
	return nil
}

func (op *CredentialBackupOperation) beginMutation() error {
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveMutableLocked(); err != nil {
		return err
	}
	op.inflight++
	return nil
}

func (op *CredentialBackupOperation) endMutation() {
	op.mu.Lock()
	op.inflight--
	op.mu.Unlock()
}

func (op *CredentialBackupOperation) bindContext(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(op.ctx, cancel)
	return context.WithValue(child, credentialOperationContextKey{}, op), func() {
		stop()
		cancel()
	}
}

func queuedFileKey(accountID, itemID string) string {
	return accountID + "|" + itemID
}

func (op *CredentialBackupOperation) queueCommittedAccount(accountID string, generation, revision, epoch uint64, password []byte, artifacts []credentialArtifact) error {
	if op == nil {
		return nil
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveMutableLocked(); err != nil {
		return err
	}
	entry := &queuedAccount{
		accountID:  accountID,
		generation: generation,
		revision:   revision,
		epoch:      epoch,
		password:   append([]byte(nil), password...),
	}
	if old := op.accounts[accountID]; old != nil {
		old.clear()
	}
	op.accounts[accountID] = entry
	for _, artifact := range artifacts {
		digest := sha256.Sum256(artifact.Ciphertext)
		itemID := "file:" + artifact.Kind + ":" + hex.EncodeToString(digest[:])
		file := &queuedFile{
			accountID: accountID,
			itemID:    itemID,
			kind:      artifact.Kind,
			name:      artifact.Name,
			path:      artifact.Path,
			digest:    hex.EncodeToString(digest[:]),
			password:  append([]byte(nil), artifact.Password...),
		}
		fileKey := queuedFileKey(accountID, itemID)
		if old := op.files[fileKey]; old != nil {
			old.clear()
		}
		op.files[fileKey] = file
	}
	delete(op.deletions, accountID)
	return nil
}

func (op *CredentialBackupOperation) refreshQueuedAccount(accountID string, generation, revision, epoch uint64) error {
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveMutableLocked(); err != nil {
		return err
	}
	entry := op.accounts[accountID]
	if entry == nil {
		return ErrCredentialBackupUnavailable
	}
	entry.generation = generation
	entry.revision = revision
	entry.epoch = epoch
	return nil
}

func (op *CredentialBackupOperation) queueFileArtifact(accountID string, artifact credentialArtifact) error {
	if op == nil {
		return nil
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveMutableLocked(); err != nil {
		return err
	}
	digest := sha256.Sum256(artifact.Ciphertext)
	itemID := "file:" + artifact.Kind + ":" + hex.EncodeToString(digest[:])
	file := &queuedFile{
		accountID: accountID,
		itemID:    itemID,
		kind:      artifact.Kind,
		name:      artifact.Name,
		path:      artifact.Path,
		digest:    hex.EncodeToString(digest[:]),
		password:  append([]byte(nil), artifact.Password...),
	}
	fileKey := queuedFileKey(accountID, itemID)
	if old := op.files[fileKey]; old != nil {
		old.clear()
	}
	op.files[fileKey] = file
	return nil
}

func (op *CredentialBackupOperation) queueDeletion(accountID string) {
	if op == nil {
		return
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.closed {
		return
	}
	if old := op.accounts[accountID]; old != nil {
		old.clear()
		delete(op.accounts, accountID)
	}
	op.deletions[accountID] = struct{}{}
}

func (op *CredentialBackupOperation) WithAccountPassword(ctx context.Context, accountID string, fn func([]byte) error) error {
	if op == nil || fn == nil {
		return fmt.Errorf("password callback is required")
	}
	ctx, release := op.bindContext(ctx)
	defer release()
	if err := op.alive(); err != nil {
		return err
	}
	account, err := op.service.vault.repository.GetAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if account.SignerKind != SignerKindSoftware {
		return ErrCapabilityDenied
	}
	if account.State != AccountStateActive && account.State != AccountStateLocked && account.State != AccountStateUnavailable {
		return ErrRecoveryUnavailable
	}
	vault := op.service.vault
	ref := keepass.Ref{VaultID: op.binding.VaultID, AccountID: accountID, ItemID: credentialItemAccount}
	return op.store.WithPassword(ctx, ref, account.EnvelopeGeneration, func(password []byte) error {
		if err := vault.withVerifiedRecoveryMaterial(ctx, accountID, password, false, true, nil, func(*Account, canonicalSecretV1, []byte) error {
			return nil
		}); err != nil {
			return err
		}
		return fn(password)
	})
}

func (op *CredentialBackupOperation) WithFilePassword(ctx context.Context, kind string, ciphertext []byte, fn func([]byte) error) error {
	if op == nil || fn == nil {
		return fmt.Errorf("password callback is required")
	}
	if kind != credentialKindKeystoreV3 && kind != credentialKindEncryptedFile {
		return fmt.Errorf("%w: file kind is outside the managed schema", ErrCredentialBackupUnavailable)
	}
	ctx, release := op.bindContext(ctx)
	defer release()
	if err := op.alive(); err != nil {
		return err
	}
	digest := sha256.Sum256(ciphertext)
	return op.store.WithFilePassword(ctx, kind, hex.EncodeToString(digest[:]), fn)
}

func (op *CredentialBackupOperation) BackupAccount(ctx context.Context, accountID string, password []byte) error {
	if op == nil {
		return ErrCredentialBackupUnavailable
	}
	ctx, release := op.bindContext(ctx)
	defer release()
	if err := op.beginMutation(); err != nil {
		return err
	}
	defer op.endMutation()
	vault := op.service.vault
	var persisted *Account
	err := vault.withVerifiedRecoveryMaterial(ctx, accountID, password, true, false, nil, func(account *Account, _ canonicalSecretV1, _ []byte) error {
		persisted = account
		return nil
	})
	if err != nil {
		return err
	}
	if err := op.aliveMutable(); err != nil {
		return err
	}
	var recorded *Account
	if err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		latest, err := transaction.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if latest.Revision != persisted.Revision || latest.AuthorizationEpoch != persisted.AuthorizationEpoch {
			return ErrAccountRevisionConflict
		}
		if err := op.aliveMutable(); err != nil {
			return err
		}
		if err := vault.recordAccountIntent(ctx, transaction, latest); err != nil {
			return err
		}
		recorded = latest
		return nil
	}); err != nil {
		return err
	}
	return op.queueCommittedAccount(recorded.AccountID, recorded.EnvelopeGeneration, recorded.Revision, recorded.AuthorizationEpoch, password, nil)
}

func (op *CredentialBackupOperation) RetryArtifact(ctx context.Context, key CredentialBackupKey, ciphertext, password []byte, sourcePath string) error {
	if op == nil {
		return ErrCredentialBackupUnavailable
	}
	ctx, release := op.bindContext(ctx)
	defer release()
	if err := op.beginMutation(); err != nil {
		return err
	}
	defer op.endMutation()
	if err := key.Validate(); err != nil {
		return err
	}
	if key.TargetID != op.binding.TargetID || key.VaultID != op.binding.VaultID {
		return ErrCredentialBackupConflict
	}
	repository := op.service.repository
	if repository == nil {
		return ErrCredentialBackupUnavailable
	}
	row, err := repository.GetCredentialBackup(ctx, key)
	if err != nil {
		return err
	}
	if row.Operation != CredentialBackupOperationUpsert || (row.State != CredentialBackupStatePending && row.State != CredentialBackupStatePrepared) {
		return ErrCredentialBackupConflict
	}
	digest := sha256.Sum256(ciphertext)
	digestHex := hex.EncodeToString(digest[:])
	if row.ArtifactDigest != digestHex || key.ItemID != "file:"+row.ArtifactKind+":"+digestHex {
		return ErrCredentialBackupConflict
	}
	account, err := op.service.vault.repository.GetAccount(ctx, key.AccountID)
	if err != nil {
		return err
	}
	var preview ImportPreview
	switch row.ArtifactKind {
	case credentialKindKeystoreV3:
		preview, err = PreviewKeystoreImportContext(ctx, ciphertext, password)
	case credentialKindEncryptedFile:
		preview, err = op.service.vault.PreviewEncryptedAccountImport(ctx, ciphertext, password)
	default:
		err = ErrCredentialBackupConflict
	}
	if err != nil {
		return err
	}
	if !addressesEqual(preview.Address, account.Address) {
		return ErrCredentialBackupConflict
	}
	if row.State == CredentialBackupStatePrepared || row.OperationID != op.id {
		updated := row
		updated.State = CredentialBackupStatePending
		updated.OperationID = op.id
		if sourcePath != "" {
			updated.ArtifactPath = sourcePath
			updated.ArtifactName = filepath.Base(sourcePath)
		}
		if err := repository.PutCredentialBackup(ctx, updated, row.Revision); err != nil {
			return err
		}
	}
	name := row.ArtifactName
	path := row.ArtifactPath
	if sourcePath != "" {
		name = filepath.Base(sourcePath)
		path = sourcePath
	}
	return op.queueFileArtifact(key.AccountID, credentialArtifact{
		Kind:       row.ArtifactKind,
		Name:       name,
		Path:       path,
		Ciphertext: ciphertext,
		Password:   password,
	})
}

func (op *CredentialBackupOperation) RetryDeletion(ctx context.Context, accountID, confirmAccountID string) error {
	if op == nil {
		return ErrCredentialBackupUnavailable
	}
	ctx, release := op.bindContext(ctx)
	defer release()
	if accountID == "" || accountID != confirmAccountID || !uuidV4(accountID) {
		return ErrAccountDeleteConfirmation
	}
	if err := op.beginMutation(); err != nil {
		return err
	}
	defer op.endMutation()
	repository := op.service.repository
	if repository == nil {
		return ErrCredentialBackupUnavailable
	}
	account, err := op.service.vault.repository.GetAccount(ctx, accountID)
	if err == nil && account.State != AccountStateTombstoned {
		return ErrCredentialBackupConflict
	}
	if err != nil && !errors.Is(err, ErrAccountNotFound) {
		return err
	}
	rows, err := repository.ListCredentialBackups(ctx, op.binding.TargetID, op.binding.VaultID)
	if err != nil {
		return err
	}
	matched := false
	for _, row := range rows {
		if row.AccountID != accountID || row.State != CredentialBackupStateDeletePending || row.Operation != CredentialBackupOperationDelete {
			continue
		}
		matched = true
		if row.OperationID == op.id {
			continue
		}
		updated := row
		updated.OperationID = op.id
		if err := repository.PutCredentialBackup(ctx, updated, row.Revision); err != nil {
			return err
		}
	}
	if !matched {
		return ErrCredentialBackupConflict
	}
	op.queueDeletion(accountID)
	return nil
}

func (op *CredentialBackupOperation) Sync(ctx context.Context) (CredentialBackupReport, error) {
	report := CredentialBackupReport{}
	if op == nil {
		return report, ErrCredentialBackupUnavailable
	}
	ctx, release := op.bindContext(ctx)
	defer release()
	op.mu.Lock()
	if op.closed {
		op.mu.Unlock()
		return report, ErrCredentialBackupUnavailable
	}
	if op.inflight != 0 || op.syncing {
		op.mu.Unlock()
		return report, keepass.ErrBusy
	}
	op.sealed = true
	op.syncing = true
	accounts := make(map[string]*queuedAccount, len(op.accounts))
	for key, value := range op.accounts {
		entry := *value
		entry.password = append([]byte(nil), value.password...)
		accounts[key] = &entry
	}
	files := make(map[string]*queuedFile, len(op.files))
	for key, value := range op.files {
		entry := *value
		entry.password = append([]byte(nil), value.password...)
		files[key] = &entry
	}
	deletions := make(map[string]struct{}, len(op.deletions))
	for accountID := range op.deletions {
		deletions[accountID] = struct{}{}
	}
	op.mu.Unlock()
	defer op.Close()
	vault := op.service.vault
	repository := op.service.repository
	var records []keepass.Record
	defer func() {
		for index := range records {
			records[index].Destroy()
		}
		for _, entry := range accounts {
			entry.clear()
		}
		for _, entry := range files {
			entry.clear()
		}
	}()
	failureSeen := make(map[string]bool)
	failure := func(accountID, itemID, code string) {
		key := accountID + "|" + itemID
		if failureSeen[key] {
			return
		}
		failureSeen[key] = true
		report.Failures = append(report.Failures, CredentialBackupFailure{AccountID: accountID, ItemID: itemID, Code: code})
		report.Pending++
	}
	ledgerRows, err := repository.ListCredentialBackups(ctx, op.binding.TargetID, op.binding.VaultID)
	if err != nil {
		return report, err
	}
	pendingUpsert := make(map[string]CredentialBackupState)
	pendingDelete := make(map[string][]CredentialBackupState)
	for _, row := range ledgerRows {
		if row.OperationID != op.id {
			continue
		}
		key := row.TargetID + "|" + row.VaultID + "|" + row.AccountID + "|" + row.ItemID
		switch {
		case row.State == CredentialBackupStatePending && row.Operation == CredentialBackupOperationUpsert:
			pendingUpsert[key] = row
		case row.State == CredentialBackupStateDeletePending && row.Operation == CredentialBackupOperationDelete:
			pendingDelete[row.AccountID] = append(pendingDelete[row.AccountID], row)
		}
	}
	var commitErr error
	for accountID, rows := range pendingDelete {
		if _, queued := deletions[accountID]; !queued || len(rows) == 0 {
			continue
		}
		account, accountErr := vault.repository.GetAccount(ctx, accountID)
		switch {
		case errors.Is(accountErr, ErrAccountNotFound):
		case accountErr != nil:
			failure(accountID, "", "account_unavailable")
			continue
		case account.State != AccountStateTombstoned:
			failure(accountID, "", "account_unavailable")
			continue
		}
		result, err := op.store.RemoveAccount(ctx, accountID)
		if errors.Is(err, keepass.ErrNotFound) {
			result, err = op.store.Upsert(ctx, nil)
		}
		if result.BackupPath != "" {
			report.BackupPaths = append(report.BackupPaths, result.BackupPath)
		}
		if err != nil {
			code := "remove_failed"
			if keepass.IsCommitted(err) {
				code = "durability_unconfirmed"
			}
			failure(accountID, "", code)
			commitErr = errors.Join(commitErr, err)
			continue
		}
		ackFailed := false
		for _, row := range rows {
			key := CredentialBackupKey{TargetID: row.TargetID, VaultID: row.VaultID, AccountID: row.AccountID, ItemID: row.ItemID}
			if err := repository.ConfirmCredentialBackup(ctx, key, row.OperationID, row.Generation); err != nil {
				failure(row.AccountID, row.ItemID, "ack_failed")
				ackFailed = true
			}
		}
		if !ackFailed {
			report.Deleted++
		}
	}
	for accountID := range deletions {
		if len(pendingDelete[accountID]) == 0 {
			failure(accountID, "", "delete_rows_missing")
		}
	}
	consumed := make(map[string]bool)
	failedAccounts := make(map[string]bool)
	type pendingRow struct {
		key     CredentialBackupKey
		account *queuedAccount
		file    *queuedFile
		ledger  CredentialBackupState
		built   *Account
	}
	var rows []pendingRow
	for _, queued := range accounts {
		key := CredentialBackupKey{TargetID: op.binding.TargetID, VaultID: op.binding.VaultID, AccountID: queued.accountID, ItemID: credentialItemAccount}
		ledgerKey := key.TargetID + "|" + key.VaultID + "|" + key.AccountID + "|" + key.ItemID
		ledger, tracked := pendingUpsert[ledgerKey]
		if !tracked || ledger.Generation != queued.generation {
			if _, acctErr := vault.repository.GetAccount(ctx, queued.accountID); errors.Is(acctErr, ErrAccountNotFound) {
				continue
			}
			failure(queued.accountID, credentialItemAccount, "unqueued")
			continue
		}
		consumed[ledgerKey] = true
		record, built, err := op.buildAccountRecord(ctx, queued)
		if err != nil {
			failedAccounts[queued.accountID] = true
			failure(queued.accountID, credentialItemAccount, "decrypt_failed")
			continue
		}
		records = append(records, record)
		rows = append(rows, pendingRow{key: key, account: queued, ledger: ledger, built: built})
	}
	for _, file := range files {
		key := CredentialBackupKey{TargetID: op.binding.TargetID, VaultID: op.binding.VaultID, AccountID: file.accountID, ItemID: file.itemID}
		ledgerKey := key.TargetID + "|" + key.VaultID + "|" + key.AccountID + "|" + key.ItemID
		ledger, tracked := pendingUpsert[ledgerKey]
		if failedAccounts[file.accountID] {
			failure(file.accountID, file.itemID, "account_unavailable")
			continue
		}
		if !tracked {
			if _, acctErr := vault.repository.GetAccount(ctx, file.accountID); errors.Is(acctErr, ErrAccountNotFound) {
				continue
			}
			failure(file.accountID, file.itemID, "unqueued")
			continue
		}
		consumed[ledgerKey] = true
		account, err := vault.repository.GetAccount(ctx, file.accountID)
		if err != nil || account.State == AccountStateTombstoned || account.State == AccountStatePendingBackup {
			failure(file.accountID, file.itemID, "account_unavailable")
			continue
		}
		records = append(records, keepass.Record{
			Ref:      keepass.Ref{VaultID: op.binding.VaultID, AccountID: file.accountID, ItemID: file.itemID},
			Title:    file.name,
			Address:  account.Address,
			Kind:     file.kind,
			FileName: file.name,
			Digest:   file.digest,
			Password: append([]byte(nil), file.password...),
		})
		rows = append(rows, pendingRow{key: key, file: file, ledger: ledger, built: account})
	}
	if len(rows) > 0 {
		vault.lifecycle.Lock()
		if vault.closed {
			vault.lifecycle.Unlock()
			for _, row := range rows {
				failure(row.key.AccountID, row.key.ItemID, "vault_closed")
			}
			return report, &CredentialBackupPendingError{Cause: ErrCredentialBackupUnavailable}
		}
		accountOK := make(map[string]bool)
		for _, row := range rows {
			if _, checked := accountOK[row.built.AccountID]; checked {
				continue
			}
			verdict := false
			if latest, err := vault.repository.GetAccount(ctx, row.built.AccountID); err == nil &&
				latest.Revision == row.built.Revision &&
				latest.AuthorizationEpoch == row.built.AuthorizationEpoch &&
				latest.EnvelopeGeneration == row.built.EnvelopeGeneration &&
				latest.SignerKind == SignerKindSoftware &&
				latest.Capabilities&CapabilityExportSecret != 0 &&
				addressesEqual(latest.Address, row.built.Address) &&
				(latest.State == AccountStateActive || latest.State == AccountStateLocked) {
				verdict = true
			}
			accountOK[row.built.AccountID] = verdict
		}
		var validRows []pendingRow
		var validRecords []keepass.Record
		for index, row := range rows {
			if !accountOK[row.built.AccountID] {
				code := "account_unavailable"
				if row.file == nil {
					code = "revision_changed"
				}
				failure(row.key.AccountID, row.key.ItemID, code)
				continue
			}
			validRows = append(validRows, row)
			validRecords = append(validRecords, records[index])
		}
		var backupPath string
		var upsertErr error
		if len(validRecords) > 0 {
			result, err := op.store.Upsert(ctx, validRecords)
			backupPath = result.BackupPath
			upsertErr = err
			commitErr = errors.Join(commitErr, upsertErr)
		}
		vault.lifecycle.Unlock()
		if backupPath != "" {
			report.BackupPaths = append(report.BackupPaths, backupPath)
		}
		if upsertErr == nil {
			for _, row := range validRows {
				if err := repository.ConfirmCredentialBackup(ctx, row.key, row.ledger.OperationID, row.ledger.Generation); err != nil {
					failure(row.key.AccountID, row.key.ItemID, "ack_conflict")
					continue
				}
				if row.file != nil {
					report.FilesSaved++
				} else {
					report.AccountsSaved++
				}
			}
		} else {
			code := "commit_failed"
			if keepass.IsCommitted(upsertErr) {
				code = "durability_unconfirmed"
			}
			for _, row := range validRows {
				failure(row.key.AccountID, row.key.ItemID, code)
			}
		}
	}
	for key, row := range pendingUpsert {
		if !consumed[key] {
			report.Failures = append(report.Failures, CredentialBackupFailure{AccountID: row.AccountID, ItemID: row.ItemID, Code: "unqueued"})
			report.Pending++
		}
	}
	for _, row := range ledgerRows {
		if row.State == CredentialBackupStatePrepared && row.OperationID == op.id {
			report.Failures = append(report.Failures, CredentialBackupFailure{AccountID: row.AccountID, ItemID: row.ItemID, Code: "prepared"})
			report.Pending++
		}
	}
	if report.Pending > 0 {
		cause := commitErr
		if cause == nil {
			cause = fmt.Errorf("credential backup incomplete: %s", report.Failures[0].Code)
		}
		return report, &CredentialBackupPendingError{Cause: cause}
	}
	return report, nil
}

func (op *CredentialBackupOperation) buildAccountRecord(ctx context.Context, queued *queuedAccount) (keepass.Record, *Account, error) {
	vault := op.service.vault
	record := keepass.Record{}
	var built *Account
	err := vault.withVerifiedRecoveryMaterial(ctx, queued.accountID, queued.password, true, false, nil, func(account *Account, secret canonicalSecretV1, privateKey []byte) error {
		if account.EnvelopeGeneration != queued.generation {
			return ErrCredentialBackupConflict
		}
		built = account
		record = keepass.Record{
			Ref:            keepass.Ref{VaultID: op.binding.VaultID, AccountID: account.AccountID, ItemID: credentialItemAccount},
			Title:          account.Name,
			Address:        account.Address,
			SecretType:     string(account.SecretType),
			DerivationPath: account.DerivationPath,
			Language:       account.BIP39Language,
			Generation:     account.EnvelopeGeneration,
			Kind:           credentialItemAccount,
			Password:       append([]byte(nil), queued.password...),
			PrivateKey:     []byte("0x" + hex.EncodeToString(privateKey)),
		}
		if secret.Kind == SecretTypeMnemonic {
			record.Mnemonic = []byte(normalizedMnemonic(secret.Mnemonic))
			if secret.BIP39Passphrase != "" {
				record.Passphrase = []byte(secret.BIP39Passphrase)
			}
		}
		return nil
	})
	if err != nil {
		record.Destroy()
		return keepass.Record{}, nil, err
	}
	return record, built, nil
}

func (op *CredentialBackupOperation) Close() {
	if op == nil {
		return
	}
	op.mu.Lock()
	if op.closed {
		op.mu.Unlock()
		return
	}
	op.closed = true
	accounts := op.accounts
	files := op.files
	op.accounts = nil
	op.files = nil
	op.deletions = nil
	store := op.store
	stop := op.stopAfterFunc
	cancel := op.cancel
	op.mu.Unlock()
	if stop != nil {
		stop()
	}
	if cancel != nil {
		cancel()
	}
	for _, entry := range accounts {
		entry.clear()
	}
	for _, file := range files {
		file.clear()
	}
	if store != nil {
		store.Close()
	}
	op.service.mu.Lock()
	delete(op.service.ops, op)
	op.service.mu.Unlock()
}

func uuidV4(value string) bool {
	return accountUUIDPattern.MatchString(value)
}

func credentialOperationFromContext(ctx context.Context) *CredentialBackupOperation {
	if ctx == nil {
		return nil
	}
	op, _ := ctx.Value(credentialOperationContextKey{}).(*CredentialBackupOperation)
	return op
}

func (vault *WalletVault) beginCredentialMutation(ctx context.Context) (*CredentialBackupOperation, func(), error) {
	vault.credentialMu.Lock()
	service := vault.credentialService
	vault.credentialMu.Unlock()
	noop := func() {}
	if service == nil {
		return nil, noop, nil
	}
	service.mu.Lock()
	enabled := service.policy.Enabled
	serviceClosed := service.closed
	service.mu.Unlock()
	if serviceClosed {
		if enabled {
			return nil, nil, ErrCredentialBackupUnavailable
		}
		return nil, noop, nil
	}
	if !enabled {
		return nil, noop, nil
	}
	op := credentialOperationFromContext(ctx)
	if op == nil || op.service != service {
		return nil, nil, ErrCredentialBackupRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := op.alive(); err != nil {
		return nil, nil, err
	}
	if err := op.store.Preflight(ctx); err != nil {
		return nil, nil, err
	}
	op.mu.Lock()
	if err := op.aliveMutableLocked(); err != nil {
		op.mu.Unlock()
		return nil, nil, err
	}
	op.inflight++
	op.mu.Unlock()
	return op, op.endMutation, nil
}

func (vault *WalletVault) recordAccountIntent(ctx context.Context, transaction AccountRepository, account *Account, artifacts ...credentialArtifact) error {
	op := credentialOperationFromContext(ctx)
	if op == nil {
		return nil
	}
	ledger, ok := transaction.(CredentialBackupRepository)
	if !ok {
		return ErrCredentialBackupUnavailable
	}
	if err := op.aliveMutable(); err != nil {
		return err
	}
	if err := putCredentialState(ctx, ledger, CredentialBackupState{
		TargetID:    op.binding.TargetID,
		VaultID:     op.binding.VaultID,
		AccountID:   account.AccountID,
		ItemID:      credentialItemAccount,
		OperationID: op.id,
		Operation:   CredentialBackupOperationUpsert,
		State:       CredentialBackupStatePending,
		Generation:  account.EnvelopeGeneration,
	}); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := validateCredentialArtifact(artifact); err != nil {
			return err
		}
		digest := sha256.Sum256(artifact.Ciphertext)
		digestHex := hex.EncodeToString(digest[:])
		if err := putCredentialState(ctx, ledger, CredentialBackupState{
			TargetID:       op.binding.TargetID,
			VaultID:        op.binding.VaultID,
			AccountID:      account.AccountID,
			ItemID:         "file:" + artifact.Kind + ":" + digestHex,
			OperationID:    op.id,
			Operation:      CredentialBackupOperationUpsert,
			State:          CredentialBackupStatePending,
			Generation:     0,
			ArtifactKind:   artifact.Kind,
			ArtifactName:   artifact.Name,
			ArtifactPath:   artifact.Path,
			ArtifactDigest: digestHex,
		}); err != nil {
			return err
		}
	}
	return nil
}

func putCredentialState(ctx context.Context, ledger CredentialBackupRepository, state CredentialBackupState) error {
	existing, err := ledger.GetCredentialBackup(ctx, CredentialBackupKey{
		TargetID:  state.TargetID,
		VaultID:   state.VaultID,
		AccountID: state.AccountID,
		ItemID:    state.ItemID,
	})
	switch {
	case errors.Is(err, ErrAccountNotFound):
		state.Revision = 1
		return ledger.PutCredentialBackup(ctx, state, 0)
	case err != nil:
		return err
	default:
		state.SyncedGeneration = existing.SyncedGeneration
		return ledger.PutCredentialBackup(ctx, state, existing.Revision)
	}
}

func (vault *WalletVault) registerPreparedArtifact(ctx context.Context, op *CredentialBackupOperation, accountID string, artifact credentialArtifact) (CredentialBackupState, error) {
	if err := validateCredentialArtifact(artifact); err != nil {
		return CredentialBackupState{}, err
	}
	digest := sha256.Sum256(artifact.Ciphertext)
	digestHex := hex.EncodeToString(digest[:])
	state := CredentialBackupState{
		TargetID:       op.binding.TargetID,
		VaultID:        op.binding.VaultID,
		AccountID:      accountID,
		ItemID:         "file:" + artifact.Kind + ":" + digestHex,
		OperationID:    op.id,
		Operation:      CredentialBackupOperationUpsert,
		State:          CredentialBackupStatePrepared,
		ArtifactKind:   artifact.Kind,
		ArtifactName:   artifact.Name,
		ArtifactPath:   artifact.Path,
		ArtifactDigest: digestHex,
	}
	err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		ledger, ok := transaction.(CredentialBackupRepository)
		if !ok {
			return ErrCredentialBackupUnavailable
		}
		if err := op.aliveMutable(); err != nil {
			return err
		}
		return putCredentialState(ctx, ledger, state)
	})
	return state, err
}

func (vault *WalletVault) markArtifactPending(ctx context.Context, op *CredentialBackupOperation, key CredentialBackupKey) error {
	return vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		ledger, ok := transaction.(CredentialBackupRepository)
		if !ok {
			return ErrCredentialBackupUnavailable
		}
		row, err := ledger.GetCredentialBackup(ctx, key)
		if err != nil {
			return err
		}
		if row.Operation != CredentialBackupOperationUpsert || row.State != CredentialBackupStatePrepared {
			return ErrCredentialBackupConflict
		}
		row.State = CredentialBackupStatePending
		row.OperationID = op.id
		return ledger.PutCredentialBackup(ctx, row, row.Revision)
	})
}

func queueCommittedAccountCredential(op *CredentialBackupOperation, account *Account, password []byte, artifacts []credentialArtifact) error {
	if op == nil || account == nil {
		return nil
	}
	if err := op.queueCommittedAccount(account.AccountID, account.EnvelopeGeneration, account.Revision, account.AuthorizationEpoch, password, artifacts); err != nil {
		return &CredentialBackupPendingError{Cause: err}
	}
	return nil
}

func validateCredentialArtifact(artifact credentialArtifact) error {
	if artifact.Kind != credentialKindKeystoreV3 && artifact.Kind != credentialKindEncryptedFile {
		return fmt.Errorf("artifact kind is outside the managed schema")
	}
	if len(artifact.Ciphertext) == 0 || len(artifact.Ciphertext) > maxKeystoreImportSize {
		return fmt.Errorf("artifact ciphertext is outside policy")
	}
	if artifact.Path != "" && !filepath.IsAbs(artifact.Path) {
		return fmt.Errorf("artifact source path must be absolute")
	}
	if artifact.Name == "" {
		return fmt.Errorf("artifact name is required")
	}
	return nil
}
