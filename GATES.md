# Gates: complete KeePass/KDBX integration

OWNS: internal/keepass/**, internal/wallet/**, internal/storage/**, internal/ui/**, pkg/config/**, pkg/localization/locales/**, cmd/blocowallet/main.go, internal/constants/constants.go, .github/workflows/**, Dockerfile, Makefile, go.mod, go.sum, .gitmodules, scripts/verify-kdbx-gates.mjs, docs/gokeepasslib-decoder-hardening.md, GATES.md

Scope: Deliver the approved dedicated-KDBX integration, including published SSH submodule, configuration, automatic account and artifact backups, exact credential lookup, explicit removal, recoverable partial failures, localized TUI, and verification. User-owned skill updates and skills-lock.json are outside this task.

Execution: sequential checks from this repository root, with synthetic wallets and isolated application homes. The lead authors the oracles; implementation work must not weaken them. Review scripts/verify-kdbx-gates.mjs before approving execution. No local govulncheck or real wallet access. Approval and evidence are separate from implementation status.

Evidence correction: the previous G10 evidence is invalidated. Formatting/vet followed by an unconditional success echo did not prove localization, sanitization, or UI behavior. Broad go-test filters could also pass while required tests were absent. The new runner requires each named test to run and pass, rejects skipped required cases, checks package/process outcomes, and emits a success marker only after all assertions complete.

G16 network-boundary scope: the guard scans the root module's first-party Go sources and skips only an explicitly declared Git submodule when its own go.mod identifies a different module. Directory names such as vendor and third_party are not exemptions, and the gateway exception is restricted to internal/blockchain/rpc_gateway.go. Positive and negative classification fixtures are part of the guard test.

Publication history: the owner initially authorized publishing the feature branch with G16 deferred. PR #55 exists at commit da88ce6. The owner subsequently required all failures and review findings to be addressed; the classification correction is now implemented locally and G16 is being reverified. Production release publication is not authorized by this verification request.

- [x] G0: The acceptance runner rejects missing, skipped, failed, malformed, and interrupted test evidence while accepting a valid positive control
  CHECK: node scripts/verify-kdbx-gates.mjs G0
  EXPECT: KDBX_GATE_OK G0
  EVIDENCE: automatic-evidence=v1; definition-sha256=6e6f482551c88ed24b25e28ef632eb6647dea0790c0a925073f52438a358a238; exit=0; EXPECT=matched; output-sha256=bd106f1be238f019fe1112d8b1564f4bb9707f103eec9c83146c444ff107e284; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G1: The master-password overlay is visible over deletion/recovery and does not swallow blur, quit, clock, or asynchronous completion events
  CHECK: node scripts/verify-kdbx-gates.mjs G1
  EXPECT: KDBX_GATE_OK G1
  EVIDENCE: automatic-evidence=v1; definition-sha256=0b441d2ae8ee8ee2ae7cb461140cf87cc99714921cab6da015ed8bd7f84308bc; exit=0; EXPECT=matched; output-sha256=093944b31361e595449c070e07432c4ab0202ba93a97dbdb82c6120f7e51cfb8; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G2: Separate read-only actions require separate master authentication and worker cancellation reaches the authenticated operation
  CHECK: node scripts/verify-kdbx-gates.mjs G2
  EXPECT: KDBX_GATE_OK G2
  EVIDENCE: automatic-evidence=v1; definition-sha256=406523e6fda0a73b9f81e704bfa1b805aa48ae5b3648b6e05d5f1dd35cc139b2; exit=0; EXPECT=matched; output-sha256=65ced737c17e341c81565e8989e2f18502f4d2ba27e4191c5e6cfe6d665459c5; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G3: Creation, suspension/resume, rotation, and exports execute asynchronously and preserve committed wallet/file results when backup completion fails
  CHECK: node scripts/verify-kdbx-gates.mjs G3
  EXPECT: KDBX_GATE_OK G3
  EVIDENCE: automatic-evidence=v1; definition-sha256=32db18f1546eab1fb5efb79aa2b8b9128add4b6401c1008bdb7e09a98bcf1614; exit=0; EXPECT=matched; output-sha256=fbb86e0374e8af5ed7758881fe668be9c99db8438e696b7337ac6c5a64bf2018; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G4: Individual import previews and commits use the selected KDBX source credential without exposing it, while matching .pwd files retain priority and all software import methods archive their results
  CHECK: node scripts/verify-kdbx-gates.mjs G4
  EXPECT: KDBX_GATE_OK G4
  EVIDENCE: automatic-evidence=v1; definition-sha256=c6630b2753fe1256844a1de0340a3fd02dea073f8acc2f4a726f85ef939eb048; exit=0; EXPECT=matched; output-sha256=803c14a9de2c8aec13182f18c94e032ab96746f948d2d974a9efdcb2afaa73da; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G5: Keystore and mnemonic batches preserve import counts, perform one coordinated backup, and distinguish absent source credentials from authentication/ambiguity failures and explicit lookup opt-out
  CHECK: node scripts/verify-kdbx-gates.mjs G5
  EXPECT: KDBX_GATE_OK G5
  EVIDENCE: automatic-evidence=v1; definition-sha256=6cf29c69ad12da6b7353ed648e13942a334485ab16315f948e1207c6542bd647; exit=0; EXPECT=matched; output-sha256=b4fcc6ca6bb0de60250ccfae596f493018acaa6bb06b7abcb69ed7b678518127; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G6: Pending account/file/deletion retries honor explicit credential mode and selected file digest, and every deletion retry requires the typed AccountID
  CHECK: node scripts/verify-kdbx-gates.mjs G6
  EXPECT: KDBX_GATE_OK G6
  EVIDENCE: automatic-evidence=v1; definition-sha256=73b6770d18617e7d4ffae3f3a00ec6d154b2dfe55e600a4302b9a94d408a9a7a; exit=0; EXPECT=matched; output-sha256=b46dfedfcd78c1d0f7f9078fc8f15e7bc3905f777f7f6d2aee6d88622535b627; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G7: An account created before KDBX configuration can be backed up through the TUI using its manually supplied storage password
  CHECK: node scripts/verify-kdbx-gates.mjs G7
  EXPECT: KDBX_GATE_OK G7
  EVIDENCE: automatic-evidence=v1; definition-sha256=e6b448ad3493b96edf342d5f748a31bdcb822d0efe4315d13c4319db2f8b17f1; exit=0; EXPECT=matched; output-sha256=65b6451ae029d8daf9838ce0a5ec16f536c8f82fd89eb76861caa646b14831f9; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G8: The settings wizard supports validation, cancellation/retry, autocomplete and home paths, and never announces an unlock or configuration change that did not occur
  CHECK: node scripts/verify-kdbx-gates.mjs G8
  EXPECT: KDBX_GATE_OK G8
  EVIDENCE: automatic-evidence=v1; definition-sha256=4dc5b4dba14e13bb90bff9d75a51b8ae3ee880ff2a9a6a3ab626bdd7f9d7607c; exit=0; EXPECT=matched; output-sha256=f6a7f73db0c74a4909ab273ef5d5e169c8fe0eee1c597710bc22f51c2bf10027; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G9: Native transfer, personal/EIP-712 signing, contract calls, recovery, Safe deploy/sign/execute, and deletion use the correct account credential without bypassing their existing confirmations
  CHECK: node scripts/verify-kdbx-gates.mjs G9
  EXPECT: KDBX_GATE_OK G9
  EVIDENCE: automatic-evidence=v1; definition-sha256=e513701cf109fc1f295355e736c94716d4859e45f26222390ef917f6dcb20386; exit=0; EXPECT=matched; output-sha256=3866d6fce25b9633a74c5efbcf2b00b57d0268d6f4e9ce7a450af8dbf58f4324; output-bytes=16; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G10: New TUI states remain localized and navigable at supported terminal sizes, sanitize displayed metadata, conceal secrets, and reject stale asynchronous results
  CHECK: node scripts/verify-kdbx-gates.mjs G10
  EXPECT: KDBX_GATE_OK G10
  EVIDENCE: automatic-evidence=v1; definition-sha256=d0ae7ac86f68049ef491dc11f80ac963dce480840b2e7cee57f3d3443d15fa6d; exit=0; EXPECT=matched; output-sha256=718cde5e96834599f12a884eaebebf5052466107e550d018ee24db98725dca77; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G11: The complete UI regression suite passes and every required KDBX end-to-end test is present and passes
  CHECK: node scripts/verify-kdbx-gates.mjs G11
  EXPECT: KDBX_GATE_OK G11
  EVIDENCE: automatic-evidence=v1; definition-sha256=f58b57d2bd05dd382e52a648ff7b6daccb62abade93f04aaa3a25ec2742a682d; exit=0; EXPECT=matched; output-sha256=c5fbd957ce1e739a7b2af231be2a35a1c071cef628c7f90d97242f37a7cfbd6e; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G12: Every required KDBX UI end-to-end test passes under the race detector without skipped required cases
  CHECK: node scripts/verify-kdbx-gates.mjs G12
  EXPECT: KDBX_GATE_OK G12
  EVIDENCE: automatic-evidence=v1; definition-sha256=0eeba8bb456f8b54f3e8c13a31757972a8100e5e152ac459927285d6f023ac45; exit=0; EXPECT=matched; output-sha256=581694eb277347a7a57f5fdc8ccf21ab9cc8bfa0d274b82c429072e255dc3994; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G13: Localization and CLI suites pass and the static application build executes its version command successfully
  CHECK: node scripts/verify-kdbx-gates.mjs G13
  EXPECT: KDBX_GATE_OK G13
  EVIDENCE: automatic-evidence=v1; definition-sha256=51ab554f9ae7467b0df04ed0c739053e8a4c6c47c9673d2ffa7bfa3dfaf556e5; exit=0; EXPECT=matched; output-sha256=f74293d64e20dd07c855f2e47ce6c8d647b55c8a885e4c80642f57d6edfd88d1; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G14: The KDBX store, credential coordinator, transactional deletion ledger, and both configuration loaders satisfy the required persistence, identity, recovery, and failure-isolation regressions
  CHECK: node scripts/verify-kdbx-gates.mjs G14
  EXPECT: KDBX_GATE_OK G14
  EVIDENCE: automatic-evidence=v1; definition-sha256=322f4d01d1a1aa13a2629ec91874a3fc5b5a5c99f8e465a10b68b4217c631809; exit=0; EXPECT=matched; output-sha256=28f0eb241d8d2be584010c15ef295159d1297ef6ea21aef4ebeadc392e47ccd4; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G15: An installed KeePassXC independently reads and rewrites synthetic protected password, mnemonic, private-key, and passphrase fields produced by the application store
  CHECK: node scripts/verify-kdbx-gates.mjs G15
  EXPECT: KDBX_GATE_OK G15
  EVIDENCE: automatic-evidence=v1; definition-sha256=36f0671a2f5c58f365e9c0f5816d37de2c58690dffe41f4f15b1dd160dac53cc; exit=0; EXPECT=matched; output-sha256=20f9dbee8d5703b965186239d90494ce4c2c2c303fe3fccb4e96178157630f05; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G16: The entire parent-repository short test suite passes with the network-boundary control unchanged
  CHECK: node scripts/verify-kdbx-gates.mjs G16
  EXPECT: KDBX_GATE_OK G16
  EVIDENCE: automatic-evidence=v1; definition-sha256=b83b52aaa2c3632aba0c8800f48334e572dcd65369387a5f49689ac7127ce2e2; exit=0; EXPECT=matched; output-sha256=e7ab14d7a8ef30248cc61fe1f05bab38f2ed64aa3fd1b2cb489bc81ac59385ed; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G17: The published SSH branch, local submodule HEAD, configured SSH URL, and Go replacement all identify the reviewed patched dependency
  CHECK: node scripts/verify-kdbx-gates.mjs G17
  EXPECT: KDBX_GATE_OK G17
  EVIDENCE: automatic-evidence=v1; definition-sha256=f476bb08fb7fb668504dd951500aa6b9d068fd46654b795ad8980f61241b2e21; exit=0; EXPECT=matched; output-sha256=a961ebf57c13718eeea7f664853b1697f27aac975fcfd421b2e9d89e14666c77; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G18: The application's KDBX file-operation tests cross-compile for Windows and Linux, without claiming native execution on this macOS host
  CHECK: node scripts/verify-kdbx-gates.mjs G18
  EXPECT: KDBX_GATE_OK G18
  EVIDENCE: automatic-evidence=v1; definition-sha256=25c57074f9bc3dc5e20906623907bc2de1b7f93f24ce56c4886e209574f27a08; exit=0; EXPECT=matched; output-sha256=bf405138ca61d11ffe69615bf75ba68d908c1e8c7a43416a2eb694519aaa6a38; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [x] G20: Home expansion is isolated, fresh confirmation requires queued credentials, backup status refreshes immediately, durability warnings preserve pending intents, and export notices distinguish committed files from warnings and pending synchronization
  CHECK: node scripts/verify-kdbx-gates.mjs G20
  EXPECT: KDBX_GATE_OK G20
  EVIDENCE: automatic-evidence=v1; definition-sha256=f950457e476e0f8060e3884aee470b4fc07644f3331c6c470039073a53d9a2ef; exit=0; EXPECT=matched; output-sha256=d1da7b27c6c7e19107d499ad7ed43bdc88575b9572d2a393854fceb3b66f0451; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries

- [ ] G21: Remote CI for the correction commit passes tests, native build matrix and release-equivalent production verification without disabled guards or publishing a release
  EVIDENCE: pending

- [ ] G22: Every Copilot and Devin finding on the correction commit has been reviewed and addressed or explicitly escalated with supporting evidence
  EVIDENCE: pending

- [x] G19: Affected Go packages pass vet, the parent passes lint, workflow syntax validates, and both staged and unstaged diffs are whitespace-clean
  CHECK: node scripts/verify-kdbx-gates.mjs G19
  EXPECT: KDBX_GATE_OK G19
  EVIDENCE: automatic-evidence=v1; definition-sha256=ea10d46c194fa09c04cff4aa4b89681f635c4ddbf7955e6fbfb7b27e6925b24f; exit=0; EXPECT=matched; output-sha256=9008544909977f48e786db4135f1bfc046842b24901b4cfef4588a973985ba85; output-bytes=17; shell=/bin/sh; cwd=/Users/t798157/Projects/bloco/bloco-wallet; path=176f9420c573/76 entries
