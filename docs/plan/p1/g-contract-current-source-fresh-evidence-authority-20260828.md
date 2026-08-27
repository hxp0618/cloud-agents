# D-053 current-source fresh C→D→E evidence authority

Date: 2026-08-28 Asia/Shanghai  
Revision: `D-053-FRESH-20260828-B`  
Status: `PRE_REPLAY_AUTHORITY_FROZEN`

## Scope and lineage

This versioned authority records one superseding, local-only freshness run for
ADR-0030/D-053 after the approved P0 checkout incorporated later bounded
Worker and D-052 descendants. Those descendants change the current Git source
tree, so the earlier D-053 projection, replay receipts, assembled lock,
binding tuple, and terminal review remain immutable historical evidence and
are not silently reused. The approved pre-replay source/consumer repair is
already present in the fixed lineage (`5d9a2666efcdd477edda115a945de96edc11acca`
and `cdadb8908d53252412088af37a4a0543f4553a48`); this revision introduces no
second unversioned repair to those consumers.

The fixed source baseline is the clean P0 parent:

| item | identity |
| --- | --- |
| parent commit | `02d559b97bfbcd46fd52ac02e9af7307fbde39bf` |
| parent tree | `202e947b2851123d7640269a4d8bdce4086f86a0` |
| source | `tools/generator-supply/v3/source.json`, `100644`, 40,076 bytes, Git blob `abd8cd178582acb538d095ace43f914b698804d3`, SHA-256 `sha256:e483a297c20149f34d1a3ad0efc8446a131d3553af114ec319c13a6a3949cfc1` |
| source schema | `tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json`, `100644`, 24,929 bytes, Git blob `2e12dc8464325d7a48caa5fbb9d8cf33c33f7d4d`, SHA-256 `sha256:13c11ffd9c6c8628d59f046ac678b6341f5ea5e694d9a8eefff3f9cd48211464` |
| output schema | `tools/generator-supply/v3/generator-supply-profile-v3.schema.json`, `100644`, 3,772 bytes, Git blob `e19c46819c1898b96345ff50bb327ee0a6b71217`, SHA-256 `sha256:0b500db662990bc80e3cbaef2063ae9c1e72030f0111957803d8315959eb7e57` |

The next reset child may change only the predeclared late-bound D-053 paths
below. It must preserve every r1/r2 migration object, `D-053-EC-2.r3`, all
v1/v2 predecessor source/schema/profile/manifest/archive/review bytes, and all
non-D-053 current source files byte-for-byte. The reset child identity is
bound by the independent review; this authority deliberately does not
self-reference that future commit.

## Exact late-bound reset and projection

The reset removes the existing v3/R5/tuple/registry outputs and restores the
live lock to the fixed post-H v2 predecessor (`contracts/generation.lock.json`,
Git blob `39ee20e035d8770340d46a8663633c6519830de1`, SHA-256
`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`,
17,377 bytes). The exact ordered exclusion set is:

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

Projection is built only by
`scripts/replay-platform-generators-isolated-v3.sh build-projection` from a
clean staged tree. The archive/member and input-tree algorithms are frozen as
`utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1` and
`utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`; node module closure uses
`utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1`. Every path is bound by
canonical path, regular mode, size, Git blob, and SHA-256. No wildcard,
symlink, alias, duplicate, special file, or untracked input is admissible.

## Replay runner and receipt authority

The sole runner is `scripts/replay-platform-generators-v3.ts`, wrapped by
`VERSIONED_ISOLATION_WRAPPER_V3`; helper and archive inspector are
`scripts/lib/generator-replay-path-authority.ts` and
`scripts/lib/inspect-generator-replay-archive.py`. The expected external
toolchain is Node `24.18.1`, Bun `1.3.14`, Python `3.14.7`, uv `0.12.5`,
protoc `35.1`, protoc-gen-go `1.36.12`, and protoc-gen-connect-go `1.20.0`;
Go is recorded from the pinned platform supply. Only `darwin-arm64` and
`linux-amd64` are claimed; `linux-arm64` remains `NOT_CLAIMED`.

Fresh C→D evidence must contain exactly these ordered receipts:

1. `tools/generator-supply/v3/evidence/replay.json`
2. `tools/generator-supply/v3/evidence/replay/darwin-a.json`
3. `tools/generator-supply/v3/evidence/replay/darwin-b.json`
4. `tools/generator-supply/v3/evidence/replay/darwin-isolation.json`
5. `tools/generator-supply/v3/evidence/replay/linux-a.json`
6. `tools/generator-supply/v3/evidence/replay/linux-b.json`
7. `tools/generator-supply/v3/evidence/replay/linux-isolation.json`
8. `tools/generator-supply/v3/evidence/replay/projection.json`

The derived summary must agree with both A/B runs on the projection tree and
archive, candidate manifest SHA-256, replay manifest SHA-256, exact 49 core
outputs, `candidateOutputsEqual=true`, and `nonAllowlistedChanges=0`. A
failed platform or mismatched receipt is a hard failure, not a success.

## Ordered review and side-effect fence

The only permitted order is fresh projection (Slice C), fresh Darwin/Linux
A/B replay (Slice D), bounded profile/lock assembly (Slice E), independent
supply review (Slice F), then the already specified detached R5/tuple/registry
and terminal review steps (Slices G–J). Each candidate/review child is
single-parent and append-only; a P0/P1 finding may be repaired once in the
same revision and rereviewed, while P2 is recorded and deferred. No old byte
is overwritten, no review is self-referential, and no tracker text can promote
the result.

This authority is non-Gate and read-only with respect to external systems. It
does not authorize canonical/production Runner, PostgreSQL or migration writes,
HTTP/OIDC/JWKS, P2/provider/workload/credential/trust effects, SSH or hardware
power actions, deployment, publication, release, force-push, history rewrite,
or any Gate transition. `notGateClosure=true`, `ALL_GATES_OPEN`, and
`closureDecision=NONE` remain mandatory.
