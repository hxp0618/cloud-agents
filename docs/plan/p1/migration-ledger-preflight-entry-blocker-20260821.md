# P1 runner ledger/catalog preflight entry blocker - 2026-08-21

- Status: **PROPOSED - OWNER APPROVAL REQUIRED**
- Branch: `codex/cloud-agents-p1-ledger-preflight-contract-20260821`
- Scope: a future, read-only runner ledger disposition/catalog preflight contract
- This proposal is not an implementation approval and does not alter the current runner behavior

## 1. Why this is a separate entry

The approved A2.4 entry covers its versioned compatibility/recovery registry, append-only PostgreSQL writer
kernel, typed service/claim wiring, and its review matrix. Runner ledger/catalog preflight is a different authority
boundary: it selects the cumulative schema/catalog projection for the current database and must eventually bind
that result to the evidence recovery state machine before a transaction can start.

The current runner deliberately keeps that boundary closed. `prepareCurrentDatabaseSession` reads and validates the
ledger prefix on the locked dedicated session, but returns `NOT_IMPLEMENTED` for both a non-empty partial ledger and
a complete ledger. The empty-ledger path is the only path that can bind the existing brand-new recovery snapshot.
`runCurrentSingleEntry` still accepts only one migration and one statement and is the only production write-chain
consumer. These are intentional fail-closed limits, not permission to infer a recovery or no-op result.

## 2. Confirmed blockers before a production consumer

1. **Complete ledger requires an explicit no-op authority.** A complete, catalog-matching ledger cannot fall through
   to the current statement writer. The future contract must prove that the signed bundle is already complete, that
   the database catalog is the exact cumulative catalog for the final applied head, and that the evidence terminal
   state permits a deterministic `return_success`/no-op. A generic `complete` boolean or a reused one-entry
   `Run` path is not sufficient.
2. **Partial ledger requires evidence recovery cross-binding.** A non-empty prefix must select the exact last-applied
   entry/catalog and the exact next entry or retry/repair action. Dangling statement/intermediate/commit, ambiguous
   terminal, continuation, and next-entry cases require the same-verifier evidence facts and owner-bound recovery
   artifacts. The catalog projection alone cannot mint that authority.
3. **The read must stay one closed session and be revalidated.** The existing order (trust, connected authority,
   role/settings, advisory lock, migration-role authority, ledger, cumulative catalog, ledger reread) must remain
   exact. Any ledger/catalog drift, lease/connection ambiguity, or post-read mismatch is a stable failure; it is not a
   retryable second transition.
4. **The result must not be a capability by construction.** A pure disposition fact may be copied and inspected, but
   it must not contain a database session, transaction, evidence lease, receipt, verifier-owned artifact, or writer
   token. A later binder must cross-bind all of those inputs in one explicitly reviewed transition.

## 3. Proposed ordered slices (not approved)

### Slice A - generated, versioned pure contract/profile

Add a new versioned internal registry/profile for `runner-ledger-preflight` only after owner approval. The generated
profile should define the closed disposition/state machine and exact identity domains, without exposing HTTP or a
public API. At minimum it must distinguish:

- `empty_brand_new`;
- `partial_next_entry`;
- `partial_retry_or_recovery`;
- `complete_return_success`;
- `unknown_or_failed`.

The profile must bind the schema-bundle/lineage digest, ordered migration prefix digest and head, last-applied
catalog contract digest, next-entry identity where applicable, evidence recovery disposition, and the exact
`NOT_IMPLEMENTED`/unknown precedence. It must not accept a caller-selected profile, a guessed migration name, or a
lossy identity mapping.

The Go contract should remain an ordinary, package-private fact until a later binder exists. It must be independent
of database handles and must have deterministic canonical bytes, clone/zero/cross-profile rejection, and state-table
tests. No runner consumer is added in this slice.

### Slice B - locked, read-only catalog projection kernel

Using the existing dedicated session and advisory-lock order, implement only the projection needed by the selected
disposition. For every state, the kernel must:

- read and validate the exact ledger prefix and digest;
- select the signed cumulative catalog for the applied head (or the signed initial predecessor for an empty prefix);
- project the required authority, schema, role/settings, database identity, and catalog facts;
- reread the ledger and reject any prefix/catalog/session drift;
- return a sealed read-only result with no `BEGIN`, append, commit, evidence mutation, or writer token.

The kernel must preserve the current close/lock/error precedence. It may not call `runCurrentSingleEntry`, open a
migration transaction, or reinterpret a complete ledger as the existing one-entry execution scope.

### Slice C - typed recovery/no-op dispatch, matrix, and independent review

Only after A and B are independently fixed should a typed service-owned binder combine the read-only result with the
same-verifier evidence recovery facts. The dispatch must be a closed result, for example:

| Ledger disposition     | Required cross-bind                                         | Allowed next action                        |
| ---------------------- | ----------------------------------------------------------- | ------------------------------------------ |
| empty                  | initial schema scope + brand-new snapshot                   | existing brand-new entry only              |
| partial next-entry     | last catalog + ordered next-entry evidence                  | a separately approved continuation entry   |
| partial retry/recovery | exact dangling/ambiguous artifacts and terminal boundary    | recovery/reconcile only                    |
| complete               | final cumulative catalog + complete terminal/no-op evidence | explicit `return_success`, no writer       |
| unknown/failed         | connection/ledger/catalog/error precedence                  | fail closed; no retry-as-second-transition |

The service/claim matrix must cover PostgreSQL 15/16/17, empty/partial/complete states, stale and cross-bundle
identity, ledger/catalog drift, response-lost or close ambiguity, lease/lock expiry, recovery-required versus
corrupt precedence, and forbidden-surface scans. An independent reviewer must inspect the generated profile, locked
projection, evidence cross-bind, and no-op/write-chain separation before any consumer is called complete.

## 4. Explicit non-claims and requested decision

Until this proposal is separately approved:

- current partial and complete ledger paths remain `NOT_IMPLEMENTED`;
- no runner transaction, append, commit, ledger mutation, evidence mutation, or production database write is
  authorized;
- no HTTP route, P2, provider, worker, session, turn, execution, deployment, release, publication, or Gate closure
  is authorized;
- A2.4 registry/writer/service evidence is not reused as runner ledger authority.

**Requested owner decision:** approve or reject the three proposed runner-ledger preflight slices as a separate
versioned entry. Approval would authorize contract/profile work only in Slice A first; it would not authorize a
transaction consumer or production database write.
