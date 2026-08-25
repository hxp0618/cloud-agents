# G-CONTRACT successor/supply rebind Slice D native replay independent review

Date: 2026-08-26 Asia/Shanghai

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This is an independent, read-only review of the fixed ADR-0029/D-052
successor/supply-rebind Slice D candidate, after the D-053 current-source
contract-standards repair. The review validates the late-bound projection,
the dual-platform A/B receipts, the isolation receipts, and the assembled
generated v3 profile. The reviewer did not modify the candidate, the primary
worktree, a remote host, a database, a deployment, a release, or a Gate
record. The reviewer did not execute another native replay; the replay
receipts below were independently re-hashed and semantically checked.

This approval is limited to the bounded generated replay/evidence slice. It
does not approve a successor generation-lock publication, a detached review
binding, a production database write, HTTP/OIDC/JWKS, P2/provider effects,
deployment, publication, release, or any Gate transition. `ALL_GATES_OPEN`
and `notGateClosure=true` remain in force.

## Fixed candidate identity

- candidate branch:
  `codex/p1-g-contract-slice-d-native-replay`;
- candidate commit:
  `e45ef4e3c5014bec97c7cbe73661559c3d6eced2`;
- candidate tree:
  `f53b8bfcc524518fb4e7f2ba06fab85ad4c42fdc`;
- parent:
  `8cd75d1beb10906547b35cde4c8e79756b146913`;
- parent-to-candidate binary diff SHA-256:
  `6f93f7d150b0b7994a200d03d7c70ea7cf40a80710ffc90401e588635facd121`;
- the review record is intentionally absent from the candidate and is
  recorded as a separate child commit.

The candidate contains exactly these ten late-bound generated paths:

1. `tools/generator-supply/v3/evidence-manifest.json`;
2. `tools/generator-supply/v3/evidence/replay.json`;
3. `tools/generator-supply/v3/evidence/replay/darwin-a.json`;
4. `tools/generator-supply/v3/evidence/replay/darwin-b.json`;
5. `tools/generator-supply/v3/evidence/replay/darwin-isolation.json`;
6. `tools/generator-supply/v3/evidence/replay/linux-a.json`;
7. `tools/generator-supply/v3/evidence/replay/linux-b.json`;
8. `tools/generator-supply/v3/evidence/replay/linux-isolation.json`;
9. `tools/generator-supply/v3/evidence/replay/projection.json`;
10. `tools/generator-supply/v3/profile.json`.

No `.idea` file, migration test binary, backup artifact, or unrelated tracked
path entered the candidate.

## Projection and archive authority

The fresh projection reconstructs Git tree
`513ac8d8331efbcccbebf22baa29e5903115bead`. Its archive is
`sha256:edcf3764d3b09cffc0047f2afe307d251193bd124e21fd5d416eb5d7b474b036`
with 48,465,920 bytes and 1,677 entries (196 directories, 1,481 regular
files, zero symlinks, hardlinks, special entries, unsafe entries, or duplicate
entries). The archive member manifest is
`sha256:c0b4e337bb671f88ccda6d30d116708998fb0e1d0d5c127c69ddf0fa05a138bb`;
the regular-file input manifest is
`sha256:d9b5522f0096497e00287b826999e35359c53674a0ae33ad5f10d3f2e7df200c`.

An independent archive inspection reconstructed the same tree and both
manifest digests. The old projection singleton is not silently reused: its
tree/archive/input values are superseded by the fresh values above, while the
declared exact-17 exclusion order remains unchanged. The compact projection
receipt is canonical wrapper metadata and is intentionally not reformatted by
the repository formatter.

## Replay and assembled evidence

The canonical replay summary reports
`DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED`, binds the exact 49 core
outputs, and records `candidateOutputsEqual=true` with
`nonAllowlistedChanges=0`. Its candidate output manifest is
`sha256:bedb5d26301f627393a107afda9863899dae0909793ae7df8d0ad06018a282e9`.
The wrapper is v3 and has SHA-256
`sha256:9acfc4163fead4dace517c069b8b0e74aaacc859e8cdd2dee17b84182d0be990`.
The pinned Linux rootfs is bound by
`sha256:25ecc117cd77a289cc25006605dcf4ec8b137fec326db766d0abcd4147f6093e`.

