package wallet

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	secretEnvelopeVersion  = 1
	argon2idAlgorithm      = "argon2id"
	xchacha20Algorithm     = "xchacha20-poly1305"
	maxSecretPlaintextSize = 64 * 1024
	maxEncodedEnvelopeSize = 256 * 1024
)

var envelopeKDFSemaphore = make(chan struct{}, 1)

type SecretType string

const (
	SecretTypeMnemonic   SecretType = "mnemonic"
	SecretTypePrivateKey SecretType = "private_key"
)

type DerivationMetadata struct {
	Scheme       string
	Path         string
	AccountIndex uint32
	ChangeIndex  uint32
	AddressIndex uint32
	Language     string
}

type EnvelopeMetadata struct {
	AccountID          string
	SecretType         SecretType
	Address            string
	EnvelopeGeneration uint64
	PassphrasePresent  bool
	Derivation         DerivationMetadata
}

type Argon2idPolicy struct {
	Time           uint32
	MemoryKiB      uint32
	Parallelism    uint8
	KeyLength      uint32
	SaltLength     uint32
	MaxTime        uint32
	MaxMemoryKiB   uint32
	MaxParallelism uint8
	MaxKeyLength   uint32
	MaxSaltLength  uint32
}

func ProductionArgon2idPolicy() Argon2idPolicy {
	return Argon2idPolicy{
		Time:           3,
		MemoryKiB:      64 * 1024,
		Parallelism:    4,
		KeyLength:      chacha20poly1305.KeySize,
		SaltLength:     16,
		MaxTime:        10,
		MaxMemoryKiB:   256 * 1024,
		MaxParallelism: 8,
		MaxKeyLength:   chacha20poly1305.KeySize,
		MaxSaltLength:  64,
	}
}

type EnvelopeKDFV1 struct {
	Algorithm   string `json:"algorithm"`
	Time        uint32 `json:"time"`
	MemoryKiB   uint32 `json:"memory_kib"`
	Parallelism uint8  `json:"parallelism"`
	KeyLength   uint32 `json:"key_length"`
	Salt        []byte `json:"salt"`
}

type EnvelopeCipherV1 struct {
	Algorithm string `json:"algorithm"`
	Nonce     []byte `json:"nonce"`
}

type SecretEnvelopeV1 struct {
	Version    uint32           `json:"version"`
	KDF        EnvelopeKDFV1    `json:"kdf"`
	Cipher     EnvelopeCipherV1 `json:"cipher"`
	Ciphertext []byte           `json:"ciphertext"`
}

type SecretEnvelope interface {
	Seal(password []byte, metadata EnvelopeMetadata, plaintext []byte) ([]byte, error)
	Open(password []byte, metadata EnvelopeMetadata, encoded []byte) ([]byte, error)
}

type SecretEnvelopeCodec struct {
	policy Argon2idPolicy
}

func NewSecretEnvelopeCodec(policy Argon2idPolicy) (*SecretEnvelopeCodec, error) {
	if err := validateArgon2Policy(policy); err != nil {
		return nil, err
	}
	return &SecretEnvelopeCodec{policy: policy}, nil
}

