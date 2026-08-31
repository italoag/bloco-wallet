// Package backup implements versioned, authenticated vault backups.
// A backup is a single encrypted file containing the manifest (account
// metadata) and every secret envelope, sealed with AES-256-GCM under a
// password-derived key. Restore runs in staging: the archive is decrypted,
// every account is validated and every envelope is reopened before the
// active vault is touched. Corrupt or wrong backups cannot replace the
// active database.
package backup

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"blocowallet/internal/wallet"
)

const (
	// Magic identifies the backup format.
	Magic = "BLOCWBK1"
	// CurrentVersion is the backup format version.
	CurrentVersion = 2
	// MaxArchiveBytes bounds a single backup archive.
	MaxArchiveBytes = 128 << 20
	// MaxAccounts bounds restored accounts.
	MaxAccounts = 256
	// KDFSaltBytes is the Argon2 salt size stored in the archive.
	KDFSaltBytes = 16
	// MinPasswordRunes is the minimum archive password strength.
	MinPasswordRunes = 15
)

// AccountEntry is one account inside the archive: public metadata plus the
// encrypted secret envelope bytes.
type AccountEntry struct {
	AccountID          string `json:"account_id"`
	Name               string `json:"name"`
	Address            string `json:"address"`
	SignerKind         string `json:"signer_kind"`
	SignerReference    string `json:"signer_reference"`
	State              string `json:"state"`
	Capabilities       uint64 `json:"capabilities"`
	SecretType         string `json:"secret_type"`
	SecretEnvelope     []byte `json:"secret_envelope,omitempty"`
	DerivationScheme   string `json:"derivation_scheme,omitempty"`
	DerivationPath     string `json:"derivation_path,omitempty"`
	AccountIndex       uint32 `json:"account_index,omitempty"`
	ChangeIndex        uint32 `json:"change_index,omitempty"`
	AddressIndex       uint32 `json:"address_index,omitempty"`
	HasBIP39Passphrase bool   `json:"has_bip39_passphrase,omitempty"`
	SourceIdentity     string `json:"source_identity"`
	BIP39Language      string `json:"bip39_language,omitempty"`
	AuthorizationEpoch uint64 `json:"authorization_epoch"`
	EnvelopeGeneration uint64 `json:"envelope_generation"`
	BackupGeneration   uint64 `json:"backup_generation"`
}

// Manifest is the authenticated metadata of the archive.
type Manifest struct {
	Magic       string         `json:"magic"`
	Version     int            `json:"version"`
	Schema      int            `json:"schema_version"`
	CreatedAtMS int64          `json:"created_at_ms"`
	Accounts    []AccountEntry `json:"accounts"`
}

// Archive is the decrypted backup payload.
type Archive struct {
	Manifest Manifest
}

// KeyDeriver derives the archive sealing key from the password.
type KeyDeriver interface {
	DeriveKey(password []byte, salt []byte) ([]byte, error)
}

// Sealer creates and opens archives.
type Sealer struct {
	deriver KeyDeriver
}

// NewSealer creates a backup sealer.
func NewSealer(deriver KeyDeriver) (*Sealer, error) {
	if deriver == nil {
		return nil, fmt.Errorf("backup: key deriver required")
	}
	return &Sealer{deriver: deriver}, nil
}

