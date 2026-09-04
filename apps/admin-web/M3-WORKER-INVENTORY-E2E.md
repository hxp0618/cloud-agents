# M3 Admin Web Worker Inventory E2E

Date: 2026-09-04

Status: passed for the Lease-backed Worker inventory vertical slice. This does not complete M3 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `98d235c`.
- Local release version: `0.0.0-m3-worker.1`, built from dirty source without publishing.
- Release source commit: `98d235ca91648e6a9b4c28f02a4810df16d17c28`.
- Relevant arm64 artifact hashes:
  - Control Plane: `sha256:7fa03c1c4db17a08b155c56c381f349e96588fbe04149f5ed7708f6078f5eac5`
  - Worker: `sha256:fea8b2b31bdd466b32bcbfb41c9ff8e5c10882d89d8cb4c8da1e57dd9d30ba47`
  - Contract bundle: `sha256:0a0f6673d619bef272afc11b5daddabad112662f01850f9541d196c096362909`
  - Deployment bundle: `sha256:a53e11144ad312732ab5120e30bec90827c3b112a82c821df8e14e72da5d75e0`
- Runtime: packaged PostgreSQL 17.6, migrations, Control Plane, Worker, local registry, and a real Docker Deployment Target through the host Docker API.

## Authority and contract

- `GET /v1/admin/tenants/{tenantId}/projects/{projectId}/workers` requires `workers.list` and returns the generated `WorkerPage` contract.
- A Worker is the current logical projection of an authoritative Environment Lease joined to its Deployment Target. No second Worker table, compatibility row, mock response, or static UI data was added.
- The cursor is bound to tenant, project, and Lease identity. Terminated Leases whose cleanup phase is `complete` are excluded.
- The Admin response intentionally omits Target endpoint, `credentialRef`, `providerCredentialRef`, Provider credentials, conversation, Prompt, Workspace, and Artifact fields.
- `lastHealthAt` is the successful mTLS Worker observation recorded when deployment became ready. It is not presented as a periodic heartbeat. CPU and memory values are configured limits, not measured usage.

## Real Docker lifecycle evidence

The final packaged Compose smoke used Profile `compose-docker-profile:v1` to create Environment and Lease `environment-0cc46b1599b67da3345de72ee27f3312` on `docker-compose-target`.

While the Docker container was running, the Admin API returned exactly one ready Worker with:

```text
uid/lease        environment-0cc46b1599b67da3345de72ee27f3312
target           docker-compose-target (docker), generation 1
worker generation 1
release digest   sha256:5da6536b67765907962249f5425d2c5a64d603a75bfdd3a54fff822a3eebb519
limits           1000 mCPU, 536870912 bytes
identity         spiffe://cloud-agents.compose/worker-target
server name      host.docker.internal
state/cleanup    ready / none
```

The smoke recursively scanned the decoded response and rejected forbidden infrastructure or user-content keys. The ordinary User token request returned HTTP `403` with stable code `AUTHORIZATION_DENIED`.

After Lease termination and Target cleanup, the same Admin route returned an empty `WorkerPage`. The final label-filtered Docker inventory was empty:

```text
platform Compose smoke passed (linux/arm64, darwin-arm64, profile=compose-docker-profile:v1, environment=environment-0cc46b1599b67da3345de72ee27f3312, docker-workers=0)
```

The full packaged flow passed before and after the browser run, including Control Plane restart and PostgreSQL backup/restore. Each run removed its Compose network, database volume, test-owned Worker containers, credential volumes, local registry, and test images.

## Real browser evidence

The Codex in-app Chromium browser opened the independent Admin Web on `127.0.0.1:4174`; Vite proxied only `/v1/admin` to the packaged TLS Control Plane on the ephemeral loopback port `33080`, with the smoke CA trusted by Node. No request interception or UI fixture was used.

- Project: `project-2fde604e9ef099aad6125e5a2181196c` in `tenant-compose-smoke`.
- The sidebar, Overview count, Worker list, status, resource limits, and detail Sheet all rendered the one running Docker Worker.
- `zh-CN` and `en-US` switched immediately from the Daytona-style account menu. Both light and dark list/detail states were visually checked.
- The measured viewport was `791x998`; document width was `791/791` and the open Sheet was `499/499`, so neither overflowed. The ten-column table used its intended internal horizontal scroller (`501/1280`) without shrinking typography.
- No Target endpoint, `credentialRef`, `providerCredentialRef`, or translation message key was visible in the Worker list or Sheet.
- Browser logs contained Vite debug/info messages only; warning and error counts were zero after the final connection.
- The browser tab was closed before the temporary Admin Web and Compose processes were stopped.

This browser run is current evidence for the new Worker page at the measured desktop-panel viewport. It does not replace M4's required automated `zh-CN`/`en-US` × light/dark × desktop/mobile visual-regression matrix.

## Source verification

The repository-pinned Bun 1.3.14, Node 24.18.1, uv 0.12.5, Python 3.14.7, and Go 1.26.6 toolchains were used. These checks passed:

```text
apps/admin-web test                         # 2 files, 12 tests
apps/admin-web typecheck and production build
sdk/typescript test                        # 3 files, 34 tests
sdk/typescript typecheck and build
go test: control-plane server/store/authn/control-plane command packages
go test and vet: sdk/go ./...
platform:contracts:check                   # 124 schemas, 68 OpenAPI operations, 14 tests
platform:sdk:check and platform:sdk:consumers
platform:migrations:check
platform:go:check                          # 3 modules, go1.26.6, readonly
repository typecheck, lint, format check, secret scan
shell syntax and git diff whitespace checks
packaged Compose smoke                     # final source script, no browser pause hook
```

Fresh generated-SDK consumer artifacts were:

```text
TypeScript sha256:087c25e18a9ef70798f717e505a972e4c049cc9028f1fcad4b852426773d7f89
Go module zip sha256:67b21cf62536f7e33477c684e9f8b2b35b976fbb031e14b3186beafb441cf637
```

The root typecheck was run serially after rebuilding the TypeScript SDK. An earlier parallel attempt overlapped the SDK consumer's clean build and temporarily observed the package without its generated `dist`; the serial run passed for every workspace package.

## Evidence boundary

- This slice proves an authorized, paginated, generated-SDK-backed Admin Worker inventory over a real Profile-driven Docker Worker lifecycle and cleanup.
- It does not add or claim a continuous Worker heartbeat, measured CPU/memory usage, Cluster authority, Drain/Resume, Upgrade/Rollback, Releases catalog, Quota, Storage/Network policy management, real Kubernetes/SSH deployment, or real Codex/Claude Code Provider turns.
- The Compose Kubernetes endpoint exercised by this script is a local protocol fixture for existing packaging checks, not the required real Kubernetes M4 target. No SSH target was exercised by this slice.
