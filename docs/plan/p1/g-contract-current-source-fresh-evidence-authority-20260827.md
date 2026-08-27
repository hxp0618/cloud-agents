# D-053 fresh C→D→E evidence authority

Date: 2026-08-27 Asia/Shanghai
Revision: `D-053-FRESH-20260827-A`
Status: `PRE_REPLAY_AUTHORITY_FROZEN`

This record freezes the next fresh-evidence candidate after the historical
v3 replay/assembly outputs were reset. It is a local, non-Gate authority only.
It does not authorize replay execution, assembled-lock writes, production
database access, HTTP/P2/provider effects, deployment, publication, release,
or any Gate transition. `notGateClosure=true` and `ALL_GATES_OPEN` remain in
force.

## Lineage fence and reset

The reset is an ordinary child of the approved migration-binding review:

| item | identity |
| --- | --- |
| reset parent | `fde46b4857ed859e7c7cd1c97a219b7569b9a071` |
| reset commit | `f9433788ea8abbbbc23446e1ec0bd28229fdd3b9` |
| reset tree | `4a3b6b0b415b9e5f8a36f88f3ec63035443ab611` |
| reset diff (binary patch SHA-256) | `sha256:89b99b0e277f953c731904d78b6214001ea99f0ecc2f61e138575d83c2c83f0c` |
| fixed post-H predecessor commit/tree | `16275f6cbf390c343a9ac00f9193e75eaad0094e` / `ca595b8e1258a8b78c4da3a545b2a31d8f62b531` |
| live lock predecessor | `contracts/generation.lock.json`, mode `100644`, Git blob `39ee20e035d8770340d46a8663633c6519830de1`, SHA-256 `sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`, 17,377 bytes |

The reset restores the lock to that exact predecessor and removes only the 16
late-bound v3/R5/tuple/registry outputs listed in the exact exclusion set
below. Their Git objects remain recoverable; no v1/v2 source, schema, profile,
SQL, catalog, or portable-runtime tree is changed.

## Frozen current source and schemas

The fresh projection must bind these checked-in regular files byte-for-byte:

| path | mode | bytes | SHA-256 | Git blob |
| --- | --- | ---: | --- | --- |
| `tools/generator-supply/v3/source.json` | `100644` | 40,076 | `sha256:e483a297c20149f34d1a3ad0efc8446a131d3553af114ec319c13a6a3949cfc1` | `abd8cd178582acb538d095ace43f914b698804d3` |
| `tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json` | `100644` | 24,929 | `sha256:13c11ffd9c6c8628d59f046ac678b6341f5ea5e694d9a8eefff3f9cd48211464` | `2e12dc8464325d7a48caa5fbb9d8cf33c33f7d4d` |
| `tools/generator-supply/v3/generator-supply-profile-v3.schema.json` | `100644` | 3,772 | `sha256:0b500db662990bc80e3cbaef2063ae9c1e72030f0111957803d8315959eb7e57` | `e19c46819c1898b96345ff50bb327ee0a6b71217` |

The source's predecessor closure is the eight ordered v1/v2/lineage/history
groups already recorded in `source.json`; its complete input scope is the
sorted, explicit tracked file list, with each member bound by path, mode,
Git blob, byte length, and SHA-256. No wildcard or implicit discovery is
allowed. The replay contract binds exactly 49 core generator outputs.

## Projection, archive, and member-manifest algorithms

The projection is an exact ordered path list. The only exclusions are the
following 17 paths, in this order; every other tracked byte is included:

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

The exclusion policy is `EXACT17_ONLY_NO_WILDCARD_ALL_OTHER_TRACKED_BYTES_INCLUDED`.
The frozen algorithms are:

- node modules: `utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1`;
- projection archive members:
  `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`;
- extracted input tree:
  `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`.

The archive is produced by
`scripts/replay-platform-generators-isolated-v3.sh` under
`VERSIONED_ISOLATION_WRAPPER_V3`; archive bytes, member count, and both tree
manifests must be captured in fresh receipts. No historical archive or replay
receipt is admissible evidence for this revision.

## Runner, toolchain, and platform fence

The runner is `scripts/replay-platform-generators-v3.ts`; path and archive
helpers are `scripts/lib/generator-replay-path-authority.ts` and
`scripts/lib/inspect-generator-replay-archive.py`. The runner requires
`ENV_I_MINIMAL_V1`, fresh extraction/caches, no ambient `node_modules`, and
the pinned external supply closure. Claimed native platforms are only
`darwin-arm64` and `linux-amd64`; `linux-arm64` remains `NOT_CLAIMED`.
Expected tool versions are Node `24.18.1`, Bun `1.3.14`, Python `3.14.7`,
uv `0.12.5`, protoc `35.1`, protoc-gen-go `1.36.12`, and
protoc-gen-connect-go `1.20.0`; Go is the version emitted by the pinned
platform supply and is independently recorded in each run receipt.

## Receipt set and review rules

Fresh C→D evidence must produce this exact ordered receipt set:

1. `tools/generator-supply/v3/evidence/replay.json`
2. `tools/generator-supply/v3/evidence/replay/darwin-a.json`
3. `tools/generator-supply/v3/evidence/replay/darwin-b.json`
4. `tools/generator-supply/v3/evidence/replay/darwin-isolation.json`
5. `tools/generator-supply/v3/evidence/replay/linux-a.json`
6. `tools/generator-supply/v3/evidence/replay/linux-b.json`
7. `tools/generator-supply/v3/evidence/replay/linux-isolation.json`
8. `tools/generator-supply/v3/evidence/replay/projection.json`

The derived summary must agree with both A/B runs on candidate manifest
SHA-256, replay manifest SHA-256, `outputFiles=49`, and
`candidateOutputsEqual=true`; any mismatch fails closed. After receipts are
fresh and independently admissible, the ordered ADR flow permits the bounded
Slice E lock assembly preflight. Slice F is the independent generator-supply
review after assembly; it is not a prerequisite for starting Slice E. Any
assembled lock candidate remains non-Gate and requires its own fixed-object
review before later slices consume it.

Independent review must bind this revision, reset parent/commit/tree, every
receipt's path/mode/size/SHA-256, the exact exclusion order, and the complete
lineage back to the fixed post-H predecessor. Review output is P0/P1/P2 only:
P0 or P1 permits one repair within this same revision followed by one fresh
review; P2 is recorded and deferred. A review cannot close a Gate or authorize
production, external effects, deployment, publication, or release.
