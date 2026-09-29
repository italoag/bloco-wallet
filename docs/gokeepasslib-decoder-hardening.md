# gokeepasslib decoder hardening: findings and local patch

## Status

This report documents a reproduced input-validation defect in `github.com/tobischo/gokeepasslib/v3` and the reviewed hardening patch. The library patch is published in the `italoag/gokeepasslib` fork as `257926a46375a14b21dde8a0b5f507fd0259ec5e` on `fix/bounded-kdbx-decoding`, using SSH. The Bloco working tree resolves the dependency to that pinned submodule. The verification record below describes the patch-only validation; current wallet-integration acceptance is tracked separately in `GATES.md`. The library patch itself does not implement wallet-facing KDBX functionality.

No issue, pull request, advisory, or patched commit has been published upstream. No CVE or severity rating has been assigned. Only synthetic inputs and upstream test fixtures are used. No production wallet, password database, or user credential was opened.

## Upstream version and scope

- Investigated release: `v3.7.0`.
- Release commit: `f0f28ea3afce0e769597b42ef04e9c49f4400925`.
- Release date: July 28, 2026.
- Repository: https://github.com/tobischo/gokeepasslib
- License: MIT; the upstream license and attribution must remain with the submodule.

The reviewed upstream commit `4fa52e497f7956e7c874da8f2b7a06f049627ccf` contains dependency updates after this release but does not change the decoder, header parser, or block parser discussed here. The executable reproduction was run against the release, not every historical release or fork. This report is not a comprehensive security audit of gokeepasslib, KeePass, or KeePassXC.

KeePassXC is a separate application used as an independent interoperability reference. An initial macOS first-run authorization problem prevented its CLI from starting; after the user authorized it, `keepassxc-cli --version` returned `2.7.12`. That environment issue was unrelated to the Go decoder defect.

## Reproduced defect: malformed compression header causes a panic

### Expected behavior

`Decoder.Decode` should reject malformed or truncated KDBX input with an error. A caller should not need a `recover` wrapper to handle an invalid file header.

### Observed behavior

A 17-byte input containing a KDBX 4.0 signature and a compression-flags field whose declared length is zero causes `runtime.boundsError` in `v3.7.0`.

The field is read by `FileHeaders.readHeader4`, then passed to `FileHeaders.readFileHeader`. The compression-flags case calls `binary.LittleEndian.Uint32(data)` without first requiring four bytes. At this point the decoder has not derived a key or authenticated the database header. The supplied password is irrelevant to reaching this failure.

Relevant source:

- https://github.com/tobischo/gokeepasslib/blob/v3.7.0/header.go
- https://github.com/tobischo/gokeepasslib/blob/v3.7.0/decoder.go

### Minimal reproduction

Run this only as a test with a synthetic input. It deliberately exercises an upstream panic; it does not open any file or allocate a large payload.

```go
package main

import (
    "bytes"

    keepass "github.com/tobischo/gokeepasslib/v3"
)

func main() {
    data := []byte{
        0x03, 0xd9, 0xa2, 0x9a,
        0x67, 0xfb, 0x4b, 0xb5,
        0x00, 0x00, 0x04, 0x00,
        0x03, 0x00, 0x00, 0x00, 0x00,
    }
    db := keepass.NewDatabase()
    db.Credentials = keepass.NewPasswordCredentials("synthetic-gate-password")
    _ = keepass.NewDecoder(bytes.NewReader(data)).Decode(db)
}
```

The isolated confirmation test caught the panic solely to record the observation. Its output included:

```text
Confirmed decoder panic on 17-byte synthetic header: runtime.boundsError
```

That confirmation test passed because it expected the defect. The patch regression test has the opposite contract: `Decode` must return an error without panicking. A production `recover` wrapper is not the proposed fix.

## Related findings from source review

These findings are separated from the executable reproduction above. Their regression-test results must be reported individually rather than presenting every source observation as an independently reproduced exploit.

### 1. Unchecked field lengths and block boundaries

