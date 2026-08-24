# ADR-0019: P1 runner ledger preflight versioned contract

- Status: Accepted by owner approval on 2026-08-21
- Scope: runner ledger/catalog preflight Slice A plus the isolated, read-only Slice B kernel
- Depends on: ADR-0009, ADR-0010, and the approved runner ledger preflight entry blocker

## Decision

Runner ledger/catalog preflight proceeds as three ordered slices:

1. generate a versioned, pure `runner-ledger-preflight/v1` registry/profile;
2. add a later locked, read-only cumulative catalog projection kernel; and
3. add a later typed recovery/no-op binder, service/claim matrix, and independent review.

Slice A implements the generated state machine. Slice B is implemented only as
an isolated, package-private candidate pending independent review; it has no
production caller. The generated state machine has one unclassified state and
exactly five closed dispositions:

- `empty_brand_new`;
- `partial_next_entry`;
- `partial_retry_or_recovery`;
- `complete_return_success`; and
- `unknown_or_failed`.

The generated profile binds the exact current schema-bundle digest, signed
execution-lineage digest, ordered migration-prefix length/head/domain digest,
last-applied cumulative catalog digest, next-entry migration ID and entry digest
when required, evidence recovery state/action or explicit unavailability, and
the stored-corrupt/context/recovery-required/`NOT_IMPLEMENTED`/unknown error
precedence. Selection is by the generated profile ID and digest only. A caller,
stored row, guessed migration identity, or lossy mapping cannot select a profile.

Recovery state and action are not independently admitted enums. The generated
profile binds one closed disposition-to-recovery matrix (17 exact triples): a
fact is valid only when its ledger disposition, recovery state, and permitted
next action appear together in that matrix. In particular, a completed recovery
may only return success; it cannot be repackaged as a next-entry action. This
keeps inherited first-entry/retry/next-entry cases distinct from terminal,
dangling, ambiguous, divergent, and completed states without turning the pure
fact into recovery authority.

## Ordinary fact boundary

The package-private Go fact is an ordinary, copyable value. It has deterministic
RFC 8785 canonical bytes and a domain-separated SHA-256 subject digest. Its
constructor rejects zero, unknown, cross-profile, disposition-swapped, malformed
prefix, malformed next-entry, and tampered facts. It contains no database
session, transaction, advisory lock, evidence lease, verifier artifact, receipt,
writer token, or other authority.

Slice A deliberately has no production caller of the fact binder. The existing
runner continues to return `MIGRATION_PROJECTION_NOT_IMPLEMENTED` for partial and
complete ledgers. No complete/no-op or recovery dispatch is inferred from the
ordinary fact.

## Ordered slice status

Slice B reuses the existing dedicated session and advisory-lock order, projects
only the signed cumulative catalog selected by the exact prefix (or the signed
initial predecessor for an empty prefix), rereads the prefix, preserves
close/error precedence, and returns a copyable, integrity-sealed read-only
ordinary fact only after all
snapshot/session/unlock cleanup succeeds. It reconstructs the runtime view and
statement plans from private verified runtime inputs, rather than trusting public
`RuntimeBundle` or caller plan projections. It does not call `BeginMigration`,
open a SERIALIZABLE/RW migration transaction, append a ledger or evidence
record, commit, or enter the current writer. Projection queries continue to use
the existing runner-owned RR/RO snapshot transaction required by ADR-0010 and
must rollback it before the result is constructed. Its fixed-source independent
review is still required.

Slice C may begin only after Slice B is fixed. It must cross-bind the read-only
result with same-verifier evidence recovery facts and preserve a distinct
complete-return-success no-op path. Its matrix and independent review remain
required before any production consumer is considered complete.

## Explicit non-claims

These slices add no production runner consumer, migration/write transaction,
ledger/evidence mutation, production database write, HTTP route, P2/provider/
worker/session/turn/execution surface, deployment, release, or publication.
Slice B has only a package-private read-only projection candidate and no
production caller. The result has no identity registry or authority handle;
its canonical subject digest detects mutation while preserving ordinary value
copies. It closes no immutable or aggregate Gate; all Gates remain open.
