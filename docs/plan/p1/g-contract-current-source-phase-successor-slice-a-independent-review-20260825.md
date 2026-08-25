# G-CONTRACT current-source phase successor Slice A independent review

Date: 2026-08-25

## Verdict

`REQUEST_CHANGES - P0=0 / P1=1 / P2=0`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        1 |
| P2       |        0 |

This is an independent, read-only review of the fixed ADR-0030 / D-053 Slice
A candidate. The candidate was not modified and no generated exact17 output,
database, HTTP/P2/provider effect, deployment, publication, signing, or Gate
transition was performed. `notGateClosure=true` and all Gates remain OPEN.
The requested Slice B progression is blocked until the P1 below is repaired
and a new fixed candidate is independently reviewed.

## Fixed identity and scope

- fixed candidate: `cd45aad4523f19a1344487510d5d98e813959a59`
- candidate tree: `b0276ddaeb8e4c38ac6e0ce8fc7dd93287f24dc2`
- unique parent: `cabce4981b22202446ef47b037ad41cb49e4e304`
- candidate subject: `feat(platform): add G-CONTRACT phase successor contracts`
- candidate-to-parent change set: exactly 19 added paths, 10,518 insertions;
  no deletions or modifications
- review path was absent from the candidate

The review covered ADR-0030, the Slice A implementation record, the complete
19-path candidate diff, all five new phase/successor TypeScript authorities,
their tests, and all eight new JSON source/schema files. Static checks were
limited to the exact path/diff inventory, `git diff --check`, JSON parsing, and
source inspection; no Bun or Go test was run, no dependency tree was created,
and `.idea`/token material was not touched.

## Positive evidence

- `platform-successor-dag-v3.ts` fixes an ordered exact17 exclusion list and an
  unchanged ordered 49-output core; wildcard, alias, symlink, duplicate, and
  topology drift are fail-closed.
- `platform-successor-predecessor-v3.ts` fixes the 39-member predecessor fence,
  manifest traversal, historical lock identity, Git chain, and stable reads.
- `platform-contract-lock-v3.ts` is append-only in this slice, has no filesystem
  writer, and permits only the declared `ASSEMBLED -> PHASE_BOUND` successor.
- The state checker (`platform-g-contract-phase-state.ts`) correctly enforces
  single-parent review children, exact one-path `100644` additions, path
  absence, domain-separated diffs, live-byte identity, and the unique structured
  verdict when its capture/inspection path is used.
- Schemas are strict (`additionalProperties:false`) and preserve Gate-open,
  non-closure, and no-external-side-effect boundaries.

## P1 finding

### R5 model construction does not independently verify the supply-review child

`scripts/lib/platform-g-contract-phase-record.ts:845-925`
(`assertFreshSupplyV3Review`) checks caller-supplied `ReviewGitBinding` fields,
the declared parent/tree/diff identities, the bound review blob, and the
caller-declared `verdict`/`findings`. It does **not** call the strict
`assertReviewOnlyCommit` verifier (or an equivalent local verifier), so this
entry point does not prove all of the requirements that ADR-0030 places on the
R5 writer:

- the review commit changed exactly one path, with status `A` and mode `100644`;
- the review path was absent from the candidate parent; and
- the actual review blob contains exactly one `## Verdict` whose first content
  line is the required structured approval.

Those checks exist only in the separate state/capture path at
`scripts/lib/platform-g-contract-phase-state.ts:307-339` and are exercised by
the tuple lineage checks. `buildGContractPhaseRecordModel` invokes
`assertFreshSupplyV3Review` directly at `platform-g-contract-phase-record.ts:395`
and accepts a typed `ReviewGitBinding`; therefore a caller can provide a
single-parent review with an extra/modified path or a `REQUEST_CHANGES` review
blob while setting the typed `verdict` and zero `findings` fields to the
required values. The model builder can then proceed before any later tuple
check. This contradicts the ADR's hard rule that every supply review child has
exactly one added predeclared file and its claim that R5 construction itself
requires a unique structured zero-finding approval.

The existing tests prove the strict checks for
`captureGContractPhaseReviewBinding`, but contain no direct mutation cases for
the `buildGContractPhaseRecordModel` boundary with a forged `ReviewGitBinding`.
Repair the R5 builder by reusing a non-circular strict review-lineage/verdict
verifier (or moving the shared verifier to a lower-level module), and add
builder-level cases for an extra path, an existing-path modification, and a
reject/ambiguous verdict. Preserve the no-writer and all Gate-open boundaries.

## Gate and progression boundary

No Gate is closed or advanced by this review. No exact17 late-bound output is
created. Slice B and any generated R5 candidate remain unauthorized until the
repair is fixed as a new candidate with a fresh independent review.