// Create seals an archive for the given accounts. Every account is
// validated before sealing; a broken account cannot produce a backup.
func (sealer *Sealer) Create(password []byte, schemaVersion int, accounts []wallet.Account, nowMS int64) ([]byte, error) {
	if sealer == nil {
		return nil, fmt.Errorf("backup: nil sealer")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	if schemaVersion <= 0 {
		return nil, fmt.Errorf("backup: invalid schema version")
	}
	if len(accounts) == 0 || len(accounts) > MaxAccounts {
		return nil, fmt.Errorf("backup: account count out of range")
	}
	manifest := Manifest{
		Magic: Magic, Version: CurrentVersion, Schema: schemaVersion,
		CreatedAtMS: nowMS,
		Accounts:    make([]AccountEntry, 0, len(accounts)),
	}
	for _, account := range accounts {
		if err := account.Validate(); err != nil {
			return nil, fmt.Errorf("backup: account %s: %w", account.AccountID, err)
		}
		if len(account.SecretEnvelope) > 0 && len(account.SecretEnvelope) < 16 {
			return nil, fmt.Errorf("backup: account %s has a truncated envelope", account.AccountID)
		}
		entry := AccountEntry{
			AccountID: account.AccountID, Name: account.Name, Address: account.Address,
			SignerKind: string(account.SignerKind), SignerReference: account.SignerReference,
			State: string(account.State), Capabilities: uint64(account.Capabilities),
			SecretType:       string(account.SecretType),
			DerivationScheme: account.DerivationScheme, DerivationPath: account.DerivationPath,
			AccountIndex: account.AccountIndex, ChangeIndex: account.ChangeIndex, AddressIndex: account.AddressIndex,
			HasBIP39Passphrase: account.HasBIP39Passphrase,
			SourceIdentity:     account.SourceIdentity, BIP39Language: string(account.BIP39Language),
			AuthorizationEpoch: account.AuthorizationEpoch,
			EnvelopeGeneration: account.EnvelopeGeneration,
			BackupGeneration:   account.BackupGeneration,
		}
		if len(account.SecretEnvelope) > 0 {
			entry.SecretEnvelope = append([]byte(nil), account.SecretEnvelope...)
		}
		manifest.Accounts = append(manifest.Accounts, entry)
	}
	return sealer.seal(password, manifest)
}

func validatePassword(password []byte) error {
	if len(password) == 0 {
		return fmt.Errorf("backup: password required")
	}
	runes := 0
	for _, character := range password {
		if character < 0x20 || character > 0x7e {
			return fmt.Errorf("backup: password must be printable ASCII")
		}
		runes++
	}
	if runes < MinPasswordRunes {
		return fmt.Errorf("backup: password must be at least %d characters", MinPasswordRunes)
	}
	return nil
}

func (sealer *Sealer) seal(password []byte, manifest Manifest) ([]byte, error) {
	plaintext, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("backup: encode manifest: %w", err)
	}
	if len(plaintext) > MaxArchiveBytes {
		return nil, fmt.Errorf("backup: archive too large")
	}
	salt := make([]byte, KDFSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("backup: salt entropy: %w", err)
	}
	key, err := sealer.deriver.DeriveKey(password, salt)
	if err != nil {
		return nil, fmt.Errorf("backup: derive key: %w", err)
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("backup: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("backup: gcm: %w", err)
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("backup: nonce entropy: %w", err)
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(plaintext)))
	header := make([]byte, 0, 16+KDFSaltBytes+3)
	header = append(header, []byte(Magic)...)
	header = append(header, 0)
	header = append(header, byte(CurrentVersion))
	header = append(header, length[:]...)
	header = append(header, salt...)
	// KDF parameters are persisted so archives survive parameter changes.
	timeCost, memory, threads := kdfParams(sealer.deriver)
	header = append(header, timeCost)
	header = append(header, memory)
	header = append(header, threads)
	// The header (magic, version, length, salt, kdf params) is bound as AEAD
	// data so tampering with any of those fields fails authentication.
	sealed := gcm.Seal(nil, iv, plaintext, header)
	archive := make([]byte, 0, len(header)+len(iv)+len(sealed))
	archive = append(archive, header...)
	archive = append(archive, iv...)
	archive = append(archive, sealed...)
	return archive, nil
}

// kdfParams reports the deriver parameters for persistence.
func kdfParams(deriver KeyDeriver) (timeCost, memoryKiB, threads byte) {
	argon, ok := deriver.(*Argon2idDeriver)
	if !ok {
		return 3, 64, 4
	}
	timeCostValue := argon.Time
	if timeCostValue == 0 {
		timeCostValue = 3
	}
	memoryValue := argon.Memory
	if memoryValue == 0 {
		memoryValue = 64 * 1024
	}
	threadsValue := argon.Threads
	if threadsValue == 0 {
		threadsValue = 4
	}
	if memoryValue/1024 > 255 {
		return 3, 64, 4
	}
	return byte(timeCostValue), byte(memoryValue / 1024), byte(threadsValue)
}

