# Runner ledger entry success-kernel service matrix — 2026-08-22

- Status: `SLICE_C_LOCAL_IMPLEMENTATION_RECORDED_INDEPENDENT_REVIEW_PENDING`
- Approved Slice B candidate: `c375fac6ae5a7ffd95e0931dbe384ae213f5087b`
- Slice B independent review: `d49f89cd53163414ecec6ca77d6705ff9a7e84ad` — `APPROVE, P0=0/P1=0/P2=0`
- Slice C base: `d49f89cd53163414ecec6ca77d6705ff9a7e84ad`
- Slice C branch: `codex/cloud-agents-p1-runner-entry-success-kernel-20260822`
- Decision: [`ADR-0022`](../adr/0022-p1-runner-ledger-entry-success-writer-contract.md)
- Gate effect: none; every immutable and aggregate Gate remains open or at its prior phase status

This record covers only ADR-0022 Slice C's disconnected one-entry, first-attempt, known-success kernel. It does not
connect `Runner.Run`, start a second entry, add a retry/abort/reconcile/failure writer, authorize production database
writes, or add HTTP/P2/provider, deployment, publication, release, main-merge, or Gate-closing behavior.

## Closed success sequence

The package-private kernel consumes one registry-backed Slice B execution permit and then drives exactly one selected
signed entry through this closed sequence:

1. atomically remove and consume the execution permit, then rebind the exact runtime bundle, ordered statement plans,
   current same-verifier candidate/generation/journal/recovery boundary, generated profile closure, and retained
   dedicated database session;
2. reread the idle role/lock/session boundary and open one migration transaction;
3. for every exact signed statement index, project the before-state, append one durable `StatementIntent`, execute only
   the signed byte range once, project the after-state, and append one durable `StatementIntermediateEvidence`;
4. require the final intermediate to carry both exact preledger authority and catalog results while every non-final
   intermediate carries neither;
5. insert the one selected signed ledger row in that same transaction, read back the full predecessor-plus-row prefix,
   and compare its exact length, head, rows, and domain-separated digest;
6. append one durable `CommitIntent`, call the sealed commit protocol once, and close the old database session;
7. only after an exact known-committed result and proven old-session close, append one durable committed terminal; and
8. return an ordinary `entry_committed_complete` or `entry_committed_next_entry` result, never a writer permit.

Every successful state transition consumes the old pointer/registry authority and seals a fresh state that binds the
same database/evidence owner, candidate, generation, journal cursor, selected entry, plan closure, transaction where
applicable, projection facts, and durable recovery boundary. The live state is cross-bound to independent primary and
cleanup record/data/binding snapshots that share one atomic claim cell. A transition first reads all three claim-cell
bindings, accepts only the state-plus-record or record-pair provenance, and then lets exactly one consumer claim that
cell. A missing, replaced, or field-drifted copy can only use the other copy to revoke the old cursor and return
recovery-required, never mint a successor. Literal, copied, stale, swapped, registry-missing, or second-use state and
evidence-request values fail closed.

## Evidence and mutation boundary

The new evidence binder is sealed to the current `generationEvidenceSession`. A registry-backed request binds the
candidate, generation, current recovery digest, exact cursor, one typed record, one exact statement plan, and signed
`maxAttempts`. The production binder holds the evidence-session and journal locks, strict-reads every physical segment,
strict-decodes and validates the complete current chain with its same-verifier witness, revalidates the snapshot, and
only then constructs the existing typed `OwnedEvidenceRecord` for the next append.

Each append validates the composite append/checkpoint result, including optional rotation identities and the only next
cursor. A zero-write error returns no successor. Any attempted write with unknown durability, a foreign cursor, or a
contradictory durable result invalidates the old cursor and returns recovery-required. The kernel never retries a
mutation from an old state.

## Local conformance matrix

The focused in-process matrix covers:

- exact plan closures with `1`, `20`, `34`, `46`, `52`, `71`, `89`, and `161` statements;
- one-statement complete, two-statement complete with physical segment rotation, first entry with a successor, and
  immediate partial-ledger next entry;
- exact SQL bytes and call count, ordered intent/intermediate pairs, final-only preledger pair, one ledger insert and
  readback, one commit, one terminal append, and complete/next-entry result classification;
- permit/state/request zero, literal, copy, field tamper, foreign binder/cursor, old successor, second transition, and
  sequential plus competing-goroutine one-shot consumption;
- after a known commit and after a durable terminal, state self/canonical/live-binding/data/foreign-or-nil-claim drift plus primary
  and cleanup registry missing, replacement, binding drift, or otherwise-valid typed replacement carrying a foreign
  already-claimed cell, each requiring recovery-required, cursor revocation, both registry entries removed, and no
  authority recovery after restoring the external state or registry field;
- competing-goroutine consumption of one known-commit state, requiring exactly one terminal authority, one rejected
  second consumer, an unretracted winning cursor, and a valid ordinary result from that sole terminal authority;
- pre-cancel and state drift before transaction acquisition;
- transaction open, statement-before projection, intent binder/zero-write/foreign cursor/durable-result tamper,
  statement execution after durable intent, intermediate unknown, ledger insert/readback contradiction, commit-intent
  zero-write, commit rejected/ambiguous, post-commit close, terminal binder, and terminal unknown boundaries;
- a failure at the second statement after the first durable intermediate, preserving the exact dangling-intent
  recovery state without executing a third operation; and
- a production-AST guard proving the kernel has no production caller and the two Slice C files add no HTTP/provider,
  successor, retry, abort, reconciliation, or failure-writer edge.

