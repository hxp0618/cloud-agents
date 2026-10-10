# Cloud Agents

Cloud Agents first delivers **long-lived Workspaces, general-purpose Sandboxes, outbound customer-node access, and the complete Admin Web together**. User-facing CloudAgents conversation is the later application layer. Backend behavior and the corresponding Admin workflow form one accepted capability.

The foundation owns physical workspace/volume lifetime, sandbox execution, authorized access, placement, policy and recovery. Agent Session/Turn/Execution, approvals, conversation history and provider checkpoints remain application concerns. Stopping a new-model Sandbox must retain its Workspace; deleting a Workspace is a separate authorized operation.

## Architecture and delivery order

Start with the [execution plan](docs/plan/cloud-agents-platform/04-extraction-and-migration.md) and [current status](docs/plan/cloud-agents-platform/06-status-tracker.md). The [product decision](docs/plan/adr/0032-infrastructure-admin-delivery-and-document-routing.md) defines joint infrastructure/Admin delivery; the [target architecture](docs/plan/cloud-agents-platform/02-target-architecture.md) defines the technical boundaries. The detailed sequence is maintained only in the execution plan, not copied here or inferred from historical records.

No-Agent acceptance removes the Agent/Provider dependency, not the Admin Web requirement. Existing Lease-owned volumes retain their existing cleanup semantics; documentation changes do not migrate or alter live data.

