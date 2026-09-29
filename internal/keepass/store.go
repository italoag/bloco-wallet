package keepass

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	kp "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

var (
	ErrAuthentication      = errors.New("keepass: authentication failed")
	ErrWrongDatabase       = errors.New("keepass: database does not match the configured target")
	ErrUnsupportedDatabase = errors.New("keepass: database profile is unsupported")
	ErrConflict            = errors.New("keepass: vault file changed since it was opened")
	ErrBusy                = errors.New("keepass: vault file is locked")
	ErrExpired             = errors.New("keepass: vault operation is closed or expired")
	ErrNotFound            = errors.New("keepass: record not found")
	ErrAmbiguous           = errors.New("keepass: multiple records match")
	ErrInvalidRecord       = errors.New("keepass: record is invalid")
)

const (
	defaultOperationTTL = 10 * time.Minute
	kdfBaselineMemory   = 64 << 20
	kdfBaselineRounds   = 3
	kdfBaselineLanes    = 2
	kdfArgon2Version    = 19
	maxVaultFileBytes   = 32 << 20
	masterMinRunes      = 12
	masterMaxBytes      = 1024
)

type Options struct {
	Now    func() time.Time
	Random io.Reader
	TTL    time.Duration
}

type CommitResult struct {
	Committed  bool
	BackupPath string
}

type CommittedWarning struct {
	Cause error
}

func (warning *CommittedWarning) Error() string {
	if warning == nil || warning.Cause == nil {
		return "keepass commit completed but durability confirmation failed"
	}
	return "keepass commit completed but durability confirmation failed: " + warning.Cause.Error()
}

func (warning *CommittedWarning) Unwrap() error {
	if warning == nil {
		return nil
	}
	return warning.Cause
}

func IsCommitted(err error) bool {
	var warning *CommittedWarning
	return errors.As(err, &warning)
}

func ValidateMasterPassword(master []byte) error {
	if len(master) == 0 {
		return fmt.Errorf("%w: master password is required", ErrInvalidRecord)
	}
	if len(master) > masterMaxBytes {
		return fmt.Errorf("%w: master password exceeds %d bytes", ErrInvalidRecord, masterMaxBytes)
	}
	if !utf8.Valid(master) {
		return fmt.Errorf("%w: master password must be valid UTF-8", ErrInvalidRecord)
	}
	if utf8.RuneCount(master) < masterMinRunes {
		return fmt.Errorf("%w: master password requires at least %d characters", ErrInvalidRecord, masterMinRunes)
	}
	if strings.TrimSpace(string(master)) == "" {
		return fmt.Errorf("%w: master password cannot be only whitespace", ErrInvalidRecord)
	}
	return nil
}

type storeHooks struct {
	open    func(path string) (*os.File, os.FileInfo, error)
	temp    func(dir string) (*os.File, error)
	install func(tempPath, target string, replace bool) (bool, error)
	syncDir func(dir string) error
	lock    func(path string) (func(), error)
	secure  func(path string) error
}

type Store struct {
	options Options
	hooks   storeHooks
}

func NewStore(options Options) *Store {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.TTL <= 0 || options.TTL > defaultOperationTTL {
		options.TTL = defaultOperationTTL
	}
	return &Store{
		options: options,
		hooks: storeHooks{
			open:    openVaultFile,
			temp:    createVaultTemp,
			install: installVaultFile,
			syncDir: syncVaultDirectory,
			lock:    lockVaultFile,
			secure:  secureVaultFile,
		},
	}
}

func (s *Store) newUUID() (string, error) {
	raw, err := s.randomBytes(16)
	if err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

func (s *Store) entryUUID() (kp.UUID, error) {
	raw, err := s.randomBytes(16)
	if err != nil {
		return kp.UUID{}, err
	}
	var id kp.UUID
	copy(id[:], raw)
	return id, nil
}

func (s *Store) randomBytes(count int) ([]byte, error) {
	value := make([]byte, count)
	if _, err := io.ReadFull(s.options.Random, value); err != nil {
		return nil, err
	}
	return value, nil
}

func passwordCredentials(master []byte) *kp.DBCredentials {
	digest := sha256.Sum256(master)
	return &kp.DBCredentials{Passphrase: digest[:]}
}

func validateVaultPath(path string, mustExist bool) (string, os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return "", nil, fmt.Errorf("keepass vault path must be absolute")
	}
	cleaned := filepath.Clean(path)
	if strings.ToLower(filepath.Ext(cleaned)) != ".kdbx" {
		return "", nil, fmt.Errorf("keepass vault path must have a .kdbx extension")
	}
	parent := filepath.Dir(cleaned)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return "", nil, err
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return "", nil, fmt.Errorf("keepass vault parent must be a regular directory")
	}
	info, err := os.Lstat(cleaned)
	if err != nil {
		if os.IsNotExist(err) && !mustExist {
			return cleaned, parentInfo, nil
		}
		return "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("keepass vault must be a regular file")
	}
	return cleaned, parentInfo, nil
}

