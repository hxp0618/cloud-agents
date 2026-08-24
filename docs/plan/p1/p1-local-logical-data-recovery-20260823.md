# P1 local logical data recovery matrix — 2026-08-23

- Status: `IMPLEMENTATION PASS; INDEPENDENT REVIEW PENDING; G-DATA IN PROGRESS`
- Base candidate: `242ecef9334b6d76a621b21b97720180950cf9e8`
- Host-crash evidence import: `1057a22d5f1dc2a36d87e1802ac8fd9ad2476aa5`
- Implementation commit: `298879c403c4f4d50c6dacf4e857687896c74086`
- Implementation tree: `e54a62f17f3be0b846ac40c5619ead90216853ad`
- Cleanup remediation commit: `4e8c8f800c0f905e3a0bdd0cc9e2f2c3ef5c990c`
- Remediated code tree: `dac6d08581ed38db7dc7d7e3103131d99ac6ee6a`
- Remediated control-plane subtree: `d6df77d63bfbee7500e831dcd2414966209820d5`
- Branch: `codex/cloud-agents-p1-data-recovery-local-20260823`
- Independent reviewer: `PENDING`
- Gate effect: none; `G-DATA` and every immutable or aggregate Gate remain open

This record covers one local, label-owned PostgreSQL 17 logical backup/restore and durable-coordination recovery drill.
It implements the missing executable artifact identified by the P1 Gate gap audit: strict current-manifest migration
closure, a real logical dump and restore into a fresh cluster, exact restored-data comparison, and post-restore
idempotency/outbox/leader recovery. It is implementation evidence only. It does not claim an immutable `G-DATA`
signature, N/N-1 rolling closure, production migration-runner authority, PITR, HA, failover, physical power loss,
deployment, release, or any external service effect.

## Fixed implementation

| Path                                                        | SHA-256                                                            |
| ----------------------------------------------------------- | ------------------------------------------------------------------ |
| `internal/store/postgres/data_recovery_integration_test.go` | `bb09a201a8ed60b7a5be18e413468dc2cbe33f6d796155aacf5140822543f7df` |
| `scripts/data-recovery-validator/main.go`                   | `3471adbae5a7226b5f773cc61c2dfbbde8e68e7c33c2c1c9ceda618648607efb` |
| `scripts/data-recovery-validator/main_test.go`              | `19bcfda150e9c11f9ab966c576724bbea7d83c72883d58943e87eaf7fe499807` |
| `scripts/p1-data-recovery-cleanup.sh`                       | `2f18e9d7cb5d973f5be01c43829a00f5718ae39a4ece79cef46e5841df11d7b8` |
| `scripts/test-p1-data-recovery-cleanup.sh`                  | `7fde374a0ae2d7b602031843756c641f9b921221e3254bde93917314d8a84c27` |
| `scripts/test-p1-data-recovery-postgres.sh`                 | `96e58c257ff3a2540370bfd8ebf846b5d07982f805f2ec2c1443b6939222090c` |
| `migrations/manifest.json`                                  | `d716baa0e2e3dc8dfdb8acdf9d7732c564f4105826d28e76d417ec5432c63980` |
| `go.mod`                                                    | `a4d98dcbd65803a22bcf946cf042d17484e714500c0502b616d68742a02f1d14` |
| `go.sum`                                                    | `c5e16bfbadc2461fd349b94ce6487aadcb2edea11fa0aa37fd29bc2f46bfc88c` |

The validator strict-decodes the checked-in manifest with the existing migration contract decoder, rereads every SQL
artifact, verifies its exact size and SHA-256, and emits an ordered eleven-row PostgreSQL ledger. Every row has the
seventeen current manifest fields, including explicit `NULL` predecessor handling. It separately emits the transaction-
bounded apply script for migrations `000001` through `000011` and the exact ledger load.

The Bash 3.2-compatible runner requires Go `1.26.6`, refuses implicit image pulls, creates only two temporary containers
with a run-unique ownership label, binds the repository read-only, and removes only matching containers and the run
directory. Its local password and backup contain test-only data and are never written to a durable repository artifact.

## Real execution boundary

The successful run used:

- Docker client/server `29.4.0`, Linux/arm64 engine;
- GNU Bash `3.2.57(1)-release` on Darwin/arm64;
- Go `1.26.6 darwin/arm64` with `GOWORK=off`, `GOTOOLCHAIN=local`, and `GOFLAGS=-mod=readonly`;
- `postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193`;
- inspected image ID `sha256:8ee7c900f4de054e8f0a3b42b8f5da38b8fba57c3ccd7c1a02be28cb0b06494f`;
- exact PostgreSQL `server_version_num=170010`.

The runner bootstrapped source roles and database, applied all eleven current migrations, loaded and reread the exact
ledger, and seeded one tenant with organization/project/RBAC facts. Through the production durable-coordination service
it then created one succeeded idempotency record, one pending record, one claimed outbox event whose lease expired, and
one expired leader lease.

