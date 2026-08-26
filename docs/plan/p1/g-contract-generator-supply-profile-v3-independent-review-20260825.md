# G-CONTRACT generator-supply profile v3 independent review

Date: 2026-08-26

Review subject: the superseding D-053 Slice E generator-supply-v3 candidate on
`codex/cloud-agents-platform-p0`. The reviewer evaluated the fixed current
source, fresh Slice C projection, fresh native Slice D replay, assembled
profile/manifest, and assembled generation lock as one candidate. This record
is a direct child of the candidate and is not an input to projection or
replay.

## Verdict

APPROVE - P0=0 / P1=0 / P2=0

Normalized verdict: `APPROVE_P0_0_P1_0_P2_0`.

This verdict approves only the fixed Slice E supply-v3 candidate and permits
the predeclared G-CONTRACT R5 and review-binding slices to proceed. It is not
a Gate closure and does not authorize a production database write, HTTP/P2 or
provider effect, deployment, publication, release, signing, or any other
external side effect. `notGateClosure=true` and `gateStatus=ALL_GATES_OPEN`
remain in force. Linux arm64 remains `NOT_CLAIMED`.

## Fixed candidate and superseding lineage

- candidate commit: `e72d510bd623592c6078e4e76aee0ea52e910804`;
- immediate parent (assembled-profile commit):
  `b1c919bd4302f19c5459e332ab1fa2fb29474d50`;
- candidate tree: `12e0e5ca1fc53e0f6292ce2c5c673feeb226a7d7`;
- parent tree: `dae3dd01692b14ba3c3f3e4443ac633d32206baf`;
- parent-to-candidate domain-separated binary diff:
  `sha256:9d0018c91e404bd2b0e1ed466adcfaa5e87f40f691459e6475e0c577878a132f`;
- the candidate is a single-parent direct child and changes exactly
  `contracts/generation.lock.json`;
- Slice C projection commit: `80e80ceafc28beea7a8bb5d3db0984c42d90a64a`;
- fresh Slice D replay commit: `1104dd8eccf01a0cc4eaa7d6d43b32c6a0318ec3`;
- Slice E assembled-profile commit: `b1c919bd4302f19c5459e332ab1fa2fb29474d50`;
- the pre-replay reset and authorization lineage remains recoverable in Git;
  the former D-053 late-bound evidence is not reused;
- the immutable v2 predecessor is commit
  `16275f6cbf390c343a9ac00f9193e75eaad0094e`, tree
  `ca595b8e1258a8b78c4da3a545b2a31d8f62b531`, Git blob
  `39ee20e035d8770340d46a8663633c6519830de1`, and SHA-256
  `sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`.

The supply review path, R5 path, R5 review path, tuple, registry, and terminal
review path are absent from the candidate tree. The review therefore cannot
self-review the candidate.

## Fresh projection and native replay

The current projection receipt binds:

- projected tree: `a3287badd18438b046dd56d79a974be01eb60835`;
- archive SHA-256: `sha256:fc1c1f11f0d80df2fd4f458fc04434a66fdbf5f60678be684df9442230082c22`;
- archive size: `48,537,600` bytes;
- archive members: `1,686`; regular input files: `1,490`;
- archive-member manifest SHA-256:
  `sha256:b5d895e30314363fc62df64ca923241c7d7e44d4b914d262920ac29bcdeda676`;
- input regular-file manifest SHA-256:
  `sha256:1ae644e53c7d6d6226037ac6bf61d3850c16fc48ff54e8040da1728c3a8dbe57`;
- projection receipt SHA-256:
  `sha256:30104aae57db37a356408020db4c661dd9c5a30c0fe330af13ede0e82fe17be7`.

Darwin-arm64 and Linux-amd64 A/B native replay receipts bind the same
projection, the exact 49-output candidate manifest
`sha256:bedb5d26301f627393a107afda9863899dae09097993ae7df8d0ad06018a282e9`,
and report `candidateOutputsEqual=true` with
`nonAllowlistedChanges=0`:

