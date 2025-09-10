# Technical Stack

## Programming Language
- Go (version 1.24.3)

## Key Libraries and Dependencies
- **Ethereum**: github.com/ethereum/go-ethereum - Core Ethereum functionality and keystore management
- **UI Framework**: github.com/charmbracelet/bubbletea - Terminal UI framework
- **UI Components**: github.com/charmbracelet/bubbles - UI components for Bubble Tea
- **UI Styling**: github.com/charmbracelet/lipgloss - Style definitions for terminal UI
- **Configuration**: github.com/BurntSushi/toml, github.com/spf13/viper - Configuration management
- **Localization**: github.com/nicksnyder/go-i18n/v2 - Internationalization support
- **Cryptography**: 
  - github.com/tyler-smith/go-bip32 - BIP32 HD wallet implementation
  - github.com/tyler-smith/go-bip39 - BIP39 mnemonic generation
  - golang.org/x/crypto - Cryptographic primitives
- **Database**: gorm.io/gorm - ORM with support for multiple database backends
- **Logging**: go.uber.org/zap - Structured logging
- **Testing**: github.com/stretchr/testify - Testing utilities and assertions

## Database Support
- SQLite (only supported database)

## Common Commands

### Building the Application
```bash
go build -o blocowallet cmd/blocowallet/main.go
```

### Running the Application
```bash
./blocowallet
# or
go run cmd/blocowallet/main.go
```

### Running Tests
```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests for a specific package
go test ./internal/wallet/...
```

### Generating Test Data
```bash
# Generate test keystores
cd internal/wallet/testdata
go run generate_test_keystores.go
```