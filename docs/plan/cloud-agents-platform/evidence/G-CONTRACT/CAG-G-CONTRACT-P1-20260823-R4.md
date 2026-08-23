# Gate candidate record: `G-CONTRACT` / P1 / R4

- Evidence ID：`CAG-G-CONTRACT-P1-20260823-R4`
- Record type：`PHASE`
- Phase / aggregate Gate：P1 contract foundation / `G-CONTRACT`
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`、`CAG-G-BASELINE-P0-20260823-R4`
- Supersedes：`CAG-G-CONTRACT-P1-20260823-R3`
- Status：`IN PROGRESS`
- DRI：hxp0618（owner）；Codex current-source rebind executor
- Independent reviewer：`PENDING`
- Date：2026-08-23 Asia/Shanghai
- Gate effect：none；this is a current-source non-Gate candidate, not an immutable closure signature

## Why R4 supersedes R3

R3 repaired the contract-standards evidence, but its fixed prerequisite still named
`CAG-G-BASELINE-P0-20260810-R3`. Baseline R3 is now historical `INVALIDATED`; the current verified prerequisite is
Baseline R4. R3 also fixed `contracts/generation.lock.json` at `c1554d8...`, while the current source fixes the lock at
`4f29535...`. The latter includes migration `000012`, the current migration-bundle input and its receipt-only
live-instance retirement repair. A prerequisite-name substitution would therefore create false current-source evidence.

R4 freshly replays the current contract, generated registry/profile, SDK and migration-bound lock inputs. It preserves
R1-R3 as immutable history. It does not reinterpret any of the seven formal `missing` items, close the remaining
generator supply-chain review, or upgrade `G-CONTRACT` from `IN PROGRESS`.

## Fixed source and prerequisites

- Implementation source commit/tree：`1a9f15ee75a955beb993ab4bc534f4dab72d46a7` /
  `17bb5c849298cacedda875bd86bdc7f0d4c8186c`
- `services/control-plane` / `contracts` / `sdk` / `scripts` subtrees：
  - `689942aecbc7f84f692dd71c17d66a607d12b950`
  - `40f0f3b44f83c986f9b015d059451e195e285c0a`
  - `e4c5abf9d9cb591df39d9377529c201a1307997e`
  - `d65f14ec5cc8b2bda27af056673e891cda8cebd1`
- Source branch：`codex/cloud-agents-p0-baseline-r4-repair-20260823`
- Evidence branch：`codex/cloud-agents-p1-g-contract-r4-rebind-20260823`
- Source state before the evidence-only change：clean；upstream `0/0`；remote source branch exact
- Inventory R3 record / SHA-256：`CAG-G-INVENTORY-P0-20260810-R3` /
  `d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
- Baseline R4 record / SHA-256：`CAG-G-BASELINE-P0-20260823-R4` /
  `57429377291d1b6a41ff886cde2a6692afd63b5c15adf0677767d59e87b03dd9`
- Baseline R4 independent review SHA-256：
  `44db2df153bbfcc5fa0bd4c928bbdf9b207c60c4458ec61b2e2557c7d97d4c94`
- Gate-criteria / ADR-0007 SHA-256：
  - `4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`
  - `9e59b17e6f43db986ca4d5cc09ff62f4acb7cd8ebdfc61583d821ecdba11899c`
- Superseded R3 record SHA-256：
  `67268e58174f9c845d2599bac20e99aa2010d52d557c78b3982c13deae5aae85`
- Deployment profile：none；no production database, HTTP/P2/provider, deployment or publication action

## Contract, generator and lock identities

- `contracts/generation.lock.json` SHA-256：
  `4f2953540e9305f034a8f6fc7d13af0947d7f5b91f43b7ce6256bc137d071c76`
- Source-contract manifest：
  `sha256:eb51453861feb6685eadcd335c0620fea5ca98de9058a9a3d3f5198f6e67406e`
- Toolchain-authority manifest：
  `sha256:99c6358f0e8e3546bfb5f178f1e2a780aab0257fadeca17643b294a62106ca2a`
- `package.json` / `bun.lock` SHA-256：
  - `718d89ec962503c0d97df4b03ee068726f1ed7d9f03630d91ac756667595e9c1`
  - `cbd9d3dc8976c2dcbc340fb69f4dad250e788687bb252428dcd611b256fc884a`
- Contract-lock source / test SHA-256：
  - `a0dedeff03f1826a5b0c98c3467c2e862fd87d8b74fd038d9086af5ad6e6a4e0`
  - `bb752dac5e3d9714607843003845f193ec8caf5f5747d32b7a418f7344f48074`
