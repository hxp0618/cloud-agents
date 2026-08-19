# ADR-0016: P1 compatibility and recovery PostgreSQL kernel

- Status: Accepted
- Scope: A2.4 schema-only PostgreSQL kernel (`000010`)
- Depends on: ADR-0008, ADR-0009, ADR-0014, ADR-0015

## Decision

A2.4 binds the generated compatibility/recovery registry to an append-only
PostgreSQL schema migration. `000010` is a forward migration after `000009`;
the nine predecessor migrations remain immutable byte-for-byte inputs. The
registry, state-machine, policy, and five profile digests are exposed through
five `IMMUTABLE PARALLEL SAFE` SQL helpers. The helpers return generated facts
only; they do not read tables and do not create an authority channel.

The migration adds five owner-controlled tables for workload principal
registration, bounded migration backfill state, local logical restore evidence,
live-instance compatibility, and instance-retirement receipts. Foreign keys,
profile-bound checks, version/range checks, completion invariants, and bounded
digest/identifier profiles reject contradictory stored facts before any future
service consumer can use them. The `schema_restore_evidence` timestamp rule
requires the recorded drill to be no later than evidence creation and keeps
updates monotonic.

## Authority and dependency boundary

The generated registry remains the contract source and is not edited by the
SQL migration. `000010` consumes its exact generated output and the v3 global
table authority projection. Future writers are named by the catalog only:
`cloud_agents_migration_owner` for the migration/backfill path, an audited
admin path for restore evidence, a typed live-instance registration function,
and a typed instance-reconciler function. None of those service consumers is
implemented by this slice, and PostgreSQL must not infer one from a table row.

The migration owner is the only owner of the new tables. Runtime, bootstrap,
and PUBLIC receive no table privileges. Runtime receives `EXECUTE` only on the
five pure helpers, with PUBLIC and bootstrap execution revoked. The migration
contains no `SECURITY DEFINER`, mutation function, DML, trigger, HTTP/P2
adapter, provider/worker/session/turn/execution path, or external side effect.

## Recovery boundary

Recovery is limited to local logical backup/restore and preflight evidence.
PITR, HA, failover, remote object storage, and production database writes remain
outside this slice. A fresh PostgreSQL 15/16/17 matrix applies every migration
in a new database, verifies ownership/ACL/helper volatility, inserts only
deterministic migration-owner facts, and exercises negative constraint and
foreign-key cases. A local matrix is implementation evidence only; it does not
close independent review, immutable closure, or any aggregate Gate.

## Acceptance and non-claims

- Generated registry/profile/schema/manifest checks and the migration bundle
  must remain deterministic and source-bound.
- The matrix must prove the new tables are not writable by runtime or
  bootstrap, the helpers are pure and non-definer, and constraint failures do
  not create partial rows.
- No Go service consumer or HTTP/P2/external-side-effect path is added.
- `000001` through `000009` and their historical catalog evidence remain
  immutable.
- All Gate statuses remain OPEN; this ADR records a local implementation slice,
  not a release, deployment, or production authority decision.
