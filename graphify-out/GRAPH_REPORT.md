# Graph Report - bloco-wallet  (2026-08-26)

## Corpus Check
- 163 files · ~119,896 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1352 nodes · 3148 edges · 76 communities (62 shown, 14 thin omitted)
- Extraction: 86% EXTRACTED · 14% INFERRED · 0% AMBIGUOUS · INFERRED: 456 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Bubble Tea Commands
- Network UI Integration
- Configuration Loading
- Universal KDF
- Import Localization
- Balance Providers
- File Picker UI
- Import State Tests
- Import Completion UI
- Wallet Import Tests
- Batch Import Tests
- Application Storage Bootstrap
- Wallet Details Localization
- Enhanced Import State
- Error Handling Tests
- Import Progress UI
- Batch Import Service
- Wallet Repository Mocks
- Error Categorization
- Error Aggregation
- Password Popup Tests
- CLI Model and Networks
- Comprehensive Fixture Generator
- ChainList RPC Client
- CLI Configuration Helpers
- Keystore Validation
- Error Aggregator Tests
- Wallet Domain Types
- Network Classification Tests
- Import Error Model
- Keystore Fixture Generator
- Wallet Core Service
- Import Job State
- Enhanced Keystore Crypto
- Simple Fixture Generator
- Test Keystore Generator
- ChainList Test Doubles
- Network Classification
- Enhanced Import Messaging
- Network Manager
- Additional Fixture Generator
- Password File Errors
- Configuration Tests
- Password Popup
- Password File Tests
- Network Manager Tests
- GORM Repository
- Network Configuration Wiring
- Import Summary Views
- Additional Test Data
- Storage Integration Tests
- Source Hash Validation
- Blockchain Error Tests
- Import Progress State
- Development Scrypt Parameters
- Localization Documentation
- Test Data Shell Generator
- Generator Runner
- Additional Keystore Documentation
- File Picker Documentation
- Import Progress Documentation
- Keystore Fixture Catalog
- Password Fixture Documentation
- Password Popup Documentation
- Test Keystore Overview

## God Nodes (most connected - your core abstractions)
1. `EnhancedImportState` - 64 edges
2. `NewWalletService()` - 44 edges
3. `InitCryptoService()` - 38 edges
4. `ImportCompletionModel` - 35 edges
5. `CLIModel` - 34 edges
6. `AddNetworkComponent` - 29 edges
7. `Wallet` - 29 edges
8. `Config` - 28 edges
9. `ImportProgressModel` - 26 edges
10. `KeystoreErrorType` - 26 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `SetLogger()`  [EXTRACTED]
  cmd/blocowallet/main.go → internal/ui/add_network.go
- `main()` --calls--> `NewCLIModel()`  [EXTRACTED]
  cmd/blocowallet/main.go → internal/ui/tui.go
- `main()` --calls--> `SetLogger()`  [EXTRACTED]
  cmd/blocowallet/main.go → internal/wallet/logger.go
- `main()` --calls--> `NewWalletService()`  [EXTRACTED]
  cmd/blocowallet/main.go → internal/wallet/wallet_service.go
- `main()` --calls--> `NewConfigurationManager()`  [EXTRACTED]
  cmd/blocowallet/main.go → pkg/config/configuration_manager.go

## Import Cycles
- None detected.

## Communities (76 total, 14 thin omitted)

### Community 0 - "Bubble Tea Commands"
Cohesion: 0.07
Nodes (22): github.com/charmbracelet/bubbletea.Cmd, github.com/charmbracelet/bubbletea.Model, github.com/charmbracelet/bubbletea.Msg, NewAddNetworkComponent(), TestAddNetwork_TypingIntoNameFieldUpdatesValue(), walletCountCmd(), NewConfigMenu(), NewImportMenu() (+14 more)

### Community 1 - "Network UI Integration"
Cohesion: 0.05
Nodes (31): github.com/charmbracelet/bubbles/list.Model, go.uber.org/zap.Field, go.uber.org/zap.Logger, go.uber.org/zap/zapcore.Level, net/http/httptest.Server, NetworkSuggestion, drive(), newRPCServer() (+23 more)

### Community 2 - "Configuration Loading"
Cohesion: 0.06
Nodes (30): ConfigurationManagerInterface, DatabaseConfig, SecurityConfig, github.com/spf13/viper.Viper, DecryptMnemonic(), EncryptMnemonic(), NewCryptoService(), setupTestConfig() (+22 more)

