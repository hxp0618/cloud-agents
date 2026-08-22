# Runner ledger recovery commit-reconciliation service matrix — 2026-08-23

- Status: `SLICE_D_FIXED_IMPLEMENTATION_PENDING_INDEPENDENT_REVIEW`
- Approved Slice C candidate: `6fd2873497765336d872dfaa53cd4e12541c5c26`
- Slice C independent review: `be597debb7b3edfb68616dff5ae648284c796ab7` — `APPROVE, P0=0/P1=0/P2=0`
- Slice D code commit: `7214a5c6c42d68f577abf1ea3f0f4839b0d2e112`
- Slice D code tree: `70c8db60064ca5a39ec2c63137ed1a28340eb908`
- Slice D control-plane subtree: `5c2000cdc6a6838b8cfa2dfeeef4475e482c9790`
- Slice D branch: `codex/cloud-agents-p1-runner-recovery-commit-reconciliation-20260823`
- Decision: [`D-047 / ADR-0023`](runner-ledger-recovery-contract-decision-20260822.md)
- Gate effect: none; every immutable and aggregate Gate remains open

This record covers only ADR-0023 Slice D's dangling-commit observation terminal and adjacent unresolved-resolution
pairs. It does not implement retry handoff, recovery execution, recovery success, or typed failure. It does not
authorize production database writes, HTTP/P2/provider behavior, deployment, publication, release, main merge, or
Gate closure.

## Closed action mapping

The versioned `runner-ledger-commit-observation-writer/v1` and
`runner-ledger-ambiguous-resolution-writer/v1` remain distinct generated identities, permits, registries, record
binders, and append kernels. Neither can consume the other's authority.

| Recovery state           | Generated writer action              | Fresh read-only classification | Durable record outcome           | Next recovery action                     |
| ------------------------ | ------------------------------------ | ------------------------------ | -------------------------------- | ---------------------------------------- |
| `dangling_commit_intent` | `append_commit_observation_terminal` | `exact_committed`              | `ambiguous_reconciled_committed` | next entry or `return_success`           |
| `dangling_commit_intent` | `append_commit_observation_terminal` | `exact_pending`                | `ambiguous_reconciled_pending`   | next attempt, or `return_failure` at max |
| `dangling_commit_intent` | `append_commit_observation_terminal` | `divergent`                    | `ambiguous_divergent`            | `return_failure`                         |
| `ambiguous_unresolved`   | `append_ambiguous_resolution`        | `exact_committed`              | `resolved_committed`             | next entry or `return_success`           |
| `ambiguous_unresolved`   | `append_ambiguous_resolution`        | `exact_pending`                | `resolved_pending`               | next attempt, or `return_failure` at max |
| `ambiguous_unresolved`   | `append_ambiguous_resolution`        | `divergent`                    | `resolved_divergent`             | `return_failure`                         |

The six implemented state/outcome rows span PostgreSQL 15, 16, and 17 fixtures. Operationally unknown observation
authorizes no append. The four Slice C abort pairs remain unchanged; the other six generated recovery pairs retain
`close_without_mutation`. The public consumer still returns its stable recovery `MIGRATION_PROJECTION_NOT_IMPLEMENTED`
after a successful Slice D append; later families remain ordered behind their own fixed-candidate reviews.

The immutable generated v1 pair is only `partial_retry_or_recovery`, which requires a non-empty durable predecessor
prefix. A valid first-entry dangling commit with an empty durable prefix is therefore still unsupported: it falls back
to the existing close-only/`NOT_IMPLEMENTED` path, is not labeled corrupt, and cannot mint either Slice D writer.

## Read-only classification and mutation sequence

For one selected pair, the package-private service performs this sequence:

1. derive an ordinary, full-CommitIntent-bound reconciliation hint only from a valid same-generation recovery
   snapshot; the hint carries no append authority;
2. open a fresh locked migration-role session, read the exact ledger prefix, project the expected catalog through the
   same verified bindings, and classify exact pending, exact committed, or divergent; a stable catalog-drift result is
   known divergent, while context or operational uncertainty returns without append;
3. bind the ordinary preflight fact to the durable evidence prefix, exact ordered row, historical catalog domains,
   generation, migration, attempt, checkpoint, terminal/resolution boundary, runtime, plan closure, and full-root
   evidence claim;
4. reopen a fresh locked observation for recovery admission, repeat ledger/catalog/role/lock/session validation, and
   reread the full evidence boundary;
5. unlock, reset, and close that exact read-only database session before minting mutation authority; cleanup
   uncertainty mints no writer;
6. bind a closed receipt over the classification, observed prefix, database identity, preflight/admission canonicals,
   consumer fact, and evidence boundary;
