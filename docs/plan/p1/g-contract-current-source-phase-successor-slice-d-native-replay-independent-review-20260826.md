# G-CONTRACT current-source phase successor Slice D replay review — scope correction

Date: 2026-08-26 Asia/Shanghai

## Corrected verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This is a fresh, read-only scope-correction review of candidate
`e45ef4e3c5014bec97c7cbe73661559c3d6eced2` under the correct
ADR-0030/D-053 current-source phase successor. The approval is limited to
Slice D's native replay evidence. It does not approve formal Slice E
assembled-lock materialization or Slice F's predeclared supply-v3 review.

The earlier review child `4a087e27aa24f49f8e34b3f6a849ae58c1115f82` and its
record
`g-contract-successor-supply-rebind-slice-d-native-replay-independent-review-20260826.md`
are retained as immutable historical evidence, but their ADR-0029/D-052
label is not a valid D-053 Slice F review binding. This record corrects that
scope without changing the candidate, replay receipts, or historical review
bytes.

No database, HTTP/OIDC/JWKS, P2/provider, deployment, publication, release,
or Gate operation was performed. `notGateClosure=true` and
`ALL_GATES_OPEN` remain in force.

## Fixed D-053 identity

- candidate: `e45ef4e3c5014bec97c7cbe73661559c3d6eced2`;
- parent: `8cd75d1beb10906547b35cde4c8e79756b146913`;
- candidate tree: `f53b8bfcc524518fb4e7f2ba06fab85ad4c42fdc`;
- authority `tools/generator-supply/v3/source.json` has `decisionId: D-053`;
- D-053 baseline commit/tree:
  `16275f6cbf390c343a9ac00f9193e75eaad0094e` /
  `ca595b8e1258a8b78c4da3a545b2a31d8f62b531`.

The candidate's ten changed paths are the v3 replay projection receipt,
Darwin/Linux A/B and isolation receipts, canonical replay summary, evidence
manifest, and v3 profile. The full path and byte table remains in the
historical review record cited above.

## Slice D evidence independently rechecked

The fresh projection reconstructs tree
`513ac8d8331efbcccbebf22baa29e5903115bead`; its archive is
`sha256:edcf3764d3b09cffc0047f2afe307d251193bd124e21fd5d416eb5d7b474b036`
with 48,465,920 bytes and 1,677 safe entries. The exact 49-output candidate
manifest is
`sha256:bedb5d26301f627393a107afda9863899dae0909793ae7df8d0ad06018a282e9`.
Darwin arm64 and Linux amd64 A/B receipts agree on projection/archive,
candidate manifest, output set, and stable fields; `candidateOutputsEqual` is
true and `nonAllowlistedChanges` is zero. Isolation receipts retain network
denial, read-only input/dependency roots, fresh roots, and Linux
UID/GID-65534, zero-capability, `no_new_privs` constraints. Linux arm64 is
`NOT_CLAIMED`; no host-path or identity leakage was found.

Focused v3 tests remain `48 pass / 0 fail` in the post-merge check, profile
assembly/source checks report `ASSEMBLED_PROFILE_CURRENT`, and the bootstrap
checker remains `notGateClosure=true` with only
`remaining-generator-supply-chain-review` missing. These are bounded replay
facts, not a Gate result.

## Formal Slice E boundary — not started

The candidate does not modify `contracts/generation.lock.json`. Its live
generation-lock remains the immutable post-H v2 document (SHA-256
`de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`), and
`bun scripts/generate-platform-contract-lock-v3.ts --check` reports
`PRE_REPLAY_LEGACY_LOCK_ONLY`. Therefore the formal D-053 Slice E tuple
(assembled v3 lock, no-output current checks, and a distinct assembled
candidate) does not exist. `--check-assembled` correctly rejects the current
v2 state. No lock writer was invoked.

The predeclared D-053 Slice F review path
`docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md`
is also absent. This corrected D-Slice review cannot substitute for that
future F review and does not authorize it.

## Progression decision

`D053_SLICE_D_REPLAY_EVIDENCE_APPROVED; SLICE_E_AND_F_PENDING`

The replay evidence is approved only as D-053 Slice D support. Any formal
Slice E lock assembly must start from a reconciled, projection-admissible
lineage under ADR-0030 and remain a separate non-Gate candidate. All
production and Gate boundaries remain closed/open as previously stated.
