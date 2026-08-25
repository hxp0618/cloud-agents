# ADR-0030 current-source phase successor repair independent review

Date: 2026-08-25 Asia/Shanghai

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

The superseding ADR-0030 fixed object closes all three P1 findings from the
preserved `78ac538... -> 11513d8...` rejection lineage. Exact17, the
post-assembly R5 record, the two-lineage detached binding, the terminal
read-only state check, and ordered Slices A-J are internally consistent and
admissible for bounded non-Gate implementation.

This approval does not close `G-CONTRACT`, `G-SUPPLY-CHAIN`, or any other Gate.
It authorizes no production database write, HTTP/P2/provider effect,
deployment, publication, signing, release, main merge, Beta, or GA.

## Fixed candidate identity

- candidate commit:
  `edb2be53605419f734a344b88966d6af5131787e`;
- candidate tree:
  `e1586f903544fa22446510912c2f114f1554ac21`;
- candidate parent and preserved rejection review:
  `11513d8e6ae87d2c3352e73b0a471d2834a5af19`;
- canonical binary candidate diff SHA-256:
  `cc4bda7089ef28e0378a4d2317246e95790f7af6af1432cfee6f6e555258795c`;
- candidate changed paths: exactly the two modified `100644` files below;
- this review path in the candidate tree: absent.

| Candidate path | Git blob | SHA-256 | Size |
| --- | --- | --- | ---: |
| `docs/plan/adr/0030-p1-g-contract-current-source-phase-successor.md` | `b4652a86c54734bf2e91725d78c0e58068f9e671` | `5733c3e951a2db6552144b0136d2451ef6961b3739a3731970e07ebf63149d94` | 26720 |
| `docs/plan/p1/g-contract-post-h-current-source-successor-entry-audit-20260825.md` | `dae778e16ebefaab824245299ccc05c130c38066` | `7d87113763a7d1546f625b5d555ac030ae0082bfd5fe77a6c036c906c17365f0` | 12367 |

The parent preserves the rejected candidate review as one added path:

- parent tree:
  `8118288894cccca705baec54b9ad851252a7fb2f`;
- rejected candidate:
  `78ac538725b6bb000d0963021119b852df784248`;
- rejection review path:
  `docs/plan/p1/g-contract-current-source-phase-successor-design-independent-review-20260825.md`;
- rejection review SHA-256:
  `05b4b0032accbe121eb155ffe9eea9cb1b9ea2ade0c1ba631506e8b94c340f14`.

The candidate worktree was clean. Remote refs reproduced the candidate feature
branch at `edb2be5...`, the rejected-review branch at `11513d8...`, and P0 at
`16275f6...`.

## Closed P1-1: authorized two-snapshot lock transition

The repaired design no longer compares R5's assembled lock snapshot with the
later live lock as if they must remain byte-identical.

R5 binds the immutable Slice E `ASSEMBLED` lock commit, tree, blob, SHA-256,
size, format, and state. Slice I advances the same live path once to a
`PHASE_BOUND` document that binds that exact historical snapshot plus the R5
candidate/review and tuple/registry bytes. The read-only state verifier accepts
only this exact `ASSEMBLED -> PHASE_BOUND` relation.

The assembled bytes remain recoverable from their fixed Git commit. A skipped
state, alternate predecessor, unexpected byte, later lock mutation, or any
other transition remains invalidating and fail closed. No second lock path is
required.

Finding status: `CLOSED`.

## Closed P1-2: terminal review timing and finite state authority

The binding registry now reports only
`PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT`. It cannot claim that the terminal
review is already present or verified.

Slice J reviews only the fixed Slice I candidate and reproduces the
pre-terminal state. It does not claim an effective result that depends on its
own future review commit. After the direct review-only child exists, the
versioned read-only CLI
`scripts/check-platform-g-contract-phase-state.ts` verifies its exact
commit/parent/tree/path/blob/SHA-256/diff/verdict and all current invalidation
inputs before emitting `REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE` to stdout.

The checker has no write mode and no tracked output. It does not regenerate the
tuple, registry, lock, R5, tracker, review, or another receipt. This is a finite
terminal computation and does not create review recursion.

Finding status: `CLOSED`.

## Closed P1-3: exact review-only and candidate diffs

The repaired contract requires all three future review commits to be
single-parent direct children. Each diff must contain exactly one operation:
add the predeclared review path as a regular non-symlink `100644` blob. The
review path must be absent in the candidate tree.

The tuple or read-only terminal checker verifies a domain-separated review
diff SHA-256, exact path, mode, operation, parentage, blob, structured verdict,
and reviewer separation. Merge parents, rename/copy, deletion, modification of
an existing path, symlink, mode drift, and extra paths fail closed.

The R5 candidate is also constrained to add only the predeclared R5 record.
Slice I is constrained to exactly three operations: add tuple, add registry,
and advance the fixed assembled lock to its sole authorized phase-bound
successor.

Finding status: `CLOSED`.

## Reviewed successor boundary

The review also reproduced these unchanged decisions:

- generator-supply v2 becomes historical for the new source but remains an
  immutable predecessor;
- supply-v3 source, schemas, replay/profile code, R5 typed renderer/checker,
  binding code, lock-v3 code, state checker, tests, tracker, and plan index are
  pre-replay inputs;
- the projection excludes exactly 17 ordered paths and no wildcard;
- the single persisted R5 candidate is a deterministic Markdown record; the
  typed/schema-validated source is its machine input authority;
- the binding registry is a machine-readable pre-terminal view, not a second
  phase record or Gate signature;
- R5 is produced only after the fixed supply-v3 assembly and independent
  supply review, so it can bind actual profile and review bytes without
  self-reference;
- fresh Darwin arm64 and Linux amd64 A/B replay is mandatory; Linux arm64
  remains `NOT_CLAIMED` and v2 receipts cannot be reused;
- tracker text remains a derived index and all Gate statuses remain open or
  in progress.

The exact dependency chain is acyclic:

```text
pre-replay authority
  -> projection
  -> native A/B replay
  -> supply-v3 assembly
  -> supply review-only child
  -> R5-only candidate
  -> R5 review-only child
  -> tuple + registry + phase-bound lock
  -> terminal review-only child
  -> read-only stdout state check
```

No tracked byte is written after the terminal review.

## Verification boundary

This review used only fixed Git objects, current plan/Gate documents, and the
existing v2 DAG/lock/binding code as read-only evidence. It reproduced the
candidate tree, parent, exact two-path diff, file blobs, SHA-256 values, sizes,
preserved rejection lineage, and absence of this review path.

It ran no broad Bun suite, no broad Go or migration tests, no writer, no
database, no remote host, and no production or external-effect command. Those
tests would not add criterion-specific evidence to a documentation-only design
candidate.

## Progression decision

`SLICE A ENTRY APPROVED WITHIN D-053 NON-GATE BOUNDARY`.

Under the continuing Platform goal authority, implementation may proceed in
the exact ordered Slices A-J without another per-slice approval. Any departure
from exact17, the immutable predecessor fence, the two-snapshot lock transition,
the review-only diff rules, or the terminal no-output rule requires a new
versioned decision and fixed-object review.

This verdict is not a Gate closure record and cannot authorize production
database writes, HTTP/P2/provider effects, deployment, publication, signing,
release, main merge, Beta, GA, or any phase/aggregate Gate transition.
