# ADR-0028: P1 generator supply profile

- Status: Accepted under the standing P1 execution approval on 2026-08-24
- Date: 2026-08-24
- Decision ID: D-051
- Depends on: ADR-0007, CAG-G-CONTRACT-P1-20260823-R4, and current bounded G-SUPPLY-CHAIN evidence
- Decision owner: hxp0618
- Implementation executor: Codex
- Gate effect: none; G-CONTRACT and G-SUPPLY-CHAIN remain IN PROGRESS
- Scope: G-CONTRACT remaining generator supply-chain evidence

## Context

The contract generators already have immutable input/output contracts, but replay is only reviewable when every executable, dependency closure, native binding, evidence scanner, and isolation transcript is bound to versioned bytes. The repository root `bun.lock` reflects legacy `npmmirror` workspace context and is not acceptable as generator execution authority.

The first evidence pass pinned Node 24.13.1. The same pinned Grype 0.117.0 database found six High findings on both supported platforms. That runtime is rejected before creation of the first profile; it is historical evidence, not a predecessor profile and not a waiver.

## Decision

Create `tools/generator-supply/v1` as an independent generated profile registry. It is outside contract bootstrap/schema discovery. Its source, strict schemas, declared evidence closure, evidence manifest, generated profile, generator, replay runner, and generation-lock pipeline are versioned together.

The current runtime is Node 24.18.1. npm 11.8.0 uses an isolated official-registry lock containing 35 package records including the root and 34 dependency records with `registry.npmjs.org` integrity URLs. Effective native installations contain 16 packages on Darwin arm64 and 17 on Linux amd64; Linux must load the GNU binding even though the musl binding is also present. Go 1.26.6 and `gofmt` are sibling executables. Bun 1.3.14, Astral CPython 3.14.7, uv 0.12.5, protoc 35.1, both Go plugins, and all 21 wheels per platform are exact-byte inputs.

The only claimed platforms are native Darwin arm64 and native Linux amd64. Linux arm64 is `NOT_CLAIMED`. A versioned isolation wrapper builds one deterministic Git-tree archive of the explicit core-generator input projection. The projection excludes only replay summary/raw receipts, the generated supply profile and evidence manifest, `contracts/generation.lock.json`, and the current slice's future independent-review record. It includes the wrapper, runner, core generators and transitive inputs, static raw supply evidence, and checked-in core outputs. Its canonical extracted-tree manifest is UTF-8-bytewise path, Git mode, size, and SHA-256 bound. Both platforms and both A/B runs bind the same projection tree/archive bytes, independently absent extraction roots, complete extracted input tree, exact external executable set, `node_modules`, wheelhouse, and core output manifest.

The wrapper, not caller-supplied verdict variables, creates and records the isolation boundary. Each Darwin run executes its negative probes and core replay in a separate `/usr/bin/sandbox-exec` deny-default boundary. The profile denies network and Mach lookup, admits only the fixed system/supply/current-run/projection read and executable authority, and admits writes only to the fresh ephemeral root and exact generated outputs. The A write root is destroyed before B is created; fork/`setsid` and detached `posix_spawn` probes must remain unable to read a trusted-parent-only sentinel. Because no descendant is carried across A/B as a runtime probe, a detached process can retain non-path resources, and `sandbox-exec` is deprecated, Darwin explicitly records both `detachedDescendantsCrossRunReadDenied=false` and `processLifetimeClosure=NOT_CLAIMED_RESOURCE_ONLY_RESIDUAL`; it does not claim that every descendant has exited.

Each Linux run starts only after the trusted parent has snapshotted and inspected the projection and pinned Ubuntu rootfs. It then uses independent extraction roots under `unshare --net --mount --pid --fork --kill-child`, a read-only rootfs, supply, projection, source and `node_modules`, tmpfs `/tmp`, ephemeral authority and minimal `/dev`, and no default route. Trusted orchestration remains uid 0, while every generator child runs as uid/gid 65534 with no supplementary groups, zero effective/permitted/bounding/ambient capabilities and `no_new_privs`; only the exact generated outputs and fresh cache authority are candidate-writable. An earlier empty-`/dev` isolation configuration raised `SIGILL`; the same bound Bun and Node passed after the task namespace supplied the minimal device tree, so CPU incompatibility is not claimed and the failed configuration is not replay evidence.

Assembly is intentionally acyclic: authoritative A/B replays execute only core generators; once their reports and isolation summaries are fixed, generation proceeds in order `replay summary -> evidence manifest/profile write+check -> generation lock write+check -> no-output post-assembly current check`. A post-assembly check result is never fed back into the projection it verifies. Independent review must rebuild the projection and match the reported tree/archive digests; neither the final implementation commit nor its future review record is asserted as an input to itself.

The evidence checker fails closed unless the source-declared paths, recursively enumerated evidence files, and explicit semantic-reader closure are identical. It independently verifies safe archive inventory and archive-member-to-effective-executable bytes, derives SBOM cross-format PURL multisets, image digests, NOTICE inventory, per-scope current and rejected vulnerability findings, scanner/database/source receipts and timestamps, ignored/suppressed matches, isolation probes, and A/B/cross-platform replay equality from bound raw files. Installed npm manifests use global UTF-8 bytewise path ordering, including their exact contained `.bin` symlink set.

The profile status is `REPLAY_VERIFIED_REVIEW_PENDING`. Attestation semantics are `DETACHED_DIGEST_BOUND_INDEPENDENT_REVIEW`; no cryptographic signature, authenticated signer identity, or publication is claimed. This decision does not authorize HTTP/P2/provider effects, production database writes, deployment, release, or Gate closure.

## Consequences

- `v1` evidence and generated bytes are immutable after independent review; a repair requires a new version.
- Node 24.13.1 evidence remains bound as a rejected historical baseline with the exact six-CVE/fixed-version mapping.
- SBOM/NOTICE completeness is an inventory and text claim only, never legal approval.
- Vulnerability results are tied to the recorded scanner/database bytes and collection time; they are not a timeless-clean claim.
- The supply profile and generation lock cannot include themselves as source inputs. Their current bytes are proven only by the later no-output post-assembly check and independent review.
