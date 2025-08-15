# Implementation Plan

- [x] 1. Create enhanced data structures and types
  - Create ImportMethod enum and constants
  - Create SourceHashGenerator utility struct
  - Create enhanced error types for duplicate detection
  - _Requirements: 3.1, 3.2, 5.4_

- [x] 2. Update Wallet model structure
  - [x] 2.1 Modify Wallet struct to support new fields
    - Make Mnemonic field nullable (*string)
    - Add ImportMethod field with validation
    - Add SourceHash field with unique constraint
    - Remove unique constraint from Address field
    - _Requirements: 3.1, 3.2, 3.3_

  - [x] 2.2 Update WalletDetails struct
    - Add ImportMethod field to WalletDetails
    - Add HasMnemonic boolean helper field
    - Make Mnemonic field nullable in WalletDetails
    - _Requirements: 3.1, 5.1, 5.2_

- [x] 3. Implement source hash generation logic
  - [x] 3.1 Create SourceHashGenerator with hash methods
    - Implement GenerateFromMnemonic method using kecak256, SHA-256 or sha3-256, what fits better
    - Implement GenerateFromPrivateKey method using kecak256, SHA-256 or sha3-256, what fits better
    - Implement GenerateFromKeystore method using kecak256, SHA-256 or sha3-256, what fits better
    - Write unit tests for hash generation consistency
    - _Requirements: 4.1, 4.2, 6.3_

  - [x] 3.2 Add hash validation and collision handling
    - Implement hash uniqueness validation
    - Add error handling for hash collisions
    - Write tests for edge cases in hash generation
    - _Requirements: 4.4, 6.3_

- [x] 4. Enhance repository layer for new duplicate detection
  - [x] 4.1 Update WalletRepository interface
    - Add FindBySourceHash method
    - Modify FindByAddress to return multiple wallets
    - Add methods for duplicate checking by import method
    - _Requirements: 4.1, 4.2, 4.3_

  - [x] 4.2 Implement enhanced GORM repository methods
    - Implement FindBySourceHash in GORMRepository
    - Update FindByAddress to handle multiple results
    - Add database migration logic for new schema
    - Write integration tests for repository methods
    - _Requirements: 3.4, 4.1, 4.2_

- [x] 5. Fix ImportWallet method for mnemonic-based imports
  - [x] 5.1 Update mnemonic import duplicate detection
    - Generate source hash from mnemonic before checking duplicates
    - Compare source hashes instead of addresses for mnemonic imports
    - Update error messages to specify mnemonic-based conflicts
    - _Requirements: 1.1, 1.2, 1.3, 1.5_

  - [x] 5.2 Implement proper mnemonic validation and storage
    - Validate mnemonic before generating source hash
    - Store ImportMethod as "mnemonic" for mnemonic imports
    - Ensure encrypted mnemonic is properly stored
    - Write unit tests for mnemonic import scenarios
    - _Requirements: 1.1, 1.4, 6.1_

- [ ] 6. Fix ImportWalletFromPrivateKey method
  - [ ] 6.1 Remove incorrect mnemonic generation
    - Remove calls to GenerateDeterministicMnemonic
    - Set Mnemonic field to nil for private key imports
    - Update WalletDetails to not include fake mnemonic
    - _Requirements: 2.1, 2.2, 2.3_

  - [ ] 6.2 Implement private key duplicate detection
    - Generate source hash from private key for duplicate checking
    - Check duplicates based on source hash, not just address
    - Store ImportMethod as "private_key" for private key imports
    - Update error messages for private key conflicts
    - _Requirements: 4.2, 4.4, 2.4_

  - [ ] 6.3 Update private key import validation
    - Validate private key format before processing
    - Ensure proper error handling for invalid private keys
    - Write unit tests for private key import scenarios
    - _Requirements: 2.5, 6.2_

- [x] 7. Implement Universal KDF Service
  - [x] 7.1 Create UniversalKDFService with pluggable handlers
    - Implement base UniversalKDFService structure
    - Create KDFHandler interface for different KDF types
    - Implement ScryptHandler with universal parameter support
    - Implement PBKDF2Handler with multiple hash function support
    - Add KDF name normalization (case-insensitive, variations)
    - _Requirements: 6.1, 6.2, 6.3_

  - [x] 7.2 Implement universal parameter conversion
    - Create convertToInt method supporting multiple JSON types
    - Create convertToBytes method for salt handling
    - Implement getIntParam with multiple parameter names
    - Add getSaltParam with multiple salt formats
    - Write unit tests for parameter conversion edge cases
    - _Requirements: 6.1, 6.2, 8.6, 8.7_

  - [x] 7.3 Add KDF security validation and analysis
    - Implement parameter range validation for security
    - Create SecurityAnalysis with complexity calculations
    - Add memory usage validation for scrypt parameters
    - Implement security level classification (Low, Medium, High, Very High)
    - Write tests for security validation scenarios
    - _Requirements: 6.5, 7.2, 7.3, 8.9_

