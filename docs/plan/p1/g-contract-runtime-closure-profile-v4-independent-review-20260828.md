# D-053 runtime closure profile v4 independent review

Date: 2026-08-28 (Asia/Shanghai)  
Review type: independent, fixed-object, read-only review  
Authority: `D-053-EC-2.r4` / `D-053-RUNTIME-CLOSURE-20260828-V4`

## Verdict

`APPROVE` — P0=0 / P1=0 / P2=0.

This review covers the repaired v4 candidate and does not authorize a
canonical or production Runner, database or migration writes, HTTP/P2/provider
effects, deployment, publication, release, Gate transition, force-push,
history rewrite, reflog expiry, GC, or prune.  `notGateClosure=true` and
`gateStatus=ALL_GATES_OPEN` remain in force.

## Fixed candidate lineage

The review target is the single-parent commit
`700bc72fe641fef5a473fb0ed55977ab0b67888f`, tree
`301983f5d5977aaae6178c61b59086e83c89ad0f`, with direct parent
`ac06d9535d18f5bc3e6bd90182f0bd0e38701f07`.  Independent recomputation of
the parent-to-candidate binary diff gives
`sha256:b1c2779132bd66432e4fde01560ba267a9470294ca88de09ae412f8f8806c0be`.
The candidate is the one permitted in-scope repair successor; no new r3/r4
candidate was introduced.

The preceding implementation/authority lineage is preserved:

* P0 `6ff645bbea150602226dc0cb727d21579a54f0a7` → implementation
  `c571e81b5e265644d75c5d97a522f022d22898c5` → authority-lineage child
  `41ffbc665d51e29aa988de6be1d01bb29954f149` → repaired candidate above.
* The c571 implementation has tree
  `ca22a345bfba0ab9c9562a01fd9b98aec442a9cc`; the 41ff authority child has
  tree `1b00a41b907f1a7f586d8b9f65ee491aeb59c6c5`.
* The repair changes only the v4 source/output/schema/test implementation
  bytes, authority evidence binding, selector freezes, and the restored
  reviewed runtime-pair document; no v3 predecessor path is changed.

## Immutable predecessor and authority checks

All five v3 predecessor objects independently match their declared mode,
Git blob, raw SHA-256, and byte count, and match the fixed baseline commit
`16275f6cbf390c343a9ac00f9193e75eaad0094e` / tree
`ca595b8e1258a8b78c4da3a545b2a31d8f62b531`.  The exact v3 paths are byte
identical to that baseline and to P0.

The runtime closed pair is unchanged and independently verified:

* candidate `b79d01028c652d004e67a00fdcbdf204e04dc946`, tree
  `289c7c2ff7ab39b0af1ea0bac84a902d461de8dc`, parent
  `4ee0e847a7c8e6d0c7313f0f359acc7002ec9d97`, diff
  `sha256:e967207e24167e8461fbffbbc98df41103e06eacc508f1bc9baca289433b639c`;
* review child `62da35c546b3a53659315b6873e6dadbe29fb2d3`, tree
  `d77b068399b42e13fbf0f0337f0fc94f49556dbb`, parent b79d, diff
  `sha256:8dda3ca9418ecb504e0019ae440bf8d38d940500c96be53cfff6185649ab2289`;
* reviewed runtime review blob `9d20b017639114b827545170be54156abba9e3fd`,
  raw SHA `sha256:46bd55af8d0bb6983062cba7c104fd6432785adbf7db24b046a92e4b39b4fcd6`,
  5,030 bytes, and the two module files match their fixed 672/3,634-byte
  raw digests.

The v4 authority markdown binding is also exact: path
`docs/plan/p1/g-contract-runtime-closure-profile-v4-authority-20260828.md`,
mode `100644`, Git blob `307dfbbe6a9f2f6696ae435996b447d595c1f9ac`, raw
SHA `sha256:0a61c6d3029d595d2ea8a573b1130d77d0756e0b1739a030d52f7f6b8eac2e9a`,
16,991 bytes, commit 41ff, tree
`1b00a41b907f1a7f586d8b9f65ee491aeb59c6c5`.

## v4 source/profile and replay authority

The v4 source and generated registry both validate against their strict
2020-12 schemas.  Recomputed domain digests match the declared values:

* source `sha256:bd3208c3b341c9f7ba358a501b2081c12aeb1a80c9842df089ef0d7e882eb523`;
* profile `sha256:3f9e0ce4fc3e83f445b0bfbf16a70d1c0c8491fd8d1ff54b92635cab01de500b`;
* registry `sha256:4324e5bb0850f5f2bded2c192b79c6f0033db8469e6daa9ff50ca5d03f84a8c`.

The replay authority retains the fixed C projection archive/tree and
manifest algorithms, all 17 exclusions, 49 core output paths, eight ordered
receipt paths, pinned toolchain/platform declarations, single-parent/no-self-
reference lineage fence, and one-repair/P2-defer review rules.  The source
reports one missing supply-chain criterion, `notGateClosure=true`, and
`ALL_GATES_OPEN`; complete-ledger remains a no-op and entry/recovery writers
remain `NOT_IMPLEMENTED`.

## Checks

From an independent temporary worktree at the exact candidate:

* `bunx vitest run scripts/lib/platform-contract-closure-profile-v4.test.ts --reporter=dot` — **18/18 passed**;
* `bun scripts/generate-platform-contract-closure-profile-v4.ts --check-source` — **passed**;
* `bun scripts/generate-platform-contract-closure-profile-v4.ts --check` — **passed**;
* v3 predecessor paths, fixed baseline tree/blobs, candidate/review commit
  types, parents, trees, and binary diffs were independently recomputed;
* negative checks cover authority/runtime selector mutation, authority byte
  drift, v3 byte/path/symlink substitution, source/output formatting,
  symlink, digest, foreign-path, self-reference, and manual-missing drift.

No native replay, production database, HTTP/P2/provider, SSH, deployment,
publication, release, or Gate operation was run.  The v4 runner/receipt paths
remain declared closure-only inputs with no native success claim.