func verifyParent(path string, parentInfo os.FileInfo) error {
	current, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() {
		return fmt.Errorf("%w: vault parent is not a regular directory", ErrConflict)
	}
	if !os.SameFile(current, parentInfo) {
		return ErrConflict
	}
	return nil
}

func metaCustomValues(meta *kp.MetaData, key string) []string {
	var found []string
	for _, item := range meta.CustomData {
		if item.Key == key {
			found = append(found, item.Value)
		}
	}
	return found
}

func setMetaCustomValue(meta *kp.MetaData, key, value string) {
	for index := range meta.CustomData {
		if meta.CustomData[index].Key == key {
			meta.CustomData[index].Value = value
			return
		}
	}
	meta.CustomData = append(meta.CustomData, kp.CustomData{Key: key, Value: value})
}

func groupRole(group *kp.Group) string {
	count := 0
	role := ""
	for _, item := range group.CustomData {
		if item.Key == customRole {
			count++
			role = item.Value
		}
	}
	if count > 1 {
		return "\x00duplicate"
	}
	return role
}

func decodeDatabase(master []byte, data []byte) (*kp.Database, error) {
	if len(master) == 0 {
		return nil, fmt.Errorf("%w: master password is required", ErrAuthentication)
	}
	database := kp.NewDatabase()
	database.Credentials = passwordCredentials(master)
	if err := kp.NewDecoderWithLimits(bytes.NewReader(data), kp.DefaultDecodeLimits()).Decode(database); err != nil {
		destroyDatabase(database)
		return nil, classifyDecodeError(err)
	}
	return database, nil
}

func classifyDecodeError(err error) error {
	if errors.Is(err, kp.ErrInvalidDatabaseOrCredentials) || strings.HasPrefix(err.Error(), "Wrong password") {
		return ErrAuthentication
	}
	return fmt.Errorf("%w: %v", ErrUnsupportedDatabase, err)
}

func validateProfile(database *kp.Database) error {
	if database == nil || database.Header == nil || database.Header.FileHeaders == nil || database.Content == nil {
		return ErrUnsupportedDatabase
	}
	header := database.Header
	if !header.IsKdbx4() {
		return fmt.Errorf("%w: KDBX4 file format required", ErrUnsupportedDatabase)
	}
	headers := header.FileHeaders
	if !bytes.Equal(headers.CipherID, kp.CipherAES) {
		return fmt.Errorf("%w: AES cipher required", ErrUnsupportedDatabase)
	}
	parameters := headers.KdfParameters
	if parameters == nil || !bytes.Equal(parameters.UUID, kp.KdfArgon2) || parameters.Version != kdfArgon2Version {
		return fmt.Errorf("%w: Argon2d v1.3 KDF required", ErrUnsupportedDatabase)
	}
	if database.Content.InnerHeader == nil || database.Content.InnerHeader.InnerRandomStreamID != kp.ChaChaStreamID {
		return fmt.Errorf("%w: ChaCha20 inner stream required", ErrUnsupportedDatabase)
	}
	return nil
}

func readTargetID(database *kp.Database) (string, error) {
	if database.Content.Meta == nil {
		return "", ErrUnsupportedDatabase
	}
	versions := metaCustomValues(database.Content.Meta, customSchemaVersion)
	if len(versions) != 1 || versions[0] != schemaVersionV1 {
		return "", fmt.Errorf("%w: dedicated vault marker missing", ErrUnsupportedDatabase)
	}
	targets := metaCustomValues(database.Content.Meta, customTargetID)
	if len(targets) != 1 || !isUUIDv4(targets[0]) {
		if len(targets) > 1 {
			return "", ErrAmbiguous
		}
		return "", fmt.Errorf("%w: dedicated vault target is missing", ErrUnsupportedDatabase)
	}
	return targets[0], nil
}

func roleGroup(root *kp.Group, role string) (*kp.Group, error) {
	var matched *kp.Group
	for index := range root.Groups {
		groupRole := groupRole(&root.Groups[index])
		if groupRole == "\x00duplicate" {
			return nil, fmt.Errorf("%w: duplicate %s role key", ErrAmbiguous, role)
		}
		if groupRole != role {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("%w: duplicate %s group", ErrAmbiguous, role)
		}
		matched = &root.Groups[index]
	}
	if matched == nil {
		return nil, fmt.Errorf("%w: missing %s group", ErrUnsupportedDatabase, role)
	}
	return matched, nil
}