Before backup it generated a deterministic snapshot of every ordinary or partitioned `cloud_agents` table and every
sequence. It then executed `pg_dump --format=custom --compress=0 --no-owner`, created a second fresh PostgreSQL cluster,
bootstrapped only roles/database ownership, and ran
`pg_restore --exit-on-error --no-owner --role=cloud_agents_migration_owner`. The restored migration ledger was byte-
compared with the current generated ledger before any recovery action, and the full restored snapshot digest was exact:

| Fact                   | Observed value                                                     |
| ---------------------- | ------------------------------------------------------------------ |
| backup size            | `528924` bytes                                                     |
| backup SHA-256         | `e55aa6894b851ab7b957e8f5f72a4205a9b2e129ef388666613e33a7b7c2275d` |
| source data SHA-256    | `e6f1fbc53bb47b128f32c49f56a7ef5067f0b32d41f1a5d920c6da2a2136ee30` |
| restored data SHA-256  | `e6f1fbc53bb47b128f32c49f56a7ef5067f0b32d41f1a5d920c6da2a2136ee30` |
| recovered data SHA-256 | `11bbb16307c8b58973fcd1a803ee114c8889c04d318f411e0c8d16d18bfbf7ae` |

Recovery proved terminal idempotency replay, pending idempotency replay and terminal failure, leader reacquisition with
fencing token `2`, expired outbox reaping, reclaim, delivery acknowledgement, and an empty final outbox probe. Exact
post-recovery database facts required one succeeded record, one recovered terminal failure, one delivered event with two
delivery attempts, and the reacquired leader.

This is migration replay in the bounded data sense: strict current-manifest SQL closure plus exact restored eleven-row
ledger comparison. It is not a successful authenticated production `migration.Runner.Run`; trusted evidence provisioning
and runner authority remain separate reviewed prerequisites and are not synthesized by this test.

## Fail-closed execution history

The first two real executions are retained as `NOT PASS` evidence:

1. the first run rejected a seed that referenced absent resource-change identity `project-recovery-result`; the fixture
   was corrected to the existing `project-recovery` identity before any backup was accepted;
2. the second run completed restore and recovery but the final audit query used `cag_migration` without the required
   `SET ROLE cloud_agents_migration_owner`; the check failed with PostgreSQL `42501` and was corrected to set and reset
   the migration-owner role explicitly.

The third run produced the PASS facts above with predecessor runner SHA-256
`db435f1abe7d9e000f6c1d022889e4d12c6153f2f35e5bcc9f7008c8a0e14b56`. The first independent review correctly
blocked that candidate because an `EXIT` trap cannot propagate cleanup failure over an already successful Bash status;
container or dump removal failure could therefore leave residue after printing `PASS`. Remediation `4e8c8f8` makes
normal success explicit: it disables the EXIT trap, runs cleanup, propagates any cleanup failure, verifies the run
directory is absent, and only then publishes the prepared PASS line. A shared Bash helper plus a Bash 3.2 fixture prove
that successful cleanup precedes PASS and simulated cleanup failure returns nonzero without PASS.

The database setup, migration, snapshot, dump, restore, ledger comparison, recovery calls, image binding, and digest
calculation preceding cleanup did not change, but the runner bytes did. Therefore this record does not call the real run
a full remediated fixed-tree or Gate pass and does not repeat the real matrix. The remediated exact code received these
bounded checks:

- `/bin/bash scripts/test-p1-data-recovery-cleanup.sh`: PASS;
- `go test ./scripts/data-recovery-validator -count=1`: PASS (`0.825s`);
- `go test -race ./scripts/data-recovery-validator -count=1`: PASS (`1.595s`);
- `go vet ./scripts/data-recovery-validator`: PASS;
- `/bin/bash -n` over the cleanup helper, cleanup fixture, and PostgreSQL runner: PASS;
- `git diff --check`: PASS;
- scoped remediation-input Gitleaks scan: PASS over `2.42 KiB`.

No source/restore containers or run directories remained after the PASS. The temporary dump was deliberately deleted;
only its size and SHA-256 plus the source/restored/recovered data digests are retained here. This record contains no test
password, database URL, customer data, remote host identity, or production credential.

## Remaining `G-DATA` work and invalidation

This slice closes only the previously absent executable local logical backup/restore behavior. `G-DATA` remains
`IN PROGRESS` pending at least the complete N/N-1 rolling/live-instance retirement matrix, current-source filesystem
closure required by the accepted software-crash decision, a fixed aggregate current-source record, and independent Gate
review. PG15/16 compatibility, deployment PITR, HA/failover, and physical controller/cache-loss are not inferred from
this PG17 local drill.

This evidence is invalidated by any change to the fixed implementation or manifest inputs above, the dump/restore
options, canonical snapshot algorithm, PostgreSQL image/version, durable-coordination recovery semantics, database role
bootstrap, or the current `G-DATA` acceptance criteria. An independent reviewer must bind a later fixed candidate before
this implementation record may be used as reviewed evidence.

No production database write, remote host access, HTTP/P2/provider call, deployment, publication, release, main merge,
or Gate transition was performed or authorized by this slice.
