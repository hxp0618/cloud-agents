# P1 runner ledger recovery contract audit - 2026-08-22

- Status: **CONTRACT-ONLY AUDIT; RECOVERY AUTHORITY NOT APPROVED**
- Audited source: `b72664291b0a6324e1b2c463d63a156a7b92773f`
- Repository tree: `e7afe8d7a740c3c6c83443e8fc985aead43465ec`
- Control-plane subtree: `c78ffc27c88b0f50871795a281669b7b2ef9bd27`
- Proposed decision: [`ADR-0023`](../adr/0023-p1-runner-ledger-recovery-writer-contract.md)
- Scope: read-only source audit and versioned contract proposal only
- Independent review: [`6d4da5b`, `APPROVE, P0=0/P1=0/P2=0`](runner-ledger-recovery-contract-audit-independent-review-r2-20260822.md)
- Review record SHA-256: `e8192b5ae4525ed11717935b2e821e9160f0f24161d3868f8869a4d2cfeb924c`

This record does not add or approve a generated profile, claim, permit, database session, transaction, SQL execution,
ledger/evidence mutation, lineage mutation, `Runner.Run` branch, HTTP/P2/provider surface, production database write,
deployment, publication, release, main merge, or Gate closure. Every audited pair continues to return
`MIGRATION_PROJECTION_NOT_IMPLEMENTED` in the fixed source.

## 1. Fixed inputs

| Input                                   | Repository path                                                                         | SHA-256                                                            |
| --------------------------------------- | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Generated consumer v1 registry          | `contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json`         | `fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852` |
| Generated consumer Go profile           | `services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go` | `afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928` |
| Recovery snapshot and action classifier | `services/control-plane/internal/migration/evidence_recovery.go`                        | `301cb8836b5223fe5c8be7903682dac27c79f678ca49dc5f8e86dc5ca503de75` |
| Evidence contracts and validators       | `services/control-plane/internal/migration/evidence_contract.go`                        | `03c8c201d78899ea5b1f3a57e1350ea3f19579668f6eb35c4c5384b6c474bffa` |
| Generated consumer and caller           | `services/control-plane/internal/migration/runner_ledger_consumer_service.go`           | `cbd2027adfa2dff557cc359da1932575bd05abc50f1b08370ed48d38a7546ac3` |
| Reviewed first-attempt success kernel   | `services/control-plane/internal/migration/runner_ledger_entry_success_kernel.go`       | `9a3b7388df68f8cf0967f2a19f9dde45b78aacf47ec7962fe355ea2bf79fa567` |
| Commit observation classifier           | `services/control-plane/internal/migration/runner_commit_protocol.go`                   | `4b0dde6a32af86b57507c70bf44cda93855ac4e1f3bca6b35b4f78458bc0b6f2` |

The generated consumer registry file binds registry digest
`sha256:f1248220aa18f6f7c85792e4b168fcc18a172ddeca65778686d033535617ed43` and profile digest
`sha256:11015e7d78a3edb5adc0e666c2898ca4cc3aaeae097537c867b25aaf0d22f732`.
Those v1 identities remain immutable.

## 2. Exact unsupported matrix

The fixed consumer has one entry pair excluded by ADR-0022 and eleven pairs mapped to
`recovery_not_implemented`:

| #   | Disposition                 | Recovery state              | Recovery action            | Required authority family         |
| --- | --------------------------- | --------------------------- | -------------------------- | --------------------------------- |
| 1   | `empty_brand_new`           | `brand_new_inherited`       | `begin_next_attempt`       | recovery execution                |
| 2   | `partial_retry_or_recovery` | `brand_new_inherited`       | `begin_first_attempt`      | recovered first-attempt execution |
| 3   | `partial_retry_or_recovery` | `brand_new_inherited`       | `begin_next_attempt`       | recovery execution                |
| 4   | `partial_retry_or_recovery` | `dangling_statement_intent` | `append_aborted_retryable` | abort terminal append             |
| 5   | `partial_retry_or_recovery` | `dangling_statement_intent` | `append_aborted_terminal`  | abort terminal append             |
| 6   | `partial_retry_or_recovery` | `dangling_intermediate`     | `append_aborted_retryable` | abort terminal append             |
| 7   | `partial_retry_or_recovery` | `dangling_intermediate`     | `append_aborted_terminal`  | abort terminal append             |
| 8   | `partial_retry_or_recovery` | `dangling_commit_intent`    | `reconcile_commit`         | commit observation terminal       |
| 9   | `partial_retry_or_recovery` | `ambiguous_unresolved`      | `reconcile_commit`         | adjacent ambiguous resolution     |
| 10  | `partial_retry_or_recovery` | `terminal`                  | `begin_next_attempt`       | retry lineage handoff             |
| 11  | `partial_retry_or_recovery` | `terminal`                  | `return_failure`           | typed failure result              |
| 12  | `partial_retry_or_recovery` | `divergent`                 | `return_failure`           | typed divergent result            |

