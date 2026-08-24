# Gate candidate record: `G-CONTRACT` / P1 / R2

- Evidence ID：`CAG-G-CONTRACT-P1-20260823-R2`
- Record type：`PHASE`
- Phase / aggregate Gate：P1 contract foundation / `G-CONTRACT`
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`、`CAG-G-BASELINE-P0-20260810-R3`
- Supersedes：`CAG-G-CONTRACT-P1-20260823-R1` because the fixed source changed
- Status：`IN PROGRESS`
- DRI：hxp0618（owner）；Codex current-source evidence executor
- Independent reviewer：`PENDING`
- Date：2026-08-23 UTC / Asia/Shanghai
- Gate effect：none；this is a non-Gate current-source candidate, not an immutable closure signature

## Scope and fixed source

R2 preserves R1's exact public contract/SDK/descriptor authority while adding a checked-in, versioned and
hash-locked independent standards pipeline. The pipeline runs the production Ajv/in-repo validator and an independent
JSON Schema/OpenAPI environment over the same current fixture inventory. It vendors exact official test-suite bytes,
but does not replace Ajv as the production validator or create a second runtime contract authority.

- Fixed implementation commit：`393074f9373dd87a1a84d12b3ae463a3e1251a40`
- Source tree：`b14d537072207a1bbf780c7cc3aae942f1b6376f`
- `services/control-plane` subtree：`c1d678f708ec231b446a11e46572a11fccefc97c`
- Source branch：`codex/cloud-agents-p1-g-contract-standards-entry-20260823`
- Source state before this evidence-only change：clean；upstream `0/0`；remote branch exact
- R1 evidence commit / record SHA-256：`086c659c3b5d6411eab89582349bffa57b7e1a1c` /
  `ebac580f39a7c3c397c1000a3b04abb7281732e45ade3f8aa75474c231334189`
- Gate criteria SHA-256：`4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`
- Inventory prerequisite record SHA-256：`d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
- Baseline prerequisite record SHA-256：`d8953c34b912757591881b4363fbb67855b2e578d669de4f697c97cbcb57ffd2`
- Deployment profile：none

The R1 source-contract manifest remains exact. The R1-to-R2 implementation delta adds only the standards checker,
its fixed test corpus/tool closure, generation-lock bindings and non-Gate documentation/configuration; it does not
change a JSON Schema, OpenAPI document, Proto source, descriptor, generated SDK, fixture or control-plane runtime
subtree.

## Contract, standards and tool identities

- `contracts/generation.lock.json` SHA-256：
  `6f3aae893394bd5c01005499b99e28376c3091d9f06ccf30f0cdb7ee974ca4dd`
- Source-contract manifest：
  `sha256:eb51453861feb6685eadcd335c0620fea5ca98de9058a9a3d3f5198f6e67406e`
- Toolchain-authority manifest：
  `sha256:99c6358f0e8e3546bfb5f178f1e2a780aab0257fadeca17643b294a62106ca2a`
- Standards pipeline input/source manifest：
  `sha256:6fc96beab00884b2bb0e723ee977630eef012d395c60ee2e6c0453c1e956dd2b`
- Standards profile SHA-256：
  `01ca4deb93d945d13987d68a7fd8a6aed459060e2be47360a6c27b01b7c79973`
- Checker / wrapper / checker-test SHA-256：
  `f9b5b80526e43a8f2293195e5b51abafbec6b99613917bd03e4538aa95ccfd3c` /
  `2cf9543308069525ecb96ecd4ba804c2c80916bbbef5022296e670f80ddee39b` /
  `b9515fdc7e4fff0caef018945a9b94e02b06f2f33a8b094a2ea3efe1d9309b25`
- `pyproject.toml` / `uv.lock` SHA-256：
  `b0a8b81937d783f1021e72f788f2b567769000ccef1bef044a2cb2b783646fb6` /
  `485c89d8f6bc03cc9eecf37003854d452439bd37b0252d43dcb1f8474cef6d49`
