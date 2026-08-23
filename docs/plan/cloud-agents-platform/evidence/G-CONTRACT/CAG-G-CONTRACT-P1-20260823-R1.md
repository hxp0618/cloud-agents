# Gate candidate record: `G-CONTRACT` / P1 / R1

- Evidence ID：`CAG-G-CONTRACT-P1-20260823-R1`
- Record type：`PHASE`
- Phase / aggregate Gate：P1 contract foundation / `G-CONTRACT`
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`、`CAG-G-BASELINE-P0-20260810-R3`
- Status：`IN PROGRESS`
- DRI：hxp0618（owner）；Codex current-source evidence executor
- Independent reviewer：`PENDING`
- Date：2026-08-23 UTC / Asia/Shanghai
- Gate effect：none；this is a current-source candidate, not an immutable closure signature

## Scope and fixed source

This record binds the current JSON Schema/OpenAPI/Proto authority graph, generated Go/TypeScript SDKs, fixtures,
descriptor baseline and contract lock to one exact source. It also records the focused replay and fresh-consumer results
already obtained at that source. It deliberately does not convert bootstrap validation or earlier bounded slice reviews
into `G-CONTRACT = VERIFIED`.

- `cloud-agents` source commit：`2023f73b14aa57f1ded0c06006de20e6e2294141`
- Source tree：`f460b00faabf5400f4d065628da345bb46f7c962`
- `services/control-plane` subtree：`c1d678f708ec231b446a11e46572a11fccefc97c`
- Source branch：`codex/cloud-agents-p1-g-contract-candidate-20260823`
- Source state before this evidence-only change：clean；upstream `0/0`；remote branch exact
- Gate criteria SHA-256：`4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`
- Inventory prerequisite record SHA-256：`d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
- Baseline prerequisite record SHA-256：`d8953c34b912757591881b4363fbb67855b2e578d669de4f697c97cbcb57ffd2`
- Synara prerequisite source/tree：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0` /
  `ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- T3 main prerequisite source/tree：`8101cd044911c7dc2a2adf7c7a9ba7962abf57b6` /
  `e98f5650379f428bf5dcc6e7cae287c68fb8b080`
- T3 Cloud Agents prerequisite source/tree：`9584a266e91fa94354e8c07f79af3a5e01755d16` /
  `171624a2dbfb68f1d91f0a67175cbaf68f2947c2`
- Deployment profile：none

The Synara and T3 identities above are inherited only from the verified P0 prerequisite. No host repository was changed
or executed for this candidate.

## Contract and generated artifact identities

- `contracts/generation.lock.json` SHA-256：
  `4d1d58c1784a70480c8ce0c7865de4cbaadcc9b060a39c340629ff35acbd49c5`
- Source-contract manifest：
  `sha256:eb51453861feb6685eadcd335c0620fea5ca98de9058a9a3d3f5198f6e67406e`
- Toolchain-authority manifest：
  `sha256:8b39b6ac480c2d7d69d0d51a5faad4ebf278163376d4df242e8d6e32d577323f`
- Proto generation profile SHA-256：
  `41091415a46ee32d6566fcabd35a501526ced36785bdf01672d8379b2b0e1758`
- Proto descriptor set / exact breaking baseline SHA-256：
  `cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218` / same bytes
- Managed Agent / Managed Host OpenAPI SHA-256：
  `91bcb0f7fc1ba66a7ed0b0db97303148287b5bd603212fe5a7916b1e564be954` /
  `4b5a7b9a9afa6f3c5b240c228040db8e1abf13f9fe982731546d87cf7e802997`
- Common / Platform fixture manifest SHA-256：
  `e89a1c0fba59337a7bd49548f44dc2a6a047a4e0c13a9d81214401a1fd0eee01` /
  `97ca05b981e76cb670c61cb5b4b6253f63174ce678208b2a8d7c819e34556799`
- Go identity / JSON / Proto generated manifest SHA-256：
  `89a60bf047608910570afdc401648a60d1e021aec5580c9c88863fa8fff53734` /
  `7cf7e8eddac3fbbe2de5cbee50ab52ea29fb4bdb6ed5ac5d989ae99c590d6685` /
  `ceb1df7f72070b1b5aab5f16757136b381418d5ad827c9df30decc4273e9293d`