func (codec *SecretEnvelopeCodec) Seal(password []byte, metadata EnvelopeMetadata, plaintext []byte) ([]byte, error) {
	if codec == nil {
		return nil, fmt.Errorf("envelope codec is required")
	}
	if err := validateNewStoragePassword(password); err != nil {
		return nil, err
	}
	if err := validateEnvelopeMetadata(metadata); err != nil {
		return nil, err
	}
	if len(plaintext) == 0 || len(plaintext) > maxSecretPlaintextSize {
		return nil, fmt.Errorf("secret plaintext size is outside policy")
	}

	salt := make([]byte, codec.policy.SaltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate envelope salt: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate envelope nonce: %w", err)
	}
	key := deriveEnvelopeKey(password, salt, codec.policy.Time, codec.policy.MemoryKiB, codec.policy.Parallelism, codec.policy.KeyLength)
	defer clear(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create envelope cipher: %w", err)
	}
	aad, err := encodeEnvelopeAAD(secretEnvelopeVersion, metadata)
	if err != nil {
		return nil, err
	}
	envelope := SecretEnvelopeV1{
		Version: secretEnvelopeVersion,
		KDF: EnvelopeKDFV1{
			Algorithm:   argon2idAlgorithm,
			Time:        codec.policy.Time,
			MemoryKiB:   codec.policy.MemoryKiB,
			Parallelism: codec.policy.Parallelism,
			KeyLength:   codec.policy.KeyLength,
			Salt:        salt,
		},
		Cipher: EnvelopeCipherV1{
			Algorithm: xchacha20Algorithm,
			Nonce:     nonce,
		},
	}
	envelope.Ciphertext = aead.Seal(nil, nonce, plaintext, aad)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode secret envelope: %w", err)
	}
	return encoded, nil
}

func (codec *SecretEnvelopeCodec) Open(password []byte, metadata EnvelopeMetadata, encoded []byte) ([]byte, error) {
	if codec == nil {
		return nil, fmt.Errorf("envelope codec is required")
	}
	if err := validateEnvelopeMetadata(metadata); err != nil {
		return nil, err
	}
	if len(encoded) == 0 || len(encoded) > maxEncodedEnvelopeSize {
		return nil, fmt.Errorf("encoded envelope size is outside policy")
	}
	var envelope SecretEnvelopeV1
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode secret envelope: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if err := codec.validateEnvelope(envelope); err != nil {
		return nil, err
	}
	key := deriveEnvelopeKey(password, envelope.KDF.Salt, envelope.KDF.Time, envelope.KDF.MemoryKiB, envelope.KDF.Parallelism, envelope.KDF.KeyLength)
	defer clear(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create envelope cipher: %w", err)
	}
	aad, err := encodeEnvelopeAAD(envelope.Version, metadata)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, envelope.Cipher.Nonce, envelope.Ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("authenticate secret envelope: %w", err)
	}
	return plaintext, nil
}

func (codec *SecretEnvelopeCodec) validateEnvelope(envelope SecretEnvelopeV1) error {
	if envelope.Version != secretEnvelopeVersion {
		return fmt.Errorf("unsupported secret envelope version")
	}
	if envelope.KDF.Algorithm != argon2idAlgorithm {
		return fmt.Errorf("unsupported envelope KDF")
	}
	if envelope.Cipher.Algorithm != xchacha20Algorithm {
		return fmt.Errorf("unsupported envelope cipher")
	}
	if envelope.KDF.Time == 0 || envelope.KDF.Time > codec.policy.MaxTime {
		return fmt.Errorf("envelope KDF time is outside policy")
	}
	if envelope.KDF.MemoryKiB < 8 || envelope.KDF.MemoryKiB > codec.policy.MaxMemoryKiB {
		return fmt.Errorf("envelope KDF memory is outside policy")
	}
	if envelope.KDF.Parallelism == 0 || envelope.KDF.Parallelism > codec.policy.MaxParallelism {
		return fmt.Errorf("envelope KDF parallelism is outside policy")
	}
	if envelope.KDF.KeyLength != chacha20poly1305.KeySize || envelope.KDF.KeyLength > codec.policy.MaxKeyLength {
		return fmt.Errorf("invalid envelope key length")
	}
	if len(envelope.KDF.Salt) < 16 || uint32(len(envelope.KDF.Salt)) > codec.policy.MaxSaltLength {
		return fmt.Errorf("invalid envelope salt length")
	}
	if len(envelope.Cipher.Nonce) != chacha20poly1305.NonceSizeX {
		return fmt.Errorf("invalid envelope nonce length")
	}
	if len(envelope.Ciphertext) < chacha20poly1305.Overhead || len(envelope.Ciphertext) > maxSecretPlaintextSize+chacha20poly1305.Overhead {
		return fmt.Errorf("invalid envelope ciphertext")
	}
	return nil
}

