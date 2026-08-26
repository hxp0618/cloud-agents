# Superseding D-053 generator-supply v3 independent review

Date: 2026-08-26

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This is an independent, fixed-object, read-only review of the superseding
D-053 Slice E assembled generator-supply-v3 candidate. It approves the
assembled profile, evidence manifest, and v3 lock bytes only. It does not
authorize a production database write, HTTP/OIDC/JWKS/P2 or provider effect,
deployment, publication, release, merge outside the approved branch, or any
Gate transition. `notGateClosure=true` and `ALL_GATES_OPEN` remain in force.

## Fixed candidate

- candidate commit: `9cf7809`;
- parent: `1eb1e44d440412759c97469f69a1b26f2c59f7e5`;
- candidate tree: `87f6b166ef275d7a9711deb4b29e73383eeeb02b`;
- fixed candidate diff binding:
  `sha256:f06e17d442aae1e82b57865f882a433e4deff3c8c306eda2f8ccfd3aceef45fd`;
- candidate changes exactly `contracts/generation.lock.json` and contains no
  review record, so it cannot self-review.

The v3 lock remains an exact successor of the immutable v2 predecessor:
commit `16275f6cbf390c343a9ac00f9193e75eaad0094e`, tree
`ca595b8e1258a8b78c4da3a545b2a31d8f62b531`, blob
`39ee20e035d8770340d46a8663633c6519830de1`, SHA-256
`sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`
(the complete predecessor byte
identity is checked by the v3 fence).

## Fresh projection and replay

The fixed projection is tree
`1cb0742e2a563d2a07bf1194dbd233611746eca6`, archive
`sha256:f77152ada4c862269bfac2ac28d6cd8278739f886ebad89da3b3c0a1261c9766`
(48,537,600 bytes), with 1,686 archive entries and 1,490 regular files. The
projection receipt is
`sha256:8d2e9acd98f186a24a8c5383626885a8a9a5a7faf3080254d1a46128cdcf2ea1`.
The exact ordered 17 exclusions are unchanged and are bound by the v3 source,
wrapper, receipt, and lock.

The fresh native receipts are byte-fixed as follows:

| receipt | SHA-256 |
| --- | --- |
| Darwin A | `sha256:9fee7891b5a2de94516d7f70361e1a9ea488cb2c2377c87508accf051004fa7d` |
| Darwin B | `sha256:58bd3c0995c3d40e50905f91e2986f35afb08a53ad7e0a423fba77360e53bfed` |
| Darwin isolation | `sha256:4840dcd68648a0764e6a49170bc4e473a183544a4a17038426169358dcb2f651` |
| Linux A | `sha256:e7ecce2c1e69de2f4f4e500fe7e1503230e3906f1968978fdc0f48bd61975898` |
| Linux B | `sha256:6f736579a32f764c389071840921a13b5f1f5e8b247c434219e418ad0ba54f2a` |
| Linux isolation | `sha256:35aa8e3582abd21dc868e2bc9e69e9f328079d08f531d12b4bd485465c32b895` |

Both Darwin and Linux A/B runs bind the same projection and candidate manifest
`sha256:bedb5d26301f627393a107afda9863899dae09097993ae7df8d0ad06018a282e9`,
produce 49 outputs, report `candidateOutputsEqual=true`, and report
`nonAllowlistedChanges=0`. The isolation receipts independently record denied
network access and read-only authority boundaries; Linux additionally records
uid/gid 65534, zero capabilities, and `NoNewPrivs=1`. No real executor address
or temporary host path is part of the committed evidence.

## Assembled authority and checks

The assembled profile binds registry digest
`sha256:011d4af7e2f11ee5ef44781d82569632e3d380d893c00ccaeb52e1afb4bb9759`,
profile digest
`sha256:be89f7d7c3e072d36aeee729597f5454197a9657a8665b43f51979d91abb6d6`,
and candidate manifest above. The profile and evidence-manifest files are
regular, non-symlink files; the lock is `cloud-agents-platform-contract-
generation-lock/v3`, state `ASSEMBLED`, with `notGateClosure=true` and
`gateStatus=ALL_GATES_OPEN`.

Independent checks observed:

- profile source and assembly checks: current;
- v3 lock check: `ASSEMBLED` current;
- focused replay/profile/lock/DAG tests: all pass;
- exact candidate diff: `git diff --check` clean.

The review intentionally does not claim Linux arm64, production operation,
artifact signing, authenticated identity, or any external side effect. It
authorizes only the pre-approved next G-CONTRACT R5 record slice to consume
this exact candidate/review binding; all Gates remain open.
