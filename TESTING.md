# Testing Guide

This document provides guidance on running tests for the bloco-wallet-manager project with optimized performance.

## Quick Start

For day-to-day development, use the fastest test commands:

```bash
# Quick development testing (fastest)
make t              # Alias for test-fast
make test-fast      # Fast tests with optimized parameters
make test-wallet    # Only wallet package tests
```

## Test Strategy

The project uses **dual-parameter testing** to balance speed and security:

- **Development Mode** (default): Uses lightweight scrypt parameters (~10x faster)
- **Production Mode**: Uses full-strength scrypt parameters (secure but slower)

### Why Two Modes?

1. **Fast Development**: Cryptographic tests with secure parameters can take 20+ seconds
2. **Security Validation**: Production builds still test with full security
3. **CI Efficiency**: Faster feedback loops for developers

## Test Commands Overview

### Development Testing (Recommended)

```bash
# Fastest options for development
make t                  # Ultra-fast alias
make test-fast         # Short tests with fast parameters
make test-wallet       # Only wallet package tests
make test-ui           # Only UI package tests

# Standard development testing
make test              # All tests with optimized parameters
make test-quiet        # Suppress macOS linker warnings
```

### Production Testing (Security Validation)

```bash
# Full security validation (slower)
make test-production   # All tests with production scrypt parameters
make cover-production  # Coverage with production parameters
```

### Coverage and Benchmarks

```bash
make cover             # Test coverage (fast parameters)
make cover-production  # Test coverage (production parameters)
make bench             # Run benchmarks
```

## macOS CGO Linker Warnings

If you see warnings like this when running tests on macOS:

```
ld: warning: 'file.o' has malformed LC_DYSYMTAB, expected X undefined symbols to start at index Y, found Z undefined symbols starting at index Y
```

These warnings are **harmless** and are caused by a known issue with the Go linker on macOS 11+ when CGO is enabled (which is required for SQLite support).

**Solution**: All Makefile targets automatically handle this with `CGO_LDFLAGS="-Wl,-ld_classic"`

## Performance Comparison

| Test Type | Parameters | Typical Time | Use Case |
|-----------|------------|--------------|----------|
| `make t` | Fast | ~10-15s | Development iteration |
| `make test` | Fast | ~30-45s | Pre-commit validation |
| `make test-production` | Secure | ~5-10 min | Release validation |

## Test Parameters Explained

### Fast Parameters (Development)
- **Scrypt N**: 4,096 (vs 262,144 production)
- **Scrypt P**: 1 (same as production)
- **Speed**: ~10x faster
- **Security**: Adequate for testing, not for production keys

### Production Parameters (Security)
- **Scrypt N**: 262,144 (standard secure value)
- **Scrypt P**: 1
- **Speed**: Full computation time
- **Security**: Production-grade cryptographic security

## Build Tags

The project uses build tags to control test behavior:

```bash
# Development mode (default - fast parameters)
go test ./...

# Production mode (secure parameters)
go test ./... -tags=production
```

## Test Categories

### Unit Tests
- Located alongside source files with `_test.go` suffix
- Test individual functions and components
- Run with: `make test-wallet` or `make test-ui`

### Integration Tests  
- Test interactions between components
- Include database and cryptographic operations
- Automatically included in standard test runs

### Performance Tests
- Benchmark critical cryptographic operations
- Run with: `make bench`

## Direct Go Commands

If you prefer using Go directly:

```bash
# Fast development testing
go test ./... -short -v

# Production security testing
go test ./... -v -tags=production

# With macOS linker fix
CGO_LDFLAGS="-Wl,-ld_classic" go test ./... -v
```

## Troubleshooting

### Common Issues

1. **Slow Tests**: Use `make t` or `make test-fast` for development
2. **SQLite CGO Errors**: Ensure `CGO_ENABLED=1` and CGO toolchain is available
3. **macOS Linker Warnings**: Automatically handled by Makefile targets
4. **Test Timeouts**: Use `-timeout=30m` for production parameter tests

### Platform-Specific Notes

#### macOS
- Requires Xcode Command Line Tools for CGO
- Makefile automatically applies linker fixes
- No manual configuration needed

#### Linux  
- Requires `gcc` for CGO compilation
- Generally no special configuration needed

#### Windows
- Requires TDM-GCC or similar for CGO
- May need specific CGO configuration

## Continuous Integration

CI/CD uses optimized testing strategy:
- **Pull Requests**: Fast parameters for quick feedback
- **Main Branch**: Production parameters for security validation
- **Releases**: Full production testing with all security checks

## Recommendations

### For Development
1. Use `make t` for quick iteration
2. Use `make test` before committing
3. Use `make test-wallet` when working on wallet features

### For Production
1. Always run `make test-production` before releases
2. Verify `make cover-production` meets coverage requirements
3. Run `make bench` to check performance regressions

### For CI/CD
1. Use fast parameters for PR validation
2. Use production parameters for release validation
3. Cache Go modules and build artifacts for speed