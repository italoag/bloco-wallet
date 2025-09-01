# BLOCO Wallet Manager – Development Guidelines (Project‑Specific)

This document captures project‑specific build, test, and development practices verified on 2025‑09‑01. It is intended for experienced Go developers working on this codebase.


## 1) Build and Configuration

- Module and entrypoints
  - Module: `blocowallet`
  - Primary CLI: `cmd/blocowallet`
  - Additional demos: `cmd/progress_demo` (dev/demo only)

- Make targets (preferred)
  - `make build` – Builds the CLI for the host platform with version metadata injection (ldflags). CGO is enabled by default.
  - `make build-all` / `build-<os>` – Cross‑compiles release artifacts (uses matrix defined in Makefile). Intended for CI/release.
  - `make info` – Prints the effective build environment (useful when debugging local toolchain issues).

- CGO and SQLite
  - Storage uses GORM with `github.com/mattn/go-sqlite3` (indirect). This requires CGO for the normal build path.
  - macOS: harmless CGO linker warnings can appear; our Makefile sets `CGO_LDFLAGS="-Wl,-ld_classic"` automatically to reduce noise.
  - Linux/Windows: ensure a working C toolchain (gcc / TDM-GCC) for CGO.

- Static builds
  - The Makefile has a `build-static` target (CGO_DISABLED=0) intended for a pure‑Go SQLite driver; the repository does not currently vendor `modernc.org/sqlite`. Treat `build-static` as experimental until the driver swap is wired. Prefer `make build` for now.

- Version metadata
  - The binary embeds: `main.version`, `main.commit`, `main.date` via `-ldflags`. Use `make build` to ensure values are injected (see `GIT_REV`, `DATE` in Makefile).

- Configuration system (pkg/config)
  - Default config is embedded from `pkg/config/default_config.toml` and written to `<AppDir>/config.toml` if missing.
  - Environment variables are supported via Viper with prefix `BLOCO_WALLET`. Dots are converted to underscores.
    - Example: `BLOCO_WALLET_DATABASE_TYPE=sqlite` overrides `database.type`.
  - Legacy env vars with prefix `BLOCO_WALLET_` are also recognized for backward compatibility (see `LoadConfig` for the exact set).
  - App directories are resolved per‑OS (XDG on Linux, `~/Library/Application Support` on macOS, `%AppData%` on Windows). If `app.app_dir` is empty, a platform‑specific default is chosen.
  - Note: `default_config.toml` comments mention `wallets.db`, but runtime defaults in `config.go` currently resolve to `<app_dir>/bloco.db` if `database_path` is empty. Code wins.


## 2) Testing

- Overview
  - Tests are split across internal packages (`internal/wallet`, `internal/ui`, etc.) and pkg‑level code (`pkg/config`, `pkg/localization`, etc.). Some tests exercise crypto and UI flows and can be slow.
  - The Makefile provides curated targets that set sane defaults and mitigate macOS CGO noise.

- Quick commands
  - `make t` or `make test-fast` – Fast dev loop (`go test ./... -short -v`).
  - `make test` – Full test run with race detector (still optimized parameters).
  - `make test-production` – Security‑grade parameters via `-tags=production`; much slower, use pre‑release.
  - `make test-wallet` / `make test-ui` – Scope to wallet or UI packages.
  - `make cover` / `make bench` – Coverage and benchmarks.

- Parameters and build tags
  - Development runs prefer faster code paths (primarily via `-short`). Production/security validation uses `-tags=production` to enable heavy KDFs and longer paths.
  - Historical docs may mention scrypt; current implementation focuses on Argon2 parameters (see `pkg/config.SecurityConfig`). Use `-short` vs `-tags=production` as the effective switch.

- Platform specifics
  - macOS: CGO linker warnings are benign; Makefile already sets `CGO_LDFLAGS` to use the classic linker.
  - Ensure CGO toolchains are present (Xcode CLI tools on macOS; gcc on Linux; TDM‑GCC on Windows) if running database‑touching tests.

