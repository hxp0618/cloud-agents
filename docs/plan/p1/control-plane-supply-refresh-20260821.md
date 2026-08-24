# P1 Control Plane source-bound supply refresh — 2026-08-21

- Status：**SOURCE-BOUND DERIVED EVIDENCE — PASS；`G-SUPPLY-CHAIN` OPEN**
- Fixed source commit：`a5df1cb672d7636e551b9fb24b74c9cc5e047ba6`
- Fixed repository tree：`bc7409828a520e1ff625eb94451f40c5de17cbbb`
- Fixed `services/control-plane` subtree：`fce57f0703a5c6c435353eae712dbef73f264762`
- Snapshot：`2026-08-20T23:24:53Z`
- Toolchain：Node `24.13.1`、Bun `1.3.14`、Go `1.26.6 darwin/arm64`
- Evidence owner：Codex P1 supply refresh executor

## 1. Refresh decision

A2.4 versioned compatibility-recovery registry/profile、append-only PostgreSQL writer kernel、typed
service/claim/matrix，A3 generated JSON/Proto SDK 和 pre-DB runner CLI changed the committed source
identity. They did not change `go.mod`, `go.sum`, the selected module graph, Linux/Darwin non-test import
closures, license/PATENTS scope or `THIRD_PARTY_NOTICES.md`.

This refresh therefore updates only the current-source fields in the dependency lock and CycloneDX root
component, adds this evidence record and links it from the P1 indexes. Historical dependency reviews keep
their original exact-source boundary. The current generation lock already matches all generated contract,
migration and SDK inputs and remains byte-identical during this refresh.

## 2. Exact source and closure identity

| Evidence                                    | Exact value                                                        |
| ------------------------------------------- | ------------------------------------------------------------------ |
| Source commit                               | `a5df1cb672d7636e551b9fb24b74c9cc5e047ba6`                         |
| Repository tree OID                         | `bc7409828a520e1ff625eb94451f40c5de17cbbb`                         |
| `services/control-plane` subtree OID        | `fce57f0703a5c6c435353eae712dbef73f264762`                         |
| 376-file tracked manifest SHA-256           | `16ef86565ce738cd66bfa5db4dbe30ee3467d65a6141e23050eb242a7fa0a022` |
| 273-file tracked Go-source manifest SHA-256 | `cc2d7a690083f067bc97694f1289091ebc2beae0a4b1cf067cf62c56f13a3933` |
| `go.mod` SHA-256                            | `a4d98dcbd65803a22bcf946cf042d17484e714500c0502b616d68742a02f1d14` |
| `go.sum` SHA-256                            | `c5e16bfbadc2461fd349b94ce6487aadcb2edea11fa0aa37fd29bc2f46bfc88c` |
| sorted `go list -m all` SHA-256             | `d92f23836990be0c7e967b5bb5deb1b34d67e248e707a569726d06f2914d7ef4` |
| sorted `go mod graph` SHA-256               | `a52ef78c1e0db672077c4d6c713afaa693d923fc29ea630e0d97831a8beb397f` |

The selected graph remains 16 non-main modules: 7 Linux production-linked and 9 graph-only, with 4
ordinary direct requirements. Production closures remain byte-identical to the previous fixed source:

| Closure                            | Modules / packages | Modules SHA-256                                                    | Packages SHA-256                                                   |
| ---------------------------------- | -----------------: | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| Linux amd64/arm64, `CGO_ENABLED=0` |             7 / 30 | `48ca0dbaba0f918d99091decd0520a70327c36badb7d74c7cbbe1e180cd66e5f` | `12a56c91f56460e9757560f00234c06cec462f248df6a770d41172168e9a8d08` |
| Darwin arm64, `CGO_ENABLED=0`      |             6 / 29 | `12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` | `07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354` |

Linux amd64 and arm64 module/package closures are byte-identical. Darwin excludes the build-tagged
`golang.org/x/sys/unix`; production evidencefs and migration `EvidenceSink` remain Linux-only and fail
closed on unsupported platforms.

