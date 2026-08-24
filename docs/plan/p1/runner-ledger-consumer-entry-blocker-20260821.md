# P1 runner ledger consumer/writer entry blocker - 2026-08-21

- Status: **OWNER DECISION REQUIRED — READ-ONLY ENTRY AUDIT COMPLETE**
- Audited source: `e64e0a28e2cd81203c91a7eba4df264bd29889f3`
- Independent review: `9ed71b84b8b35d1fcb2b983bba068c320793ff7d`
- Canonical status parent: `24fd89a4bc6df96504110e11b27c41097b70f6eb`
- Scope: the first production consumer after runner ledger/catalog preflight Slice C

This audit does not implement a `Runner.Run` caller, writer permit, migration/RW transaction, SQL execution,
ledger/evidence mutation, HTTP/P2/provider surface, production database write, deployment, publication, release, or
Gate closure. It records the authority decision that must precede any such implementation.

## 1. Verified current boundary

The fixed Slice C candidate is complete within its approved scope:

1. `runner-ledger-preflight/v1` classifies five ledger dispositions and 17 exact recovery pairs;
2. the locked projection kernel opens a dedicated session, performs read-only projection and ledger reread, and
   proves unlock/session cleanup before returning;
3. the evidence-owned binder mints and consumes one exact same-verifier claim;
4. the resulting `runnerLedgerPreflightDispatch` is deliberately ordinary, copyable data with no database session,
   transaction, evidence lease, receipt, verifier artifact, writer token, or mutation port; and
5. `prepareRunnerLedgerPreflightClaim` and `claimRunnerLedgerPreflightDispatch` still have no production caller from
   `Runner.Run`.

The generated v1 profile explicitly freezes:

- `runnerConsumerBoundary = not_implemented`;
- `databaseSessionBoundary = none`;
- `databaseTransactionBoundary = forbidden`;
- `ledgerMutationBoundary = forbidden`;
- `evidenceMutationBoundary = forbidden`; and
- `productionDatabaseWritesBoundary = not_authorized`.

Those values are contract identity, not comments. A consumer or writer cannot be attached by silently weakening the
existing generated profile or by treating the ordinary dispatch as authority.

## 2. Confirmed blockers

### B1 — a new versioned consumer contract is required

`runner-ledger-preflight/v1` must remain immutable. The next slice needs a new generated registry/profile whose exact
identity distinguishes at least:

- complete `return_success` with no migration transaction;
- entry execution admission that still has no SQL/write authority;
- recovery/evidence-only actions;
- reconciliation that may inspect an ambiguous database outcome; and
- closed failure with no retry-as-a-second-transition.

The new profile must bind the preflight dispatch subject, current candidate/generation/journal/recovery identity,
signed runner projection decision, selected entry/plan closure, database identity, ledger prefix, catalog contract,
and exact allowed next transition. It must not edit or reinterpret v1.

### B2 — the public runner rejects the required scope before preflight

`Runner.Run` calls `validateRunnerCurrentExecutionScope` before opening evidence or the database. That validator
accepts exactly one migration with exactly one statement. The new ledger preflight service is called nowhere in the
public runner, so partial and complete ledgers never reach Slice C.

Any integration must preserve the existing early rejection for unapproved shapes while introducing a typed branch
selected only after trust/runtime/evidence binding. A caller-supplied disposition, ledger length, migration ID, or
ordinary dispatch must not choose the branch.

### B3 — claim consumption currently ends at ordinary data

The reviewed one-shot claim proves that a dispatch was minted from the same evidence verifier, but consumption
returns ordinary data and deliberately destroys the claim. That is sufficient for inspection and may support a
separately approved no-op return inside one closed call. It is not sufficient to open or drive a writer.

A writer path needs a distinct, sealed, one-shot transition minted from the exact consumed preflight claim and
consumed only by the fresh execution-session admission kernel. The ordinary dispatch must never be accepted as a
writer permit.

### B4 — the read-only session is closed before the fact exists

Slice B proves unlock and session close before sealing its result. A later writer therefore cannot reuse its
database handle or advisory lock. It must open a fresh dedicated session and repeat, on that same session:

1. connected-session authority;
2. signed role/settings;
3. advisory lock;
4. migration-role authority;
5. exact ledger prefix and cumulative/predecessor catalog;
6. comparison with the preflight subject and current evidence boundary; and
7. one final ledger/catalog reread immediately before minting execution admission.