- TypeScript identity / JSON / Proto generated manifest SHA-256：
  `2541343d98afbfa409a6b25f01739ce1a1f1b44aca3e09e5b842688131040322` /
  `9e1f45de9924efdfe20b8f3ca8864a7d1d3d9001b6fa2a6233c67bcb0e25fecb` /
  `a523e1899b9bd1724acebffef5dd84212d9c23b4c1450a7448a3a537e8e2c9d1`
- Go / TypeScript SDK notices SHA-256：
  `7c73e29fe4b6bfdd700393923db47ac5ff733b621507dcaff01b429e3a4a88f8` /
  `6adc822035361a6eb61c6b1fd2646159b971eabaf2e85c121860ce7b0d32c432`

The lock remains intentionally marked `BOOTSTRAP_VALIDATED` and `notGateClosure: true`. The JSON SDK profile remains
`cloud-agents-json-contract-sdk/v1alpha1`; its authority profile remains
`cloud-agents-json-contract-sdk-model-authority/v1alpha1` with manifest
`sha256:4433d262ff189d9777d992262693f9edeac91a7c757bff4336888a7894782661`.

## Fixed toolchains

- Node `24.13.1`
- Bun `1.3.14`
- Go `1.26.6 darwin/arm64`
- protoc `35.1` as bound by the Proto generation profile
- oxfmt `0.62.0`
- oxlint `1.77.0`
- Gitleaks `8.30.1`

No Provider CLI/SDK, PostgreSQL, Kubernetes, image, chart, workload descriptor, production credential, SBOM/VEX or
release artifact is in this phase candidate's execution scope. Their absence is not recorded as a PASS for another
Gate.

## Exit criteria mapping

| Criterion                                                                                                                        | Result                     | Current-source evidence                                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| JSON Schema is the public JSON model authority; OpenAPI carries route/status/header/schema refs without a second model authority | PASS                       | generated JSON authority profile; two OpenAPI inputs; strict schema/ref and server-seam tests                            |
| versioned Proto is Worker/Platform Adapter wire authority and generates Connect/gRPC mappings                                    | PASS                       | three Proto inputs; pinned protoc/plugins; descriptor and breaking baseline are exact same bytes                         |
| descriptor, OpenAPI, schema/fixture and SDK identities are bound                                                                 | PASS                       | source-contract manifest, six SDK manifests, two fixture manifests and exact hashes above                                |
| Go/TypeScript/server mapping replay shared golden, negative and N/N-1 cases                                                      | PASS AS CANDIDATE EVIDENCE | focused identity/JSON/Proto/lock tests passed `33/33`; prior A3 implementation/review defines the shared fixture mapping |
| unknown fields, version downgrade, stable error, watch cursor, idempotency and deadline/cancellation                             | PASS AS CANDIDATE EVIDENCE | JSON sidecar/server tests, Proto raw-byte round-trip and cancellation tests, fresh fixture transports                    |
| Proto↔domain↔JSON shared semantic mapping                                                                                        | PASS AS CANDIDATE EVIDENCE | generated mapping/conformance fixtures; no alternate all-RPC JSON wire was introduced                                    |
| fresh external exact-pinned TypeScript and Go consumers compile and call fixture services                                        | PASS AS CANDIDATE EVIDENCE | fresh packed TypeScript consumer and local Go module-proxy consumer passed; no workspace/file/Git dependency             |
| deterministic generation leaves zero diff at fixed source                                                                        | PASS                       | JSON SDK generator and contract-lock `--check` current; generated source/manifests bind the runtime-only timeout repair  |
| bootstrap contract inventory remains internally valid                                                                            | PASS, NOT GATE CLOSURE     | `118` JSON, `52` schemas, `2` OpenAPI, `3` Proto, `71` fixture cases, `9` operation IDs                                  |
| official JSON Schema 2020-12 test suite                                                                                          | NOT RUN                    | remains an explicit closure gap                                                                                          |
| independent OpenAPI 3.1 semantic validator                                                                                       | NOT RUN                    | in-repo fail-closed subset passed; independent semantic validation remains open                                          |
| remaining generator supply-chain review                                                                                          | NOT RUN                    | dependency/notices are bound, but final independent generator supply review is not signed                                |
| immutable current-source independent review                                                                                      | PENDING                    | no reviewer has signed this R1 candidate                                                                                 |

`contracts/generation.lock.json` has a static bootstrap `missing` list. Three entries in that list—generated SDK replay,
N/N-1 compatibility, and response/watch unknown-field preservation—now have focused implementation evidence outside the
bootstrap checker. The list is not edited or silently reinterpreted here. The official JSON suite, independent OpenAPI
semantic validator and remaining generator supply review are still genuine closure gaps, and the lock itself remains
explicitly non-Gate.