### Community 3 - "Universal KDF"
Cohesion: 0.06
Nodes (27): hash.Hash, time.Time, TestKDFParameterConversion(), TestKDFSecurityAnalysis(), TestUniversalKDFCompatibilityAnalysis(), getCurrentTime(), getElapsedTime(), hexCharToInt() (+19 more)

### Community 4 - "Import Localization"
Cohesion: 0.08
Nodes (42): TestBatchImportServiceWithErrorAggregation(), TestEnhancedImportIntegration(), TestErrorRecoveryWorkflow(), TestLocalizationIntegration(), TestPasswordFileManagerWithEnhancedErrors(), GetCurrentLanguage(), SetCurrentLanguage(), AddEnhancedImportMessages() (+34 more)

### Community 5 - "Balance Providers"
Cohesion: 0.06
Nodes (16): BalanceProvider, Ethereum, Mock, MultiProvider, NetworkBalance, Provider, RPCConnectionResult, context.Context (+8 more)

### Community 6 - "File Picker UI"
Cohesion: 0.07
Nodes (30): github.com/charmbracelet/bubbles/key.Binding, github.com/charmbracelet/lipgloss.Renderer, github.com/charmbracelet/lipgloss.Style, os.DirEntry, DefaultEnhancedFilePickerKeyMap(), DefaultEnhancedFilePickerStyles(), DefaultEnhancedFilePickerStylesWithRenderer(), NewEnhancedFilePicker() (+22 more)

### Community 7 - "Import State Tests"
Cohesion: 0.09
Nodes (42): testing.B, NewEnhancedImportState(), TestChannelOperations(), TestCleanupFunctions(), TestConcurrentAccess(), TestImportCompletion(), TestNewEnhancedImportState(), TestPasswordHandling() (+34 more)

### Community 8 - "Import Completion UI"
Cohesion: 0.08
Nodes (21): github.com/charmbracelet/bubbletea.KeyMsg, NewImportCompletionModel(), TestImportCompletionModel_ActionExecution(), TestImportCompletionModel_ErrorDetailsView(), TestImportCompletionModel_GetRetryableFiles(), TestImportCompletionModel_GettersAndSetters(), TestImportCompletionModel_InitializeActions(), TestImportCompletionModel_KeyboardNavigation() (+13 more)

### Community 9 - "Wallet Import Tests"
Cohesion: 0.10
Nodes (37): TestFinalIntegrationComplexPassword(), TestImportWalletFromKeystoreV3WithUniversalKDF(), TestKeystoreImportErrorMessages(), TestImportWallet_InvalidMnemonic(), TestImportWallet_MnemonicDuplicateDetection(), TestImportWallet_MnemonicSuccess(), CreateMockConfig(), TestImportWalletFromPrivateKey_Duplicate() (+29 more)

### Community 10 - "Batch Import Tests"
Cohesion: 0.09
Nodes (38): testing.T, NewBatchImportService(), TestCreateImportJobsFromDirectory(), TestCreateImportJobsFromFiles(), TestCreatePasswordRequest(), TestGetImportSummary(), TestImportBatchChannelCommunication(), TestImportError() (+30 more)

### Community 11 - "Application Storage Bootstrap"
Cohesion: 0.10
Nodes (31): main(), github.com/ethereum/go-ethereum/accounts/keystore.KeyStore, github.com/ethereum/go-ethereum/common.Address, gorm.io/gorm.Dialector, ensureDir(), NewWalletRepository(), createSQLiteDialector(), InitCryptoService() (+23 more)

### Community 12 - "Wallet Details Localization"
Cohesion: 0.10
Nodes (28): TestWalletDetailsViewConsistency(), TestWalletDetailsViewImportMethodDisplay(), TestWalletDetailsViewMnemonicHandling(), DefaultCryptoMessages(), DefaultCryptoMessagesPortuguese(), DefaultCryptoMessagesSpanish(), SetLanguage(), createDefaultLanguageFiles() (+20 more)

### Community 14 - "Error Handling Tests"
Cohesion: 0.11
Nodes (21): TestIsPasswordFileErrorRecoverable(), TestKeystoreErrorType_CategoryMethods(), TestKeystoreErrorType_GetDefaultRecoveryHint(), TestKeystoreErrorType_GetLocalizationKey(), TestKeystoreErrorType_String(), TestNewPasswordFileErrorConstructors(), TestPasswordFileError_Context(), TestPasswordFileError_EnhancedFields() (+13 more)

### Community 15 - "Import Progress UI"
Cohesion: 0.12
Nodes (5): github.com/charmbracelet/bubbles/progress.Model, ImportError, ImportProgressModel, ImportProgressMsg, TickMsg