- [x] 8. Create KDF Compatibility Analyzer
  - [x] 8.1 Implement compatibility analysis service
    - Create KDFCompatibilityAnalyzer with comprehensive reporting
    - Implement AnalyzeKeyStoreCompatibility method
    - Generate detailed CompatibilityReport with issues/warnings/suggestions
    - Add support for analyzing KeyStores from different wallet providers
    - _Requirements: 7.1, 7.4, 8.10_

  - [x] 8.2 Add enhanced error reporting and logging
    - Implement KDFLogger interface with detailed operation logging
    - Create context-aware error messages for KDF issues
    - Add debugging information for failed KDF operations
    - Implement performance monitoring for KDF operations
    - _Requirements: 6.7, 7.5_

- [x] 9. Update ImportWalletFromKeystoreV3 method with Universal KDF
  - [x] 9.1 Integrate Universal KDF Service into keystore import
    - Replace existing KDF validation with UniversalKDFService
    - Add compatibility analysis before processing KeyStore
    - Generate source hash from keystore JSON content
    - Store ImportMethod as "keystore" for keystore imports
    - _Requirements: 4.3, 3.1, 6.1_

  - [x] 9.2 Enhance keystore import with KDF information
    - Add KDFInfo to WalletDetails for keystore imports
    - Preserve existing mnemonic generation for keystore imports
    - Update error messages to include KDF-specific context
    - Add security warnings for low-security KDF parameters
    - Write comprehensive tests for Universal KDF keystore import
    - _Requirements: 3.5, 5.4, 6.1, 7.2, 7.3_

- [ ] 10. Update localization and error messages
  - [ ] 10.1 Add new localized error messages
    - Add messages for mnemonic-based duplicate detection
    - Add messages for private key-based duplicate detection
    - Add messages for unavailable mnemonic scenarios
    - Update existing error messages for clarity
    - _Requirements: 5.1, 5.2, 5.4_

  - [ ] 10.2 Implement context-aware error reporting
    - Include import method in error messages
    - Provide specific guidance based on conflict type
    - Add helper methods for generating localized messages
    - _Requirements: 5.4, 5.5_

- [ ] 11. Create database migration functionality
  - [ ] 11.1 Implement schema migration
    - Create migration to add new columns (import_method, source_hash)
    - Create migration to make mnemonic column nullable
    - Create migration to update address index constraints
    - Add rollback functionality for migrations
    - _Requirements: 3.3, 3.4_

  - [ ] 12.2 Implement data migration for existing wallets
    - Migrate existing wallets to use new schema
    - Generate source hashes for existing mnemonic-based wallets
    - Set appropriate ImportMethod for existing wallets
    - Preserve all existing wallet data during migration
    - Write tests for migration scenarios
    - _Requirements: 3.5, 6.4_

- [ ] 13. Update UI components and user feedback
  - [ ] 13.1 Update wallet display logic
    - Show "Mnemonic not available" for private key imports
    - Display import method in wallet details
    - Add conditional mnemonic export options
    - _Requirements: 5.1, 5.2, 5.5_

  - [ ] 13.2 Enhance error display in UI
    - Show specific error messages for different conflict types
    - Provide clear guidance for resolving import issues
    - Update help text to explain import method differences
    - _Requirements: 5.3, 5.4, 5.5_

- [ ] 14. Comprehensive testing implementation
  - [ ] 14.1 Create unit tests for duplicate detection scenarios
    - Test multiple mnemonic imports with same/different addresses
    - Test private key import without mnemonic generation
    - Test source hash generation and uniqueness
    - Test error message generation for different scenarios
    - _Requirements: 8.1, 8.2, 8.3_

  - [ ] 14.2 Create Universal KDF unit tests
    - Test KDF parameter conversion for different JSON types
    - Test KDF name normalization and case variations
    - Test salt format conversion (hex, array, string)
    - Test security validation for different parameter ranges
    - Test compatibility analysis for various KeyStore formats
    - _Requirements: 8.6, 8.7, 8.8, 8.9_

  - [ ] 14.3 Create integration tests for import workflows
    - Test end-to-end mnemonic import with duplicate detection
    - Test end-to-end private key import without mnemonic
    - Test coexistence of wallets with same address but different methods
    - Test database migration with existing data
    - Test Universal KDF keystore import with real-world KeyStore files
    - _Requirements: 8.4, 8.5, 8.10_

  - [ ] 14.4 Create comprehensive KeyStore compatibility test suite
    - Test KeyStores from different wallet providers (Geth, MetaMask, Trust Wallet, etc.)
    - Test KeyStores with various KDF configurations and parameter types
    - Test KeyStores with edge cases and unusual parameter values
    - Test performance with high-security KDF parameters
    - Create test data generator for comprehensive coverage
    - _Requirements: 8.10, 6.5, 7.1_

- [ ] 15. Remove deprecated deterministic mnemonic functionality
  - [ ] 15.1 Clean up deterministic mnemonic code
    - Remove GenerateDeterministicMnemonic function
    - Remove related validation functions
    - Update any remaining references to deterministic mnemonics
    - Clean up unused imports and dependencies
    - _Requirements: 2.1, 2.2_

  - [ ] 15.2 Update documentation and comments
    - Remove references to deterministic mnemonic generation
    - Update code comments to reflect new import logic
    - Add documentation for new duplicate detection approach
    - _Requirements: 2.3, 2.4_