# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repository is

A monorepo prioritizing the **cloud-development foundation: long-lived Workspace + general Sandbox + outbound customer-node access**. User CloudAgents is the later application consumer; Admin Web accompanies infrastructure delivery. Follow ADR-0031 and the BASE-M0–M5 → APP-M1 sequence in docs/plan/cloud-agents-platform/04-extraction-and-migration.md. Preserve existing Agent Runtime/API capabilities and compatibility; do not use new conversation features or page counts as foundation completion.

The implementation notes below describe the existing Lease-backed predecessor, not the target foundation. Synara and T3 Code are downstream consumers; nothing here may depend on them. This documentation task does not authorize code implementation, production data changes, deployment or publication.

Toolchain is pinned in `.mise.toml`: Node 24.18.1, Bun 1.3.14, Go 1.26.6, Helm 4.2.4. Install with `bun install --frozen-lockfile --ignore-scripts`.

## Commands

Full gate (what CI runs; slow):

```sh
bun run check
```

Individual checks:

```sh
bun run fmt:check            # oxfmt (fix with: bun run fmt)
bun run lint                 # oxlint, warnings denied
bun run typecheck            # all @cloud-agents/* packages
bun run test                 # package vitest suites + vitest run scripts
bun run build
bun run platform:go:check    # Go module policy
bun run platform:contracts:check   # every generated contract/SDK artifact must be byte-identical
bun run platform:sdk:check
bun run secret:scan
```

Use `bun run test`, never `bun test` — package Vitest configs and process-level suites are part of the gate.

Single package / single test:

```sh
bun run --cwd packages/cloud-agent-runtime test
bun run --cwd apps/user-web test
bunx vitest run packages/cloud-agent-runtime/src/some.test.ts
bunx vitest run -t "test name" packages/cloud-agent-runtime
```

Go (run from the module directory or use `-C`; always `GOTOOLCHAIN=local GOFLAGS=-mod=readonly`):

```sh
go -C services/control-plane test ./...
go -C services/control-plane test -race ./internal/server/...
go -C services/control-plane test ./internal/server -run TestName
go -C services/worker test ./...
```

`cmd/cloud-agents-control-plane` and `cmd/cloud-agents-worker` have two entry points selected by build tag: `main.go` (`//go:build localdev`, loopback-only, in-memory auth) and `main_production.go` (`//go:build !localdev`). Tests for the localdev entry need `-tags=localdev`:

```sh
go -C services/control-plane test -tags=localdev ./cmd/cloud-agents-control-plane
```

`internal/migration` is ~110k lines and its full suite takes ~15–20 minutes; run it deliberately with `-timeout=30m`, not as part of a quick loop.

Local stack (ephemeral Postgres 17 in Docker, migrations, localdev CP on `127.0.0.1:8080`, Worker on `:8091`, prints a bearer token path and a `cloud-agentsctl` prefix):

```sh
bun run dev
```

Web consoles proxy `/v1` to a Control Plane rather than enabling CORS:

```sh
CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:8080 bun run --cwd apps/user-web dev    # :4173
CLOUD_AGENTS_CONTROL_PLANE_URL=http://127.0.0.1:8080 bun run --cwd apps/admin-web dev
```

Regenerating contract artifacts after editing schemas/OpenAPI/proto:

```sh
bun run platform:contracts:generate
bun run platform:sdk:generate
```

Real-target acceptance scripts (need Docker / a cluster / an SSH host): `scripts/test-platform-compose.sh`, `scripts/test-platform-kubernetes-target.sh`, `scripts/test-platform-ssh-target.sh`, `scripts/test-cloud-agents-helm.sh`.

## Architecture

### Existing business planes and the new foundation boundary

The Control Plane (`services/control-plane`) exposes two HTTP/JSON planes over the same tenancy model (`PlatformTenant → Organization → Project → Membership/RoleBinding`):

