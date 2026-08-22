# Runner ledger recovery contract audit independent review — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `15b9b232cc2005b5011dfa3c24e361b7b020d9e4`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-contract-audit-20260822`
- Review branch: `codex/cloud-agents-p1-runner-recovery-contract-audit-independent-review-20260822`
- Gate effect: none; this review does not accept ADR-0023 or close or advance any Gate

## Fixed identities

The review resolved these identities before inspection and again before creating this record:

| Identity                                         | Value                                      |
| ------------------------------------------------ | ------------------------------------------ |
| fixed candidate commit                           | `15b9b232cc2005b5011dfa3c24e361b7b020d9e4` |
| fixed candidate tree                             | `4b10fe3f162f0aa1cf7f00af1d2b360d60ffe8a9` |
| fixed source/base commit                         | `b72664291b0a6324e1b2c463d63a156a7b92773f` |
| fixed source/base tree                           | `e7afe8d7a740c3c6c83443e8fc985aead43465ec` |
| fixed candidate `services/control-plane` subtree | `c78ffc27c88b0f50871795a281669b7b2ef9bd27` |
| fixed candidate `contracts` subtree              | `f2d7e4d5221e3ecedf0117fead15945e067b4e70` |

The five candidate documentation identities are:

| File                                                              | SHA-256                                                            |
| ----------------------------------------------------------------- | ------------------------------------------------------------------ |
| `docs/plan/README.md`                                             | `6a5bdfca6e2d1b54ab58d2118d3ec30abfc599e7ed7bf40cbbf3f3306a984b1f` |
| `docs/plan/cloud-agents-platform/06-status-tracker.md`            | `d2eaac8c368946cbfab833eed0f6feac4bb45f03d4955aba4ca49be4ae20df8d` |
| `docs/plan/p1/README.md`                                          | `42385bf838abc46d084665a364bd4cb2cdbdcb8896cf621dc44592985b07a0e8` |
| `docs/plan/adr/0023-p1-runner-ledger-recovery-writer-contract.md` | `7ad47fa49ac771cc7f05f7e8610b173614e4935fce13e2563770c05629c207b1` |
| `docs/plan/p1/runner-ledger-recovery-contract-audit-20260822.md`  | `bd49c70e5bc1dceb9213cca62a1c6bfb173321d8a4046e2b8871d763aa57c7ff` |

The candidate worktree was clean, its local and remote refs were equal, and upstream was `0/0`. The remote candidate
ref resolved to the fixed commit. Its diff from the fixed source/base contains exactly the five documentation files
above. `contracts/`, `services/`, `scripts/`, package inputs, and lock inputs are byte-identical. This review did not
edit the candidate.

## Exact unsupported matrix

Independent extraction from
`contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json` produced the following ordered set:

| #   | Disposition                 | Recovery state              | Recovery action            | Current consumer action    |
| --- | --------------------------- | --------------------------- | -------------------------- | -------------------------- |
| 1   | `empty_brand_new`           | `brand_new_inherited`       | `begin_next_attempt`       | `entry_not_implemented`    |
| 2   | `partial_retry_or_recovery` | `brand_new_inherited`       | `begin_first_attempt`      | `recovery_not_implemented` |
| 3   | `partial_retry_or_recovery` | `brand_new_inherited`       | `begin_next_attempt`       | `recovery_not_implemented` |
| 4   | `partial_retry_or_recovery` | `dangling_statement_intent` | `append_aborted_retryable` | `recovery_not_implemented` |
| 5   | `partial_retry_or_recovery` | `dangling_statement_intent` | `append_aborted_terminal`  | `recovery_not_implemented` |
| 6   | `partial_retry_or_recovery` | `dangling_intermediate`     | `append_aborted_retryable` | `recovery_not_implemented` |
| 7   | `partial_retry_or_recovery` | `dangling_intermediate`     | `append_aborted_terminal`  | `recovery_not_implemented` |
| 8   | `partial_retry_or_recovery` | `dangling_commit_intent`    | `reconcile_commit`         | `recovery_not_implemented` |
| 9   | `partial_retry_or_recovery` | `ambiguous_unresolved`      | `reconcile_commit`         | `recovery_not_implemented` |
| 10  | `partial_retry_or_recovery` | `terminal`                  | `begin_next_attempt`       | `recovery_not_implemented` |
| 11  | `partial_retry_or_recovery` | `terminal`                  | `return_failure`           | `recovery_not_implemented` |
| 12  | `partial_retry_or_recovery` | `divergent`                 | `return_failure`           | `recovery_not_implemented` |

