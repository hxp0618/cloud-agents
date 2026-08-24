# G-CONTRACT generator-supply profile v2 independent review

Date: 2026-08-25

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

Normalized verdict: `APPROVE_P0_0_P1_0_P2_0`.

This independent fixed-object review approves only the assembled
generator-supply-v2 replay/profile/security candidate below. It is not a Gate
closure, does not approve a detached review-binding output or final consumer,
and does not authorize a production database write, HTTP/P2/provider effect,
deployment, publication, release, main merge, or any Gate transition. The
profile remains `REPLAY_VERIFIED_REVIEW_PENDING`; `G-CONTRACT`,
`G-SUPPLY-CHAIN`, and every aggregate Gate remain `IN PROGRESS`/OPEN.

## Fixed candidate identity

- branch reviewed: `codex/review-supply-v2-slice-f-1ba7eda`;
- parent: `5def3ad5deb157264429dc5178f57ec916c66dc7`;
- candidate: `1ba7eda5ad6241ad8a065408d787e73cd7013ce0`;
- candidate tree: `5a73a8edd4aee56a38aeb37c37b8009e481dfeae`;
- parent-to-candidate full-index binary diff SHA-256:
  `861c5b1a2e434bbdf84618e9f5826a726614e117fd9d7344b2ea0655001b7f61`;
- candidate path count: exactly `11`;
- live `origin/codex/cloud-agents-platform-p0` pointed exactly to the candidate
  during review;
- this review path was absent from the candidate, so the candidate does not
  self-review; and
- the review worktree/index remained clean until this separate review record
  was authored.

The following SHA-256 values are exact file-content digests and sizes are
candidate byte counts.

| Path                                                              | SHA-256                                                            |  Bytes |
| ----------------------------------------------------------------- | ------------------------------------------------------------------ | -----: |
| `contracts/generation.lock.json`                                  | `6050d52e185da0f78618a71215a38ecf978700cc46a010706a15339d76aa6b99` | 16,763 |
| `tools/generator-supply/v2/evidence-manifest.json`                | `da539baf6ab644d49f782d26b3f9ad8e2a12cdab048c3fcf34b756192cd933ba` |  1,688 |
| `tools/generator-supply/v2/evidence/replay.json`                  | `234cafe720cea04d0ab9bd68954f88109bad4070300e882e1563855fd4441c37` |  2,127 |
| `tools/generator-supply/v2/evidence/replay/darwin-a.json`         | `77f530f102f97d9e7afd09c4de224dd1c0b5f7678244db723636c8deffda62a1` |  3,054 |
| `tools/generator-supply/v2/evidence/replay/darwin-b.json`         | `f15584d4f95b5cebae39ef746316b3ad44c7bc47e7a6a0c4240f54e149a6c807` |  3,054 |
| `tools/generator-supply/v2/evidence/replay/darwin-isolation.json` | `856a2a4ef326435a45abfba81c6345f798a34e526c6845b56fe201d05c64339c` |  7,697 |
| `tools/generator-supply/v2/evidence/replay/linux-a.json`          | `178d2de199eb7a6a512f84c4f0c62ef086138b03ca210cd735f52f3fade64837` |  3,055 |
| `tools/generator-supply/v2/evidence/replay/linux-b.json`          | `19ec5a092886ddda687e0483e0cdf06d9503661a71a7f54d5a314a7d4de3513f` |  3,055 |
| `tools/generator-supply/v2/evidence/replay/linux-isolation.json`  | `f71797df9d659b81291e4d6ab2928861d3b276970b9f7dd10ffff0be43842a6f` | 11,459 |
| `tools/generator-supply/v2/evidence/replay/projection.json`       | `095c353f1f284ddcb9e396e393ba82bf2fed3956f9857f0b021283aca9bad234` |  1,903 |
| `tools/generator-supply/v2/profile.json`                          | `e52a6e24e2903ee403abb5b7252f6472437d8cddcb78bcaf8e2699c5ab393252` |  9,362 |

