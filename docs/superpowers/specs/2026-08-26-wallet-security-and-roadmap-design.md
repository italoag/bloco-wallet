# Wallet Security and Roadmap Design

Date: 2026-08-26
Status: Approved design, pending final user review

## 1. Intent

This design replaces the unsafe custody and import model in Bloco Wallet and expands the product into a secure EVM wallet with local software keys, hardware and external signers, FIDO2 approvals, WalletConnect, multisig, import/export, transactions, backups, and verifiable releases.

The work is delivered as vertical milestones. Each milestone leaves enabled paths complete and usable. Incomplete functionality remains disabled and is never represented by mock data or documentation claims.

There are no current users whose database or encrypted records must remain compatible. The implementation may replace the schema, secret format, storage layout, and internal interfaces. It must not silently modify or delete files in an existing user directory.

## 2. Approved decisions

- Delivery approach: secure foundation through vertical milestones, not a big-bang rewrite.
- Custody model: hybrid signer model.
- Signer types: local software, watch-only, Ledger, Trezor, HashiCorp Vault-compatible endpoints, AWS KMS/CloudHSM, Azure Key Vault/Managed HSM, and multisig.
- Strong second factor: FIDO2 with user presence or verification. Local TOTP may only be described as an application access gate, not cryptographic protection of a local private key.
- Transaction scope: complete EVM support, including native assets, ERC-20, ERC-721, ERC-1155, contract calls, EIP-191, EIP-712, WalletConnect, history, and analytics.
- Runtime architecture: TUI plus an optional private local daemon for WalletConnect, WebAuthn, and persistent tasks.
- External integration assurance: contract tests are mandatory; live tests are opt-in. An adapter is not called live-verified until a real device or provider sandbox passes.
- Multisig: use an audited protocol such as Safe/EIP-1271. Do not create a custom multisig smart contract.
- Internal software-key storage: encrypted blobs in SQLite rather than managed keystore files named by address.
- Database adapter: direct `database/sql` repository backed by a vetted pure-Go SQLite driver, replacing the current GORM/CGO coupling. The dependency version must be pinned to a release at least seven days old when introduced.

## 3. Scope

### 3.1 Included

- Repair every confirmed P0/P1 security and correctness finding from the 2026-08-26 audit.
- Replace mnemonic encryption and source hashing.
- Replace address-derived file identity and non-transactional filesystem/database workflows.
- Make all tests hermetic and remove secret output.
- Strict, bounded, restart-safe import/export.
- Complete BIP39 handling and configurable EVM derivation.
- Secure RPC and ChainList access.
- Secure EVM transaction engine.
- FIDO2/WebAuthn approvals.
- WalletConnect v2 through the local daemon.
- Software, hardware, cloud, watch-only, and multisig signer adapters.
- Backup, restore, password rotation, health checks, audit metadata, and signed updates.
- CI, release, SBOM, provenance, signing, dependency scanning, and private vulnerability disclosure.

### 3.2 Explicit non-goals

- No custom cryptographic primitives.
- No custom multisig smart-contract bytecode.
- No cloud-hosted custody service operated by this project.
- No promise of forensic secure deletion from SSD media. The product recommends full-disk encryption and accurately documents SQLite/WAL and filesystem limits.
- No silent fallback to a mock balance, alternate network, alternate signer, default KDF parameters, or software key.
- No compatibility promise for the current development database, custom XOR mnemonic envelope, or address-named files.

## 4. Threat model

### 4.1 Protected assets

- Mnemonics and BIP39 passphrases.
- Raw private keys and derived signing material.
- Keystore passwords and vault unlock credentials.
- FIDO2 registrations and approval challenges.
- Cloud provider credentials and RPC API tokens.
- Unsigned and signed transactions.
- WalletConnect sessions and local-daemon capability tokens.
- Backup archives and update manifests.
- Integrity of address, chain, nonce, value, fee, calldata, and signing intent.

### 4.2 Adversaries and failures

- Malicious or malformed import files.
- Malicious RPC, ChainList, dApp, WalletConnect peer, ABI metadata, filename, or terminal text.
- Other local users on a multi-user host.
- Another process under the same UID.
- DNS rebinding, redirects, private-network targets, large responses, and provider equivocation.
- Compromised CI action, mutable toolchain, registry tag, or release channel.
- Crashes, power loss, cancellation races, concurrent application instances, and partial writes.
- Shoulder surfing, terminal recording, crash dumps, swap, and residual heap copies.
- User mistakes involving derivation paths, passphrases, fees, approvals, backup words, or wrong networks.