- **Managed Agent** (`contracts/managed-agent`): `Session → Turn → Execution → Event`. This is the agent conversation. Executions carry approvals, user-input requests, cancel/interrupt, and artifacts. Events are read by cursor-paged `GET .../events` polling — there is no SSE/WebSocket.
- **Managed Host** (`contracts/managed-host`): `DeploymentTarget` (kind `docker|kubernetes|ssh`, registered and probed per project) and `CloudEnvironmentLease`. A lease is one deployed Worker container/pod at a specific `generation`, pinned to a `releaseDigest`, with `desiredPhase/observedPhase/cleanupPhase`. Sessions bind to a lease's environment.

Admin routes live under `/v1/admin/...` in the same process and verifier; they differ from user routes only by required permission names (`leases.list`, `target.*`, …).

### Existing execution Worker is not the target RemoteWorker

The "Worker" is not a node agent. Each lease deploys exactly one `cloud-agents-worker` container (`--runtime-max-sessions 1`) that runs `cloud-agent-runtime` inside itself with `/workspace` as a persistent named volume and provider credentials mounted read-only at `/run/cloud-agents/provider-credentials`. The Control Plane **dials into** the Worker over mTLS (Worker is the server, identity checked by SPIFFE ID); on Kubernetes the Worker is exposed via a `LoadBalancer` Service. Docker/Kubernetes/SSH actuators live in `internal/{dockertarget,kubernetestarget,sshtarget}`; SSH means "run Docker on a remote host over SSH", not user SSH access. Lease upgrade creates generation N+1 reusing the same workspace volume and retires generation N only after N+1 is Ready.

Lease create/terminate/upgrade currently actuate **synchronously inside the HTTP handler** (`internal/server/managed_host_environment_lease_http.go`). Production runs a bounded, PostgreSQL-backed Worker health observer; that observer is not a lifecycle controller. Outbox/leader/store primitives exist, but the environment lifecycle has no complete background dispatch/reconciliation loop and recovery can require idempotent client replay.

For the new foundation, Workspace/Volume outlive Sandbox stop/TTL; node RemoteWorker identity is independent of Sandbox generation. Use compatible new contracts and explicit adoption for old Lease volumes. Do not silently change existing retention, claim a health poll is reconciliation, or advertise unsupported policy/runtime capabilities.

### Contracts are the authority; code is generated

`contracts/` is the single wire authority (see `contracts/README.md`):

- JSON Schema 2020-12 (`contracts/common`, `contracts/platform`) owns data models.
- OpenAPI 3.1 (`contracts/managed-agent`, `contracts/managed-host`) owns paths/operations only and `$ref`s the schemas.
- Proto3 + ConnectRPC (`contracts/worker`, `contracts/platform-adapter`) owns Worker/Supervisor RPC.

Never derive a schema from a Go/TS struct, and never hand-write a DTO parallel to a generated one. `sdk/go/gen` and `sdk/typescript` are generated; `contracts/generation.lock.json` pins generator inputs and CI fails if regeneration is not byte-identical. Many `*_generated.go` files and `contracts/platform/v1alpha1/schemas/*-registry-*.json` are produced by `scripts/generate-platform-*.ts` — edit the source profile and regenerate, not the output.

### Persistence

PostgreSQL is the only source of truth. Migrations are plain SQL in `services/control-plane/migrations/*.sql` but are bound into a signed per-step bundle under `migrations/product/NNNNNN/` (manifest + schema-bundle + catalog); adding a migration means adding the SQL **and** regenerating the bundle (`bun run platform:migrations:check` validates). All tenant-owned tables carry `tenant_id` with FORCE ROW LEVEL SECURITY; the online role (`cloud_agents_runtime`) is separate from the migration owner and the production entry point refuses to start if role privileges are broader than expected (`runtimeAuthoritySQL`). Store code is in `internal/store/postgres`; `internal/migration` is the migration runner/ledger kernel and is largely independent of request handling.

### Portable Runtime (TypeScript, `packages/`)

