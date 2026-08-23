# P1 current-source data recovery and retirement independent review — 2026-08-23

## Verdict

`APPROVE — P0=0 / P1=0 / P2=0`

This is an independent, read-only review of the fixed candidate. It approves the append-only preflight repair and its
bounded local evidence only. It does not close `G-DATA` or any aggregate Gate and does not authorize a merge,
production database write, remote host access, HTTP/P2/provider effect, deployment, publication or release.

## Fixed identity

- Candidate branch: `codex/cloud-agents-p1-live-instance-retirement-repair-20260823`
- Candidate commit: `91b8e59f4b2d35ab65b3b94e0ef025124a37ed56`
- Candidate tree: `94f98445c46c81f2c8d1a54e59dc7781f4b8f85c`
- Implementation commit: `fa3c568b61b3a8123a817049557f92951c5806a7`
- Implementation tree: `b1cebfa45cac3bbf4f578c01d97239595327127f`
- `services/control-plane` subtree: `689942aecbc7f84f692dd71c17d66a607d12b950`
- [Implementation record](p1-current-source-data-recovery-retirement-20260823.md) SHA-256:
  `388ae77b0758b8ddf85cb7d4ce7fe5c2ac20c9363b3441188d10cd3f232866cf`

The candidate was clean, had upstream divergence `0/0`, and matched the remote branch at opening and closing identity
checks.

## Fixed implementation hashes

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

## Receipt-only repair

Migration `000012` contains exactly one `CREATE OR REPLACE FUNCTION` statement targeting the existing read-only
`compatibility_recovery_migration_preflight_evaluate_v2` identity. PostgreSQL therefore retains the function owner and
ACL. The migration creates no table, role, grant, writer or new production entry point.

Both incompatible-unexpired and expired live-instance queries use the same fail-closed exclusion rule. A row leaves
the blocking set only when a retirement receipt matches tenant, service kind, instance ID, incarnation, rollout
generation and writer epoch; is `complete`; has all credential, endpoint, process, leader, claim and generation facts
true; and has a non-null receipt digest. Fencing alone is never sufficient. The time predicates are exhaustive at the
boundary: unexpired is `>= evaluated_at` and expired is `< evaluated_at`.

The existing registry/profile/current-writer/restore-evidence checks and error ordering are unchanged. Static SQL
classification and the checked-in validator reject a fenced shortcut, missing receipt binding, unexpected replacement
target, statement-count drift, DML, ACL changes and known external-effect tokens.

## Generated and historical closure

- The current manifest has exactly twelve entries, head `000012`, exact predecessor `000011`, and SQL digest
  `sha256:15577c7c1071b0369c1b528241c149ce3679ca8e11cc424382b0917cd2cda730`.
- The archived predecessor is byte-identical to the predecessor candidate's generated schema bundle, SHA-256
  `a01a22e09c7301aeafc87eb1f09b67cb844e5ac5bc5b3c6dd1e66827e348b90f`.
- Catalogs `000011` and `000012` have identical declared-object identity sets; only the exact replacement source is
  appended. No production authority is widened.
- The contract lock names the new SQL and focused matrix as direct fixed inputs and binds the current generated
  manifest, schema bundle, catalog and predecessor archive outputs.

## Focused evidence audit

The PG15/16/17 matrix applies `000001` through `000012` to a fresh exact-image, run-labelled container for each leg. Its
assertions cover active incompatible N-1, fenced without receipt, incomplete receipt, exact complete receipt, expired
fenced without receipt and expired exact complete receipt in order. Per-leg `PASS` is printed only after synchronous
owned-container removal and post-removal inspection; error and signal paths cannot produce a success line.

The current-source PG17 logical-recovery record binds the changed twelve-row manifest and reports exact source/restored
snapshot equality followed by idempotency, outbox and leader recovery. The existing recovery runner publishes success
only after synchronous container and temporary-dump cleanup. Current read-only inspection of the local OrbStack context
found no matching labelled container or temporary run directory.

The record distinguishes two pre-commit focused-harness failures from the corrected three-leg PASS, does not claim the
old broad writer/service matrices were rerun, and leaves production runner authority, PITR/HA, filesystem crash replay,
physical controller/cache-loss, immutable aggregation and every Gate open.

## Fresh checks and evidence boundary

- Exact Go `1.26.6` twelve-row data-recovery validator: PASS (`0.899s`).
- Focused matrix, logical-recovery runner and cleanup helper Bash syntax: PASS.
- Predecessor archive same-bits and `000011`/`000012` declared-identity equality: PASS.
- Target oxfmt, gofmt and `git diff --check`: PASS.
- Gitleaks over both candidate commits: PASS, `451.48 KB`, no leak.
- Candidate record, README and status tracker retain `G-DATA IN PROGRESS` and no Gate transition.

The Bun migration/lock short check was attempted in the dependency-less independent worktree and failed before tests at
Ajv module resolution. A second attempt with `NODE_PATH` failed at the same pre-test import boundary because Bun does
not use that path for this ESM resolution. Both attempts are environment NOT PASS and are not used as candidate
evidence. The checked-in candidate's recorded focused generator and exact lock-current results were instead audited
against the fixed hashes, generated outputs and lock input closure.

This review did not rerun PostgreSQL matrices, logical backup/restore, full migration, migration shards, broad
writer/service tests, broad race, remote database or any production operation.