- Adding new tests
  - Place `_test.go` next to the code, same package unless you intentionally need black‑box tests with `package xxx_test`.
  - Prefer table‑driven tests; honor `testing.Short()` for heavy operations.
  - For security/crypto parametrization, gate expensive variants behind the `production` build tag and/or skip when `testing.Short()` is true.
  - Use `testify` (already a dependency) when helpful, but keep core logic in standard library if assertions are simple.

- Running subsets
  - Run a single package: `go test -v ./internal/wallet`
  - Run by pattern: `go test -run TestNamePattern ./internal/ui -v -short`
  - CI should prefer Makefile targets to ensure uniform flags and env.

- Simple demo test (verified locally)
  - We validated the test workflow by adding a temporary, pure‑Go example and running it:
    - File (temporary): `internal/devexample/demo.go`
      ```go
      package devexample
      func Add(a, b int) int { return a + b }
      ```
    - Test: `internal/devexample/demo_test.go`
      ```go
      package devexample
      import "testing"
      func TestAdd(t *testing.T) {
          if got := Add(2, 3); got != 5 { t.Fatalf("Add(2,3)=%d, want 5", got) }
      }
      ```
    - Command executed: `go test -v ./internal/devexample`
    - Result: PASS (confirmed).
  - The files above were removed after verification as they were only for demonstration.


## 3) Additional Development Notes

- Code layout
  - CLI (Bubble Tea TUI) under `internal/ui` and service logic under `internal/wallet` and `internal/blockchain`.
  - Configuration and localization live in `pkg/config` and `pkg/localization` respectively. Locales are TOML files in `pkg/localization/locales`.

- Logging
  - Uses `go.uber.org/zap`. Prefer structured logs in lower layers; UI paths should keep user‑facing messages localized.

- Localization
  - Implemented via `github.com/nicksnyder/go-i18n/v2` with TOML resources. Utilities in `pkg/localization` load `language.<lang>.toml` from the configured locale directory and will enumerate all `*.toml` files there.

- Makefile utilities
  - `make fmt`, `make vet`, `make lint` (uses `golangci-lint` if present). `make mod` to tidy/download modules.

- Releasing
  - Use `make build-all` then `make checksums` (or `make release-prep`). Container images are built with `docker-build`/`docker-push` using `docker buildx`.

- Known rough edges
  - Static builds are not fully wired for a pure‑Go SQLite driver; default path requires CGO. Prefer `make build` unless you plan the driver migration.
  - TESTING.md still references scrypt in places; the current configuration code uses Argon2 parameters. Follow the guidance in this document for test modes.


## 4) Quickstart (copy‑paste)

- Build (host platform):
  - `make build`
- Run CLI:
  - `./build/bloco-wallet` (or move into PATH)
- Fast tests during development:
  - `make t`
- Wallet‑only tests:
  - `make test-wallet`
- Full security validation:
  - `make test-production`


## 5) Environment Overrides (examples)

- Linux (XDG):
  - `BLOCO_WALLET_APP_APP_DIR="$XDG_DATA_HOME/bloco" make build`
- macOS:
  - `BLOCO_WALLET_DATABASE_TYPE=sqlite make test`
- Windows (PowerShell):
  - `$env:BLOCO_WALLET_APP_DATABASE_PATH = "$env:APPDATA\Bloco\bloco.db"; go test ./pkg/config -v`


## 6) How to add a new feature safely

1. Write or extend unit tests for the target package; honor `-short` for heavy/crypto paths.
2. If you need production‑grade KDFs or integration coverage, add `//go:build production` guarded tests/branches.
3. Run `make t`, then scope deeper with `make test-wallet` or `make test-ui`.
4. Run `make cover` or `make bench` when optimizing crypto/IO paths.
5. Build with `make build` and validate the TUI flows.


— End —