### Community 16 - "Batch Import Service"
Cohesion: 0.16
Nodes (7): PasswordRequest, PasswordResponse, BatchImportService, DirectoryScanError, KeystoreDiscoveryReport, PasswordInputError, ScanErrorType

### Community 17 - "Wallet Repository Mocks"
Cohesion: 0.12
Nodes (4): MockWalletRepository, Wallet, MockWalletRepository, mockRepo

### Community 18 - "Error Categorization"
Cohesion: 0.13
Nodes (13): categorizeKeystoreError(), getRetryDescription(), getRetryPriority(), getRetryStrategy(), isPasswordInputErrorRecoverable(), mapPasswordFileErrorType(), mapPasswordInputErrorType(), TestCategorizeKeystoreError() (+5 more)

### Community 19 - "Error Aggregation"
Cohesion: 0.15
Nodes (6): AggregatedError, ErrorAggregator, ErrorCategory, ErrorReport, ErrorSummary, RetryRecommendation

### Community 20 - "Password Popup Tests"
Cohesion: 0.17
Nodes (21): NewPasswordPopupModel(), TestNewPasswordPopupModel(), TestPasswordPopupModel_CharacterLimit(), TestPasswordPopupModel_GetResult_Cancelled(), TestPasswordPopupModel_GetResult_Confirmed(), TestPasswordPopupModel_GetResult_NotCompleted(), TestPasswordPopupModel_HasExceededMaxRetries(), TestPasswordPopupModel_Integration_CancelFlow() (+13 more)

### Community 21 - "CLI Model and Networks"
Cohesion: 0.12
Nodes (8): github.com/charmbracelet/bubbles/table.Model, github.com/digitallyserviced/tdfgo/tdf.TheDrawFont, CLIModel, NewNetworkListComponent(), BackToNetworkListMsg, BackToNetworkMenuMsg, networkAddedMsg, NetworkListComponent

### Community 22 - "Comprehensive Fixture Generator"
Cohesion: 0.27
Nodes (19): copyMap(), generateCorruptedCiphertext(), generateDocumentation(), generateInvalidJSON(), generateInvalidKDFParams(), generateInvalidKeystores(), generateInvalidMAC(), generateInvalidVersion() (+11 more)

### Community 23 - "ChainList RPC Client"
Cohesion: 0.16
Nodes (8): NetworkOperationError, RPCEndpoint, net/http.Client, ChainListService, isTransientNetworkError(), NewChainListService(), TestValidateRPCEndpoint_EmptyURL(), NewNetworkOperationError()

### Community 24 - "CLI Configuration Helpers"
Cohesion: 0.16
Nodes (15): CLIModel, github.com/digitallyserviced/tdfgo/tdf.FontInfo, loadOrCreateConfig(), sanitizeNetworkKey(), TestSanitizeNetworkKey(), TestSaveConfigToFile(), TestNetworkConfigurationIntegration(), buildFontsList() (+7 more)

### Community 25 - "Keystore Validation"
Cohesion: 0.22
Nodes (11): TestIsRecoverableErrorType(), TestNewKeystoreImportErrorConstructors(), isRecoverableErrorType(), NewKeystoreImportErrorWithField(), NewKeystoreImportErrorWithFile(), KeystoreV3, KeystoreV3CipherParams, KeystoreV3Crypto (+3 more)

### Community 26 - "Error Aggregator Tests"
Cohesion: 0.19
Nodes (17): TestKeystoreImportError_Context(), TestKeystoreImportError_GetUserFriendlyMessage(), TestKeystoreImportError_IsRecoverable(), NewErrorAggregator(), TestErrorAggregator_AddError(), TestErrorAggregator_AddGenericError(), TestErrorAggregator_AddPasswordInputError(), TestErrorAggregator_AddSuccess() (+9 more)

### Community 27 - "Wallet Domain Types"
Cohesion: 0.15
Nodes (10): crypto/ecdsa.PrivateKey, crypto/ecdsa.PublicKey, NewDuplicateWalletError(), NewInvalidImportDataError(), DuplicateWalletError, EnhancedWallet, EnhancedWalletDetails, InvalidImportDataError (+2 more)

### Community 28 - "Network Classification Tests"
Cohesion: 0.23
Nodes (15): NewNetworkClassificationService(), TestNetworkClassificationService_ClassifyExistingNetwork_LegacyFormat(), TestNetworkClassificationService_ClassifyExistingNetwork_WithTypePrefix(), TestNetworkClassificationService_ClassifyNetwork_Custom(), TestNetworkClassificationService_ClassifyNetwork_Standard(), TestNetworkClassificationService_GenerateNetworkKey(), TestNetworkClassificationService_GetNetworkTypeFromKey(), TestNetworkClassificationService_IsNetworkCustom() (+7 more)