The eight checked-in evidence records are:

| Record | SHA-256 | Size (bytes) |
| --- | --- | ---: |
| `replay.json` | `00d483386143f7239bd0bec0a0ce64c082d185a081c78b813064269b95582b06` | 2,127 |
| `replay/darwin-a.json` | `7d0cd84afc96bf276d1c728b819d2b28872143b9bc3853c127679262d622af41` | 3,054 |
| `replay/darwin-b.json` | `46f2c905d64c3a027652552a0111e082ad7134c238243f3198bca9c2ff675e8c` | 3,054 |
| `replay/darwin-isolation.json` | `d2715cb53db28612472f76e207d42a6666ae403ce699f5bcedf7c7dd812027e1` | 7,697 |
| `replay/linux-a.json` | `461bff47597cf5ed642d3eda220435e64f064c5604cb3fdce99609ed51575bfd` | 3,055 |
| `replay/linux-b.json` | `e4ee269253f83a88cb0f779959d8d05aea495a2a8c21350a351f6bd69e4bd43f` | 3,055 |
| `replay/linux-isolation.json` | `4076cb42d67ed4e5cb93aa0de3eda8ed78addc3269f0fc575c048c66b932571a` | 11,459 |
| `replay/projection.json` | `733705592c8aba39b22a4008cca415c9eea9e6f62ad6d1e6fbaa914fcc3203f6` | 1,999 |

`buildGeneratorSupplyReplayV3PreparedReceipts` derives the canonical summary
from the seven platform/isolation/projection receipts and performs an exact
canonical comparison before the summary is accepted. The checked-in summary
therefore remains a derived late-bound record, not an unverified hand-written
input. The evidence manifest and nested profile receipt list have the same
ordered paths, digests, and sizes.

Darwin arm64 and Linux amd64 A/B runs bind the same projection tree/archive,
1,677 archive members, 1,481 input regular files, 49 output files, and the
candidate manifest. Stable fields match across A/B; run-specific labels are
the only expected differences.

## Isolation and leakage review

The Darwin isolation receipt records sandbox network denial, read-only input
and dependency roots, fresh output/cache roots, and negative probes. The Linux
receipt records network namespace denial, a read-only pinned rootfs and
read-only projection/input/node-module roots, UID/GID 65534, zero effective
capabilities, and `no_new_privs`. Both receipts reject host-path, username,
and address leakage. Detached-descendant cross-run denial remains explicitly
unclaimed by this slice; that is a documented v3 boundary rather than a
candidate defect.

## Focused verification

| Check | Result |
| --- | --- |
| v3 replay/profile/DAG/predecessor/contract-lock focused tests | 61 pass / 0 fail |
| generator-supply v3 assembly check | `ASSEMBLED_PROFILE_CURRENT` |
| generator-supply v3 source check | current |
| contract-lock v3 check | `PRE_REPLAY_LEGACY_LOCK_ONLY` (expected; no v3 lock claim) |
| bootstrap contract checker | `BOOTSTRAP_VALIDATED`, 64 schemas / 2 manifests / 79 cases, `notGateClosure=true` |
| fixed v1 predecessor and v2 evidence checks | pass/current |
| changed-file formatter, linter, diff and credential-like literal scan | pass; no finding |
| full Bun orchestration | not claimed; local Bun 1.4.0 differs from pinned 1.3.14 |

The review did not run a broad Bun suite, `go test ./...`, the broad migration
suite, a production database operation, HTTP/P2/provider code, deployment,
publication, release, or any Gate operation.

## Progression decision

`SLICE_D_NATIVE_REPLAY_APPROVED_FOR_ASSEMBLED_NON_GATE_EVIDENCE`

The fixed candidate and its evidence may be merged into the P0 lineage with a
normal non-squashing merge. The next work remains bounded by the accepted
successor order: keep v3 `REPLAY_VERIFIED_REVIEW_PENDING`, perform only the
separately authorized review/binding steps, and retain all production and Gate
boundaries. No aggregate Gate changes from `IN PROGRESS`/`OPEN`.
