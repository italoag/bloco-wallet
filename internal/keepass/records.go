package keepass

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	kp "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

const (
	customSchemaVersion  = "Bloco.SchemaVersion"
	customTargetID       = "Bloco.TargetID"
	customRole           = "Bloco.Role"
	customVaultID        = "Bloco.VaultID"
	customAccountID      = "Bloco.AccountID"
	customItemID         = "Bloco.ItemID"
	customGeneration     = "Bloco.Generation"
	customSecretType     = "Bloco.SecretType"
	customDerivationPath = "Bloco.DerivationPath"
	customLanguage       = "Bloco.Language"
	customKind           = "Bloco.Kind"
	customFileName       = "Bloco.FileName"
	customDigest         = "Bloco.Digest"

	secretMnemonic    = "Bloco.Mnemonic"
	secretPrivateKey  = "Bloco.PrivateKey"
	secretPassphrase  = "Bloco.BIP39Passphrase"
	schemaVersionV1   = "1"
	rootGroupName     = "Bloco Wallet"
	roleAccounts      = "accounts"
	roleFiles         = "files"
	itemIDAccount     = "account"
	kindAccount       = "account"
	kindKeystoreV3    = "keystore_v3"
	kindBlocoCipher   = "bloco_encrypted"
	fileItemPrefix    = "file:"
	maxSecretBytes    = 16 << 10
	maxMetadataBytes  = 4 << 10
	maxManagedEntries = 10000
)

var (
	uuidV4Pattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	digestPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	itemIDPattern  = regexp.MustCompile(`^(account|file:(keystore_v3|bloco_encrypted):[0-9a-f]{64})$`)
	privateKeyForm = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
)

func isUUIDv4(value string) bool {
	return uuidV4Pattern.MatchString(value)
}

type Binding struct {
	Path     string
	TargetID string
	VaultID  string
}

type Ref struct {
	VaultID   string
	AccountID string
	ItemID    string
}

type Record struct {
	Ref            Ref
	Title          string
	Address        string
	SecretType     string
	DerivationPath string
	Language       string
	Generation     uint64
	Kind           string
	FileName       string
	Digest         string
	Password       []byte
	Mnemonic       []byte
	PrivateKey     []byte
	Passphrase     []byte
}

type Metadata struct {
	Ref            Ref
	Title          string
	Address        string
	SecretType     string
	DerivationPath string
	Language       string
	Generation     uint64
	Kind           string
	FileName       string
	Digest         string
}

var errRecordSerialization = errors.New("keepass records cannot be serialized")

func (r Record) String() string {
	return fmt.Sprintf("keepass.Record{vault:%s account:%s item:%s kind:%s generation:%d}", r.Ref.VaultID, r.Ref.AccountID, r.Ref.ItemID, r.Kind, r.Generation)
}

func (r Record) GoString() string {
	return r.String()
}

func (r Record) MarshalJSON() ([]byte, error) {
	return nil, errRecordSerialization
}

func (r *Record) Destroy() {
	if r == nil {
		return
	}
	clear(r.Password)
	clear(r.Mnemonic)
	clear(r.PrivateKey)
	clear(r.Passphrase)
	r.Password = nil
	r.Mnemonic = nil
	r.PrivateKey = nil
	r.Passphrase = nil
}