func managedRoot(database *kp.Database) (*kp.Group, error) {
	if database.Content.Root == nil || len(database.Content.Root.Groups) == 0 {
		return nil, ErrUnsupportedDatabase
	}
	return &database.Content.Root.Groups[0], nil
}

func managedGroups(database *kp.Database) (accounts *kp.Group, files *kp.Group, err error) {
	root, err := managedRoot(database)
	if err != nil {
		return nil, nil, err
	}
	accounts, err = roleGroup(root, roleAccounts)
	if err != nil {
		return nil, nil, err
	}
	files, err = roleGroup(root, roleFiles)
	if err != nil {
		return nil, nil, err
	}
	return accounts, files, nil
}

func forEachEntry(group *kp.Group, fn func(*kp.Entry)) {
	for index := range group.Entries {
		fn(&group.Entries[index])
	}
	for index := range group.Groups {
		forEachEntry(&group.Groups[index], fn)
	}
}

func removeEntries(group *kp.Group, match func(*kp.Entry) bool) []kp.UUID {
	var removed []kp.UUID
	retained := group.Entries[:0]
	for index := range group.Entries {
		if match(&group.Entries[index]) {
			removed = append(removed, group.Entries[index].UUID)
			continue
		}
		retained = append(retained, group.Entries[index])
	}
	for index := len(retained); index < len(group.Entries); index++ {
		group.Entries[index] = kp.Entry{}
	}
	group.Entries = retained
	for index := range group.Groups {
		removed = append(removed, removeEntries(&group.Groups[index], match)...)
	}
	return removed
}

func findManagedEntry(group *kp.Group, ref Ref) (*kp.Entry, error) {
	var matched *kp.Entry
	var walkErr error
	forEachEntry(group, func(entry *kp.Entry) {
		if walkErr != nil || !managedEntry(entry) || !entryMatchesRef(entry, ref) {
			return
		}
		if matched != nil {
			walkErr = ErrAmbiguous
			return
		}
		matched = entry
	})
	return matched, walkErr
}

func validateManagedEntry(entry *kp.Entry) error {
	marked := false
	for _, value := range entry.Values {
		if strings.HasPrefix(value.Key, "Bloco.") {
			marked = true
			break
		}
	}
	if !marked {
		return nil
	}
	seen := make(map[string]struct{}, len(entry.Values))
	for _, value := range entry.Values {
		if _, exists := seen[value.Key]; exists {
			return fmt.Errorf("%w: duplicate field %q", ErrAmbiguous, value.Key)
		}
		seen[value.Key] = struct{}{}
	}
	if !managedEntry(entry) {
		return fmt.Errorf("%w: incomplete managed markers", ErrInvalidRecord)
	}
	ref := entryRef(entry)
	if err := validateRef(ref); err != nil {
		return err
	}
	password, ok := entryValue(entry, "Password")
	if !ok || !password.Protected.Bool {
		return fmt.Errorf("%w: managed record password must be protected", ErrInvalidRecord)
	}
	for _, key := range []string{secretMnemonic, secretPrivateKey, secretPassphrase} {
		value, present := entryValue(entry, key)
		if present && !value.Protected.Bool {
			return fmt.Errorf("%w: managed record secret %s must be protected", ErrInvalidRecord, key)
		}
	}
	if ref.ItemID == itemIDAccount {
		if entryGet(entry, customKind) != kindAccount || entryGet(entry, customDigest) != "" ||
			entryGeneration(entry) == 0 || len(password.Content) == 0 {
			return fmt.Errorf("%w: malformed managed account record", ErrInvalidRecord)
		}
		return nil
	}
	kind := entryGet(entry, customKind)
	digest := entryGet(entry, customDigest)
	if ref.ItemID != fileItemPrefix+kind+":"+digest || (kind != kindKeystoreV3 && kind != kindBlocoCipher) ||
		!digestPattern.MatchString(digest) || entryGet(entry, customFileName) == "" {
		return fmt.Errorf("%w: malformed managed file record", ErrInvalidRecord)
	}
	return nil
}

func validateManagedStructure(database *kp.Database) (int, error) {
	accounts, files, err := managedGroups(database)
	if err != nil {
		return 0, err
	}
	count := 0
	refs := make(map[Ref]struct{}, 64)
	var walkErr error
	walk := func(entry *kp.Entry, accountRole bool) {
		if walkErr != nil {
			return
		}
		if err := validateManagedEntry(entry); err != nil {
			walkErr = err
			return
		}
		if !managedEntry(entry) {
			return
		}
		ref := entryRef(entry)
		if (ref.ItemID == itemIDAccount) != accountRole {
			walkErr = fmt.Errorf("%w: managed record in wrong role group", ErrInvalidRecord)
			return
		}
		if _, exists := refs[ref]; exists {
			walkErr = ErrAmbiguous
			return
		}
		refs[ref] = struct{}{}
		count++
	}
	forEachEntry(accounts, func(entry *kp.Entry) { walk(entry, true) })
	forEachEntry(files, func(entry *kp.Entry) { walk(entry, false) })
	if walkErr != nil {
		return 0, walkErr
	}
	if count > maxManagedEntries {
		return 0, fmt.Errorf("%w: managed entry budget exceeded", ErrUnsupportedDatabase)
	}
	return count, nil
}

