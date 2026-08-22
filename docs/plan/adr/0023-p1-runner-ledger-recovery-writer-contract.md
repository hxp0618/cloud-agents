# ADR-0023: P1 runner ledger recovery writer versioned contract

- Status: Proposed; explicit owner approval required
- Scope: versioned runner recovery admission, action-specific writer profiles, and ordered local implementation/review
- Depends on: ADR-0010, ADR-0019, ADR-0020, ADR-0021, ADR-0022, and the runner recovery contract audit

This proposal grants no implementation authority. The twelve audited retry/recovery/reconcile/failure pairs continue
to return `MIGRATION_PROJECTION_NOT_IMPLEMENTED` until this ADR is explicitly accepted and each ordered fixed slice is
independently reviewed.

## Context

ADR-0022 deliberately implements only four first-attempt known-success pairs. It states that retry, abort, commit
reconciliation, terminal failure, and return failure require separately generated contracts. The current consumer-v1
already classifies one excluded entry retry pair and eleven recovery pairs, but its ordinary fact is not a capability.

The repository contains strict recovery replay, closed evidence DTOs, retry-receipt types, ambiguous-boundary facts,
lineage handoff machinery, and append/checkpoint primitives. It does not contain a production runner authority that
may combine those mechanisms for any audited pair. Reusing the close-only entry permit, the first-attempt execution
permit, the historical brand-new writer, or the legacy reconcile helper would silently widen an immutable identity.

## Proposed decision

Keep every preflight, consumer, entry-admission, execution-admission, and first-attempt success-writer v1 artifact
byte-identical. Add a new recovery admission registry bound to those exact identities, then mint only an
action-specific one-shot permit for the selected generated pair.

The proposal uses separate generated identities for:

1. abort-terminal append;
2. dangling-commit observation terminal;
3. unresolved ambiguous resolution;
4. retry lineage handoff;
5. inherited/retry execution; and
6. typed return-failure result.

No permit is a union writer. A consumer for one family cannot call another family's binder or mutation port.

## Proposed closed pair mapping

| Family                      | Exact generated pairs                                                                                                       |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Recovery execution          | entry `brand_new_inherited/begin_next_attempt`; recovery `brand_new_inherited/begin_first_attempt` and `begin_next_attempt` |
| Abort terminal              | dangling intent/intermediate times retryable/terminal abort                                                                 |
| Commit observation terminal | dangling commit intent / reconcile commit                                                                                   |
| Ambiguous resolution        | ambiguous unresolved / reconcile commit                                                                                     |
| Retry lineage handoff       | terminal / begin next attempt                                                                                               |
| Typed return failure        | terminal / return failure; divergent / return failure                                                                       |

Unknown, complete, first-attempt success, next-entry success, caller-selected, copied, literal, stale, foreign-verifier,
cross-profile, cross-generation, replaced-registry, and second-consume inputs do not match any family.

## Proposed common recovery admission

The common admission consumes one exact same-verifier consumer fact, reacquires the full-root evidence inventory,
and, where a database observation is required, opens a fresh dedicated session. Before sealing it cross-binds:

1. every immutable runner profile identity and the proposed recovery profile identity;
2. candidate, generation, journal, cursor, checkpoint, terminal/resolution, continuation, schema bundle, manifest,
   runner projection, authority, and catalog identities;
3. the exact ledger prefix, head, length, rows, and domain-separated digest;
4. max-attempt and ordered-migration facts from the verified bundle;
5. action-specific rollback/connection-lifecycle/commit-rejection or ambiguous-boundary receipts; and
6. a final evidence, database-session, ledger, catalog, and authority reread.

The admission permit is registry-backed, pointer/owner-bound, non-copyable, and one-shot. Its initial implementation
slice is `close_without_mutation` only.

## Proposed action boundaries

### Abort terminal

Append one `AttemptTerminalState` only after an owned exact rollback, irreversibly terminated old connection, or
confirmed commit-rejection receipt proves the predecessor lifecycle cannot mutate again. The outcome and retry proof
must match the generated action and max-attempt budget. Unknown lifecycle status cannot append an abort.

### Commit observation and ambiguous resolution

Reconciliation opens a fresh locked read-only session and compares the exact expected ledger row/prefix and cumulative
catalog. It classifies exactly committed, exactly pending, divergent, or operationally unknown. The first three may
authorize one record of the correct kind; unknown authorizes no append.

A dangling commit intent can append one observed terminal. An unresolved terminal can append only an immediately
adjacent `AmbiguousResolutionState` whose unresolved-terminal digest is exact. These permits and record binders remain
distinct.

