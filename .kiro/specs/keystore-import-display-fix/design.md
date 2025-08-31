# Design Document

## Overview

Este documento descreve o design para corrigir problemas específicos na importação e exibição de carteiras a partir de arquivos keystore V3 no BlocoWallet. O design foca em três problemas principais:

1. **Correção da exibição do tipo de importação**: Carteiras importadas via keystore V3 devem mostrar "Chave Privada" ao invés de "Frase Mnemônica"
2. **Correção da barra de progresso**: A barra de progresso da TUI deve atualizar corretamente durante o processo de importação
3. **Remoção da geração incorreta de mnemônica**: Keystores V3 não devem gerar mnemônicas sintéticas, pois isso é conceitualmente incorreto

## Architecture

### Current Architecture Issues

**Problema 1: Exibição Incorreta do Tipo de Importação**
- O código atual em `ImportWalletFromKeystoreV3` gera uma mnemônica sintética usando `generateSyntheticMnemonicFromPrivateKey`
- A UI em `tui.go` determina o tipo de carteira baseado apenas na presença de mnemônica (`w.Mnemonic == nil`)
- Carteiras importadas via keystore mostram "Frase Mnemônica" porque têm mnemônica sintética armazenada
- O campo `ImportMethod` existe mas não é usado consistentemente na UI

**Problema 2: Barra de Progresso Não Atualiza**
- A barra de progresso em `ImportProgressModel` depende de mensagens `ImportProgressMsg`
- O serviço de importação pode não estar enviando atualizações de progresso corretamente
- Canais de comunicação podem estar bloqueados ou não sendo processados

**Problema 3: Geração Incorreta de Mnemônica**
- `generateSyntheticMnemonicFromPrivateKey` cria mnemônicas "falsas" a partir de chaves privadas
- Isso confunde usuários sobre as capacidades reais de suas carteiras
- Keystores V3 contêm apenas chaves privadas, não mnemônicas originais

### Proposed Architecture

**Nova Estratégia de Exibição de Tipo:**
- Usar o campo `ImportMethod` como fonte primária de verdade para determinar o tipo de carteira
- Remover geração de mnemônica sintética para keystores V3
- Atualizar a UI para mostrar tipos baseados em `ImportMethod` ao invés de presença de mnemônica

**Correção da Barra de Progresso:**
- Garantir que o serviço de importação envie atualizações de progresso em todas as etapas
- Implementar timeouts e fallbacks para evitar travamentos
- Adicionar logging detalhado para debugging de problemas de progresso

**Remoção de Mnemônica Sintética:**
- Keystores V3 devem ter `Mnemonic: nil` e `ImportMethod: "keystore"`
- UI deve mostrar "Chave Privada" para `ImportMethodKeystore`
- Exportação deve oferecer apenas chave privada, não mnemônica

## Components and Interfaces

### 1. Updated WalletService.ImportWalletFromKeystoreV3

```go
func (ws *WalletService) ImportWalletFromKeystoreV3(name, keystorePath, password string) (*WalletDetails, error) {
    // ... existing validation and decryption logic ...

    // REMOVE: Step 15 - synthetic mnemonic generation
    // OLD CODE:
    // mnemonic, err := generateSyntheticMnemonicFromPrivateKey(privateKey)
    
    // NEW: No mnemonic for keystore imports
    var nilMnemonic *string = nil

    // ... existing file operations ...

    // Create wallet entry WITHOUT mnemonic
    wallet := &Wallet{
        Name:         name,
        Address:      address,
        KeyStorePath: destPath,
        Mnemonic:     nilMnemonic,  // No mnemonic for keystore imports
        ImportMethod: string(ImportMethodKeystore),
        SourceHash:   sourceHash,
    }

    // ... existing repository operations ...

    // Return wallet details WITHOUT mnemonic
    walletDetails := &WalletDetails{
        Wallet:       wallet,
        Mnemonic:     nil,  // No mnemonic available
        PrivateKey:   privateKey,
        PublicKey:    &privateKey.PublicKey,
        ImportMethod: ImportMethodKeystore,
        HasMnemonic:  false,  // Keystore imports don't have mnemonics
        KDFInfo:      kdfInfo,
    }

    return walletDetails, nil
}
```

### 2. Enhanced UI Type Display Logic

```go
// In tui.go - Update wallet type determination
func determineWalletType(w wallet.Wallet) string {
    // Use ImportMethod as primary source of truth
    switch wallet.ImportMethod(w.ImportMethod) {
    case wallet.ImportMethodMnemonic:
        return localization.Labels["imported_mnemonic"]
    case wallet.ImportMethodPrivateKey:
        return localization.Labels["imported_private_key"]
    case wallet.ImportMethodKeystore:
        return localization.Labels["imported_keystore"] // New label
    default:
        // Fallback to old logic for backward compatibility
        if w.Mnemonic == nil {
            return localization.Labels["imported_private_key"]
        }
        return localization.Labels["imported_mnemonic"]
    }
}
```

### 3. Enhanced Progress Tracking

