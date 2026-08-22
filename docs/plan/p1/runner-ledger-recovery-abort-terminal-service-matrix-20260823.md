# Runner ledger recovery abort-terminal service matrix — 2026-08-23

- Status: `SLICE_C_FIXED_IMPLEMENTATION_PENDING_INDEPENDENT_REVIEW`
- Approved Slice B candidate: `23c3083b7d7b58089f2cb208b1381b2d510500ff`
- Slice B independent review: `4808d20e1d36f5f0bb6efe557ccc6347e955bab0` — `APPROVE, P0=0/P1=0/P2=0`
- Slice C code commit: `f85cce616de3ab978e03ef5b3b7ca5a22021df21`
- Slice C code tree: `b7545949479b52fe3fa154b7e9fb4f3fe4775903`
- Slice C control-plane subtree: `f8f773da29e12638afac46d15061f28ffd3f0c65`
- Slice C branch: `codex/cloud-agents-p1-runner-recovery-abort-terminal-20260823`
- Decision: [`D-047 / ADR-0023`](runner-ledger-recovery-contract-decision-20260822.md)
- Gate effect: none; every immutable and aggregate Gate remains open

This record covers only ADR-0023 Slice C's four abort-terminal pairs. It does not implement commit observation,
ambiguous resolution, retry handoff, recovery execution, recovery success, or typed failure. It does not authorize
production database writes, HTTP/P2/provider behavior, deployment, publication, release, main merge, or Gate closure.

## Closed action mapping

The versioned `runner-ledger-abort-terminal-writer/v1` remains the only writer identity accepted by this kernel. The
common recovery-admission profile first selects one exact pair and retains a read-only, fresh, locked observation.

| Recovery state              | Generated action           | Durable terminal outcome | Next recovery action |
| --------------------------- | -------------------------- | ------------------------ | -------------------- |
| `dangling_statement_intent` | `append_aborted_retryable` | `aborted_retryable`      | `begin_next_attempt` |
| `dangling_statement_intent` | `append_aborted_terminal`  | `aborted_terminal`       | `return_failure`     |
| `dangling_intermediate`     | `append_aborted_retryable` | `aborted_retryable`      | `begin_next_attempt` |
| `dangling_intermediate`     | `append_aborted_terminal`  | `aborted_terminal`       | `return_failure`     |

All other eight generated recovery pairs keep their Slice B `close_without_mutation` behavior and public
`MIGRATION_PROJECTION_NOT_IMPLEMENTED` result. The four implemented pairs append exactly one terminal, but the public
consumer still returns its stable recovery `NOT_IMPLEMENTED` result; later families remain ordered behind their own
independent reviews.

## Exact mutation sequence

For one selected pair, the package-private service performs this sequence:

1. consume the exact registry-backed Slice B admission permit and transfer its same-verifier candidate, generation,
   full evidence boundary, runtime, plan, ledger, catalog, authority, database, state, action, attempt, and max-attempt
   facts;
2. rebuild the verified runtime and exact statement-plan closure, reread the current evidence generation and recovery
   boundary, and revalidate the fresh locked migration-role database observation;
3. unlock, reset, and close that exact read-only database session before minting any mutation authority; cleanup
   uncertainty returns `MIGRATION_TRANSACTION_BOUNDARY` and mints no writer;
4. bind a typed `precommit_connection_terminated_exact_predecessor` lifecycle receipt over the exact generation,
   migration, attempt, ledger prefix, predecessor/observed catalog, authority, and ordered lifecycle token;
5. mint a registry-backed, pointer/owner/cursor-bound, non-copyable, one-shot abort-writer permit that binds the
   recovery-admission canonical, generated writer identity, entry and plan closure, database identity, receipt, and
   complete terminal body;
6. let only the concrete generation evidence session consume that permit, revalidate the current journal and full
   physical prefix, attach the owned lifecycle receipt to the chain witness, and bind one owned
   `AttemptTerminalState` record;
7. perform exactly one composite `AppendDurable` operation, including the canonical checkpoint and optional rotation
   header/checkpoint; and
8. require the returned cursor, record/checkpoint identities, current journal pointer, terminal, typed predecessor
   bodies, pre-append tail, recovery state, and next action to match exactly before returning.

No transaction, SQL execution, ledger insert, database commit, generation supersession, successor reservation, or
activation edge exists in the Slice C writer.

## Failure and crash closure

- Stored evidence, prefix, receipt, header, generation, cursor, entry, plan, ledger, catalog, authority, database, or
  typed predecessor contradiction fails closed before append where knowable.
- Context, session, lock, projection, reset, unlock, or close uncertainty dominates the requested action and mints no
  writer permit.
- A zero-valued append error with the old cursor still valid is mapped to its stable pre-mutation error class. Any
  non-empty append result metadata, invalidated old cursor, unknown outcome, durable-result contradiction, or
  post-append snapshot drift returns `MIGRATION_EVIDENCE_RECOVERY_REQUIRED` and revokes the relevant cursor authority.
- Literal, copy, registry-missing, foreign binder/journal/cursor, changed terminal, changed plan/database identity, and
  second-consume inputs cannot bind or reuse the one-shot writer.
- `aborted_retryable` is rejected when the current attempt is already the signed `MaxAttempts`; terminal abort remains
  distinct and cannot be relabeled retryable.

## Focused conformance

The fixed code commit was checked with Node `24.13.1`, Bun `1.3.14`, and Go/gofmt `1.26.6 darwin/arm64`:

- focused abort writer, Slice B admission, generated profile, and public consumer matrix normal tests: PASS in
  `83.292s`;
- abort writer plus public consumer matrix under `-race`: PASS in `245.509s`;
- migration package compile-only, `go vet ./internal/migration`, and `go build ./internal/migration`: PASS;
- recovery registry generator and recovery Go generator: current;
- generation-lock writer/checker: current;
- changed Go files: `gofmt` clean; `git diff --check`: PASS.

The generation lock SHA-256 is
`d00246a7699284909d427df48cec085ccbd99a80c56d28ac298d5e772993f269`. Its only generated-lock change from Slice B
is the recovery Go profile suite input-manifest SHA-256,
`sha256:7d0bb4feaef0ff3bfeb42e1911995d77e806b1460e9c17ea5a647d5c9ebe5eb9`, because the bound AST test now permits exactly
one abort-terminal writer call while continuing to reject every other recovery writer edge. All generated registry,
profile, fixture, schema, and historical runner v1 bytes remain unchanged.

No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, HTTP/P2/provider,
deployment, publication, release, or Gate check is claimed. The test database and evidence implementations are
in-process fixtures; this record does not claim a live production authority invocation.

## Independent review boundary

Slice C remains incomplete until a fixed candidate containing this record receives an independent read-only P0/P1/P2
verdict. Slice D must not begin on a `BLOCK` verdict. An `APPROVE` verdict closes only Slice C's local implementation
and review boundary; it cannot authorize production side effects or close any Gate.
