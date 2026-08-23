# G-CONTRACT R5-A formal closure profile independent review - 2026-08-24

- Review status: **APPROVE - P0=0 / P1=0 / P2=0**
- Fixed candidate: `d4bb96a7ab15138cedfb0753a44ebe8bf3488adf`
- Candidate tree: `2969a5049c5030c2d109b55232e578113f32c6ea`
- Parent: `9b4f19ce8ac96796c669ced37aa471a93b77ef1a`
- Candidate diff SHA-256: `ce2f3d38b858c7cb7c89f06adad5b7a003cce6cdf4a842732351360c12b3238d`
- Candidate branch: `codex/cloud-agents-p1-g-contract-r5-a-profile-20260824`
- Review mode: independent, fixed-candidate, read-only code/evidence review plus focused replay
- Gate effect: none; `G-CONTRACT` remains `IN PROGRESS` and every Gate remains OPEN

## Verdict

The fixed candidate is approved for the bounded R5-A generated formal-closure-profile slice. It replaces the
generation lock's hand-written missing inventory with a strict, versioned, generated profile while retaining exactly
three unresolved criteria. The review found no P0, P1 or P2 issue in this scope.

This verdict is not a `G-CONTRACT` signature and does not approve the three remaining criteria. It does not authorize
HTTP/P2/provider effects, production database writes, deployment, publication, release, or any Gate closure.

## Findings

| Priority | Count | Result |
| -------- | ----: | ------ |
| P0       |     0 | none   |
| P1       |     0 | none   |
| P2       |     0 | none   |

## Reviewed invariants

| Boundary                              | Result | Independent evidence                                                                                                                                                                                                                                                                  |
| ------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| fixed lineage                         | PASS   | candidate commit, tree, parent and binary diff SHA-256 reproduce the identities above                                                                                                                                                                                                 |
| strict source and output schemas      | PASS   | both schemas are Draft 2020-12 closed objects; the source constrains profile identity, status, derivation, implementation boundary, criterion IDs, path grammar, status-dependent review/reason shape and review verdict                                                              |
| exact classification                  | PASS   | the ordered inventory is exactly seven criteria; exactly `openapi-3.1-semantic-validation`, `generated-sdk-replay`, `n-minus-one-compatibility` and `response-watch-unknown-field-preservation` are `SATISFIED_CANDIDATE`                                                             |
| exact unresolved set                  | PASS   | generated profile and generation lock retain only `json-schema-2020-12-official-test-suite`, `runtime-server-path-and-tenant-authority-enforcement` and `remaining-generator-supply-chain-review`                                                                                     |
| review binding                        | PASS   | satisfied items require review records; the R4 review digest reproduces `f0d5b12f...458f61` and the SDK review digest reproduces `cd39fac1...9c1fe5`, with `APPROVE_P0_0_P1_0_P2_0` fixed by schema and runtime validation                                                            |
| JSON and Proto compatibility evidence | PASS   | N-1 and unknown-field criteria bind both JSON and Proto generator/test paths, including Go and TypeScript Proto unknown-field round trips, the exact Proto breaking baseline, and the previously approved SDK review                                                                  |
| path safety                           | PASS   | lexical escape, absolute/root escape, non-regular input, symlink components and realpath escape fail closed; directory expansion is sorted and recursively revalidates every child                                                                                                    |
| deterministic generation              | PASS   | NUL-framed domain-separated source/profile/registry digests are deterministic; checked-in output is byte-current and has no timestamp or absolute local path                                                                                                                          |
| generation-lock binding               | PASS   | profile source, schemas, generator, tests, implementation record and every declared authority/evidence/review input are sorted, unique inputs; the generated output path, SHA-256, size, profile/registry digests and derived missing list are recorded without a circular self-input |
| no manual lock missing inventory      | PASS   | `validatePlatformContractTree` obtains `missing` from the generated-profile builder; the generation lock copies that derived result, while v1's constants enforce the approved immutable classification rather than independently editing lock state                                  |
| generated SDK delta                   | PASS   | generated Go/TypeScript sources change only their contract-manifest header; generated manifests change only the deterministic contract/input/output digests caused by adding the two schemas/fixtures                                                                                 |
| forbidden surfaces and Gate state     | PASS   | the 29-file candidate contains no service/runtime, migration, HTTP, provider, deployment, publication or release implementation and changes no Gate tracker; the profile explicitly retains all external authority boundaries and `all_gates_open`                                    |

## Focused replay

The review used Node `24.13.1`, Bun `1.3.14` and Go `1.26.6`, with `GOWORK=off` and
`GOFLAGS=-mod=readonly` for generator/consumer commands that inspect Go. A temporary `node_modules` symlink pointed
only at the previously installed immutable dependency tree and was removed before commit.

| Command                                                              | Result                                                                 |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `git rev-parse HEAD^{tree} HEAD^` and binary diff SHA-256            | PASS; exact tree, parent and candidate diff identities reproduced      |
| contract-closure and contract-lock focused Vitest files              | PASS; 2 files, 25 tests                                                |
| contract-closure generator `--check`                                 | PASS; generated registry current                                       |
| contract-lock generator `--check` under exact pinned PATH            | PASS; generation lock current                                          |
| identity, JSON and Proto SDK generators `--check`                    | PASS; all generated artifacts current, Proto exact-baseline compatible |
| fresh TypeScript and Go SDK consumers                                | PASS                                                                   |
| in-memory lexical `../` source mutation                              | PASS; rejected by strict binding validation                            |
| in-memory source mutation pointing at a temporary repository symlink | PASS; rejected as a symbolic-link path                                 |
| `git diff --check`                                                   | PASS                                                                   |
| exact `oxfmt --check` over the changed TypeScript/JSON/profile files | PASS; 11 files                                                         |

An initial contract-lock/Proto replay with the host's ambient PATH correctly failed because it exposed Node/Go/Bun
versions different from the declared pins. Repeating the same checks with the exact pinned PATH passed. This is an
environment-selection observation, not a candidate finding, and no broad migration test was run.

## Remaining boundary

R5-A intentionally leaves the production Ajv official-suite result, runtime server-path/tenant-authority enforcement,
and current all-platform generator supply-chain closure unresolved. Later versioned slices and their own independent
reviews are required before those items can leave `missing` or before `G-CONTRACT` can be considered for closure.