The existing opt-in PostgreSQL 15/16/17 projection fixtures remain the separately approved concrete
session/transaction/projection adapter evidence. This Slice C candidate does not claim a fresh live PostgreSQL run:
its success-kernel execution matrix is fake/in-process, and the independent reviewer must not relabel checked-in
metadata or a skipped environment-gated test as a live database PASS. No production database credentials or writes
were used.

## Fresh local checks

The final-source local matrix used Node `24.13.1`, Bun `1.3.14`, and Go `1.26.6 darwin/arm64`. It produced:

- focused success-kernel, execution-admission/writer, authority-spread, production-consumer, and forbidden-graph
  normal: PASS in `151.371s` package time;
- the same focused scope with `-race -timeout=30m`: PASS in `1376.839s` package time;
- full `internal/migration` normal with `-timeout=30m`: PASS in `1591.051s` package time;
- full platform contract/generator/lock check: PASS/current for `115` JSON files, `50` schemas, and `62` fixture cases;
- control-plane `go vet ./...`, `go build ./...`, `go mod tidy -diff`, and `go mod verify`: PASS;
- repository lint and TypeScript typecheck: PASS;
- Linux `amd64` and `arm64` migration test-binary compile with `CGO_ENABLED=0`: PASS; and
- all changed Go files `gofmt`, all changed documentation `oxfmt --check`, and `git diff --check`: PASS.

The focused race and full normal commands ran serially; their package-reported durations are conformance evidence, not
a performance baseline. Neither a still-running process nor the default ten-minute bounded stop is counted as PASS.

The repository-wide formatter still reports eight pre-existing HEAD files outside this Slice; none is dirty or was
rewritten here, so the full formatter invocation is explicitly not recorded as PASS. The matrix does not claim a
full-package race run: the final race evidence is the exact changed authority/kernel/consumer/graph scope above.

## Generated and historical boundary

The candidate does not edit either generated registry, source schema, fixture, generated Go profile, SDK, SQL
migration, or historical ADR-0021 v1 output. The fixed toolchain contract check must remain current on the final source,
and historical preflight/consumer/entry-admission/execution-admission/success-writer identities remain exact.

The public `Runner.Run` graph remains unchanged. The new `executeRunnerLedgerEntrySuccess` method has zero production
callers; Slice D must add its own typed caller after this fixed candidate receives an independent P0/P1/P2 verdict.
All entry/recovery branches exposed by the current public runner continue to return their existing stable
`NOT_IMPLEMENTED` boundary.

## Review handoff

The first fixed Slice C candidate `d5cb59a` received `BLOCK, P0=0/P1=1/P2=0`: after `Commit` had actually
been called, ambiguous, rejected, or close-unproven exits returned recovery-required without revoking the shared
journal-cursor validity cell. The repair makes every non-success exit after an invoked commit revoke that shared cell,
including commit-observation registry tamper and every later terminal/result failure. The focused boundary matrix now
asserts cursor revocation for rejected and ambiguous commits, unproven old-session close, observation-registry tamper,
terminal-binder failure, and terminal unknown durability.

The first repair candidate `eeaa24b` also received `BLOCK, P0=0/P1=1/P2=0`: corrupting a commit-known or
terminal-durable state's self/canonical/primary-registry edge made the ordinary transition return no trusted data, so
its post-commit failure helper still could not reach and revoke the original cursor. The second repair retains two
independently cloned registry records and binding snapshots behind one atomic claim. A valid pair advances exactly
once; any single state, binding, primary-registry, or cleanup-registry contradiction consumes the authority, retrieves
only the unaffected cleanup facts needed to revoke the old cursor, removes both registry entries, and returns
recovery-required. Normal and race tests cover both post-commit phases, every named tamper class, restore-after-failure,
and competing consumption.

The second repair candidate `cda403d` received `BLOCK, P0=0/P1=1/P2=0`: it attempted the cleanup record's CAS
before cross-binding that claim cell with the primary record. A typed cleanup replacement whose record facts remained
valid but whose foreign claim cell was already true therefore looked like a legitimate concurrent loser; it left both
registries, the cursor, and the restored original state reusable. The third repair binds the original claim cell in the
live state as a third provenance point, loads both registry records before any CAS, selects only a state-plus-record or
record-pair claim identity, and then lets the sole CAS winner classify and remove both records. A real concurrent loser
still returns without revoking the winner's cursor, while a one-sided typed primary or cleanup replacement is consumed
through the other two matching claim bindings and can only revoke the old cursor. The two-phase tamper matrix now has
`28` concrete subcases plus the competing-consumer race case.

The third repair candidate `922468e` received `BLOCK, P0=0/P1=1/P2=0`: the transition still returned before
loading either registry when the live state's claim pointer was nil. Restoring that pointer after the failed
transition could therefore revive the unconsumed state and its still-valid cursor. The fourth repair treats a
non-nil state with a missing live claim as drift rather than absence: it loads and cross-binds the primary and cleanup
records, selects their exact shared claim cell, consumes that cell, removes both records, and uses the trusted record
facts only to revoke the old cursor and return recovery-required. Both commit-known and terminal-durable phases now
exercise nil-claim tamper followed by restoration and require that neither state nor cursor can revive.

This local record is not an approval. Before Slice D may begin, the complete fixed candidate must be clean, pushed,
hash-identified, and independently reviewed read-only for authority provenance, one-shot state transitions, full
multi-statement ordering, evidence append/checkpoint/rotation behavior, commit and cleanup precedence, generated
same-bits, forbidden surfaces, and test/record accuracy. The review must emit an explicit P0/P1/P2 verdict.

No result in this record authorizes production database mutation, live credentials, HTTP/P2/provider behavior,
deployment, publication, release, merge to main, or any Gate closure.
