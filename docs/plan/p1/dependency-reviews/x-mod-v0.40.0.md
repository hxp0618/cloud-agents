# P1 dependency security closure：golang.org/x/mod v0.40.0

- Status：**APPROVED — test-gate dependency security closure only**
- Fixed source：`b3f8d9aeed0ec10eef8edcbab0029f1c20cee2ae`
- Reviewed dependencies：`golang.org/x/mod v0.40.0`、selected graph edge
  `golang.org/x/tools v0.49.0`
- Accountable owner：hxp0618
- Evidence owner：Codex P1 supply-chain executor
- Snapshot：`2026-08-16T16:29:15Z`
- Toolchain：Go `1.26.6 darwin/arm64`；production closure replay uses Linux amd64/arm64 and Darwin arm64
- Prohibited：`replace`、`exclude`、`retract`、fork、floating version、blank import、把 test-only dependency 误报为 production-linked

## Decision

`golang.org/x/mod v0.37.0` was selected by the previous `x/text v0.39.0` graph and is affected by
[`GO-2026-6179`](https://vuln.go.dev/ID/GO-2026-6179.json) and
[`GO-2026-6180`](https://vuln.go.dev/ID/GO-2026-6180.json). Both official records identify
`x/mod v0.40.0` as the first fixed module release.

The Control Plane now has an ordinary exact direct requirement on `x/mod v0.40.0`. It is consumed only by
`internal/modpolicy/policy_test.go`, where the platform gate strictly parses the checked-in `go.work` and all three
`go.mod` files, rejects dependency-policy bypass directives, and rejects an x/mod downgrade or indirect-only floor.
The gate is explicitly executed by `scripts/check-platform-go-modules.ts`.

The policy implementation is intentionally test-only. A prior ordinary `main` package was removed because it would
have entered `go list -deps ./...` and falsely classified x/mod as a production-distributed dependency. With the final
shape, `go mod tidy` retains the direct requirement, while Linux and Darwin non-test closures contain no x/mod or
x/tools package. `x/mod v0.40.0` and its selected `x/tools v0.49.0` edge therefore remain **graph-only / not linked /
not distributed** for the Control Plane runtime.

This approval closes the two selected-module advisory findings. It does not close `G-SUPPLY-CHAIN`, authorize a
release, or change trusted-mount, runner, database, RC, Beta, GA, or deployment status.

## Exact source authority

| Evidence                                          | Exact value                                                        |
| ------------------------------------------------- | ------------------------------------------------------------------ |
| Source commit                                     | `b3f8d9aeed0ec10eef8edcbab0029f1c20cee2ae`                         |
| Repository tree OID（Git SHA-1）                  | `1329ddaba419248a10757a2ffe05cc688f6b077c`                         |
| `services/control-plane` subtree OID（Git SHA-1） | `ad38008c9d449fb5600348bcb038ded42667c4ac`                         |
| 294-file tracked manifest SHA-256                 | `ebcf89683cff3a9bbf8fc3b6ef68528cd819c1f4e09aeff737b2cc2731fc5c0b` |
| 226-file tracked Go-source manifest SHA-256       | `f68ca7d7ccf61479bc2eafec6eadf388f1b4dfb9f588444ae767fd3fbe3540f5` |
| `go.mod` SHA-256                                  | `a4d98dcbd65803a22bcf946cf042d17484e714500c0502b616d68742a02f1d14` |
| `go.sum` SHA-256                                  | `c5e16bfbadc2461fd349b94ce6487aadcb2edea11fa0aa37fd29bc2f46bfc88c` |
| sorted `go list -m all` SHA-256                   | `d92f23836990be0c7e967b5bb5deb1b34d67e248e707a569726d06f2914d7ef4` |
| sorted `go mod graph` SHA-256                     | `a52ef78c1e0db672077c4d6c713afaa693d923fc29ea630e0d97831a8beb397f` |

The dependency lock, SBOM, this review, and live status documents are derived after the fixed source commit. They do
not retroactively become source/import authority for that commit.

## Selected graph and production boundary

The selected graph remains 16 non-main modules. The only selected version changes are:

| Module               | Previous  | Fixed selection | Why selected                       | Runtime classification |
| -------------------- | --------- | --------------- | ---------------------------------- | ---------------------- |
| `golang.org/x/mod`   | `v0.37.0` | `v0.40.0`       | exact direct test-gate requirement | graph-only             |
| `golang.org/x/tools` | `v0.47.0` | `v0.49.0`       | x/mod module edge                  | graph-only             |

Production closure identities did not change:

| Closure                            | Modules / packages | Modules SHA-256                                                    | Packages SHA-256                                                   |
| ---------------------------------- | -----------------: | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| Linux amd64/arm64, `CGO_ENABLED=0` |             7 / 30 | `48ca0dbaba0f918d99091decd0520a70327c36badb7d74c7cbbe1e180cd66e5f` | `12a56c91f56460e9757560f00234c06cec462f248df6a770d41172168e9a8d08` |
| Darwin arm64, `CGO_ENABLED=0`      |             6 / 29 | `12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` | `07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354` |

Linux amd64 and arm64 lists are byte-identical. Linux still links `x/sys/unix`; Darwin still excludes it. Root SBOM
`dependsOn` therefore remains the same seven Linux production modules. The graph-only x/mod/x/tools components stay
inventory-only and do not enter `THIRD_PARTY_NOTICES.md`.

## Fixed upstream bits and provenance

| Evidence             | `x/mod v0.40.0`                                                                   | `x/tools v0.49.0`                                                  |
| -------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Canonical tag commit | `d3398d06de5fa5c71083d3d1c26f2cda73508e0f`                                        | `18332fec72972efbb8ab9881984fec2d8cfc2b58`                         |
| Tag time             | `2026-08-13T19:09:22Z`                                                            | `2026-08-13T14:53:26Z`                                             |
| Minimum Go           | `1.25.0`                                                                          | `1.25.0`                                                           |
| Module sum           | `h1:hUv+3cXcdRHz08UmSiOob7sadHig73uo5bkXxQ/tvUs=`                                 | `h1:3NI7VXzL9+1WZD52Dx2ttoPwD5DWrFGpl9mFZDlmisI=`                  |
| go.mod sum           | `h1:0/weTWkPWGBikyTWAX3dkjVztMmBA5hM0DH6BElSupE=`                                 | `h1:SJNXV9DBKT0UbdttsQjbfJlAE/q+y36++zo3uL3N0Oo=`                  |
| Proxy ZIP SHA-256    | `a191080b1494c194059ae71aa1d300741a8e24dfb6e28cd32580dbdcc52a0598`                | `babd2d5a0ccf4eac49b6c9785be2b9e39b261097ad8b8a6b92578b9f248703d5` |
| Proxy go.mod SHA-256 | `7458cd1a66875b76fb962428ffb61aee84acfd871a32e56b4efb43b3b8d2a70d`                | `86e34efea35b8aa52aabff0ea18c0ef217f074dff67ef7b4c700ae833968cf62` |
| SumDB lookup SHA-256 | `c4fa37719eb23113d907f97d832d97563815435be69050a470c773959a59425e`                | `ac9c5957f1e3a8ea10e09dcf9b4aabe12f00cb3e6afc18f5934b00b86bcaba29` |
| License / SHA-256    | BSD-3-Clause / `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` | same                                                               |
| PATENTS SHA-256      | `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc`                | same                                                               |

The lock records the configured `https://goproxy.cn,direct` proxy and `sum.golang.org`; proxy ZIP/go.mod bytes,
module sums, and signed lookup responses were independently hashed. No replace, fork, vendor patch, exclude, or retract
is present.

## Advisory and vulnerability evidence

| Evidence                   | Exact result                                                               | SHA-256                                                                                                                                                         |
| -------------------------- | -------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| GO-2026-6179 official JSON | x/mod fixed at `v0.40.0`; transparency-log tile verification bypass        | `0c9188802ec6e0fe75d24c187ec7af3e75e9dee649fcba31caed55d6b14dc6ae`                                                                                              |
| GO-2026-6180 official JSON | x/mod fixed at `v0.40.0`; unrelated unauthenticated lookup hashes rejected | `440cf96581be963cc51ab4bb32029107d682138dfb2156d935132c981f76fa4d`                                                                                              |
| OSV querybatch             | 16 exact selected modules; 16 responses; 0 findings                        | query `d3582ad8e9b31aac6dc9fa2f548cf407f5b2a7858db16822129f0df56bb2450d`; canonical response `ab5a0787744e90d4b9bef630420e8085dd8045f7cd5fe87fc0b5acc7b6a55b93` |
| govulncheck module         | v1.6.0; DB `2026-08-14T16:22:54Z`; exit 0; no findings                     | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                                                                              |
| govulncheck Linux symbol   | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`; exit 0; no reachable findings     | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                                                                              |

These are fresh results for the fixed graph and source, not inherited zero-finding claims. Advisory databases and
SumDB signed tree heads are time-bound and non-bit-safe; a later release or Gate record must rerun them.

## Supply artifacts and notice boundary

| Artifact                                        | Decision                                                                           | SHA-256                                                            |
| ----------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/dependency-lock.json`   | 16-module selected graph; 7/9 production/graph-only split; fresh security evidence | `d6c2107a9878052953dff8b080a5d894f996f0a13f71f60fbac9c9829cffe482` |
| `services/control-plane/sbom.cdx.json`          | CycloneDX 1.6; 16 components; root depends on exactly 7 production modules         | `08b6435f9b4cd3f873e0a00871d9fc8f6dd70f85abc78e86920f9b96a3c1c152` |
| `services/control-plane/THIRD_PARTY_NOTICES.md` | production closure unchanged; exact same bits                                      | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` |

x/mod and x/tools remain not linked and not distributed, so adding their license text to the runtime notice would be a
scope error. Their SPDX/license/PATENTS identities remain machine-recorded in the lock/review inventory.

## Gates replayed

- exact Go `1.26.6`, Node `24.13.1`, and Bun `1.3.14` runtime pin checks;
- three-module `go mod tidy -diff`, `go mod verify`, and `go test ./...` through `platform:go:check`;
- control-plane `go vet ./...` and `go build ./...`;
- module-policy focused and race tests, including downgrade/indirect/directive bypass faults;
- Linux amd64/arm64 test-binary cross compilation for the policy gate;
- all package/script tests (`119/119` scripts), lint, typecheck, build, dirty-scope formatting, and `git diff --check`;
- fresh OSV and govulncheck module/Linux-symbol scans.

Full-repository formatting still reports only the two pre-existing, unchanged dependency-review formatting files. They
are not part of this security remediation and were not rewritten.

## Invalidation and Gate boundary

Any module version/sum, proxy bit, SumDB response, license/PATENTS text, Go toolchain, module graph, import/build tag,
test-gate scope, production closure, advisory DB, or scanner result change invalidates this review.

`G-SUPPLY-CHAIN` remains **IN PROGRESS**. A final immutable Gate record still needs the release-candidate source and
artifact identities, final binary/artifact scan, distribution manifest, and independent closure approval. Trusted-mount
authority, positive production `Open`, physical controller power-loss, runner/database completion, RC, Beta, GA, merge,
release, and deployment remain open or unauthorized.

## Primary references

- [`golang.org/x/mod v0.40.0`](https://github.com/golang/mod/tree/v0.40.0)
- [`golang.org/x/tools v0.49.0`](https://github.com/golang/tools/tree/v0.49.0)
- [`GO-2026-6179`](https://vuln.go.dev/ID/GO-2026-6179.json)
- [`GO-2026-6180`](https://vuln.go.dev/ID/GO-2026-6180.json)
- [`dependency-lock.json`](../../../../services/control-plane/dependency-lock.json)
- [`sbom.cdx.json`](../../../../services/control-plane/sbom.cdx.json)
- [`THIRD_PARTY_NOTICES.md`](../../../../services/control-plane/THIRD_PARTY_NOTICES.md)
