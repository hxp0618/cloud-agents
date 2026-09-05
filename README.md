# Cloud Agents

Cloud Agents first delivers **long-lived Workspaces, general-purpose Sandboxes, outbound customer-node access, and the complete Admin Web together**. User-facing CloudAgents conversation is the later application layer. Backend behavior and the corresponding Admin workflow form one accepted capability.

The foundation owns physical workspace/volume lifetime, sandbox execution, authorized access, placement, policy and recovery. Agent Session/Turn/Execution, approvals, conversation history and provider checkpoints remain application concerns. Stopping a new-model Sandbox must retain its Workspace; deleting a Workspace is a separate authorized operation.

## Architecture and delivery order

Start with the [execution plan](docs/plan/cloud-agents-platform/04-extraction-and-migration.md) and [current status](docs/plan/cloud-agents-platform/06-status-tracker.md). The [product decision](docs/plan/adr/0032-infrastructure-admin-delivery-and-document-routing.md) defines joint infrastructure/Admin delivery; the [target architecture](docs/plan/cloud-agents-platform/02-target-architecture.md) defines the technical boundaries. The detailed sequence is maintained only in the execution plan, not copied here or inferred from historical records.

No-Agent acceptance removes the Agent/Provider dependency, not the Admin Web requirement. Existing Lease-owned volumes retain their existing cleanup semantics; documentation changes do not migrate or alter live data.

The portable Runtime keeps its host-neutral JavaScript/stdio ABI. Synara and T3 Code remain downstream consumers with their own logical workspace, VCS, checkpoint and application authority; they do not become dependencies of the foundation.

The seven Runtime packages and the public Control Plane SDK use the independent `@cloud-agents/*` namespace. They do not depend on a Synara application root or T3-private package.

## Runtime baseline

- Node.js `24.18.1`
- Bun `1.3.14`
- Provider Host Protocol `2.2` and `2.3`; Runtime Event `2`
- ordinary JavaScript/TypeScript values, `Promise`, `AsyncIterable`, `AbortSignal`, NDJSON, and JSON Schema

`createCloudAgentStdioClient` keeps ambient environment inheritance by default for compatibility. New hosts should set `extendEnvironment: false` and explicitly provide the minimal child environment. Async `subscribe` listeners are receipt barriers: the client waits for each returned promise before delivering the next frame or resolving the terminal command.

Runtime and Provider configuration uses only the `CLOUD_AGENT_*` environment namespace.

The coordinated RC keeps every internal package edge as an exact peer pin. Consumers install the required tarball closure as top-level GitHub Release URLs using the package filenames and SHA-256 values in `candidate-manifest.json`; no unpublished `@cloud-agents/*` package is resolved through npm, and no package-manager security switch needs to be relaxed.

## Local development

With the pinned Node.js, Bun, and Go versions on `PATH` and Docker running:

```sh
bun install --frozen-lockfile --ignore-scripts
bun run dev
```

The command starts an ephemeral PostgreSQL 17 database, applies the product migrations, bootstraps `tenant-local` and `organization-local`, builds the Runtime and local Go binaries, then serves the Worker on `127.0.0.1:8091` and the Control Plane on `127.0.0.1:8080`. It enables the packaged `codex` and `claudeAgent` Providers unless `CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS` is explicitly set, prints the generated 0600 bearer-token path, and prints an exact `cloud-agentsctl` prefix. `Ctrl-C` removes the owned container, local credentials, and default managed workspace; set `CLOUD_AGENTS_DEV_WORKSPACE_DIRECTORY` to an existing absolute directory to retain Runtime workspace and Provider state. Provider credential files remain optional and can be supplied with `CLOUD_AGENTS_DEV_PROVIDER_CREDENTIALS_DIR`; the local tenant uses `tenant-local.codex.json` or `tenant-local.claudeAgent.json`.

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

Tags matching `v<semver>` on `main` run the same product checks, publish the release assets to GitHub, and push multi-architecture Control Plane, Worker, and migration images to GHCR. Pre-release semver tags create GitHub pre-releases; published image digests are included in `cloud-agents-oci-images.json`.
