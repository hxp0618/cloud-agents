# D-053 superseding runtime closure authority v4

Date: 2026-08-28 (Asia/Shanghai)
Authority: `D-053-EC-2.r4` / `D-053-RUNTIME-CLOSURE-20260828-V4`
Status: `IMPLEMENTED_PENDING_INDEPENDENT_REVIEW`

## Decision and scope

This record authorizes one additive, versioned successor for the stale
runtime criterion in the generated contract-closure profile.  It supersedes
the runtime binding only by lineage.  It does not rewrite, replace, or
re-identify any v1, v2, or v3 source, schema, generated profile, manifest,
SQL/catalog, archive, receipt, or review byte.

The successor is local and read-only.  It may add v4 source/schema/profile,
versioned generator-supply metadata, a versioned replay runner/wrapper, and
their review records.  It may not mutate the v3 runner or v3 predecessor
checker in place.  The complete-ledger consumer remains a no-op;
entry/recovery writers remain `NOT_IMPLEMENTED`.

This authority does not authorize a canonical or production Runner,
PostgreSQL or migration writes, HTTP/OIDC/JWKS/P2/provider/workload or
credential effects, SSH or hardware power actions, deployment, publication,
release, force-push, history rewrite, reflog expiry, GC/prune, or a Gate
transition.  `notGateClosure=true`, `gateStatus=ALL_GATES_OPEN`, and
`closureDecision=NONE` are mandatory in every v4 generated object and
receipt.

## Immutable v3 predecessor fence

The v4 successor must preserve the following v3 baseline and closure objects
byte-for-byte.  Any mismatch is a hard failure, not a repair opportunity:

| object | mode | Git blob | raw SHA-256 | bytes |
| --- | --- | --- | --- | ---: |
| `contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json` | `100644` | `d714424ac6b42a44ee775a6edde6327d87f2d7c3` | `e8384fb25f3828dfafeecf0040110df3a51cd64ce5877e966ecec12769099bf4` | 14,215 |
| `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json` | `100644` | `58f651367aea31c5662423b602bf293d085a8afa` | `face6b9f01732255d4f3ae3aebb040d0af19efae416bad074a2f84510e385862` | 13,451 |
| `contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json` | `100644` | `eb2c46f916ac52b13a6d225685bf48064cf35836` | `3fbc85313f2195860b6211f8c31fc185d825469146f703b7e442c34b0612ed25` | 23,642 |
| `contracts/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json` | `100644` | `ccdef422ab3ef6a61cb2be8ff1e071572cd99374` | `3a98b5558cf7d359e4854a46ab95a1a14fb3cc1298a304954d4033a092f8fcb2` | 2,080 |
| `docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md` | `100644` | `95cd52d4074852f1792620bcac8cf6bf6ffc0853` | `83975f780dbcaed587988155f680c33e3b1a42ee10776af2a3077a5482d13001` | 10,102 |

The v3 predecessor checker is frozen to baseline commit
`16275f6cbf390c343a9ac00f9193e75eaad0094e` (tree
`ca595b8e1258a8b78c4da3a545b2a31d8f62b531`).  V4 must treat that commit,
its fixed Git blobs, and the v3 source rows as historical evidence.  Updating
those rows or changing a v3 constant would invalidate the v3 authority and
is prohibited.

## Runtime closed pair carried into v4

The current runtime module identity is bound by a closed pair, not by a
caller-selected ref:

| item | frozen identity |
| --- | --- |
| candidate commit | `b79d01028c652d004e67a00fdcbdf204e04dc946` |
| candidate tree | `289c7c2ff7ab39b0af1ea0bac84a902d461de8dc` |
| candidate parent | `4ee0e847a7c8e6d0c7313f0f359acc7002ec9d97` |
| candidate→parent binary diff SHA-256 | `sha256:e967207e24167e8461fbffbbc98df41103e06eacc508f1bc9baca289433b639c` |
| dedicated review child | `62da35c546b3a53659315b6873e6dadbe29fb2d3` |
| review tree | `d77b068399b42e13fbf0f0337f0fc94f49556dbb` |
| review parent | `b79d01028c652d004e67a00fdcbdf204e04dc946` |
| review→parent binary diff SHA-256 | `sha256:8dda3ca9418ecb504e0019ae440bf8d38d940500c96be53cfff6185649ab2289` |
| review path | `docs/plan/p1/g-contract-runtime-current-lineage-rebind-independent-review-20260828.md` |
| review Git blob | `9d20b017639114b827545170be54156abba9e3fd` |
| review raw SHA-256 / bytes | `sha256:46bd55af8d0bb6983062cba7c104fd6432785adbf7db24b046a92e4b39b4fcd6` / `5,030` |
| review verdict | `APPROVE` — P0=0 / P1=0 / P2=0 |