- The outer header allocates buffers directly from declared field lengths before confirming that the input contains those bytes.
- Variant-dictionary name/value lengths and inner-header lengths are signed, but are passed to allocation without first rejecting negative values.
- Fixed-width KDF/header fields are converted with `Uint32`, `Uint64`, or fixed slices without consistently validating their lengths.
- Block readers slice input using offsets and declared lengths without checking the remaining byte count before allocation and slicing.

Malformed inputs can therefore reach panic paths or request disproportionate allocations. This report does not claim that a memory-exhaustion attack was executed; large allocations are deliberately avoided in tests.

### 2. Unbounded expansion and discarded IO errors

In `decoder.go`, the outer payload read discards its returned error. Gzip decompression uses an unbounded `io.ReadAll`, and its returned error is also discarded. Checking the size of the encrypted file alone does not bound the size of its decompressed content.

`Binary.GetContentBytes` similarly performs unbounded gzip expansion and accepts `io.ErrUnexpectedEOF` as if it were a successful result.

The consequences depend on the input and call site: expansion may consume excessive resources, and an otherwise parseable prefix may be accepted without preserving a decompressor's corruption/truncation error. For normal KDBX 4 decoding with hash validation enabled, payload expansion occurs after header/block authentication; this is not the same pre-authentication path as the 17-byte panic.

Relevant source:

- https://github.com/tobischo/gokeepasslib/blob/v3.7.0/decoder.go
- https://github.com/tobischo/gokeepasslib/blob/v3.7.0/binary.go

### 3. KDF validation before expensive work

KDF parameters originate in the file header. In the reviewed implementation, Argon2 parameters are narrowed to smaller integer types, and an unrecognized KDBX 4 KDF identifier falls into the AES branch rather than being explicitly rejected.

Validation must distinguish structural invalidity, unsupported algorithms/options, and caller-defined resource limits before deriving a key. A decode budget must not silently modify the parameters of an otherwise valid database: reject the operation instead.

Relevant source: https://github.com/tobischo/gokeepasslib/blob/v3.7.0/credentials.go

### 4. Cipher framing, integrity, and recursive content

Malformed CBC input must be rejected before invoking block-mode decryption, which requires a correctly sized IV and complete blocks. Decrypted framing must be checked before slicing start bytes or parsing inner headers.

The KDBX 3.1 block reader also needs to verify block indices and SHA-256 checksums rather than merely extracting the data. KDBX 4 HMAC checks, including the terminal block, must remain mandatory in the block reader.

XML byte limits alone do not bound nesting or token counts. A bounded token-validation pass is intended to run before recursive unmarshalling. Missing required document structures must be rejected before downstream code assumes they exist.

## Patch design

The patch keeps the KDBX format, cipher implementations, and existing public `NewDecoder(io.Reader)` signature. It does not introduce a wallet-specific encryption format.

Implemented changes:

1. Read encrypted input with a bounded reader and propagate IO errors.
2. Validate signatures, supported versions, required fields, scalar lengths, duplicate keys/fields, and declared lengths before use.
3. Read actual available field bytes rather than allocating an attacker-declared size immediately.
4. Validate KDF parameters and resource budgets before derivation; reject unknown KDFs instead of selecting AES implicitly.
5. Check block framing and integrity without unchecked offset arithmetic.
6. Validate cipher framing/padding, then bound decompression and propagate checksum/truncation errors.
7. Bound XML nesting and token counts before unmarshalling into recursive models.
8. Expose parsed database content only after the complete decode succeeds.
9. Provide a bounded attachment-extraction method, preserving existing convenience methods with default limits.
10. Add negative-input tests, existing-fixture regressions, bounded fuzzing, and a KeePassXC interoperability test using only synthetic credentials.

### Local API and defaults

The local API adds `DecodeLimits`, `DefaultDecodeLimits()`, and `NewDecoderWithLimits(io.Reader, DecodeLimits)`. Zero-valued individual fields in `DecodeLimits` select defaults. Invalid negative/overflowing limits are errors, not an unlimited mode. `Binary.GetContentBytesWithLimit(int64)` accepts an explicit positive output limit; zero or invalid limits are rejected for that method.

