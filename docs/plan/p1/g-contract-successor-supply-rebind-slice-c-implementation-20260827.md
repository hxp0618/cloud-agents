# G-CONTRACT successor/supply rebind Slice C projection — 2026-08-27

## Boundary

This record implements only ADR-0029 / D-052 Slice C on the clean review-child
lineage descended from the approved Slice B implementation and its fixed-object
review. The child contains bookkeeping only; it does not rewrite the Slice A
predecessors, the Slice B source/profile/schema/manifest bytes, the legacy
generation lock, or any v1/v2 evidence. The projection is built from the child
`HEAD`, not from the historical Slice B candidate or any diagnostic checkout.

This slice constructs the immutable pre-replay projection authority and runs
only deterministic projection preflight. It does not run native replay, write
or assemble a supply-v2 profile, derive a successor lock, create detached
binding output, or modify any external system. `G-CONTRACT`,
`G-SUPPLY-CHAIN`, and every aggregate Gate remain `IN PROGRESS`/OPEN.

## Clean review-child

The parent is the current `codex/cloud-agents-platform-p0` P0 commit at the
time this record is committed. The child records the already approved Slice B
review and updates only the plan/status indexes so the projection entry is
auditable. No Slice B review verdict is reissued and no predecessor bytes are
reopened. The child is expected to be a direct, clean descendant with no
unstaged or untracked paths when `build-projection` is invoked.

The reviewed implementation ancestor remains
`c547f04f15b86a6b33f73ea633837fd8db6cc00b`, with fixed-object review
`g-contract-successor-supply-rebind-r1-assembly-writer-repair-independent-review-20260825.md`
returning `APPROVE, P0=0/P1=0/P2=0`. The earlier Slice B fixed candidate
`a2f4ec986ce8ff5d6e707254ce475673eda9d3ff` and review remain historical
evidence; this child does not amend or replace either object.

## Projection authority and checks

The versioned wrapper
`scripts/replay-platform-generators-isolated.sh build-projection` is the sole
projection builder. It requires a canonical clean repository, writes a fresh
external output leaf, removes exactly the ordered 16 late-bound paths from a
temporary Git index, rejects non-regular modes, writes a deterministic Git
tree and tar archive, reconstructs the tree with the archive inspector, and
emits `projection.json` only after all checks pass. The archive and metadata
remain outside the repository and are not replay receipts.

The exact exclusion set is fixed by ADR-0029 / Slice A and is checked by the
wrapper and the v2 projection helpers:

1. `contracts/generation.lock.json`;
2. `tools/generator-supply/v2/evidence-manifest.json`;
3. `tools/generator-supply/v2/profile.json`;
4. `tools/generator-supply/v2/evidence/replay.json`;
5. `tools/generator-supply/v2/evidence/replay/darwin-a.json`;
6. `tools/generator-supply/v2/evidence/replay/darwin-b.json`;
7. `tools/generator-supply/v2/evidence/replay/darwin-isolation.json`;
8. `tools/generator-supply/v2/evidence/replay/linux-a.json`;
9. `tools/generator-supply/v2/evidence/replay/linux-b.json`;
10. `tools/generator-supply/v2/evidence/replay/linux-isolation.json`;
11. `tools/generator-supply/v2/evidence/replay/projection.json`;
12. `docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md`;
13. `docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md`;
14. `tools/contract-review-binding/v1/review-tuple.json`;
15. `tools/contract-review-binding/v1/registry.json`;
16. `docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md`.

The focused evidence for this slice is limited to deterministic `build-projection`
write and a second write/check comparison, exact 16-path exclusion count/order,
candidate-tree versus projection-tree reconstruction, archive member/regular-file
manifest reconstruction, SHA-256 capture, `git diff --check`, format/lint checks
for changed bookkeeping documents, and redacted secret scanning of the candidate
range.

No command in this slice invokes `run-darwin`, `run-linux`, replay assembly,
generation-lock writing, PostgreSQL, HTTP/P2/provider code, deployment,
publication, release, or a Gate transition.

## State and next boundary

On success the child is the fixed pre-replay projection candidate. The
projection remains an external immutable archive bound to the child tree;
`projection.json` is not copied into an excluded repository path. Native
Darwin arm64/Linux amd64 replay is the separate ordered Slice D and remains
pending. Slice E lock/evidence assembly and all later detached review binding
remain unauthorized. This record makes no Gate or production claim.
