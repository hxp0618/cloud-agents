# P1 Control Plane source-bound supply refresh — 2026-08-17

- Status：**SOURCE-BOUND DERIVED EVIDENCE — PASS；`G-SUPPLY-CHAIN` OPEN**
- Fixed source commit：`c0658f4aea8cddf9414749f783f2ca64a300a8c2`
- Fixed repository tree：`cefb8005b48fbe8e457203db829f27886c4a3068`
- Fixed `services/control-plane` subtree：`48f73de1d73949ebfe12b2b545f8487c018ebeae`
- Snapshot：`2026-08-16T22:08:19Z`
- Toolchain：Node `24.13.1`、Bun `1.3.14`、Go `1.26.6 darwin/arm64`
- Evidence owner：Codex P1 supply refresh executor
- Independent Gate reviewer：**not assigned**

## 1. Refresh decision

The production `EvidenceSink` implementation changed source identity and the truthful filesystem runtime
boundary, but did not change `go.mod`, `go.sum`, the selected module graph, Linux/Darwin non-test import
closures, license/PATENTS scope or `THIRD_PARTY_NOTICES.md`. This refresh therefore updates only:

1. `contracts/generation.lock.json`, whose migration-checker input includes the amended ADR-0010;
2. `services/control-plane/dependency-lock.json`, whose source identity and runtime-boundary statements
   must describe the current committed source;
3. `services/control-plane/sbom.cdx.json`, whose root component identity and filesystem boundary must
   match that same source;
4. this evidence record and its indexes.

The historical x/sys, x/mod and pgx/x-text dependency reviews remain fixed-source evidence and are not
rewritten. Their dependency/license/provenance decisions still apply to their declared commits; this
record supersedes only stale current-runtime and current-source metadata.

## 2. Exact source and closure identity

| Evidence                                    | Exact value                                                        |
| ------------------------------------------- | ------------------------------------------------------------------ |
| Source commit                               | `c0658f4aea8cddf9414749f783f2ca64a300a8c2`                         |
| Repository tree OID                         | `cefb8005b48fbe8e457203db829f27886c4a3068`                         |
| `services/control-plane` subtree OID        | `48f73de1d73949ebfe12b2b545f8487c018ebeae`                         |
| 312-file tracked manifest SHA-256           | `5f5e28334551c6c3588b82a0f26c3e00206796678d648a9af1a03f6c9a4ffeb0` |
| 243-file tracked Go-source manifest SHA-256 | `56fc4a698904b0a81895c6dcb78b6bfdc4dc2a0933afa5c16238ae04f031f996` |
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
| `contracts/generation.lock.json`                | `84d827fdb976aa3c7404e20decd721da39f8954845487a27825a4d0e76c6d544` | current for amended ADR-0010 inputs      |
| `services/control-plane/dependency-lock.json`   | `372cc31759054c570c911e9cf799746f3a6456fc1aa3a1351791a62b8e40048e` | current source/closure/security metadata |
| `services/control-plane/sbom.cdx.json`          | `8a0acfff168ca03faed7c9e36315d16cfe84b4dea3486c0ee65a008610fb562c` | CycloneDX 1.6, 16 unique components      |
| `services/control-plane/THIRD_PARTY_NOTICES.md` | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` | exact same bits; no legal-scope change   |

The generation lock changes only the migration-checker source/input manifest from
`b7a44f4e16d013384d11090ecefd63f951a0a3e879f3c8bb04cb605f334ddf01` to
`91da131c2573aa1ecc51f305be8f9635ac8d33a0d1a7095188863156ff55a0d4`. C3 schemas, fixtures, generated
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
| OSV querybatch, 16 exact selected modules    | 16 responses; 0 findings                          | query `d3582ad8e9b31aac6dc9fa2f548cf407f5b2a7858db16822129f0df56bb2450d`; canonical response `ab5a0787744e90d4b9bef630420e8085dd8045f7cd5fe87fc0b5acc7b6a55b93` |

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

The exact Control Plane subtree in `c0658f4` is the already tested `3fe05ec` implementation subtree.
The supply refresh does not rerun or relabel the previously recorded 45-minute full migration race
timeout; normal/full module tests, evidencefs race and the focused changed-authority race remain recorded
in the production `EvidenceSink` evidence.

## 6. Gate boundary

This refresh makes current source, dependency and SBOM metadata internally consistent. It is not a final
binary/artifact scan, distribution manifest, independent immutable closure or release attestation.
`G-SUPPLY-CHAIN` therefore remains `IN PROGRESS`, as do `G-DATA`, `G-AUTHORITY-P1` and
`G-SECURITY-P1`. It does not authorize runner/DB integration, deployment, Platform RC, Beta, GA, merge
or release.
