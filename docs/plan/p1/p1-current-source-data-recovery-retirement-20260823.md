# P1 current-source data recovery and retirement preflight — 2026-08-23

- Status: `IMPLEMENTATION PASS; INDEPENDENT REVIEW PENDING; G-DATA IN PROGRESS`
- Base candidate: `282d4cd3f8a6783eb1dceb1c8cfecf3e4b7a367a`
- Implementation commit: `fa3c568b61b3a8123a817049557f92951c5806a7`
- Implementation tree: `b1cebfa45cac3bbf4f578c01d97239595327127f`
- Control-plane subtree: `689942aecbc7f84f692dd71c17d66a607d12b950`
- Branch: `codex/cloud-agents-p1-live-instance-retirement-repair-20260823`
- Independent reviewer: `PENDING`
- Gate effect: none; `G-DATA` and every immutable or aggregate Gate remain open

This record binds two changed-input local checks to one settled source: the append-only
`000012` live-instance retirement preflight repair and a fresh PostgreSQL 17 logical
backup/restore over the resulting twelve-migration manifest. It is implementation
evidence only. It does not authorize a production database write, deployment,
publication, HTTP/P2/provider effect, remote-host action, or Gate transition.

## Fixed implementation

| Path                                                                                                                            | SHA-256                                                            |
| ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql`                                             | `15577c7c1071b0369c1b528241c149ce3679ca8e11cc424382b0917cd2cda730` |
| `services/control-plane/scripts/test-compatibility-recovery-preflight-retirement-postgres-matrix.sh`                            | `2a910fef8d964ee97e9df053f50a966e17b13befafc3c7c691a5331626d9c960` |
| `services/control-plane/migrations/manifest.json`                                                                               | `97c02a54639d9a7d00dbc55a14e06db8e97bc2c36444cf51b61a680539cfd44e` |
| `services/control-plane/migrations/schema-bundle.json`                                                                          | `948e504b77c409065d2160056f45356d84d136d2512f35a4c4fe9e16e575aaaf` |
| `services/control-plane/migrations/catalog/schema-000012.json`                                                                  | `c424e9a62180c8e3de4cb444d95812c2606c6355065f4fa7e5655fcd733dab48` |
| `services/control-plane/migrations/archive/6dfd3fed7ba473e6a119a8b6ec3544d88b1a97a4bc5189a6536c64b6fba98110.schema-bundle.json` | `a01a22e09c7301aeafc87eb1f09b67cb844e5ac5bc5b3c6dd1e66827e348b90f` |
| `scripts/lib/platform-migration-bundle.ts`                                                                                      | `bc81e0ebbea03391301551a4dd6c8aba0707a503ff681f0198ca181ed0f4b35e` |
| `scripts/lib/platform-migration-bundle.test.ts`                                                                                 | `f5b4679f4f2dd91a3f58e5aaf1714b051d629352e4ccfe0befbc754ed44c246f` |
| `scripts/lib/platform-migration-sql.ts`                                                                                         | `39cc037db22138959ef18d614db51620e10a29e807efa1a0218b819407f4889a` |
| `contracts/generation.lock.json`                                                                                                | `4f2953540e9305f034a8f6fc7d13af0947d7f5b91f43b7ce6256bc137d071c76` |

The generated current bundle binds schema head `000012`, predecessor bundle
`sha256:6dfd3fed7ba473e6a119a8b6ec3544d88b1a97a4bc5189a6536c64b6fba98110`,
current schema-bundle digest
`sha256:54bd987183d6e2d8a7e3ba58a5fa5ee0666015a101193f363f671be294bb2907`,
manifest digest
`sha256:454345322827369258f8496cce2c1e7f4d4b3e5b8b5f841f20c9fc84f53b3ddc`,
and runtime tar digest
`sha256:5e5c34b6c6cda7467c4b1fb2527dd03695b6204ac9b26ffc42628e9bcd8e4c12`.
The contract lock directly includes the new SQL and focused matrix; it does not
depend on generated-directory discovery to bind either input.

## Retirement/preflight repair

The predecessor `000011` preflight excluded an incompatible unexpired or expired
registration whenever its live row was merely `fenced`. That contradicted the
accepted ADR-0007 boundary: fencing is not proof that credentials, endpoints,
processes, leadership, claims, and generation authority are all retired.

`000012` uses one `CREATE OR REPLACE FUNCTION` statement for the existing read-only
preflight identity. It keeps the current-writer proof and error ordering, but both
incompatible-unexpired and expired queries now exclude a registration only when a
retirement receipt matches the exact tenant, service kind, instance, incarnation,
rollout generation, and writer epoch; is `complete`; has all six boolean facts true;
and has a non-null receipt digest. The migration creates no table, grant, role,
writer, or external effect. The strict TypeScript validator rejects a fenced
shortcut, a missing receipt field, an unexpected statement/target, and any DML or
external-effect token.

The new focused matrix used the already-local exact PostgreSQL images:

| PostgreSQL | `server_version_num` | Image digest                                                                       | Result |
| ---------- | -------------------: | ---------------------------------------------------------------------------------- | ------ |
| 15         |             `150018` | `postgres@sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | PASS   |
| 16         |             `160014` | `postgres@sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | PASS   |
| 17         |             `170010` | `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | PASS   |

