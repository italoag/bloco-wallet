# Project Configuration
NAME            := bloco-wallet-manager
VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0")
PACKAGE         := blocowallet
OUTPUT_BIN      ?= build/${NAME}
GO_FLAGS        ?=
GO_TAGS         ?= netgo
CGO_ENABLED     ?= 0
GIT_REV         ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH      ?= $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")

# Container Configuration
IMG_NAME        := ghcr.io/italoag/${NAME}
IMAGE           := ${IMG_NAME}:${VERSION}
BUILD_PLATFORMS ?= linux/amd64,linux/arm64

# Date handling for different OS
SOURCE_DATE_EPOCH ?= $(shell date +%s)
ifeq ($(shell uname), Darwin)
DATE            ?= $(shell TZ=UTC date -j -f "%s" ${SOURCE_DATE_EPOCH} +"%Y-%m-%dT%H:%M:%SZ")
else
DATE            ?= $(shell date -u -d @${SOURCE_DATE_EPOCH} +"%Y-%m-%dT%H:%M:%SZ")
endif

# Build matrix for cross-compilation
PLATFORMS       := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64
BUILD_DIR       := build
DIST_DIR        := dist
CMD_DIR         := cmd/blocowallet

# LDFLAGS for version injection
LDFLAGS         := -w -s \
				   -X main.version=${VERSION} \
				   -X main.commit=${GIT_REV} \
				   -X main.date=${DATE}

