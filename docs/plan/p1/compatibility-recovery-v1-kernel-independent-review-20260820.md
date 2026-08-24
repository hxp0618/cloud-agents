# P1-A2.4 compatibility/recovery v1 and schema-only kernel independent review - 2026-08-20

- Status: **APPROVE bounded existing-artifact review — P0=0 / P1=0 / P2=0**
- Reviewed HEAD: `dc309f091107bdb55b1f0b86e5186a47ab86fd34`
- Reviewed implementation commits: `5a0ed7b654162ef61d6b22cdde18a54a778a2188` (generated v1 registry) and
  `48c93f9148986e031a98d6677830c8a084f0343b` (schema-only `000010`; the commit is reviewed by its checked-out
  tree and artifact hashes below)
- Branch: `codex/cloud-agents-platform-p1`
- Review mode: independent, read-only against the fixed existing artifacts
- Scope: generated compatibility/recovery contract/state-machine registry and append-only PostgreSQL schema-only
  kernel; this record does not authorize the next service-entry proposal

## 1. Verdict and boundary

The existing v1 generated registry and `000010` schema-only PostgreSQL kernel are internally consistent and pass
the bounded checks below. This is an implementation review of already-committed artifacts, not owner approval for
the proposed A2.4 v2 repair/writer/service sequence.

| Boundary                                       | Verdict                | Evidence                                                                                                    |
| ---------------------------------------------- | ---------------------- | ----------------------------------------------------------------------------------------------------------- |
| generated v1 registry and five closed profiles | PASS                   | source/profile/state-machine/policy digests are generated, cross-bound, deterministic and mutation-tested   |
| historical migration compatibility             | PASS                   | `000001` through `000009` are byte-identical to the pre-`000010` tree                                       |
| append-only `000010` schema kernel             | PASS                   | five owner-controlled tables, seven indexes and five pure digest/profile helpers; no writer or DML function |
| ACL and SQL authority boundary                 | PASS                   | runtime receives only `EXECUTE` on the pure helpers; table privileges are revoked; no `SECURITY DEFINER`    |
| local checks                                   | PASS with runtime note | registry checks, 27 focused Bun tests, migration bundle check and focused migration Go test pass            |
| HTTP/P2/provider/external effects              | PASS                   | no implementation is added in the reviewed commits                                                          |
| next A2.4 service-entry proposal               | **NOT APPROVED HERE**  | `compatibility-recovery-service-entry-blocker-20260820.md` remains owner-approval-required/proposed-only    |
| immutable and aggregate Gates                  | OPEN                   | this record closes no Gate                                                                                  |

## 2. Generated registry facts

The checked-in registry is a generated contract artifact, not a runtime capability. It contains five same-id closed
state machines and five bounded profiles for backfill, live registration, migration preflight, local restore, and
retirement. The inclusive historical schema range remains `000001..000009`; the implementation boundary continues to
declare no consumer, writer, HTTP surface or external side effect.

Reviewed hashes:

- source fixture: `c073227df8a2bdd5f2f247e993523e942d016c6edd3c226f9090b08428f5ca65`;
- generated registry: `f8a0ff0ebc91bab93b1bacf5ec6241f44c8639ae8a11dc0712b485f88156e812`;
- `ADR-0015`: `8dc5adf7c32518a150663b514eeaabf2b6a65a66173394135dda88b38d0d3305`;
- generation lock: `574bce3831ddd7fdbfc5ef94bb9b75c474882a8ddb13cb01103a09ba98df418c`.

The generated digest set is registry `9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d`,
source `55c232153ff9ee6e2d1929eca7c015567b264a3d9a2f5082cacf5b8b15122234`, state-machine
`5fb7f076c40aed31d5309a4de6aa2a66b93f3d560a535ecf992dc1f817d8f408`, and policy
`804ee0280ab5c98a48989abf511659d2a6f801fa5201617c3e436f848dfdc11d`.

The registry generator and focused semantic tests reject profile, state-machine, policy, retirement-proof and
implementation-boundary drift before generation. No caller-provided profile is treated as authority.

## 3. Schema-only `000010` facts

`000010_expand_compatibility_recovery_kernel.sql` binds the exact generated registry facts while remaining schema
only. It creates five owner-controlled tables, seven indexes and five `IMMUTABLE PARALLEL SAFE` helpers with a fixed
search path. The static classifier and migration bundle reject mutation functions, raw DML, triggers, `COPY`,
`SECURITY DEFINER`, table grants and external/runtime side effects. The SQL grants runtime only the five helper
functions and revokes table privileges from PUBLIC, runtime and bootstrap roles.

Reviewed hashes:

- SQL: `ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6`;
- global catalog: `20e84b349a70d3fe64a3b6b30b0e3458707018e9e4c2d356a52c4cb20b9b9a32`;
- `schema-000010.json`: `a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236`;
- migration manifest: `2d13d8175f3a3c6eaefc73b1172e1644833e4a7753bd42fa79086e9aa0dd7317`;
- schema bundle: `ca5fea1b9f0056439fd2b58af4a796616d9be3e7ec483869f1cb5bb4f5bfdbb8`;
- matrix script: `ca7235850f9b219076fc2a3499b62990b4dd47fff3a9085ca766c551d4f254e1`.

The implementation record retains the PostgreSQL 15/16/17 schema-only matrix evidence and exact image digests. That
matrix was not rerun during this read-only review; the SQL, classifier, catalog and script were rechecked instead.

## 4. Checks and limitations

Fresh checks in this review:

- `bun scripts/generate-platform-compatibility-recovery-registry.ts --check` — PASS;
- focused registry/lock/bundle tests — 27 passed, 133 expectations;
- `bun run platform:migrations:check` — PASS, `notGateClosure=true`;
- focused `go test ./internal/migration -run 'Compatibility|Recovery'` — PASS;
- historical `000001..000009` byte comparison — PASS;
- `git diff --check` — PASS.

The aggregate `platform:contracts:check` was not upgraded to a pass in this environment: its final contract-lock
step correctly rejected ambient Node `26.7.0` and Go `1.26.5` against declared Node `24.13.1` and Go `1.26.6` (Bun
`1.3.14` matched). Individual generated-registry checks passed before that toolchain guard. The pinned-toolchain
claim in the implementation record is historical evidence, not a fresh result from this machine.

The repository-wide migration closure remains outside this review and is not claimed as passed. The only unrelated
working-tree item, untracked `services/control-plane/migration.test`, was not inspected, changed, staged or included.

## 5. Next-entry boundary

This review does not approve `compatibility-recovery-service-entry-blocker-20260820.md` Section 3. Any versioned
registry repair, append-only writer kernel, typed service/claim consumer, or new matrix must receive explicit owner
approval and a separate independent review. No HTTP/P2/provider/external effect, production database write,
deployment, release, or Gate closure is authorized by this record.