For each fresh owned container the matrix applied `000001` through `000012`,
recorded and completed exact restore evidence, activated one current N writer and
one incompatible N-1 writer, and proved this ordered state machine:

1. active incompatible N-1 rejects with `preflight_unexpired_instance_incompatible`;
2. fenced N-1 without a receipt still rejects with the same code;
3. a collecting/incomplete exact-instance receipt still rejects;
4. only the exact complete receipt approves;
5. a second fenced predecessor allowed to expire rejects with
   `preflight_expired_instance_unretired` until its exact receipt completes;
6. the final preflight approves only after that completion.

The successful focused matrix completed in `25.529s`. Two pre-commit harness runs
failed before acceptance: one supplied an extra activate/fence argument, and one
expected the returned `state` to be `observed` instead of the actual `approved`.
Both failures cleaned their owned container; the corrected fixed script produced
the three PASS legs above. No prior broad compatibility writer/service matrix,
full migration suite, shard runner, or broad race suite was rerun.

## Current-source logical recovery

Because adding `000012` invalidated the earlier eleven-row manifest evidence, the
local PostgreSQL 17 logical recovery runner was executed once against the changed
twelve-row source. It applied the exact current manifest, loaded and reread the
twelve-row ledger, created deterministic durable-coordination facts, performed a
custom-format `pg_dump`, restored into a fresh cluster, compared the complete
ordinary/partitioned table and sequence snapshot, then exercised idempotency,
outbox, and leader recovery.

| Fact                   | Observed value                                                            |
| ---------------------- | ------------------------------------------------------------------------- |
| PostgreSQL image ID    | `sha256:8ee7c900f4de054e8f0a3b42b8f5da38b8fba57c3ccd7c1a02be28cb0b06494f` |
| backup size            | `530169` bytes                                                            |
| backup SHA-256         | `1e097e466d38bf83a50a175535dfe477cbede17410e4f1d17bf2bb47a1cb7954`        |
| source data SHA-256    | `b64e578acccc1c3d2f49bd4185792b7154cb7736e607a43aedb827b9181c497a`        |
| restored data SHA-256  | `b64e578acccc1c3d2f49bd4185792b7154cb7736e607a43aedb827b9181c497a`        |
| recovered data SHA-256 | `a2d8e341c0fafe28874ea41b481e855aa7cd9e8e75930a6cf0b7455ec74afd36`        |
| Result                 | PASS in `18.501s`                                                         |

The runner synchronously removed both run-labelled containers and the temporary
dump directory before publishing PASS. A post-run residue scan found no container
with the test ownership label. No password, database URL, dump, customer payload,
or remote-host identity is retained in this record.

## Bounded checks and remaining boundary

- migration-bundle focused Vitest: `17/17` PASS;
- contract-lock recursive-input focused Vitest: `1/1` PASS;
- data-recovery validator Go test: PASS in `0.905s`;
- exact Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6` contract-lock `--check`: current;
- Bash 3.2 syntax, target oxfmt `0.62.0`, gofmt, and `git diff --check`: PASS.

This slice does not claim a full deployed rolling upgrade, traffic cutover,
production migration-runner authority, PITR/HA/failover, a current-source ext4/XFS
software-crash replay, physical controller/cache-loss, immutable Gate signature,
or aggregate closure. `G-DATA` remains `IN PROGRESS` pending current-source
filesystem/crash evidence aggregation and an independent fixed-candidate review.