All 11 objects are regular non-symlink files with a final newline. No v1
generator-supply byte, closure-v1/v2 predecessor byte, v2 source/schema byte,
or predecessor review byte differs from the parent. The immutable predecessor
validator independently traversed the fixed Git lineage, six v1 outer files,
all 39 v1 manifest members, and the normalized v1 review verdict.

## Projection and raw replay review

The installed `projection.json` is byte-identical to the formal Slice C
receipt. Its complete nine-tuple is:

| Field                             | Value                                                                             |
| --------------------------------- | --------------------------------------------------------------------------------- |
| projection tree                   | `eb383707ef1a0818cbd09935cc67f417abe8b96f`                                        |
| archive SHA-256 / bytes           | `39e9ca8ab531b08acfbc087259d0a01a52a43f2f6c012aeedb3f6baacb3c4067` / `46,786,560` |
| archive-member algorithm          | `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`               |
| archive-member manifest / members | `bf1a05fe23caa4190cdc614da44b293a10c8b4599230d5c4a5ce7519d0eb1496` / `1,557`      |
| input-tree algorithm              | `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`                               |
| input-tree manifest / files       | `08424e411abb88dc693a4ac07f1a6d2b04599e447601253db9cda01804da1722` / `1,371`      |

All duplicate, unsafe, special, hardlink, symlink, link-prefix-descendant, and
link-cycle safety counters required to be zero are zero. The exact ordered 16
exclusions match the source, DAG, wrapper, and projection receipt.

The installed Darwin and Linux receipts are byte-identical to the formal
Slice D raw outputs. All four run receipts bind the same projection nine-tuple,
the same 49-output candidate/replay manifest
`d0136124f1f760ae60c34e3b0e47161cb528fa3222f3330a440338f6a47da50e`,
`candidateOutputsEqual=true`, and `nonAllowlistedChanges=0`. Darwin A/B and
Linux A/B differ only in the declared run identity/fresh root, and both
platform pairs normalize equal. The derived summary binds the exact four raw
run receipt digests and both isolation-receipt digests.

Every run and isolation receipt binds the same fixed replay authority:

| Authority         | SHA-256                                                            |
| ----------------- | ------------------------------------------------------------------ |
| wrapper           | `b4c0f23c45c2a3a1a391daadcc44554793fda948168f35f3ffaf4d32cedd9070` |
| runner            | `96bc41cd702a35b0c4febfd62c48e0e261fc0656f6f91583522eb47e96cf07a1` |
| path helper       | `4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf` |
| archive inspector | `db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31` |

The isolation claims remain deliberately bounded. Darwin records deny-default
network isolation and negative probes, read-only supply/archive/node_modules,
separate replay process groups, and trusted-parent framing, but does not claim
same-process-group emptiness, cross-run detached-descendant denial, or full
resource lifetime closure. Linux records separate network/mount/PID
namespaces, no default route, read-only projection/input/rootfs/node_modules,
fresh rootfs and tmpfs state, uid/gid 65534 with no supplementary groups or
capabilities, `NoNewPrivs`, PID-namespace child cleanup, and an unreadable
trusted stdout channel. Neither receipt exposes the real executor address or
real temporary paths. Linux arm64 remains `NOT_CLAIMED`.

The formal Slice C and D independent attestations remain external to the
repository. Slice C is 5,094 bytes, mode `0444`, SHA-256
`6b62a2890b4655e68a18ce45538e4cd73a7c83cfbf5c508e8c40bcd5b99938a6`;
Slice D is 8,140 bytes, mode `0444`, SHA-256
`bce4735a51a75e45c0d6492c286ff4f6f3a477919df334c306f56583521c7247`.
The first Linux attempt recorded by the external Slice D attestation is
non-admissible history only: its mode-0700 supply-root permission failure
produced no run receipt, had empty capture, was not mixed into the formal task,
and is not an input to this candidate.

## Assembled profile, evidence, and lock

The writer-installed seven raw receipts are byte-identical to the reviewed
external objects. The canonical derived summary, exact ordered eight-receipt
evidence manifest, and profile all validate against the fixed v2 authority.
The profile is `ASSEMBLED_LATE_BOUND` with
`REPLAY_VERIFIED_REVIEW_PENDING`; it inherits exactly the immutable v1
39-member material and time-bound security evidence while claiming no new
legal approval or signature. Its boundaries are
`REVIEW_PENDING_NOT_CLOSED`, `ALL_GATES_OPEN`, production database/provider/
deployment/release not authorized, HTTP/P2 not implemented, and bootstrap
discovery forbidden.

