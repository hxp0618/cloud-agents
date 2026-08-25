# P1 durable Project-create versioned lineage repair

## Status

`IMPLEMENTATION CANDIDATE — FRESH INDEPENDENT REVIEW PENDING`.

This bounded repair adds a versioned durable Project-create lineage/profile
authority. It does not rewrite the historical v2 generation lock, the
canonical v1 fixture manifest, the v1 registry/profile/source, or migrations
`000001`–`000012`. The implementation is intended for the future append-only
successor authority; it does not claim that the ADR-0030 v3 lock or any Gate is
current or closed.

## Boundary and predecessor

- branch: `codex/cloud-agents-platform-p0`
- candidate parent before this repair: `327cf73e5a5305548e52e42b249d59f361660923`
- historical generation-lock identity: commit
  `16275f6cbf390c343a9ac00f9193e75eaad0094e`, tree
  `ca595b8e1258a8b78c4da3a545b2a31d8f62b531`, lock SHA-256
  `sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`
- predecessor closure: exact v1 registry/profile/source plus ordered
  migrations `000001`–`000012`, each checked by SHA-256, size, mode, and Git
  blob identity
- durable authority: six versioned fixture cases, eight exact schema paths,
  and seven exact generator/helper paths (including the Go helper)
- migration closure: `000013` SQL, manifest/schema-bundle self-digests,
  predecessor/catalog contracts, and the predecessor schema-bundle archive;
  this is a versioned lineage binding and is intentionally not added to the
  immutable global v2 lock input list
- generated lineage document digest: `sha256:f2e8754e169fa196f9e3f2bf67d37b629af6c04a1873bc77ebd7a3c2ca058c2c`

## Implementation

The v2 registry and Go generator entry points now delegate to reusable
read-only builders. The lineage builder validates the predecessor fence,
localdev-only route authority, fixture schema/instance mappings, exact source
path sets, regular-file/symlink boundaries, and generated output currentness.
The migration helper returns an exact `000013` closure and fails closed on
missing, mode, path, digest, or runtime-artifact drift. The generated lineage
document records `ALL_GATES_OPEN`, `NOT_AUTHORIZED` production/deployment/
publication boundaries, and no HTTP/P2/provider side effect.

The separate `feat/portable-runtime` worktree is not part of this candidate.
All other existing P1 branch tips inspected for the merge are already
ancestors of the P0 branch; no unrelated branch was merged.

## Focused verification

The following checks pass on the candidate worktree:

```text
bun scripts/generate-platform-durable-coordination-registry-v2.ts --check
bun scripts/generate-platform-durable-coordination-go-v2.ts --check
bun scripts/generate-platform-durable-project-create-lineage-v2.ts --check
bunx vitest run scripts/lib/platform-durable-coordination-registry.test.ts --run
  6 passed
bunx vitest run scripts/lib/platform-migration-bundle.test.ts -t 'exact 000013' --run
  1 passed, 17 skipped
bunx vitest run scripts/lib/platform-durable-project-create-lineage-v2.test.ts --run
  3 passed
bun scripts/check-platform-contracts.ts
  BOOTSTRAP_VALIDATED; schemaFiles=64; fixtureCases=79;
  missing=remaining-generator-supply-chain-review; notGateClosure=true
git diff --check
oxfmt --check (all changed TS/JSON files)
oxlint --deny-warnings (all changed TS files)
```

No production database write, HTTP/P2/provider call, deployment, publication,
SSH operation, broad migration suite, or physical/remote durability test was
run for this repair. The existing P2 MD5 namespace observation remains
unchanged and is not part of this candidate.

## Next review boundary

Freeze the candidate commit, then perform one independent read-only review of
that exact commit/tree. The review may create a separate review record, but it
must not modify the candidate or close any Gate. A fresh `APPROVE` is required
before proceeding to the already-approved successor Slice C direction.
