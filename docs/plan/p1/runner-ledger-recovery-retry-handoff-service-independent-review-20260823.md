# Runner ledger recovery retry-handoff service independent review — 2026-08-23

- Verdict: `APPROVE`
- Severity: `P0=0 / P1=0 / P2=0`
- Fixed candidate: `f86e8ca698df8cb5cbedd3a5b8daf2854c342c27`
- Candidate tree: `5d21b8023162fc0cf7be870a32e150692ec8dc9a`
- Candidate control-plane subtree: `f3b19b905f1ac497f0c451d804fadee6dd83f37f`
- Code commit: `facf4ec06bf679b0cdc4431b78c54ce2f60aaa11`
- Approved Slice D base: `7bbc39185f11e72223e40a081e6dd62b23e53de8`
- Slice D independent review: `cb94b53f95b31edb79ce453f8992e5fbed0ce956`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-retry-handoff-20260823`
- Candidate matrix: [`runner-ledger-recovery-retry-handoff-service-matrix-20260823.md`](runner-ledger-recovery-retry-handoff-service-matrix-20260823.md)
- Candidate matrix SHA-256: `4b8067daa25688344275df38cedcd0f98031e0566c0a34ac7a44a8f0f43a45de`

This is an independent, read-only review of ADR-0023/D-047 Slice E. Approval applies only to the fixed candidate above
and does not authorize a merge, production database access or writes, HTTP/P2/provider behavior, deployment,
publication, release, Slice F implementation, or Gate closure.

## Fixed identity and scope

The candidate branch was clean, matched its remote branch exactly, and was `0/0` relative to its upstream at review
time. HEAD, tree, control-plane subtree, matrix hash, and the reviewed implementation hashes matched the supplied fixed
identity. The diff from the approved Slice D candidate contains 16 paths. It does not change any generated registry,
schema, fixture, generated Go profile, SDK, or historical runner v1 output.

Verified candidate hashes include:

- retry-handoff kernel: `c7030dbbb40d5f31cb833dcf78daf8efd91801b853af2d860b2f201e38a74ce5`;
- retry-handoff tests: `c98cef95e19db6c744097c2858d950ba2eec86cf0f52599baf97eb39522e1ec2`;
- preflight service: `7d27ad0af1df760a93511053eec458409e942963630e3414745d5c19ca605caa`;
- recovery-admission routing: `2717764e06d91afab43558499597b0d2d0aba588f0b852f8d978fee5212a9e6a`;
- generation lock: `e5d0c79be12b1109d3577c9cd50b6cebd3d6379b7d6ed8dee035a6c4a9a3bdd3`.

The generation-lock diff changes only the recovery Go profile suite input-manifest SHA-256 to
`sha256:eeb198248c6bfeb6af1689a91eca852261f06cb601e2b8bcd340dff7f35747a0`. The generated 12-pair mapping therefore
remains byte-identical: one entry-not-implemented pair and eleven recovery-not-implemented pairs, with retry handoff
still distinct from the Slice F recovery execution-admission and success-writer identities.

## Authority and state-machine verdict

The review confirmed the following boundaries:

- only `aborted_retryable`, `ambiguous_reconciled_pending`, and the adjacent
  `ambiguous_unresolved` -> `resolved_pending` durable boundaries can select profile 4; max-attempt, terminal,
  divergent, committed, stale-checkpoint, wrong-tail, and non-adjacent resolution variants mint no handoff;
- the ancestor generation identity, recovery snapshot, composite cursor, checkpoint/tail, terminal or resolution,
  migration/attempt, historical recovery policy, and the current successor decision/schema are cross-bound before a
  current database projection is accepted;
- the fresh locked role, session, advisory lock, ledger/catalog, runtime, plan, and evidence facts are reread; the
  exact read-only database session is unlocked, reset, and closed before any handoff permit is minted, and close
  uncertainty dominates without invoking the successor binder;
- the post-close receipt contains no database handle, and the handoff permit is package-private, registry-backed,
  non-copyable, one-shot, and bound to owner, candidate, old generation, shared cursor-validity cell, boundary, and
  concrete evidence-session binder;
- the concrete evidence session consumes that permit, locks and revalidates the still-active ancestor journal, binds
  the historical policy and exact supersession evidence, and has exactly one production call to
  `ReserveAndActivateSuccessor`;
- that delegated transition invalidates the old session, journal, and cursor before reacquiring the lease, then
  performs ALL-history replay, versioned quota recomputation, adjacent supersession/reservation/header/activation,
  handoff/replay, and final installation under the existing reviewed kernel;
- success is accepted only for a fresh current generation with the current decision/schema, a different journal,
  header-only sequence 1, `brand_new_inherited`, exact attempt `N+1` continuation, a valid new cursor, and an invalid
  old cursor;
- any error after permit consumption, unknown outcome, contradictory successor, or final binder/snapshot drift
  revokes the old cursor; contradictory returned/current successor cursors are also revoked and cannot be reused.

The retry-handoff kernel contains no `BeginMigration`, SQL statement execution, ledger insert, `AppendDurable`, entry
result, recovery execution, recovery success-writer, HTTP, P2, or provider edge. Its only mutation edge is the single
existing successor-generation transition. After a successful handoff the public recovery consumer still returns
`MIGRATION_PROJECTION_NOT_IMPLEMENTED`; Slice F/G remain unavailable.

## Checks and evidence boundary

Fresh independent checks on the fixed candidate:

- HEAD/tree/control-plane subtree, matrix SHA-256, clean worktree, upstream `0/0`, and remote exact branch: PASS;
- static review of boundary selection, ancestor/current cross-binding, claim/permit registries, DB close ordering,
  concrete evidence-session replay, successor result validation, cursor invalidation, and production call graph: PASS;
- exact Go 1.26.6 tests
  `TestRunnerLedgerRetryHandoffPermitRejectsLiteralCopyAndSecondUse` and
  `TestRunnerLedgerRetryHandoffUnknownOrContradictoryResultRevokesOldCursor`: PASS in `10.543s`;
- changed-file `gofmt -d`, candidate `git diff --check`, and target `oxfmt` checks: PASS.

The review relied on the candidate record for its wider bounded evidence: the exact named Slice E normal suite passed
in `103.781s`, the narrow one-shot/unknown-result race suite passed in `92.983s`, and the recovery generator,
generation-lock check, vet, and build were recorded as PASS. Those wider commands were not independently rerun.

No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, live filesystem
power-loss run, deployment, publication, release, or Gate check was run or claimed. Existing successor crash/reopen
and quota behavior was reviewed through the unchanged delegated kernel and its previously reviewed focused evidence;
this review does not claim a new external integration run.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`f86e8ca698df8cb5cbedd3a5b8daf2854c342c27` only. Slice E's local implementation/review boundary may proceed under
the existing approval, but Slice F, every external-side-effect boundary, and every Gate remain open and unauthorized.
