# Contributing

Follow the [joint infrastructure/Admin boundary](docs/plan/adr/0032-infrastructure-admin-delivery-and-document-routing.md) and [execution plan](docs/plan/cloud-agents-platform/04-extraction-and-migration.md): infrastructure and the complete Admin Web form the first deliverable; new user conversation features follow their joint readiness. Reuse existing implementations without treating historical Agent evidence as foundation acceptance.

Use Node.js `24.18.1` and Bun `1.3.14`; `.mise.toml` is the executable toolchain declaration. Install with `bun install --frozen-lockfile --ignore-scripts` after the lockfile exists.

For documentation-only changes, check the diff, local links, current-vs-target claims, and preserved approval/acceptance boundaries; runtime E2E and release closure are not prerequisites for editing prose. If executable examples, contracts, or behavior change, run the affected checks as well. This exception does not waive any explicit candidate/release gate.

Before submitting Runtime code changes, run `bun run fmt:check`, `bun run lint`, `bun run typecheck`, `bun run test`, and `bun run build`. The test command is intentionally `bun run test`; do not substitute `bun test`, because package Vitest configuration and process-level suites are part of the gate.

For other code changes, select the applicable verification routes below. Reuse [package scripts](package.json) and [CI](.github/workflows/ci.yml); do not infer Go or infrastructure verification from Runtime tests alone.

| Changed surface | Existing verification route |
| --- | --- |
| Go Control Plane, Worker or Go SDK | `bun run platform:go:check` and `sh scripts/test-platform-go-products.sh` (product tests, race, vet and module integrity). Changes outside that script's declared package scope also need their affected package tests. |
| Contracts or generated SDKs | `bun run platform:contracts:check`, `bun run platform:sdk:check`, and `bun run platform:sdk:consumers` when consumer compatibility is affected. |
| SQL/migration bundle | `bun run platform:migrations:check` plus the affected database, isolation and recovery tests in an authorized test environment; static checks do not prove a successful migration. |
| Admin Web | `bun --filter @cloud-agents/admin-web typecheck`, `bun --filter @cloud-agents/admin-web test`, and `bun --filter @cloud-agents/admin-web build`; exercise affected rendered flows, including relevant authorization, language and visual states. |

Run corresponding real integration checks when runtime behavior changes. These are change-scoped verification routes, not a requirement to run every E2E before each edit or permission to deploy. Full CI and explicit candidate/release gates remain unchanged; an unavailable check is reported as unverified, not passed.

Keep the public ABI host-neutral. Public packages may expose JSON-compatible objects, ordinary errors, promises, async iterables, abort signals, Node stdio primitives where documented, and JSON Schema. Do not export Effect values or Synara/T3 application types. Do not add Turbo or a dependency on either application root.

Protocol changes must be additive across the supported 2.2/2.3 window and must not change existing schema `$id` values. Internal package dependencies are exact RC pins. Each package tarball must retain `LICENSE`.

Never commit credentials. Synthetic secret-shaped inputs belong only in an explicitly allowlisted test or fixture path. A GitHub RC is not an npm publication or GA approval.
