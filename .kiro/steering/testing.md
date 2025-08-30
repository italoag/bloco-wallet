---
inclusion: always
---

# Testing Guidelines for BLOCO Wallet Manager

## Core Testing Principles
- **Security-First Testing**: All cryptographic operations and keystore handling must be thoroughly tested
- **Test-Driven Development**: Write tests before implementing new wallet functionality
- **Deterministic Testing**: Use fixed test data and mocks to ensure reproducible results
- **Error Path Coverage**: Test all error conditions, especially for wallet operations and keystore validation

## Testing Commands
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific package tests
go test ./internal/wallet/...
go test ./pkg/localization/...

# Run tests with verbose output
go test -v ./...
```

## Testing Workflow
1. **Before Changes**: Run `go test ./...` to ensure baseline functionality
2. **During Development**: Add unit tests for new functions as you implement them
3. **After Implementation**: Run full test suite including integration tests
4. **Manual Validation**: Test CLI commands manually for user-facing features

## Test Data Management
- Use `testdata` directories for test keystores and fixtures
- Generate test keystores using existing generators in `internal/wallet/testdata/`
- Never commit real private keys or sensitive data to test files
- Use deterministic test data for reproducible results

## Critical Testing Areas
- **Keystore Validation**: Test all KeyStoreV3 format variations and error conditions
- **Cryptographic Operations**: Verify mnemonic generation, key derivation, and encryption
- **Localization**: Test error messages in multiple languages
- **Database Operations**: Test wallet storage and retrieval with proper cleanup
- **Network Integration**: Mock blockchain calls for consistent testing

## Test Structure Requirements
- Place test files alongside source code with `_test.go` suffix
- Use `testify/assert` and `testify/require` for assertions
- Create mock implementations with `mock_` prefix for external dependencies
- Group related tests using subtests (`t.Run()`)

## Security Testing Guidelines
- Test password validation and encryption/decryption cycles
- Verify proper handling of invalid or corrupted keystores
- Test memory cleanup for sensitive data
- Validate proper error messages without leaking sensitive information