## 3. Derived artifacts

| Artifact                                        | SHA-256                                                            | Decision                                  |
| ----------------------------------------------- | ------------------------------------------------------------------ | ----------------------------------------- |
| `contracts/generation.lock.json`                | `35375ec5fc10da7072ca3f802344c5f64ead7737cf5c497f3cacc9d9e5ff23bd` | exact same bits; generated inputs current |
| `services/control-plane/dependency-lock.json`   | `b99b451aeb0184450009cf8c7051f98fdbbf330976a71c7257b8cbc5fac4d8ee` | current source/closure/security metadata  |
| `services/control-plane/sbom.cdx.json`          | `18a7a10a8c4b67c3e91dbddc48c21517f7e1c60a0b6220984d6a0d9561428fe0` | CycloneDX 1.6, 16 unique components       |
| `services/control-plane/THIRD_PARTY_NOTICES.md` | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` | exact same bits; no legal-scope change    |

The SBOM root depends on exactly the 7 Linux production-linked modules. All 16 component refs remain
unique and resolvable; graph-only modules remain inventory-only rather than runtime-distributed. The
x/sync, x/sys and x/text PATENTS metadata and NOTICE binding remain unchanged.

## 4. Vulnerability evidence boundary

No vulnerability scan is inherited as current for `a5df1cb`. The dependency lock and SBOM retain the
historical, time-bound `govulncheck` and OSV snapshot from source
`350b53c72b62ea2bb33b8399aeabb1a1c8727a4c`, but explicitly keep
`current_source_inheritance: NOT_CLAIMED` / `current-source-vulnerability-scan: NOT_CLAIMED`.

The module graph and import closures are same-bits, but source changes are sufficient to invalidate a
symbol-level zero-finding claim. A new scanner/database snapshot is required before any current-source
vulnerability result may be asserted.

## 5. Mechanical verification

The exact Node/Bun/Go tuple passed:

- `platform:contracts:check`, including generated durable-coordination, compatibility-recovery, identity,
  JSON SDK and Proto SDK registry/manifest checks;
- `platform:migrations:check`, including the migration checker and deterministic generator `--check` for
  51 generated files;
- `platform:go:check`, including module policy, `go mod tidy -diff` and all-package runtime tests for all
  three Go workspace modules with `GOWORK=off`, `GOTOOLCHAIN=local` and `GOFLAGS=-mod=readonly`;
- `go mod verify`, selected module graph and Linux/Darwin closure recomputation;
- CycloneDX 1.6 validation against the official tagged BOM/SPDX/JSF schemas with fixed SHA-256
  `3e92dddbc30cf7f6a02b80f0942b1a4cfd4fb1c26f1dfc4310afa9d613cafb93`,
  `baa9d3bd1ed57b6751b0887edead6b5063ff53ff7429cf85d476c6c94af0166e` and
  `8bae002c25e723db7ee1f26afde680ae1a2b1a8f6b4b4b0fd65dc3becb090aae`;
- focused Control Plane normal/race tests, `go vet ./...`, `go build ./...`, and Linux amd64/arm64 plus
  Darwin arm64 compile-only checks.

The generated checks still report the intended fail-closed boundaries: production catalog runtime
introspection, schema publication and signing remain unavailable. This refresh does not turn a local
test result into a production authority or immutable Gate signature.

## 6. Gate and side-effect boundary

This record makes the current source, dependency lock and SBOM root identity internally consistent. It
does not add or activate an HTTP route, P2/provider integration or external side effect. It does not
authorize production database writes, deployment, publication, release, merge or any Gate closure.

Final binary/artifact scanning, a current-source vulnerability snapshot, distribution manifests and
independent immutable closure remain open. `G-SUPPLY-CHAIN` therefore remains `IN PROGRESS`; all other
aggregate and phase Gates retain their prior status.