func (s *Store) applyCryptoProfile(database *kp.Database) error {
	headers := database.Header.FileHeaders
	headers.CipherID = append([]byte(nil), kp.CipherAES...)
	headers.CompressionFlags = kp.GzipCompressionFlag
	masterSeed, err := s.randomBytes(32)
	if err != nil {
		return err
	}
	headers.MasterSeed = masterSeed
	encryptionIV, err := s.randomBytes(16)
	if err != nil {
		return err
	}
	headers.EncryptionIV = encryptionIV
	parameters := headers.KdfParameters
	if parameters == nil {
		parameters = &kp.KdfParameters{}
		headers.KdfParameters = parameters
	}
	parameters.UUID = append([]byte(nil), kp.KdfArgon2...)
	parameters.Version = kdfArgon2Version
	parameters.Rounds = 0
	parameters.SecretKey = nil
	parameters.AssocData = nil
	if parameters.Memory < kdfBaselineMemory {
		parameters.Memory = kdfBaselineMemory
	}
	if parameters.Iterations < kdfBaselineRounds {
		parameters.Iterations = kdfBaselineRounds
	}
	if parameters.Parallelism < kdfBaselineLanes {
		parameters.Parallelism = kdfBaselineLanes
	}
	if _, err := io.ReadFull(s.options.Random, parameters.Salt[:]); err != nil {
		return err
	}
	inner := database.Content.InnerHeader
	if inner == nil {
		inner = &kp.InnerHeader{}
		database.Content.InnerHeader = inner
	}
	inner.InnerRandomStreamID = kp.ChaChaStreamID
	innerKey, err := s.randomBytes(64)
	if err != nil {
		return err
	}
	inner.InnerRandomStreamKey = innerKey
	return nil
}

func wipeEntryValues(entries []kp.Entry) {
	for index := range entries {
		for value := range entries[index].Values {
			entries[index].Values[value].Key = ""
			entries[index].Values[value].Value.Content = ""
		}
		for history := range entries[index].Histories {
			wipeEntryValues(entries[index].Histories[history].Entries)
		}
	}
}

func wipeGroups(groups []kp.Group) {
	for index := range groups {
		wipeEntryValues(groups[index].Entries)
		wipeGroups(groups[index].Groups)
	}
}

func destroyDatabase(database *kp.Database) {
	if database == nil {
		return
	}
	if database.Credentials != nil {
		clear(database.Credentials.Passphrase)
		clear(database.Credentials.Key)
		clear(database.Credentials.Windows)
	}
	if database.Header != nil && database.Header.FileHeaders != nil {
		headers := database.Header.FileHeaders
		clear(headers.MasterSeed)
		clear(headers.EncryptionIV)
		clear(headers.ProtectedStreamKey)
		clear(headers.StreamStartBytes)
		clear(headers.TransformSeed)
		if headers.KdfParameters != nil {
			clear(headers.KdfParameters.Salt[:])
			clear(headers.KdfParameters.SecretKey)
			clear(headers.KdfParameters.AssocData)
		}
	}
	if database.Content != nil {
		clear(database.Content.RawData)
		if database.Content.InnerHeader != nil {
			clear(database.Content.InnerHeader.InnerRandomStreamKey)
			for index := range database.Content.InnerHeader.Binaries {
				clear(database.Content.InnerHeader.Binaries[index].Content)
			}
		}
		if database.Content.Meta != nil {
			for index := range database.Content.Meta.Binaries {
				clear(database.Content.Meta.Binaries[index].Content)
			}
		}
		if database.Content.Root != nil {
			wipeGroups(database.Content.Root.Groups)
		}
	}
}

