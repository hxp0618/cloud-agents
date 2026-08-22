# Runner ledger recovery abort-terminal service independent review — 2026-08-23

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `6fd2873497765336d872dfaa53cd4e12541c5c26`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-abort-terminal-20260823`
- Review branch: `codex/cloud-agents-p1-runner-recovery-abort-terminal-independent-review-20260823`
- Gate effect: none; this review does not start Slice D or close or advance any Gate

This is an independent read-only review of ADR-0023/D-047 Slice C. It approves only the fixed
abort-terminal evidence writer for four generated dangling intent/intermediate pairs. It does not
approve any other recovery writer, a public recovery success result, a database mutation, or an
external side effect.

## Fixed identity and scope

| Identity                               | Value                                                              |
| -------------------------------------- | ------------------------------------------------------------------ |
| fixed candidate                        | `6fd2873497765336d872dfaa53cd4e12541c5c26`                         |
| fixed candidate tree                   | `79e45a581d45602d8ec72eccd3568fa3a94cbb6f`                         |
| approved Slice B review base           | `4808d20e1d36f5f0bb6efe557ccc6347e955bab0`                         |
| Slice C code commit                    | `f85cce616de3ab978e03ef5b3b7ca5a22021df21`                         |
| fixed `services/control-plane` subtree | `f8f773da29e12638afac46d15061f28ffd3f0c65`                         |
| abort-terminal kernel SHA-256          | `ec670389b6ce5c9bb432c934da26e5a9896ff5f08bc4e98e1648e32ffd437a0d` |
| abort-terminal test SHA-256            | `db9cabad03c5d6232853dd2015ca185eaea53f9e7d2b6ced5dc24613d51ea393` |
| recovery admission permit SHA-256      | `edc0357053811d559823f54ad0f8f5716ecd9950752a2980318d8e6924e5b36e` |
| recovery consumer service SHA-256      | `b2e84ecae7906f487058a63fea34baad27ad8a4417a37f7451831e36a05c7719` |
| generation lock SHA-256                | `d00246a7699284909d427df48cec085ccbd99a80c56d28ac298d5e772993f269` |
| implementation matrix SHA-256          | `eebf9bdd374de51956f91d882543d225a498d28139bbc9a0acb5fee9b791c458` |

The candidate was clean, its upstream was `0/0`, and the remote candidate branch resolved to the
exact commit. The approved Slice B review-to-candidate diff is exactly 13 paths: one generation
lock, seven migration code/test paths, and five documentation paths. This review did not edit the
candidate.

## Closed action and authority graph

Independent extraction reproduced exactly four direct pairs under the immutable
`runner-ledger-abort-terminal-writer/v1` identity:

| Recovery state              | Selected action            | Durable outcome     | Next action          |
| --------------------------- | -------------------------- | ------------------- | -------------------- |
| `dangling_statement_intent` | `append_aborted_retryable` | `aborted_retryable` | `begin_next_attempt` |
| `dangling_statement_intent` | `append_aborted_terminal`  | `aborted_terminal`  | `return_failure`     |
| `dangling_intermediate`     | `append_aborted_retryable` | `aborted_retryable` | `begin_next_attempt` |
| `dangling_intermediate`     | `append_aborted_terminal`  | `aborted_terminal`  | `return_failure`     |

The other eight generated recovery pairs still consume their Slice B admission through
`close_without_mutation`. The distinct commit-observation, ambiguous-resolution, retry-handoff,
recovery-execution, recovery-success, and return-failure identities have no Slice C writer caller.
The public consumer continues to return its stable
`MIGRATION_PROJECTION_NOT_IMPLEMENTED` result after the exact abort terminal is durably appended.

The Slice B admission permit transfers the same-verifier candidate, generation, full evidence
boundary, recovery tail, runtime/plan closure, ledger/catalog/authority observation, database
identity, and exact action into one abort-specific seed. The retained locked database session is
fully revalidated, then unlock/reset/closed before a lifecycle receipt or writer permit is minted.
Close uncertainty dominates and no database handle enters the evidence writer.

The lifecycle receipt is the typed
`precommit_connection_terminated_exact_predecessor` receipt. It binds one shared ordered lifecycle
token, the exact migration/attempt/generation, closed old handle, ledger prefix, predecessor and
observed catalog, and migration authority. The writer permit additionally binds the immutable
generated writer/predecessor identities, admission canonical, evidence boundary, current cursor,
terminal body, entry/plan closure, projection subject, and database identity. Registry ownership,
self identity, shared atomic cursor/consume cells, canonical digest, and same concrete binder make
the permit non-copyable and one-shot.

Only the sealed concrete generation evidence session can consume that permit. It revalidates the
live current generation, recovery digest/tail/cursor, full stored prefix, schema ordering and retry
budget, header authority, and exact receipt. It adds the receipt to an owned chain clone, binds one
typed `AttemptTerminalState`, and exposes that record to exactly one `AppendDurable` call.

## Mutation, result, and failure closure

The terminal distinguishes intent-only from intermediate recovery, rejects any commit/terminal/
resolution successor, preserves the exact durable predecessor, and rejects retryable abort at the
signed maximum attempt. The append validator independently reproduces the candidate frame and
checks checkpoint identity, old-cursor invalidation, distinct next-cursor validity, sequence,
segment, lineage-index, and optional rotation header/checkpoint arithmetic.

Unknown or contradictory mutation results return `MIGRATION_EVIDENCE_RECOVERY_REQUIRED`; pre-
mutation zero-result failures retain only their stable sanitized class. Durable success must match
the current journal pointer and a complete post-append recovery snapshot, including intent and
optional intermediate bodies, terminal digest/body, new cursor/tail, and exact next action. Literal,
copy, registry-missing, foreign binder/journal/cursor, changed record/plan/database facts, and second
consume paths cannot mint or reuse writer authority.

Diff-scoped and AST review found exactly one `AppendDurable` call and no new `BeginMigration`, SQL
execution, database transaction/commit, ledger insert, generation supersession/reservation/
activation, HTTP/P2/provider, deployment, or publication edge.

## Fresh checks

The review used Bun `1.3.14` and Go/gofmt `1.26.6`:

- fixed commit/tree/subtree, remote exact ref, clean state, upstream `0/0`, exact 13-path scope, and
  listed SHA-256 values: PASS;
- exact four-pair append, unknown-append cursor revocation, and forbidden-edge focused tests: PASS
  in `11.903s`;
- recovery registry and recovery Go generators: PASS/current;
- changed Go files under `gofmt -d`, changed documentation under repository `oxfmt --check`, and
  `git diff --check`: PASS;
- generated registries/profiles/schemas/fixtures and historical runner-v1 files have no candidate
  diff; the generation lock changes only the recovery Go profile test input manifest; and
- independent Gitleaks over `4808d20..6fd2873`: PASS, three commits and approximately `97.83 KB`,
  no leaks.

The candidate record's bounded normal/race, compile, vet/build, and generation-lock current results
were reviewed as candidate evidence and were not redundantly rerun in full. No full
`internal/migration`, full shard suite, broad race, live PostgreSQL, production database, or
external-side-effect test was run or claimed. The in-process database/evidence fixtures are not a
live production authority invocation. No timeout, skipped check, or metadata-only result is a PASS.

## Non-claims

This review does not modify the fixed candidate, start Slice D, implement or authorize any other
recovery writer/result, or convert the public recovery result to success. It does not merge or open
a pull request, connect to a live or production database, execute SQL, mutate a database or ledger,
add HTTP/P2/provider behavior, deploy, publish, release, or close a Gate.

The verdict approves only fixed Slice C candidate
`6fd2873497765336d872dfaa53cd4e12541c5c26` as the reviewed four-pair abort-terminal evidence
writer closure.
