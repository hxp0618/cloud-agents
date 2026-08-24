# Cloud Agents

Portable Cloud Agent protocol, provider APIs, runtime, provider implementations, conformance tooling, and immutable distributions shared by independent host applications.

The repository is being rebuilt around a host-neutral JavaScript/stdio ABI. Synara and T3 Code remain separate consumers and retain their own orchestration, workspace, VCS, checkpoint, UI, and lifecycle authority.

The seven packages keep their `@synara/cloud-agent-*` names for first-RC wire and import compatibility. That namespace does not make the repository depend on the Synara application root: the workspace contains no Turbo, Effect, Synara Control Plane, or T3-private dependency.

## Runtime baseline

- Node.js `24.18.1`
- Bun `1.3.14`
- Provider Host Protocol `2.2` and `2.3`; Runtime Event `2`
- ordinary JavaScript/TypeScript values, `Promise`, `AsyncIterable`, `AbortSignal`, NDJSON, and JSON Schema

`createCloudAgentStdioClient` keeps ambient environment inheritance by default for compatibility. New hosts should set `extendEnvironment: false` and explicitly provide the minimal child environment. Async `subscribe` listeners are receipt barriers: the client waits for each returned promise before delivering the next frame or resolving the terminal command.

Portable `CLOUD_AGENT_*` environment names take precedence over legacy `SYNARA_*` names. If both names are supplied with different values, the runtime fails closed. Credential metadata written to a child is temporarily written under both names with the same value.

The coordinated RC keeps every internal package edge as an exact peer pin. Consumers install the required tarball closure as top-level GitHub Release URLs from `cloud-agent-candidate.lock.json`; no unpublished `@synara/*` package is resolved through npm, and no package-manager security switch needs to be relaxed.

## Verification

```sh
bun install --frozen-lockfile --ignore-scripts
bun run fmt:check
bun run lint
bun run typecheck
bun run test
bun run build
bun run secret:scan
node scripts/cloud-agent-release-smoke.ts --output-dir candidate
```

The release smoke emits seven read-only tarballs, a standalone runtime, checksums, an SPDX 2.3 SBOM, SLSA-shaped provenance, and a candidate manifest. See `docs/release-candidate.md` for the exact boundary.

No package from this repository is published to npm yet. GitHub release candidates are engineering artifacts and do not imply deployment, public beta, production support, or GA.
