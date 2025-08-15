package wallet

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// ImportMethod represents the method used to import a wallet
type ImportMethod string

const (
	ImportMethodMnemonic   ImportMethod = "mnemonic"
	ImportMethodPrivateKey ImportMethod = "private_key"
	ImportMethodKeystore   ImportMethod = "keystore"
)

// EnhancedWallet represents an enhanced wallet with import method tracking
type EnhancedWallet struct {
	ID           int       `gorm:"primaryKey"`
	Name         string    `gorm:"not null"`
	Address      string    `gorm:"not null;index"` // Changed from uniqueIndex to regular index
	KeyStorePath string    `gorm:"not null"`
	Mnemonic     *string   `gorm:"type:text"`            // Nullable for private key imports
	ImportMethod string    `gorm:"not null"`             // Track import method
	SourceHash   string    `gorm:"uniqueIndex;not null"` // Unique constraint on source data
	CreatedAt    time.Time `gorm:"not null;autoCreateTime"`
}

// TableName define o nome da tabela no banco de dados
func (EnhancedWallet) TableName() string {
	return "wallets"
}

// EnhancedWalletDetails represents enhanced wallet details with KDF information
type EnhancedWalletDetails struct {
	Wallet       *EnhancedWallet
	Mnemonic     *string // Nullable
	PrivateKey   *ecdsa.PrivateKey
	PublicKey    *ecdsa.PublicKey
	ImportMethod ImportMethod
	HasMnemonic  bool     // Helper field for UI
	KDFInfo      *KDFInfo // KDF analysis information
}

// KDFInfo informações sobre o KDF usado
type KDFInfo struct {
	Type           string                 `json:"type"`
	NormalizedType string                 `json:"normalized_type"`
	SecurityLevel  string                 `json:"security_level"`
	Parameters     map[string]interface{} `json:"parameters"`
}

// SourceHashGenerator generates hashes for duplicate detection
type SourceHashGenerator struct{}

// GenerateFromMnemonic generates a hash from a mnemonic phrase
func (g *SourceHashGenerator) GenerateFromMnemonic(mnemonic string) string {
	hash := sha256.Sum256([]byte(mnemonic))
	return hex.EncodeToString(hash[:])
}

// GenerateFromPrivateKey generates a hash from a private key
func (g *SourceHashGenerator) GenerateFromPrivateKey(privateKeyHex string) string {
	hash := sha256.Sum256([]byte(privateKeyHex))
	return hex.EncodeToString(hash[:])
}

// GenerateFromKeystore generates a hash from keystore JSON content
func (g *SourceHashGenerator) GenerateFromKeystore(keystoreJSON []byte) string {
	hash := sha256.Sum256(keystoreJSON)
	return hex.EncodeToString(hash[:])
}

// DuplicateWalletError represents a duplicate wallet error
type DuplicateWalletError struct {
	Type    string // "mnemonic", "private_key", "keystore"
	Address string
	Message string
}

func (e *DuplicateWalletError) Error() string {
	return fmt.Sprintf("duplicate wallet detected (%s): %s", e.Type, e.Message)
}

// InvalidImportDataError represents invalid import data error
type InvalidImportDataError struct {
	Type    string
	Message string
}

func (e *InvalidImportDataError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Type, e.Message)
}

// NewDuplicateWalletError creates a new duplicate wallet error
func NewDuplicateWalletError(importType, address, message string) *DuplicateWalletError {
	return &DuplicateWalletError{
		Type:    importType,
		Address: address,
		Message: message,
	}
}

// NewInvalidImportDataError creates a new invalid import data error
func NewInvalidImportDataError(importType, message string) *InvalidImportDataError {
	return &InvalidImportDataError{
		Type:    importType,
		Message: message,
	}
}