func (s *Store) encodeDatabase(database *kp.Database) ([]byte, error) {
	if err := database.LockProtectedEntries(); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encodeErr := kp.NewEncoder(&buffer).Encode(database)
	if err := database.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("restore vault session: %w", err)
	}
	if encodeErr != nil {
		return nil, encodeErr
	}
	data := buffer.Bytes()
	if len(data) > maxVaultFileBytes {
		return nil, fmt.Errorf("%w: encoded vault exceeds size budget", ErrUnsupportedDatabase)
	}
	verify := kp.NewDatabase()
	verify.Credentials = &kp.DBCredentials{Passphrase: append([]byte(nil), database.Credentials.Passphrase...)}
	defer destroyDatabase(verify)
	if err := kp.NewDecoderWithLimits(bytes.NewReader(data), kp.DefaultDecodeLimits()).Decode(verify); err != nil {
		return nil, fmt.Errorf("verify encoded vault: %w", err)
	}
	return data, nil
}

func (s *Store) readVault(path string, parentInfo os.FileInfo) ([]byte, os.FileInfo, error) {
	if err := verifyParent(path, parentInfo); err != nil {
		return nil, nil, err
	}
	file, info, err := s.hooks.open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxVaultFileBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > maxVaultFileBytes {
		return nil, nil, fmt.Errorf("%w: vault file exceeds size budget", ErrUnsupportedDatabase)
	}
	return data, info, nil
}

func (s *Store) writePrivateFile(dir, pattern string, data []byte) (string, error) {
	temp, err := s.hooks.temp(dir)
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	if err := s.hooks.secure(tempPath); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	return tempPath, nil
}

func (s *Store) writeExclusive(ctx context.Context, path string, data []byte, parentInfo os.FileInfo) error {
	parent := filepath.Dir(path)
	tempPath, err := s.writePrivateFile(parent, "", data)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tempPath)
		}
	}()
	if err := verifyParent(path, parentInfo); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	published, err = s.hooks.install(tempPath, path, false)
	if err != nil {
		if published {
			return &CommittedWarning{Cause: err}
		}
		return err
	}
	if err := s.hooks.syncDir(parent); err != nil {
		return &CommittedWarning{Cause: err}
	}
	return nil
}

