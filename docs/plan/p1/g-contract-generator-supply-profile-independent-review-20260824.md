# G-CONTRACT generator-supply profile independent review

Date: 2026-08-24

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

Two independent, read-only reviews approved the fixed candidate: one reviewed
the commit graph, projection reconstruction, raw replay receipts, and digest
closure; the other reviewed evidence semantics, fail-closed behavior, security
boundaries, and Gate claims. Neither reviewer modified the candidate or reran
native A/B replay or broad tests.

This approval covers only the fixed generator-supply replay profile candidate.
It does not mutate that candidate into a review-complete generated profile, does
not satisfy the remaining generator supply-chain criterion, and does not close
G-CONTRACT, G-SUPPLY-CHAIN, or any other Gate. `ALL_GATES_OPEN` remains in
force.

## Fixed lineage and scope

- candidate branch: `codex/cloud-agents-platform-p0`;
- candidate commit: `e5f981c8197cea7527a57c391e7198570f61b92c`;
- candidate tree: `7fb98abf71066e8009581c658b41a299ae1a5c2c`;
- parent: `0a331fde18a909d37b64f11efe879df7bbc09d25`;
- candidate diff SHA-256:
  `d012683bf1a13dda79a8393afdf44ff20088711b9ccce1c608cd74db5843587e`.

The candidate changes exactly these 12 paths:

1. `contracts/generation.lock.json`;
2. `scripts/lib/platform-generator-supply-profile.test.ts`;
3. `tools/generator-supply/v1/evidence-manifest.json`;
4. `tools/generator-supply/v1/evidence/replay.json`;
5. `tools/generator-supply/v1/evidence/replay/darwin-a.json`;
6. `tools/generator-supply/v1/evidence/replay/darwin-b.json`;
7. `tools/generator-supply/v1/evidence/replay/darwin-isolation.json`;
8. `tools/generator-supply/v1/evidence/replay/linux-a.json`;
9. `tools/generator-supply/v1/evidence/replay/linux-b.json`;
10. `tools/generator-supply/v1/evidence/replay/linux-isolation.json`;
11. `tools/generator-supply/v1/evidence/replay/projection.json`;
12. `tools/generator-supply/v1/profile.json`.

No HTTP/P2/provider surface, production database writer, deployment,
publication, or release path was added. The candidate excludes `.idea`,
`.vscode`, `.env`, `node_modules`, and the compiled `migration.test` artifact.

## Fail-closed test repair and superseded replay

The only non-generated candidate edit updates two expected error paths to match
the existing exact-key and field validators:

- a Syft artifact with a missing `purl` first fails exact-key closure at
  `/sbom/darwin-bundle.syft/artifacts/0`;
- a stale OSV scanner version fails at
  `/vulnerability/osvScannerReceipt/version`.

Both assertions continue to require `GENERATOR_SUPPLY_EVIDENCE_MISMATCH`; they
do not relax validation. The first replay projection was therefore correctly
superseded after this projection-included test file changed. All authoritative
Darwin and Linux receipts below bind only the rebuilt projection. The rejected
executor receipt remains historical evidence and is not counted as a replay.

## Authoritative projection and dual-platform replay

The independent graph reviewer rebuilt the projection from the fixed candidate
in a fresh directory with the versioned wrapper. The reconstructed archive and
receipt were byte-identical to the committed authority:

```text
projection tree                 4a70fb8b1e18801f4f02a753668ffe91b63b6275
archive SHA-256                 36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478
archive size                    46008320 bytes
entries                         1505
regular files                   1323
directories                     182
archive member manifest         c5bb19292d8d7c0e966b6b0f08bce2b837b33fec975c24e21fe815a99f0684d1
regular-file manifest           b8567c51fb421740ccac073c479df871d83eb31c3cfbb6cd06362fef4de03ee8
projection receipt file         1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4
```

The TypeScript projection builder and versioned wrapper contain the same 13
late-bound exclusions in the same order. Candidate tree versus projection tree
differs by exactly the 12 existing excluded paths. The thirteenth exclusion is
this independent review record, which did not exist in the fixed candidate and
therefore does not make the candidate self-reviewing.

