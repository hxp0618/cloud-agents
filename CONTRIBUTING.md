# Contributing

Follow the [foundation-first boundary](docs/plan/adr/0031-foundation-first-cloud-workspace-platform.md) and [delivery sequence](docs/plan/cloud-agents-platform/04-extraction-and-migration.md): long-lived Workspaces, general Sandbox execution, and customer-node access first; Admin Web accompanies those slices; new user-facing CloudAgents features follow foundation readiness. Reuse existing implementations without treating historical Agent evidence as foundation acceptance.

Use Node.js `24.18.1` and Bun `1.3.14`; `.mise.toml` is the executable toolchain declaration. Install with `bun install --frozen-lockfile --ignore-scripts` after the lockfile exists.

For documentation-only changes, check the diff, local links, current-vs-target claims, and preserved approval/acceptance boundaries; runtime E2E and release closure are not prerequisites for editing prose. If executable examples, contracts, or behavior change, run the affected checks as well. This exception does not waive any explicit candidate/release gate.

Before submitting Runtime code changes, run `bun run fmt:check`, `bun run lint`, `bun run typecheck`, `bun run test`, and `bun run build`. The test command is intentionally `bun run test`; do not substitute `bun test`, because package Vitest configuration and process-level suites are part of the gate.

Keep the public ABI host-neutral. Public packages may expose JSON-compatible objects, ordinary errors, promises, async iterables, abort signals, Node stdio primitives where documented, and JSON Schema. Do not export Effect values or Synara/T3 application types. Do not add Turbo or a dependency on either application root.

Protocol changes must be additive across the supported 2.2/2.3 window and must not change existing schema `$id` values. Internal package dependencies are exact RC pins. Each package tarball must retain `LICENSE`.

Never commit credentials. Synthetic secret-shaped inputs belong only in an explicitly allowlisted test or fixture path. A GitHub RC is not an npm publication or GA approval.
