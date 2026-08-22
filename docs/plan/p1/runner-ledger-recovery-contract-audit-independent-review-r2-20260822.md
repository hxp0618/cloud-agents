# Runner ledger recovery contract audit independent review r2 — 2026-08-22

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `deb3dc61fe3fba11c4b09af1b714dbae592554ed`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-contract-audit-20260822`
- Review branch: `codex/cloud-agents-p1-runner-recovery-contract-audit-independent-review-r2-20260822`
- Supersedes: invalidated fixed-candidate review `395a5fd508bd74da0a03fc1939e04a776d006400`
- Gate effect: none; this review does not accept ADR-0023 or close or advance any Gate

This is a new review of the superseding fixed candidate. It does not delete, rewrite, or rehabilitate the invalidated
review of candidate `15b9b232cc2005b5011dfa3c24e361b7b020d9e4`; that candidate remains `BLOCK, P0=0/P1=1/P2=0`.

## Fixed identities

| Identity                                         | Value                                      |
| ------------------------------------------------ | ------------------------------------------ |
| superseding fixed candidate                      | `deb3dc61fe3fba11c4b09af1b714dbae592554ed` |
| fixed candidate tree                             | `02ff362e2faad03c7a3f155bd6bbe5ca41f4650c` |
| fixed candidate parent / blocked candidate       | `15b9b232cc2005b5011dfa3c24e361b7b020d9e4` |
| fixed source/base commit                         | `b72664291b0a6324e1b2c463d63a156a7b92773f` |
| fixed source/base tree                           | `e7afe8d7a740c3c6c83443e8fc985aead43465ec` |
| fixed candidate `services/control-plane` subtree | `c78ffc27c88b0f50871795a281669b7b2ef9bd27` |
| fixed candidate `contracts` subtree              | `f2d7e4d5221e3ecedf0117fead15945e067b4e70` |

The reviewed documentation identities are:

| File                                                              | SHA-256                                                            |
| ----------------------------------------------------------------- | ------------------------------------------------------------------ |
| `docs/plan/README.md`                                             | `6a5bdfca6e2d1b54ab58d2118d3ec30abfc599e7ed7bf40cbbf3f3306a984b1f` |
| `docs/plan/cloud-agents-platform/06-status-tracker.md`            | `dfec606f13fabbb4d6ae78b1bcdbbee4510a09d13665d910c65dd3f80f0ad774` |
| `docs/plan/p1/README.md`                                          | `42385bf838abc46d084665a364bd4cb2cdbdcb8896cf621dc44592985b07a0e8` |
| `docs/plan/adr/0023-p1-runner-ledger-recovery-writer-contract.md` | `b1c0735eb9f00bcfaeb7a14bb6c7bfd4fec1bbfe6dba1c6faa5854caa548fe13` |
| `docs/plan/p1/runner-ledger-recovery-contract-audit-20260822.md`  | `ee3978bc4084332301a4a42db41920bd6813efbfda648f89f8f3d32024302848` |

The candidate was clean, its local and remote refs were equal, and upstream was `0/0`. Its fixed-source diff contains
only the five documentation files above; `contracts/`, `services/`, `scripts/`, packages, and locks are byte-identical.
This review did not edit the candidate.

## Superseded P1 closure

The previous candidate named only `runner-ledger-recovery-execution/v1` while claiming separate execution-admission
and success-writer authority. The superseding candidate closes that P1 in every required contract location:

- the identity list separately names inherited/retry execution admission and inherited/retry success writer;
- `runner-ledger-recovery-execution-admission/v1` and `runner-ledger-recovery-success-writer/v1` are distinct
  versioned profile identities;
- their profile IDs, registries, digests, and registry-backed one-shot records must be distinct;
- the pair mapping binds the execution-admission identity to the exact three inherited/retry consumer rows, while the
  success writer has no direct consumer row and accepts only the exact consumed admission permit;
- Slice A generates both identities independently;
- Slice B implements only close-only recovery admission, with the execution-admission permit restricted to
  `close_without_mutation` and every writer still `NOT_IMPLEMENTED`;
- Slice F alone connects the separately versioned writer, which consumes one exact admission permit and opens one
  fresh session for one attempt; and
- the conformance matrix requires distinct IDs/digests/registries/records, close-only admission, one-shot writer,
  cross-profile rejection, and ordinary-outcome conversion rejection.

Neither a direct consumer fact, literal/copy/foreign registry record, the other profile's permit, nor an ordinary
kernel outcome may enter the recovery success writer. The repaired split therefore prevents the admission profile
from becoming a union mutation identity and preserves every ADR-0022 v1 identity unchanged.

## Exact unsupported matrix and source authority

Independent extraction of the immutable consumer-v1 registry produced the same ordered twelve unsupported rows:

1. `empty_brand_new / brand_new_inherited / begin_next_attempt / entry_not_implemented`;
2. `partial_retry_or_recovery / brand_new_inherited / begin_first_attempt / recovery_not_implemented`;
3. `partial_retry_or_recovery / brand_new_inherited / begin_next_attempt / recovery_not_implemented`;
4. dangling statement intent with retryable abort;
5. dangling statement intent with terminal abort;
6. dangling intermediate with retryable abort;
7. dangling intermediate with terminal abort;
8. dangling commit intent with commit reconciliation;
9. ambiguous unresolved with commit reconciliation;
10. terminal with begin-next-attempt;
11. terminal with return-failure; and
12. divergent with return-failure.

The complete registry remains one `return_success_noop`, five `entry_not_implemented`, and eleven
`recovery_not_implemented` rows. Removing ADR-0022's four separately admitted first-attempt rows leaves exactly the
one excluded entry retry plus eleven recovery rows above.

All seven audit source hashes remain exact: consumer registry `fa708280...e96852`, generated profile
`afc77e72...e928`, recovery classifier `301cb883...0375`, evidence contracts `03c8c201...74bffa`, consumer/caller
`cbd2027a...6ac3`, first-attempt kernel `9a3b7388...fa567`, and commit classifier `4b0dde6a...b0b6f2`. The source still
contains recovery DTOs, replay summaries, append/checkpoint/rotation, lineage planning, and handoff mechanisms, but no
production recovery binder/permit/kernel that combines them for these rows. The legacy characterization/reconcile
helper has no public `Runner.Run` edge, no production runner calls the handoff primitive, and production
contracts/services/scripts contain zero proposed profile identities.

ADR-0023 remains `Proposed; explicit owner approval required`. Every audited path remains
`MIGRATION_PROJECTION_NOT_IMPLEMENTED`; no generated profile, claim, permit, writer, `Runner.Run` branch, database
handle, HTTP/P2/provider edge, deployment, publication, release, or Gate authority is added. Root/P1 README wording
remains unapproved, and tracker D-047 remains `PROPOSED` and unchecked.

## Fresh documentation checks

- fixed identity, remote ref, clean, and upstream `0/0`: PASS;
- exact twelve-row ordered registry extraction: PASS;
- seven fixed-source SHA-256 comparisons: PASS;
- docs-only/control-plane-subtree drift and proposed production-identity scans: PASS;
- target `oxfmt 0.62.0 --check` over all five changed documents: PASS;
- local Markdown links and candidate-range `git diff --check`: PASS; and
- Gitleaks over `b726642..deb3dc6`: PASS, two commits and `36.32 KB`, no leaks.

No Go, migration, full-package, race, or live PostgreSQL test was run or required. No runtime PASS is inferred from
unchanged source or documentation checks.

## Non-claims

This review does not accept ADR-0023, authorize generation or implementation, modify the fixed candidate, merge or
open a pull request, connect to a live or production database, write production data, deploy, publish, release, or
close a Gate. It does not authorize HTTP/P2/provider behavior or any production writer.

The verdict approves only superseding fixed candidate `deb3dc61fe3fba11c4b09af1b714dbae592554ed` as an accurate
docs-only audit and still-unapproved recovery contract proposal. Owner approval and separately fixed generated,
read-only admission, writer, handoff, execution, result, and independent-review slices remain mandatory before any
current `NOT_IMPLEMENTED` path may change.
