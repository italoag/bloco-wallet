Project-specific development guidelines for BLOCO Wallet Manager

Audience: Advanced Go developers working on this codebase. This document captures non-obvious build/config/testing knowledge verified against the current repository state.

1. Build and Configuration
- Go toolchain
  - Module path: blocowallet (see go.mod)
  - Go version: 1.23.1 (as declared in go.mod)
- Building the CLI
  - Command: go build -o blocowallet ./cmd/blocowallet
  - Output binary can be executed directly: ./blocowallet
  - The CLI uses Charm’s Bubble Tea and runs in an alt-screen TUI. Logs are written to blocowallet.log in the repo/workdir.
- Runtime configuration
  - On first run, a config file is materialized at: ~/.wallets/config.toml
    - It is generated from the embedded pkg/config/default_config.toml (via go:embed and viper).
    - The app ensures ~/.wallets exists.
  - Effective config structure (pkg/config.Config):
    - app: app_dir, language, wallets_dir, database_path, locale_dir
    - database: type (only sqlite supported in code paths), dsn (optional)
    - security: Argon2id params (argon2_time, argon2_memory, argon2_threads, argon2_key_len, salt_length)
    - fonts.available: string list returned by Config.GetFontsList()
    - networks: map[string]Network (name, rpc_endpoint, chain_id, symbol, explorer, is_active)
  - Environment overrides
    - Prefix: BLOCOWALLET_
    - Mapping uses dot->underscore replacement by viper. Examples:
      - BLOCOWALLET_APP_APP_DIR, BLOCOWALLET_APP_WALLETS_DIR, BLOCOWALLET_APP_DATABASE_PATH
      - BLOCOWALLET_DATABASE_TYPE, BLOCOWALLET_DATABASE_DSN
    - Paths with a leading ~/ are expanded to the user home directory by pkg/config.expandPath.
  - Database
    - internal/storage/gorm_repository.go currently selects gorm.io/driver/sqlite exclusively for dev/test.
    - dsn behavior: if Config.Database.DSN is non-empty, it is used; otherwise Config.DatabasePath is used. 
    - The code auto-migrates internal/wallet.Wallet on startup.
    - Note on CGO: gorm.io/driver/sqlite pulls github.com/mattn/go-sqlite3 (CGO). Ensure a working C toolchain:
      - macOS: xcode-select --install; Linux: gcc/clang + libc headers; Windows: use MSYS2/MinGW. Alternatively, use pure-Go SQLite only if you refactor to modernc/sqlite.
- Localization
  - Initialization: pkg/localization.InitLocalization(cfg) in cmd/blocowallet/main.go after config load.
  - There is a legacy map pkg/localization.Labels retained for compatibility; do not rely on it unless necessary. Prefer the newer locale-driven APIs where applicable.

2. Testing
- Running tests
  - Full suite: go test ./...
    - Current state (verified): some packages pass (internal/storage, internal/ui, pkg/config), while internal/wallet and pkg/localization contain tests that may fail depending on localization message expectations and environment. When iterating quickly, target packages or tests to avoid unrelated failures.
  - Package subset: go test ./internal/storage ./pkg/config
  - Single package, verbose: go test -v ./pkg/config
  - Filter tests by regex: go test ./pkg/localization -run 'TestGetKeystoreErrorMessage|TestFormatKeystoreErrorWithField'
  - Skip heavy/interactive domains: avoid ./internal/wallet and TUI-centric tests unless you are explicitly working there. You can hone in with -run on specific tests you’re fixing.
- Coverage and race checks
  - Coverage: go test -cover ./...
  - HTML coverage: go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
  - Race detector (recommended for concurrency/code touching UI or storage): go test -race ./...
- Test data and fixtures
  - Keystore fixtures live under internal/wallet/testdata/keystores and keystores/.
  - Some tests generate additional fixtures (see internal/wallet/testdata/generate_*.go). These are not part of typical runs; do not run generators unless you intend to update fixtures and their README.
  - When writing new tests that touch keystores or Argon2, keep parameters small (as in default_config.toml) for CI performance.