func metadataWithinBounds(values ...string) bool {
	for _, value := range values {
		if len(value) > maxMetadataBytes || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return true
}

func validateRef(ref Ref) error {
	if !isUUIDv4(ref.VaultID) || !isUUIDv4(ref.AccountID) {
		return fmt.Errorf("%w: vault and account identifiers must be canonical UUIDv4", ErrInvalidRecord)
	}
	if !itemIDPattern.MatchString(ref.ItemID) {
		return fmt.Errorf("%w: item identifier is outside the managed schema", ErrInvalidRecord)
	}
	return nil
}

func validateRecord(record *Record) error {
	if record == nil {
		return fmt.Errorf("%w: record is required", ErrInvalidRecord)
	}
	if err := validateRef(record.Ref); err != nil {
		return err
	}
	if !metadataWithinBounds(record.Title, record.Address, record.SecretType, record.DerivationPath, record.Language, record.Kind, record.FileName, record.Digest) {
		return fmt.Errorf("%w: record metadata exceeds bounds", ErrInvalidRecord)
	}
	if len(record.Password) > maxSecretBytes || len(record.Mnemonic) > maxSecretBytes || len(record.PrivateKey) > maxSecretBytes || len(record.Passphrase) > maxSecretBytes {
		return fmt.Errorf("%w: secret field exceeds size budget", ErrInvalidRecord)
	}
	if record.Ref.ItemID == itemIDAccount {
		if record.Generation == 0 {
			return fmt.Errorf("%w: account records require a positive generation", ErrInvalidRecord)
		}
		if len(record.Password) == 0 {
			return fmt.Errorf("%w: account records require a password", ErrInvalidRecord)
		}
		if record.Kind != "" && record.Kind != kindAccount {
			return fmt.Errorf("%w: account item requires account kind", ErrInvalidRecord)
		}
		if record.Digest != "" || record.FileName != "" {
			return fmt.Errorf("%w: account item cannot carry file metadata", ErrInvalidRecord)
		}
		if len(record.PrivateKey) != 0 && !privateKeyForm.MatchString(string(record.PrivateKey)) {
			return fmt.Errorf("%w: private key must be a 0x-prefixed 64-hex value", ErrInvalidRecord)
		}
		return nil
	}
	if record.Kind != kindKeystoreV3 && record.Kind != kindBlocoCipher {
		return fmt.Errorf("%w: file record kind is outside the managed schema", ErrInvalidRecord)
	}
	expectedItem := fileItemPrefix + record.Kind + ":" + record.Digest
	if record.Ref.ItemID != expectedItem || !digestPattern.MatchString(record.Digest) {
		return fmt.Errorf("%w: file record digest does not match item identifier", ErrInvalidRecord)
	}
	if record.FileName == "" {
		return fmt.Errorf("%w: file records require a file name", ErrInvalidRecord)
	}
	if len(record.PrivateKey) != 0 || len(record.Mnemonic) != 0 || len(record.Passphrase) != 0 {
		return fmt.Errorf("%w: file records cannot carry account secrets", ErrInvalidRecord)
	}
	return nil
}

func entryValue(entry *kp.Entry, key string) (kp.V, bool) {
	for _, value := range entry.Values {
		if value.Key == key {
			return value.Value, true
		}
	}
	return kp.V{}, false
}

func entryGet(entry *kp.Entry, key string) string {
	value, ok := entryValue(entry, key)
	if !ok {
		return ""
	}
	return value.Content
}

func managedEntry(entry *kp.Entry) bool {
	return isUUIDv4(entryGet(entry, customVaultID)) &&
		isUUIDv4(entryGet(entry, customAccountID)) &&
		itemIDPattern.MatchString(entryGet(entry, customItemID)) &&
		entryGet(entry, customSchemaVersion) == schemaVersionV1
}

func entryRef(entry *kp.Entry) Ref {
	return Ref{
		VaultID:   entryGet(entry, customVaultID),
		AccountID: entryGet(entry, customAccountID),
		ItemID:    entryGet(entry, customItemID),
	}
}

func entryMatchesRef(entry *kp.Entry, ref Ref) bool {
	return entryRef(entry) == ref
}

func entryGeneration(entry *kp.Entry) uint64 {
	generation, err := strconv.ParseUint(entryGet(entry, customGeneration), 10, 64)
	if err != nil {
		return 0
	}
	return generation
}

func metadataFromEntry(entry *kp.Entry) Metadata {
	return Metadata{
		Ref:            entryRef(entry),
		Title:          entryGet(entry, "Title"),
		Address:        entryGet(entry, "UserName"),
		SecretType:     entryGet(entry, customSecretType),
		DerivationPath: entryGet(entry, customDerivationPath),
		Language:       entryGet(entry, customLanguage),
		Generation:     entryGeneration(entry),
		Kind:           entryGet(entry, customKind),
		FileName:       entryGet(entry, customFileName),
		Digest:         entryGet(entry, customDigest),
	}
}

func recordToEntry(record *Record) kp.Entry {
	entry := kp.NewEntry()
	entry.Values = ownedValues(record)
	return entry
}

var ownedEntryKeys = map[string]struct{}{
	"Title": {}, "UserName": {}, "Password": {},
	secretMnemonic: {}, secretPrivateKey: {}, secretPassphrase: {},
	customSchemaVersion: {}, customVaultID: {}, customAccountID: {}, customItemID: {},
	customGeneration: {}, customSecretType: {}, customDerivationPath: {}, customLanguage: {},
	customKind: {}, customFileName: {}, customDigest: {},
}

func ownedValues(record *Record) []kp.ValueData {
	values := []kp.ValueData{
		{Key: "Title", Value: kp.V{Content: record.Title}},
		{Key: "UserName", Value: kp.V{Content: record.Address}},
		{Key: "Password", Value: kp.V{Content: string(record.Password), Protected: w.NewBoolWrapper(true)}},
	}
	if len(record.Mnemonic) != 0 {
		values = append(values, kp.ValueData{Key: secretMnemonic, Value: kp.V{Content: string(record.Mnemonic), Protected: w.NewBoolWrapper(true)}})
	}
	if len(record.PrivateKey) != 0 {
		values = append(values, kp.ValueData{Key: secretPrivateKey, Value: kp.V{Content: string(record.PrivateKey), Protected: w.NewBoolWrapper(true)}})
	}
	if len(record.Passphrase) != 0 {
		values = append(values, kp.ValueData{Key: secretPassphrase, Value: kp.V{Content: string(record.Passphrase), Protected: w.NewBoolWrapper(true)}})
	}
	kind := record.Kind
	if kind == "" && record.Ref.ItemID == itemIDAccount {
		kind = kindAccount
	}
	values = append(values,
		kp.ValueData{Key: customSchemaVersion, Value: kp.V{Content: schemaVersionV1}},
		kp.ValueData{Key: customVaultID, Value: kp.V{Content: record.Ref.VaultID}},
		kp.ValueData{Key: customAccountID, Value: kp.V{Content: record.Ref.AccountID}},
		kp.ValueData{Key: customItemID, Value: kp.V{Content: record.Ref.ItemID}},
		kp.ValueData{Key: customGeneration, Value: kp.V{Content: strconv.FormatUint(record.Generation, 10)}},
		kp.ValueData{Key: customSecretType, Value: kp.V{Content: record.SecretType}},
		kp.ValueData{Key: customDerivationPath, Value: kp.V{Content: record.DerivationPath}},
		kp.ValueData{Key: customLanguage, Value: kp.V{Content: record.Language}},
		kp.ValueData{Key: customKind, Value: kp.V{Content: kind}},
		kp.ValueData{Key: customFileName, Value: kp.V{Content: record.FileName}},
		kp.ValueData{Key: customDigest, Value: kp.V{Content: record.Digest}},
	)
	return values
}

func updateEntryFromRecord(entry *kp.Entry, record *Record) {
	kept := entry.Values[:0]
	for _, value := range entry.Values {
		if _, owned := ownedEntryKeys[value.Key]; owned {
			continue
		}
		kept = append(kept, value)
	}
	entry.Values = append(kept, ownedValues(record)...)
}