Darwin A/B and Linux A/B ran on fresh roots and produced these common results:

```text
outputs per run                 48
candidateOutputsEqual           true
nonAllowlistedChanges           0
common output manifest          sha256:dd8c54f5786947f879fbb41c112d686d7abaeeef2f3e505fc0ea73bd31882696
```

Stable A/B fields are equal within both platforms. All four receipts bind the
same projection tree, archive SHA-256, and archive size. They also bind the same
pinned versions of Node.js 24.18.1, Bun 1.3.14, Go 1.26.6, Python 3.14.7, uv
0.12.5, protoc 35.1, protoc-gen-go 1.36.12, and
protoc-gen-connect-go 1.20.0.

The Darwin and Linux isolation receipts bind their corresponding A/B receipts
and the fixed projection. Both report denied network access, read-only input,
archive, dependency, and tool-supply authorities, sanitized runner
environments, and `notGateClosure=true`. Linux additionally records an
independent network/mount/PID boundary, uid/gid 65534, no supplementary groups,
zero capabilities, `no_new_privs`, and `PID_NAMESPACE_KILL_CHILD`. Darwin
truthfully retains its resource-only descendant-lifetime limitation rather than
claiming a complete process-lifetime boundary.

## Generated evidence closure

The replay summary binds the four run receipts, two isolation receipts,
projection receipt, and rejected-executor receipt with no digest mismatch. Its
result is `DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED` and remains
non-Gate evidence.

The independent reviewers verified all 39 files in the evidence manifest by
SHA-256 and size. The embedded profile manifest is semantically identical to
the standalone manifest. The generated domain digests independently recompute
to:

```text
sourceDigest                    sha256:8c2f462e30baefdf420179b66399461a22a0de71efcefca99e1ff3134bd62b3c
artifactSetDigest               sha256:f307e4c73f56e62c5d38b928acff5db284cd5a1706bb76fa7ba55b1437faa0c5
evidenceManifestDigest          sha256:59e7dfe0d85d7fd2cb9ad069037019c484a4c8039c897e5d5cabf45f517f64ab
profileDigest                   sha256:b1201cd3d22398fa808a05190ef4ce49422db665277e8cf8c936938cb5cd741c
registryDigest                  sha256:86452d655cd05a73211e52c28107e93d38a244026648e28a8369cefd4e4eed9c
```

The generation lock binds the current manifest and profile bytes, contains 32
tools and 32 pipelines, and includes the versioned generator-supply profile
generator and pipeline. It remains `BOOTSTRAP_VALIDATED`,
`notGateClosure=true`, and `ALL_GATES_OPEN`. Its unresolved set still includes:

1. `runtime-server-path-and-tenant-authority-enforcement`;
2. `remaining-generator-supply-chain-review`.

The fixed candidate profile and lock intentionally remain
`REPLAY_VERIFIED_REVIEW_PENDING`/`independentReview=PENDING`: this late-bound
review approves their immutable bytes but does not rewrite their claimed state.

## Focused verification accepted by the reviewers

The reviewers accepted these fixed-candidate checks and independently verified
their binding to the reviewed bytes:

```text
targeted repaired assertions    2/2 passed
focused Vitest                  41/41 passed
Python standards tests          10/10 passed
generated profile check         current
generation lock check           current
git diff --check                passed
Gitleaks candidate range        0 findings
```

The final focused Vitest run covered
`scripts/replay-platform-generators.test.ts` and
`scripts/lib/platform-generator-supply-profile.test.ts`. No broad Go migration
suite, broad Bun suite, production database write, reboot/poweroff, deployment,
publication, or Gate-closing action was performed.

## Evidence boundary

This review does not constitute legal approval of the SBOM/NOTICE inventory.
Vulnerability observations are bound to their recorded scanner databases and
timestamps. It does not establish Linux arm64 replay, Darwin descendant
process-lifetime closure, artifact signing, authenticated identity,
publication, production operation, or any external HTTP/P2/provider effect.

The independent verdict is therefore `APPROVE`, with P0/P1/P2 all zero, for the
fixed generator-supply profile candidate only. All Gates remain open.