The pair carries these current module bytes:

| path | mode | Git blob | raw SHA-256 | bytes |
| --- | --- | --- | --- | ---: |
| `services/control-plane/go.mod` | `100644` | `8e7f87cadf8b6bb283230fcc1b9a1b2466e6ca73` | `sha256:d27871e7d4d8788d455ac2a5b9d512b0b6628903fad05213a9e227c0f0883d3d` | 672 |
| `services/control-plane/go.sum` | `100644` | `e516097c321550eba034aa50d1039a1bd1e81ac0` | `sha256:4b870f580591894010f0762c8d04b83cba95a5c09eabc4ffc2631e41290abfbc` | 3,634 |

The four runtime/tenant-authority paths remain the exact reviewed blobs:

```text
services/control-plane/internal/server/managed_agent_create_project.go        52545f173291f1e2655eb914746d783552a57f06
services/control-plane/internal/server/managed_agent_create_project_test.go   abc1b07a6f02a3ea40a93a11bfbf34a8ed176a46
services/control-plane/internal/authn/runtime_server_external_test.go         2a964d332a01bb0785075dacbe1e8cd28eb41852
services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh
                                                                                f2891ad9eb7de1f7233a9eb03f4e50801bbd7864
```

V4 must reject a path, mode, size, byte digest, Git identity, parent, or
review verdict that differs from this pair.  A recomputed but differently
identified pair is not equivalent evidence.

## Current P0 and C projection predecessor

The current target line is the clean P0 commit
`6ff645bbea150602226dc0cb727d21579a54f0a7`, tree
`24a0198cdf551e7834b3e1ebb924aca4249edcda`, with first parent
`32f430ae8e3b8d4b5c8c8263ae63979927182280` and the C projection side
parent `f73c2cfcf0b420c694ed8013a434c1bf37763180`.  V4 candidate work must
start from this exact P0 line and use a single parent; a merge parent,
working-tree state, or floating branch name is not an authority selector.

The immutable C projection predecessor is candidate
`fa0a687729d62e2e69f7c7923f1e3d3d430f19a8`, tree
`f29bebbefd8f8a4e2bd09eee5191f83059f3bde6`, with reconstructed core
projection tree `21cc7f741262f1e3b5059a2457772cc49bf31888`.  Its archive and
member identities are:

The C source and schema selectors are also immutable inputs:

| path | mode | Git blob | raw SHA-256 | bytes |
| --- | --- | --- | --- | ---: |
| `tools/generator-supply/v3/source.json` | `100644` | `abd8cd178582acb538d095ace43f914b698804d3` | `sha256:e483a297c20149f34d1a3ad0efc8446a131d3553af114ec319c13a6a3949cfc1` | 40,076 |
| `tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json` | `100644` | `2e12dc8464325d7a48caa5fbb9d8cf33c33f7d4d` | `sha256:13c11ffd9c6c8628d59f046ac678b6341f5ea5e694d9a8eefff3f9cd48211464` | 24,929 |
| `tools/generator-supply/v3/generator-supply-profile-v3.schema.json` | `100644` | `e19c46819c1898b96345ff50bb327ee0a6b71217` | `sha256:0b500db662990bc80e3cbaef2063ae9c1e72030f0111957803d8315959eb7e57` | 3,772 |

| fact | frozen value |
| --- | --- |
| archive SHA-256 / bytes | `sha256:7e9d44d5e288a98e0572cadfebe8ae4ca898d051b02947e82a4a260f21f2500c` / `50,708,480` |
| archive/member algorithm | `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1` |
| member manifest SHA-256 | `7994311a83fc541d8c9c1064b10f0fea94a7a460c29247ef9e14b32f3dafc7e5` |
| regular/input-tree algorithm | `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1` |
| regular manifest SHA-256 | `3b2e7eaefb3f51e35f9fbf9c82d25dd727b19578ab130bd4aa9efb5d3b06f6f9` |
| archive entries / regular files | `1,842` / `1,626` |
| symlinks / special / unsafe entries | `0` / `0` / `0` |
| pinned supply root | `/private/tmp/codex-cloud-agents-generator-supply-20260824` |

