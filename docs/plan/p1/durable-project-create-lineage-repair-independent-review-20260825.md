# Durable Project-create lineage repair independent review

Date: 2026-08-25

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This is a read-only review of fixed candidate
`defb66c4bf62091133b6097997f866eb0b70601c` with tree
`e0e34cca9fabdb9d1caadb115d97ea5aed20740`. The candidate was not modified.
This verdict is limited to the versioned durable Project-create lineage repair;
it is not a generation-lock, release, deployment, production-database, or Gate
closure approval.

## Fixed predecessor and scope

- unique parent: `327cf73e5a5305548e52e42b249d59f361660923`
- candidate change: 19 bounded paths; no unrelated worktree/runtime changes
- historical lock predecessor: commit
  `16275f6cbf390c343a9ac00f9193e75eaad0094e`, tree
  `ca595b8e1258a8b78c4da3a545b2a31d8f62b531`
- predecessor lock blob: `39ee20e035d8770340d46a8663633c6519830de1`
- predecessor lock SHA-256: `sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`
- predecessor lock size: `17377`

The v1 durable registry, profile, source, and migrations `000001` through
`000012` remain byte-identical to the fixed predecessor inputs. The new closure
binds only the versioned v2 authority and append-only `000013` artifacts.

## Evidence

The following read-only checks passed:

```text
bun scripts/generate-platform-durable-coordination-registry-v2.ts --check
bun scripts/generate-platform-durable-coordination-go-v2.ts --check
bun scripts/generate-platform-durable-project-create-lineage-v2.ts --check
bunx vitest run scripts/lib/platform-durable-project-create-lineage-v2.test.ts scripts/lib/platform-migration-bundle.test.ts -t 'exact 000013|durable Project-create lineage v2'
  4 passed, 17 skipped
bun scripts/check-platform-contracts.ts
  BOOTSTRAP_VALIDATED; AJV_2020_AND_IN_REPO_SEMANTICS_PASS;
  missing=remaining-generator-supply-chain-review; notGateClosure=true
git diff --check HEAD^ HEAD
  PASS
```

The `000013` closure checker verifies the manifest and schema-bundle
self-digests, the exact manifest/schema-bundle migration entry, SQL and both
catalog contracts, the predecessor schema-bundle archive, and runtime artifact
digest/size closure. Focused mutation cases reject SQL drift and missing files.

## Boundary

The generated lineage records `ALL_GATES_OPEN`, production database writes,
deployment, publication, and HTTP/P2/provider effects as unauthorized or
forbidden. No database, HTTP, SSH, deployment, publication, or physical
durability action was performed. The existing generation lock remains
unchanged; any successor lock binding requires its separately authorized and
reviewed Slice C work.
