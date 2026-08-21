# ADR-0022: P1 runner ledger entry execution and success-writer versioned contract

- Status: Proposed on 2026-08-22; owner approval required before implementation
- Scope: generated execution-admission and one-entry success-writer profiles only
- Depends on: ADR-0009, ADR-0010, ADR-0019, ADR-0020, ADR-0021, and the runner entry writer contract audit

This proposal is not execution authority. Until it is explicitly accepted, `runner-ledger-entry-admission/v1`
remains close-only and every entry/recovery writer remains `NOT_IMPLEMENTED`.

## Context

The reviewed entry-admission v1 contract is immutable. Its generated implementation boundary says:

- `databaseTransaction = migration_and_read_write_forbidden`;
- `beginMigration = forbidden`;
- `ledgerMutation = forbidden`;
- `evidenceMutation = forbidden`;
- `permitConsumer = none`; and
- `entryWriter = not_implemented` and `recoveryWriter = not_implemented`.

Those values are registry identity. A later slice must not add a consumer to the v1 permit, reinterpret its ordinary
facts as writer authority, or weaken its generated same-bits assertions.

The existing production writer is not a reusable general entry kernel. It accepts only an empty ledger,
`brand_new` or `brand_new_inherited`, `begin_first_attempt`, entry index zero, attempt one, one migration, and one
statement. The current signed schema bundle contains eleven migrations whose exact statement counts are
`20, 71, 46, 20, 1, 1, 89, 34, 30, 52, 161`. A production entry contract therefore has to model an exact ordered
multi-statement attempt and cannot be obtained by deleting the existing single-statement checks.

## Decision proposed

Add two new generated registries with independent identities:

1. `runner-ledger-entry-execution-admission/v1`; and
2. `runner-ledger-entry-success-writer/v1`.

The existing preflight, consumer, and entry-admission v1 registries remain byte-identical. The execution-admission
registry repeats the fresh same-session locked database/evidence revalidation and mints its own sealed one-shot
execution permit. It does not consume or mutate the ADR-0021 close-only permit.

The success-writer registry consumes exactly one execution permit and drives exactly one migration entry through
one first attempt. It supports every signed statement in that selected entry and only the known-success path. It
does not retry, append abort/recovery records, reconcile an ambiguous commit, or begin a second entry.

## Closed selection matrix

Execution admission v1 accepts exactly four existing generated pairs:

| Ledger disposition   | Recovery state        | Recovery action                  | Execution action          |
| -------------------- | --------------------- | -------------------------------- | ------------------------- |
| `empty_brand_new`    | `brand_new`           | `begin_first_attempt`            | `prepare_entry_execution` |
| `empty_brand_new`    | `brand_new_inherited` | `begin_first_attempt`            | `prepare_entry_execution` |
| `partial_next_entry` | `brand_new_inherited` | `begin_first_attempt_next_entry` | `prepare_entry_execution` |
| `partial_next_entry` | `terminal`            | `begin_first_attempt_next_entry` | `prepare_entry_execution` |

The fifth ADR-0021 entry-admission pair,
`empty_brand_new / brand_new_inherited / begin_next_attempt`, is a retry transition. It remains
`recovery_not_implemented` for these profiles. All dangling intent/intermediate/commit, ambiguous,
retryable/terminal failure, divergent, complete, unknown, caller-selected, copied, literal, stale, or cross-profile
inputs also fail closed without a writer transition.

The selected entry is always derived from the exact verified ledger prefix and ordered signed bundle. The caller
cannot supply the disposition, action, entry index, migration ID, attempt index, statement count, profile, or
digest. A first-attempt execution has attempt index one and no previous-attempt terminal. A next-entry execution
starts the exact immediate successor migration at statement zero; it does not reuse the predecessor transaction or
database session.

## Execution-admission authority

The execution-admission service consumes one fresh same-verifier entry consumer claim inside a closed
package-private call. It opens a new dedicated database session and, while the signed advisory lock is held on that
same session, cross-binds:

1. the exact generated preflight, consumer, ADR-0021 admission, execution-admission, and writer profile identities;
2. candidate, generation, journal, recovery, schema-bundle, manifest, runner-projection, catalog-contract,
   authority-profile, and authority-binding identities;
