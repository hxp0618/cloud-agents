# Runner ledger recovery retry-handoff service matrix — 2026-08-23

- Status: `SLICE_E_FIXED_IMPLEMENTATION_PENDING_INDEPENDENT_REVIEW`
- Approved Slice D candidate: `7bbc39185f11e72223e40a081e6dd62b23e53de8`
- Slice D independent review: `cb94b53f95b31edb79ce453f8992e5fbed0ce956` — `APPROVE, P0=0/P1=0/P2=0`
- Slice E code commit: `facf4ec06bf679b0cdc4431b78c54ce2f60aaa11`
- Slice E code tree: `c84a6e8eaa7d7dd703a4f511a05b238a1fbe3633`
- Slice E control-plane subtree: `f3b19b905f1ac497f0c451d804fadee6dd83f37f`
- Slice E branch: `codex/cloud-agents-p1-runner-recovery-retry-handoff-20260823`
- Decision: [`D-047 / ADR-0023`](runner-ledger-recovery-contract-decision-20260822.md)
- Gate effect: none; every immutable and aggregate Gate remains open

This record covers only ADR-0023 Slice E's versioned `runner-ledger-retry-handoff/v1` transition from one exact
retryable or resolved-pending durable boundary to a header-only inherited successor generation. It does not execute
the inherited attempt, append an entry result, return a public recovery success/failure, or implement Slice F/G. It
does not authorize production database writes, HTTP/P2/provider behavior, deployment, publication, release, main
merge, or Gate closure.

## Closed boundary mapping

The generated retry-handoff profile remains distinct from recovery execution-admission and recovery success-writer.
It can consume only the generated `prepare_retry_handoff` action and can call neither execution identity.

| Durable old-generation state | Exact stored boundary                                | Supersession outcome          | Exact successor continuation                                            |
| ---------------------------- | ---------------------------------------------------- | ----------------------------- | ----------------------------------------------------------------------- |
| `terminal`                   | `aborted_retryable`                                  | `precommit_aborted_retryable` | same migration, attempt `N+1`, previous/source terminal exact           |
| `terminal`                   | `ambiguous_reconciled_pending`                       | `exact_pending`               | same migration, attempt `N+1`, previous/source terminal exact           |
| `terminal`                   | adjacent `ambiguous_unresolved` → `resolved_pending` | `resolved_pending`            | same migration, attempt `N+1`, unresolved terminal and resolution exact |

Every row requires attempt `N < maxAttempts`, the latest durable checkpoint to be the lineage-index tail, the exact
journal tail, migration/entry identity, old generation identity, historical recovery policy, current successor
decision/schema binding, and `begin_next_attempt`. Terminal, divergent, committed, unresolved-without-resolution,
wrong-resolution, wrong-tail, stale-checkpoint, skipped-attempt, and max-attempt boundaries mint no handoff.

## Read-only admission and successor sequence

The package-private service performs this sequence:

1. accept only a fresh `activeGenerationAncestorRecovery` session whose ALL-history recovery execution binding ties
   the old generation to the exact current candidate decision and successor schema;
2. project the database and catalog with the current candidate bindings while retaining the old generation identity
   separately in the evidence claim;
3. consume the generated recovery-admission profile-4 permit, then repeat the locked ledger, catalog, role, session,
   advisory-lock, runtime, plan, evidence, and retry-boundary validation;
4. unlock, reset, and close that exact read-only database session before minting any successor mutation authority;
5. seal a database-handle-free receipt and one one-shot, registry-backed, owner/candidate/generation/cursor/boundary
   permit; literals, copies, foreign binders, mutations, and second use fail closed;
6. let only the concrete generation evidence session consume the permit, reread the still-locked old journal boundary,
   reconstruct the exact historical policy and checkpoint supersession evidence, and bind
   `VerifiedLineageSupersessionAuthority`;
7. call the existing reviewed `ReserveAndActivateSuccessor` transition exactly once; and
8. accept only a current, header-only `brand_new_inherited` successor at sequence 1 with the exact current decision,
   schema, migration, attempt `N+1`, previous terminal, lineage continuation, fresh cursor, and revoked old cursor.

