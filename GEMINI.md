## Project Overview

This project is a Go-based command-line interface (CLI) application named "BLOCO Wallet Manager" for managing cryptocurrency wallets. It provides a Terminal User Interface (TUI) for wallet management, focusing on Ethereum-compatible wallets and the KeyStoreV3 standard.

**Main Technologies:**

*   **Programming Language:** Go
*   **TUI Framework:** [Bubble Tea](https://github.com/charmbracelet/bubbletea)
*   **Configuration:** [Viper](https://github.com/spf13/viper)
*   **Database:** [GORM](https://gorm.io/) with SQLite
*   **Ethereum Interaction:** [go-ethereum](https://github.com/ethereum/go-ethereum)

**Architecture:**

The application follows a modular structure, with clear separation of concerns:

*   `cmd/blocowallet`: Main application entry point.
*   `internal/ui`: Contains the Bubble Tea TUI components and logic.
*   `internal/wallet`: Core wallet management logic, including cryptographic functions and keystore interactions.
*   `internal/storage`: GORM-based repository for wallet data persistence.
*   `pkg/config`: Configuration management using Viper.
*   `pkg/logger`: Logging setup.
*   `pkg/localization`: Internationalization and localization.

## Building and Running

The project uses a `Makefile` for common development tasks.

**Build:**

To build the application for the current platform, run:

```sh
make build
```

The binary will be located at `build/bloco-wallet`.

**Run:**

To run the application, execute the built binary:

```sh
./build/bloco-wallet
```

**Test:**

To run the test suite, use the following command:

```sh
make test
```

There are also other test targets available, such as `test-fast` for a quicker development cycle and `test-production` for more thorough testing.

**Lint:**

To run the linter, use:

```sh
make lint
```

This requires `golangci-lint` to be installed. If not present, it falls back to `go vet`.

## Development Conventions

*   **Code Style:** The project follows standard Go formatting, which can be applied using `make fmt`.
*   **Testing:** The project has a comprehensive test suite. New features should be accompanied by corresponding tests. The `testing` package and `testify` are used for assertions.
*   **Dependency Management:** Go modules are used for dependency management. Use `make mod` to tidy and download modules.
*   **Configuration:** Configuration is handled through a `config.toml` file, managed by the Viper library.
*   **Internationalization:** The `go-i18n` library is used for localization. Language files are located in the `pkg/localization` directory.