### 4.3 Assurance boundary

The application cannot protect a local software key from a fully compromised process running as the same user while the key is unlocked. It minimizes exposure, supports hardware/external signers, uses FIDO2 approval, and never markets local TOTP as protection against host compromise.

## 5. Architecture

```text
TUI -------------------+
Local daemon ----------+--> Application --> ApprovalPolicy --> Signer
WalletConnect ---------+         |                |              |- SoftwareSigner
                                |                `- FIDO2       |- LedgerSigner
                                |                               |- TrezorSigner
                                |- WalletVault                 |- CloudSigner
                                |- ImportExport                |- MultisigSigner
                                |- TransactionEngine           `- WatchOnlySigner
                                |- RPCGateway
                                `- WalletRepository
```

### 5.1 `WalletVault`

The module owns all local secret lifecycle behavior behind one interface:

- create a software account;
- import a mnemonic or raw private key;
- unlock into a short-lived signing handle;
- lock and invalidate handles;
- rotate storage password and KDF parameters;
- export under explicit policy;
- delete encrypted material by account ID;
- verify and restore encrypted backups.

Callers never receive a raw private key. Tests exercise the same interface as production callers.

### 5.2 `Signer`

The signer interface accepts a typed signing request and returns a typed result. It exposes identity, address, capabilities, and health. It does not expose `SignHash` as the primary public interface because a digest alone hides intent from policy and user confirmation.

Required capabilities include transaction signing, EIP-191 message signing, EIP-712 typed-data signing, account discovery, and availability. Watch-only signers report no signing capabilities. Adapters may internally reduce an approved request to a digest only after the transaction engine and approval policy bind the displayed intent.

### 5.3 `ImportExport`

The module detects format by bounded content, strictly parses it, authenticates it, canonicalizes mnemonic/path/password semantics, derives the expected account, resolves conflicts, and commits through `WalletVault`.

An import result is successful only after the newly stored record is reopened and yields the same public key and address. Accepted external formats are converted to the canonical internal envelope. The source file is never modified.

### 5.4 `WalletRepository`

The repository uses `database/sql` with a pure-Go SQLite adapter. It stores account metadata, signer references, encrypted local-secret blobs, FIDO2 public metadata, transaction metadata, WalletConnect sessions, and audit events.

It configures one controlled write path, busy timeout, foreign keys, an explicitly selected journal and synchronous mode, integrity checks, pagination, and protected sidecars. The application directory is `0700`; database, sidecars, config, and logs are `0600` on Unix, with equivalent Windows ACLs.

### 5.5 `TransactionEngine`

The engine owns intent construction, chain validation, nonce and fee handling, simulation, decoding, policy checks, approval, signing, broadcast, receipt tracking, reorg handling, replacement, and cancellation. Every source, including WalletConnect, crosses this same interface.

### 5.6 `RPCGateway`

The gateway is the only module that creates HTTP, WebSocket, or JSON-RPC connections. It applies destination policy, TLS policy, redirect policy, DNS/IP validation, body and fan-out limits, timeouts, cancellation, rate limits, chain identity validation, provider health, and privacy metadata.

### 5.7 `ApprovalPolicy`

The module converts signing intent and configured risk policy into required approvals. It supports explicit TUI confirmation and FIDO2 presence/verification. It records non-secret approval metadata that is cryptographically bound to the request digest, chain ID, account ID, and expiry.

### 5.8 TUI and daemon

The TUI uses typed states. Password, mnemonic, private-key, and path fields are never reused for another purpose. `View()` is pure and performs no network or filesystem I/O.

The daemon uses a user-private Unix socket or Windows named pipe by default. A loopback listener exists only for WebAuthn/browser interactions and uses fixed origin rules, a random single-use token, CSRF protection, short expiry, and no wildcard binding.

## 6. Domain model

### 6.1 Account

An account record includes:

- stable random `account_id`;
- checksummed EVM address;
- signer kind and signer reference;
- derivation scheme, path, account/change/address indexes, and BIP39 language when applicable;
- capabilities;
- state: pending backup, active, locked, unavailable, or tombstoned;
- encrypted secret envelope for software accounts only;
- timestamps and non-secret policy metadata.

Address alone is not a storage identity. Multiple signer references for the same address are allowed only when each has a unique account ID and an explicit user-visible relationship. No encrypted blob or exported file is shared implicitly.

### 6.2 Secret envelope

The version-1 envelope uses:

- Argon2id for password-based key derivation;
- XChaCha20-Poly1305 for authenticated encryption;
- random salt and nonce from `crypto/rand`;
- serialized KDF algorithm and parameters;
- a length-prefixed AAD encoding of version, account ID, secret type, address, and derivation metadata;
- ciphertext containing the minimal canonical secret material.

KDF defaults are calibrated within a bounded production policy and stored per envelope. Test policies are injected only from test code and are not compiled as production defaults. Configuration changes do not change how an existing envelope is decrypted.

Passwords are exact UTF-8 byte sequences. The application never trims them. New encryption requires confirmation and a minimum of 15 characters while allowing long passphrases and all printable Unicode. Import authentication accepts the external password exactly and can request a separate new storage password.

A BIP39 passphrase is a distinct field and is normalized according to BIP39. It is never conflated with the storage password.

### 6.3 Audit metadata

Audit records contain operation ID, account ID, operation category, signer kind, chain ID, result category, duration, and timestamp. They never contain secrets, complete RPC URLs, calldata unless explicitly non-sensitive, transaction signatures, authentication challenges, DSNs, or provider tokens.

## 7. Security invariants

1. The mnemonic shown for backup derives exactly the persisted address and signing key.
2. Backup confirmation is required before normal use of a newly generated software account.
3. No plaintext mnemonic or private key is persisted or displayed by default.
4. No account import overwrites another account or shares storage implicitly.
5. Import success implies restart, unlock, and export success for the canonical account.
6. Password bytes are preserved exactly; BIP39 passphrase is separate.
7. Cancellation acknowledged before commit prevents every later commit for that operation.
8. Signing is bound to validated chain identity and the intent shown to the user.
9. No synthetic value is represented as real wallet data.
10. Untrusted input is bounded and sanitized before terminal rendering.
11. Tests never read or write the real user home and never print secrets.
12. A promoted release is the exact artifact that passed tests and was signed and attested.
13. External adapters are described according to measured assurance: compiled, contract-tested, or live-verified.
14. Failures never fall back to another network, signer, key, parser interpretation, or KDF default.

## 8. Core flows

### 8.1 Create

1. `WalletVault.Create` generates entropy and derives the selected account/path.
2. It creates an encrypted envelope and pending record in one transaction.
3. It reopens the envelope and confirms the public key and address before commit.
4. It returns backup words through a short-lived backup challenge, not a private-key object.
5. The TUI displays the words once and requests randomly selected words.
6. Successful confirmation activates the account and clears backup state.
7. Failure or cancellation removes the pending record. A user may explicitly restart backup confirmation, but the application does not silently reveal an active account's mnemonic.

### 8.2 Import

1. Open a regular file or bounded input stream without following symlinks.
2. Detect the format from content and parse strictly.
3. Calculate KDF cost and reject inputs above policy before execution; an explicit one-time override is separately audited.
4. Authenticate using exact password bytes.
5. Normalize BIP39 language, NFKD text, passphrase, and path where relevant.
6. Derive the account and show a preview of source type, address, path, and security conversion.
7. Resolve identity conflicts before storage.
8. Re-encrypt into the canonical envelope and commit.
9. Reopen and verify public key and address.
10. Return success without modifying the source.

Batch import applies file-count, depth, individual-size, total-size, CPU, memory, and elapsed-time budgets. It is idempotent per source identity and reports partial success explicitly.

### 8.3 Unlock and sign

`WalletVault.Unlock` returns a capability handle with TTL and inactivity expiry. The handle cannot cross daemon IPC. Lock, expiry, process shutdown, or policy change invalidates it.

The transaction engine validates network identity, builds and simulates intent, derives human-readable effects, checks policy, collects TUI and FIDO2 approval, and then calls the selected signer. Broadcast retries transmit exactly the signed bytes and do not reconstruct the transaction.

### 8.4 Delete

Delete accepts an account ID, loads the trusted record, and removes encrypted material in a database transaction while retaining a minimal tombstone when required for audit or duplicate prevention. It never deletes an arbitrary path supplied by a caller.

The product documents that logical deletion is not forensic erasure on SSD or from prior backups. Full-disk encryption is recommended.

### 8.5 Backup and restore

A backup contains a versioned encrypted vault, authenticated manifest, account metadata, and no plaintext secret. Restore occurs in staging, verifies every envelope and derived address, checks schema and application version compatibility, and only then atomically swaps the active vault. A failed restore cannot replace the active database.

### 8.6 Export

Secret export requires explicit destination, exact format, explicit new password where applicable, and a second confirmation. It uses a private temporary file, `0600`, sync, close, exclusive atomic rename, and parent-directory sync. It never overwrites an existing destination without a separate explicit replace flow.

## 9. BIP39 and account discovery

- Support 12, 15, 18, 21, and 24 words.
- Support the official BIP39 wordlists, defaulting to English.
- Apply NFKD to mnemonic and passphrase exactly as specified.
- Support standard EVM path `m/44'/60'/account'/change/index`.
- Preserve arbitrary validated EVM derivation paths for external wallets.
- Offer bounded account discovery with an explicit gap limit and provider privacy warning.
- Show the derived address before import commit.
- Store language, passphrase-present flag, and derivation metadata, but never the plaintext passphrase outside the encrypted envelope.

