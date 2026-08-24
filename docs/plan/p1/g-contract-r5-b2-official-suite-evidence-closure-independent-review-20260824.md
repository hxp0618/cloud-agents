# G-CONTRACT R5-B2 official-suite evidence closure independent review

Date: 2026-08-24

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

The fixed candidate is approved for the bounded, generated
`contract-closure-profile/v2` evidence-classification slice. The review found no P0, P1, or P2 issue in scope.
This verdict does not claim Ajv generic conformance, satisfy either remaining criterion, authorize an external
effect, or close any Gate.

## Fixed lineage and scope

- candidate branch: `codex/cloud-agents-p1-g-contract-profile-v2-20260824`
- candidate commit: `5599f9d20e761532e08906eab1fc8384d48e5b8e`
- candidate tree: `3a9c5274bf9779b50720c20f39b61fe29228b84c`
- parent: `73ba42cb8d5d17833dd96532b2a527f9ed7250f9`
- canonical candidate diff SHA-256: `7a01d2e10b7e7ad28db3b5e00662faf38235b205ef594dca9e4b10fe7ecf600a`
- review mode: independent fixed-candidate code/evidence review plus focused replay
- Gate effect: none; every Gate remains OPEN

The 32-file candidate adds ADR-0026/D-050, strict v2 source/output schemas and a generated v2 registry, then updates
the deterministic contract manifest, generation lock, fixture inventory, SDK headers/manifests, and plan indexes.
It introduces no HTTP/P2/provider path, production database write, deployment, publication, release, or Gate
transition.

## Reviewed invariants

| Boundary                  | Result | Independent evidence                                                                                                                                                                                                     |
| ------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| append-only versioning    | PASS   | v2 is the active/default successor while explicit v1 build/current APIs remain; v2 generation first asserts all four frozen v1 artifact hashes                                                                           |
| v1 byte immutability      | PASS   | source `411e4b...a49`, source schema `8b87a0...c4af`, output schema `107dbc...bc0`, and generated registry `823e935...1e68` reproduce exactly and have no parent/candidate diff                                          |
| criterion meaning         | PASS   | ADR-0026/D-050 defines satisfaction as fixed-corpus and validator-authority evidence completeness; it explicitly denies Ajv generic conformance, waivers, accepted failures, and expected-failure relabeling             |
| independent oracle        | PASS   | jsonschema-rs `0.50.1` independently replays 46 files, 383 cases, 1,299 assertions, and 79 remotes with zero failures                                                                                                    |
| production Ajv disclosure | PASS   | Ajv `8.20.0` remains `EXECUTED_NONCONFORMANT`, `conformanceClaim=false`, 1,241/1,299 passed and 58 non-passing; ordinary replay is exact and conformance-required replay exits 1                                         |
| current-contract parity   | PASS   | the digest-bound current set contains 58 schemas, two manifests, and 77 fixtures with exact Ajv/in-repo versus jsonschema-rs result parity                                                                               |
| B1 review binding         | PASS   | the exact review path has raw SHA-256 `01b5c410879be0230e9a975a34c64242835905729adc413feed98b0d519413bb` and `APPROVE_P0_0_P1_0_P2_0`; generator validation rejects review digest or verdict drift                       |
| predecessor evidence      | PASS   | v2 binds the four exact predecessor hashes and retains the prior four satisfied criteria, their authority/evidence paths, and their fixed review records byte-semantically                                               |
| exact v2 classification   | PASS   | only the prior four criteria plus `json-schema-2020-12-official-test-suite` are `SATISFIED_CANDIDATE`; the derived missing list contains exactly the runtime/server and remaining generator-supply criteria              |
| fail-closed derivation    | PASS   | missing is derived from ordered criterion status; strict schemas/runtime checks reject manual removal, false parity, changed oracle totals, an Ajv conformance claim, stale output, bad review identity, and v1 mutation |
| generation-lock binding   | PASS   | both v1/v2 generated outputs, schemas, sources, fixed corpus/audits/reviews, exact evidence tuple, predecessor hashes, and two-item missing list are digest-bound without using this independent review as a self-input  |
| generated SDK delta       | PASS   | Go/TypeScript generated sources change only the deterministic contract-manifest header; associated manifests change only hashes induced by the two added schemas/fixtures and generated profile                          |
| Gate boundary             | PASS   | profile and lock retain `notGateClosure=true`, `gateStatus=ALL_GATES_OPEN`, runtime/HTTP and supply as not implemented, and production write/deploy/publish as not authorized                                            |

## Independent replay

Pinned runtimes used:

- Node.js `24.13.1`;
- Bun `1.3.14`;
- Go `1.26.6`;
- CPython `3.14.7`;
- uv `0.12.5`;
- jsonschema-rs `0.50.1`;
- openapi-spec-validator `0.9.0`;
- oxfmt `0.62.0`;
- oxlint `1.77.0`.

`GOWORK=off` and `GOFLAGS=-mod=readonly` were used for contract commands that inspect Go. No broad migration or
repository-wide test suite was run.

| Command                                                                           | Result                                                                                    |
| --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `git show -s --format='%H%n%T%n%P' 5599f9d...`                                    | PASS; exact candidate, tree, and single parent reproduced                                 |
| `git diff 73ba42c... 5599f9d... \| shasum -a 256`                                 | PASS; canonical diff SHA-256 reproduced                                                   |
| SHA-256 and parent/candidate diff of the four v1 files                            | PASS; all exact and byte-unchanged                                                        |
| focused Vitest for closure profile, generation lock, and Ajv official-suite audit | PASS; 3 files, 33 tests                                                                   |
| `bun scripts/generate-platform-contract-closure-profile.ts --check`               | PASS; v1 immutable and v2 current                                                         |
| `bun scripts/generate-platform-contract-lock.ts --check`                          | PASS; generation lock current                                                             |
| `bun scripts/check-platform-ajv-official-suite.ts --check`                        | PASS; exact non-Gate nonconformant audit current                                          |
| `bun scripts/check-platform-ajv-official-suite.ts --require-conformance`          | PASS boundary; `AJV_OFFICIAL_SUITE_AUDIT_CONFORMANCE_REQUIRED`, exit 1                    |
| `bun scripts/check-platform-contract-standards.ts`                                | PASS; 1,299/1,299 official assertions, 58/2/77 current parity, 10 Python tests            |
| `bun run platform:contracts:check`                                                | PASS; all contract registries, generated Go, SDKs, standards, audit, and lock are current |
| changed-file `oxfmt --check`                                                      | PASS; 26 matched format-supported files                                                   |
| repository-wide `oxlint . --deny-warnings`                                        | PASS                                                                                      |
| `git diff --check 73ba42c... 5599f9d...`                                          | PASS                                                                                      |

Repository-wide `oxfmt . --check` reports the same 134 pre-existing baseline files at both the fixed parent and the
candidate. None of the candidate's changed files appears in that set; the changed-file check passes. This is a
baseline observation, not a candidate finding.

## Remaining boundary

The v2 generated missing inventory intentionally retains exactly:

1. `runtime-server-path-and-tenant-authority-enforcement`;
2. `remaining-generator-supply-chain-review`.

Those items require their own implementation candidates and independent reviews. This approval alone is not a
`G-CONTRACT` signature and cannot be used to close a phase or aggregate Gate.
