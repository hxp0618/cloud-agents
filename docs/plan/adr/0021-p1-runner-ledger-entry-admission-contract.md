# ADR-0021: P1 runner ledger entry-admission versioned contract

- Status: Accepted under the standing Platform P1 execution approval on 2026-08-22
- Scope: generated entry-admission profile and a fresh-session read-only permit boundary
- Depends on: ADR-0019, ADR-0020, and the runner ledger consumer/writer entry blocker

## Decision

Add a distinct generated `runner-ledger-entry-admission/v1` registry. The immutable
`runner-ledger-preflight/v1` and `runner-ledger-consumer/v1` registries remain byte-identical. The new registry binds
the exact generated consumer-v1 identity and admits only the five consumer pairs already classified as entry work:

- the three `empty_brand_new` pairs; and
- the two `partial_next_entry` pairs.

All five map to `prepare_entry_admission`. Complete-ledger no-op, retry, recovery, reconcile, failure, unknown,
caller-selected, copied, or cross-profile inputs do not enter this state machine.

## Fresh-session authority boundary

An entry admission may be minted only inside one closed package-private call that consumes an exact
same-verifier entry consumer fact and then opens a fresh dedicated database session. While the signed advisory lock
is held on that same session, the kernel must repeat and cross-bind:

1. connected-session authority;
2. signed role and settings;
3. migration-role authority;
4. exact ledger-prefix length, head, rows, and digest;
5. the exact initial predecessor or signed cumulative catalog projection;
6. the exact next migration entry and its complete statement-plan closure;
7. candidate, generation, journal, recovery, schema-bundle, and runner-projection identities; and
8. a final ledger, catalog, and evidence reread immediately before sealing.

The permit retains the session and advisory lock only so it can prove exact cleanup. It is registry-backed,
non-copyable, and one-shot. In this slice its only production transition is `close_without_mutation`; it has no
`BeginMigration`, SQL, ledger insert, evidence append, transaction-opening, or writer method. The public runner
therefore still returns stable `MIGRATION_PROJECTION_NOT_IMPLEMENTED` for every entry action after the permit is
closed successfully.

Stored contradictions take precedence over `NOT_IMPLEMENTED`. Context, authority, projection, lock, filesystem,
unlock, close, or response-lost uncertainty invalidates the whole permit and also takes precedence. No failed or
unknown attempt may be retried as a second transition from the earlier ordinary fact.

## Ordered slices

### Slice A - generated registry/profile

Generate and validate the versioned JSON registry and package-private Go profile. Mechanically prove both existing
runner ledger v1 generated outputs are unchanged. This slice has no database session, permit, caller, or mutation
surface.

### Slice B - fresh-session read-only permit

Implement the fresh locked revalidation and one-shot close-only permit. Keep every entry and recovery writer
`NOT_IMPLEMENTED`; prohibit transaction, SQL, ledger, and evidence mutation call edges.

### Slice C - matrix and independent review

Cover all five admitted pairs, all twelve non-entry/unknown pairs, stale/copy/literal/cross-profile inputs,
ledger/catalog/evidence drift, lock and cleanup faults, context interruption, second consumption, and forbidden
surfaces. Record a fixed-candidate P0/P1/P2 independent verdict before advancing canonical status.

## Explicit non-claims

This decision does not implement or authorize entry execution, retry, recovery, reconciliation, migration/RW
transactions, SQL, ledger or evidence mutation, HTTP/P2/provider surfaces, production database writes, deployment,
publication, release, or any Gate closure.
