# D-053 generator-supply v3 Slice F independent review

Date: 2026-08-28 Asia/Shanghai  
Authority: `D-053-FRESH-20260828-B`  
Review: fixed-object, independent, read-only

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This review approves only the fixed Slice E assembled profile, evidence
manifest, and v3 generation lock. It authorizes no canonical/production
Runner, production database or migration write, HTTP/OIDC/JWKS/P2/provider
effect, deployment, publication, release, force-push, history rewrite, or
Gate transition. `notGateClosure=true`, `gateStatus=ALL_GATES_OPEN`, and
`closureDecision=NONE` remain mandatory.

## Candidate and predecessor binding

The reviewed pre-review candidate is commit
`94cbb23127a6a6c1ca31398d731d99b54cac80f9`, tree
`052d5d1a994349c42766bf9dafc292088add1a90`, parent
`25b3a47a185db2151ca4ba6e1916811cba6e155e`; parent-to-candidate
`git diff --no-ext-diff` SHA-256 is
`97693241f4d30a55c15034122d20109c908866cddb18e0da52aac7bbef647a08`.
That candidate diff changes exactly `contracts/generation.lock.json` and has
no review record, so this review is not self-referential; `git diff --check`
is clean. The review child is a separate single-parent commit and adds only
this predeclared review path.

The v3 lock is an exact successor of immutable v2 commit
`16275f6cbf390c343a9ac00f9193e75eaad0094e`, tree
`ca595b8e1258a8b78c4da3a545b2a31d8f62b531`, Git blob
`39ee20e035d8770340d46a8663633c6519830de1`, SHA-256
`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`.
The assembled lock is `cloud-agents-platform-contract-generation-lock/v3`,
`state=ASSEMBLED`, with lock digest
`sha256:2429a379bdd7cfb2f5cadf27ba40db3af081475184246418ea6c7e7176d6cba0`.

## Frozen source, projection, and algorithms

The authority binds canonical source/schema files by regular `100644` mode,
size, Git blob and SHA-256: source blob
`abd8cd178582acb538d095ace43f914b698804d3` /
`sha256:e483a297c20149f34d1a3ad0efc8446a131d3553af114ec319c13a6a3949cfc1`
(40,076 bytes), source-schema blob
`2e12dc8464325d7a48caa5fbb9d8cf33c33f7d4d` /
`sha256:13c11ffd9c6c8628d59f046ac678b6341f5ea5e694d9a8eefff3f9cd48211464`
(24,929 bytes), and output-schema blob
`e19c46819c1898b96345ff50bb327ee0a6b71217` /
`sha256:0b500db662990bc80e3cbaef2063ae9c1e72030f0111957803d8315959eb7e57`
(3,772 bytes). The exact ordered 17 exclusions are frozen in the authority
and projection receipt; wildcard, alias, duplicate, symlink, special-file,
or untracked inputs are inadmissible.

Projection tree is `d91447364745af16314f348d550b358f995fad0b`; archive SHA-256
is `sha256:395f64058dbdae27ba0897861c39264ca3da36deca342ec1f98f5173c67b777b`
(50,083,840 bytes), with 1,775 members and 1,569 regular files. The archive
algorithm is `utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1`;
the input-tree algorithm is `utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`.
Archive inspection records zero symlinks, hardlinks, special, duplicate,
unsafe, and cyclic entries.

## Replay and assembled profile

The sole runner is `scripts/replay-platform-generators-v3.ts` under
`VERSIONED_ISOLATION_WRAPPER_V3`; committed authority SHA-256 values are
wrapper `sha256:9acfc4163fead4dace517c069b8b0e74aaacc859e8cdd2dee17b84182d0be990`,
runner `sha256:759c3b6578d3ec5c818c9ce5bc92b2d560727363e32c833494823a296ef555fd`,
path helper `sha256:4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf`,
and archive inspector
`sha256:db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31`.
Only native `darwin-arm64` and `linux-amd64` are claimed; `linux-arm64` is
`NOT_CLAIMED`. Receipts record Node 24.18.1, Bun 1.3.14, Python 3.14.7,
uv 0.12.5, protoc 35.1, protoc-gen-go 1.36.12, protoc-gen-connect-go
1.20.0, and Go 1.26.6.

Every A/B receipt binds candidate manifest
`sha256:bedb5d26301f627393a107afda9863899dae0909793ae7df8d0ad06018a282e`,
the projection above, 49 outputs, `candidateOutputsEqual=true`, and
`nonAllowlistedChanges=0`:

| receipt | SHA-256 |
| --- | --- |
| Darwin A | `sha256:97bd491413d777012f435a85ae0b4b829dcb88a4af9051b65786cd0b50c60611` |
| Darwin B | `sha256:7a88412f9f012aa027f05e1217ed5eca08328d813d5e2fea0d6fa72ff2a71283` |
| Darwin isolation | `sha256:4937e597b09aa6c7a04f12881fc2db2434362581cad5b7fd91e6c486a8a3db01` |
| Linux A | `sha256:132c22f8f29831e2f62541cf64117a94bacfe683c0a862364b3c76ece5805b56` |
| Linux B | `sha256:dc0116d8dd44aad7d6d49c6b1ae2a32fb7bb33ecc5d55975beba9294ce72c313` |
| Linux isolation | `sha256:2983f98f929f8d99c7f3aed28a14a971f08ac839c22528cc1b448b58ec45bc95` |
| Projection | `sha256:0d53dcfa2efbc0bc6f44bf35a3f7819bcde28099be79836e2b7c03cde5f01841` |
| Replay summary | `sha256:b108e4e58f345eb12b5322cd109964d98aabe80ab54c98564d30673a0d0a70db` |

Linux isolation records separate network/mount/PID namespaces, read-only
rootfs and authority binds, uid/gid 65534, zero capabilities, NoNewPrivs=1,
denied network probes, and no trusted-parent evidence access. Darwin records
equivalent sandbox/network denial. No executor address or temporary host path
is treated as authority.

The assembled registry/profile/evidence-manifest digests are respectively
`sha256:f9b3e97b2d7d23b53984c41324fba18284f23c067c7858de3b26c02fc6133c77`,
`sha256:998e8873a66139235ebb111ec27628412dd41dbba956fcefb9fd0fe8918c2b7d`,
and `sha256:d383daaa1257ef16239fab8d88b854162531562bbadac844b625b8ddae22b60f`.
The profile (`sha256:3bf98cea12c3ba068dd64fff1d6181eb3d974c9dcfc5a9b0bbe5437e15366297`)
and evidence manifest (`sha256:545d414fd29aa796b0b9c54b3af1ea7842830e2d07cfd1b6ee8bed3b9c542d25`)
are regular `100644` files. Profile status remains
`REPLAY_VERIFIED_REVIEW_PENDING`; this independent review does not rewrite
profile bytes.

## Checks

Observed without external side effects:

- `generate-platform-generator-supply-profile-v3.ts --check-source` ->
  `ASSEMBLED_PROFILE_CURRENT`;
- `--check-assembly` -> `ASSEMBLED_PROFILE_CURRENT`;
- `generate-platform-contract-lock-v3.ts --check` -> `ASSEMBLED`;
- `--check-assembled` -> `ASSEMBLED current`;
- focused v3 profile/replay/lock/phase-state/replay-path suite -> 5 files,
  42 tests passed;
- exact SHA-256 and `git diff --check` checks matched the bindings above.

No production, HTTP/P2/provider, deployment, release, or Gate operation was
performed. Only the pre-approved next code-bearing slice may consume this
fixed review; all Gates remain open.

