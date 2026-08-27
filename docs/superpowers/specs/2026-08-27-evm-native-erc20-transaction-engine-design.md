# EVM Native and ERC-20 Transaction Engine Design

**Date:** 2026-08-27
**Status:** Proposed for implementation
**Scope:** Phase 4 vertical slice: native transfers, ERC-20 transfers, finite ERC-20 approvals, signing, broadcast, receipts, confirmations, and reorg detection

## 1. Purpose

This design introduces a deterministic EVM transaction engine for Bloco Wallet. The engine must ensure that the transaction shown to the user is exactly the transaction signed and broadcast. It supports:

- native EVM transfers;
- ERC-20 `transfer`;
- finite ERC-20 `approve`;
- legacy/EIP-155 and EIP-1559 transactions;
- mandatory simulation with optional trace enrichment;
- digest-bound, single-use approval;
- software signing through `WalletVault` and `SoftwareSigner`;
- idempotent broadcast;
- receipt, confirmation, and reorg tracking;
- serialized nonce reservation per account and chain.

The slice excludes permits, ERC-721, ERC-1155, arbitrary contract calls, blind signing, replacement/cancellation UX, WalletConnect, hardware signers, and message signing. Those features must use the same engine boundaries in later phases.

## 2. Security invariants

1. A plan is bound to one account, chain ID, provider session, nonce, fee model, gas limit, recipient, value, calldata, simulation block, metadata provenance, and warning set.
2. Any change to a bound field invalidates the plan and requires planning, simulation, preview, and approval again.
3. The preview is rendered from a frozen plan. Editable UI fields are never used after the plan is frozen.
4. An approval is single-use, expires, and is bound to both the frozen plan hash and EVM signing digest.
5. The signer receives only a typed transaction request with a non-empty bound `ApprovalID`.
6. The engine recovers the signer address and verifies account, chain, transaction digest, and transaction hash before broadcast.
7. Broadcast sends the exact signed bytes produced after approval. Retries reuse those bytes and never reconstruct or re-sign.
8. A provider cannot change the chain, nonce, gas, fees, calldata, or recipient after approval.
9. Simulation must be conclusive. Blind signing is unavailable in this slice.
10. Unlimited ERC-20 approvals are blocked by default.
11. No password, capability handle, private key, mnemonic, or decrypted secret is persisted or logged.
12. Remote RPC messages are converted to stable categories and codes; printable reflected credentials do not cross UI or log boundaries.

## 3. Architecture

The subsystem lives in `internal/evm` and uses small units with explicit interfaces.

### 3.1 Intent model

The intent model defines immutable requests:

- `NativeTransferIntent`;
- `ERC20TransferIntent`;
- `ERC20ApproveIntent`.

Every intent includes account ID, chain ID, recipient or spender, and an integer amount. Addresses are normalized to EIP-55 checksum form. Amounts use copied `big.Int` values in base units; floating-point values are prohibited.

### 3.2 Planner

The planner:

- obtains a serialized nonce reservation;
- detects EIP-1559 support and otherwise uses legacy/EIP-155;
- obtains bounded fee suggestions;
- resolves ERC-20 metadata on-chain;
- encodes ABI calldata locally;
- estimates gas;
- constructs a typed unsigned go-ethereum transaction;
- produces an `UnsignedPlan` without signing or broadcasting.

Fee, gas, nonce, and quantity responses use canonical EIP-1474 parsing and explicit upper bounds. Absurd but syntactically valid provider values are rejected by policy.

### 3.3 Simulator

Mandatory simulation consists of:

- `eth_call` against the exact planned sender, recipient, value, and calldata;
- `eth_estimateGas` for the same call;
- revert decoding for `Error(string)`, `Panic(uint256)`, and bounded unknown revert data.

Trace is optional. When the provider supports a bounded trace method, decoded state changes enrich the preview. Trace absence produces a provenance warning but does not weaken the mandatory call/gas requirement. An inconclusive call or gas estimate fails closed.

Simulation records the block number and hash used to evaluate the plan. If the block context, transaction fields, or provider chain identity changes before approval, the plan is stale.

### 3.4 Risk policy

The policy engine returns typed findings with severity and stable identifiers. Initial rules cover:

- unlimited ERC-20 approval: blocked;
- finite approval to a new spender: critical warning and reinforced confirmation;
- recipient or spender with no code where code is required: blocked;
- ERC-20 metadata or ABI response that is malformed or oversized: blocked;
- decoded method inconsistent with the requested intent: blocked;
- suspicious visual similarity to known local addresses: warning;
- new contract, proxy/delegatecall uncertainty, or unavailable trace: warning;
- simulation revert or inconclusive result: blocked;
- undecodable calldata: blocked in this typed slice.