The table is a classification of existing generated facts, not a permit table. A future registry must bind the exact
source pair, current replay boundary, and one action-specific authority. It must not accept a caller-selected row or
promote `runnerLedgerConsumerFact` into mutation authority.

## 3. What exists and what does not

### 3.1 Reusable read-only facts

- `RecoverySnapshot` already classifies the closed state/action enum and retains bounded, defensive copies of the
  final intent, intermediate, commit, terminal, resolution, continuation, record digests, and current cursor.
- Same-verifier replay already checks the signed statement plan, execution policy, final catalog, ordered migration
  prefix, retry receipts, ambiguous commit boundary, lineage continuation, and generation identity.
- The evidence wire contract already validates `AttemptTerminalState`, `RetryProofEvidence`, and
  `AmbiguousResolutionState`, including terminal/resolution self digests and closed outcome combinations.
- Evidence append/checkpoint/rotation and lineage supersession primitives already exist behind sealed package-private
  authorities. They are mechanisms, not permission to call them from the runner recovery path.

### 3.2 Existing types that cannot be reused as writer authority

- `runner-ledger-consumer/v1` produces ordinary copyable data and explicitly keeps `recoveryWriter=not_implemented`.
- `runner-ledger-entry-admission/v1` is close-only. Its permit cannot open a transaction or append evidence.
- ADR-0022 execution-admission and success-writer v1 admit only four first-attempt pairs; their authority binds
  attempt one and no previous-attempt terminal.
- The historical brand-new writer hard-codes empty ledger, entry zero, attempt one, one statement, segment zero, and
  fixed early cursor positions.
- `runLegacyCharacterization` and `reconcileAmbiguous` are characterization-only legacy code with no production
  `Runner.Run` call edge and no registry-backed same-verifier recovery permit.
- Wire DTO validation, a durable replay summary, a PostgreSQL error, an idle-looking connection status, or a ledger
  read alone is not a receipt and cannot authorize an append or retry.

### 3.3 Missing production authorities

The fixed source has no production binder/kernel that can:

1. mint a terminal append from an owned rollback, terminated-connection, or exact commit-rejection receipt;
2. open a fresh locked reconciliation session and bind an exact committed/pending/divergent database observation;
3. append the first post-commit terminal for a dangling `CommitIntent`;
4. append an adjacent `AmbiguousResolutionState` after an unresolved terminal;
5. consume a terminal/resolution into an exact retry lineage supersession and successor generation;
6. execute an inherited or attempt-greater-than-one success path; or
7. return a public failure result bound to an exact terminal/divergent replay without converting it into a permit.

## 4. Threat conclusions

### T1 - classification is not capability

All twelve pairs have typed facts today, but none has mutation authority. The future entry must consume a new sealed,
registry-backed, same-verifier claim after a fresh full replay. Literal, copied, stale, foreign-owner, replaced, or
second-use facts fail before opening a transaction or appending evidence.

### T2 - abort needs an external lifecycle receipt

A dangling intent or intermediate does not prove that the old transaction rolled back. A retryable or terminal abort
can be constructed only from one exact owned rollback, irrevocably terminated old handle, or confirmed commit-reject
receipt cross-bound to the predecessor ledger/catalog/authority and current journal tail. Reopening a new connection
is not proof that the old lifecycle ended.

### T3 - commit reconciliation has two different append authorities

`dangling_commit_intent` has no terminal and may append exactly one observed terminal after a fresh database
observation. `ambiguous_unresolved` already has a terminal and may append only its immediately adjacent resolution.
The two record kinds, predecessor digests, and one-shot registries must remain distinct even if they share a read-only
database observation kernel.

### T4 - retry handoff is not retry execution

A terminal or resolved-pending old generation first authorizes exact
`GenerationSuperseded -> GenerationReserved -> header -> GenerationActivated` handoff with a
`begin_next_attempt` continuation. The inherited successor then needs a separate fresh-session execution admission.
Neither the old terminal nor the new header-only snapshot directly authorizes SQL.

### T5 - retry execution needs distinct admission and writer identities

Attempt greater than one binds an exact previous terminal, retry proof, predecessor catalog/ledger, generation
continuation, and max-attempt budget. Removing attempt-one checks from ADR-0022 v1 would change its generated identity.
A future retry execution path must use one distinct close-only execution-admission profile and a second distinct
one-shot success-writer profile while preserving the same multi-statement/dynamic-cursor/commit-once barriers. Neither
profile, its permit, nor an ordinary writer outcome can stand in for the other authority.

### T6 - return failure is read-only but still typed

`terminal/return_failure` and `divergent/return_failure` need no mutation, but a public failure result must still be
sealed to the exact replay, stable error, terminal/resolution, ledger/catalog prefix, and bundle identity. It cannot be
returned from an ordinary caller-supplied error or used as retry authority.

