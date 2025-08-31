# Build System Documentation

This document describes the comprehensive build and CI/CD system implemented for the bloco-wallet-manager project.

## Overview

The build system includes:
- **Makefile**: Multi-platform build automation for local development
- **GitHub Actions**: CI/CD pipelines for automated testing, building, and releasing
- **Docker**: Multi-platform container builds
- **Version Management**: Automated semantic versioning and release management

## Files Created

### Core Build Files
- `Makefile` - Comprehensive build automation
- `Dockerfile` - Multi-stage container build
- `.dockerignore` - Container build optimization
- `cmd/blocowallet/main.go` - Main application entry point

### CI/CD Workflows
- `.github/workflows/ci.yml` - Continuous Integration
- `.github/workflows/release.yml` - Release automation  
- `.github/workflows/container.yml` - Container builds and security scanning

### Configuration
- `.golangci.yml` - Code quality and linting configuration

## Makefile Targets

### Build Targets
- `make build` - Build for current platform
- `make build-all` - Build for all supported platforms (Linux amd64/arm64, macOS amd64/arm64, Windows amd64)
- `make build-linux` - Build only Linux platforms
- `make build-darwin` - Build only macOS platforms
- `make build-windows` - Build only Windows platform

### Testing Targets
- `make test` - Run all tests with race detection
- `make test-short` - Run short tests only
- `make cover` - Generate test coverage report
- `make bench` - Run benchmarks

### Code Quality Targets
- `make lint` - Run golangci-lint
- `make fmt` - Format code
- `make vet` - Run go vet
- `make mod` - Tidy and download modules

### Container Targets
- `make docker-build` - Build multi-platform container images
- `make docker-push` - Push images to registry
- `make docker-run` - Run container locally

### Release Targets
- `make release-prep` - Prepare release artifacts
- `make release-local` - Create local release package
- `make checksums` - Generate SHA256 checksums

### Utility Targets
- `make clean` - Clean build artifacts
- `make deps` - Install build dependencies
- `make install` - Install binary to GOPATH/bin
- `make version` - Show version information
- `make info` - Show build environment info
- `make help` - Display all available targets

## GitHub Actions Workflows

### CI Workflow (`ci.yml`)
**Triggers**: Push to main/develop, Pull Requests to main

**Jobs**:
- **Test**: Run unit tests with coverage reporting
- **Lint**: Code quality checks with golangci-lint
- **Build**: Cross-compile for all platforms
- **Security**: Vulnerability scanning with Gosec and Trivy

**Features**:
- Go module caching for faster builds
- Artifact upload for testing
- Coverage reporting to Codecov
- Security scanning results in GitHub Security tab

### Release Workflow (`release.yml`)
**Triggers**: Version tags (v*.*.*), Manual dispatch

**Jobs**:
- **Validate**: Version format validation and prerelease detection
- **Build**: Multi-platform binary compilation
- **Checksums**: SHA256 checksum generation
- **Release**: GitHub release creation with assets
- **Update-Version**: Automatic version updates in main branch

**Features**:
- Semantic versioning support
- Prerelease detection (alpha, beta, rc)
- Automated release notes generation
- Multi-platform binary archives
- Checksum verification files

### Container Workflow (`container.yml`)
**Triggers**: Push to main/develop, Tags, Manual dispatch

**Jobs**:
- **Container**: Multi-platform image builds
- **Security-Scan**: Container vulnerability scanning
- **Container-Test**: Container functionality testing
- **Cleanup**: Old image cleanup

**Features**:
- Multi-platform support (linux/amd64, linux/arm64)
- GitHub Container Registry integration
- Security scanning with Trivy and Grype
- SBOM (Software Bill of Materials) generation
- Automated image tagging strategy

## Platform Support

### Supported Platforms
| Platform | Architecture | Binary Format | Container Support |
|----------|-------------|---------------|-------------------|
| Linux | AMD64 | tar.gz | ✅ |
| Linux | ARM64 | tar.gz | ✅ |
| macOS | x86_64 | tar.gz | ❌ |
| macOS | ARM64 (Apple Silicon) | tar.gz | ❌ |
| Windows | AMD64 | zip | ❌ |