| Decode budget | Default |
| --- | ---: |
| Encrypted input | 32 MiB |
| Decompressed content | 64 MiB |
| Argon2 memory | 256 MiB |
| Argon2 iterations | 10 |
| Argon2 parallelism | 16 |
| AES-KDF rounds | 10,000,000 |
| XML nesting depth | 128 |
| XML token count | 1,000,000 |

These are per-operation budgets, not recommended encryption settings or a hard cap on total process memory or elapsed time. Decoding can hold multiple buffers and parsed objects at once. Applications can explicitly raise supported limits for legitimate larger databases. Introducing default limits can reject files previously accepted without a budget; this compatibility change needs upstream discussion.

The patch must return a limit error rather than reducing a database's KDF cost. It does not change the existing Argon2id policy protecting Bloco wallet envelopes.

### Compatibility cases that must not be confused with corrupt input

The existing encoder adds PKCS#7-style padding even for ChaCha20 and emits an extra 36 zero bytes after the authenticated KDBX 4 terminal block. The reader must distinguish these existing legacy outputs from arbitrary trailing corruption. Standard files without those legacy additions must remain readable.

CBC padding must be validated/removed before enforcing gzip trailer errors. Simply replacing the ignored gzip error with an unconditional return, without considering encrypted framing, could reject valid existing files. For compressed ChaCha20 payloads, the reader must finish validating the gzip stream before recognizing an optional legacy padding suffix; otherwise genuine gzip trailer bytes can be mistaken for padding.

The upstream attachment writer has a separate closing-order issue: when gzip wraps a base64 encoder, closing gzip alone does not flush the base64 encoder. The patch closes both writers in the correct order instead of accepting truncated gzip as success. Old attachments whose encoded data is genuinely truncated are now rejected; the patch does not reconstruct missing bytes.

Protected field operations must follow the library's existing contract: call `LockProtectedEntries` after supplying plaintext fields and before `Encode`, and `UnlockProtectedEntries` after decoding and before inspecting those values. An early interoperability test omitted these calls. That test failure is not evidence of an upstream protected-field interoperability defect.

### Deliberate exclusions

- No new Argon2id support in the upstream library as part of this fix.
- No new cryptographic primitives or changes to their implementations.
- No keyfile-parser redesign.
- No guarantee of immediate cancellation while an existing KDF primitive is executing.
- No guarantee of forensic erasure of Go runtime/library memory.
- No assertion that the encoder automatically refreshes all encryption parameters when an already-open database is saved. The Bloco adapter will need its own explicit write-side lifecycle and independent verification.
- No claim that this patch is a complete security audit or proves the absence of other decoder defects.

## Verification record

### Completed before the patch

- Source review of the release decoder, header, block, inner-header, credentials, binary, and cipher-wrapper paths.
- Reproduction of the 17-byte compression-header panic in an isolated module using `v3.7.0`.
- Independent client availability: KeePassXC CLI `2.7.12` responds after first-run authorization.

### Completed patch verification

| Check | Result |
| --- | --- |
| Minimal malformed-header regression | Failed with a panic before the patch; returns an error after the patch |
| Upstream library suite (`go test ./... -count=1`) | Passed, including KDBX 3.1/4.0/4.1 fixtures |
| Final changed-test subset and targeted race checks | Passed |
| Bounded decoder fuzzing | 15-second run passed with no observed crash; the valid low-KDF seed is explicitly checked to reach content decoding |
| Final fuzz seed-corpus check | Passed for all three seed cases; encoder failure cannot silently omit the valid seed |
| KeePassXC 2.7.12 interoperability on macOS | Passed: protected password/custom fields, export/show, rename/save in KeePassXC, then decode/unlock and verify all values and protection flags |
| Library and parent `go vet` | Passed |
| Parent `make lint` | Passed: 0 issues |
| Actionlint v1.7.12 | Passed |
| Parent `internal/keepass` dependency test | Passed using the patched-only constructor and limit error |
| Module resolution | `go list -m -json` confirms `Replace.Dir` is `third_party/gokeepasslib` |
| Static Bloco build | Passed with `CGO_ENABLED=0` and `netgo,nocgo`; the resulting binary executed `--version` successfully |
| Library test cross-compilation | Passed for Linux and Windows; this is not native execution on those systems |
| Whitespace checks | `git diff --check` passed in both repositories |