The next application scope is [Anywhere Runtime](docs/plan/cloud-agents-platform/01-product-scope-and-authority.md#13-anywhere-runtime-的产品目标): Codex, Claude Code, Pi, and deepseek-harness through unified SDKs on Docker, outbound remote nodes, and Kubernetes, with recovery and failover. This is a delivery target; use the [acceptance definition](docs/plan/cloud-agents-platform/05-gates-and-acceptance.md#anywhere-runtime-v1) and current status to distinguish planned capabilities from verified support.

The portable Runtime keeps its host-neutral JavaScript/stdio ABI. Synara and T3 Code remain downstream consumers with their own logical workspace, VCS, checkpoint and application authority; they do not become dependencies of the foundation.

The nine public Runtime, Provider, Testkit, and Distribution packages use the independent `@cloud-agents/*` namespace. The Control Plane SDK source and release-candidate artifacts use the same namespace but are not published to npm. None of them depends on a Synara application root or T3-private package.

## Repository layout

| Path                                        | Contents                                                                                                                                         |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `services/control-plane`, `services/worker` | Go Control Plane (API, reconcilers, PostgreSQL migrations) and Worker                                                                            |
| `apps/admin-web`, `apps/user-web`           | Admin Web console and user web client (Vite + React)                                                                                             |
| `packages/`                                 | Portable Runtime, protocol and Provider packages (`@cloud-agents/*`)                                                                             |
| `contracts/`                                | Wire contracts — the editable source for generated SDKs ([README](contracts/README.md))                                                          |
| `sdk/go`, `sdk/typescript`                  | Generated Control Plane SDKs; regenerate, never hand-edit                                                                                        |
| `deploy/`                                   | Docker, Compose and Helm deployment assets                                                                                                       |
| `scripts/`                                  | Checks, generators, local dev and E2E harnesses                                                                                                  |
| `tools/`                                    | Pinned generator supply, contract standards and identity verifier tooling                                                                        |
| `docs/`                                     | [Plans and design](docs/plan/README.md), [phase-1 acceptance](docs/acceptance/phase-1.public.md), [release candidate](docs/release-candidate.md) |

## Runtime baseline

- Node.js `24.18.1`
- Bun `1.3.14`
- Provider Host Protocol `2.2` and `2.3`; Runtime Event `2`
- ordinary JavaScript/TypeScript values, `Promise`, `AsyncIterable`, `AbortSignal`, NDJSON, and JSON Schema

`createCloudAgentStdioClient` keeps ambient environment inheritance by default for compatibility. New hosts should set `extendEnvironment: false` and explicitly provide the minimal child environment. Async `subscribe` listeners are receipt barriers: the client waits for each returned promise before delivering the next frame or resolving the terminal command.

Runtime and Provider configuration uses only the `CLOUD_AGENT_*` environment namespace.

The coordinated RC keeps every internal package edge as an exact peer pin. Consumers install the required tarball closure as top-level GitHub Release URLs using the package filenames and SHA-256 values in `candidate-manifest.json`; no unpublished `@cloud-agents/*` package is resolved through npm, and no package-manager security switch needs to be relaxed.

## Local development

With the toolchain pinned in `.mise.toml` (Node.js, Bun, Go, Python, uv, Helm) on `PATH` — for example via `mise install` and `mise exec -- zsh -lc` — and Docker running:

```sh
bun install --frozen-lockfile --ignore-scripts
bun run dev
```

The command starts an ephemeral PostgreSQL 17 database, applies the product migrations, creates the initial administrator and tenant, and builds the production Identity, Control Plane, Worker and Web applications. Admin Web is available at `https://localhost:4174`, User Web at `https://127.0.0.1:4173`, and the Control Plane at `https://127.0.0.1:8080`. The Worker uses mutual TLS. The launcher prints the temporary CA and initial password file paths; trust that CA in your development browser and sign in as `admin@example.com`. Tenant and project selection happens in the application.

`Ctrl-C` removes the owned database, local credentials, certificates and default workspace. Set `CLOUD_AGENTS_DEV_WORKSPACE_DIRECTORY` to an existing absolute directory to retain Runtime workspace and Provider state. Provider credentials can be supplied with `CLOUD_AGENTS_DEV_PROVIDER_CREDENTIALS_DIR`, using filenames such as `tenant-local.codex.json` or `tenant-local.claudeAgent.json`. The packaged `codex`, `claudeAgent`, `pi`, and `deepseek-harness` Providers are enabled unless `CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS` is explicitly set. The default ports can be changed with `CLOUD_AGENTS_DEV_CONTROL_PLANE_LISTEN`, `CLOUD_AGENTS_DEV_WORKER_LISTEN`, `CLOUD_AGENTS_DEV_IDENTITY_PORT`, `CLOUD_AGENTS_DEV_ADMIN_PORT` and `CLOUD_AGENTS_DEV_USER_PORT`.

The default stack supports login and project operations. Managed Agent Sessions also require a ready environment lease or Foundation Sandbox. Run `mise exec -- zsh -lc 'bash test/e2e/test-cloud-agents-dev.sh'` to provision the temporary Foundation fixture and verify the bound Session, `/workspace` execution path, credential boundary and cleanup. Current validation results are recorded in [06](docs/plan/cloud-agents-platform/06-status-tracker.md).

## CLI login and automation

Use the login command printed by the local launcher, or supply your deployment's HTTPS origins:

```sh
cloud-agentsctl login --web-endpoint https://admin.example.com \
  --control-plane-endpoint https://api.example.com --application admin
cloud-agentsctl tenant get
cloud-agentsctl organization list
cloud-agentsctl profile show
cloud-agentsctl profile logout
```

Login opens the browser for approval and returns through a temporary loopback listener. It selects an authorized tenant and project automatically. Use the User Web origin with `--application user` for Agent operations. An optional `--ca-file /absolute/path/ca.pem` trusts a private deployment CA; `--profile /absolute/path/profile.json` selects a separate profile. Profiles are private files containing a revocable CLI grant; commands obtain short-lived, tenant-bound tokens automatically. Logout revokes that grant and removes its profile.

For automation, create a service account in Admin Web, choose its application, role and scope, and save its one-time credential in a private file. Configure a separate automation profile:

```sh
cloud-agentsctl profile configure-service-account \
  --web-endpoint https://admin.example.com --control-plane-endpoint https://api.example.com \
  --application admin --credential-file /absolute/path/credential \
  --profile /absolute/path/automation.json --tenant TENANT_ID --project PROJECT_ID
cloud-agentsctl --profile /absolute/path/automation.json project get
```

Omit `--project` for a tenant-scoped profile. The application must match the service account. Rotate or disable the service account in Admin Web to invalidate its credential and issued tokens. Automation profile logout removes the local file; revocation remains an explicit service-account management action.

## Verification

```sh
bun install --frozen-lockfile --ignore-scripts
bun run fmt:check
bun run lint
bun run typecheck
bun run build
bun run test
bun run secret:scan
mise exec -- zsh -lc 'node scripts/cloud-agent-release-smoke.ts --output-dir /tmp/cloud-agents-release-candidate'
```

The Go, contract and migration checks are listed in [CONTRIBUTING.md](CONTRIBUTING.md). Contract and Proto SDK checks verify the exact toolchain versions, so run them through `mise exec -- zsh -lc '<command>'` or set the documented `CLOUD_AGENTS_NODE` override explicitly.

The release smoke emits nine read-only tarballs, a standalone runtime, checksums, an SPDX 2.3 SBOM, SLSA-shaped provenance, and a candidate manifest. See `docs/release-candidate.md` for the exact boundary.

No package from this repository is published to npm yet. GitHub release candidates are engineering artifacts and do not imply deployment, public beta, production support, or GA.

Tags matching `v<semver>` on `main` run the same product checks, publish the release assets to GitHub, and push multi-architecture Control Plane, Worker, and migration images to GHCR. Pre-release semver tags create GitHub pre-releases; published image digests are included in `cloud-agents-oci-images.json`.

## Contributing, security and license

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request; it lists the checks for each changed surface. Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md), not in public issues.

Cloud Agents is released under the [MIT License](LICENSE). The portable Runtime packages were extracted from the MIT-licensed Synara repository and keep their original notices; see [NOTICE](NOTICE) and [SOURCE_PROVENANCE.md](SOURCE_PROVENANCE.md).