## 10. RPC and network policy

Remote RPC is HTTPS or WSS by default. Plain HTTP is allowed only for an explicitly enabled local-node policy and only after destination classification.

The transport:

- parses URLs strictly;
- rejects userinfo and redacts query/path credentials;
- resolves all A/AAAA records;
- blocks loopback, RFC1918, link-local, multicast, unspecified, metadata, IPv4-mapped IPv6, and rebinding unless local-node policy allows the exact target;
- pins the validated address for the connection;
- revalidates every redirect and rejects HTTPS downgrade;
- applies decompressed body limits and response-shape limits;
- limits concurrent probes and total endpoints;
- propagates context and cancellation;
- validates chain ID on the actual provider session;
- can optionally validate known genesis identity;
- never converts failure to mock data.

Provider metadata distinguishes registry presence, endpoint validation, current health, privacy/tracking status, and quorum confidence. No UI label says "verified" based only on a configuration-key prefix.

Balances are loaded asynchronously and cached for a bounded TTL. Opening wallet details does not automatically send an address to a hardcoded third party.

## 11. Transaction engine

### 11.1 Native and token operations

Support legacy, EIP-155, EIP-1559, native transfers, ERC-20 transfer/approve/permit where applicable, ERC-721 transfer/approval, and ERC-1155 single/batch transfer/approval.