The C archive is a predecessor receipt, not a v4 success claim.  A v4
candidate that adds files must produce its own archive and manifests with the
same algorithms; it may not overwrite or relabel the C receipt.

## Complete input set and exact exclusions

The v4 projection input is the exhaustive regular-file set in the candidate
staged tree after removing exactly these ordered paths.  Every remaining
entry must be a regular, non-symlink `100644` or `100755` file and must be
bound by canonical path, mode, size, Git blob, and raw SHA-256:

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

No wildcard, pattern, alias, symlink, duplicate, untracked file, or path
outside this list is admissible.  The v4 manifest must report complete
coverage and deterministic archive bytes; omission, addition, ordering drift,
or duplicate members is a hard failure.

## Versioned v4 objects and receipt namespace

The implementation may add only versioned v4 objects.  The following paths
are the authority namespace; their final commit/tree/blob identities are
late-bound by the v4 candidate and must be recorded before review:

```text
contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v4.json
contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v4.schema.json
contracts/platform/v1alpha1/schemas/contract-closure-profile-v4.schema.json
contracts/generated/platform/v1alpha1/contract-closure-profile-v4.json
scripts/lib/platform-contract-closure-profile-v4.ts
scripts/lib/platform-contract-closure-profile-v4.test.ts
scripts/lib/platform-successor-predecessor-v4.ts
tools/generator-supply/v4/source.json
tools/generator-supply/v4/generator-supply-profile-source-v4.schema.json
tools/generator-supply/v4/generator-supply-profile-v4.schema.json
tools/generator-supply/v4/profile.json
tools/generator-supply/v4/evidence-manifest.json
tools/generator-supply/v4/evidence/replay/projection.json
tools/generator-supply/v4/evidence/replay/darwin-a.json
tools/generator-supply/v4/evidence/replay/darwin-b.json
tools/generator-supply/v4/evidence/replay/darwin-isolation.json
tools/generator-supply/v4/evidence/replay/linux-a.json
tools/generator-supply/v4/evidence/replay/linux-b.json
tools/generator-supply/v4/evidence/replay/linux-isolation.json
tools/generator-supply/v4/evidence/replay.json
scripts/replay-platform-generators-v4.ts
scripts/replay-platform-generators-isolated-v4.sh
```

The ordered v4 native receipt set is projection, Darwin A, Darwin B, Darwin
isolation, Linux A, Linux B, Linux isolation, then the aggregate replay
receipt.  Receipts are append-only, single-parent outputs.  A failed,
mismatched, or partially present platform receipt is a hard failure; old v3
receipts are never reused.  Until these v4 bytes exist and pass review, the
v4 authority makes no native replay claim.

The closure-only v4 implementation bytes are fixed for the independent review
as follows (no native replay receipt is implied):

| path | raw SHA-256 | bytes |
| --- | --- | ---: |
| `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v4.json` | `sha256:a87e47ab638bec80f558d3295de7cad99a6952f593d3d68af00f910fc1b38f45` | 23,674 |
| `contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v4.schema.json` | `sha256:c445533e9d750791e7f9df74a586d342005faa246025ac2ccc2bd7d8188bb00b` | 25,130 |
| `contracts/platform/v1alpha1/schemas/contract-closure-profile-v4.schema.json` | `sha256:910e6c214ca7df03eafdcea5a89a0b6dc79ded27b065f136c7b84ac1016e8856` | 2,413 |
| `contracts/generated/platform/v1alpha1/contract-closure-profile-v4.json` | `sha256:e9db4df59f18205ba251349a9a33417ef2fdd1644c8c5d89da1f444845ff28da` | 24,440 |
| `scripts/lib/platform-contract-closure-profile-v4.ts` | `sha256:8de0cd7277b233175db5e088f44f2017861ca8271afed2cdf38432af8acf4d1e` | 53,616 |
| `scripts/lib/platform-contract-closure-profile-v4.test.ts` | `sha256:5e3f61890728cc7f42b9c40d803c9d08557922cfac77137284c0fdedf304b39d` | 23,091 |
| `scripts/generate-platform-contract-closure-profile-v4.ts` | `sha256:3947434bf1a51af9be88a763cfd4f627a35e0f1233581f4f2a499bc1cb34f738` | 1,771 |

