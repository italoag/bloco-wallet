# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Quick Development Testing
```bash
make t                  # Ultra-fast tests (alias for test-fast)
make test-fast         # Fast tests with lightweight parameters (~10-15s)
make test-wallet       # Only wallet package tests (fastest)
make test-ui           # Only UI package tests
```

### Standard Testing
```bash
make test              # All tests with optimized parameters (~30-45s)
make test-production   # Production-grade security tests (~5-10 min)
make cover             # Test coverage report
make bench             # Run benchmarks
```

### Build Commands
```bash
make build             # Build for current platform
make build-all         # Cross-compile for all platforms
make build-linux       # Build for Linux (amd64, arm64)
make build-darwin      # Build for macOS (amd64, arm64) 
make build-windows     # Build for Windows amd64
```

### Code Quality
```bash
make lint              # Run golangci-lint (or go vet if not installed)
make fmt               # Format code with go fmt
make vet               # Run go vet
make mod               # Tidy and download Go modules
```

### Development Workflow
```bash
make deps              # Install build dependencies
make install           # Install binary to GOPATH/bin
make clean             # Clean build artifacts
make version           # Show version information
```

## Project Architecture

### Core Structure
- **cmd/blocowallet/**: Main application entry point with version injection
- **internal/**: Core business logic (not exported)
  - **wallet/**: Cryptocurrency wallet operations, keystore handling, mnemonic generation
  - **ui/**: Terminal User Interface using Bubble Tea framework
  - **blockchain/**: Ethereum network interaction, multi-provider support
  - **storage/**: Database operations using GORM with SQLite
- **pkg/**: Reusable packages (exported)
  - **config/**: Configuration management with TOML
  - **localization/**: Internationalization support
  - **logger/**: Structured logging with Zap

### Key Technologies
- **UI Framework**: Bubble Tea (charmbracelet/bubbletea) for Terminal UI
- **Database**: SQLite with GORM ORM
- **Crypto**: go-ethereum for Ethereum operations, BIP32/39 for mnemonics
- **Config**: TOML format with Viper
- **Testing**: Standard Go testing with production/development parameter modes

### Import Patterns
- Use `blocowallet/` as the module prefix for all internal imports
- External imports: go-ethereum, charmbracelet libraries, tyler-smith BIP packages
- Database models are defined in the storage package
- UI components use Bubble Tea model-view-update pattern

### Testing Strategy
The project uses **dual-parameter testing**:
- **Development mode** (default): Fast cryptographic parameters for rapid iteration
- **Production mode** (`-tags=production`): Full-strength security parameters

Always use `make test-fast` or `make t` during development, and `make test-production` before releases.

### Build Tags and CGO
- **CGO**: Required for SQLite support (`CGO_ENABLED=1`)
- **Build tags**: `netgo` for networking, `production` for secure crypto parameters
- **macOS**: Automatically handles linker warnings with `CGO_LDFLAGS="-Wl,-ld_classic"`
- **Static builds**: Available with pure Go SQLite driver (`make build-static`)

### Configuration Files
- **go.mod**: Go 1.24.3+ required
- **Makefile**: Comprehensive build automation with cross-platform support
- **.golangci.yml**: Linting configuration (install with `make deps`)

## Key Features and Components

### Wallet Management
- **Keystore V3**: Compatible with Ethereum keystore format
- **Import methods**: Mnemonic phrases, private keys, keystore files
- **Enhanced import**: Multi-file selection, batch processing, automatic password detection
- **Security**: Scrypt KDF with configurable parameters

### UI Components
- **File picker**: Enhanced multi-selection with keyboard navigation
- **Progress tracking**: Import progress with pause/resume functionality
- **Password input**: Secure password entry with masking
- **Network management**: Add/configure blockchain networks

### Data Storage
- **SQLite**: Embedded database with GORM ORM
- **Repository pattern**: Clean separation between data access and business logic
- **Migrations**: Handled automatically by GORM

### Network Support
- **Ethereum**: Primary blockchain support
- **Multi-provider**: Chainlist.org integration for network discovery
- **Custom networks**: User can add custom RPC endpoints

## Development Guidelines

### Testing Recommendations
- Use `make t` for quick iteration during development
- Run `make test` before committing changes
- Use `make test-production` before releases or when testing cryptographic security
- Test specific packages with `make test-wallet` or `make test-ui` for faster feedback

### Code Organization
- Follow the existing package structure (internal/ vs pkg/)
- Use repository pattern for data access
- Implement UI components following Bubble Tea patterns
- Add proper error handling with localized messages

### Performance Considerations
- Database operations use connection pooling
- Cryptographic operations are optimized with different parameter sets
- File operations include proper error handling and cleanup
- UI rendering is optimized for terminal performance

### Security Best Practices
- Never log or expose private keys or passwords
- Use secure random generation for cryptographic operations
- Validate all user inputs, especially file paths and passwords
- Follow Ethereum keystore V3 specification for compatibility