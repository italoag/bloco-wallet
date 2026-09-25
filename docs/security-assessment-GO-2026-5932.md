# GO-2026-5932 — assessment of non-applicability

- Record date: 2026-09-25 (UTC).
- Technical conclusion: not applicable to the assessed executable and build configuration delimited below, due to the absence of the affected OpenPGP code in the compiled dependency graph.
- Nature: evidence record; it does not constitute scanner suppression, a change to the security policy, or a general approval of other versions and platforms.

## Assessed scope

| Item | Value |
| --- | --- |
| Repository | `italoag/bloco-wallet` |
| Image commit | `4786ad218b1305a9a5e7a5c2636cde50416deef7` |
| Code review commit | `ea40b658c6bc33695450b0b22583505105aa4b43` |
| Identified module | `golang.org/x/crypto@v0.56.0` |
| Verified platform | `linux/amd64`, `CGO_ENABLED=0` |
| Build tags | `netgo,osusergo,nocgo,timetzdata` |
| Application entrypoint | `./cmd/blocowallet` |
| Executable in the image | `/usr/local/bin/bloco-wallet-manager` |
| Immutable image reference | `ghcr.io/italoag/bloco-wallet@sha256:2725961454f0fa1b3c7722ea6978bde1a07201f16fa8af90873e005db258b5e8` |
| AMD64 platform manifest | `sha256:e312df8d126c367b1a20b2fe111e1b47645003e2fe7286f5669476d6dc4efae1` |

The comparison between the two commits showed no changes in `go.mod`, `go.sum`, `cmd/`, `internal/` or `pkg/`. The graph review used the platform and build tags of the image, not just the development machine's default settings. The scope does not automatically include other architectures, upstream module tools, or future builds.

## What the advisory describes

The official [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) advisory states that `golang.org/x/crypto/openpgp` is unmaintained and has known security issues. Its record in the [Go vulnerability database](https://vuln.go.dev/ID/GO-2026-5932.json) identifies the packages:

- `golang.org/x/crypto/openpgp`
- `golang.org/x/crypto/openpgp/packet`
- `golang.org/x/crypto/openpgp/armor`
- `golang.org/x/crypto/openpgp/clearsign`
- `golang.org/x/crypto/openpgp/errors`
- `golang.org/x/crypto/openpgp/elgamal`
- `golang.org/x/crypto/openpgp/s2k`

The upstream module remains covered by the advisory, with no fixed version indicated. This assessment does not declare the entire module safe nor dispute the existence of the problem in OpenPGP.

## Evidence

### 1. The project does not need the OpenPGP package

Command executed on the verified checkout:

```sh
go mod why golang.org/x/crypto/openpgp
```

Result:

```text
# golang.org/x/crypto/openpgp
(main module does not need package golang.org/x/crypto/openpgp)
```

The search for OpenPGP imports in `cmd/`, `internal/` and `pkg/` also found no occurrences.

### 2. Absence from the Linux AMD64 build dependency graph

Command executed:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go list -deps -tags=netgo,osusergo,nocgo,timetzdata ./cmd/blocowallet
```

None of the affected OpenPGP packages appeared in the output. The `golang.org/x/crypto` packages present were:

```text
golang.org/x/crypto/pbkdf2
golang.org/x/crypto/scrypt
golang.org/x/crypto/ripemd160
golang.org/x/crypto/blake2b
golang.org/x/crypto/argon2
golang.org/x/crypto/internal/alias
golang.org/x/crypto/chacha20
golang.org/x/crypto/internal/poly1305
golang.org/x/crypto/chacha20poly1305
golang.org/x/crypto/hkdf
```

The presence of these other packages explains the module dependency. In particular, `go mod why -m golang.org/x/crypto` showed the path `blocowallet/internal/backup` → `golang.org/x/crypto/argon2`. The ProtonMail OpenPGP library does not replace these components; adding it unused neither removes the dependency nor fixes this alert.

### 3. Preserved scanner finding

The scan of the immutable image with Trivy `0.74.0`, platform `linux/amd64`, found a single advisory:

| Field | Result |
| --- | --- |
| Advisory | `GO-2026-5932` |
| Package | `golang.org/x/crypto` |
| Version | `v0.56.0` |
| Severity | `UNKNOWN` |
| Fixed version | Not provided |
| Status in the Trivy report | `affected` |
| Scan exit code | `1` |
| Local report identifier | `01a0d5d9-5f94-7ae1-8e23-db4b5cefb53d` |

The CI SARIF report for the same commit, GitHub Code Scanning analysis `1836396647`, contains the same advisory as its only result. Corresponding runs: [CI](https://github.com/italoag/bloco-wallet/actions/runs/36073348731) and [Container](https://github.com/italoag/bloco-wallet/actions/runs/36073348845).

The assessment of non-applicability derives from the absence of the affected packages in the application build, and not from the `UNKNOWN` severity, from the removal of findings, or from a claim of an upstream fix.

## Limitations and pipeline impact

- No ignore rule, automatically consumed VEX statement, or severity/exit-code change was created. Trivy may still block the pipeline on this advisory.
- The analysis is specific to the advisory and the scope above; it is not a guarantee that no other vulnerabilities exist.
- `govulncheck` was not validated in this investigation: the local query to the database returned HTTP 403 and the CI step was skipped after the Trivy failure. Its approval is not used as evidence in this conclusion.
- Any operational use of this record for handling the finding depends on review by the owner of the security policy.

## When to reassess

Repeat the assessment if dependencies, imports, entrypoints, platform, build tags, packaging process, or the content of the upstream advisory change. In particular, any introduction of a `golang.org/x/crypto/openpgp` package or its subpackages invalidates the absent-affected-code justification for the corresponding build. Do not extend this conclusion to new images merely because they share the same mutable tag.