Policies consume structured values rather than rendered strings.

### 3.5 Approval service

The approval service builds a canonical preview document from the frozen plan and simulation. It computes:

- `PlanHash`: hash of the canonical frozen plan;
- `TransactionDigest`: the EVM signer digest for the exact unsigned transaction;
- `ApprovalID`: a random, single-use identifier bound to account ID, plan hash, transaction digest, risk level, creation time, and expiry.

Approval states are `pending`, `consumed`, and `invalidated`. Consumption is transactional and exactly once. Reinforced confirmation does not create a second plan; it confirms the same approval and digest.

### 3.6 Signer adapter

The adapter translates an approved frozen plan into `wallet.SoftwareSigningRequest` with:

- account ID;
- `SigningPurposeTransaction`;
- validated chain ID;
- exact transaction digest;
- exact `ApprovalID`.

Password-per-transaction is the default. A private configuration option may allow a temporary `WalletVault` capability session with existing TTL and inactivity expiry. Session reuse never bypasses per-digest preview and approval.

After signing, the adapter verifies the recovered address before constructing the signed transaction.

### 3.7 Broadcaster and receipt tracker

The broadcaster:

- serializes the signed transaction once;
- computes the local transaction hash;
- submits the same bytes through `eth_sendRawTransaction`;
- rejects a mismatched provider hash;
- retries only the same signed payload;
- classifies already-known responses idempotently.

The receipt tracker polls through a validated gateway session, stores receipt status and block identity, and tracks confirmations. The default confirmation target is 12 and is configurable per network with a minimum of one.

If a previously observed receipt disappears or its block hash changes, the transaction enters `reorged` and tracking resumes. Reorg handling never triggers automatic reconstruction or signing.

### 3.8 Nonce coordinator

Reservations are serialized by `(accountID, chainID)`. The coordinator:

- reconciles against `eth_getTransactionCount(address, "pending")`;
- reserves the next available nonce transactionally;
- prevents concurrent duplicate reservations;
- links a reservation to the frozen plan generation;
- invalidates unsigned reservations after cancellation or expiry;
- preserves signed/broadcast reservations until reconciliation proves they are replaceable or finalized.

A failed broadcast does not make a signed nonce available for a different transaction.

### 3.9 Repository

The repository extends the existing private SQLite storage with transactional APIs and migrations for:

- `evm_nonce_reservations`;
- `evm_approvals`;
- `evm_transactions`;
- optional bounded ERC-20 metadata cache.

The repository never stores unlock material. Signed payloads are retained only after the first broadcast attempt, protected against modification, and used exclusively for idempotent retry of the same transaction.

### 3.10 Orchestrator and TUI

A single orchestrator coordinates plan, simulation, risk evaluation, approval, signing, broadcast, and tracking. Network access remains exclusively behind `RPCGateway`.

The TUI performs each online step in cancellable Bubble Tea commands with component and operation generations. `View()` renders model snapshots and performs no RPC, filesystem, clock, or state mutation.

## 4. Data flow

1. The user enters a typed intent.
2. The orchestrator validates capability, chain, addresses, asset, and amount.
3. ERC-20 metadata is queried through bounded `eth_call`; contract address and provenance remain visible.
4. The nonce coordinator reserves a nonce.
5. The planner chooses the fee model, fees, gas, and canonical transaction.
6. Mandatory simulation runs; optional trace may enrich the result.
7. The risk policy blocks or annotates the plan.
8. The engine freezes the plan and computes its hash and signer digest.
9. The TUI renders the canonical preview.
10. The user confirms; critical warnings require a second confirmation of the same approval.
11. The approval service issues a single-use `ApprovalID`.
12. The configured password/session policy obtains a capability handle.
13. `SoftwareSigner` signs the approved digest.
14. The engine verifies the signature and serializes the signed transaction.
15. The broadcaster sends the exact bytes and verifies the returned hash.
16. The tracker follows receipt, confirmations, revert, and reorg state.

Cancellation before signing invalidates approval and reconciles the unsigned reservation. Cancellation after signing preserves the signed transaction record for explicit idempotent retry.

## 5. Canonical preview

The preview contains:

- chain name and currently validated chain ID;
- provider identity and simulation block number/hash;
- checksummed sender and recipient/spender;
- address context available from local accounts;
- operation and decoded selector;
- native asset or ERC-20 contract;
- token name, symbol, decimals, and on-chain provenance;
- human amount and raw integer amount;
- nonce and transaction type;
- gas limit;
- EIP-1559 max fee and priority fee, or legacy gas price;
- maximum gas cost and total maximum debit;
- calldata in bounded form;
- simulation result and decoded revert;
- optional trace/state changes and provenance;
- structured risk findings;
- fiat estimate only when a configured, identified source is available;
- abbreviated plan hash, transaction digest, and approval expiry.

