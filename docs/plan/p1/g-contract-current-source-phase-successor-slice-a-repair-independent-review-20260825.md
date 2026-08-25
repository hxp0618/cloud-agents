# G-CONTRACT current-source phase successor Slice A repair independent review

Date: 2026-08-25 Asia/Shanghai

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This is a fresh, fixed-object, read-only review of the ADR-0030 / D-053 Slice
A repair candidate `b806ee6c19ee48888df5cbe27816c2d19cdb9465`. It closes the
single P1 finding in the historical Slice A review for `cd45aad4523f19a1344487510d5d98e813959a59`:
the R5 model builder now independently verifies the supply-review child
topology and verdict bytes instead of trusting only caller-supplied typed
fields. The candidate was not modified by this review.

This approval is bounded to the repaired Slice A contract. It does not close
`G-CONTRACT`, `G-SUPPLY-CHAIN`, any phase Gate, or any aggregate Gate, and does
not authorize production database writes, HTTP/P2/provider effects,
deployment, publication, signing, release, main merge, SSH, or external
durability operations.

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

## Fixed candidate identity

- candidate commit: `b806ee6c19ee48888df5cbe27816c2d19cdb9465`;
- candidate tree: `0aa0741331d86c994541c957c86069f25d45d45f`;
- unique parent: `efad636b8ff674416eba7597f05232601e47798d`;
- parent tree: `74d2371fa4322b3bd19693783f472d3abfc8b502`;
- candidate subject: `fix(platform): harden phase review model boundary`;
- candidate-to-parent diff: exactly two modified `100644` paths:
  `scripts/lib/platform-g-contract-phase-record.ts` and
  `scripts/lib/platform-g-contract-phase-record.test.ts`;
- this fresh review path is not part of the reviewed candidate object.

The preserved historical review
`g-contract-current-source-phase-successor-slice-a-independent-review-20260825.md`
remains a `REQUEST_CHANGES - P0=0 / P1=1 / P2=0` review of the older
`cd45aad...` candidate. It is not rewritten or reinterpreted by this record.

## Closed P1: independent supply-review child and verdict verification

At `scripts/lib/platform-g-contract-phase-record.ts:845-938`,
`assertFreshSupplyV3Review` invokes `assertSupplyReviewChildShape` before it
accepts the typed `ReviewGitBinding`. The new verifier at lines `940-1021`
proves all of the following from fixed Git objects:

- the predeclared review path is absent from the candidate tree;
- the review child diff is exactly one `A` operation for that path, using
  NUL-framed `git diff --name-status -z --no-renames`;
- the review path is an exact regular, non-symlink `100644` blob;
- the blob contains exactly one level-two `## Verdict` section whose first
  non-empty content is an approval with `P0=0 / P1=0 / P2=0`;
- the domain-separated review-child diff digest equals the bound digest.

The existing checks immediately after that verifier continue to bind the
candidate and review direct-parent/tree identities, candidate diff, current
projection/manifest/profile bytes, review blob/SHA/size/mode/live bytes,
reviewer separation, typed verdict, and zero findings. Therefore a forged
typed binding cannot bypass the review-only child contract at the R5 model
construction boundary.

The candidate test file adds builder-boundary mutation cases at
`platform-g-contract-phase-record.test.ts:174-185` and helper setup at
`:327-368` for an extra path, an existing-path modification, a rejecting
verdict, and an ambiguous verdict. Each is expected to fail closed with
`G_CONTRACT_PHASE_IDENTITY_MISMATCH`.

## Read-only evidence

The following commands were run against the fixed Git objects only:

```text
git show --no-patch --format='%H %P %T' b806ee6
git diff --raw b806ee6^ b806ee6
git diff --check b806ee6^ b806ee6
git show b806ee6:scripts/lib/platform-g-contract-phase-record.ts | nl -ba | sed -n '845,1021p'
git show b806ee6:scripts/lib/platform-g-contract-phase-record.test.ts | nl -ba | sed -n '160,190p;325,370p'
```

Observed results were the fixed commit/tree/parent identities above, exactly
the two intended modified paths with `100644 -> 100644` modes, and a clean
`git diff --check`. No test suite, writer, CLI generation, replay, database,
HTTP/P2/provider, SSH, deployment, publication, or other external-side-effect
command was run for this review.

## Progression boundary

The repaired Slice A object is independently approved for bounded continuation
under ADR-0030. Any later Slice B action remains subject to the ordered
versioned authorities and the no-external-effect boundary. This record does
not modify the candidate, the tracker, the README, any Gate status, or any
generation-lock/output artifact.
