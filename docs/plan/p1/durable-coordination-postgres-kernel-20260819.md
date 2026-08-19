# P1-A2.3 append-only PostgreSQL coordination kernel - 2026-08-19

- Status: **LOCAL IMPLEMENTATION VALIDATED - SLICE 2 ONLY**
- Parent ref: `ff9ea337d5f5b79ebfd8662095a3998b1a54e05f`
- Branch: `codex/cloud-agents-platform-p1`
- Decision: [ADR-0013](../adr/0013-p1-durable-coordination-contract.md)
- Authorization: owner approval on 2026-08-19 for the ordered
  `contract/state-machine registry -> append-only PostgreSQL kernel -> service/claim/matrix/independent review`
  direction
- Does not claim: service or claim implementation, runtime write authority, HTTP/P2 or external side effects,
  independent review, production database mutation, deployment, release, or any Gate closure

## Implemented boundary

This slice adds append-only migration `000007_expand_durable_coordination_kernel.sql`. Its only coordination
profile is the generated `managedAgentCreateProject/v1alpha1` profile. SQL binds the exact generated registry,
state-machine, policy, and profile digests rather than accepting a hand-maintained or caller-selected profile:

| Generated fact            | Exact value                                                               |
| ------------------------- | ------------------------------------------------------------------------- |
| registry digest           | `sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30` |
| state-machine digest      | `sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15` |
| policy digest             | `sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8` |
| profile digest            | `sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a` |
| replay TTL                | `86400` seconds                                                           |
| creates PlatformOperation | `false`                                                                   |
| outbox class              | `resource_change`                                                         |
| external side effect      | `forbidden`                                                               |

Seven immutable SQL helper functions expose those exact registry facts. They are pure, non-`SECURITY DEFINER`
functions with no SQL mutation. Runtime receives EXECUTE on exactly those seven helpers; PUBLIC and bootstrap do
not. The migration creates the following migration-owned kernel tables:

- tenant-owned `platform_operations`, `operation_attempts`, `terminal_receipts`, `operation_finalizers`,
  `idempotency_records`, `outbox_events`, and `coordination_audit_facts`;
- global `leader_leases`, admitted by versioned `global-table-authority-v2` while the historical v1 artifact remains
  byte-identical for ancestor bundles.

All seven tenant tables enable and force RLS. Their tenant/resource links use exact foreign keys into existing
tenant and `resource_changes` facts. The schema fixes closed state/outcome/class/profile checks, bounded retry and
lease values, database-owned `transaction_timestamp()` clocks, append-oriented immutable identities, one
`operation_effect` outbox effect per operation, and redacted digest/reference facts only. No raw request, response,
credential, secret, or provider payload column is admitted.

This is intentionally a schema-only kernel. Runtime receives tenant-scoped SELECT over the eight new tables and no
INSERT, UPDATE, DELETE, TRUNCATE, sequence, or ownership capability. No service function can create, claim,
reconcile, finalize, or publish a coordination row in this slice. The current generated profile additionally has
`createsPlatformOperation=false`, so even a direct migration-owner test cannot manufacture a PlatformOperation for
that profile without violating a checked constraint.

## Versioned bundle and quota closure

The migration generator preserves all prior SQL and global-authority bytes. The exact `000006` schema bundle is now
an ancestor artifact; the current schema head selects the additive global-authority v2 contract. The Go runtime
decoder accepts only the two exact closed v1/v2 writer sets, so historical archives do not reinterpret the new
`leader_leases` authority.

| Artifact                              | Exact value                                                               |
| ------------------------------------- | ------------------------------------------------------------------------- |
| migration `000007` file SHA-256       | `f6f542f9f1df906217e76bff6f57af44441873dc654216a83bbb9a7af0ba0385`        |
| SQL statement count                   | `89`                                                                      |
| schema bundle digest                  | `sha256:8592d8f96dfeffea9379b1588dddd78909cd558db50b0d40157b7b780581544c` |
| manifest digest                       | `sha256:6194048fa8a13d1664dd421ad548d6808fb8eb97b33bb80ceb0b717f89331202` |
| runtime archive digest                | `sha256:2c0f963424b2a71311556911c0f25a5aced16167fa1902f394ed74623cfd66e7` |
| runtime archive size                  | `834048` bytes                                                            |
| bootstrap archive digest              | `sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175` |
| bootstrap archive size                | `32256` bytes                                                             |
| global authority v1 file SHA-256      | `d8330d06ead9a1cbc68c89e1741dcb3dc43d88d3e843590fea1ca56e242cb53d`        |
| global authority v2 file SHA-256      | `774fca639d56b984432cca1d0a013f2dee063bb7acaa53a09c3c57251f8f0d3b`        |
| archived `000006` bundle file SHA-256 | `8088b2ff98a7077ec98ca4f925c076501f9478b5b3aa1d8f976582d956884336`        |
| generation lock file SHA-256          | `120e973e0565f11ca6149198b9d7961da5fd3f0205453f8cd0c77746a0f5a1f7`        |