No Slice E file imports `database/sql`, `pgx`, or `net/http`. The new kernel has no `BeginMigration`, statement/ledger
writer, `AppendDurable`, HTTP/P2/provider, deployment, or publication edge. Its sole mutation edge is the concrete
evidence session's existing successor-generation transition.

## Failure, crash, quota, and cursor closure

- Failure or uncertainty before the final read-only database close mints no successor permit. Close uncertainty
  dominates, the binder is not called, and the still-unmutated old cursor remains available only for a fresh reopen.
- Once a handoff permit is consumed, a context/operational error, unknown successor outcome, or returned successor
  contradiction revokes the old cursor; a contradictory new cursor is also revoked and the result is
  `MIGRATION_EVIDENCE_RECOVERY_REQUIRED` or the stable operational code.
- The delegated successor transition already performs fresh lease reacquisition, ALL-history replay, versioned quota
  recomputation, exact reservation, adjacent supersession/reservation/header/activation, handoff/replay, and cleanup.
  Slice E does not duplicate or weaken that kernel. Existing focused successor tests bind quota exact/+1, stale
  authority, crash/reopen, transition order, and old-cursor revocation to the same call reached here.
- A crash after any durable successor sub-step is recovered by the existing registered/historical successor graph;
  this Slice does not claim a new filesystem implementation or a live power-loss run.
- Recovery execution remains disconnected. After a successful handoff the current public consumer reaches the
  separately generated Slice F boundary and still returns `MIGRATION_PROJECTION_NOT_IMPLEMENTED`.

## Focused conformance

The fixed code commit was checked with Node `24.13.1`, Bun `1.3.14`, and Go/gofmt `1.26.6 darwin/arm64`:

- exact named Slice E normal suite: PASS in `103.781s`;
- exact narrow one-shot/unknown-result race suite: PASS in `92.983s`;
- recovery Go generator and generation-lock checker: current;
- `go vet ./internal/migration`, `go build ./internal/migration`, changed-file `gofmt`, target `oxfmt`, and
  `git diff --check`: PASS.

The generation lock SHA-256 is `e5d0c79be12b1109d3577c9cd50b6cebd3d6379b7d6ed8dee035a6c4a9a3bdd3`. Its only change from Slice D is the recovery Go profile suite
input-manifest SHA-256, now
`sha256:eeb198248c6bfeb6af1689a91eca852261f06cb601e2b8bcd340dff7f35747a0`, because the profile test now binds the
single generated retry-handoff consumer and the sole successor-transition edge. Generated registries, generated Go
profile bytes, historical runner v1 bytes, and the 12-pair mapping remain unchanged.

Key implementation SHA-256 values:

- retry-handoff kernel: `c7030dbbb40d5f31cb833dcf78daf8efd91801b853af2d860b2f201e38a74ce5`;
- retry-handoff tests: `c98cef95e19db6c744097c2858d950ba2eec86cf0f52599baf97eb39522e1ec2`;
- preflight service: `7d27ad0af1df760a93511053eec458409e942963630e3414745d5c19ca605caa`;
- recovery admission routing: `2717764e06d91afab43558499597b0d2d0aba588f0b852f8d978fee5212a9e6a`;
- generation lock: `e5d0c79be12b1109d3577c9cd50b6cebd3d6379b7d6ed8dee035a6c4a9a3bdd3`.

No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, HTTP/P2/provider,
deployment, publication, release, or Gate check is claimed. PostgreSQL behavior in the focused path is an in-process
read-only fixture. Authentic evidencefs successor crash/reopen behavior is inherited from the existing reviewed
kernel and focused tests; this record does not claim a new live filesystem or production authority invocation.

## Independent review boundary

Slice E remains incomplete until a fixed candidate containing this record receives an independent read-only P0/P1/P2
verdict. Slice F must not begin on a `BLOCK` verdict. An `APPROVE` verdict closes only Slice E's local implementation
and review boundary; it cannot authorize production side effects or close any Gate.
