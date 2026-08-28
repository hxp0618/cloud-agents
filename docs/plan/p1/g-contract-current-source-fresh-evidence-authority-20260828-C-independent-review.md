# D-053-FRESH-20260828-C independent read-only review

Date: 2026-08-28 Asia/Shanghai  
Review type: independent, fixed-object, read-only review  
Authority: `D-053-FRESH-20260828-C`

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This review accepts only the versioned current-source authority/reset and its
deterministic Slice C projection receipt. It does not authorize native Slice D
replay, profile or lock assembly, canonical/production Runner use, PostgreSQL
or migration writes, HTTP/OIDC/JWKS/P2/provider effects, deployment,
publication, release, or any Gate transition. `notGateClosure=true`,
`gateStatus=ALL_GATES_OPEN`, and `closureDecision=NONE` remain mandatory.

## Fixed lineage and candidate binding

The review was performed from fixed Git objects and a clean read-only checkout;
the review record was absent from the reviewed candidate, so the result is not
self-referential.

| item | identity |
| --- | --- |
| P0 baseline | `32f430ae8e3b8d4b5c8c8263ae63979927182280`, tree `74c964a25b7507cd3ca2169e4b781b128b74aacf` |
| reset commit | `c8829058f56914de807a22debef1ebe9a977e147`, parent P0, tree `87c1403cf0a8d70fb67467ef64d25ee1733d6d61` |
| same-revision authority repair | `f263cd789bbb819e3a6af9cfff85476bb8b78f6b`, parent reset, tree `a645c3750d4ab7f19e820bd4379bfce7c20d5090` |
| reviewed projection candidate | `fa0a687729d62e2e69f7c7923f1e3d3d430f19a8`, parent `f263cd789bbb819e3a6af9cfff85476bb8b78f6b`, tree `f29bebbefd8f8a4e2bd09eee5191f83059f3bde6` |
| candidate-parent binary diff SHA-256 | `sha256:762f8069b312496e73268e80439c8f536fcebafe6aa5badd3a18bf6962a7a6e3` |
| candidate-parent name/status SHA-256 | `sha256:27a08dee181d9e3641484801f3bcd1111d9f8a3390af9ade5b53d9601a33a1bd` |
| P0-to-candidate binary diff SHA-256 | `sha256:64f06629dd74d12e6416c95b63a37635fc20f6340ffc12ccda5b17787f72084d` |
| P0-to-candidate name/status SHA-256 | `sha256:09b41122707c5ec209b2b5805668965b7e45fed7188af1a841398de73dc3e7fb` |

The reset commit changes exactly the authority's declared 17 late-bound paths
(the lock is restored to its fixed v2 predecessor and the other 16 are
removed) plus the new C authority document. The same-revision repair changes
only that new authority document. The projection child changes only that
document's explicit historical-lock clarification and the one projection
receipt path. No unrelated, SQL/catalog, source/schema, generated, or review
bytes were changed.

## Authority and immutable-byte checks

The C document independently matches the following frozen identities:

- source `tools/generator-supply/v3/source.json`: mode `100644`, 40,076 bytes,
  Git blob `abd8cd178582acb538d095ace43f914b698804d3`, SHA-256
  `sha256:e483a297c20149f34d1a3ad0efc8446a131d3553af114ec319c13a6a3949cfc1`;
- source schema: mode `100644`, 24,929 bytes, Git blob
  `2e12dc8464325d7a48caa5fbb9d8cf33c33f7d4d`, SHA-256
  `sha256:13c11ffd9c6c8628d59f046ac678b6341f5ea5e694d9a8eefff3f9cd48211464`;
- output schema: mode `100644`, 3,772 bytes, Git blob
  `e19c46819c1898b96345ff50bb327ee0a6b71217`, SHA-256
  `sha256:0b500db662990bc80e3cbaef2063ae9c1e72030f0111957803d8315959eb7e57`.

The EXACT17 exclusion order in the authority, wrapper, and projection metadata
is identical. All 17 parent-side blob IDs, raw SHA-256 values, and byte sizes
in the authority preservation table were independently re-hashed. The
candidate contains no historical collision: 15 old receipt/review paths are
absent, `projection.json` is the sole new receipt, and the lock is the exact
historical v2 anchor:

`contracts/generation.lock.json` mode `100644`, 17,377 bytes, Git blob
`39ee20e035d8770340d46a8663633c6519830de1`, SHA-256
`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`.

The v2 lock is explicitly documented as an immutable predecessor anchor, not a
C selector or current replay authority. The complete C input set is the
exhaustive candidate staged tree after EXACT17 removal, restricted to regular
Git modes `100644`/`100755`; it contains 1,626 regular files and includes the
approved D-054, D-055, D-056, and D-057 descendants.

Runner/toolchain authority also matches the C document: wrapper
`f58db5b2f4a7558b125253e1a4ea1003835ae864` (mode `100755`, 84,116 bytes,
`sha256:9acfc4163fead4dace517c069b8b0e74aaacc859e8cdd2dee17b84182d0be990`),
runner `146ab31141a97d583a9e2b91a9db042e64451f9e` (mode `100644`, 41,535
bytes, `sha256:759c3b6578d3ec5c818c9ce5bc92b2d560727363e32c833494823a296ef555fd`),
path helper `4a99b1bff5b6a3b827f88b0eb24223f498fe8a05` (mode `100644`, 4,282
bytes, `sha256:4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf`),
and inspector `b1d276d00494def1f303b4bf33e1414eda6ca350` (mode `100755`,
10,860 bytes, `sha256:db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31`).
Declared toolchain/platform claims are Node `24.18.1`, Bun `1.3.14`, Python
`3.14.7`, uv `0.12.5`, protoc `35.1`, protoc-gen-go `1.36.12`,
protoc-gen-connect-go `1.20.0`, and only `darwin-arm64`/`linux-amd64` (Linux
arm64 `NOT_CLAIMED`).

## Slice C projection evidence

The fixed receipt is
`tools/generator-supply/v3/evidence/replay/projection.json` (mode `100644`,
1,999 bytes, SHA-256
`sha256:b9ffeb287ba1ae9e9fedff6f5a7555b323a73cce67dfd99eaecbf910bb0d888a`).
It binds:

| projection fact | value |
| --- | --- |
| projected Git tree | `21cc7f741262f1e3b5059a2457772cc49bf31888` |
| archive | `sha256:7e9d44d5e288a98e0572cadfebe8ae4ca898d051b02947e82a4a260f21f2500c`, 50,708,480 bytes |
| archive entries | 1,842 total / 216 directories / 1,626 regular files |
| symlink/hardlink/special/unsafe/duplicate entries | `0 / 0 / 0 / 0 / 0` |
| archive member-manifest | `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`, SHA-256 `7994311a83fc541d8c9c1064b10f0fea94a7a460c29247ef9e14b32f3dafc7e5` |
| regular/input-tree manifest | `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`, SHA-256 `3b2e7eaefb3f51e35f9fbf9c82d25dd727b19578ab130bd4aa9efb5d3b06f6f9` |

Two fresh canonical invocations of
`replay-platform-generators-isolated-v3.sh build-projection` from the fixed
candidate produced byte-identical archive and metadata. A separate trusted
`inspect-generator-replay-archive.py core-projection` invocation reconstructed
the exact projected tree and matched both manifest digests. Temporary archives
and indexes remained outside the repository; no external service was contacted.

## Focused checks and findings

Passed checks:

- focused v3 replay/profile/predecessor/path-authority suites: 5 files, 51/51
  tests;
- wrapper authority suite: 2/2 tests;
- adversarial archive inspector: 5 tests passed, 1 explicitly unprovided-rootfs
  case skipped;
- exact authority/wrapper/EXACT17/tree/diff checker: pass;
- `git diff --check`: pass; candidate worktree clean.

The review found no P0, P1, or P2 issue. The one same-revision authority repair
was accepted before this review; no further repair or r3/r4 was created.

## Lineage fence and progression

This approval is limited to candidate `fa0a687729d62e2e69f7c7923f1e3d3d430f19a8`
and this append-only review child. It preserves the B authority, all prior
D-053-EC-2/r1/r2 source/profile/schema/manifest/SQL/catalog/archive/review
objects, and all unrelated P0 history. The next permitted step is the already
approved Slice D native replay against this exact projection; no C/D lock or
profile assembly may occur before that step. All production, external-effect,
deployment, publication, release, history-rewrite, and Gate actions remain
forbidden.
