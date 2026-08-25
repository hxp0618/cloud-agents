# G-CONTRACT current-source phase successor Slice B independent review

Date: 2026-08-25 Asia/Shanghai

## Verdict

`APPROVE - P0=0 / P1=0 / P2=1 (deferred, non-blocking)`

This is an independent, read-only review of the fixed ADR-0030 / D-053
Slice B pre-replay implementation candidate. The review did not modify the
candidate, its parent, the primary worktree, any generated late-bound output,
the live generation lock, a remote host, a database, a deployment, a release,
or a Gate record.

The approval is limited to the versioned pre-replay contract/profile/replay
implementation and its local writer/state-machine boundaries. It does not
authorize native replay, projection or assembly, a production database write,
HTTP/P2/provider effect, deployment, publication, signing, release, or Gate
closure. `notGateClosure=true`, `gateStatus=ALL_GATES_OPEN` remain in force.

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        1 (Slice D deferred hardening) |

## Fixed candidate identity

- candidate commit: `ff57e5eb6bb04aaea65450e9d5e52a4cdab0a836`;
- candidate tree: `713523754e2a9686fb4ad7e600a2d7021ae0c890`;
- unique parent: `09db1311bfacac43010b48fb7bf3d253303e0f66`;
- parent-to-candidate binary diff SHA-256:
  `c3763033504bdb8a967854da95a628c5a11ab860f593d7556b14769906203b76`;
- changed paths: exactly 21, all `100644` except the declared executable
  wrapper at `100755`;
- this review path is absent from the candidate tree.

The exact candidate path set is:

1. `scripts/check-platform-g-contract-phase-state.ts`;
2. `scripts/generate-platform-contract-lock-v3.ts`;
3. `scripts/generate-platform-g-contract-phase-record.ts`;
4. `scripts/generate-platform-generator-supply-profile-v3.ts`;
5. `scripts/lib/platform-g-contract-phase-record.test.ts`;
6. `scripts/lib/platform-g-contract-phase-record.ts`;
7. `scripts/lib/platform-generator-supply-profile-source-v3-schema.test.ts`;
8. `scripts/lib/platform-generator-supply-profile-v3.test.ts`;
9. `scripts/lib/platform-generator-supply-profile-v3.ts`;
10. `scripts/lib/platform-generator-supply-replay-v3.test.ts`;
11. `scripts/lib/platform-generator-supply-replay-v3.ts`;
12. `scripts/lib/platform-generator-supply-v3-wrapper.test.ts`;
13. `scripts/lib/platform-successor-dag-v3.test.ts`;
14. `scripts/lib/platform-successor-dag-v3.ts`;
15. `scripts/lib/platform-successor-predecessor-v3.test.ts`;
16. `scripts/lib/platform-successor-predecessor-v3.ts`;
17. `scripts/replay-platform-generators-isolated-v3.sh`;
18. `scripts/replay-platform-generators-v3.ts`;
19. `tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-record-model-v1.schema.json`;
20. `tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json`;
21. `tools/generator-supply/v3/source.json`.

No `tools/generator-supply/v1`, `tools/generator-supply/v2`, or
`tools/contract-review-binding/v1` predecessor byte was changed. `.idea` and
the `migration.test` build artifact are absent from the candidate.

## Reviewed Slice B boundary

The fixed object was checked against ADR-0030 / D-053 and the preceding
approved Slice A repair. The review confirmed:

- the v3 source binds four versioned replay authorities and the immutable
  predecessor fence; source/schema bytes are canonical and self-consistent;
- the exact ordered D-053 projection exclusion set contains 17 unique paths,
  and the frozen core generator output set contains 49 unique paths with no
  overlap;
- the v3 replay contract rejects legacy wrapper policy, reordered or partial
  exclusions, changed core outputs, receipt identity drift, and unknown
  fields; v1/v2 evidence remains predecessor-only;
- the profile registry uses versioned source/artifact/evidence/profile/registry
  digests, binds exact ordered receipts, keeps v3 immutable after replay begins,
  and exposes only pre-replay/receipt/assembly states;
- source, profile, phase-record, binding, and lock writers are explicit,
  append-only exclusive-create/no-op paths. They reject divergent, partial,
  stale, reordered, aliased, symlinked, or downstream topologies and use
  `O_NOFOLLOW` plus stable descriptor/path identity checks;
- the lock writer performs an exclusive transition fence, pre-rename
  revalidation, parent fsync, and post-rename committed-byte readback;
- the Gate-specific phase authority preserves all five current G-CONTRACT
  criteria and derives every `OPEN_NOT_CLAIMED` / `REVIEW_PENDING` row in the
  missing set. It does not promote the Gate or rewrite the tracker;
- all declared non-Gate boundaries remain explicit: production database,
  HTTP/P2/provider, deployment, publication, signing, release, and Gate
  transition are not authorized.

## Fixed-candidate verification

The bounded checks run against the fixed candidate produced:

| Check | Result |
| --- | ---: |
| focused Vitest suite | 9 files / 62 tests PASS |
| generation-lock CLI `--check` | `PRE_REPLAY_LEGACY_LOCK_ONLY` |
| phase-state CLI `--check` | `PRE_CANDIDATE_ABSENT` |
| profile source authority check | `DECLARED_PRE_REPLAY` |
| wrapper shell syntax | PASS |
| targeted `oxfmt --check` | PASS |
| candidate `git diff --check` | PASS |
| candidate-range secret scan (Gitleaks) | 0 findings |

No broad Bun or Go suite, native replay, SSH operation, production database,
HTTP/P2/provider call, deployment, publication, release, or Gate command was
run. These are outside this pre-replay review boundary.

## Deferred P2 hardening

The native runner remains intentionally dormant until ADR-0030 Slice D. A
read-only inspection found that `scripts/replay-platform-generators-v3.ts`
still has a few authority/evidence paths that perform `lstat`/`realpath` and
then reopen by path (`sha256RegularFile`, `requireRegularFile`, and several
v1 evidence reads). This is a theoretical check/read TOCTOU or ABA window.
It is recorded as a deferred P2 for Slice D native-replay hardening; Slice B
does not execute that runner, and the v3 replay semantic snapshot used here
already has stable-read/ABA fences. It is not a blocker for this verdict.

## Progression boundary

`SLICE B PRE-REPLAY IMPLEMENTATION APPROVED WITHIN D-053 NON-GATE BOUNDARY`.

The candidate may be merged as an ordinary history-preserving child into the
P0 branch. Subsequent Slice C projection work must still produce a new fixed
candidate and evidence; this record does not create any exact17 output and
does not authorize projection, native replay, assembly, production writes,
deployment, publication, release, or closure of any Gate.
