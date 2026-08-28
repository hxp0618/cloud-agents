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

## Exact reset set and historical predecessor lock

The reset performs explicit `git rm` on exactly the following ordered paths;
the lock is then restored byte-for-byte from the fixed post-H v2 predecessor
`39ee20e035d8770340d46a8663633c6519830de1` (SHA-256
`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`,
17,377 bytes):

This restored v2 lock is a historical, immutable predecessor anchor only. Its
v2 `sourceContract` and `generatorSupply` references are not the C selector,
do not authorize reading or writing any v2/v3 profile, and must not be treated
as current replay evidence. The C selector is the exact current parent/tree,
source/schema bindings, and the EXACT17 reset list below; a future C/D lock may
be assembled only in its separately authorized late-bound step.

Because the authority record is itself part of the reset child, it cannot
embed that child's final self-referential commit/tree. The fixed projection
candidate is therefore the single-parent child produced after the reset, and
the independent review must record and verify its exact commit, parent, tree,
and parent-to-candidate path diff before accepting the projection receipt.

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

The parent-side objects removed by this reset are preserved by their original
Git blob, raw SHA-256, and byte size (the lock is the sole successor-restored
object):

| path | parent blob | bytes | raw SHA-256 |
| --- | --- | ---: | --- |
| `contracts/generation.lock.json` | `e762161fced4bb7ad8a64f8ba4802a475cad2c82` | 5,979 | `sha256:25ef7da81692c46c40c6f97fa4bfb69976063a68c1cfae47a8176fbdc547609e` |
| `tools/generator-supply/v3/evidence-manifest.json` | `6e3c82febc536afc9494fabcb244823f99a5bf2a` | 1,688 | `sha256:545d414fd29aa796b0b9c54b3af1ea7842830e2d07cfd1b6ee8bed3b9c542d25` |
| `tools/generator-supply/v3/profile.json` | `ded7684a049d966a4ca66d48cfd2d27852388867` | 5,909 | `sha256:3bf98cea12c3ba068dd64fff1d6181eb3d974c9dcfc5a9b0bbe5437e15366297` |
| `tools/generator-supply/v3/evidence/replay.json` | `2122eac6c5aa758c07d8c7ba463b57bca969e5e4` | 2,127 | `sha256:b108e4e58f345eb12b5322cd109964d98aabe80ab54c98564d30673a0d0a70db` |
| `tools/generator-supply/v3/evidence/replay/darwin-a.json` | `f3b77ed6b290c0f9a73e4131291075267d9ae003` | 3,054 | `sha256:97bd491413d777012f435a85ae0b4b829dcb88a4af9051b65786cd0b50c60611` |
| `tools/generator-supply/v3/evidence/replay/darwin-b.json` | `82046103e6fa3c2f32de73d6a156b21c525a769f` | 3,054 | `sha256:7a88412f9f012aa027f05e1217ed5eca08328d813d5e2fea0d6fa72ff2a71283` |
| `tools/generator-supply/v3/evidence/replay/darwin-isolation.json` | `86ce771a950a25cb1c2d55c53fb3acc1bae10c63` | 7,697 | `sha256:4937e597b09aa6c7a04f12881fc2db2434362581cad5b7fd91e6c486a8a3db01` |
| `tools/generator-supply/v3/evidence/replay/linux-a.json` | `28d1b626bd10e1705de5e9db7093fac96139568e` | 3,055 | `sha256:132c22f8f29831e2f62541cf64117a94bacfe683c0a862364b3c76ece5805b56` |
| `tools/generator-supply/v3/evidence/replay/linux-b.json` | `34fedeb684dfaa9769ba3f043c696c68ad6b6d76` | 3,055 | `sha256:dc0116d8dd44aad7d6d49c6b1ae2a32fb7bb33ecc5d55975beba9294ce72c313` |
| `tools/generator-supply/v3/evidence/replay/linux-isolation.json` | `ade64d29bf5b0a713719ef75192cbc5262a81d15` | 11,459 | `sha256:2983f98f929f8d99c7f3aed28a14a971f08ac839c22528cc1b448b58ec45bc95` |
| `tools/generator-supply/v3/evidence/replay/projection.json` | `b89bc8924ca7e65fde8a15d927670daa05de9dc7` | 1,999 | `sha256:0d53dcfa2efbc0bc6f44bf35a3f7819bcde28099be79836e2b7c03cde5f01841` |
| `docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md` | `c66cd2ffe97695bbfbf7addf5494f440f2ad8494` | 6,371 | `sha256:c7f2bf258c0dc2f06bb43940ef323aa77f01e351b9fedf92c344d3000b423cbb` |
| `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md` | `41ad944fd3b06840010245f3f76f00fc1986b52d` | 8,318 | `sha256:7e17fb12a8e78870a1cbcf1b17809cb019cc5fe18cb68a0202311ee614875492` |
| `docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md` | `e2c8b82329d34fc6e6cf191be47dc759fc8b07bf` | 5,722 | `sha256:2046666a570481512178fa721c374a11661014bb1bcd5d97fb0a24b3b9f7c3c9` |
| `tools/gate-phase-record/g-contract-p1/v1/review-tuple.json` | `172973ba0bf311d3b52e6d6d0ddaeceb55c05ce9` | 3,025 | `sha256:be8f6a351684a51fa2ddfb513d020905dbe9d5a6352df32e39b4f149c8360569` |
| `tools/gate-phase-record/g-contract-p1/v1/registry.json` | `5338bcfd60269e8db4d944697004735ebd70bfb3` | 2,143 | `sha256:3785cde9869520207bb41a834969367614c48f6a09a93e3d2f1f53822743a5dd` |
| `docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md` | `d88e8a258fb9652f971b1385aefd8d1a91870e17` | 4,180 | `sha256:27fa455ae103c41cbb42015376f3e72b995ac53190ebae67874ef2a4a46bff29` |

