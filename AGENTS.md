# Repository Guidelines

## Project Structure & Module Organization
- `cmd/blocowallet/`: CLI entry point (version injected at build).
- `internal/`: Non-exported app code
  - `wallet/` (keystore, mnemonics), `ui/` (Bubble Tea TUI), `blockchain/`, `storage/` (GORM+SQLite).
- `pkg/`: Reusable packages — `config/` (TOML + env), `localization/`, `logger/`.
- `build/`, `dist/` (artifacts), `docs/` (design/testing), `fonts/` (assets).

## Build, Test, and Development Commands
- `make build`: Build current platform to `build/bloco-wallet`.
- `make build-all`: Cross-compile (Linux/macOS/Windows). Archives in `dist/`.
- `make t` / `make test-fast`: Fast tests (dev parameters).
- `make test`: Full suite with race detector (optimized params).
- `make test-production`: Tests with production crypto params (`-tags=production`).
- `make cover` / `make bench`: Coverage HTML and benchmarks.
- `make lint` / `make fmt` / `make vet`: Lint, format, vet.

Examples:
```bash
make deps && make build
CGO_LDFLAGS="-Wl,-ld_classic" make test   # macOS
go test ./... -v -tags=production         # direct Go
```

## Coding Style & Naming Conventions
- Language: Go 1.23+. Format with `go fmt`; lint with `golangci-lint` (via `make lint`).
- Indentation: tabs (Go default). Line length: keep readable; prefer small functions.
- Packages: lower-case short names; files use `snake_case.go`.
- Exported identifiers: `CamelCase`; unexported: `camelCase`. Errors use `error` values, wrap with context.
- Module imports: start with `blocowallet/...`.

## Testing Guidelines
- Framework: standard `testing` package; tests co-located as `*_test.go`.
- Modes: fast (default) vs production (`-tags=production`). See `TESTING.md`.
- Naming: `TestXxx`, short focused cases; add benches for crypto/IO hotspots.
- Minimum: ensure `make t`, `make lint` pass before PR; aim to keep/raise coverage.

## Commit & Pull Request Guidelines
- Commits: follow Conventional Commits (e.g., `feat:`, `fix:`, `chore:`) as seen in history.
- PRs: clear description, link issues, include screenshots/GIFs for TUI changes, list test scope.
- Checklist: `make t`/`make lint` green, update docs when flags/UI change, avoid committing keys/keystores.

## Security & Configuration Tips
- Do not commit secrets, private keys, or real keystores. Use `internal/wallet/testdata` only for tests.
- Config lives under the per-OS app dir (resolved by `pkg/config`); env prefix: `BLOCO_WALLET_` (e.g., `BLOCO_WALLET_APP_APP_DIR`).
- SQLite requires CGO; macOS linker warnings are handled via `CGO_LDFLAGS` in the Makefile.

