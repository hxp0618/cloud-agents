# G-CONTRACT closure/supply successor R1 assembly-writer repair — 2026-08-25

## Boundary and progression decision

This record is the R1 repair after the fixed Slice B pre-replay candidate and
the diagnostic Slice C/D run. It is limited to the missing production
consumer/summary/manifest/profile and append-only assembly-writer authority
required before a formal projection can be admitted. It does not replace the
Slice B fixed-object review or itself constitute the separate R1 fixed-object
review.

The Slice B predecessor is fixed candidate `a2f4ec986ce8ff5d6e707254ce475673eda9d3ff`,
with its bounded independent review already returning
`APPROVE, P0=0/P1=0/P2=0`. The v1 predecessor remains immutable. The tracked
`contracts/generation.lock.json` remains unchanged at SHA-256
`29cd59f1f69e35a6c0fd312524883b6a90be6fe09616dd21864ed9ce52c96101` and
237,214 bytes.

The diagnostic replay was technically successful and its Darwin/Linux
receipts are retained outside the repository, but progression review returned
`REQUEST_CHANGES, P0=0/P1=1/P2=0`: the current production source had no raw
receipt consumer, canonical summary builder, evidence-manifest/profile
builder, or append-only writer. Adding those authorities changes non-excluded
projection inputs, so the diagnostic projection and replay are stale and
non-admissible for Slice E. No diagnostic receipt is installed or copied into
the repository. Formal Slice C projection authority and formal Slice D replay
have not started.

No production database write, HTTP/P2/provider effect, deployment,
publication, release, main merge, history rewrite, or Gate transition is part
of this repair. `G-CONTRACT`, `G-SUPPLY-CHAIN`, and every aggregate Gate remain
`IN PROGRESS`/OPEN. Slice E remains unauthorized until a repaired candidate is
fixed, independently reviewed, reprojected, and replayed.

## R1 contract

The repaired production path is a single ordered, versioned assembly contract:

1. Consume exactly seven external raw receipts:
   `darwin-a.json`, `darwin-b.json`, `darwin-isolation.json`, `linux-a.json`,
   `linux-b.json`, `linux-isolation.json`, and `projection.json`, all under
   the caller-supplied external output roots.
2. Copy each raw byte sequence into an owned stable snapshot, validate its
   schema and topology, and derive exactly one canonical
   `tools/generator-supply/v2/evidence/replay.json` summary. The summary is
   generated from the seven raw receipts and may not be caller-owned mutable
   memory.
3. Assemble the ordered eight replay receipts (the derived summary followed
   by the seven raw receipts), then the two assembly outputs:
   `tools/generator-supply/v2/evidence-manifest.json` and
   `tools/generator-supply/v2/profile.json`.
4. Publish exactly ten files with append-only, resumable semantics. Existing
   exact bytes are an idempotent no-op; a divergent existing destination is a
   conflict. No overwrite, truncation, lock file, or replacement of the v1
   generation lock is allowed.

The writer's authority is fail-closed at all boundaries. It captures one
cumulative snapshot of the complete v1 predecessor before deriving any v2
value: all six outer immutable files, the 39-member evidence set, and the
semantic profile/review reads. It separately captures the v2 source/schemas,
raw inputs, prepared receipts, destination parents, and published outputs. It
copies caller buffers before validation/derivation, validates the exact
seven-key set, and requires projection and platform output roots to be
distinct external regular-file trees. External file identities and every
ancestor topology are stable across the read; no symlink ancestor or final
symlink is accepted.

Before and after each no-replace publication, the writer rechecks that same
complete v1 snapshot, every other captured input, the complete destination-
parent identity set, and every already-published output identity. Temporary
files use exclusive creation, no-follow reads, fsync, and identity-safe
cleanup. A failed append may leave only an exact published prefix; a subsequent
invocation validates that prefix and resumes without rewriting it. The final
fence covers the complete ten-file output set and all captured authority
identities.

## Preserved working-byte findings and repairs

Two findings from the R1 working-byte review are retained here as part of the
implementation lineage, not silently erased:

| Finding                                                                              | Risk                                                                                                                                                                  | Repair required before fixed candidate                                                                                                       |
| ------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| Caller-owned `Buffer` values were exposed as derived authority                       | A caller could mutate receipt bytes after the supposed stable snapshot, changing the summary or manifest inputs                                                       | Copy every raw buffer on ingress and derive/validate only from the owned snapshots; test mutation after ingress and before each derived read |
| The complete v1 predecessor fence was not cumulative across the ten-file transaction | An outer v1 schema, profile, review, or evidence member outside replay's derived subset could change after the first publication while the writer continued appending | Capture one complete predecessor snapshot and recheck the same identities before and after every publication and at the terminal fence       |

The repair also preserves the previously fixed R1 boundaries: exact key-set
validation, canonical JSON serialization, full replay graph validation,
source/schema/profile authority binding, external-root separation, no-follow
topology checks, collective parent/output ABA fences, no-replace publication,
same-byte no-op, divergent conflict, resumable prefix recovery, and
identity-safe temporary cleanup.

## Fixed-object candidate rejection

The implementation was fixed as candidate
`96d72c966bd86ed29abb301cb0ff5bb1fb8ce43e` (tree
`84e9c7125c26dc90f637d0a5afca910bdca42b61`; parent
`7504e2ee9fb4941bcbcee3cdb4a29ebd13f5de58`). Its separate fixed-object review
returned `REQUEST_CHANGES, P0=0/P1=1/P2=0`; see the
[candidate review](g-contract-successor-supply-rebind-r1-assembly-writer-repair-candidate-96d72c9-independent-review-20260825.md).

The reviewer found that the writer captures source and schema identities but
discards the schema bytes before `validateAgainstSchema`/`schemaValidator`
lexically rereads both schema paths. A schema directory A → B → A working-byte
ABA can therefore let Ajv validate with B while the final identity fence sees
A. The existing tests have no deterministic schema working-byte ABA case.
The required repair is to retain owned schema bytes, build one validator from
that captured pair for both source/output validation, and add a deterministic
hook/test for the A → B → A sequence. This is a third, fixed-object authority
finding; the two earlier working-byte findings above remain unchanged and are
not rewritten.

A second read-only crosscheck returned `APPROVE, P0=0/P1=0/P2=0` and treated
the schema sequence as proof hardening because canonical output bytes are not
changed by Ajv's B read and the final A identity is rechecked. The formal
fixed-object reviewer treated the uncaptured validation authority as a P1
proof gap. The conservative progression control verdict is therefore
`REQUEST_CHANGES`; the disagreement is retained rather than presented as
consensus.

## Candidate-ready working-byte repair

The repair addresses the fixed-object finding without changing the historical
`96d72c9` verdict. The writer now
copies both captured schema byte sequences into owned memory, constructs one
Ajv validator from that pair, and uses the same compiled validator for source
and output validation. Validation no longer rereads schema paths during the
assembly transaction.

The focused test suite now injects a deterministic two-stage schema A → B → A
working-byte replacement around both source and output validation phases and
proves that the captured validator remains authoritative. A second test
mutates the captured output schema into invalid JSON and into a canonical
reject-all schema; both fail closed before any late receipt or assembly output
is created.

Two independent read-only working-byte reviews returned
`APPROVE, P0=0/P1=0/P2=0`. One bound the two-file repair to binary-diff
full-index SHA-256
`8eadb91d40b1c41236801d7458b141b00449e1f49e9049f320f4b78e498aa64c`.
These verdicts approve only the working bytes; they do not by themselves
satisfy the fixed-object review prerequisite.

## Expected state machine

The repaired state transition is:

```text
DECLARED_PRE_REPLAY
  -> raw seven-receipt snapshots accepted
  -> canonical derived replay summary prepared
  -> ordered eight receipts prepared
  -> exact ten-file assembly published/resumed
  -> ASSEMBLED_PROFILE_CURRENT (repair candidate-ready; fixed-object review pending)
```

The assembly state is not a review verdict. It cannot create a detached review
tuple, review registry, effective `missing=[]`, final review, successor lock,
or Gate signature. The successor lock remains dormant until the later ordered
late-bound review steps have their own fixed inputs and independent reviews.

## Diagnostic evidence retained as stale/non-admissible

These are historical diagnostics only and are not inputs to the repaired
candidate or any formal projection:

