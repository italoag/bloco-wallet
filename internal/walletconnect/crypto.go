// Package walletconnect implements the WalletConnect v2 client core used by
// the wallet: pairing cryptography, message envelopes, session scopes, and
// relay JSON-RPC. Every signing request still crosses the transaction engine
// and approval policy; sessions never auto-approve signing.
package walletconnect

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	// MaxEnvelopeBytes bounds a decrypted envelope payload.
	MaxEnvelopeBytes = 1 << 20
	// MaxMessageBytes bounds a single relay message.
	MaxMessageBytes = 1 << 20
	// MaxTopicLength is the hex length of a 32-byte topic.
	MaxTopicLength = 64
	// SymKeyBytes is the AES-256 key size.
	SymKeyBytes = 32
	// IVBytes is the AES-GCM nonce size.
	IVBytes = 12
)

// KeyPair is a WalletConnect pairing key pair.
type KeyPair struct {
	privateKey *ecdh.PrivateKey
	PublicKey  []byte
}

// GenerateKeyPair creates a fresh X25519 key pair for pairing.
func GenerateKeyPair() (*KeyPair, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: generate key pair: %w", err)
	}
	return &KeyPair{privateKey: privateKey, PublicKey: privateKey.PublicKey().Bytes()}, nil
}

// ParsePrivateKey reconstructs a key pair from its raw private key.
func ParsePrivateKey(raw []byte) (*KeyPair, error) {
	if len(raw) != 32 {
		return nil, fmt.Errorf("walletconnect: invalid private key size")
	}
	privateKey, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: invalid private key: %w", err)
	}
	return &KeyPair{privateKey: privateKey, PublicKey: privateKey.PublicKey().Bytes()}, nil
}

// PrivateKey returns the raw private key bytes.
func (pair *KeyPair) PrivateKey() []byte {
	return append([]byte(nil), pair.privateKey.Bytes()...)
}

// PairingTopic derives the pairing topic from both public keys.
func PairingTopic(publicKeyA, publicKeyB []byte) string {
	hash := sha256.Sum256(append(append([]byte(nil), publicKeyA...), publicKeyB...))
	return hex.EncodeToString(hash[:])
}

// deriveSymmetricKey computes the shared AES-256 key from a peer public key
// using the WalletConnect v2 derivation (HKDF-SHA256 seeded by the ECDH
// shared secret).
func (pair *KeyPair) deriveSymmetricKey(peerPublicKey []byte) ([]byte, error) {
	peerKey, err := ecdh.X25519().NewPublicKey(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: invalid peer public key: %w", err)
	}
	shared, err := pair.privateKey.ECDH(peerKey)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: ecdh: %w", err)
	}
	key := make([]byte, SymKeyBytes)
	reader := hkdf.New(sha256.New, shared, nil, nil)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("walletconnect: derive symmetric key: %w", err)
	}
	return key, nil
}

// SymmetricKey derives the session symmetric key from our key pair and the
// peer public key.
func SymmetricKey(pair *KeyPair, peerPublicKey []byte) ([]byte, error) {
	return pair.deriveSymmetricKey(peerPublicKey)
}

// EncryptEnvelope seals a plaintext payload with AES-256-GCM and returns
// iv || ciphertext || tag.
func EncryptEnvelope(key, plaintext []byte) ([]byte, error) {
	if len(key) != SymKeyBytes {
		return nil, fmt.Errorf("walletconnect: invalid symmetric key size")
	}
	if len(plaintext) > MaxEnvelopeBytes {
		return nil, fmt.Errorf("walletconnect: envelope too large")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: gcm: %w", err)
	}
	iv := make([]byte, IVBytes)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("walletconnect: nonce: %w", err)
	}
	sealed := gcm.Seal(nil, iv, plaintext, nil)
	envelope := make([]byte, 0, IVBytes+len(sealed))
	envelope = append(envelope, iv...)
	envelope = append(envelope, sealed...)
	return envelope, nil
}

// DecryptEnvelope opens an envelope produced by EncryptEnvelope.
func DecryptEnvelope(key, envelope []byte) ([]byte, error) {
	if len(key) != SymKeyBytes {
		return nil, fmt.Errorf("walletconnect: invalid symmetric key size")
	}
	if len(envelope) < IVBytes+16 {
		return nil, fmt.Errorf("walletconnect: envelope too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, envelope[:IVBytes], envelope[IVBytes:], nil)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: envelope authentication failed")
	}
	if len(plaintext) > MaxEnvelopeBytes {
		return nil, fmt.Errorf("walletconnect: envelope too large")
	}
	return plaintext, nil
}

// ValidateTopic rejects malformed hex topics.
func ValidateTopic(topic string) error {
	if len(topic) != MaxTopicLength {
		return fmt.Errorf("walletconnect: invalid topic length")
	}
	if _, err := hex.DecodeString(topic); err != nil {
		return fmt.Errorf("walletconnect: invalid topic encoding")
	}
	return nil
}