func deriveEnvelopeKey(password, salt []byte, timeCost, memoryKiB uint32, parallelism uint8, keyLength uint32) []byte {
	envelopeKDFSemaphore <- struct{}{}
	defer func() { <-envelopeKDFSemaphore }()
	return argon2.IDKey(password, salt, timeCost, memoryKiB, parallelism, keyLength)
}

func validateArgon2Policy(policy Argon2idPolicy) error {
	if policy.Time == 0 || policy.MaxTime < policy.Time {
		return fmt.Errorf("invalid Argon2id time policy")
	}
	if policy.MemoryKiB < 8 || policy.MaxMemoryKiB < policy.MemoryKiB {
		return fmt.Errorf("invalid Argon2id memory policy")
	}
	if policy.Parallelism == 0 || policy.MaxParallelism < policy.Parallelism {
		return fmt.Errorf("invalid Argon2id parallelism policy")
	}
	if policy.KeyLength != chacha20poly1305.KeySize || policy.MaxKeyLength != chacha20poly1305.KeySize {
		return fmt.Errorf("Argon2id key length must be 32")
	}
	if policy.SaltLength < 16 || policy.MaxSaltLength < policy.SaltLength {
		return fmt.Errorf("invalid Argon2id salt policy")
	}
	return nil
}

func ValidateStoragePassword(password []byte) error {
	return validateNewStoragePassword(password)
}

func validateNewStoragePassword(password []byte) error {
	if len(password) > 4096 {
		return fmt.Errorf("storage password size is outside policy")
	}
	if !utf8.Valid(password) {
		return fmt.Errorf("storage password must be valid UTF-8")
	}
	if utf8.RuneCount(password) < 15 {
		return fmt.Errorf("storage password must contain at least 15 characters")
	}
	for remaining := password; len(remaining) > 0; {
		value, size := utf8.DecodeRune(remaining)
		if !unicode.IsPrint(value) {
			return fmt.Errorf("storage password must contain only printable Unicode")
		}
		remaining = remaining[size:]
	}
	return nil
}

func validateEnvelopeMetadata(metadata EnvelopeMetadata) error {
	if metadata.AccountID == "" || metadata.Address == "" || metadata.EnvelopeGeneration == 0 {
		return fmt.Errorf("account identity metadata is required")
	}
	if metadata.SecretType != SecretTypeMnemonic && metadata.SecretType != SecretTypePrivateKey {
		return fmt.Errorf("invalid secret type")
	}
	return nil
}

func encodeEnvelopeAAD(version uint32, metadata EnvelopeMetadata) ([]byte, error) {
	var buffer bytes.Buffer
	if err := binary.Write(&buffer, binary.BigEndian, version); err != nil {
		return nil, err
	}
	if err := binary.Write(&buffer, binary.BigEndian, metadata.EnvelopeGeneration); err != nil {
		return nil, err
	}
	if metadata.PassphrasePresent {
		buffer.WriteByte(1)
	} else {
		buffer.WriteByte(0)
	}
	values := []string{
		metadata.AccountID,
		string(metadata.SecretType),
		metadata.Address,
		metadata.Derivation.Scheme,
		metadata.Derivation.Path,
		metadata.Derivation.Language,
	}
	for _, value := range values {
		if len(value) > 1<<20 {
			return nil, fmt.Errorf("envelope metadata field is too large")
		}
		if err := binary.Write(&buffer, binary.BigEndian, uint32(len(value))); err != nil {
			return nil, err
		}
		if _, err := buffer.WriteString(value); err != nil {
			return nil, err
		}
	}
	for _, value := range []uint32{metadata.Derivation.AccountIndex, metadata.Derivation.ChangeIndex, metadata.Derivation.AddressIndex} {
		if err := binary.Write(&buffer, binary.BigEndian, value); err != nil {
			return nil, err
		}
	}
	return buffer.Bytes(), nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("secret envelope contains trailing data")
		}
		return fmt.Errorf("decode trailing envelope data: %w", err)
	}
	return nil
}