# Colors for output
RED     := \033[31m
GREEN   := \033[32m
YELLOW  := \033[33m
BLUE    := \033[34m
PURPLE  := \033[35m
CYAN    := \033[36m
WHITE   := \033[37m
RESET   := \033[0m

.PHONY: help
default: help

##@ Build Targets

.PHONY: build
build: ## Build the application for current platform
	@echo "$(CYAN)Building ${NAME} for current platform...$(RESET)"
	@mkdir -p ${BUILD_DIR}
	@CGO_ENABLED=${CGO_ENABLED} go build ${GO_FLAGS} \
		-ldflags "${LDFLAGS}" \
		-a -tags=${GO_TAGS} \
		-o ${OUTPUT_BIN} \
		./${CMD_DIR}
	@echo "$(GREEN)✓ Build complete: ${OUTPUT_BIN}$(RESET)"

.PHONY: build-all
build-all: clean ## Build for all supported platforms
	@echo "$(CYAN)Building ${NAME} for all platforms...$(RESET)"
	@mkdir -p ${DIST_DIR}
	@$(foreach platform,$(PLATFORMS),$(call build_platform,$(platform)))
	@echo "$(GREEN)✓ All platform builds complete$(RESET)"

.PHONY: build-linux
build-linux: ## Build for Linux platforms (amd64, arm64)
	@echo "$(CYAN)Building ${NAME} for Linux platforms...$(RESET)"
	@mkdir -p ${DIST_DIR}
	@$(call build_platform,linux/amd64)
	@$(call build_platform,linux/arm64)
	@echo "$(GREEN)✓ Linux builds complete$(RESET)"

.PHONY: build-darwin
build-darwin: ## Build for macOS platforms (amd64, arm64)
	@echo "$(CYAN)Building ${NAME} for macOS platforms...$(RESET)"
	@mkdir -p ${DIST_DIR}
	@$(call build_platform,darwin/amd64)
	@$(call build_platform,darwin/arm64)
	@echo "$(GREEN)✓ macOS builds complete$(RESET)"

.PHONY: build-windows
build-windows: ## Build for Windows platform (amd64)
	@echo "$(CYAN)Building ${NAME} for Windows...$(RESET)"
	@mkdir -p ${DIST_DIR}
	@$(call build_platform,windows/amd64)
	@echo "$(GREEN)✓ Windows build complete$(RESET)"

##@ Testing Targets

.PHONY: test
test: ## Run all tests
	@echo "$(CYAN)Running tests...$(RESET)"
	@go clean --testcache 
	@go test ./... -v -race
	@echo "$(GREEN)✓ Tests complete$(RESET)"

.PHONY: test-short
test-short: ## Run tests with short flag
	@echo "$(CYAN)Running short tests...$(RESET)"
	@go test ./... -short -v
	@echo "$(GREEN)✓ Short tests complete$(RESET)"

.PHONY: cover
cover: ## Run test coverage suite
	@echo "$(CYAN)Generating test coverage...$(RESET)"
	@go test ./... --coverprofile=coverage.out
	@go tool cover --html=coverage.out -o coverage.html
	@echo "$(GREEN)✓ Coverage report generated: coverage.html$(RESET)"

.PHONY: bench
bench: ## Run benchmarks
	@echo "$(CYAN)Running benchmarks...$(RESET)"
	@go test ./... -bench=. -benchmem
	@echo "$(GREEN)✓ Benchmarks complete$(RESET)"

##@ Code Quality Targets

.PHONY: lint
lint: ## Run linting checks
	@echo "$(CYAN)Running linter...$(RESET)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "$(YELLOW)⚠ golangci-lint not installed, running go vet instead$(RESET)"; \
		go vet ./...; \
	fi
	@echo "$(GREEN)✓ Linting complete$(RESET)"

.PHONY: fmt
fmt: ## Format code
	@echo "$(CYAN)Formatting code...$(RESET)"
	@go fmt ./...
	@echo "$(GREEN)✓ Code formatted$(RESET)"

.PHONY: vet
vet: ## Run go vet
	@echo "$(CYAN)Running go vet...$(RESET)"
	@go vet ./...
	@echo "$(GREEN)✓ Vet complete$(RESET)"

.PHONY: mod
mod: ## Tidy and download modules
	@echo "$(CYAN)Tidying modules...$(RESET)"
	@go mod tidy
	@go mod download
	@echo "$(GREEN)✓ Modules updated$(RESET)"

##@ Container Targets

.PHONY: docker-build
docker-build: ## Build multi-platform Docker images
	@echo "$(CYAN)Building Docker images for platforms: ${BUILD_PLATFORMS}$(RESET)"
	@docker buildx build \
		--platform ${BUILD_PLATFORMS} \
		--build-arg VERSION=${VERSION} \
		--build-arg GIT_REV=${GIT_REV} \
		--build-arg BUILD_DATE=${DATE} \
		--rm -t ${IMAGE} \
		--load .
	@echo "$(GREEN)✓ Docker images built$(RESET)"

.PHONY: docker-push
docker-push: ## Push Docker images to registry
	@echo "$(CYAN)Pushing Docker images to registry...$(RESET)"
	@docker buildx build \
		--platform ${BUILD_PLATFORMS} \
		--build-arg VERSION=${VERSION} \
		--build-arg GIT_REV=${GIT_REV} \
		--build-arg BUILD_DATE=${DATE} \
		--rm -t ${IMAGE} \
		--push .
	@echo "$(GREEN)✓ Docker images pushed$(RESET)"

.PHONY: docker-run
docker-run: ## Run the container locally
	@echo "$(CYAN)Running container locally...$(RESET)"
	@docker run --rm -it ${IMAGE}

##@ Release Targets

.PHONY: release-prep
release-prep: clean build-all checksums ## Prepare release artifacts
	@echo "$(GREEN)✓ Release artifacts prepared in ${DIST_DIR}$(RESET)"

.PHONY: release-local
release-local: release-prep ## Create local release package
	@echo "$(CYAN)Creating local release package...$(RESET)"
	@mkdir -p ${DIST_DIR}/release
	@cd ${DIST_DIR} && tar -czf release/${NAME}-${VERSION}-release.tar.gz *.tar.gz *.zip checksums.txt
	@echo "$(GREEN)✓ Local release package created: ${DIST_DIR}/release/${NAME}-${VERSION}-release.tar.gz$(RESET)"

.PHONY: checksums
checksums: ## Generate checksums for release artifacts
	@echo "$(CYAN)Generating checksums...$(RESET)"
	@cd ${DIST_DIR} && \
		find . -name "*.tar.gz" -o -name "*.zip" | \
		xargs shasum -a 256 > checksums.txt
	@echo "$(GREEN)✓ Checksums generated: ${DIST_DIR}/checksums.txt$(RESET)"

##@ Utility Targets

.PHONY: clean
clean: ## Clean build artifacts
	@echo "$(CYAN)Cleaning build artifacts...$(RESET)"
	@rm -rf ${BUILD_DIR} ${DIST_DIR}
	@rm -f coverage.out coverage.html
	@echo "$(GREEN)✓ Clean complete$(RESET)"

.PHONY: deps
deps: ## Install build dependencies
	@echo "$(CYAN)Installing dependencies...$(RESET)"
	@go mod download
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing golangci-lint...$(RESET)"; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.60.1; \
	fi
	@echo "$(GREEN)✓ Dependencies installed$(RESET)"

.PHONY: install
install: build ## Install the binary to GOPATH/bin
	@echo "$(CYAN)Installing ${NAME} to GOPATH/bin...$(RESET)"
	@cp ${OUTPUT_BIN} $$(go env GOPATH)/bin/
	@echo "$(GREEN)✓ ${NAME} installed to $$(go env GOPATH)/bin/$(RESET)"

.PHONY: uninstall
uninstall: ## Remove the binary from GOPATH/bin
	@echo "$(CYAN)Uninstalling ${NAME}...$(RESET)"
	@rm -f $$(go env GOPATH)/bin/${NAME}
	@echo "$(GREEN)✓ ${NAME} uninstalled$(RESET)"

.PHONY: version
version: ## Show version information
	@echo "Name:        ${NAME}"
	@echo "Version:     ${VERSION}"
	@echo "Git Commit:  ${GIT_REV}"
	@echo "Git Branch:  ${GIT_BRANCH}"
	@echo "Build Date:  ${DATE}"
	@echo "Go Version:  $$(go version)"

.PHONY: info
info: ## Show build environment information
	@echo "$(CYAN)Build Environment Information:$(RESET)"
	@echo "Name:           ${NAME}"
	@echo "Version:        ${VERSION}"
	@echo "Package:        ${PACKAGE}"
	@echo "Git Commit:     ${GIT_REV}"
	@echo "Git Branch:     ${GIT_BRANCH}"
	@echo "Build Date:     ${DATE}"
	@echo "Go Version:     $$(go version)"
	@echo "Go Env GOOS:    $$(go env GOOS)"
	@echo "Go Env GOARCH:  $$(go env GOARCH)"
	@echo "CGO Enabled:    ${CGO_ENABLED}"
	@echo "Build Tags:     ${GO_TAGS}"
	@echo "Platforms:      $(PLATFORMS)"

##@ Help

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\n$(CYAN)Usage:$(RESET)\n  make $(YELLOW)<target>$(RESET)\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  $(YELLOW)%-15s$(RESET) %s\n", $$1, $$2 } /^##@/ { printf "\n$(CYAN)%s$(RESET)\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

# Build function for cross-compilation
define build_platform
	$(eval GOOS := $(word 1,$(subst /, ,$(1))))
	$(eval GOARCH := $(word 2,$(subst /, ,$(1))))
	$(eval EXT := $(if $(filter windows,$(GOOS)),.exe,))
	$(eval OUTPUT := ${DIST_DIR}/${NAME}-${VERSION}-$(GOOS)-$(GOARCH)$(EXT))
	$(eval ARCHIVE := $(if $(filter windows,$(GOOS)),${DIST_DIR}/${NAME}-${VERSION}-$(GOOS)-$(GOARCH).zip,${DIST_DIR}/${NAME}-${VERSION}-$(GOOS)-$(GOARCH).tar.gz))
	
	@echo "$(BLUE)  Building for $(GOOS)/$(GOARCH)...$(RESET)"
	@CGO_ENABLED=${CGO_ENABLED} GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build ${GO_FLAGS} \
		-ldflags "${LDFLAGS}" \
		-a -tags=${GO_TAGS} \
		-o $(OUTPUT) \
		./${CMD_DIR}
	
	@if [ "$(GOOS)" = "windows" ]; then \
		cd ${DIST_DIR} && zip -q $(notdir $(ARCHIVE)) $(notdir $(OUTPUT)); \
	else \
		cd ${DIST_DIR} && tar -czf $(notdir $(ARCHIVE)) $(notdir $(OUTPUT)); \
	fi
	@rm -f $(OUTPUT)
	@echo "$(GREEN)    ✓ $(GOOS)/$(GOARCH) → $(notdir $(ARCHIVE))$(RESET)"
endef