The complete generated registry has one `return_success_noop`, five `entry_not_implemented`, and eleven
`recovery_not_implemented` rows. ADR-0022 separately admits four first-attempt entry rows through its immutable
execution profile. Removing those four leaves exactly row 1 above plus all eleven recovery rows. The audit therefore
has no missing, duplicated, reordered, or misclassified unsupported pair.

## Authority review

All seven fixed-source hashes recorded by the audit match independently: generated consumer registry
`fa708280...e96852`, generated Go profile `afc77e72...e928`, recovery classifier `301cb883...0375`, evidence contracts
`03c8c201...74bffa`, consumer/caller `cbd2027a...6ac3`, first-attempt success kernel `9a3b7388...fa567`, and commit
observation classifier `4b0dde6a...b0b6f2`.

Those sources support the audit's reusable-versus-missing conclusion. Recovery snapshots, replay summaries,
terminal/retry/resolution DTO validation, append/checkpoint/rotation, lineage planning, and supersession/handoff
primitives exist, but they are typed facts or sealed mechanisms. No current production runner binder or registry-backed
permit joins them into authority for any of the twelve pairs. The historical brand-new writer retains its entry-zero,
attempt-one, early-cursor, empty-prefix, and single-statement assumptions. `runLegacyCharacterization` is called only
through the test wrapper, and its `reconcileAmbiguous` helper has no public production `Runner.Run` call edge.
Production search found zero occurrences of the seven proposed profile identities and no runner call to
`ReserveAndActivateSuccessor`.

ADR-0023 prevents a union writer by assigning distinct generated identities to abort-terminal append,
dangling-commit observation terminal, unresolved ambiguous resolution, retry lineage handoff, inherited/retry
execution, and typed return failure. It explicitly keeps dangling-commit terminal append separate from adjacent
ambiguous resolution, and lineage handoff separate from the later fresh-session execution admission. It requires an
owned rollback, irreversibly terminated old handle, or exact commit-rejection receipt; a new or idle-looking
connection cannot self-prove the old lifecycle ended. It also requires new retry identities instead of weakening any
ADR-0022 v1 attempt-one check. An ordinary result cannot be converted into a permit.

The proposal is explicitly `Proposed; explicit owner approval required`. It grants no implementation authority and
keeps every audited path `MIGRATION_PROJECTION_NOT_IMPLEMENTED`. It adds no generated source/profile, claim, permit,
writer, `Runner.Run` branch, database handle, external surface, or production side effect. The root README and P1
README describe it as unapproved; tracker decision D-047 remains `PROPOSED` and unchecked. No document closes a Gate
or reports implementation/review/runtime evidence that does not exist.

## Fresh documentation checks

The independent candidate worktree produced:

- exact ordered registry extraction: PASS, twelve rows consisting of one excluded entry retry and eleven recovery
  rows;
- seven fixed-source SHA-256 comparisons: PASS;
- docs-only and control-plane-subtree drift checks: PASS;
- proposed-identity and forbidden production-call-edge source scans: PASS;
- target `oxfmt 0.62.0 --check` over all five changed files: PASS;
- local Markdown link existence check over all five changed files: PASS;
- candidate-range `git diff --check`: PASS; and
- Gitleaks over `b726642..15b9b23`: PASS, one commit and `28.92 KB`, no leaks.

No Go, migration, full-package, race, or live PostgreSQL test was run or required for this docs-only audit. No runtime
PASS is inferred from unchanged source or from the static documentation checks.

## Non-claims

This review does not accept ADR-0023, authorize generation or implementation, change current behavior, modify the
fixed candidate, merge or open a pull request, connect to a live or production database, write production data,
deploy, publish, release, or close a Gate. It does not authorize HTTP/P2/provider behavior or any production writer.

The verdict approves only fixed candidate `15b9b232cc2005b5011dfa3c24e361b7b020d9e4` as an accurate docs-only source
audit and unapproved recovery-contract proposal. Owner approval plus separately fixed generated, read-only admission,
writer, handoff, execution, result, and independent-review slices remain mandatory before any current
`NOT_IMPLEMENTED` path may change.