- Standards dependency-review SHA-256：
  `83fd2dac50423e7e6b8c6530ee4a3d8309b52aa98cc265b7452f96988a776452`
- Proto descriptor / exact breaking baseline SHA-256：
  `cd896a7aaccf70426be94314c5cfa09733a6908c33867c1f8942b4eb6b179218` / same bytes
- Managed Agent / Managed Host OpenAPI SHA-256：
  `91bcb0f7fc1ba66a7ed0b0db97303148287b5bd603212fe5a7916b1e564be954` /
  `4b5a7b9a9afa6f3c5b240c228040db8e1abf13f9fe982731546d87cf7e802997`
- Common / Platform fixture manifest SHA-256：
  `e89a1c0fba59337a7bd49548f44dc2a6a047a4e0c13a9d81214401a1fd0eee01` /
  `97ca05b981e76cb670c61cb5b4b6253f63174ce678208b2a8d7c819e34556799`

Official JSON Schema Test Suite identity:

- upstream：`json-schema-org/JSON-Schema-Test-Suite`
- commit：`ea54899edb898f4cd99fb0f778e0856e7d337c8f`
- full tree：`b41de18d1ce464a942ca78fd0e286cd726e9090f`
- mandatory Draft 2020-12 tree：`8caa839664321324a31424adf0dae3811a73e6da`
- vendored license SHA-256：`837402bd25fad9b704265801ca3f92566a98157c1f9a7acd6f446299ba1c305a`
- 126-file corpus manifest：`d69af29cbaf4c7ffd1f8577c986294c51ec4efb177ebc340022978043b2a88a1`
- manifest algorithm：`sorted-path-nul-sha256-nul-size-v1`

The vendored corpus contains the 46 mandatory top-level Draft 2020-12 files plus all 79 remotes and the MIT license.
It deliberately excludes optional suites. A path-scoped Git whitespace exemption preserves four upstream trailing
spaces; the complete corpus was independently compared byte-for-byte with the fixed upstream commit.

## Fixed execution environment

- Bun `1.3.14`
- Python `3.14.7`
- uv `0.12.5`
- `jsonschema-rs 0.50.1`
- `openapi-spec-validator 0.9.0`
- oxfmt `0.62.0`
- Gitleaks `8.30.1`

The exact 21-package Python closure is fixed by `uv.lock`; install uses exported hashes, `--require-hashes`,
`--no-build`, `--strict` and a temporary venv. The reviewed macOS universal2 `jsonschema-rs` wheel SHA-256 is
`300b154ded2be928d68b0f0408e0590b7e99af37019374fbe096b45bcd5eede1`. Python runtime download and source build are
forbidden. These packages are test-only and have no production Go/TypeScript import edge.

## Exit criteria delta from R1

| Criterion                                     | R1      | R2 candidate result                   | Evidence                                                                                    |
| --------------------------------------------- | ------- | ------------------------------------- | ------------------------------------------------------------------------------------------- |
| production schema/semantic fixture path       | PASS    | PASS                                  | Ajv/in-repo `118` JSON, `52` schemas, `2` manifests, `71` cases                             |
| official JSON Schema 2020-12 mandatory suite  | NOT RUN | PASS AS CANDIDATE EVIDENCE            | `46` files, `383` cases, `1,299` assertions, `79` remotes, zero failures                    |
| independent current-schema replay             | NOT RUN | PASS AS CANDIDATE EVIDENCE            | `52` schemas and all `71` manifest cases match their exact expected validity                |
| independent OpenAPI 3.1 semantic validator    | NOT RUN | PASS AS CANDIDATE EVIDENCE            | two OpenAPI `3.1.1` documents, nine operations, local refs, zero validation errors          |
| deterministic profile/corpus/tool lock        | NOT RUN | PASS AS CANDIDATE EVIDENCE            | exact corpus/profile/uv/source manifests and generation-lock `--check`                      |
| production Ajv full official-suite compliance | OPEN    | NOT CLAIMED                           | bounded audit observed `53` differences; independent oracle does not change runtime Ajv     |
| remaining generator supply-chain review       | OPEN    | IMPLEMENTED CANDIDATE, REVIEW PENDING | exact package hashes/licenses recorded; current advisory and final supply review not signed |
| immutable current-source independent review   | PENDING | PENDING                               | no independent reviewer has signed R2                                                       |