| Receipt | SHA-256 | Bytes |
| --- | --- | ---: |
| Darwin A | `sha256:d5c23cdd14190acdc610fb55189fbadfcf6dbd346709347e2ed9a8c8752217e9` | 3054 |
| Darwin B | `sha256:f0e62a34be5dfc661a6c91951cb40fc41f4c88837931ae2fbe1858ab7905d505` | 3054 |
| Darwin isolation | `sha256:8dcd0d94725bc78568843df0784d22651951ecde1cb6bcc720326a1fb71907cd` | 7697 |
| Linux A | `sha256:d569876ad22aa40e4070bd13fcbf43f27d5a9fa2d1de6c5edb261a7020181abe` | 3055 |
| Linux B | `sha256:502efcb00d20a25e957b209abbb48500a9dd2a7591976c27e86a9e8fe678afdd` | 3055 |
| Linux isolation | `sha256:d2ef053eee1dd39703a1f9104e384c6c8afb6fe8a3e614ddc7bf50db8ec5722a` | 11459 |
| Derived replay summary | `sha256:8a768891ff99895b489913ddd73d21f53753d4c32bfd9ab9284cf190ec142fed` | 2127 |

The Darwin receipt reports network denial and read-only supply/archive
authority. The Linux receipt reports fresh per-run extraction, read-only
rootfs/input bindings, network/mount/PID isolation, and unprivileged generator
children. These are bounded receipt claims, not a claim of full distribution
coverage or Gate completion.

## Assembled profile and generation lock

The assembled profile and evidence manifest are current and byte-stable:

- profile digest: `sha256:463772ade2db4ba5f4deded5aff0a82a3ec5b74715add1423fcd3ee188ffa37d`;
- registry digest: `sha256:2466c6dde3d086773fee04889ca9676997521c64a4f362d972ba8669aeff9094`;
- source digest: `sha256:77070adf213107b72012202cf44c55f93f49df96f4b6bd1a1cc2b28b295dbba3`;
- artifact-set digest: `sha256:28f3d5435a9a8f509fe79850c90fcae35a8e7bcf30c8a6dea94cc111516ca26b`;
- evidence state: `ASSEMBLED_LATE_BOUND`;
- profile status: `REPLAY_VERIFIED_REVIEW_PENDING`;
- candidate output manifest: `sha256:bedb5d26301f627393a107afda9863899dae09097993ae7df8d0ad06018a282e9`;
- output files: `49`.

The v3 lock is `ASSEMBLED`, binds the current profile/manifest/projection and
the exact 17-path exclusion list, and explicitly retains
`notGateClosure=true`, `gateStatus=ALL_GATES_OPEN`, and the v2 predecessor.
The lock document records lock digest
`sha256:dba2fe2297d9fd5b064eecd4e9611b8f321643050c8f41a722b86eba5c908785`.

The MD5 successor migration `000014_harden_durable_project_create_identifiers.sql`
is physically present in the exact17 lexical projection because it is not an
excluded path. It remains semantically uninstalled and not runner-bound; the
  immutable v1/`000013` authority and predecessor bytes were not replaced by this
  review; the current `000013` SHA-256 remains
  `d8c3687e300767f7e27f673c6a9fc3de098fbec1b8911dc018c47d32de33dffa`.

## Independent checks and limitations

The following checks were rerun against the fixed candidate before this review:

- source and assembly checks: `ASSEMBLED_PROFILE_CURRENT`;
- assembled lock check: `ASSEMBLED` current;
- G-CONTRACT topology check: `PRE_CANDIDATE_ABSENT`;
- focused profile/replay/lock/successor-DAG tests: `37/37 PASS`;
- replay authority validation and `git diff --check`: pass;
- no production database, HTTP/P2/provider, deployment, publication, release,
  signing, or Gate action: pass by operation boundary.

The first Linux transfer attempt with AppleDouble metadata is retained only as
non-admissible diagnostic history; it produced no receipt. The final Linux
receipts above came from the pinned clean supply transfer and native Linux
isolation boundary. The bounded authorized SSH execution used only an explicit
temporary directory, which was removed after receipt recovery; no database
write or other remote persistent state remains.

## Progression boundary

This review approves consumption of the exact v3 supply profile, manifest,
projection, replay receipts, and assembled lock by the predeclared G-CONTRACT
phase state machine. It does not create the R5 phase record, review-binding
tuple, registry, or terminal review. Those objects require their own fixed
direct-child candidates and independent reviews. All aggregate Gates remain
OPEN.
