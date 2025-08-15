# Design Document

## Overview

Este documento descreve o design para corrigir dois bugs críticos na funcionalidade de importação de carteiras do BlocoWallet:

1. **Verificação incorreta de duplicatas** que impede a importação de múltiplas carteiras com mnemônicas diferentes
2. **Geração incorreta de mnemônica** a partir de chaves privadas, que é tecnicamente impossível

A solução envolve refatorar a lógica de verificação de duplicatas, modificar a estrutura de dados para suportar carteiras sem mnemônica, e corrigir a lógica de importação via chave privada.

## Architecture

### Current Architecture Issues

**Problema 1: Verificação de Duplicatas Inadequada**
- Atualmente usa apenas `Address` com `uniqueIndex` no modelo `Wallet`
- Diferentes mnemônicas podem derivar o mesmo endereço Ethereum
- Sistema rejeita incorretamente carteiras legítimas

**Problema 2: Geração Incorreta de Mnemônica**
- `ImportWalletFromPrivateKey` gera mnemônica "determinística" usando `GenerateDeterministicMnemonic`
- Conceitualmente incorreto: não é possível recuperar mnemônica original de chave privada
- Confunde usuários com informações falsas

**Problema 3: Compatibilidade Limitada de KeyStore**
- Validação KDF atual suporta apenas configurações padrão
- Type assertions rígidas falham com diferentes tipos JSON
- Nomenclatura case-sensitive rejeita variações válidas
- Suporte limitado a ~60% dos KeyStores V3 existentes

### Proposed Architecture

**Nova Estratégia de Verificação de Duplicatas:**
- Verificação baseada no **método de importação** e **dados de origem**
- Para mnemônicas: comparar mnemônicas descriptografadas
- Para chaves privadas: comparar apenas endereços
- Permitir coexistência de carteiras com mesmo endereço mas origens diferentes

**Estrutura de Dados Atualizada:**
- Campo `Mnemonic` torna-se opcional (nullable)
- Novo campo `ImportMethod` para rastrear origem da carteira
- Novo campo `SourceHash` para verificação de duplicatas eficiente

**Arquitetura Universal KDF:**
- Serviço `UniversalKDFService` com handlers plugáveis para diferentes KDFs
- Normalização automática de nomes KDF (case-insensitive)
- Conversão automática de tipos JSON (int, float64, string, json.Number)
- Suporte a múltiplas nomenclaturas de parâmetros
- Validação de segurança automática com análise de risco
- Compatibilidade com ~95% dos KeyStores V3 existentes

## Components and Interfaces

### 1. Wallet Model Updates

```go
type Wallet struct {
    ID           int       `gorm:"primaryKey"`
    Name         string    `gorm:"not null"`
    Address      string    `gorm:"not null"` // Remove uniqueIndex
    KeyStorePath string    `gorm:"not null"`
    Mnemonic     *string   `gorm:"type:text"` // Now nullable
    ImportMethod string    `gorm:"not null"`  // "mnemonic", "private_key", "keystore"
    SourceHash   string    `gorm:"uniqueIndex;not null"` // Hash of source data for duplicate detection
    CreatedAt    time.Time `gorm:"not null;autoCreateTime"`
}
```

### 2. Import Method Enum

```go
type ImportMethod string

const (
    ImportMethodMnemonic    ImportMethod = "mnemonic"
    ImportMethodPrivateKey  ImportMethod = "private_key"
    ImportMethodKeystore    ImportMethod = "keystore"
)
```

### 3. Enhanced WalletService Interface

```go
type WalletService interface {
    // Existing methods
    CreateWallet(name, password string) (*WalletDetails, error)
    ImportWallet(name, mnemonic, password string) (*WalletDetails, error)
    ImportWalletFromPrivateKey(name, privateKeyHex, password string) (*WalletDetails, error)
    ImportWalletFromKeystoreV3(name, keystorePath, password string) (*WalletDetails, error)
    
    // New methods for duplicate checking
    CheckDuplicateByMnemonic(mnemonic string) (*Wallet, error)
    CheckDuplicateByPrivateKey(privateKeyHex string) (*Wallet, error)
    CheckDuplicateBySourceHash(sourceHash string) (*Wallet, error)
}
```

### 4. Source Hash Generation

```go
type SourceHashGenerator struct{}

func (g *SourceHashGenerator) GenerateFromMnemonic(mnemonic string) string {
    hash := sha256.Sum256([]byte(mnemonic))
    return hex.EncodeToString(hash[:])
}

func (g *SourceHashGenerator) GenerateFromPrivateKey(privateKeyHex string) string {
    hash := sha256.Sum256([]byte(privateKeyHex))
    return hex.EncodeToString(hash[:])
}

func (g *SourceHashGenerator) GenerateFromKeystore(keystoreJSON []byte) string {
    hash := sha256.Sum256(keystoreJSON)
    return hex.EncodeToString(hash[:])
}
```