The final interoperability test uses AES-256, Argon2d with 64 MiB / three iterations / two lanes, and synthetic protected fields. It follows the library's required lock/unlock sequence. The earlier failed test did not; it is not included as evidence of an upstream encryption defect. Test files are temporary and the external client's plaintext output is retained only in process memory.

Fuzzing and passing fixtures provide regression evidence, not proof of complete coverage or absence of vulnerabilities.

### Parent-repository network-boundary validation

The initial G16 failure was a scope-classification defect: the guard scanned the root tree and treated an explicitly vendored dependency inside the `third_party/gokeepasslib` Git submodule as first-party code. The flagged `testify` helper constructs a request in memory with `httptest.NewRecorder`; it does not send an outbound request.

The guard now reads the root module identity and `.gitmodules`, skips only a declared submodule whose own `go.mod` identifies a different module, and continues scanning all other Go files. This is not a directory-name exemption: first-party fixtures under `vendor` and `third_party` are deliberately detected. A same-named `rpc_gateway.go` outside `internal/blockchain/rpc_gateway.go` is also detected. The exact approved gateway path is the only exception.

Regression coverage in `internal/blockchain/network_boundary_test.go` proves:

- forbidden transport calls in first-party files fail the scan;
- `vendor` and undeclared `third_party` paths remain covered;
- the declared external KeePass submodule is classified separately;
- only the exact gateway path is exempt.

The controls remain active and unchanged in intent: no skipped test, scanner suppression, blanket vendor exclusion, or `continue-on-error` workaround was introduced. The corrected local oracle passes:

```sh
go test ./internal/blockchain -run '^Test(RPCGatewayOwnsAllOutboundTransports|NetworkBoundaryScannerClassifiesModuleOwnership)$' -count=1
node scripts/verify-kdbx-gates.mjs G16
```

The new commit must still complete remote CI before release claims are made. The initial remote failure also included the now-corrected sandbox test; that issue was fixed by binding HOME/USERPROFILE to a temporary directory. The separate GitHub tracking issue attempt remains blocked by HTTP 403 on issue creation, so the PR and this document retain the record.

At patch-only validation, no remote CI or clean remote submodule checkout had been run because the patched commit was still unpublished. Publication has since succeeded, as recorded below; publication alone does not establish a clean-checkout or CI result. No Docker image was built in that validation; its dependency-copy order and workflow syntax were reviewed. That validation did not exercise real hardware/wallet flows, native Windows/Linux execution, or wallet-facing KDBX integration.

### PR #55 corrective validation

The initial remote CI run also detected `TestTestsNeverWriteOutsideSandbox` rejecting `os.UserHomeDir()` in the new configuration test. That test now binds `HOME` and `USERPROFILE` to a temporary directory; the sandbox guard is unchanged. Native Windows paths in configuration fixtures are quoted for TOML.

Review corrections require queued credentials before account activation, route expired creation authentication through password re-entry and a fresh backup challenge, and refresh the account ledger status after successful backup. Installed-but-durability-unconfirmed writes retain pending intents and expose a distinct localized warning with backup paths. Retrying a deletion whose entry is already absent performs a durable rewrite before acknowledging the ledger; reading an absent entry alone is insufficient.

The named G20 regressions passed, including race checks. The prior local `make test-production` run at f955d1f passed every other package but failed the now-corrected network-boundary classification. The corrected G16 oracle passes locally; a fresh full production run and remote CI for the correction commit are still required before release claims. Cross-builds succeeded for Linux amd64/arm64, Darwin amd64/arm64, and Windows amd64. The Darwin arm64 binary executed `--version` and `release-smoke` successfully under an isolated application home. These results do not establish native execution on other platforms or a published release.

`govulncheck` remains a GitHub Actions-only verification step in this project and will not be executed locally.

## Submodule and publication lifecycle

The submodule path is `third_party/gokeepasslib`. The patch branch is `fix/bounded-kdbx-decoding`, based on upstream commit `4fa52e497f7956e7c874da8f2b7a06f049627ccf`. The investigated parser code matches the release; using the existing newer upstream base preserves its dependency updates and consistent vendor tree rather than downgrading dependencies. The original reproduction still identifies the release separately. The reviewed local patch commit is `257926a46375a14b21dde8a0b5f507fd0259ec5e`; the parent gitlink is included in PR #55.

