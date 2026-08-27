# G-CONTRACT R5 current-source independent review

Date: 2026-08-28 Asia/Shanghai
Authority: `D-053 / ADR-0030 Slice H`
Review type: fixed-object, independent, read-only

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This review covers only the R5 current-source phase record at the fixed
candidate below. It does not close `G-CONTRACT` or any other Gate, and it
authorizes no canonical or production Runner, production database or
migration write, HTTP/P2/provider effect, deployment, publication, release,
or external signing. `notGateClosure=true`, `gateStatus=ALL_GATES_OPEN`, and
`closureDecision=NONE` remain binding.

## Candidate and direct-parent lineage

The reviewed candidate is commit
`f1d66a4d57f1241ca3ac364a77524c2520476c6c`, tree
`b66631c3e27c7b9713c3eb67539fbaced598980d`, with direct parent
`58e8f98a8b57760721306b3636a06dc3f10283b2`. The parent-to-candidate
`git diff --no-ext-diff` SHA-256 is
`sha256:59665a3d265feb6154b191bed628d0addea4e2e0e68a567bcdbe9607c21c9b6d`.
The diff adds exactly one predeclared path,
`docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md`,
as a regular `100644` file (Git blob
`41ad944fd3b06840010245f3f76f00fc1986b52d`, SHA-256
`sha256:7e17fb12a8e78870a1cbcf1b17809cb019cc5fe18cb68a0202311ee614875492`).
There is no rename, symlink, additional path, or self-review byte in this
candidate; `git diff --check` is clean.

## R5 record and fixed authorities

The R5 Markdown is the typed renderer output, not a hand-edited projection.
The renderer check reproduced the committed bytes exactly. Its fixed semantic
authority is `cloud-agents/platform/gate-phase-record/g-contract-p1/v1`; the
record binds the current source, model, criteria authority, and review-binding
registry by exact digests. The criteria authority SHA-256 is
`sha256:4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`;
the current review-binding registry SHA-256 is
`sha256:e5e5a6abc573fcdcce9d0f1338dad033f5155fe76b555e1fca3a9efc19f14dde`.
The registry reports `REVIEW_BOUND_SATISFIED_CANDIDATE` with its predecessor
bindings complete (`missing=[]`), while the phase record deliberately derives
the five still-open/review-pending G-CONTRACT rows from the criteria authority:

1. `json-schema-authority-and-openapi-refs` — `OPEN_NOT_CLAIMED`;
2. `proto-authority-and-generated-connect-grpc-mapping` — `OPEN_NOT_CLAIMED`;
3. `shared-golden-negative-and-n-minus-one-fixtures` — `OPEN_NOT_CLAIMED`;
4. `exact-pinned-external-consumer` — `OPEN_NOT_CLAIMED`;
5. `digest-change-invalidation` — `REVIEW_PENDING`.

The R5 record explicitly lists those five rows as its derived missing set; no
closure is inferred from the predecessor registry's empty `missing` array.
Its invalidation rules require supersession on any prerequisite, historical,
criteria, current-candidate, projection, supply-v3 assembly/review, current
source, schema, or assembled-lock identity drift; only the declared
`ASSEMBLED` to `PHASE_BOUND` successor is exempt for the immutable assembled
snapshot. R5 and all reviews remain append-only historical evidence.

## Projection, replay, profile, and lock bindings

The record binds projection commit/tree
`a7a46853d94e8d01cf2022dd447797cedc241d19` /
`d91447364745af16314f348d550b358f995fad0b` and archive SHA-256
`sha256:395f64058dbdae27ba0897861c39264ca3da36deca342ec1f98f5173c67b777b`.
The replay summary and profile checks bind the exact archive member and input
tree algorithms, 1,775 archive members, 1,569 regular input files, 49 output
files, and `candidateOutputsEqual=true`. Native replay is claimed only for
`darwin-arm64` and `linux-amd64`; `linux-arm64` remains `NOT_CLAIMED`.

The assembled supply candidate is commit
`94cbb23127a6a6c1ca31398d731d99b54cac80f9`, tree
`052d5d1a994349c42766bf9dafc292088add1a90`, parent
`25b3a47a185db2151ca4ba6e1916811cba6e155e`; its independent supply review is
the direct child `58e8f98a8b57760721306b3636a06dc3f10283b2`, with verdict
`APPROVE - P0=0 / P1=0 / P2=0`. The assembled v3 lock is bound at that
candidate to Git blob `86ab61bc060d8a0ad7878fb43b29a54997e40c2b`, file
SHA-256 `sha256:61267f5123004c108c9bcd79a8004da35af45d7ab7beb83d7b898da57a4d81ba`,
and state `ASSEMBLED`; its lock digest remains the immutable v3 assembled
snapshot digest. Profile, evidence-manifest, replay, projection, and lock
files are regular `100644` bytes and their exact SHA-256 values match the R5
record's assembled bindings.

## Focused checks observed

- `bunx vitest run scripts/lib/platform-g-contract-phase-record.test.ts scripts/lib/platform-g-contract-phase-state.test.ts scripts/lib/platform-generator-supply-profile-v3.test.ts scripts/lib/platform-contract-lock-v3.test.ts --reporter=dot` — 4 files, 31 tests passed.
- `bun scripts/generate-platform-g-contract-phase-record.ts --check <ephemeral fixed binding input>` — `g-contract-phase-record: current` (typed renderer bytes exact; the input was not added to the candidate).
- `bun scripts/check-platform-g-contract-phase-state.ts --check` — `R5_REVIEW_CURRENT_BINDING_ABSENT` (the expected pre-binding state after this review file is present).
- `bun scripts/generate-platform-generator-supply-profile-v3.ts --check-source` and `--check-assembly` — `ASSEMBLED_PROFILE_CURRENT`.
- `bun scripts/generate-platform-contract-lock-v3.ts --check` — `ASSEMBLED`; `--check-assembled` — `ASSEMBLED current`.
- Exact candidate, parent, tree, mode, blob, and SHA-256 checks above; `git diff --check` passed.

No production database, HTTP/P2/provider, deployment, publication, release,
or Gate operation was performed. This review is complete with zero P0/P1/P2
findings and leaves the next authorized slice to the existing ADR-0030 order.