### Community 29 - "Import Error Model"
Cohesion: 0.14
Nodes (4): TestKeystoreImportError_EnhancedFields(), TestKeystoreImportError_GetRecoveryHint(), NewKeystoreImportErrorWithRecovery(), KeystoreImportError

### Community 30 - "Keystore Fixture Generator"
Cohesion: 0.35
Nodes (14): generateAdditionalInvalidKeystores(), generateAdditionalValidKeystores(), generateExtremeScryptParams(), generateFloatVersion(), generateInvalidAddressChecksum(), generateMissingKDF(), generateNonStandardCipher(), generatePBKDF2Keystore() (+6 more)

### Community 31 - "Wallet Core Service"
Cohesion: 0.30
Nodes (6): DerivePrivateKey(), GenerateMnemonic(), WalletDetails, WalletService, HexToECDSA(), ImportMethod

### Community 32 - "Import Job State"
Cohesion: 0.29
Nodes (4): ImportJob, ImportResult, MockBatchImportService, TestBatchImportService

### Community 33 - "Enhanced Keystore Crypto"
Cohesion: 0.27
Nodes (5): NewEnhancedKeyStoreService(), removePKCS7Padding(), TestEnhancedKeyStoreService_Integration(), CryptoParams, EnhancedKeyStoreService

### Community 34 - "Simple Fixture Generator"
Cohesion: 0.32
Nodes (13): copyMap(), generateInvalidKeystores(), generateInvalidVersion(), generateMissingFields(), generateNonStandardCipher(), generatePBKDF2Keystore(), generateRandomHex(), generateRealKeystore() (+5 more)

### Community 35 - "Test Keystore Generator"
Cohesion: 0.36
Nodes (12): generateCorruptedCiphertext(), generateDocumentation(), generateInvalidKDFParams(), generateInvalidKeystores(), generateInvalidMAC(), generateMalformedAddress(), generateNonStandardCipher(), generatePBKDF2Keystore() (+4 more)

### Community 36 - "ChainList Test Doubles"
Cohesion: 0.19
Nodes (4): MockChainListService, github.com/stretchr/testify/mock.Mock, ChainInfo, MockChainListService

### Community 37 - "Network Classification"
Cohesion: 0.36
Nodes (4): NetworkClassification, ChainListServiceInterface, NetworkClassificationService, NetworkType

### Community 38 - "Enhanced Import Messaging"
Cohesion: 0.15
Nodes (9): fileExists(), BatchImportServiceInterface, ContinueListeningMsg, ImportBatchCompleteMsg, PasswordRequestMsg, RetryImportRequestMsg, ReturnToFileSelectionMsg, StateInfo (+1 more)

### Community 39 - "Network Manager"
Cohesion: 0.27
Nodes (4): Network, ConfigurationManagerInterface, NetworkInfo, NetworkManager

### Community 40 - "Additional Fixture Generator"
Cohesion: 0.40
Nodes (12): generateAdditionalInvalidKeystores(), generateAdditionalValidKeystores(), generateInvalidAddressChecksum(), generateMissingKDF(), generateNonStandardCipher(), generatePBKDF2Keystore(), generateRandomHex(), generateRealKeystore() (+4 more)

### Community 41 - "Password File Errors"
Cohesion: 0.20
Nodes (3): isPasswordErrorRecoverable(), PasswordFileError, PasswordFileErrorType

### Community 42 - "Configuration Tests"
Cohesion: 0.30
Nodes (11): NewConfigurationManager(), TestConfigurationManager_EnvironmentVariables(), TestConfigurationManager_GetAppDirectory(), TestConfigurationManager_GetConfigPath(), TestConfigurationManager_LegacyEnvironmentVariables(), TestConfigurationManager_LoadConfiguration(), TestConfigurationManager_ReloadConfiguration(), TestConfigurationManager_ReloadConfiguration_WithoutLoad() (+3 more)

### Community 43 - "Password Popup"
Cohesion: 0.20
Nodes (3): github.com/charmbracelet/bubbles/textinput.Model, PasswordPopupModel, PasswordPopupResult

### Community 44 - "Password File Tests"
Cohesion: 0.29
Nodes (9): TestPasswordFileErrorScenarios(), TestPasswordFileIntegration(), NewPasswordFileManager(), TestPasswordFileManager_FindPasswordFile(), TestPasswordFileManager_GetPasswordForKeystore(), TestPasswordFileManager_ReadPasswordFile(), TestPasswordFileManager_RequiresManualPassword(), TestPasswordFileManager_ValidatePasswordFile() (+1 more)

