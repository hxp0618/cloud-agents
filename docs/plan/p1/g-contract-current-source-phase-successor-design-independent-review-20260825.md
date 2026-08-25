# ADR-0030 current-source phase successor fixed-object review

Date: 2026-08-25 Asia/Shanghai

## Verdict

`REJECT - P0=0 / P1=3 / P2=0`

The fixed ADR-0030 candidate is not admissible for Slice A. Its exact17
topology, post-assembly R5 ordering, single rendered Markdown candidate, and
non-Gate boundary are sound, but three P1 design defects must be repaired in an
append-only superseding candidate and independently reviewed again.

This review is read-only with respect to the candidate, runtime, databases,
HTTP, providers, remote hosts, deployment, publication, signing, release, and
all Gates.

## Fixed candidate identity

- candidate commit:
  `78ac538725b6bb000d0963021119b852df784248`;
- candidate tree:
  `0d3a744390a63792de002d33c989977aa6c84c09`;
- candidate parent:
  `16275f6cbf390c343a9ac00f9193e75eaad0094e`;
- canonical binary diff SHA-256:
  `89f00ddb8d62a24ff778bd413a1babd221afb2acb68c82e5ff487e3dd1c2070c`;
- candidate changed paths: exactly the two additions below;
- review path in the candidate tree: absent.

| Candidate path | Mode | Git blob | SHA-256 | Size |
| --- | --- | --- | --- | ---: |
| `docs/plan/adr/0030-p1-g-contract-current-source-phase-successor.md` | `100644` | `c40a211eca917afab6943cb8103311dc368581f7` | `eb48743441d28b617ab1d43e9e0cd1ac82e383c27ad1cfd87afd69efe1da1aec` | 22762 |
| `docs/plan/p1/g-contract-post-h-current-source-successor-entry-audit-20260825.md` | `100644` | `02f659bbd8be579cd39126bb52798ec044341faf` | `ca7bc4c513dd6cd95c9ffa18231f34a9044f721647e67378f6dc8dcb0bc25f9c` | 10700 |

The reviewed branch and worktree were clean. Remote refs reproduced
`codex/p1-contract-phase-record-successor=78ac538...` and
`codex/cloud-agents-platform-p0=16275f6...`.

## P1-1: the authorized lock transition invalidates R5 under its own rule

ADR-0030 requires R5 to bind the current generation-lock input and says any
lock change invalidates the old effective result. R5 is generated in Slice G
from the Slice E `ASSEMBLED` lock, while Slice I must advance the same live path
to `PHASE_BOUND`. Taken literally, the mandatory Slice I transition makes R5
stale before the Slice J review and prevents the declared terminal state.

Required repair:

- R5 must bind the immutable Slice E `ASSEMBLED` lock commit, tree, blob,
  SHA-256, size, and state, not an indefinitely current live path;
- the Slice I `PHASE_BOUND` lock must bind that exact historical snapshot plus
  the R5 candidate/review and tuple/registry bytes;
- the read-only effective-state verifier must recognize only that exact
  `ASSEMBLED -> PHASE_BOUND` transition as the authorized successor relation;
- the historical assembled lock remains recoverable from its fixed commit;
- every other lock change, predecessor mismatch, skipped state, or byte drift
  remains invalidating and fail closed.

Exact17 does not need another lock path. The repair is a versioned transition
rule over two immutable Git snapshots of the same live path.

## P1-2: terminal state is claimed before its required review exists

The candidate calls the Slice I binding registry the machine-readable
effective-state output, while the terminal state also requires the Slice J
review as an input. It then asks the Slice J reviewer to reproduce the terminal
effective state in the commit that adds that review. That is temporally
impossible: before the review child exists, the terminal input is absent.

Required repair:

- the registry may report only a pre-terminal binding state such as
  `PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT`;
- the Slice J reviewer reviews the fixed Slice I candidate and must not claim a
  result that depends on its own future review commit;
- after the review-only child is fixed, a named versioned read-only verifier
  validates its exact commit/parent/path/SHA/verdict and emits only to stdout:
  `REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE`;
- no tracked receipt, registry, lock, R5, tracker, or review is written after
  the terminal review;
- the verifier must distinguish `FINAL_REVIEW_PRESENT_UNVERIFIED` from a fully
  checked terminal result.

This read-only terminal computation is finite and does not create review
recursion.

## P1-3: review children are not constrained to one added path

The candidate requires supply, R5, and terminal reviews to be direct children
that add only their predeclared review files, but its tuple/verifier contract
does not require an exact review diff or exact changed-path set. The current v1
binding verifier checks parentage and candidate-path absence, but does not by
itself prove that a review child contains no unrelated change.

Required repair for all three reviews:

- the candidate must have exactly one parent and the review must be its direct
  single-parent child;
- the review path must be absent in the candidate tree;
- the review diff must contain exactly one added path: the predeclared review
  path, mode `100644`, regular non-symlink blob;
- rename, copy, mode drift, deletion, modification of an existing file,
  symlink, extra changed path, and merge parent must fail closed;
- the tuple persists or deterministically recomputes a domain-separated review
  diff SHA-256 and exact changed-path assertion;
- the R5 candidate commit similarly changes only the predeclared R5 record;
- the Slice I candidate changes only lock, tuple, and registry in their exact
  declared modes and operations.

## Accepted portions of the design

The following parts need no conceptual replacement:

- exact17 contains the live lock, all supply-v3 assembly/replay bytes, supply
  review, R5 record/review, tuple, registry, and terminal review exactly once;
- R5 source/schema/builder/checker are pre-replay inputs while the rendered R5
  record is post-supply late-bound;
- a strict typed/schema-validated object plus whole-byte deterministic Markdown
  reconstruction avoids a second persisted candidate authority;
- supply review -> R5 -> R5 review -> tuple/registry -> terminal review is
  acyclic once the terminal-state timing is repaired;
- fresh Darwin arm64 and Linux amd64 A/B replay is required; v2 receipts cannot
  be reused as current evidence;
- proposal bytes are outside v2 exact16, so supply-v2 becomes historical for
  the new source while remaining an immutable predecessor;
- `G-CONTRACT` and all other Gates remain open; no production/external effect is
  authorized.

## Reproduction boundary

The review used fixed Git objects, plan/Gate documents, and the current v2 DAG,
lock, and binding implementation as read-only evidence. It ran no broad Bun or
Go tests, no writer, no network side effect beyond read-only remote-ref
verification, and no remote-host operation.

## Progression decision

`STOP BEFORE SLICE A`.

Preserve `78ac538...` and this rejection. Create an append-only superseding
design candidate that closes all three P1 findings, then run a new independent
fixed-object review. A later approval remains non-Gate and does not authorize
production database writes, HTTP/P2/provider effects, deployment, publication,
release, main merge, or Gate closure.