func (s *Store) Create(ctx context.Context, path, vaultID string, master []byte) (Binding, error) {
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	cleaned, parentInfo, err := validateVaultPath(path, false)
	if err != nil {
		return Binding{}, err
	}
	if !isUUIDv4(vaultID) {
		return Binding{}, fmt.Errorf("%w: vault identifier must be a canonical UUIDv4", ErrInvalidRecord)
	}
	if err := ValidateMasterPassword(master); err != nil {
		return Binding{}, err
	}
	targetID, err := s.newUUID()
	if err != nil {
		return Binding{}, err
	}
	database := kp.NewDatabase(kp.WithDatabaseKDBXVersion40())
	defer destroyDatabase(database)
	database.Credentials = passwordCredentials(master)
	root, err := s.newManagedGroup(rootGroupName, "")
	if err != nil {
		return Binding{}, err
	}
	accounts, err := s.newManagedGroup("Accounts", roleAccounts)
	if err != nil {
		return Binding{}, err
	}
	files, err := s.newManagedGroup("Files", roleFiles)
	if err != nil {
		return Binding{}, err
	}
	root.Groups = []kp.Group{accounts, files}
	database.Content.Root = &kp.RootData{Groups: []kp.Group{root}}
	setMetaCustomValue(database.Content.Meta, customSchemaVersion, schemaVersionV1)
	setMetaCustomValue(database.Content.Meta, customTargetID, targetID)
	if err := s.applyCryptoProfile(database); err != nil {
		return Binding{}, err
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	data, err := s.encodeDatabase(database)
	if err != nil {
		return Binding{}, err
	}
	if err := ctx.Err(); err != nil {
		return Binding{}, err
	}
	binding := Binding{Path: cleaned, TargetID: targetID, VaultID: vaultID}
	if err := s.writeExclusive(ctx, cleaned, data, parentInfo); err != nil {
		if IsCommitted(err) {
			return binding, err
		}
		return Binding{}, err
	}
	return binding, nil
}

func (s *Store) newManagedGroup(name, role string) (kp.Group, error) {
	group := kp.NewGroup()
	id, err := s.entryUUID()
	if err != nil {
		return kp.Group{}, err
	}
	group.UUID = id
	group.Name = name
	if role != "" {
		group.CustomData = append(group.CustomData, kp.CustomData{Key: customRole, Value: role})
	}
	return group, nil
}

func (s *Store) Inspect(ctx context.Context, path string, master []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cleaned, parentInfo, err := validateVaultPath(path, true)
	if err != nil {
		return "", err
	}
	data, _, err := s.readVault(cleaned, parentInfo)
	if err != nil {
		return "", err
	}
	database, err := decodeDatabase(master, data)
	if err != nil {
		return "", err
	}
	defer destroyDatabase(database)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateProfile(database); err != nil {
		return "", err
	}
	targetID, err := readTargetID(database)
	if err != nil {
		return "", err
	}
	if _, err := validateManagedStructure(database); err != nil {
		return "", err
	}
	return targetID, nil
}

type Operation struct {
	store   *Store
	binding Binding

	mu         sync.Mutex
	database   *kp.Database
	snapshot   []byte
	fileInfo   os.FileInfo
	parentInfo os.FileInfo
	digest     [32]byte
	unlock     func()
	expiresAt  time.Time
	expiry     *time.Timer
	expired    bool
	closed     bool
}

func (op *Operation) String() string {
	return fmt.Sprintf("keepass.Operation{path:%s target:%s}", op.binding.Path, op.binding.TargetID)
}

func (op *Operation) GoString() string {
	return op.String()
}

func (op *Operation) MarshalJSON() ([]byte, error) {
	return nil, errRecordSerialization
}

func (s *Store) Open(ctx context.Context, binding Binding, master []byte) (*Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cleaned, parentInfo, err := validateVaultPath(binding.Path, true)
	if err != nil {
		return nil, err
	}
	if !isUUIDv4(binding.TargetID) || !isUUIDv4(binding.VaultID) {
		return nil, fmt.Errorf("%w: binding identifiers must be canonical UUIDv4", ErrInvalidRecord)
	}
	if len(master) == 0 {
		return nil, fmt.Errorf("%w: master password is required", ErrAuthentication)
	}
	binding.Path = cleaned
	unlock, err := s.hooks.lock(cleaned)
	if err != nil {
		return nil, err
	}
	op := &Operation{store: s, binding: binding, unlock: unlock, parentInfo: parentInfo}
	fail := func(err error) (*Operation, error) {
		op.discard()
		return nil, err
	}
	data, info, err := s.readVault(cleaned, parentInfo)
	if err != nil {
		return fail(err)
	}
	database, err := decodeDatabase(master, data)
	if err != nil {
		return fail(err)
	}
	op.database = database
	bad := func(err error) (*Operation, error) {
		op.mu.Lock()
		op.discardLocked()
		op.mu.Unlock()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return bad(err)
	}
	if err := validateProfile(database); err != nil {
		return bad(err)
	}
	targetID, err := readTargetID(database)
	if err != nil {
		return bad(err)
	}
	if targetID != binding.TargetID {
		return bad(ErrWrongDatabase)
	}
	if _, err := validateManagedStructure(database); err != nil {
		return bad(err)
	}
	if err := database.UnlockProtectedEntries(); err != nil {
		return bad(fmt.Errorf("%w: %v", ErrUnsupportedDatabase, err))
	}
	op.snapshot = data
	op.fileInfo = info
	op.digest = sha256.Sum256(data)
	op.mu.Lock()
	op.expiresAt = s.options.Now().Add(s.options.TTL)
	op.expiry = time.AfterFunc(s.options.TTL, func() {
		op.mu.Lock()
		defer op.mu.Unlock()
		op.expireLocked()
	})
	op.mu.Unlock()
	return op, nil
}

func (op *Operation) expireLocked() {
	if op.expired || op.closed {
		return
	}
	op.expired = true
	op.discardLocked()
}

func (op *Operation) discardLocked() {
	if op.expiry != nil {
		op.expiry.Stop()
		op.expiry = nil
	}
	if op.database != nil {
		destroyDatabase(op.database)
		op.database = nil
	}
	op.snapshot = nil
	if op.unlock != nil {
		op.unlock()
		op.unlock = nil
	}
}

func (op *Operation) discard() {
	op.mu.Lock()
	defer op.mu.Unlock()
	op.discardLocked()
}

func (op *Operation) aliveLocked() error {
	if op.expired || op.closed || op.database == nil {
		return ErrExpired
	}
	if !op.store.options.Now().Before(op.expiresAt) {
		op.expireLocked()
		return ErrExpired
	}
	return nil
}

func (op *Operation) verifyFileState() error {
	data, info, err := op.store.readVault(op.binding.Path, op.parentInfo)
	if err != nil {
		return err
	}
	if !os.SameFile(info, op.fileInfo) || sha256.Sum256(data) != op.digest {
		return ErrConflict
	}
	return nil
}

func (op *Operation) Preflight(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveLocked(); err != nil {
		return err
	}
	if err := op.verifyFileState(); err != nil {
		return err
	}
	parent := filepath.Dir(op.binding.Path)
	temp, err := op.store.hooks.temp(parent)
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return os.Remove(tempPath)
}

func (op *Operation) failCommit(err error) (CommitResult, error) {
	op.closed = true
	op.discardLocked()
	return CommitResult{}, err
}

func (op *Operation) commit(ctx context.Context) (CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return op.failCommit(err)
	}
	if err := op.verifyFileState(); err != nil {
		return op.failCommit(err)
	}
	if err := op.store.applyCryptoProfile(op.database); err != nil {
		return op.failCommit(err)
	}
	data, err := op.store.encodeDatabase(op.database)
	if err != nil {
		return op.failCommit(err)
	}
	if err := ctx.Err(); err != nil {
		return op.failCommit(err)
	}
	parent := filepath.Dir(op.binding.Path)
	tempPath, err := op.store.writePrivateFile(parent, "", data)
	if err != nil {
		return op.failCommit(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := op.verifyFileState(); err != nil {
		return op.failCommit(err)
	}
	backupID, err := op.store.newUUID()
	if err != nil {
		return op.failCommit(err)
	}
	backupPath := filepath.Join(parent, strings.TrimSuffix(filepath.Base(op.binding.Path), filepath.Ext(op.binding.Path))+".pre-update-"+backupID+".bak")
	backup, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return op.failCommit(err)
	}
	if err := op.store.hooks.secure(backupPath); err != nil {
		_ = backup.Close()
		_ = os.Remove(backupPath)
		return op.failCommit(err)
	}
	if _, err := backup.Write(op.snapshot); err != nil {
		_ = backup.Close()
		_ = os.Remove(backupPath)
		return op.failCommit(err)
	}
	if err := backup.Sync(); err != nil {
		_ = backup.Close()
		_ = os.Remove(backupPath)
		return op.failCommit(err)
	}
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return op.failCommit(err)
	}
	if err := op.verifyFileState(); err != nil {
		return op.failCommit(err)
	}
	if err := ctx.Err(); err != nil {
		return op.failCommit(err)
	}
	if err := op.aliveLocked(); err != nil {
		return op.failCommit(err)
	}
	published, err := op.store.hooks.install(tempPath, op.binding.Path, true)
	if err != nil {
		if published {
			committed = true
			op.closed = true
			op.discardLocked()
			return CommitResult{Committed: true, BackupPath: backupPath}, &CommittedWarning{Cause: err}
		}
		return op.failCommit(err)
	}
	committed = true
	op.digest = sha256.Sum256(data)
	op.snapshot = data
	result := CommitResult{Committed: true, BackupPath: backupPath}
	var warning error
	if err := op.store.hooks.syncDir(parent); err != nil {
		warning = &CommittedWarning{Cause: err}
	}
	if info, statErr := os.Stat(op.binding.Path); statErr == nil {
		op.fileInfo = info
	} else if warning == nil {
		warning = &CommittedWarning{Cause: statErr}
	}
	return result, warning
}