Seven `@cloud-agents/*` packages implement Provider Host Protocol 2.2/2.3 and Runtime Event 2 over stdio/NDJSON: `protocol`, `provider-api`, `provider-codex`, `provider-claude`, `runtime`, `testkit`, `distribution`. Public ABI must stay host-neutral (plain JSON values, Promise, AsyncIterable, AbortSignal, JSON Schema — no Effect types, no Synara/T3 types). Protocol changes must be additive across the 2.2/2.3 window and must not change existing schema `$id`. Configuration uses only the `CLOUD_AGENT_*` env namespace; packages pin each other with exact versions.

### Web consoles (`apps/`)

Both are Vite + React + TypeScript using the generated TypeScript Platform SDK, plain CSS with variables, no Next.js/Ant Design/Tailwind (see `docs/plan/cloud-agents-platform/07-admin-web-requirements-and-design.md`; this reference does not itself approve that document or its implementation). Bearer tokens stay in memory only; permitted non-secret connection context, IDs and cursors may be restored according to each application's storage contract. User Web already uses `EnvironmentWorkspace` / `AgentWorkspace` and a published Profile selector; Target/Lease operations are in Admin Web. Reuse this split. Add foundation Admin views alongside real backend behavior; keep user content out of Admin APIs and pages.

### Authentication

Production verifies JWTs against configured trust snapshots (issuer/audience/generation/security epoch). The auth file selects static keys or an HTTPS JWKS URL fetched at startup and SIGHUP reload. This is not complete browser OIDC login or independent User/Admin audience deployment. Every RBAC and JWT-guarded path accepts only a callback-scoped `*authn.VerifiedPrincipal`.

## Planning and decision authority

`docs/plan/` is the only plan root: ADRs (`docs/plan/adr/`), platform docs `01`–`07`, and P0/P1 evidence records. Follow the [Source of truth rules](docs/plan/README.md#source-of-truth) for precedence, current phase boundaries, and recognition of existing or later explicit authorization; do not maintain a separate precedence list here. A reference to a draft is not approval, and approval for implementation does not imply approval for production data, deployment, publication, dirty-worktree deletion, or Gate closure.

Record new scope and architecture decisions before implementation, and acceptance plans before execution. Record observed results, status, evidence indexes, and closure records after the corresponding work or verification; do not require completed evidence before starting authorized implementation. Follow the [evidence rules](docs/plan/cloud-agents-platform/evidence/README.md) for report locations and formal Gate records.

`docs/plan/legacy/` and `docs/plan/references/` are historical inputs, not current work ordering. ADR-0031 records the approved foundation-first product boundary; numbered plans contain the detailed proposal. `docs/coding_agent_cloud_infrastructure_design.html` is an aligned architectural reference, not blanket approval of every technology, stage or SLO. OpenSandbox is the first execution-base candidate to validate, not an already integrated dependency. Old P0–P6 and ADMIN-M* records keep their original evidence and approval scope; BASE-READY does not close them.

## Conventions worth knowing

- Every mutation takes `Idempotency-Key` and `X-Request-Id`; responses use stable error codes (`stableErrorCode`), and state-sensitive operations carry expected `generation`/resource version. 409 distinguishes generation conflict, invalid transition, and idempotency conflict.
- Secrets (provider JSON, kubeconfig, SSH keys, Docker credentials) never appear in API responses, logs, browser storage, or receipts — only `credentialRef` / `providerCredentialRef` identifiers. Synthetic secret-shaped test data must live in an allowlisted fixture path or `bun run secret:scan` fails.
- Worker images are referenced by `repository@sha256:digest` (`releaseDigest`), never by mutable tag.
- Container security baseline in the actuators: non-root 1000, read-only rootfs, `CapDrop ALL`, `no-new-privileges`, seccomp RuntimeDefault, `/tmp` tmpfs. Keep new workload specs at or above this.
- Application E2E reports stay next to the app with commit SHAs, IDs, and observed phase transitions. Use `BASE-M*` / `APP-M*` for new foundation/application phase reports; retain historical `ADMIN-M1`–`ADMIN-M4` and all existing report paths without renumbering. Link reports from `docs/plan` instead of copying them. Formal Gate closure records follow the evidence rules and are not implied by an application report.