```go
// Enhanced progress reporting in ImportWalletFromKeystoreV3
func (ws *WalletService) ImportWalletFromKeystoreV3WithProgress(
    name, keystorePath, password string,
    progressChan chan<- ImportProgress,
) (*WalletDetails, error) {
    
    // Send initial progress
    if progressChan != nil {
        select {
        case progressChan <- ImportProgress{
            CurrentFile: keystorePath,
            Stage: "Validating file",
            Percentage: 0.1,
        }:
        default:
        }
    }

    // ... file validation ...

    // Send validation complete progress
    if progressChan != nil {
        select {
        case progressChan <- ImportProgress{
            CurrentFile: keystorePath,
            Stage: "Parsing keystore",
            Percentage: 0.3,
        }:
        default:
        }
    }

    // ... keystore parsing ...

    // Send decryption progress
    if progressChan != nil {
        select {
        case progressChan <- ImportProgress{
            CurrentFile: keystorePath,
            Stage: "Decrypting private key",
            Percentage: 0.6,
        }:
        default:
        }
    }

    // ... decryption logic ...

    // Send saving progress
    if progressChan != nil {
        select {
        case progressChan <- ImportProgress{
            CurrentFile: keystorePath,
            Stage: "Saving wallet",
            Percentage: 0.9,
        }:
        default:
        }
    }

    // ... save operations ...

    // Send completion
    if progressChan != nil {
        select {
        case progressChan <- ImportProgress{
            CurrentFile: keystorePath,
            Stage: "Complete",
            Percentage: 1.0,
        }:
        default:
        }
    }

    return walletDetails, nil
}
```

### 4. Updated Localization Labels

```go
// Add new localization labels
var NewLabels = map[string]map[string]string{
    "en": {
        "imported_keystore":     "Keystore (Private Key)",
        "method_keystore":       "Keystore File",
        "no_mnemonic_keystore":  "Mnemonic not available - imported from keystore file",
        "export_private_key":    "Export Private Key",
        "keystore_import_stage_validating": "Validating keystore file...",
        "keystore_import_stage_parsing":    "Parsing keystore structure...",
        "keystore_import_stage_decrypting": "Decrypting private key...",
        "keystore_import_stage_saving":     "Saving wallet...",
    },
    "pt": {
        "imported_keystore":     "Keystore (Chave Privada)",
        "method_keystore":       "Arquivo Keystore",
        "no_mnemonic_keystore":  "Mnemônica não disponível - importada de arquivo keystore",
        "export_private_key":    "Exportar Chave Privada",
        "keystore_import_stage_validating": "Validando arquivo keystore...",
        "keystore_import_stage_parsing":    "Analisando estrutura do keystore...",
        "keystore_import_stage_decrypting": "Descriptografando chave privada...",
        "keystore_import_stage_saving":     "Salvando carteira...",
    },
}
```

## Data Models

### Updated WalletDetails Structure

```go
type WalletDetails struct {
    Wallet       *Wallet
    Mnemonic     *string           // Nullable - nil for keystore imports
    PrivateKey   *ecdsa.PrivateKey
    PublicKey    *ecdsa.PublicKey
    ImportMethod ImportMethod      // Primary source of truth for wallet type
    HasMnemonic  bool              // Helper field - false for keystore imports
    KDFInfo      *KDFInfo          // KDF analysis information
    
    // New fields for better UI support
    DisplayType  string            // Computed display type for UI
    CanExportMnemonic bool         // Whether mnemonic export is available
}
```

### Enhanced ImportProgress Structure

```go
type ImportProgress struct {
    CurrentFile     string
    Stage           string        // New: detailed stage information
    Percentage      float64       // More granular progress
    TotalFiles      int
    ProcessedFiles  int
    Errors          []ImportError
    PendingPassword bool
    PendingFile     string
    StartTime       time.Time
    ElapsedTime     time.Duration
    EstimatedRemaining time.Duration // New: estimated time remaining
}
```

## Error Handling

### Enhanced Error Messages

```go
type KeystoreDisplayError struct {
    Type    string
    Message string
    Context map[string]interface{}
}

// Specific error types for display issues
const (
    ErrorInvalidDisplayType = "invalid_display_type"
    ErrorMissingImportMethod = "missing_import_method"
    ErrorProgressTimeout = "progress_timeout"
)
```

### Localized Error Messages

```go
var ErrorMessages = map[string]map[string]string{
    "en": {
        "invalid_display_type":   "Unable to determine wallet display type",
        "missing_import_method":  "Wallet import method not specified",
        "progress_timeout":       "Import progress update timed out",
        "no_mnemonic_keystore":   "This wallet was imported from a keystore file and does not have a mnemonic phrase",
    },
    "pt": {
        "invalid_display_type":   "Não foi possível determinar o tipo de exibição da carteira",
        "missing_import_method":  "Método de importação da carteira não especificado",
        "progress_timeout":       "Timeout na atualização do progresso da importação",
        "no_mnemonic_keystore":   "Esta carteira foi importada de um arquivo keystore e não possui frase mnemônica",
    },
}
```

## Testing Strategy

### Unit Tests

1. **Wallet Type Display Tests**
   - Test `determineWalletType` function with different `ImportMethod` values
   - Test backward compatibility with wallets that don't have `ImportMethod` set
   - Test UI display consistency across different wallet types