func (op *Operation) Upsert(ctx context.Context, records []Record) (CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveLocked(); err != nil {
		return CommitResult{}, err
	}
	accounts, files, err := managedGroups(op.database)
	if err != nil {
		return CommitResult{}, err
	}
	seen := make(map[Ref]struct{}, len(records))
	type plan struct {
		record   *Record
		group    *kp.Group
		existing *kp.Entry
	}
	plans := make([]plan, 0, len(records))
	additions := 0
	for index := range records {
		record := &records[index]
		if err := validateRecord(record); err != nil {
			return CommitResult{}, err
		}
		if record.Ref.VaultID != op.binding.VaultID {
			return CommitResult{}, fmt.Errorf("%w: record vault does not match the open binding", ErrInvalidRecord)
		}
		if _, exists := seen[record.Ref]; exists {
			return CommitResult{}, fmt.Errorf("%w: duplicate record reference", ErrInvalidRecord)
		}
		seen[record.Ref] = struct{}{}
		group := files
		if record.Ref.ItemID == itemIDAccount {
			group = accounts
		}
		existing, err := findManagedEntry(group, record.Ref)
		if err != nil {
			return CommitResult{}, err
		}
		if existing != nil && record.Ref.ItemID == itemIDAccount && record.Generation < entryGeneration(existing) {
			return CommitResult{}, fmt.Errorf("%w: record generation regressed", ErrConflict)
		}
		if existing == nil {
			additions++
		}
		plans = append(plans, plan{record: record, group: group, existing: existing})
	}
	count, err := validateManagedStructure(op.database)
	if err != nil {
		return CommitResult{}, err
	}
	if count+additions > maxManagedEntries {
		return CommitResult{}, fmt.Errorf("%w: managed entry budget exceeded", ErrInvalidRecord)
	}
	for _, item := range plans {
		if item.existing != nil {
			updateEntryFromRecord(item.existing, item.record)
		}
	}
	for _, item := range plans {
		if item.existing != nil {
			continue
		}
		entry := recordToEntry(item.record)
		id, err := op.store.entryUUID()
		if err != nil {
			return op.failCommit(err)
		}
		entry.UUID = id
		item.group.Entries = append(item.group.Entries, entry)
	}
	return op.commit(ctx)
}