## Projection, archive, and input identity algorithms

Slice C must invoke
`scripts/replay-platform-generators-isolated-v3.sh build-projection` against a
clean staged tree. The archive/member-manifest algorithm is
`utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`; the
regular/input-tree manifest algorithm is
`utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`; node_modules closure uses
`utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1`.
The complete projection input set is the exhaustive set of paths in the reset
candidate's staged tree after removing EXACT17 above, restricted to regular
`100644` or `100755` files; D-054, D-055, D-056, and D-057 descendants are
therefore included. The final projection tree and member manifest must
enumerate every such path.
Every member is bound to canonical path, regular mode, byte size, Git blob and
SHA-256. Archive bytes, projection tree, and manifest are deterministic and
must be independently recomputed before acceptance.

## Runner, helper, inspector, toolchain, and platforms

The sole replay runner is `scripts/replay-platform-generators-v3.ts` (mode
`100644`, 41,535 bytes, Git blob `146ab31141a97d583a9e2b91a9db042e64451f9e`,
SHA-256 `sha256:759c3b6578d3ec5c818c9ce5bc92b2d560727363e32c833494823a296ef555fd`),
wrapped by policy `VERSIONED_ISOLATION_WRAPPER_V3` at
`scripts/replay-platform-generators-isolated-v3.sh` (mode `100755`, 84,116
bytes, Git blob `f58db5b2f4a7558b125253e1a4ea1003835ae864`, SHA-256
`sha256:9acfc4163fead4dace517c069b8b0e74aaacc859e8cdd2dee17b84182d0be990`).
The path helper is `scripts/lib/generator-replay-path-authority.ts` (mode
`100644`, 4,282 bytes, Git blob `4a99b1bff5b6a3b827f88b0eb24223f498fe8a05`,
SHA-256 `sha256:4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf`),
and the trusted archive inspector is
`scripts/lib/inspect-generator-replay-archive.py` (mode `100755`, 10,860 bytes,
Git blob `b1d276d00494def1f303b4bf33e1414eda6ca350`, SHA-256
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