2. **Keystore Import Tests**
   - Test that keystore imports don't generate synthetic mnemonics
   - Test that `HasMnemonic` is false for keystore imports
   - Test that `ImportMethod` is correctly set to "keystore"

3. **Progress Tracking Tests**
   - Test progress updates are sent at each stage of keystore import
   - Test progress channel doesn't block import process
   - Test timeout handling for progress updates

### Integration Tests

1. **End-to-End Import Flow**
   - Import keystore file and verify correct type display
   - Verify progress bar updates throughout import process
   - Test export functionality shows only private key option

2. **UI Consistency Tests**
   - Test wallet list shows correct types for all import methods
   - Test wallet details view shows appropriate information
   - Test export options are contextually appropriate

### Test Data

```go
// Test scenarios for wallet type display
var WalletTypeTestCases = []struct {
    Name         string
    ImportMethod string
    HasMnemonic  bool
    ExpectedType string
}{
    {
        Name:         "Keystore import",
        ImportMethod: "keystore",
        HasMnemonic:  false,
        ExpectedType: "imported_keystore",
    },
    {
        Name:         "Mnemonic import",
        ImportMethod: "mnemonic",
        HasMnemonic:  true,
        ExpectedType: "imported_mnemonic",
    },
    {
        Name:         "Private key import",
        ImportMethod: "private_key",
        HasMnemonic:  false,
        ExpectedType: "imported_private_key",
    },
    {
        Name:         "Legacy wallet (no import method)",
        ImportMethod: "",
        HasMnemonic:  true,
        ExpectedType: "imported_mnemonic", // Fallback behavior
    },
}
```

## Migration Strategy

### Database Migration

```sql
-- Update existing keystore imports to have correct ImportMethod
UPDATE wallets 
SET import_method = 'keystore', 
    mnemonic = NULL 
WHERE import_method = '' 
  AND mnemonic IS NOT NULL 
  AND keystore_path LIKE '%.json';

-- Add index for ImportMethod queries
CREATE INDEX idx_wallets_import_method ON wallets(import_method);
```

### Code Migration

```go
func MigrateKeystoreWallets(db *gorm.DB) error {
    // Find wallets that were imported from keystores but have synthetic mnemonics
    var keystoreWallets []Wallet
    err := db.Where("keystore_path LIKE ? AND mnemonic IS NOT NULL", "%.json").Find(&keystoreWallets).Error
    if err != nil {
        return err
    }

    for _, wallet := range keystoreWallets {
        // Check if this is a synthetic mnemonic by trying to derive the same private key
        if isSyntheticMnemonic(wallet.Mnemonic, wallet.Address) {
            // Remove synthetic mnemonic and set correct import method
            wallet.Mnemonic = nil
            wallet.ImportMethod = string(ImportMethodKeystore)
            
            if err := db.Save(&wallet).Error; err != nil {
                return fmt.Errorf("failed to migrate wallet %d: %w", wallet.ID, err)
            }
        }
    }

    return nil
}

func isSyntheticMnemonic(encryptedMnemonic *string, address string) bool {
    // Implementation to detect if a mnemonic is synthetic
    // This would involve decrypting the mnemonic and checking if it was generated
    // from the private key rather than being an original BIP39 phrase
    return true // Simplified for this design
}
```

## Security Considerations

1. **Mnemonic Handling**
   - Ensure no synthetic mnemonics are stored for keystore imports
   - Clear any existing synthetic mnemonics during migration
   - Validate that export functions don't expose non-existent mnemonics

2. **Progress Information**
   - Don't expose sensitive information in progress messages
   - Ensure progress updates don't leak file paths or passwords
   - Implement proper timeout handling to prevent resource leaks

3. **UI Information Disclosure**
   - Clearly distinguish between available and unavailable mnemonics
   - Provide appropriate error messages when users try to access unavailable features
   - Ensure consistent terminology across all UI components

## Performance Considerations

1. **Progress Updates**
   - Use non-blocking channel sends to avoid slowing down import process
   - Implement reasonable update intervals to balance responsiveness and performance
   - Cache computed display types to avoid repeated calculations

2. **UI Rendering**
   - Optimize wallet type determination to avoid database queries in UI loops
   - Cache localization labels to improve rendering performance
   - Use efficient string formatting for progress messages

3. **Memory Usage**
   - Remove synthetic mnemonic storage to reduce memory footprint
   - Implement proper cleanup of progress channels and goroutines
   - Use appropriate buffer sizes for progress communication channels

## Backward Compatibility

1. **Existing Wallets**
   - Provide migration path for wallets with synthetic mnemonics
   - Maintain fallback logic for wallets without `ImportMethod` set
   - Preserve existing functionality while fixing display issues

2. **API Compatibility**
   - Keep existing `ImportWalletFromKeystoreV3` signature
   - Add new progress-aware variants as optional enhancements
   - Maintain existing error types and messages where possible

3. **Configuration**
   - Add new localization labels without breaking existing ones
   - Provide default values for new configuration options
   - Ensure graceful degradation when new features are not available