func (op *Operation) RemoveAccount(ctx context.Context, accountID string) (CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return CommitResult{}, err
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveLocked(); err != nil {
		return CommitResult{}, err
	}
	if !isUUIDv4(accountID) {
		return CommitResult{}, fmt.Errorf("%w: account identifier must be a canonical UUIDv4", ErrInvalidRecord)
	}
	accounts, files, err := managedGroups(op.database)
	if err != nil {
		return CommitResult{}, err
	}
	match := func(entry *kp.Entry) bool {
		return managedEntry(entry) &&
			entryGet(entry, customVaultID) == op.binding.VaultID &&
			entryGet(entry, customAccountID) == accountID
	}
	removed := removeEntries(accounts, match)
	removed = append(removed, removeEntries(files, match)...)
	if len(removed) == 0 {
		return CommitResult{}, ErrNotFound
	}
	now := op.store.options.Now().UTC()
	for _, id := range removed {
		op.database.Content.Root.DeletedObjects = append(op.database.Content.Root.DeletedObjects, kp.DeletedObjectData{
			UUID:         id,
			DeletionTime: &w.TimeWrapper{Time: now},
		})
	}
	return op.commit(ctx)
}

func (op *Operation) WithPassword(ctx context.Context, ref Ref, generation uint64, fn func([]byte) error) error {
	if fn == nil {
		return fmt.Errorf("password callback is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRef(ref); err != nil {
		return err
	}
	if ref.VaultID != op.binding.VaultID {
		return fmt.Errorf("%w: record vault does not match the open binding", ErrInvalidRecord)
	}
	op.mu.Lock()
	var transient []byte
	err := func() error {
		defer op.mu.Unlock()
		if err := op.aliveLocked(); err != nil {
			return err
		}
		accounts, _, err := managedGroups(op.database)
		if err != nil {
			return err
		}
		entry, err := findManagedEntry(accounts, ref)
		if err != nil {
			return err
		}
		if entry == nil {
			return ErrNotFound
		}
		if ref.ItemID == itemIDAccount && entryGeneration(entry) != generation {
			return fmt.Errorf("%w: record generation does not match", ErrConflict)
		}
		value, ok := entryValue(entry, "Password")
		if !ok {
			return ErrNotFound
		}
		transient = []byte(value.Content)
		return nil
	}()
	if err != nil {
		return err
	}
	defer clear(transient)
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(transient)
}

func (op *Operation) WithFilePassword(ctx context.Context, kind, digest string, fn func([]byte) error) error {
	if fn == nil {
		return fmt.Errorf("password callback is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if (kind != kindKeystoreV3 && kind != kindBlocoCipher) || !digestPattern.MatchString(digest) {
		return fmt.Errorf("%w: file kind or digest is outside the managed schema", ErrInvalidRecord)
	}
	op.mu.Lock()
	var transient []byte
	err := func() error {
		defer op.mu.Unlock()
		if err := op.aliveLocked(); err != nil {
			return err
		}
		_, files, err := managedGroups(op.database)
		if err != nil {
			return err
		}
		var password string
		matched := false
		var walkErr error
		forEachEntry(files, func(entry *kp.Entry) {
			if walkErr != nil || !managedEntry(entry) || entryGet(entry, customKind) != kind || entryGet(entry, customDigest) != digest {
				return
			}
			value, ok := entryValue(entry, "Password")
			if !ok {
				walkErr = fmt.Errorf("%w: managed file record is missing its password", ErrInvalidRecord)
				return
			}
			if !matched {
				matched = true
				password = value.Content
				return
			}
			if password != value.Content {
				walkErr = ErrAmbiguous
			}
		})
		if walkErr != nil {
			return walkErr
		}
		if !matched {
			return ErrNotFound
		}
		transient = []byte(password)
		return nil
	}()
	if err != nil {
		return err
	}
	defer clear(transient)
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(transient)
}

func (op *Operation) List(ctx context.Context) ([]Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	op.mu.Lock()
	defer op.mu.Unlock()
	if err := op.aliveLocked(); err != nil {
		return nil, err
	}
	accounts, files, err := managedGroups(op.database)
	if err != nil {
		return nil, err
	}
	result := make([]Metadata, 0, 32)
	var walkErr error
	collect := func(entry *kp.Entry) {
		if walkErr != nil || !managedEntry(entry) {
			return
		}
		result = append(result, metadataFromEntry(entry))
	}
	forEachEntry(accounts, collect)
	forEachEntry(files, collect)
	if walkErr != nil {
		return nil, walkErr
	}
	return result, nil
}

func (op *Operation) Close() {
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.closed {
		return
	}
	op.closed = true
	op.discardLocked()
}