- Localization tests
  - pkg/localization contains helpers like InitCryptoMessagesForTesting() to set up messages deterministically for unit tests.
  - Be aware: message expectations are language-specific and some tests expect exact English strings or key passthrough semantics for unknown keys. Align your expectations or initialize the locale appropriately in your tests before assertions.
- UI/TUI tests
  - internal/ui tests validate network and fetch behaviors and typically run non-interactively. TUI-fullscreen behavior is only in the main program; tests should avoid invoking interactive TUI. If you add tests here, keep them deterministic and avoid sleeping on real network calls—mock or stub where possible.
- Database tests
  - internal/storage tests use SQLite (via GORM). If you encounter CGO issues locally, ensure your toolchain is set up (see CGO note above). To speed up, you can direct tests to use an in-memory DSN like ":memory:" when authoring new tests.

2.1 Demonstration: Adding and running a new test (verified now)
- Example file (temporary for demonstration; do not keep in VCS): pkg/config/guidelines_demo_test.go
  - Contents (essence): construct Config{Fonts: []string{"a","b"}} and assert GetFontsList() returns the same slice.
- Run the single test:
  - go test ./pkg/config -run TestGuidelinesDemo -v
  - Verified result: PASS (0.00s) on current repo state.
- Clean up: remove the demo test file after use to avoid polluting coverage or CI results.

3. Development Notes and Conventions
- Project layout
  - cmd/blocowallet: CLI entrypoint
  - internal/wallet: cryptographic operations, keystore import/validation, and wallet service (uses go-ethereum and KDFs). Many integration and debug-focused tests reside here; run selectively.
  - internal/storage: GORM repository (currently SQLite-only code path enabled).
  - internal/ui: Bubble Tea model and network fetch helpers used by the TUI.
  - pkg/config: Configuration loader (viper + embedded default_config.toml). Prefer using LoadConfig(appDir) and letting it materialize ~/.wallets/config.toml.
  - pkg/localization: Locale handling and crypto/keystore messages. Legacy Labels map exists for backward compatibility.
  - pkg/logger: Logging plumbing; main currently uses standard log to blocowallet.log.
- Coding style and quality
  - Follow standard Go formatting: go fmt ./...
  - Linting suggestions: go vet ./... and staticcheck ./... (if available locally) before submitting changes.
  - Keep tests deterministic: avoid real network calls and time-dependent flakiness. Use fixtures and explicit seeds for randomness.
  - Security and performance in tests: Argon2 parameters are intentionally small in default config for dev/test speed. Do not increase them in unit tests.
- Configuration ergonomics
  - Prefer environment variables for ad-hoc overrides during development (e.g., BLOCOWALLET_DATABASE_DSN=":memory:" go test ./internal/storage -v).
  - Values in default_config.toml use ~/ expansion; test code that relies on paths should avoid hard-coding absolute paths.
- Versioning in UI
  - cmd sets a semantic version in localization labels by inspecting build info (debug.ReadBuildInfo). If you need to surface versions in the TUI, route through localization.

Appendix: Quick commands
- Build: go build -o blocowallet ./cmd/blocowallet
- Run CLI: ./blocowallet (logs to blocowallet.log)
- Full tests: go test ./... (may include failing suites not under active work)
- Targeted tests: go test ./internal/storage ./pkg/config
- Coverage: go test -cover ./...
- Race: go test -race ./...
- Demo test (create locally, then remove):
  - echo 'package config; import "testing"; func TestGuidelinesDemo(t *testing.T){ cfg:=&Config{Fonts:[]string{"a","b"}}; got:=cfg.GetFontsList(); if len(got)!=2||got[0]!="a"||got[1]!="b"{ t.Fatalf("unexpected: %#v", got)}}' > pkg/config/guidelines_demo_test.go
  - go test ./pkg/config -run TestGuidelinesDemo -v
  - rm pkg/config/guidelines_demo_test.go