Preview includes:

- chain name and validated chain ID;
- from/to/spender/operator addresses with checksum and address-book context;
- asset, token ID, amount, decimals, and fiat estimate when available;
- nonce, gas limit, max fee, priority fee, and maximum total cost;
- decoded method and state changes from simulation;
- risk warnings for unlimited approval, new contract, address poisoning similarity, delegatecall/proxy uncertainty, or undecodable calldata.

### 11.2 Contract calls and messages

Contract calls use trusted or user-supplied ABI metadata with provenance. Unknown calldata remains signable only under a policy that requires an explicit blind-signing warning.

EIP-191 and EIP-712 requests display domain, chain, verifying contract, primary type, every field, and any bytes that cannot be safely rendered. A dApp cannot present one value and submit another; approval binds to the canonical request digest.

### 11.3 Nonce, broadcast, and history

The engine serializes or reserves nonces per account/chain, reconciles pending state, handles replacement and cancellation, tracks receipts and confirmations, detects reorgs, and records transaction metadata without storing unlock secrets.

Retries are idempotent. Multiple providers may broadcast the same signed payload, but no provider may cause reconstruction or re-signing.

## 12. FIDO2 and WalletConnect

### 12.1 FIDO2/WebAuthn

FIDO2 registration stores credential ID, public key, RP metadata, transports, and counter. Authentication validates challenge, RP ID, origin, user presence, optional user verification, signature, expiry, and counter behavior.

The TUI can use direct FIDO2/CTAP2 where supported. Browser WebAuthn uses the local daemon's fixed loopback origin and single-use flow token. Recovery requires explicitly generated recovery material or another registered factor; it never disables policy silently.

### 12.2 WalletConnect

WalletConnect v2 sessions declare allowed chains, accounts, and methods. The user sees and approves session scope. Every transaction, message, and typed-data request passes through the transaction engine and approval policy. Sessions expire, can be revoked, and cannot auto-approve signing methods.

Relay/project credentials are referenced through a credential provider and are not written to config or logs.

## 13. Signer adapters

### 13.1 Software signer

Uses the local encrypted envelope and short-lived unlock handle. It cannot expose a raw key through the signer interface.

### 13.2 Watch-only signer

