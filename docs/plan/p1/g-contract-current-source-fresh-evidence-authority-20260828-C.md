# D-053 current-source fresh evidence authority C

Date: 2026-08-28 Asia/Shanghai

Revision: `D-053-FRESH-20260828-C`

Status: `RESET_AUTHORITY_FROZEN`

## Purpose and immutable predecessor

This record authorizes one versioned, local-only freshness sequence for the
current `codex/cloud-agents-platform-p0` source after the approved D-054--D-057
descendants. It supersedes the stale B authority only by lineage; it does not
rewrite or replace B's bytes. The prior record
[`D-053-FRESH-20260828-B`](g-contract-current-source-fresh-evidence-authority-20260828.md),
its projection/replay/lock/tuple/registry/review receipts, and all D-053-EC-2
predecessors remain immutable historical evidence.

The reset candidate has exactly one parent and is created from the following
fixed clean baseline (before this candidate's targeted reset):

| item | frozen identity |
| --- | --- |
| parent commit | `32f430ae8e3b8d4b5c8c8263ae63979927182280` |
| parent tree | `74c964a25b7507cd3ca2169e4b781b128b74aacf` |
| source | `tools/generator-supply/v3/source.json`, mode `100644`, 40,076 bytes, Git blob `abd8cd178582acb538d095ace43f914b698804d3`, SHA-256 `sha256:e483a297c20149f34d1a3ad0efc8446a131d3553af114ec319c13a6a3949cfc1` |
| source schema | `tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json`, mode `100644`, 24,929 bytes, Git blob `2e12dc8464325d7a48caa5fbb9d8cf33c33f7d4d`, SHA-256 `sha256:13c11ffd9c6c8628d59f046ac678b6341f5ea5e694d9a8eefff3f9cd48211464` |
| output schema | `tools/generator-supply/v3/generator-supply-profile-v3.schema.json`, mode `100644`, 3,772 bytes, Git blob `e19c46819c1898b96345ff50bb327ee0a6b71217`, SHA-256 `sha256:0b500db662990bc80e3cbaef2063ae9c1e72030f0111957803d8315959eb7e57` |

No source/schema/profile/manifest/SQL/catalog/archive/review byte outside the
declared late-bound set may change in this candidate.

## Exact reset set and fixed successor lock

The reset performs explicit `git rm` on exactly the following ordered paths;
the lock is then restored byte-for-byte from fixed post-H v2 blob
`39ee20e035d8770340d46a8663633c6519830de1` (SHA-256
`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`,
17,377 bytes):

1. `contracts/generation.lock.json`
2. `tools/generator-supply/v3/evidence-manifest.json`
3. `tools/generator-supply/v3/profile.json`
4. `tools/generator-supply/v3/evidence/replay.json`
5. `tools/generator-supply/v3/evidence/replay/darwin-a.json`
6. `tools/generator-supply/v3/evidence/replay/darwin-b.json`
7. `tools/generator-supply/v3/evidence/replay/darwin-isolation.json`
8. `tools/generator-supply/v3/evidence/replay/linux-a.json`
9. `tools/generator-supply/v3/evidence/replay/linux-b.json`
10. `tools/generator-supply/v3/evidence/replay/linux-isolation.json`
11. `tools/generator-supply/v3/evidence/replay/projection.json`
12. `docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md`
13. `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md`
14. `docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md`
15. `tools/gate-phase-record/g-contract-p1/v1/review-tuple.json`
16. `tools/gate-phase-record/g-contract-p1/v1/registry.json`
17. `docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md`

The other sixteen paths remain absent until their versioned C/D receipts are
created. No wildcard, alias, symlink, duplicate, special file, untracked file,
or path outside this list is admissible.

## Projection, archive, and input identity algorithms

Slice C must invoke
`scripts/replay-platform-generators-isolated-v3.sh build-projection` against a
clean staged tree. The archive algorithm is
`utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`; member
manifest is `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`; input/node
module closure is `utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1`.
Every member is bound to canonical path, regular mode, byte size, Git blob and
SHA-256. Archive bytes, projection tree, and manifest are deterministic and
must be independently recomputed before acceptance.

## Runner, helper, inspector, toolchain, and platforms

The sole replay runner is
`scripts/replay-platform-generators-v3.ts` (SHA-256
`sha256:759c3b6578d3ec5c818c9ce5bc92b2d560727363e32c833494823a296ef555fd`),
wrapped by policy `VERSIONED_ISOLATION_WRAPPER_V3` at
`scripts/replay-platform-generators-isolated-v3.sh` (SHA-256
`sha256:9acfc4163fead4dace517c069b8b0e74aaacc859e8cdd2dee17b84182d0be990`).
The path helper is
`scripts/lib/generator-replay-path-authority.ts` (SHA-256
`sha256:4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf`),
and the trusted archive inspector is
`scripts/lib/inspect-generator-replay-archive.py` (SHA-256
`sha256:db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31`).
Trusted executables are `/usr/bin/git` and `/usr/bin/python3`.

The expected toolchain is Node `24.18.1`, Bun `1.3.14`, Python `3.14.7`, uv
`0.12.5`, protoc `35.1`, protoc-gen-go `1.36.12`,
protoc-gen-connect-go `1.20.0`, with Go supplied by the pinned platform
profile. Only `darwin-arm64` and `linux-amd64` may be claimed;
`linux-arm64` is `NOT_CLAIMED`.

## Receipt paths and ordered lineage

The fresh C→D sequence may emit only these ordered receipt paths:

1. `tools/generator-supply/v3/evidence/replay/projection.json` (Slice C)
2. `tools/generator-supply/v3/evidence/replay/darwin-a.json`
3. `tools/generator-supply/v3/evidence/replay/darwin-b.json`
4. `tools/generator-supply/v3/evidence/replay/darwin-isolation.json`
5. `tools/generator-supply/v3/evidence/replay/linux-a.json`
6. `tools/generator-supply/v3/evidence/replay/linux-b.json`
7. `tools/generator-supply/v3/evidence/replay/linux-isolation.json`
8. `tools/generator-supply/v3/evidence/replay.json`

Each child is single-parent and append-only. Projection must bind the reset
candidate commit/tree and exact source/schema identities above. D receipts must
agree on projection archive/tree, candidate and replay manifest SHA-256, exact
49 generator outputs, `candidateOutputsEqual=true`, and
`nonAllowlistedChanges=0`; a failed or mismatched platform is a hard failure.
Only after C and D may bounded profile/lock assembly, independent review, and
the historical G–J detached records proceed. Old receipts are never reused.

## Independent review and side-effect fence

One independent read-only review is required for this C candidate. Review checks
parent/tree, exact 17-path diff, lock blob identity, all frozen algorithms and
digests, receipt allowlist, lineage, and the side-effect fence. P0/P1 findings
may be repaired once within this same C revision and rereviewed; P2 is recorded
and deferred. No self-referential review bytes may be accepted, and no finding
may close a Gate.

This authority is non-Gate and local/read-only with respect to external
systems. It does not authorize canonical/production Runner, PostgreSQL or
migration writes, HTTP/OIDC/JWKS/P2/provider/workload/credential/trust effects,
SSH or hardware power actions, deployment, publication, release, force-push,
history rewrite, reflog expiry, GC/prune, or any Gate transition.
`notGateClosure=true`, `gateStatus=ALL_GATES_OPEN`, and
`closureDecision=NONE` remain mandatory.
