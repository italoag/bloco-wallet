<!--suppress HtmlDeprecatedAttribute -->

<h1 align="center">🔐 BLOCO | Wallet Manager</h1>

<p align="center">
<a href="https://github.com/italoag/bloco-wallet-manager/releases" style="text-decoration: none">
<img src="https://img.shields.io/github/v/release/italoag/bloco-wallet-manager?style=flat-square" alt="Latest Release">
</a>

<a href="https://github.com/italoag/bloco-wallet-manager/stargazers" style="text-decoration: none">
<img src="https://img.shields.io/github/stars/italoag/bloco-wallet-manager.svg?style=flat-square" alt="Stars">
</a>

<a href="https://github.com/italoag/bloco-wallet-manager/fork" style="text-decoration: none">
<img src="https://img.shields.io/github/forks/italoag/bloco-wallet-manager.svg?style=flat-square" alt="Forks">
</a>

<a href="https://opensource.org/licenses/MIT" style="text-decoration: none">
<img src="https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square" alt="License: MIT">
</a>

<br/>

<a href="https://github.com/italoag/bloco-wallet-manager/releases" style="text-decoration: none">
<img src="https://img.shields.io/badge/platform-windows%20%7C%20macos%20%7C%20linux-informational?style=for-the-badge" alt="Downloads">
</a>

 <a href="https://twitter.com/0xitalo">
        <img src="https://img.shields.io/badge/Twitter-%400xitalo-1DA1F2?logo=twitter&style=for-the-badge" alt=""/>
    </a>

<br/>
<br/>

## SonarQube Quality Analysis
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=coverage)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Duplicated Lines (%)](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=duplicated_lines_density)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Lines of Code](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=ncloc)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Bugs](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=bugs)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Technical Debt](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=sqale_index)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)
[![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=italoag_bloco-wallet&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=italoag_bloco-wallet)

### Table of Contents
- [Introduction](#introduction)
- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)

### Introduction
**BLOCO Wallet Manager** is a command-line interface (CLI) application designed to manage cryptocurrency wallets compatible with the Ethereum network and adhering to the KeyStoreV3 standard. Developed in GoLang, BLOCO provides a Terminal User Interface (TUI) for seamless wallet management. Future integrations will include external vaults such as Hashicorp Vault, Amazon KMS, Cloud HSM, and Azure Key Vault.

### Features
- **Wallet Management**
    - Create new wallets compatible with Ethereum.
    - Import wallets using Mnemonics.
    - Import KeyStoreV3 wallets with any file extension or no extension.
    - **Enhanced keystore import** with multi-file selection and batch processing:
        - Multi-file selection with checkbox interface
        - Directory selection for batch import
        - Automatic password file detection (.pwd files)
        - Interactive file picker with keyboard navigation
        - JSON file filtering for keystore files
    - Export wallets in KeyStoreV3 format.
    - Delete, block, and unblock wallet addresses.
    - List all managed wallets.

- **Security**
    - Compatibility with KeyStoreV3 for secure key storage.
    - Planned integration with external vaults:
        - Hashicorp Vault
        - Amazon KMS
        - Cloud HSM
        - Azure Key Vault

- **Configuration Management**
    - Robust configuration file handling with atomic operations
    - Automatic backup and restore capabilities
    - Network configuration with proper TOML formatting
    - Safe editing of configuration files

- **Balance Inquiry**
    - Query the balance of Ethereum-compatible wallets.

- **Extensibility**
    - Support for additional blockchain networks (future).
    - Support for multiple cryptographic curves and signature algorithms:
        - Curves: secp256k1, secp256r1, ed25519
        - Algorithms: ECDSA, EdDSA

### Installation
Ensure you have [Go](https://golang.org/doc/install) installed on your system.

```bash
git clone https://github.com/italoag/bloco-wallet.git
cd bloco-wallet
go build -o bloco-wallet
```

Move the executable to a directory in your PATH for easy access:

```bash
mv bloco-wallet /usr/local/bin/
```

### Usage
Run the BLOCO Wallet using the terminal:

```bash
bloco-wallet
```

Navigate through the TUI to manage your wallets. Available commands include:

- **Create Wallet:** Initialize a new Ethereum-compatible wallet.
- **Import Wallet:** Import existing wallets using Mnemonics or KeyStore files.
- **Enhanced Keystore Import:** 
    - Select multiple keystore files or entire directories
    - Automatic detection of password files (.pwd)
    - Interactive file picker with keyboard navigation
    - Batch processing with progress tracking
- **List Wallets:** Display all managed wallets.

#### Enhanced Import Workflow

The enhanced keystore import feature provides an intuitive file selection interface:

1. **File Selection**: Use the enhanced file picker to navigate directories and select keystore files
2. **Multi-Selection**: Select multiple files using the space bar or entire directories
3. **Password Detection**: Automatically detects corresponding .pwd files for seamless import
4. **Password Input**: Secure modal popup for manual password entry when needed
5. **Batch Processing**: Import multiple keystores in a single operation with progress tracking

**Key Bindings for File Picker:**
- `↑`/`↓` or `j`/`k`: Navigate files
- `Space`: Toggle file selection
- `Enter`: Open directory or confirm selection
- `Tab`: Confirm final selection
- `Ctrl+A`: Select all files
- `Ctrl+C`: Clear selection
- `Esc`: Go back or cancel

**Password Input Features:**
- **Secure Input**: Password masking with bullet characters for security
- **Error Handling**: Clear error messages with retry mechanism
- **Skip Option**: Ability to skip individual files during batch import
- **Retry Limits**: Configurable maximum attempts to prevent brute force
- **Key Bindings**: `Enter` to confirm, `Esc` to cancel, `Ctrl+S` to skip file

**Import Progress Tracking:**
- **Real-time Progress**: Animated progress bar with percentage completion
- **Current File Display**: Shows which keystore is currently being processed
- **Error Categorization**: Distinguishes between failed imports and skipped files
- **Pause/Resume**: Progress pauses during password input and resumes automatically
- **Completion Summary**: Detailed statistics including success/failure/skip counts and timing
- **Error History**: Tracks and displays recent errors with context
- **Visual Status**: Clear indicators for importing, paused, and completed states

### Roadmap
**Upcoming Features:**

- **Vault Integrations:**
    - Hashicorp Vault
    - Amazon KMS
    - Cloud HSM
    - Azure Key Vault

- **Multi-Network Support:**
    - Integration with additional blockchain networks.

- **Advanced Cryptography:**
    - Support for secp256r1 and ed25519 curves.
    - Implementation of ECDSA and EdDSA signature algorithms.

- **Enhanced Security Features:**
    - **Import Wallet:** Import existing wallets using private keys.
    - Two-factor authentication for wallet access.
    - Multi-signature wallet support.

- **User Experience Improvements:**
    - Enhanced TUI with more intuitive navigation.
    - Detailed transaction histories and analytics.
    - Delete an account address.
    - **Delete Wallet:** Remove a wallet from the manager.
    - **Block/Unblock Wallet:** Temporarily disable or enable a wallet address.
    - **Check Balance:** View the balance of a selected wallet.

### Contributing
Contributions are welcome! Please follow these steps:

1. Fork the repository.
2. Create a new branch for your feature or bugfix.
3. Commit your changes with clear messages.
4. Submit a pull request detailing your changes.


### License
This project is licensed under the [MIT License](LICENSE).