Stores public address and optional derivation metadata. Every signing capability returns a stable unsupported error.

### 13.3 Ledger and Trezor

Adapters discover accounts, validate expected path and displayed address, require on-device confirmation, and report device/app/version capabilities. Blind signing is disabled by default and cannot be enabled without an explicit policy warning.

### 13.4 Cloud and HSM

Adapters use provider-native credential chains and never store access keys in the vault config. They validate that the provider key supports secp256k1 and required signing semantics. If a provider or key type cannot produce an Ethereum-compatible signature, the adapter reports unsupported capability rather than emulating with a local key.

AWS KMS/CloudHSM, Azure Key Vault/Managed HSM, and Vault-compatible adapters each pass the shared signer contract suite. Live verification is opt-in and uses securely supplied sandbox credentials.

### 13.5 Multisig

The multisig adapter supports audited Safe/EIP-1271 workflows: discover or deploy through verified official factories, propose a transaction, collect owner signatures, inspect threshold and owners, and execute only after policy and on-chain state are revalidated. No custom multisig contract is authored.

## 14. Error model and logging

Stable categories:

- `InvalidInput`;
- `AuthenticationFailed`;
- `PolicyDenied`;
- `Conflict`;
- `ResourceLimit`;
- `Unavailable`;
- `CorruptVault`;
- `Unsupported`;
- `Internal`.

User-facing errors omit secret context. Logs redact userinfo, tokens, query values, DSNs, credential identifiers when sensitive, paths when unnecessary, passwords, mnemonics, private keys, derived keys, signatures, FIDO2 challenges, and WalletConnect secrets.

Input-controlled strings are normalized for length and stripped or escaped for C0/C1 controls, ESC, CSI, OSC, BEL, carriage return, and unexpected newlines before terminal rendering. Tests cover OSC8 and OSC52.

A panic caused by untrusted input is a release-blocking defect.

## 15. Delivery milestones

### Phase 0: Containment and proof

- Isolate the full test suite.
- Remove secret output and debug generators from normal tests.
- Add failing regressions for displayed mnemonic mismatch, same-address overwrite, popup wiring, channel reuse, permissions, and import/reload divergence.
- Add independent vectors and fuzz targets.
- Correct the mnemonic creation flow and disable unsafe paths until replaced.

### Phase 1: Secure local custody

- Introduce domain model, repository, envelope, vault, and software signer.
- Add password rotation, auto-lock, backup confirmation, private storage, and atomic export.
- Remove XOR encryption, direct secret hashes, address-based files, and private-key returns.

### Phase 2: Interoperable import/export

- Complete BIP39, path handling, strict Keystore V3, private-key import, safe batch import, password UI, canonical conversion, and independent interoperability tests.

### Phase 3: Network, configuration, and terminal

- Add RPC gateway, safe ChainList handling, asynchronous balances, atomic config, credential references, ANSI/OSC sanitization, accurate metadata, and privacy controls.

### Phase 4: EVM transaction engine

- Add native/ERC-20 transactions, legacy/EIP-1559, simulation, nonce, fees, previews, approvals, signing, broadcast, receipt/reorg handling, and poisoning defenses.

### Phase 5: Advanced EVM and dApps

- Add ERC-721/1155, contract calls, ABI decoding, EIP-191, EIP-712, watch-only mode, history, analytics, and approval-risk management.

### Phase 6: Daemon, FIDO2, and WalletConnect

- Add private daemon IPC, WebAuthn/CTAP2, WalletConnect v2 sessions, QR flow, lifecycle, revocation, and shared approval routing.

### Phase 7: External signers and multisig

- Add Ledger, Trezor, Vault-compatible, AWS, Azure, HSM, and Safe/EIP-1271 adapters with mandatory contract tests and opt-in live tests.

### Phase 8: Distribution and operations

- Finish pure-Go SQLite release path, real artifact smoke tests, pinned actions/images/toolchains, least privilege, blocking security scans, secret scanning, SBOM, provenance, signatures, backup/restore health checks, signed updates, and private disclosure policy.

Phases 6 and 7 may proceed in parallel after the signer, approval, and transaction interfaces stabilize. Phase 8 continuously applies to preceding phases and closes only after all enabled functionality passes release gates.

## 16. Verification gates

### 16.1 Global gates