The preview renderer cannot alter the plan.

## 6. Configuration

Private configuration adds:

- authorization mode: password per transaction by default, optional temporary session;
- confirmation target per network, default 12 and minimum one;
- bounded fee and gas policy limits;
- unlimited approval policy, disabled by default and not user-overridable in this slice;
- optional trace capability policy.

Credential-bearing provider settings continue to use credential references. No transaction policy may weaken RPCGateway destination policy.

## 7. Recovery and idempotency

At startup:

- unsigned stale reservations are reconciled against pending nonce and invalidated;
- consumed approvals remain consumed;
- expired approvals are invalidated;
- signed/broadcast transactions resume receipt polling;
- stored signed bytes may be rebroadcast unchanged after explicit user action;
- receipts are checked against canonical block hash and confirmation depth;
- reorged transactions return to tracking.

The engine never reconstructs an approved transaction from mutable UI or provider data during recovery.

## 8. Error model

Errors use typed categories:

- `InvalidIntent`;
- `PolicyDenied`;
- `SimulationFailed`;
- `ProviderUnavailable`;
- `PlanStale`;
- `ApprovalExpired`;
- `ApprovalConsumed`;
- `NonceConflict`;
- `SigningFailed`;
- `BroadcastRejected`;
- `ReceiptReverted`;
- `ReorgDetected`.

Errors exposed to UI contain stable categories and bounded local context. Raw remote messages and credential-bearing URLs are excluded.

## 9. Implementation slices

1. **Foundation:** canonical types, errors, repository migrations, RPC adapter, nonce reservations, and approval primitives.
2. **Native transfer:** planning, simulation, preview, approval, signing, broadcast, and tracking.
3. **ERC-20 transfer:** metadata, ABI encoding, simulation, token preview, and tests.
4. **ERC-20 approve:** finite approvals, spender risk policy, reinforced confirmation, and unlimited-approval denial.
5. **Receipt/reorg hardening:** confirmation progress, restart recovery, idempotent retries, and reorg scenarios.
6. **TUI completion:** cancellable input/preview/password/result flows and terminal-safety gates.

Each slice must pass its unit, race, integration, production, and security gates before the next slice begins.

## 10. Testing and acceptance gates

### 10.1 Unit and property tests

- known legacy/EIP-155 and EIP-1559 signing vectors;
- ERC-20 ABI vectors for `transfer` and `approve`;
- canonical amount/decimals conversion without floating point;
- deterministic plan and preview hashes;
- approval expiry, invalidation, and exactly-once consumption;
- nonce reservation concurrency and crash reconciliation;
- signature recovery and wrong-account rejection;
- fee, gas, and quantity bounds;
- revert and trace parsing bounds;
- unlimited approval denial.

Fuzz targets cover quantity parsing, calldata/revert decoding, canonical serialization, RLP/typed transaction handling, and preview rendering.

### 10.2 Hermetic integration tests

Scripted local JSON-RPC servers run through `RPCGateway` and cover:

- native success, revert, and insufficient funds;
- EIP-1559 and legacy fallback;
- ERC-20 transfer and finite approve;
- chain equivocation;
- nonce races;
- malformed or absurd fee/gas responses;
- provider hash mismatch;
- remote credential reflection;
- receipt success/revert;
- confirmation progress and reorg;
- cancellation and stale asynchronous results;
- idempotent rebroadcast of identical bytes.

Anvil integration may exist behind an optional build tag but is not required by the standard suite.

### 10.3 Required gates

- `go test -race -shuffle=on ./...`;
- production-tag tests;
- Linux/macOS/Windows build coverage for portable code;
- lint, vet, formatting, and diff checks;
- no outbound network transport outside `RPCGateway`;
- no network or filesystem I/O and no model mutation in `View()`;
- no secrets in logs, errors, config, transaction tables, or terminal output;
- at least 90% statement coverage for planner, approval policy, canonical serialization, and transaction signing/broadcast policy;
- regression proving previewed transaction bytes, signed digest, recovered signer, broadcast bytes, and local/provider transaction hash all match.

## 11. Success criteria

The slice is complete when a user can safely create, simulate, preview, explicitly approve, sign, broadcast, and track:

- a native transfer;
- an ERC-20 transfer;
- a finite ERC-20 approval;

on both EIP-1559 and legacy EVM networks, with serialized nonce handling, fail-closed simulation, single-use digest-bound approval, idempotent broadcast, receipt/confirmation tracking, and reorg detection. Unlimited approvals, blind signing, synthetic data, silent transaction mutation, and secret persistence remain impossible by default.
