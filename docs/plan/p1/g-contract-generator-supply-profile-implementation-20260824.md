# G-CONTRACT generator supply profile implementation — 2026-08-24

## Boundary

This slice implements ADR-0028 on fixed parent `5599f9d20e761532e08906eab1fc8384d48e5b8e` (tree `3a9c5274bf9779b50720c20f39b61fe29228b84c`). The previously reviewed offline wheelhouse repair is carried without semantic changes from implementation `51ce2d6b9faa71a5e89ccf709864f4d570454a38` and review `2a8600b5694b45e39b0c209ae97cbe8f03561339`; their original trees and document hashes are bound in evidence.

No existing closure-profile v1/v2 semantics, Gate record, HTTP/router/provider/project writer, production database, deployment, release, or publication surface is changed. Plan indices and the status tracker name this bounded slice without changing any Gate status.

## Implemented authority

- A sterile npm 11.8.0 official-registry lock with five exact direct dependencies, 35 lock records including root, and 34 resolved dependency records.
- An isolated Go module/sum for the two generator plugins.
- Exact official archives, archive-member-to-effective-executable byte equality, platform wheelhouses, Go build receipts, installed npm content manifests, native binding identity, pinned Ubuntu image/index/platform/config/layer/export identity, and evidence-only scanner bytes. Archive inventory preflight rejects absolute, parent-traversing, duplicate, special, escaping/missing/cyclic/non-regular link targets and link-prefix descendants before any member is accepted; the official bare OSV-Scanner artifact must equal its effective executable bytes directly.
- A strict source/output profile with domain-separated artifact, evidence, profile, and registry digests.
- Raw Syft 1.51.0, CycloneDX 1.6, SPDX 2.3, Grype 0.117.0, and OSV-Scanner 2.5.1 evidence. Canonical summaries bind every document hash, cross-format PURL multiset, per-scope Grype match/severity count, and the exact three OSV source bytes plus scanner version receipt. Current Node 24.18.1 has zero High/Critical findings in the bound database; Node 24.13.1 remains rejected historical evidence with six High findings per platform and no waiver.
- An exact NOTICE inventory derived from all current raw SBOM records. Inventory/text completeness is asserted; legal approval is not.
- A runner that requires absolute regular non-symlink executables with exact bound hashes, accepts only the four exact npm-created relative `.bin` symlinks, validates the UTF-8-bytewise recursive dependency content manifest, and performs only the acyclic core-generator write/check within each fresh projection archive. Before and after every child it revalidates the full source tree, replay-authority digests and `node_modules` binding; only the exact generated-output set may change.
- A versioned wrapper that snapshots and validates the projection, metadata and replay authority before extraction or candidate execution, creates one native isolation boundary per A/B run, requires initially absent independent extraction roots and fresh per-run HOME/TMP/UV/XDG authority, and carries candidate output only through a bounded length-prefixed stdout frame. The trusted parent alone writes the initially absent reports outside candidate-readable paths.
- A checker that requires declared evidence, directory contents, and semantic-reader paths to form one exact closure.

## Included-record acyclic identity

The staged replay authority is fixed independently of the late-bound replay results:

- isolation wrapper: `f85ab149a6a2daf36b3eb1a06a00f0258829ff71a3aae2dc6cec9f3d0601b250`;
- TypeScript runner: `2e07df97c7ca646b365a9090ee0d98af2ede386d56c6647c450c778f4147f58f`;
- in-process formatter helper: `4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf`;
- archive inspector: `db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31`.

The wrapper and runner share one exact sorted set of 48 generated-output paths. This implementation record, ADR-0028, the plan indices, the status tracker, and `source.json` are projection inputs, so they deliberately do not assert their own final candidate commit/tree/diff or projection archive digest. The excluded raw A/B and isolation receipts, projection receipt, generated profile/evidence manifest/generation lock, and fixed independent-review record bind those late values. Inserting them here would create a self-referential projection.

## Native replay evidence

The final immutable digests and command results are filled from the checked-in raw evidence after both native A/B runs. Darwin uses a deny-default sandbox boundary per run with fixed read/exec authority, exact generated-output and ephemeral write authority, denied network/Mach lookup, read-only external supply/projection/`node_modules`, and hostile detached-descendant plus symlink-replacement probes. It proves cross-boundary path denial and A-root destruction before B, but expressly does not claim complete descendant resource/process lifetime closure. Linux uses the pinned Ubuntu rootfs on a generic authorized executor with `unshare` network/mount/PID namespaces and `--kill-child`; trusted setup remains root while generator children are uid/gid 65534 with empty groups, zero capabilities and `no_new_privs`. Rootfs/input/projection/`node_modules` are read-only, only exact generated outputs and fresh tmpfs caches are writable, the candidate cannot read the trusted runner stdout channel, and the native GNU oxfmt binding must load. Machine addresses and host identifiers are not durable evidence.

Replay and profile assembly form a directed acyclic graph. The wrapper copies the staged candidate index and removes only 13 exact late-bound paths: the generation lock, evidence manifest, profile, replay summary, projection receipt, Darwin/Linux A/B reports, Darwin/Linux isolation receipts, rejected-executor receipt, and the fixed final-review record. No replay-directory wildcard is excluded. It then writes a Git tree and emits a deterministic core-projection tar. Darwin/Linux A/B reports bind that one archive and the complete extracted path/mode/size/SHA-256 tree. After all four reports and the summary are fixed, the main candidate runs profile write/check, generation-lock write/check, and a no-output post-assembly verifier. No post-check receipt is inserted into its own inputs; independent review must rebuild and compare the projection.

The earlier empty-`/dev` Linux isolation configuration stopped Bun with `SIGILL`. The same bound Bun and Node passed after adding the namespace-local minimal device tree, so the rejected record names an isolation misconfiguration and explicitly makes no CPU-incompatibility claim. It cannot satisfy Linux replay.

## Verification boundary

Only focused Vitest, formatter/linter, supply generator/checker, generation-lock, platform contract standards, and native replay commands are run. Broad Bun and Go migration suites are outside this slice.