## Commands and results

Exact runtime identity:

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node --version
/tmp/codex-cloud-agents-bun-1.3.14/bun-darwin-aarch64/bun --version
/Users/huang/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.6.darwin-arm64/bin/go version
```

Result：Node `v24.13.1`; Bun `1.3.14`; Go `go1.26.6 darwin/arm64`.

Bootstrap inventory:

```bash
bun scripts/check-platform-contracts.ts
```

Result：exit `0`; `BOOTSTRAP_VALIDATED`; `notGateClosure=true`; counts `118/52/2/3/71/9`; contract manifest
`sha256:eb514538...`; the unchanged static seven-entry bootstrap `missing` list was printed.

Focused generator and conformance replay at fixed source:

```bash
bun test \
  scripts/lib/platform-contract-lock.test.ts \
  scripts/lib/platform-identity-sdk.test.ts \
  scripts/lib/platform-json-sdk.test.ts \
  scripts/lib/platform-proto-sdk.test.ts
bun scripts/generate-platform-json-sdks.ts --check
bun scripts/generate-platform-contract-lock.ts --check
```

Result：the repaired fixed source passed `4` files / `33` tests in `65.64s`; both generators reported current.

SDK conformance:

```bash
(cd sdk/typescript && bun test && bun run typecheck)
(cd sdk/go && GOWORK=off GOFLAGS=-mod=readonly go test ./... && \
  GOWORK=off GOFLAGS=-mod=readonly go vet ./...)
bun scripts/test-platform-sdk-consumers.ts
```

Result：TypeScript `3` files / `20` tests and typecheck passed; Go tests and vet passed; fresh packed TypeScript and
local-module-proxy Go consumers passed. The temporary dependency-resolution link used in the isolated worktree was
removed before source freeze; it is not part of the tree or a consumer dependency.

Scoped source checks:

```bash
oxfmt --check <changed JSON/TypeScript/Markdown paths>
oxlint --type-aware <changed TypeScript paths>
gofmt -d <changed Go paths>
git diff --check
```

Result：PASS at the fixed source. No full migration suite, migration shards, broad race, live PostgreSQL, HTTP,
Provider, deployment or publication run was used for this record.

## Failures, retries and waivers

The first four-file focused replay reached `32/33`; `platform-json-sdk.test.ts` deterministic render exceeded Vitest's
default `5s` timeout. A narrow rerun showed that the deterministic render completed in about `65s`, rather than a
semantic mismatch. Source commit `2023f73...` changed only that test's explicit timeout to `120_000`, regenerated the
source provenance/manifests/contract lock, and then passed all `33/33` tests in `65.64s`.

This is not a waiver: the failed first result is retained, the fixed-source rerun is recorded separately, and the
generated output content remains semantically unchanged apart from regenerated provenance comments. No failing result
was relabeled as PASS.

The isolated worktree initially had no local `node_modules`; the SDK checks were retried using the existing exact-pinned
dependency installation and the temporary link was removed before commit. No production or external system was used.

## Rollback / cleanup evidence

- Source repair changes only test runtime bounds and generated provenance/lock bytes; no route, provider, database,
  deployment or publication path was opened.
- Fresh consumers and generated-output checks used temporary directories; no package was published.
- No database, endpoint, grant, workload or volume was created.
- This evidence change adds documentation only and can be abandoned by leaving its independent branch unmerged.

## Invalidation

This R1 candidate becomes `INVALIDATED` if any fixed source/tree/subtree, prerequisite record, Gate criteria, source
contract/toolchain manifest, schema, Proto, OpenAPI, descriptor/baseline, fixture, generated SDK, mapping, manifest,
notice or exact runtime identity changes. It is also invalidated by a contradictory independent review or discovery that
the fresh consumers depended on a workspace/file/Git edge.

Downstream P1–P6 records relying on these contract/model/mapping bits must be regenerated or explicitly rebound after
such a change. The previous A3 bounded review remains useful implementation evidence but is not a substitute for a
current-source reviewer signature.

## Sign-off

- DRI conclusion：current-source contract and SDK identities are reproducibly bound, and the focused candidate evidence
  is ready for independent review.
- Reviewer conclusion：`PENDING`.
- Closure decision：`G-CONTRACT` remains `IN PROGRESS`; no Gate is closed and no implementation, production database,
  HTTP/P2/provider, deployment, release or publication authority is granted by this record.