## Runner, toolchain, and platform fence

The versioned v4 runner and wrapper must carry forward the v3 fail-closed
policy while being independently hashed and reviewed:

- runner: `scripts/replay-platform-generators-v4.ts`;
- isolation wrapper: `scripts/replay-platform-generators-isolated-v4.sh`;
- helper/inspector: v4 paths must be explicit, regular, non-symlink files;
- trusted executables: `/usr/bin/git` and `/usr/bin/python3`;
- pinned toolchain: Node `24.18.1`, Bun `1.3.14`, Python `3.14.7`, uv
  `0.12.5`, protoc `35.1`, protoc-gen-go `1.36.12`,
  protoc-gen-connect-go `1.20.0`, and the pinned Go profile;
- claimable platforms: `darwin-arm64` and `linux-amd64` only;
  `linux-arm64` remains `NOT_CLAIMED`.

The runner must bind the v4 source/schema/profile digest, exact candidate
commit/tree, exact projection archive/tree/manifests, exact output path set
(the v3 core closure remains 49 outputs), and `nonAllowlistedChanges=0`.
It must refuse caller-selected or foreign paths, symlink substitution,
path/mode/size/byte/digest mismatch, and a self-consistent input with a
different identity.

## Lineage and review rules

The v4 implementation candidate is a new single-parent child of P0
`6ff645bbea150602226dc0cb727d21579a54f0a7` (or a later explicitly recorded
single-parent successor on that same line).  The candidate must record its
exact parent, tree, path diff digest, v4 source/schema/profile blobs, all
receipt blobs, and the immutable v3/C/runtime predecessor references.  Review
bytes cannot be self-referential: the review child must be a direct child of
the candidate and must not be included as an input whose identity it claims.

The fixed closure-only candidate for this review is:

| item | identity |
| --- | --- |
| candidate commit | `c571e81b5e265644d75c5d97a522f022d22898c5` |
| candidate tree | `ca22a345bfba0ab9c9562a01fd9b98aec442a9cc` |
| candidate parent | `6ff645bbea150602226dc0cb727d21579a54f0a7` |
| parent-to-candidate binary diff SHA-256 | `sha256:ecfa2be338c8a4ea71d315efad5cad3651ba43fe22fc07e1c14546c7614e9f57` |

The candidate contains only the nine additive v4 authority/review paths
listed above.  Their Git blobs and raw bytes are independently recomputed by
the review; no v3 path is changed.

One independent, read-only review is required.  The reviewer must independently
recompute:

1. candidate and review commit type, exact trees, direct parents, and binary
   diffs;
2. v3 predecessor fence and the `b79d010 → 62da35c` runtime pair;
3. v4 source/schema/profile JSON against its declared schema and exact
   canonical digests;
4. complete projection input coverage, the exact17 exclusions, archive/member
   and regular manifest algorithms, and all receipt allowlists;
5. runner/wrapper/helper bytes, pinned toolchain, platform claims, 49-output
   closure, and fail-closed negative checks;
6. the no-side-effect boundary and mandatory `notGateClosure` fields.

Only `APPROVE` with P0=0 and P1=0 is acceptable for the v4 continuation.
Within this same v4 candidate, a P0/P1 finding may be repaired once and
re-reviewed; no r3/r4 candidate is created for that same scope.  P2 findings
are recorded and deferred.  A review never closes or advances a Gate.

## Required focused evidence and stop condition

Before any merge to P0, the candidate must pass affected local-migration
checks, generator assembly, v4 closure tests, and focused negative tests for
foreign path, symlink, mode/size/byte/digest drift, caller-selected selector,
and recomputed-but-wrong identity.  A native replay failure remains a
fail-closed evidence fact; it is not bypassed by editing v3 constants or
reusing stale receipts.

The stop condition for this authority is therefore either: (a) the v4
candidate and its one independent review satisfy every item above, with no
external effect and all Gates open; or (b) a recorded P0/P1 failure blocks
the bounded slice.  No production database write, HTTP/P2/provider call,
deployment, publication, release, or Gate closure is implied by a passing
focused check.