No quota limit was widened. The selected v2 profile admits the seven-migration bundle with exact derived values:

- reserved segments: `15`;
- reserved records: `1566` with checkpoint sequence `1565`;
- journal bytes: `248414208`;
- lineage-index records: `1569`;
- lineage-index bytes: `6705152`;
- combined reserved bytes: `255119360`.

Redundant owner changes and table ACL revocations were not counted as semantic migrations: the signed runner already
executes as the migration owner, PostgreSQL grants no table rights to PUBLIC by default, and the matrix asserts the
resulting catalogs directly. Function PUBLIC EXECUTE is explicitly revoked because PostgreSQL functions do receive
that default privilege.

## Local PostgreSQL evidence

The schema-only matrix starts fresh, exact local images, applies bootstrap plus migrations `000001` through
`000007`, and never pulls implicitly:

| PostgreSQL | `server_version_num` | Exact image digest                                                                 | Result |
| ---------- | -------------------- | ---------------------------------------------------------------------------------- | ------ |
| 15         | `150018`             | `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | PASS   |
| 16         | `160014`             | `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | PASS   |
| 17         | `170010`             | `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | PASS   |

Each leg checks exact registry/profile values, migration ownership, seven forced-RLS tenant tables, runtime
SELECT-only access, zero runtime DML, exact helper EXECUTE closure, bootstrap/PUBLIC denial, absence of secret
columns, tenant isolation, profile mismatch and TTL rejection, the current profile's no-operation rule, and the
inclusive `1..60` second leader-lease bound. This is not the future service/claim race/fault matrix.

## Local verification

Passed for the fixed local implementation:

- strict migration SQL classification and exact 89-statement catalog projection;
- deterministic migration bundle generation/check, ancestor-chain validation, archive determinism, and historical
  global-authority v1/v2 decoding;
- exact v2 lineage/quota reservation tests without limit changes;
- PostgreSQL 15/16/17 schema-only coordination matrix;
- full TypeScript tests plus focused Go bundle/contract/quota tests;
- `bun fmt` execution, dirty-scope format check, lint, typecheck, build, `git diff --check`, and dirty-scope secret
  checks.

The generation lock binds the generated registry as a migration input and records only
`sqlConsumer=GENERATED_PROFILE_DIGEST_CHECK_CONSTRAINTS_000007`. It deliberately retains
`runtimeConsumer=NOT_IMPLEMENTED`, `httpSurface=NOT_IMPLEMENTED`, `externalSideEffects=FORBIDDEN`, and
`notGateClosure=true`.

The full `internal/migration` Go package was also attempted and reproduced its documented repository-wide
10-minute timeout in `TestRunnerPreledgerProjectionFaultsRollbackWithoutAppendingEvidenceOrLedger`; no slice-2
assertion failed before the timeout, but that run is not reported as PASS. Focused tests covering every changed Go
consumer completed successfully.

The repository-wide history secret scan was stopped after a bounded silent run of more than eight minutes and is
not claimed. The same checked-in secret-shape rules passed over all 24 dirty/untracked files owned by this slice.
Full-worktree formatting still differs only in three pre-existing HEAD files outside this slice; `bun fmt` touched
them mechanically, they were restored byte-for-byte, and every slice-owned path passed the formatter.

## Explicit remaining boundaries

Slice 3 still owns all executable authority: typed service/claim/reconcile/finalizer/outbox functions, transaction
semantics, commit-unknown handling, race/fault matrices, service claims, and independent review. Runtime has no write
path until that slice supplies reviewed, profile-bound typed entry points. HTTP/P2 surfaces and every external side
effect remain forbidden. Production catalog signing/publication, production database mutation, deployment, release,
and all immutable or aggregate Gates remain open.