### Container Images
- **Registry**: GitHub Container Registry (ghcr.io)
- **Base**: Multi-stage build from golang:1.23.1-alpine to scratch
- **Platforms**: linux/amd64, linux/arm64
- **Tags**: latest, develop, version tags, branch names

## Version Management

### Versioning Strategy
- **Semantic Versioning** (vX.Y.Z)
- **Automatic Detection** from Git tags
- **Version Injection** into binaries during build
- **Prerelease Support** (alpha, beta, rc suffixes)

### Release Process
1. **Tag Creation**: Developer creates version tag (e.g., v1.2.3)
2. **Validation**: GitHub Actions validates tag format
3. **Build**: Multi-platform binaries are compiled
4. **Container**: Docker images are built and pushed
5. **Release**: GitHub release is created with assets
6. **Update**: Version files are updated in main branch

### Version Information
Version information is injected into binaries:
```go
var (
    version = "dev"    // Set by build process
    commit  = "unknown" // Git commit hash
    date    = "unknown" // Build timestamp
)
```

## Security Features

### Build Security
- **Dependency Scanning**: Automated vulnerability checks
- **Container Scanning**: Multi-tool security analysis
- **Code Analysis**: Static security analysis with Gosec
- **SBOM Generation**: Software Bill of Materials for releases

### Access Control
- **Protected Branches**: Main branch requires reviews
- **Registry Access**: Restricted push access to GHCR
- **Secrets Management**: GitHub Secrets for sensitive data

## Development Workflow

### Local Development
```bash
# Setup
make deps                    # Install dependencies
make build                   # Build for current platform
make test                    # Run tests
make lint                    # Check code quality

# Multi-platform build
make build-all              # Build for all platforms
make checksums              # Generate checksums

# Container development
make docker-build           # Build container images
make docker-run             # Test locally
```

### Release Process
```bash
# Create and push version tag
git tag v1.2.3
git push origin v1.2.3

# GitHub Actions will automatically:
# 1. Build multi-platform binaries
# 2. Create container images
# 3. Generate release with assets
# 4. Update version in main branch
```

### CI/CD Integration
- **Pull Requests**: Full CI pipeline with testing and building
- **Main Branch**: Container builds and security scanning
- **Version Tags**: Complete release automation
- **Manual Triggers**: Release workflow can be triggered manually

## Configuration

### Environment Variables
- `GO_VERSION`: Go version for builds (default: 1.23.1)
- `GOLANGCI_LINT_VERSION`: Linting tool version
- `CGO_ENABLED`: CGO setting (default: 0)
- `BUILD_PLATFORMS`: Container platforms

### Build Flags
- **LDFLAGS**: Version injection and binary optimization
- **Tags**: Build tags (default: netgo)
- **CGO**: Disabled for static binaries

## Monitoring and Observability

### Metrics Tracked
- Build success/failure rates
- Test coverage percentages
- Security vulnerability counts
- Container image sizes
- Build duration times

### Alerts and Notifications
- Build failure notifications
- Security vulnerability alerts
- Release completion notifications

## Troubleshooting

### Common Issues
1. **Build Failures**: Check Go version compatibility and dependencies
2. **Test Failures**: Ensure all dependencies are available
3. **Container Issues**: Verify Docker buildx setup
4. **Release Problems**: Check tag format and permissions

### Debug Commands
```bash
make info                   # Show build environment
make version               # Show version information
go version                 # Check Go version
docker version            # Check Docker version
```

## Best Practices

### Development
- Always run `make test` before committing
- Use `make lint` to ensure code quality
- Test multi-platform builds locally when possible
- Update documentation with significant changes

### Releases
- Follow semantic versioning strictly
- Test release candidates before stable releases
- Verify checksums after release
- Monitor security scan results

### Security
- Regularly update dependencies
- Review security scan reports
- Keep base images updated
- Use minimal container images

## Future Enhancements

### Planned Improvements
- **Performance Monitoring**: Build time optimization
- **Advanced Security**: Code signing for binaries
- **Extended Platforms**: Additional architecture support
- **Integration Testing**: End-to-end test automation
- **Release Analytics**: Detailed release metrics

This build system provides a robust, secure, and automated foundation for the bloco-wallet-manager project, supporting both development workflows and production releases.