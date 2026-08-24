# ADR-0018: P1 compatibility and recovery v2 PostgreSQL writer kernel

- Status: Accepted for implementation by owner approval on 2026-08-20
- Scope: A2.4 append-only PostgreSQL writer-kernel Slice B only
- Depends on: ADR-0008, ADR-0009, ADR-0015, ADR-0016, ADR-0017, and the
  approved A2.4 service-entry blocker

## Decision

The v2 generated compatibility/recovery profiles are consumed only by the new
forward migration `000011_add_compatibility_recovery_writer.sql`. Applied
`000010` bytes, its v1 helpers, tables, constraints, and generated catalog stay
unchanged. The writer migration does not reinterpret v1 rows or map the new v2
workload-principal identity onto the differently shaped v1 database-principal
row.

`000011` therefore creates an explicit v2 storage boundary:

- tenant-scoped workload-principal, backfill, restore-evidence, live-instance,
  and retirement-receipt tables;
- a tenant-scoped append-only transition-fact table that binds the exact
  operation, profile, identity, request digest, transition digest, result,
  database timestamp, state, version, and writer epoch;
- pure v2 registry/state-machine/policy/profile helpers; and
- exactly the 26 generated SQL operation names, with mutation wrappers and
  read-only reconcile/preflight wrappers separated by ACL.

All wrapper functions hard-code the generated profile and operation. No caller,
stored row, free-form action, or guessed schema selects a profile. Private
domain-transition helpers are owned by `cloud_agents_migration_owner`, receive
no grant, and are not a public capability.

## State, idempotency, and unknown outcomes

Every mutation uses database time and first locks the ordered
`(tenant, transition_digest)` tuple, then its `(tenant, profile, identity)`
tuple. Retirement alone subsequently locks the matching live-instance identity
before its receipt identity. This common lock order makes concurrent digest
reuse across different identities a closed conflict instead of a uniqueness
error. Each helper locks at most one row per domain, and performs no external
call while holding any lock. Epoch, incarnation, generation, count, cursor, and
state predicates are checked atomically.

The caller supplies a bounded request digest and a unique transition digest.
The durable transition fact is written in the same transaction as the domain
row. Exact duplicate facts are observable only through the generated reconcile
operation. A transport or commit ambiguity is classified by the future typed Go
service as `unknown` and `reconcile_required`; it must not invoke the mutation a
second time. Conflicting reuse of a transition digest is a closed rejection.

The PostgreSQL functions return only closed results (`applied`, `rejected`,
`conflict`, `observed`, `not_observed`, or `absent`). They never return a retry
instruction. The read-only preflight function rejects missing restore evidence,
incompatible live registrations, stale/fenced writer tuples, and registry or
schema-binding drift.

## Authority and ACL

The migration adds `cloud-agents-platform-global-table-authority/v4` while
retaining v1-v3 same-bits. Raw v2 tables are owned by the migration owner and
grant no privileges to runtime, bootstrap, or PUBLIC. Forced RLS binds all
table access, including SECURITY DEFINER access, to the transaction-local
tenant context.

- bootstrap authority receives EXECUTE only on the four workload-principal
  wrappers;
- runtime receives EXECUTE only on live-instance, retirement-receipt,
  migration-preflight, and pure registry helper wrappers;
- the migration-owner path alone can execute backfill and restore-evidence
  wrappers; and
- private role, lock, identity, transition, and domain helpers receive no
  non-owner grant.

Every SECURITY DEFINER function fixes `search_path` to
`pg_catalog, cloud_agents`. Bootstrap/runtime wrappers revalidate the session
principal closure; migration-owned functions require the dedicated
non-inherited migration membership and explicit migration-owner role path.

## Versioned lineage/quota profile

The generated `000011` bundle has 161 additional statement frames. Its exact
closed reservation is 32 segments, 3,281 journal records, 3,280 checkpoints,
523,632,640 journal bytes, 3,284 index records, 13,729,792 index bytes, and
537,362,432 combined bytes. The combined value exceeds the frozen v3 512 MiB
ceiling by 491,520 bytes even though the journal and physical lineage-index
limits both remain valid. The implementation must not relabel that bundle v3
or alter the v3 arithmetic.

`000011` therefore selects a new exact pair:

- manifest `cloud-agents-platform-migration-manifest/v4`;
- lineage/quota profile `cloud-agents-platform-lineage-quota-profile/v4`;
- distinct v4 quota-reservation, quota-bundle, admission-history, runtime
  inspection, and recovery binding domains.

V4 preserves the 32-segment, 65,536-record, 4 KiB checkpoint, 16 MiB physical
index, 16,384 index-record, root, and object ceilings. It raises only the
combined reservation ceiling to 528 MiB, the closed sum of the unchanged 512
MiB journal ceiling and unchanged 16 MiB lineage-index ceiling. V1-v3
manifests, profiles, digests, archived bundles, and replay arithmetic remain
same-bits. Empty, unknown, copied, cross-version, or profile-swapped authority
fails before database or filesystem mutation.

## Explicit non-claims

This slice contains no Go service consumer, HTTP route, P2/provider/worker
effect, session/turn/execution path, deployment, publication, or production
database write. PostgreSQL 15/16/17 tests use fresh local containers only.
Generated catalogs remain `UNPUBLISHED_BOOTSTRAP_MUTABLE`, runtime catalog
introspection/signing remain `NOT_IMPLEMENTED`, and all immutable and aggregate
Gates remain OPEN.