7. mint exactly one profile-specific, registry-backed, pointer/owner/cursor-bound writer permit and let only the
   concrete generation evidence session consume it;
8. reread and validate the physical evidence prefix, bind the exact owned terminal or immediately adjacent resolution,
   and perform exactly one composite `AppendDurable`, including its checkpoint and optional rotation; and
9. require the returned cursor, record/checkpoint/rotation identities, current journal pointer, typed predecessors,
   post-append recovery snapshot, terminal/resolution body, outcome, and next action to match exactly before returning.

No migration transaction, SQL execution, ledger insert, database commit, generation supersession, successor
reservation/activation, HTTP, P2, or provider edge exists in the Slice D kernels.

## Failure and crash closure

- Stored evidence, full prefix, commit body, unresolved terminal adjacency, signed row, catalog, generation, cursor,
  plan, receipt, database identity, or outcome contradiction fails closed before append where knowable.
- Valid shorter or longer signed prefixes are classified divergent; a malformed signed ledger is not downgraded to a
  reconciliation result.
- Context, read, projection, role, session-idle, advisory-lock, reset, unlock, or close uncertainty dominates and mints
  no writer. The exact registered database session is still closed when the live admission wrapper or binder drifts.
- A zero-valued append error with the old cursor still valid retains its stable pre-mutation error class. Any non-empty
  append result metadata, invalidated old cursor, unknown outcome, durable-result contradiction, or post-append
  snapshot drift returns `MIGRATION_EVIDENCE_RECOVERY_REQUIRED` and revokes the relevant cursor authority.
- Literal, copy, registry-missing, foreign binder/journal/cursor, changed record/receipt/classification/database
  identity, and second-consume inputs cannot bind or reuse either writer.

## Focused conformance

The fixed code commit was checked with Node `24.13.1`, Bun `1.3.14`, and Go/gofmt `1.26.6 darwin/arm64`:

- one pre-freeze exact named normal suite covering historical catalog-domain facts, generated 12-pair
  preflight/admission,
  consumer dispatch, unsupported fallbacks, both Slice D writers, the six state/outcome rows, valid-short-prefix
  divergence, immutable-v1 empty-prefix fallback, operational unknown, final barriers, one-shot/cross-profile binding,
  and AST writer boundaries: PASS in `156.977s`;
- after the final admission-claim cleanup hardening, the exact six-row writer matrix passed in `18.329s` and the exact
  registered-session wrapper/binder drift cleanup test passed in `6.525s`;
- a narrower race command covering the six rows, short-prefix divergence, final barriers, one-shot permits, and
  registered-session cleanup reached its explicit `10m` timeout in `601.207s` while executing the barrier matrix:
  **NOT PASS**; it emitted no failed assertion before the bounded stop and is not claimed as race evidence;
- `go vet ./internal/migration` and `go build ./internal/migration`: PASS;
- recovery registry generator and recovery Go generator: current;
- generation-lock writer/checker: current;
- changed Go files: `gofmt` clean; `git diff --check`: PASS;
- code-commit Gitleaks scan: PASS, one commit and approximately `177.05 KB` scanned.

The generation lock SHA-256 is
`ba6eb1409a882c670e49fa08090c7bd8d149db6df0bf277e3c6d505c0f1a9340`. Its only lock change from Slice C is the
recovery Go profile suite input-manifest SHA-256,
`sha256:0993e3131bb3c7db0390bb0d3d8fada01395a08861afd387deb14034d596fedb`, because the suite now binds the Slice D
implementation and tests. All generated recovery registry, profile, fixture, schema, and historical runner v1 bytes
remain unchanged.

Key implementation SHA-256 values:

- observation: `34469339b210b53ed80407e8f6ef2d6e73ca7c40dae572dd9bdaae1806640765`;
- writer kernel: `3df1bd51cffd966ae9d2b721bc3a5f9da2fdbc0f197ff340c281041211ff475b`;
- writer matrix: `6d5d7e2e64141a95cff26f8c6d95501a9f1a6a2e9cc61cf72456cb277190a6ef`;
- catalog preflight: `075802ce629b2726a6e4e7950b05551eef39a9ee470d7f01994d56110ac69b62`;
- recovery admission routing: `93182b3d192455ac561cbc86100755c901672124d38cf3ca81b1deafd93fcaad`.

No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, HTTP/P2/provider,
deployment, publication, release, or Gate check is claimed. PostgreSQL 15/16/17 and evidence behavior are in-process
fixtures; this record does not claim a live production authority invocation.

## Independent review boundary

Slice D remains incomplete until a fixed candidate containing this record receives an independent read-only P0/P1/P2
verdict. Slice E must not begin on a `BLOCK` verdict. An `APPROVE` verdict closes only Slice D's local implementation
and review boundary; it cannot authorize production side effects or close any Gate.