// paramsKeyDeriver supports explicit KDF parameters from the archive header.
type paramsKeyDeriver interface {
	DeriveKeyWithParams(password, salt []byte, timeCost, memory uint32, threads uint8) ([]byte, error)
}

type fallbackParamsDeriver struct {
	deriver KeyDeriver
}

func (fallback fallbackParamsDeriver) DeriveKeyWithParams(password, salt []byte, _, _ uint32, _ uint8) ([]byte, error) {
	return fallback.deriver.DeriveKey(password, salt)
}

func deriverWithParams(deriver KeyDeriver) paramsKeyDeriver {
	if typed, ok := deriver.(paramsKeyDeriver); ok {
		return typed
	}
	return fallbackParamsDeriver{deriver: deriver}
}

// Open verifies and decrypts an archive. It returns the manifest; callers
// must validate every account and reopen envelopes before activation.
func (sealer *Sealer) Open(password, archive []byte) (*Manifest, error) {
	if sealer == nil {
		return nil, fmt.Errorf("backup: nil sealer")
	}
	if len(archive) == 0 || len(archive) > MaxArchiveBytes {
		return nil, fmt.Errorf("backup: archive size")
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	headerSize := len(Magic) + 1 + 1 + 4 + KDFSaltBytes + 3
	if len(archive) < headerSize || !bytes.Equal(archive[:len(Magic)], []byte(Magic)) {
		return nil, fmt.Errorf("backup: invalid archive header")
	}
	if archive[len(Magic)] != 0 || archive[len(Magic)+1] != byte(CurrentVersion) {
		return nil, fmt.Errorf("backup: unsupported archive version")
	}
	plaintextLength := binary.BigEndian.Uint32(archive[len(Magic)+2 : len(Magic)+6])
	if int(plaintextLength) == 0 || int(plaintextLength) > MaxArchiveBytes {
		return nil, fmt.Errorf("backup: invalid archive length")
	}
	salt := archive[len(Magic)+6 : len(Magic)+6+KDFSaltBytes]
	kdfTime := uint32(archive[headerSize-3])
	kdfMemory := uint32(archive[headerSize-2]) * 1024
	kdfThreads := uint8(archive[headerSize-1])
	key, err := deriverWithParams(sealer.deriver).DeriveKeyWithParams(password, salt, kdfTime, kdfMemory, kdfThreads)
	if err != nil {
		return nil, fmt.Errorf("backup: derive key: %w", err)
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("backup: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("backup: gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(archive) < headerSize+nonceSize+16 {
		return nil, fmt.Errorf("backup: archive truncated")
	}
	header := archive[:headerSize]
	plaintext, err := gcm.Open(nil, archive[headerSize:headerSize+nonceSize], archive[headerSize+nonceSize:], header)
	if err != nil {
		return nil, fmt.Errorf("backup: authentication failed (wrong password or corrupt archive)")
	}
	if len(plaintext) != int(plaintextLength) {
		return nil, fmt.Errorf("backup: archive length mismatch")
	}
	var manifest Manifest
	if err := json.Unmarshal(plaintext, &manifest); err != nil {
		return nil, fmt.Errorf("backup: manifest decode: %w", err)
	}
	if manifest.Magic != Magic || manifest.Version != CurrentVersion || manifest.Schema <= 0 {
		return nil, fmt.Errorf("backup: invalid manifest")
	}
	if len(manifest.Accounts) == 0 || len(manifest.Accounts) > MaxAccounts {
		return nil, fmt.Errorf("backup: manifest account count")
	}
	return &manifest, nil
}

// VerifyHash returns a stable fingerprint for the archive.
func VerifyHash(archive []byte) [32]byte {
	return sha256.Sum256(archive)
}
