# Contributing

Use Node.js `24.18.1` and Bun `1.3.14`; `.mise.toml` is the executable toolchain declaration. Install with `bun install --frozen-lockfile --ignore-scripts` after the lockfile exists.

Before submitting a change, run `bun run fmt:check`, `bun run lint`, `bun run typecheck`, `bun run test`, and `bun run build`. The test command is intentionally `bun run test`; do not substitute `bun test`, because package Vitest configuration and process-level suites are part of the gate.

Keep the public ABI host-neutral. Public packages may expose JSON-compatible objects, ordinary errors, promises, async iterables, abort signals, Node stdio primitives where documented, and JSON Schema. Do not export Effect values or Synara/T3 application types. Do not add Turbo or a dependency on either application root.

Protocol changes must be additive across the supported 2.2/2.3 window and must not change existing schema `$id` values. Internal package dependencies are exact RC pins. Each package tarball must retain `LICENSE`.

Never commit credentials. Synthetic secret-shaped inputs belong only in an explicitly allowlisted test or fixture path. A GitHub RC is not an npm publication or GA approval.
