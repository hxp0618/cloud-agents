# P1 Control Plane source-bound supply refresh — 2026-08-17

- Status：**SOURCE-BOUND DERIVED EVIDENCE — PASS；`G-SUPPLY-CHAIN` OPEN**
- Fixed source commit：`6e58e06b146fc3eb1ac7706837adffdeee1d8fa1`
- Fixed repository tree：`5bb74ef0b8152bd96c2b65d44421b50f3e78b1a0`
- Fixed `services/control-plane` subtree：`9ff00173a454e61f5bef26f34e0b6b6ffd0b6770`
- Snapshot：`2026-08-17T04:10:44Z`
- Toolchain：Node `24.13.1`、Bun `1.3.14`、Go `1.26.6 darwin/arm64`
- Evidence owner：Codex P1 supply refresh executor
- Independent Gate reviewer：**not assigned**

## 1. Refresh decision

The verified PostgreSQL catalog projector, its signed owner-closure subject and its PG15/16/17 matrix
changed source identity, but did not change `go.mod`, `go.sum`, the selected module graph, Linux/Darwin
non-test import closures, license/PATENTS scope or `THIRD_PARTY_NOTICES.md`. This refresh therefore updates
only:

1. `contracts/generation.lock.json`, whose migration-checker input includes the amended ADR-0010;
2. `services/control-plane/dependency-lock.json`, whose source identity must describe the current
   committed source;
3. `services/control-plane/sbom.cdx.json`, whose root component identity must match that same source;
4. this evidence record and its indexes.

The historical x/sys, x/mod and pgx/x-text dependency reviews remain fixed-source evidence and are not
rewritten. Their dependency/license/provenance decisions still apply to their declared commits; this
record supersedes only stale current-runtime and current-source metadata.

## 2. Exact source and closure identity

| Evidence                                    | Exact value                                                        |
| ------------------------------------------- | ------------------------------------------------------------------ |
| Source commit                               | `6e58e06b146fc3eb1ac7706837adffdeee1d8fa1`                         |
| Repository tree OID                         | `5bb74ef0b8152bd96c2b65d44421b50f3e78b1a0`                         |
| `services/control-plane` subtree OID        | `9ff00173a454e61f5bef26f34e0b6b6ffd0b6770`                         |
| 320-file tracked manifest SHA-256           | `14d7d4614b56858baf6d77075614577f05ab5a9cc3db0588f7b97652b63e045c` |
| 250-file tracked Go-source manifest SHA-256 | `f88b226a0a133ba437f9c2a6bf23caef536edda286e89272b7eca1439c6e560e` |
| `go.mod` SHA-256                            | `a4d98dcbd65803a22bcf946cf042d17484e714500c0502b616d68742a02f1d14` |
| `go.sum` SHA-256                            | `c5e16bfbadc2461fd349b94ce6487aadcb2edea11fa0aa37fd29bc2f46bfc88c` |
| sorted `go list -m all` SHA-256             | `d92f23836990be0c7e967b5bb5deb1b34d67e248e707a569726d06f2914d7ef4` |
| sorted `go mod graph` SHA-256               | `a52ef78c1e0db672077c4d6c713afaa693d923fc29ea630e0d97831a8beb397f` |

The selected graph remains 16 non-main modules: 7 Linux production-linked and 9 graph-only, with 4
ordinary direct requirements. Production closures remain byte-identical to the prior fixed graph:

| Closure                            | Modules / packages | Modules SHA-256                                                    | Packages SHA-256                                                   |
| ---------------------------------- | -----------------: | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| Linux amd64/arm64, `CGO_ENABLED=0` |             7 / 30 | `48ca0dbaba0f918d99091decd0520a70327c36badb7d74c7cbbe1e180cd66e5f` | `12a56c91f56460e9757560f00234c06cec462f248df6a770d41172168e9a8d08` |
| Darwin arm64, `CGO_ENABLED=0`      |             6 / 29 | `12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` | `07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354` |

Linux still links `golang.org/x/sys/unix`; Darwin excludes it and production evidencefs fails closed
there. On supported Linux ext4/XFS, production evidencefs and migration `EvidenceSink` are now enabled
only by a fresh root-provisioned trusted-mount claim. Revoked claims and every unsupported platform
remain fail closed.

## 3. Derived artifacts