3. the connected database identity, session user, current user, migration role, signed settings, and lock key;
4. the exact durable ledger-prefix rows, length, head, and domain-separated digest;
5. the exact predecessor or cumulative catalog projection for the selected entry;
6. the selected entry index, migration identity, ledger-row body, SQL artifact identity, and full ordered statement
   plan closure;
7. the exact first-attempt continuation and current evidence cursor, including segment, next sequence, previous
   record, and latest checkpoint; and
8. a final ledger, catalog, authority, evidence, and session-boundary reread immediately before sealing.

The execution permit is registry-backed, non-copyable, pointer/owner-bound, and one-shot. A zero value, ordinary
fact, ADR-0021 permit, copied permit, foreign verifier, changed inventory, changed ledger/catalog, or lost/closed
session cannot authorize the writer. Consuming it atomically removes the permit from its registry; a failed second
consumption cannot open a transaction or append evidence.

## One-entry success state machine

The writer consumes the execution permit and returns a fresh sealed successor at every durable or database
barrier. An old successor becomes invalid as soon as its transition is attempted.

```text
ExecutionReady
  -> TransactionReady
  -> StatementReady(0)
  -> IntentDurable(0)
  -> StatementExecuted(0)
  -> IntermediateDurable(0)
  -> ...
  -> StatementReady(N)
  -> IntentDurable(N)
  -> StatementExecuted(N)
  -> FinalIntermediateDurable
  -> LedgerReadbackReady
  -> CommitIntentDurable
  -> CommitKnownCommitted
  -> CommittedTerminalDurable
  -> EntryCommittedComplete | EntryCommittedNextEntry
```

`N` is the last index of the exact signed plan closure and is not assumed to be zero. Every state binds the same
session, transaction where applicable, evidence session, generation, journal, migration, attempt, ledger prefix,
bundle, authority/catalog identities, ordered plan digest, statement index, and current evidence cursor.

### Statement transition

For each statement index `i` in exact order:

1. obtain the exact SQL byte range from the verified artifact and revalidate its statement digest and
   classification;
2. project and bind the exact authority/catalog before-state;
3. append one durable `StatementIntent` before executing SQL;
4. execute exactly that one statement through the extended protocol, never a caller-provided or concatenated SQL
   string;
5. project the authority/catalog after-state on the same transaction and bind the expected transition;
6. append one durable `StatementIntermediateEvidence`; and
7. if `i < N`, return `StatementReady(i+1)` whose previous-intermediate digest is the just-durable state.

Non-final intermediates have no preledger result pair. The final intermediate must have both preledger authority and
catalog results, produced after all statements on the same transaction, and must bind the exact expected cumulative
catalog for that entry. A one-sided preledger pair or a preledger pair on a non-final statement is a stored or
candidate contradiction and cannot advance.

Evidence appends use the existing sealed current cursor and its composite checkpoint/rotation semantics. The new
writer must not assume segment zero, fixed absolute sequence numbers, or that all statements fit one segment. Each
known-durable append returns the only next cursor; a zero-write failure keeps no writer authority, and any attempted
write with unknown durability invalidates the current chain and requires reopen.

### Ledger and commit transition

After the final intermediate:

1. insert exactly the signed ledger row in the same transaction;
2. read back the exact expected prefix, length, head, rows, and digest;
3. append a durable `CommitIntent` that binds the final intermediate and readback;
4. invoke commit once; and
5. close the old database session before any result can be reused as a new-entry session.

Only an exact known-committed outcome may append `AttemptTerminalState(outcome=committed)`. The terminal append must
bind the commit intent, exact ledger row, final catalog, bundle completion/next-entry classification, and current
evidence cursor. A durable terminal produces one of two ordinary outcomes:

- `EntryCommittedComplete`; or
- `EntryCommittedNextEntry`.

Neither outcome is a writer permit. A next entry must start again through fresh preflight, same-verifier claim
consumption, and execution admission on a new dedicated locked session. Bundle-level `Runner.Run` orchestration and
result accumulation are separate later slices.

## Failure and unknown outcomes

The v1 success writer never performs an automatic second mutation:

- failure before an evidence or database mutation returns a closed pre-mutation failure;
- failure after durable intent but before durable intermediate rolls back and leaves the exact dangling-intent
  recovery state;
- failure after durable intermediate but before commit intent rolls back and leaves dangling-intermediate state;
- commit rejection or connection loss after durable commit intent leaves dangling-commit/ambiguous state;
- known database commit followed by terminal append failure or unknown durability must not execute SQL or commit
  again; and
- cancellation, deadline, cleanup, unlock, close, or evidence errors after a mutation attempt cannot be relabeled
  as a safe pre-mutation error.

All such states are reopened through strict replay. Abort, retry, reconciliation, terminal-failure, and
return-failure appenders remain `NOT_IMPLEMENTED` until separate versioned recovery profiles are approved.

Stored evidence/ledger/catalog contradictions and exact commit facts take precedence over a requested writer
action. Operational/context failures take precedence over `NOT_IMPLEMENTED` when the boundary could not be fully
classified. An unknown outcome never falls back to an earlier ordinary claim, ADR-0021 permit, or execution permit.

## Ordered implementation slices after approval

### Slice A - generated registries only

Generate the two new source schemas, fixtures, registries, package-private profiles, manifests, and generation-lock
bindings. Prove all preflight/consumer/entry-admission v1 outputs byte-identical. No database session, evidence
append, permit, runner caller, or mutation surface is added.

### Slice B - execution admission, still no mutation

Implement the fresh locked revalidation and one-shot execution permit. Its only temporary production transition is
`close_without_mutation`; transaction, SQL, ledger insert, evidence append, and public runner consumption remain
forbidden. Cover authority, ledger, catalog, evidence, stale/copy, context, and cleanup faults.

### Slice C - one-entry multi-statement success kernel

Implement the state machine through known committed terminal using fake and separately approved ephemeral
PostgreSQL fixtures. Keep `Runner.Run` disconnected from the new permit until the fixed kernel and its normal/race,
fault, same-bits, and forbidden-surface matrices receive an independent P0/P1/P2 verdict.

### Slice D - typed caller and entry loop

After Slice C approval, connect only the four generated first-attempt pairs. Execute at most one admitted entry per
fresh database session, then re-enter preflight for the next entry. Preserve the complete-ledger no-op and every
unsupported recovery pair. Run the full fixed-toolchain matrix and independent review.

### Slice E - recovery contracts

Retry, abort, commit reconciliation, terminal failure, and return failure each require separately generated closed
profiles and writer kernels. They are not implied by acceptance of this ADR.

## Minimum conformance matrix

| Boundary             | Required cases                                                                                                                   |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Historical same-bits | preflight v1, consumer v1, and entry-admission v1 source/generated/profile bytes and digests                                     |
| Generated selection  | exact four pairs; retry fifth pair rejected; all recovery/complete/unknown pairs; caller profile/action/index rejection          |
| Execution admission  | ledger/catalog/evidence before/after drift; lock/role/settings; stale/copy/literal/foreign verifier; cleanup and second consume  |
| Plan closure         | 1, 20, 34, 46, 52, 71, 89, and 161 statements; zero, missing, duplicate, reordered, wrong range/digest/classification, exact/+1  |
| Statement chain      | every index; predecessor digest; non-final versus final preledger shape; cursor rotation/checkpoint; append zero/durable/unknown |
| Transaction          | exact SQL bytes once; same session/transaction; rollback at each boundary; ledger insert/readback; commit reject/known/unknown   |
| Crash/reopen         | after every intent/intermediate/commit/terminal append and database commit barrier; no mutation retry from an old authority      |
| Entry boundary       | empty first entry and partial immediate next entry; terminal predecessor; new session/lock for every entry; no ordinary permit   |
| Forbidden surfaces   | retry/reconcile/failure writer zero callers; no HTTP/P2/provider route; no production database, deployment, publication, or Gate |

## Explicit non-claims

This proposal does not implement or authorize a writer. Even after acceptance, it would authorize only the ordered
local implementation/review slices above. It does not authorize production database writes, production
credentials, deployment, HTTP/P2/provider surfaces, package or image publication, release, merge to main, or any
immutable/aggregate Gate closure.