- Tests use temporary HOME, XDG, database, sockets, and keystores.
- A guard test fails on any write outside the sandbox.
- No secret appears in test output or artifacts.
- `go test -race -count=1 -shuffle=on ./...` passes.
- The strong production crypto test mode passes without compiling weak defaults into the application.
- Fuzz targets assert error-not-panic behavior under bounded resources.
- `go vet`, golangci-lint, govulncheck, gosec, Semgrep, Trivy, secret scanning, actionlint, and workflow security lint pass under documented policy.
- Critical modules (`vault`, `importexport`, `transaction`, `rpcpolicy`, `approval`) maintain at least 90% statement coverage with explicit error-path tests.
- Linux, macOS, and Windows build and run a database/vault smoke test using the exact release artifact.
- No unexpected working-tree artifacts remain after verification.

### 16.2 Custody gates

- Displayed mnemonic derives the persisted account.
- Official independent BIP vectors pass.
- Header, ciphertext, nonce, salt, and tag corruption fail authenticated.
- Changed defaults do not break old envelopes.
- Rotation preserves identity and rejects the old password.
- Lock, TTL, and cancellation invalidate handles.
- Same-address imports never overwrite or share blobs.

### 16.3 Import/export gates

- Official Web3 Secret Storage vectors and independent geth, ethers/keythereum, and web3j fixtures complete import, restart, unlock, export, and external decrypt.
- BIP39 passphrase, NFKD, language, and path vectors pass.
- Empty and whitespace-bound external passwords are preserved exactly.
- Oversized files, FIFOs, symlinks, malformed values, and excessive KDF costs fail before expensive work.
- Batch operations are bounded, idempotent, cancelable, and honest about partial success.

### 16.4 Network and terminal gates

- Loopback, RFC1918, link-local, metadata, IPv4-mapped IPv6, alternate IP encodings, DNS rebinding, redirects, and HTTPS downgrade are tested.
- Compressed and chunked response limits are tested.
- Endpoint fan-out and concurrent requests stay within budget.
- Chain mismatch fails closed.
- CSI, OSC8, OSC52, BEL, CRLF, and control characters never reach the terminal.
- TUI redraw performs no I/O and no error becomes synthetic wallet data.

### 16.5 Transaction gates

- Golden vectors cover legacy, EIP-155, EIP-1559, ERC-20, ERC-721, ERC-1155, EIP-191, and EIP-712.
- Concurrent nonce reservation, replacement, cancellation, reorg, fee bounds, and idempotent retry pass.
- The signature is bound to the chain and exact approved preview.
- Unlimited approvals, address poisoning, unknown calldata, and failed simulation exercise policy warnings.
- WalletConnect cannot bypass approval.

### 16.6 Signer and FIDO2 gates

- Every adapter passes the shared signer contract suite.
- Software signer does not expose key material.
- Watch-only cannot sign.
- Hardware/cloud failure does not fall back.
- FIDO2 validates RP ID, origin, challenge, presence/verification, counter, and expiry.
- Live-provider claims require opt-in integration evidence.

### 16.7 Release gates

- Corrupt or wrong backups cannot replace the active vault.
- Restore verifies every account before activation.
- The tested artifact is promoted without rebuild.
- Binary, image, checksum manifest, SBOM, and provenance verify their signatures.
- Release remains draft if any required gate fails.
- README and SECURITY claims are generated or reviewed against measured capabilities.

## 17. Immediate regression gates

Before the first functional change is accepted, these tests must first fail on the current code and then pass:

```text
DisplayedMnemonicControlsPersistedAccount
SameAddressImportNeverOverwritesExistingWallet
TestsNeverWriteOutsideSandbox
```

The existing test that wrote to `~/.wallets/keystore` must be isolated before any broad test command is run again.

## 18. Completion definition

The program is complete when:

- phases 0 through 8 are closed;
- no confirmed P0/P1 finding remains open;
- every enabled feature passes its domain gates;
- external adapters are labeled according to their measured assurance;
- no approved requirement is hidden behind a TODO, skip, disabled build tag, ignored exit code, or documentation-only claim;
- an independent final review of the complete diff finds no unresolved high-severity concern;
- a signed release candidate passes install, create, import, restore, sign, broadcast-on-testnet, lock, and update verification on supported platforms.

## 19. Repository safety note

The 2026-08-26 audit executed the current fast suite before discovering that an active test writes to the real `~/.wallets/keystore`. Eighteen files had modification times during that test run and mode `0644`. They were not deleted because there was no prior snapshot proving which files were newly created. Cleanup is a separate user-approved operation and is not part of implementation commits.