| Item                           | Digest / size                                                                                                                                                                                                |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Diagnostic checkout / tree     | `7504e2ee9fb4941bcbcee3cdb4a29ebd13f5de58` / `82e7e17774cbffd08865a5f1f1b82143741c5070`                                                                                                                      |
| Diagnostic projection tree     | `6d651a23500a7bcdda8e32134106f83281d4390b`                                                                                                                                                                   |
| Diagnostic projection archive  | `b35380d4ddfd2dd190334949f15e1051ba36ed94740da1b97f3687d7c8ff10a2`, 46,673,920 bytes                                                                                                                         |
| Diagnostic projection metadata | `b1c4020bc32772e2acf17895fc1a89b14dba0e9d357d5977df6b7d07fbd3c9f9`, 1,903 bytes                                                                                                                              |
| Darwin A / B / isolation       | `227416f212e509bcec6bea9423bee0ba6f196b0d394cc81b9b75ff84fb1e64ae` / `b89563ba04e0a0604f5049cbfdd588110d52c36e9037c499adea82252d95af03` / `70ad1f348004a33b7a99f1e9fe1a2d7b2e7afa9f090f0a21d394e1abae9430f3` |
| Linux A / B / isolation        | `cd3b9d49e61b427650a7f98d4b2d97619e8280a39335a0cac94cbcbb0f5630db` / `81d0f1fba7a709370806affc82ec87528e3c1f49f609bc3f79a0722105b521ba` / `58f4c348a925ebf34dd3f819148d8c4856916d2e25237d3506cdac1fcfc1644d` |

The diagnostic technical review was `APPROVE, P0=0/P1=0/P2=0`, but the
progression review's P1 finding makes all diagnostic C/D outputs stale for
formal lineage. The two review layers are intentionally recorded separately.

## Verification and review status

The following evidence belongs to the candidate-ready working-byte repair. It
does not become admissible fixed-object evidence until a commit fixes the
identity and a fresh independent review approves that exact object.

| Check                                                               | Current status                                                                                                                          |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Seven named focused files                                           | `105/105 PASS`                                                                                                                          |
| Captured-schema A → B → A two-stage deterministic test              | `PASS`                                                                                                                                  |
| Invalid/reject-all captured output schema fails before late outputs | `PASS`                                                                                                                                  |
| Assembly CLI state, exact arity and negative checks                 | `DECLARED_PRE_REPLAY` / `PRE_REVIEW_ABSENT` / `PASS`                                                                                    |
| Exact late-bound state                                              | 16 exclusions; only immutable legacy `contracts/generation.lock.json` present                                                           |
| Legacy generation lock                                              | 237,214 bytes; SHA-256 `29cd59f1f69e35a6c0fd312524883b6a90be6fe09616dd21864ed9ce52c96101`; unchanged                                    |
| Two code files `oxfmt` / `oxlint` / `git diff --check`              | `PASS`                                                                                                                                  |
| Narrow TypeScript diagnostic                                        | no diagnostics in the 9 R1 files; 11 inherited diagnostics: `platform-generator-supply-profile.ts` 6 and `platform-json-semantics.ts` 5 |
| Root dependency topology                                            | repository-root `node_modules` absent                                                                                                   |
| Out-of-scope legacy v1 supply-profile diagnostic run                | `18/28 PASS`; 10 failures are known Slice B immutable-v1 replay/wheelhouse binding mismatches and are excluded, not a broad failure     |
| Two independent read-only working-byte reviews                      | both `APPROVE, P0=0/P1=0/P2=0`; not fixed-object reviews                                                                                |
| Superseding fixed candidate                                         | `READY_TO_COMMIT`; exact identity will be bound by the separate review record                                                           |
| Historical fixed-object candidate `96d72c9`                         | `REQUEST_CHANGES, P0=0/P1=1/P2=0`                                                                                                       |
| Formal Slice C projection                                           | `NOT_STARTED`                                                                                                                           |
| Formal Slice D Darwin/Linux native replay                           | `NOT_STARTED`                                                                                                                           |
| Slice E successor-lock/evidence assembly                            | `NOT_AUTHORIZED`                                                                                                                        |

The next admissible order is: create a superseding fixed candidate from the
repaired bytes; obtain a fresh independent fixed-object review; rebuild the
projection from that reviewed candidate; rerun Darwin/Linux A/B replay; then
continue only according to ADR-0029's ordered Slices C-H. The v1 profile and
all v1 writer paths remain unchanged.