The successor generation lock is current at
`SUCCESSOR_ASSEMBLED_PRE_REVIEW`. It binds standards-v2, closure-v3, the exact
supply-v2 profile/digests, all 49 current core output records, replay candidate
manifest, and the ordered 16 exclusions. Its detached binding state remains
`PRE_REVIEW_ABSENT`; it retains `notGateClosure=true` and
`gateStatus=ALL_GATES_OPEN`. This review does not create the Slice G tuple or
binding registry and does not derive an effective `missing=[]` view.

## Focused verification

Pinned Bun 1.3.14 and the fixed external dependency supply were used. The
temporary repository-root `node_modules` link was removed by a shell trap; no
dependency was installed and repository-root `node_modules` is absent.

| Check                                                                   | Result                                                    |
| ----------------------------------------------------------------------- | --------------------------------------------------------- |
| Six focused standards/closure-v3/supply-v2/replay/DAG/predecessor files | `78/78 PASS`                                              |
| Successor lock builder, fail-closed drift, and single-path writer tests | `3/3 PASS`; 24 unrelated cases filtered                   |
| Supply v2 production current check                                      | `ASSEMBLED_PROFILE_CURRENT`                               |
| Evidence checker                                                        | immutable v1 current; v2 `ASSEMBLED_PROFILE_CURRENT`      |
| Successor generation-lock current check                                 | `PASS`                                                    |
| Detached binding current check                                          | `PRE_REVIEW_ABSENT`                                       |
| Derived manifest/profile/summary/lock `oxfmt 0.62.0 --check`            | `PASS`                                                    |
| Exact parent-to-candidate `git diff --check`                            | `PASS`                                                    |
| Gitleaks 8.30.1 exact one-commit range                                  | 63,117 bytes scanned; zero findings                       |
| Candidate host/path privacy scan                                        | no real executor address or `/private/tmp`/user-home path |
| Root dependency topology after checks                                   | repository-root `node_modules` absent                     |

For transparency, one diagnostic invocation reused the seven-file pre-replay
list wholesale and returned `91/105 PASS`. The 14 failures are exactly the
legacy tracked-lock static assertions beginning at lines 424, 529, 579, 605,
630, 666, 707, 773, 833, 923, 1017, 1128, 1173, and 1240 of
`scripts/lib/platform-contract-lock.test.ts`. Each directly reads the old
tracked v1 lock shape and expects its `dialects`, `pipelines`, or `tools`
inventory. They are not successor-builder failures and are not admissible
post-Slice-E checks: ADR-0029 declares `contracts/generation.lock.json` an exact
late-bound Slice E output; the reviewed Slice B authority deliberately kept
the legacy lock unchanged while the successor writer/checker was dormant; and
the R1 review-child freezes every non-exact16 byte through Slice E, so this
candidate could replace the exact lock but could not rewrite the historical
test file. The three predeclared successor-lock tests and the production
`--check-successor` path passed against the actual v2 bytes. This diagnostic is
therefore retained as an explicit scope distinction, not reported as
`105/105 PASS` and not concealed as a broad-suite result.

The raw replay receipts were not reformatted: preserving their external bytes
is the reviewed authority, and all seven installed raw files compare byte for
byte with formal C/D. No native replay, assembly writer, SSH, Go/migration
test, broad Bun suite, database, HTTP/P2/provider, deployment, publication,
release, main merge, or Gate action was executed by this review.

## Progression boundary

The assembled generator-supply-v2 fixed object is approved for its predeclared
Slice F review tuple. This review alone does not satisfy the pending closure-v3
criterion. Slice G may consume only this exact path/SHA/verdict tuple together
with the separately approved closure-v3 review tuple through the dormant
detached binding state machine. Slice H must independently review the actual
Slice G registry/lock bytes before any final candidate classification. Every
Gate remains OPEN.