The user created the fork `italoag/gokeepasslib` and authorized publication. HTTPS Git push returned HTTP 403, but the SSH push succeeded. Branch `fix/bounded-kdbx-decoding` now points to `257926a46375a14b21dde8a0b5f507fd0259ec5e`, independently confirmed with `git ls-remote git@github.com:italoag/gokeepasslib.git refs/heads/fix/bounded-kdbx-decoding`. `.gitmodules` uses that SSH URL. The parent Bloco changes are published on PR #55; neither repository has been merged or released.

A Git submodule stores a commit pointer, not an embedded copy of that commit in the parent repository. The patched dependency is reachable from the referenced fork, but a local build must not be reported as a successful remote CI run or native execution on every target platform.

Once publication is explicitly requested:

1. Create/verify the intended fork and publish only the reviewed patch branch.
2. Confirm the referenced commit is reachable from the submodule URL.
3. Validate a clean recursive checkout in a separate temporary directory.
4. Follow upstream contribution instructions: first discuss the change in an issue, then open the PR with a concise description and test evidence.
5. Keep the patched submodule pinned until an upstream release contains the accepted fix and has passed the same integration tests.
6. Only then plan removal of the local replacement/submodule; merging a PR alone does not automatically update a tagged module dependency.

## Using the local patch

The Bloco `go.mod` contains:

```go
replace github.com/tobischo/gokeepasslib/v3 => ./third_party/gokeepasslib
```

The module version label remains `v3.7.0`, but the local replacement and gitlink determine the actual source. Do not describe this as an upstream `v3.7.0` containing the fix.

From the existing initialized Bloco checkout:

```sh
git submodule status
go list -m -json github.com/tobischo/gokeepasslib/v3
make test-keepass
go test ./internal/keepass -count=1
KEEPASSXC_CLI=/opt/homebrew/bin/keepassxc-cli go -C third_party/gokeepasslib test -run '^TestKeePassXCInterop$' -count=1
```

Set `KEEPASSXC_CLI` to the installed executable on other systems. The interoperability test is opt-in and only creates synthetic files under a test-owned temporary directory. Do not run `git submodule update --remote` to follow a moving branch; use the pinned commit. Before publication, keep this local checkout intact because the parent repository alone does not contain the submodule objects.

Docker's dependency-download layer now copies the submodule's module metadata first. Relevant workflows request recursive submodule checkout and the CI test job runs the library suite explicitly; nested Go modules are not covered by the parent `go test ./...` alone. These workflows require the fork and commit to be published before they can succeed remotely.

## Draft upstream issue

**Title:** Decoder panics on a truncated KDBX 4 compression header

`Decoder.Decode` in v3.7.0 panics on the 17-byte input shown above instead of returning an error. The compression-flags field declares zero bytes, and `readFileHeader` calls `binary.LittleEndian.Uint32` without checking its length. This happens before key derivation or header authentication.

Related parser paths also trust declared lengths, and gzip decoding discards read errors and has no expansion limit. I have prepared a local patch with regression tests and configurable decode budgets. Would you prefer the structural validation fixes and the resource-limit API as separate PRs? The upstream fixture suite and a synthetic read/write round trip with KeePassXC 2.7.12 passed.

## Draft upstream PR description

**Title:** Reject malformed KDBX framing and bound decoder resources

The proposed change validates field/block lengths before allocating or slicing, propagates payload and gzip errors, and adds explicit decode budgets checked before expensive KDF work and content expansion. It preserves the standard KDBX format and existing decoder constructor.

Regression tests cover the reported 17-byte panic, malformed/truncated fields and blocks, resource-limit failures, and corrupted compressed content. Validation passed for the upstream fixture suite, targeted race checks, bounded fuzzing, `go vet`, and a synthetic protected-field round trip with KeePassXC 2.7.12. The compatibility impact of default limits must be agreed with maintainers before submission. The separate Bloco parent-suite blockers are documented above and are not presented as passing checks for this integration.