- Standards profile / dependency-review SHA-256：
  - `0bd4348680cf48819658651539d4777c412e7a3fb93c380ffacf536c675f440d`
  - `62c66d64986e8fea38fb712cb091ae5e41aa1792823370616629acc36c39703d`
- Proto descriptor / exact breaking baseline SHA-256：
  `cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218` / same bytes
- Current migration input manifest：
  `sha256:65f4c66acd01eae8c9e1f2f2750df1d4e8fb3ad729969d91fb56d6cc5264f693`
- Migration schema/bootstrap/manifest digests：
  - schema：`sha256:54bd987183d6e2d8a7e3ba58a5fa5ee0666015a101193f363f671be294bb2907`
  - bootstrap：`sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c`
  - manifest：`sha256:454345322827369258f8496cce2c1e7f4d4b3e5b8b5f841f20c9fc84f53b3ddc`
- Lock shape：`27` tools、`27` pipelines、`status=BOOTSTRAP_VALIDATED`、`notGateClosure=true`
- Exact runtime tuple：Node `24.13.1`、Bun `1.3.14`、Go `1.26.6 darwin/arm64`、Python `3.14.7`、
  uv `0.12.5`、oxfmt `0.62.0`、Gitleaks `8.30.1`

Package, lock, standards, Proto and SDK dependency/license/NOTICE inputs are byte-identical to R3 where their source
inputs are unchanged. This is a same-bits identity statement, not a fresh supply-chain closure. The current lock still
requires source-tree binding at Gate time and does not bind immutable runtime executable/container digests for every
generator.

## Fresh current-source replay

The worktree installed the exact frozen `bun.lock` dependency graph from the local Bun cache. `node_modules` is ignored
and is not an evidence input. The combined replay used the existing hash-reviewed local standards wheelhouse:

```bash
env PATH=<Node-24.13.1>:<Bun-1.3.14>:<Go-1.26.6>:/opt/homebrew/bin:/usr/bin:/bin \
  CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE=/private/tmp/codex-contract-wheelhouse \
  bun run platform:contracts:check
```

Result：PASS. Production/bootstrap validation returned `118` JSON files, `52` schemas, `2` OpenAPI documents,
`3` Proto files, `71` fixture cases and `9` operations. The independent test-only validators returned official JSON
Schema mandatory-suite `46/383/1299/79`, current fixtures `52/2/71`, OpenAPI `2/9`, ten Python tests, zero failures,
`ALL_GATES_OPEN`, and `notGateClosure=true`. All registry/profile, identity, JSON and Proto generators and the current
generation lock reported `current`; no `__pycache__` or `.pyc` remained in the source tree.

Fresh contract/SDK replay:

```bash
bun test \
  scripts/lib/platform-contract-lock.test.ts \
  scripts/lib/platform-identity-sdk.test.ts \
  scripts/lib/platform-json-sdk.test.ts \
  scripts/lib/platform-proto-sdk.test.ts

(cd sdk/typescript && bun test && bun run typecheck)
(cd sdk/go && GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test ./... && \
  GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly go vet ./...)
bun scripts/test-platform-sdk-consumers.ts
```

Result：PASS after one bounded rerun. The initial four-file Bun invocation ran heavy generator tests concurrently and
three identity-SDK cases exceeded Bun's default five-second per-test timeout; it exited nonzero and is not counted as
PASS. The aggregate invocation reported `26` passing cases alongside the three timeouts. The identity file was then
replayed alone with `--timeout 60000` and passed `4/4` with `34` assertions. TypeScript SDK tests passed `20/20` with
`108` assertions plus `tsc --noEmit`; Go SDK tests and `go vet` passed; the isolated fresh TypeScript and Go consumers
passed.

Fresh migration-bound lock replay, without running PostgreSQL or the migration package:

```bash
bun test scripts/lib/platform-migration-bundle.test.ts
bun scripts/check-platform-migration-bundle.ts
bun scripts/generate-platform-migration-bundle.ts --check
```

Result：PASS. Tests passed `17/17` with `102` assertions; migration `000012`, all catalog statements, ancestor
strict-prefix, exact SQL classification and deterministic ustar inputs were current. The output remained
`UNPUBLISHED_BOOTSTRAP_MUTABLE` with runtime introspection and signing/publication `NOT_IMPLEMENTED`.

The current-source delta after R3 was scanned independently:

```bash
gitleaks git --no-banner --redact \
  --log-opts=e14780dff612f0d5ddf1513b1c0ae3bac6e9149a..1a9f15ee75a955beb993ab4bc534f4dab72d46a7
```

Result：PASS；13 commits、approximately `568.86 KB` scanned、zero findings.

`bun run platform:go:check` was also started, then intentionally stopped with exit `130` after process inspection
showed that it had launched a broad `go test -timeout=30m ./...`. That run is `NOT PASS` and provides no evidence.
The current implementation and contract subtrees were already fixed by exact identities; repeating the long migration
suite would add no criterion-specific information to this rebind.

## Exit criteria and open boundary

| Criterion                                     | R4 candidate result          | Evidence                                                                                 |
| --------------------------------------------- | ---------------------------- | ---------------------------------------------------------------------------------------- |
| current contract/bootstrap fixtures           | PASS AS CANDIDATE EVIDENCE   | `118` JSON、`52` schemas、`2` manifests、`71` cases                                      |
| official JSON Schema mandatory suite          | PASS AS CANDIDATE EVIDENCE   | jsonschema-rs `46/383/1299/79`；zero failures                                            |
| independent OpenAPI 3.1 validation            | PASS AS CANDIDATE EVIDENCE   | two documents、nine operations、zero errors                                              |
| production Ajv full official-suite compliance | `NOT_RUN_NOT_CLAIMED`        | no checked-in Ajv official-suite runner/result                                           |
| generated registry/profile replay             | PASS AS CANDIDATE EVIDENCE   | all generated registry/Go profile checks current                                         |
| generated SDK replay and isolated consumers   | PASS AS CANDIDATE EVIDENCE   | identity/JSON/Proto Go+TS replay；fresh packed consumers                                 |
| response unknown-field preservation           | PARTIAL CANDIDATE PASS       | SDK Go/TS binary/JSON sidecar tests；formal lock item remains missing                    |
| runtime path and tenant authority enforcement | NOT COMPLETE                 | production runtime/server closure is outside this candidate                              |
| N-minus-one compatibility                     | NOT COMPLETE                 | no deployed rolling N/N-1 closure                                                        |
| current migration-bound lock                  | PASS AS CANDIDATE EVIDENCE   | 27 pipelines；000012 exact bundle；no DB execution                                       |
| current generator dependency/license identity | PASS BY EXACT SAME-BITS HASH | package/lock/review/NOTICE identities unchanged                                          |
| remaining generator supply-chain review       | MISSING                      | no current vuln scan, all-platform wheel closure or immutable generator supply signature |
| immutable current-source independent review   | PENDING                      | this candidate awaits fresh P0/P1/P2 review                                              |

The lock retains these exact formal `missing` entries:

1. `json-schema-2020-12-official-test-suite`
2. `openapi-3.1-semantic-validation`
3. `generated-sdk-replay`
4. `n-minus-one-compatibility`
5. `response-watch-unknown-field-preservation`
6. `runtime-server-path-and-tenant-authority-enforcement`
7. `remaining-generator-supply-chain-review`

Fresh candidate evidence for an item does not silently rewrite the formal lock or turn it into a Gate closure.

## Rollback, cleanup and non-claims

- Only evidence documents are changed by this candidate; no contract, generated output, runtime or migration byte is
  modified.
- Temporary consumer installs and the standards virtual environment were removed by their runners. The ignored local
  `node_modules` is not committed and contains no credential input.
- No production database, migration, HTTP route, P2/provider effect, workload, endpoint, grant, deployment,
  publication, release, Beta, GA or Gate action occurred.
- No current vulnerability result, all-platform generator executable/wheel review, aggregate supply-chain closure,
  full migration suite, live PostgreSQL or runtime tenant-path enforcement is claimed.

## Invalidation

R4 becomes stale if Inventory R3 or Baseline R4 is invalidated; if the fixed source/tree/subtrees, contract source
manifest, toolchain manifest, generation lock, dependency lock, descriptor/baseline, migration-bundle identity or any
generated SDK/registry output changes; or if the Gate criteria/formal missing semantics change. A superseding candidate
must replay the changed criterion and receive a new independent review. R1-R3 remain immutable historical records.

## Sign-off

- DRI conclusion：the current source is rebound to valid P0 prerequisites and has fresh bounded contract/generator/SDK
  candidate evidence.
- Reviewer conclusion：`PENDING`.
- Closure decision：none；`G-CONTRACT` remains `IN PROGRESS`, all formal missing items and aggregate Gates remain open.