## 5. Proposed contract split

The minimum safe direction is a common generated read-only recovery admission plus action-specific identities:

1. `runner-ledger-recovery-admission/v1` — consumes the immutable consumer-v1 fact, performs fresh full
   evidence/ledger/catalog/authority revalidation, and mints only one action-specific permit;
2. `runner-ledger-abort-terminal-writer/v1` — the four dangling intent/intermediate abort pairs;
3. `runner-ledger-commit-observation-writer/v1` — dangling commit intent to one observed terminal;
4. `runner-ledger-ambiguous-resolution-writer/v1` — unresolved terminal to one adjacent resolution;
5. `runner-ledger-retry-handoff/v1` — terminal/resolved-pending to exact successor generation continuation;
6. `runner-ledger-recovery-execution-admission/v1` — selects one inherited first/retry attempt and initially supports
   only `close_without_mutation`; it cannot open a transaction, execute SQL, or append evidence;
7. `runner-ledger-recovery-success-writer/v1` — consumes only the exact one-shot permit from identity 6, opens one
   fresh session, executes one exact inherited/retry attempt, and cannot be selected directly from a consumer fact or
   ordinary outcome; and
8. `runner-ledger-return-failure/v1` — the two immutable typed failure results, with no mutation capability.

The names and split are proposed by [`ADR-0023`](../adr/0023-p1-runner-ledger-recovery-writer-contract.md). They are
not accepted by this audit and must not be generated or implemented until that decision receives explicit owner
approval.

## 6. Required implementation order after approval

1. generated source schemas, fixtures, registries, Go profiles, manifests, lock, and historical same-bits only,
   including distinct recovery execution-admission and recovery success-writer identities;
2. common read-only recovery admission/claim and action-specific close-only permits;
3. abort-terminal writer and independent review;
4. commit-observation and ambiguous-resolution writers with fresh ephemeral PG15/16/17 reconciliation matrix and
   independent review;
5. retry lineage handoff and crash/reopen matrix;
6. recovery execution-admission plus its separately versioned success-writer kernel, one attempt per fresh session,
   explicit cross-profile/outcome-conversion rejection, and independent review;
7. typed return-failure result, bounded caller loop, complete 12-pair matrix, and final independent review.

Every slice is fixed and reviewed separately. Unknown append/commit/cleanup outcomes revoke the old cursor and return
recovery-required; they never retry a mutation from an ordinary fact. Production database writes, HTTP/P2/provider,
deployment, publication, release, main merge, and Gate closure remain outside the proposal.

## 7. Audit closure criteria

This document-only slice is complete only when formatting, links, fixed hashes, the exact 12-pair extraction,
forbidden-call-edge scan, and an independent read-only P0/P1/P2 review are recorded. No Go, migration, live PostgreSQL,
or broad race result is required or claimed for the audit itself.

## 8. Local audit evidence

The fixed worktree produced the following document/source-audit results before independent review:

| Check                          | Result                                                                                                                                                             |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Base identity                  | `HEAD=b72664291b0a6324e1b2c463d63a156a7b92773f`, tree `e7afe8d7a740c3c6c83443e8fc985aead43465ec`, control-plane subtree `c78ffc27c88b0f50871795a281669b7b2ef9bd27` |
| Fixed source SHA-256           | all seven paths in section 1 matched exactly                                                                                                                       |
| Generated pair extraction      | `12` total: `1` `entry_not_implemented` plus `11` `recovery_not_implemented`; ordered rows matched section 2 exactly                                               |
| Production/source drift        | `contracts/`, `services/`, `scripts/`, package and lock inputs byte-identical to `HEAD`; changed paths are documentation only                                      |
| Proposed production identities | zero occurrences under production `contracts/`, `services/`, or `scripts/`; the names exist only in this proposal/audit                                            |
| Markdown                       | target `oxfmt 0.62.0 --check`, local-link existence check, and `git diff --check` passed                                                                           |
| Secret scan                    | Gitleaks stdin scan of the candidate patch passed with no findings                                                                                                 |
| Runtime suites                 | intentionally not run: this slice changes no runtime/generated source and makes no runtime conformance claim                                                       |

The superseding fixed candidate `deb3dc61fe3fba11c4b09af1b714dbae592554ed` received independent review commit
`6d4da5ba6c2f9cff5b08ae48fb28d8dbbf5e1e5f` with `APPROVE, P0=0/P1=0/P2=0`. The earlier candidate/review remains
preserved and explicitly invalidated by `395a5fd508bd74da0a03fc1939e04a776d006400`; it is not reusable evidence.
This closes only the contract-only audit/review preparation, not ADR acceptance, implementation authority, any
immutable/aggregate Gate, or Platform RC.