### Retry lineage handoff

Only an exact retryable terminal or resolved-pending boundary within the verified max-attempt budget can authorize
`GenerationSuperseded -> GenerationReserved -> header -> GenerationActivated`. The successor continuation binds the
old journal, checkpoint, terminal, migration, next attempt, and previous-terminal digest. No SQL is executed during
handoff.

### Recovery execution

The inherited successor uses a new execution-admission and success-writer identity. It opens a fresh session and runs
one exact first or later attempt. A later attempt must bind the exact previous terminal and retry proof. The kernel may
reuse reviewed implementation helpers only behind the new authority and only if all first-attempt assumptions are
removed by addition, never by weakening ADR-0022 v1 checks.

### Typed return failure

The result path performs no mutation. It returns a stable typed failure only after the exact terminal/divergent replay,
ledger/catalog boundary, and runtime identity are sealed. The result is ordinary data and cannot authorize retry,
handoff, append, SQL, or a second transition.

## Proposed failure precedence

- stored evidence/ledger/catalog contradiction is corruption and takes precedence over a requested action;
- context, filesystem, session, lock, projection, cleanup, or other operational uncertainty takes precedence over
  `NOT_IMPLEMENTED` or an ordinary failure result;
- append or commit acknowledgement-unknown invalidates the old cursor/authority and returns recovery-required;
- a known committed database outcome is never retried because terminal append failed;
- a known pending/rejected outcome cannot be relabeled committed; and
- no recovery output is reused as the next permit.

## Proposed ordered slices

### Slice A - generated contracts only

Add versioned source schemas, fixtures, registries, package-private Go profiles, manifests, and generation-lock
bindings. Prove all existing runner artifacts byte-identical. No claim, database handle, append, or caller is added.

### Slice B - read-only recovery admission

Implement same-verifier full replay and action-specific close-only permits. Keep every writer and public recovery
result `NOT_IMPLEMENTED`.

### Slice C - abort terminal writer

Implement the four abort pairs, exact lifecycle receipts, append/checkpoint behavior, and fault/crash matrix. Obtain an
independent verdict before continuing.

### Slice D - commit reconciliation writers

Implement fresh-session observation, dangling-commit terminal, and adjacent unresolved resolution as distinct
one-shot kernels. Cover PG15/16/17 committed/pending/divergent/unknown and every append barrier; review independently.

### Slice E - retry handoff

Implement exact supersession/successor activation for retryable or resolved-pending outcomes, including crash/reopen,
quota, stale epoch, and old-cursor revocation matrices; review independently.

### Slice F - recovery execution

Implement one inherited first/later attempt per fresh session under a new generated identity. Preserve dynamic cursor,
multi-statement, commit-once, and post-commit recovery boundaries; review independently.

### Slice G - failure result and caller matrix

Add the two typed failure results and connect only independently approved recovery families. Cover all twelve pairs,
fresh re-entry, bounded attempts, and no ordinary-result reuse; obtain a final independent verdict.

## Minimum conformance matrix

| Boundary             | Required cases                                                                                                              |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| Historical same-bits | preflight, consumer, entry-admission, execution-admission, success-writer sources/generated/profile/lock                    |
| Selection            | exact twelve pairs, wrong disposition/state/action/profile, literal/copy/stale/foreign/replaced/second-use                  |
| Abort receipts       | rollback, terminated connection, commit rejection, predecessor mismatch, retry budget exact/+1, unknown old lifecycle       |
| Commit observation   | committed, pending, divergent, unknown; wrong row/prefix/catalog; stale role/lock/session; repeated observation             |
| Evidence append      | terminal versus resolution kind, adjacency, cursor/checkpoint/rotation, zero/durable/unknown, close and cleanup             |
| Retry handoff        | exact old terminal/resolution, supersession adjacency, successor continuation, quota, crash/reopen, stale epoch             |
| Recovery execution   | attempt 1 and 2..max, previous terminal, all statement counts, fresh session, rejected/ambiguous commit, no second mutation |
| Failure result       | terminal/divergent exact stable error, redaction, no mutation, no permit conversion                                         |
| Forbidden surfaces   | no production DB invocation, HTTP/P2/provider, deployment/publication/release, main merge, or Gate closure                  |

## Explicit non-claims

This proposal does not authorize its own implementation. It does not change the current `NOT_IMPLEMENTED` behavior,
permit production database writes, add external surfaces, deploy, publish, release, merge main, or close any immutable
or aggregate Gate. Explicit owner approval and ordered fixed-candidate independent review remain mandatory.