### 5. Enhanced Repository Interface

```go
type WalletRepository interface {
    AddWallet(wallet *Wallet) error
    GetAllWallets() ([]Wallet, error)
    DeleteWallet(walletID int) error
    FindBySourceHash(sourceHash string) (*Wallet, error) // New method
    FindByAddress(address string) ([]Wallet, error)      // Returns multiple wallets
    Close() error
}
```

### 6. Universal KDF Service Architecture

```go
// UniversalKDFService suporta qualquer configuração KDF válida
type UniversalKDFService struct {
    supportedKDFs map[string]KDFHandler
    logger        KDFLogger
}

// KDFHandler interface para diferentes tipos de KDF
type KDFHandler interface {
    DeriveKey(password string, params map[string]interface{}) ([]byte, error)
    ValidateParams(params map[string]interface{}) error
    GetDefaultParams() map[string]interface{}
    GetParamRange(param string) (min, max interface{})
}

// Enhanced KeyStore Service with Universal KDF
type EnhancedKeyStoreService struct {
    kdfService *UniversalKDFService
    logger     KDFLogger
}
```

### 7. KDF Compatibility Analysis

```go
// KDFCompatibilityAnalyzer analisa compatibilidade de KeyStores
type KDFCompatibilityAnalyzer struct {
    service *UniversalKDFService
}

// CompatibilityReport relatório de compatibilidade
type CompatibilityReport struct {
    Compatible      bool                   `json:"compatible"`
    KDFType         string                 `json:"kdf_type"`
    NormalizedKDF   string                 `json:"normalized_kdf"`
    Parameters      map[string]interface{} `json:"parameters"`
    SecurityLevel   string                 `json:"security_level"`
    Issues          []string               `json:"issues"`
    Warnings        []string               `json:"warnings"`
    Suggestions     []string               `json:"suggestions"`
}
```

## Data Models

### Updated Wallet Structure

```go
type Wallet struct {
    ID           int       `gorm:"primaryKey"`
    Name         string    `gorm:"not null"`
    Address      string    `gorm:"not null;index"` // Changed from uniqueIndex to regular index
    KeyStorePath string    `gorm:"not null"`
    Mnemonic     *string   `gorm:"type:text"`      // Nullable for private key imports
    ImportMethod string    `gorm:"not null"`       // Track import method
    SourceHash   string    `gorm:"uniqueIndex;not null"` // Unique constraint on source data
    CreatedAt    time.Time `gorm:"not null;autoCreateTime"`
}
```

### WalletDetails Enhancement

```go
type WalletDetails struct {
    Wallet       *Wallet
    Mnemonic     *string           // Nullable
    PrivateKey   *ecdsa.PrivateKey
    PublicKey    *ecdsa.PublicKey
    ImportMethod ImportMethod
    HasMnemonic  bool              // Helper field for UI
    KDFInfo      *KDFInfo          // KDF analysis information
}

// KDFInfo informações sobre o KDF usado
type KDFInfo struct {
    Type           string                 `json:"type"`
    NormalizedType string                 `json:"normalized_type"`
    SecurityLevel  string                 `json:"security_level"`
    Parameters     map[string]interface{} `json:"parameters"`
}
```

## Error Handling

### New Error Types

```go
type DuplicateWalletError struct {
    Type    string // "mnemonic", "private_key", "keystore"
    Address string
    Message string
}

func (e *DuplicateWalletError) Error() string {
    return fmt.Sprintf("duplicate wallet detected (%s): %s", e.Type, e.Message)
}

type InvalidImportDataError struct {
    Type    string
    Message string
}

func (e *InvalidImportDataError) Error() string {
    return fmt.Sprintf("invalid %s: %s", e.Type, e.Message)
}
```

### Localized Error Messages

```go
// In localization package
var ErrorMessages = map[string]map[string]string{
    "en": {
        "duplicate_mnemonic":    "A wallet with this mnemonic phrase already exists",
        "duplicate_private_key": "A wallet with this private key already exists",
        "no_mnemonic_available": "Mnemonic not available (imported via private key)",
        "invalid_mnemonic":      "Invalid mnemonic phrase",
        "invalid_private_key":   "Invalid private key format",
    },
    "pt": {
        "duplicate_mnemonic":    "Uma carteira com esta frase mnemônica já existe",
        "duplicate_private_key": "Uma carteira com esta chave privada já existe",
        "no_mnemonic_available": "Mnemônica não disponível (importada via chave privada)",
        "invalid_mnemonic":      "Frase mnemônica inválida",
        "invalid_private_key":   "Formato de chave privada inválido",
    },
}
```

