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
	CurrentVersion = 1
	// MaxArchiveBytes bounds a single backup archive.
	MaxArchiveBytes = 128 << 20
	// MaxAccounts bounds restored accounts.
	MaxAccounts = 256
	// KDFSaltBytes is the Argon2 salt size stored in the archive.
	KDFSaltBytes = 16
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
	Derivation         string `json:"derivation,omitempty"`
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

// Create seals an archive for the given accounts.
func (sealer *Sealer) Create(password []byte, schemaVersion int, accounts []wallet.Account, nowMS int64) ([]byte, error) {
	if sealer == nil {
		return nil, fmt.Errorf("backup: nil sealer")
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("backup: password required")
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
		entry := AccountEntry{
			AccountID: account.AccountID, Name: account.Name, Address: account.Address,
			SignerKind: string(account.SignerKind), SignerReference: account.SignerReference,
			State: string(account.State), Capabilities: uint64(account.Capabilities),
			SecretType:         string(account.SecretType),
			SourceIdentity:     account.SourceIdentity,
			AuthorizationEpoch: account.AuthorizationEpoch,
			EnvelopeGeneration: account.EnvelopeGeneration,
			BackupGeneration:   account.BackupGeneration,
		}
		if len(account.SecretEnvelope) > 0 {
			entry.SecretEnvelope = append([]byte(nil), account.SecretEnvelope...)
		}
		if account.DerivationScheme != "" {
			entry.Derivation = account.DerivationScheme + ":" + account.DerivationPath
		}
		manifest.Accounts = append(manifest.Accounts, entry)
	}
	return sealer.seal(password, manifest)
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
	sealed := gcm.Seal(nil, iv, plaintext, nil)
	header := make([]byte, 0, 16+KDFSaltBytes+len(iv)+len(sealed))
	header = append(header, []byte(Magic)...)
	header = append(header, 0)
	header = append(header, byte(CurrentVersion))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(plaintext)))
	header = append(header, length[:]...)
	header = append(header, salt...)
	header = append(header, iv...)
	header = append(header, sealed...)
	return header, nil
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
	if len(password) == 0 {
		return nil, fmt.Errorf("backup: password required")
	}
	headerSize := len(Magic) + 1 + 1 + 4 + KDFSaltBytes
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
	salt := archive[len(Magic)+6 : headerSize]
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
	nonceSize := gcm.NonceSize()
	if len(archive) < headerSize+nonceSize+16 {
		return nil, fmt.Errorf("backup: archive truncated")
	}
	plaintext, err := gcm.Open(nil, archive[headerSize:headerSize+nonceSize], archive[headerSize+nonceSize:], nil)
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