`contracts/generation.lock.json` intentionally retains the seven-entry bootstrap `missing` list, including
`json-schema-2020-12-official-test-suite`, `openapi-3.1-semantic-validation` and
`remaining-generator-supply-chain-review`. R2 records implementation evidence for the first two and an exact
toolchain candidate for the third, but does not remove formal gaps before independent review and immutable rebinding.

## Commands and results

Exact combined production/independent standards command:

```bash
CLOUD_AGENTS_CONTRACT_STANDARDS_WHEELHOUSE=<hash-verified-wheelhouse> \
  bun run platform:contracts:standards:check
```

Result：PASS. The production phase returned `BOOTSTRAP_VALIDATED`, `notGateClosure=true`, `118/52/2/3/71/9` and
source-contract manifest `sha256:eb514538...`. The independent phase returned
`INDEPENDENT_CONTRACT_STANDARDS_VALIDATED`, official-suite `46/383/1299/79`, current-schema `52/2/71`, OpenAPI
`2/9`, zero failures, `independentReview=PENDING` and `gateStatus=ALL_GATES_OPEN`. Eight fail-closed Python unit tests
also passed.

Lock and deterministic-source checks:

```bash
bun test scripts/lib/platform-contract-lock.test.ts
bun scripts/generate-platform-contract-lock.ts --check
oxfmt --check <changed JSON/TypeScript/Markdown paths>
git diff --check
```

Result：PASS. Lock tests passed `18/18` with `102` assertions; generation lock reported `current`; formatting and
diff checks passed. The checked-in official corpus matched the fixed upstream license, all remotes and all 46
mandatory files byte-for-byte. Static scans found no production import of the Python validators and no executable
network, database, route or provider edge in the checker/wrapper.

Fixed-candidate secret scan:

```bash
gitleaks git --no-banner --redact --log-opts=086c659c3b5d6411eab89582349bffa57b7e1a1c..393074f9373dd87a1a84d12b3ae463a3e1251a40
```

Result：PASS；one commit / approximately `525.69 KB` scanned；no leaks found.

The repository-root `bun x tsc --noEmit` command is not a claimed check: the repository has no root `tsconfig.json`,
so that exploratory invocation only printed TypeScript help. Modified TypeScript entrypoints were parsed/executed by
Bun through the successful checker, lock tests and lock generation. No full migration suite, migration shards, broad
race, live PostgreSQL, HTTP, Provider, deployment, release or publication run is claimed.

## Boundary and rollback

- The standards packages and official corpus are test-only; no runtime dependency or second model authority exists.
- No schema is fetched at validation time. Package installation may use an approved immutable cache/mirror but remains
  hash-locked; source build and Python download are forbidden.
- No production database write, HTTP/P2/provider effect, deployment, publication, release or Gate closure is
  authorized or performed.
- The implementation is isolated in one pushed branch and can be abandoned without changing any production system.

## Invalidation

R2 is invalidated by any change to its fixed source/tree/subtree, R1 or P0 prerequisite record, Gate criteria,
source-contract/toolchain/standards manifest, profile, checker, wrapper, dependency lock, official corpus, schema,
OpenAPI, Proto, descriptor/baseline, fixture, generated SDK or exact runtime identity. A contradictory independent
review also invalidates this candidate.

## Sign-off

- DRI conclusion：the current source now has reproducible production and independent standards evidence and is ready
  for a fixed-candidate independent review.
- Reviewer conclusion：`PENDING`.
- Closure decision：`G-CONTRACT` remains `IN PROGRESS`; R2 closes no Gate and grants no production or external-effect
  authority.