## Testing Strategy

### Unit Tests

1. **Duplicate Detection Tests**
   - Test mnemonic-based duplicate detection
   - Test private key-based duplicate detection
   - Test coexistence of same address with different import methods

2. **Import Method Tests**
   - Test mnemonic import without false mnemonic generation
   - Test private key import with null mnemonic
   - Test keystore import with proper method tracking

3. **Source Hash Tests**
   - Test hash generation consistency
   - Test hash uniqueness for different inputs
   - Test hash collision handling

### Integration Tests

1. **End-to-End Import Scenarios**
   - Import multiple wallets with different mnemonic phrases
   - Import wallet via private key and verify no mnemonic
   - Import keystore and verify proper method tracking

2. **Database Migration Tests**
   - Test migration from old schema to new schema
   - Verify existing data preservation
   - Test rollback scenarios

### Test Data

```go
// Test scenarios for duplicate detection
var TestScenarios = []struct {
    Name        string
    Mnemonic1   string
    Mnemonic2   string
    SameAddress bool
    ShouldAllow bool
}{
    {
        Name:        "Different mnemonics, different addresses",
        Mnemonic1:   "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
        Mnemonic2:   "legal winner thank year wave sausage worth useful legal winner thank yellow",
        SameAddress: false,
        ShouldAllow: true,
    },
    {
        Name:        "Same mnemonic",
        Mnemonic1:   "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
        Mnemonic2:   "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
        SameAddress: true,
        ShouldAllow: false,
    },
}
```

## Migration Strategy

### Database Migration

```sql
-- Add new columns
ALTER TABLE wallets ADD COLUMN import_method VARCHAR(20) NOT NULL DEFAULT 'mnemonic';
ALTER TABLE wallets ADD COLUMN source_hash VARCHAR(64);

-- Make mnemonic nullable
ALTER TABLE wallets ALTER COLUMN mnemonic DROP NOT NULL;

-- Remove unique constraint on address
DROP INDEX IF EXISTS idx_wallets_address;
CREATE INDEX idx_wallets_address ON wallets(address);

-- Add unique constraint on source_hash
CREATE UNIQUE INDEX idx_wallets_source_hash ON wallets(source_hash);
```

### Data Migration Logic

```go
func MigrateExistingWallets(db *gorm.DB) error {
    var wallets []Wallet
    if err := db.Find(&wallets).Error; err != nil {
        return err
    }
    
    hashGen := &SourceHashGenerator{}
    
    for _, wallet := range wallets {
        // For existing wallets, assume they were imported via mnemonic
        wallet.ImportMethod = string(ImportMethodMnemonic)
        
        // Generate source hash from existing mnemonic
        if wallet.Mnemonic != nil && *wallet.Mnemonic != "" {
            // Decrypt mnemonic to generate proper hash
            decryptedMnemonic, err := DecryptMnemonic(*wallet.Mnemonic, "")
            if err == nil {
                wallet.SourceHash = hashGen.GenerateFromMnemonic(decryptedMnemonic)
            } else {
                // Fallback: use encrypted mnemonic for hash
                wallet.SourceHash = hashGen.GenerateFromMnemonic(*wallet.Mnemonic)
            }
        }
        
        if err := db.Save(&wallet).Error; err != nil {
            return fmt.Errorf("failed to migrate wallet %d: %w", wallet.ID, err)
        }
    }
    
    return nil
}
```

## Security Considerations

1. **Source Hash Security**
   - Use SHA-256 for source hash generation
   - Hash raw source data, not encrypted versions
   - Ensure hash consistency across sessions

2. **Mnemonic Handling**
   - Never generate fake mnemonics
   - Clearly distinguish between available and unavailable mnemonics
   - Maintain encryption for stored mnemonics

3. **Private Key Security**
   - Continue existing encryption practices
   - Don't expose private keys in logs or error messages
   - Validate private key format before processing

## Performance Considerations

1. **Database Indexing**
   - Index on `source_hash` for fast duplicate detection
   - Index on `address` for address-based queries
   - Index on `import_method` for filtering by import type

2. **Hash Generation**
   - Cache source hashes to avoid recalculation
   - Use efficient hash algorithms (SHA-256)
   - Generate hashes only when needed

3. **Memory Usage**
   - Use pointers for nullable fields to save memory
   - Avoid loading unnecessary data in list operations
   - Implement pagination for large wallet lists