| Artifact                                        | SHA-256                                                            | Decision                                 |
| ----------------------------------------------- | ------------------------------------------------------------------ | ---------------------------------------- |
| `contracts/generation.lock.json`                | `a7a7f044547b9289ec17b9e6fc7ad166da7b75bf979763cf1d92fe8fbb2d97a0` | current for amended ADR-0010 inputs      |
| `services/control-plane/dependency-lock.json`   | `96923e86b4a8385a2e95ba7dbe0212654a6c957af00ae07e00def92e129ac634` | current source/closure/security metadata |
| `services/control-plane/sbom.cdx.json`          | `b24852ea156b24bfce6e228a303493a2bb923009cf69935046906acfd774f141` | CycloneDX 1.6, 16 unique components      |
| `services/control-plane/THIRD_PARTY_NOTICES.md` | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` | exact same bits; no legal-scope change   |

The generation lock changes only the migration-checker source/input manifest from
`91da131c2573aa1ecc51f305be8f9635ac8d33a0d1a7095188863156ff55a0d4` to
`0c2895b850665274b695eadf18ead4dda7e4205f3e9cf0127c3d4489e909ce26`. C3 schemas, fixtures, generated
files, bundle manifest and deterministic runtime/bootstrap tar bytes remain same-bits.

The SBOM retains 16 unique component refs and the root depends on exactly the 7 Linux
production-linked modules. Every ref resolves, x/sync/x/sys/x/text retain their independent PATENTS
metadata, and graph-only x/mod/x/tools remain inventory-only rather than runtime-distributed.

## 4. Fresh vulnerability snapshot

Current-source scans were rerun rather than inherited:

| Check                                        | Result                                            | Evidence SHA-256                                                                                                                                                |
| -------------------------------------------- | ------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `govulncheck v1.6.0 -scan=module`            | DB `2026-08-14T16:22:54Z`; no findings            | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                                                                              |
| Linux amd64 `govulncheck -scan=symbol ./...` | Go 1.26.6, `CGO_ENABLED=0`; no reachable findings | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                                                                              |
| OSV querybatch, 16 exact selected modules    | 16 responses; 0 findings                          | query `b0ab3c0cbc9e84fba34f1b183c9ae65dfa58c635a823d06c33914619f763d911`; canonical response `ab5a0787744e90d4b9bef630420e8085dd8045f7cd5fe87fc0b5acc7b6a55b93` |

The empty results are time-bound, non-bit-safe evidence. Any scanner/database, module graph, import,
build tag, toolchain, source, checksum, license/PATENTS or distribution-scope change invalidates them.

## 5. Mechanical verification

The final refresh must satisfy:

- declared Node/Bun/Go tuple and `platform:contracts:check`;
- migration bundle checker/generator `--check`, with unchanged 33 generated files and bundle/tar digests;
- `go mod tidy -diff`, `go mod verify`, selected graph and Linux/Darwin closure recomputation;
- dependency-lock module classification, exact production rows and NOTICE binding;
- CycloneDX 1.6 validation against the official tag's BOM/SPDX/JSF schemas, whose SHA-256 values are
  `3e92dddbc30cf7f6a02b80f0942b1a4cfd4fb1c26f1dfc4310afa9d613cafb93`,
  `baa9d3bd1ed57b6751b0887edead6b5063ff53ff7429cf85d476c6c94af0166e` and
  `8bae002c25e723db7ee1f26afde680ae1a2b1a8f6b4b4b0fd65dc3becb090aae`;
- targeted formatting, secret scan and `git diff --check`.

The exact Control Plane subtree in `6e58e06` is byte-identical to the reviewed `bbb0bf2` catalog
implementation subtree; the repository commit additionally records the catalog evidence/index update.
Focused catalog normal/race, vet/build/cross-compile and the 24-leg PG15/16/17 matrix are recorded
separately. This refresh does not relabel the pre-existing ten-minute full migration test timeout as a
catalog failure or as a passing gate.

## 6. Gate boundary

This refresh makes current source, dependency and SBOM metadata internally consistent. It is not a final
binary/artifact scan, distribution manifest, independent immutable closure or release attestation.
`G-SUPPLY-CHAIN` therefore remains `IN PROGRESS`, as do `G-DATA`, `G-AUTHORITY-P1` and
`G-SECURITY-P1`. It does not authorize runner/DB integration, deployment, Platform RC, Beta, GA, merge
or release.
