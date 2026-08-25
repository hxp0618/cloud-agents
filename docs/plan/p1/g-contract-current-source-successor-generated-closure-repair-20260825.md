# G-CONTRACT current-source successor generated-closure repair — 2026-08-25

## Boundary and decision

This is an append-only pre-replay repair record for ADR-0030 / D-053. It does
not rewrite ADR-0030, the reviewed v1/v2 supply history, the Slice C candidate,
or any Gate record. It addresses a deterministic byte-level mismatch found
before Slice D on the current post-H source tree.

The v3 replay contract keeps the same ordered 49 output paths. It now gives the
two byte authorities their explicit meanings:

1. the historical v2 generation-lock Git blob remains the immutable fence for
   the 49 outputs that existed at the fixed post-H baseline;
2. `replayContract.coreGeneratorOutputs` is the current pre-replay authority
   for the 49 bytes that the fresh v3 replay must reproduce.

The distinction is necessary because the current contract tree contains the
approved post-H durable-project/coordination sources. The generators therefore
recompute a current contract manifest and deterministic fan-out. Treating
those current bytes as if they were still the post-H baseline bytes would
fail closed before replay and would make a fresh projection impossible.

## Verified cause

The exact pinned core-generator sequence, run from a clean P0/pre-projection
tree, changed 13 of the 49 output files. The changes are deterministic
propagation of the contract manifest digest from
`97ccd739db755b1fbfaf9166f87c4cd985980d6ec78a1b172bbd65638006413c` to
`f2b1b9e64249fc9f72cceb857073e49957b78c6f3ab0b7f8d2d01b042a821e37`; no
generator order, output path, or semantic contract operation changed.

The 13 paths are:

- `contracts/generated/proto/manifest.json`;
- `sdk/go/gen/common/v1alpha1/identity_generated.go`;
- `sdk/go/gen/common/v1alpha1/json_generated.go`;
- `sdk/go/gen/openapi/v1alpha1/client_generated.go`;
- `sdk/go/gen/platform/v1alpha1/json_generated.go`;
- `sdk/go/generated-manifest.json`;
- `sdk/go/json-generated-manifest.json`;
- `sdk/go/proto-generated-manifest.json`;
- `sdk/typescript/generated-manifest.json`;
- `sdk/typescript/json-generated-manifest.json`;
- `sdk/typescript/proto-generated-manifest.json`;
- `sdk/typescript/src/index.ts`;
- `sdk/typescript/src/platform.ts`.

The remaining 36 core paths are byte-identical to the v3 source records. The
old Slice C projection and its review remain historical/superseded evidence;
they are not reused for the repaired candidate.

## Historical fence and current authority

`contracts/generation.lock.json` remains an exact projection exclusion and is
not changed. Its fixed v2 Git blob at commit
`16275f6cbf390c343a9ac00f9193e75eaad0094e` contains the complete historical
49-file map, including each old SHA-256, size, and ordered path. The v3
predecessor verifier traverses that map and checks every baseline blob, mode,
size, and digest directly from the fixed Git object.

The same verifier separately checks every current v3 output record against a
stable regular file in the candidate tree and the Git blob computed from its
current bytes. This preserves the historical predecessor fence while allowing
the current source projection to carry the regenerated outputs required by a
fresh replay. The path set remains exactly the ADR-0030 49-path authority and
the exact17 exclusion set is unchanged.

No v1/v2 source, profile, receipt, detached binding, generation-lock bytes,
review record, or historical candidate is rewritten. The repair is local,
versioned, and pre-replay only.

## Verification boundary

The repair candidate must pass, with the pinned supply toolchain:

- current v3 source/schema and historical-lock predecessor checks;
- focused predecessor/replay/profile/topology tests;
- the exact generator currentness chain and deterministic diff checks;
- canonical JSON, TypeScript formatting, and candidate secret/diff scans;
- fresh projection reconstruction from the staged candidate tree.

Native Darwin/Linux replay, supply assembly, review binding, production
database writes, HTTP/P2/provider effects, deployment, publication, release,
and Gate transitions remain outside this repair record. A fresh independent
read-only review is required before the repaired projection can enter Slice D.

## Progression

This repair is a superseding pre-replay candidate, not a Gate closure record.
After its fixed-object review returns an explicit P0/P1/P2 verdict, the next
authorized action is to rebuild the v3 projection and continue the ordered
Slice D replay. The original Slice C candidate, its review, and all historical
SHA values remain recoverable.