### Community 45 - "Network Manager Tests"
Cohesion: 0.36
Nodes (9): NewNetworkManager(), TestNetworkManager_AddNetwork_Custom(), TestNetworkManager_GetNetwork(), TestNetworkManager_Integration(), TestNetworkManager_LoadNetworks(), TestNetworkManager_RemoveNetwork(), TestNetworkManager_ValidateNetwork_Basic(), TestNetworkManager_ValidateNetwork_EmptyName() (+1 more)

### Community 47 - "Network Configuration Wiring"
Cohesion: 0.39
Nodes (7): addNetworkWithClassificationInfo(), getConfigurationManager(), getNetworkManager(), loadNetworksWithManager(), removeNetworkWithManager(), updateLanguageInConfig(), NetworkClassificationInfo

### Community 49 - "Additional Test Data"
Cohesion: 0.47
Nodes (7): generateAdditionalTestCases(), generateEmptyAddress(), generateLongAddress(), generateMalformedJSON(), generateNullAddress(), main(), updateInvalidKeystoreReadme()

### Community 50 - "Storage Integration Tests"
Cohesion: 0.43
Nodes (7): setupTestConfig(), TestGORMRepository_AddWallet(), TestGORMRepository_DeleteWallet(), TestGORMRepository_FindBySourceHash_And_AddressQueries(), TestGORMRepository_GetAllWallets(), TestGORMRepository_SQLiteConfigurations(), TestNewWalletRepository()

### Community 51 - "Source Hash Validation"
Cohesion: 0.32
Nodes (5): TestValidateUniqueSourceHash_Collision(), TestValidateUniqueSourceHash_Empty(), TestValidateUniqueSourceHash_NoCollision(), ValidateUniqueSourceHash(), WalletRepository

### Community 52 - "Blockchain Error Tests"
Cohesion: 0.48
Nodes (5): simpleErr, assertErr(), containsAll(), stringsContains(), TestNewNetworkOperationError_ErrorString()

### Community 53 - "Import Progress State"
Cohesion: 0.40
Nodes (3): ImportError, ImportProgress, ImportProgressUpdateMsg

## Knowledge Gaps
- **31 isolated node(s):** `AddNetworkRequestMsg`, `enhancedErrorMsg`, `ContinueListeningMsg`, `RetryImportRequestMsg`, `ReturnToFileSelectionMsg` (+26 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **14 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `EnhancedImportState` connect `Enhanced Import State` to `Bubble Tea Commands`, `Import Job State`, `Universal KDF`, `Balance Providers`, `Enhanced Import Messaging`, `Import State Tests`, `File Picker UI`, `Import Completion UI`, `Password Popup`, `Import Progress UI`, `Batch Import Service`, `Import Summary Views`, `CLI Model and Networks`, `Import Progress State`?**
  _High betweenness centrality (0.100) - this node is a cross-community bridge._
- **Why does `Config` connect `Configuration Loading` to `Bubble Tea Commands`, `Balance Providers`, `Network Manager`, `Wallet Import Tests`, `Application Storage Bootstrap`, `Wallet Details Localization`, `Storage Integration Tests`, `CLI Model and Networks`, `CLI Configuration Helpers`?**
  _High betweenness centrality (0.070) - this node is a cross-community bridge._
- **Why does `CLIModel` connect `CLI Model and Networks` to `Bubble Tea Commands`, `Network UI Integration`, `Configuration Loading`, `Import State Tests`, `Password Popup`, `Enhanced Import State`, `Wallet Repository Mocks`, `CLI Configuration Helpers`, `Wallet Core Service`?**
  _High betweenness centrality (0.055) - this node is a cross-community bridge._
- **Are the 27 inferred relationships involving `NewWalletService()` (e.g. with `TestFinalIntegrationComplexPassword()` and `TestImportWalletFromKeystoreV3WithUniversalKDF()`) actually correct?**
  _`NewWalletService()` has 27 INFERRED edges - model-reasoned connections that need verification._
- **Are the 20 inferred relationships involving `InitCryptoService()` (e.g. with `TestFinalIntegrationComplexPassword()` and `TestImportWalletFromKeystoreV3WithUniversalKDF()`) actually correct?**
  _`InitCryptoService()` has 20 INFERRED edges - model-reasoned connections that need verification._
- **What connects `AddNetworkRequestMsg`, `enhancedErrorMsg`, `ContinueListeningMsg` to the rest of the system?**
  _31 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Bubble Tea Commands` be split into smaller, more focused modules?**
  _Cohesion score 0.07126207126207126 - nodes in this community are weakly interconnected._