Any drift, response-lost close/unlock, context failure, or ownership mismatch invalidates the transition. It cannot
fall back to the earlier ordinary fact or retry as a second mutation attempt.

### B5 — the existing writer chain is brand-new-only

The current `runnerPreparedCurrentSession` and all downstream transaction/statement authorities require the exact
brand-new shape:

- empty ledger;
- `brand_new` or `brand_new_inherited`;
- `begin_first_attempt`;
- entry index `0` and attempt index `1`;
- one migration and one statement;
- expected ledger length/head `1`; and
- bundle completion after that first committed terminal.

Partial next-entry, inherited retry, dangling statement/intermediate, ambiguous commit reconciliation, terminal
failure, and complete no-op cannot be enabled by deleting these checks. Each requires a typed successor state and
its own error/cleanup precedence.

## 3. Recommended ordered slices

The smallest safe progression is:

### Slice A — generated consumer registry/profile only

Create a new versioned generated consumer profile. Keep it package-private and pure. It has no `Runner.Run` caller,
database handle, writer token, evidence mutation, or external surface. Freeze the exact mapping from the reviewed
preflight disposition/recovery pair to one closed consumer transition.

### Slice B — complete-ledger no-op consumer

Connect only `complete_return_success` to a typed, one-shot `Runner.Run` no-op result. It must revalidate the exact
claim/evidence boundary, return no applied migrations, report the signed final head and bundle/manifest identities,
perform no `BeginMigration`, SQL, ledger/evidence append, or write, and preserve evidence cleanup/error precedence.
All entry and recovery dispositions remain `MIGRATION_PROJECTION_NOT_IMPLEMENTED`.

### Slice C — fresh-session execution admission, still no SQL

Add the fresh dedicated-session/lock revalidation from B4 and mint a one-shot execution-admission permit for one
explicitly selected entry. This slice stops before `BeginMigration`, evidence append, or SQL. It proves the new
permit cannot be minted from an ordinary dispatch, stale preflight, caller-selected profile, or changed ledger.

### Slice D — writer/recovery kernels and independent review

Only after a separate owner approval may later fixed slices consume execution admission for migration/RW work.
Entry execution, retry/abort evidence, ambiguous commit reconciliation, terminal failure, and multi-statement or
multi-entry advancement must remain separate closed transitions with their own matrices and independent review.

No slice closes a Gate. Production database mutation, deployment, release, and publication remain separately
authorized operations even after local implementation/review succeeds.

## 4. Minimum fault and conformance matrix

| Boundary                | Required cases before approval                                                                                                        |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| Generated selection     | v1/v2 cross-profile, zero/copy/literal, unknown pair/action, caller-selected disposition, digest/profile drift                        |
| Public runner branch    | complete no-op only; every entry/recovery/unknown pair remains fail closed; pre-cancel/deadline; no double consume                    |
| No-op result            | exact final head/bundle/manifest; empty `Applied`; no connector writer call, `BeginMigration`, SQL, ledger/evidence append, or commit |
| Fresh execution session | ledger/catalog unchanged and changed; lock busy/lost; authority/role/settings drift; projection rollback, unlock, close ambiguity     |
| Evidence boundary       | candidate/generation/journal/recovery drift before and after DB reread; evidence close/revoke; foreign verifier; stale/copy claim     |
| Entry selection         | exact next migration and plan closure; reordered/missing/duplicate plans; partial prefix mismatch; attempt/continuation mismatch      |
| Recovery                | each generated append-abort/reconcile/return-failure pair; corrupt before recovery-required; unknown outcome cannot retry mutation    |
| Forbidden surfaces      | zero HTTP/P2/provider/worker/session/turn/execution route registration; zero deployment/publication/Gate-closing changes              |
| Matrix                  | PostgreSQL 15/16/17 metadata plus live ephemeral DB only when separately approved; normal/race; fixed toolchain; independent review   |

## 5. Owner decision required

Recommended next approval is intentionally narrow:

> Approve a new versioned generated runner-ledger consumer profile followed by the complete-ledger
> `return_success` no-op consumer and its matrix/independent review. Keep `runner-ledger-preflight/v1` immutable; keep
> entry/recovery writer paths `NOT_IMPLEMENTED`; do not authorize production database writes, HTTP/P2/provider
> effects, deployment, publication, release, or any Gate closure.

Fresh-session execution admission and every mutating writer/recovery slice remain separately approved future work.
