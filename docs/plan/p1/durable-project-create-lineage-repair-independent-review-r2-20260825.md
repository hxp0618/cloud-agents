# Durable Project-create lineage repair independent review — R2

Date: 2026-08-25

## Verdict

`APPROVE - P0=0 / P1=0 / P2=0`

This fresh, read-only review is bound to fixed candidate
`a3d2b1e6f5241383d16785828d41b365566deb40` and tree
`87408b2e1df9598b25c468023ddf63c2d2cd774d`. The earlier `defb66c` review is
historical only; this R2 verdict supersedes it for the closure-path repair.
The candidate was not modified.

## Fixed lineage and closure

- unique candidate parent: `fb406e29c1771285d77ca9e8fa6fe26087908c7c`
- source migration paths are checked against the exact `000013` closure
- closure inputs include:
  - `000013_add_durable_project_create_writer.sql`
  - `manifest.json`
  - `schema-bundle.json`
  - `catalog/schema-000013.json`
  - predecessor `catalog/schema-000012.json`
  - predecessor schema-bundle archive
- the generated lineage output records the predecessor catalog artifact and
  remains byte-current

The historical v1 registry/profile/source and migrations `000001`–`000012`
remain fenced and unchanged. No SQL, generation-lock, or historical authority
bytes were modified by this repair.

## Focused evidence

```text
bun scripts/generate-platform-durable-project-create-lineage-v2.ts --check
  PASS

bunx vitest run scripts/lib/platform-durable-project-create-lineage-v2.test.ts scripts/lib/platform-migration-bundle.test.ts -t 'exact 000013|durable Project-create lineage v2'
  4 passed, 17 skipped

git diff --check a3d2b1e^ a3d2b1e
  PASS
```

The focused tests cover exact source/closure path binding and explicit
predecessor-catalog input membership, as well as the existing digest/size,
drift, missing-artifact, lineage, and fixture checks.

## Boundary

This approval is limited to the versioned lineage closure-path repair. It does
not modify or advance the generation lock, and does not close any Gate or
authorize production database writes, HTTP/P2/provider effects, deployment,
publication, SSH, or release activity.
