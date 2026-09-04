# M3 Admin Web Maintenance Operations E2E

Date: 2026-09-04

Status: passed for the project-level Maintenance Operations vertical slice. This does not complete M3 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `8a6afef`.
- Runtime: source-built Control Plane on `127.0.0.1:18086`, Worker on `127.0.0.1:18096`, Admin Web on `127.0.0.1:4174`, and a fresh PostgreSQL 17.6 container.
- Database bootstrap: schema head `000043`, 43 migrations applied.
- Authority: existing PostgreSQL-backed `deployment_target_activity`; no second operation store, mock response, or static page data was added.
- Project: `project-1697f28597649651c03ba7962b688cd9` in `tenant-local`.

## Contract and authorization

- `GET /v1/admin/tenants/{tenantId}/projects/{projectId}/maintenance-operations` requires `operations.list` and returns the existing `MaintenanceOperationPage` contract.
- The generated Go and TypeScript clients expose the same Admin route.
- The Control Plane validates tenant/project/request/pagination input, verifies project and Admin authority server-side, and binds the opaque cursor to tenant, project, requested time, Target ID, and operation ID.
- An ordinary user token returned HTTP `403` with stable code `AUTHORIZATION_DENIED` and request ID `m3-maintenance-user-denied`.

## Real API and persistence evidence

Two Targets were registered through the real Admin API solely to create persisted cross-Target operation records:

```text
ssh-maintenance:target.register:succeeded:complete
docker-maintenance:target.register:succeeded:complete
```

The database contained exactly two Targets and two activity rows for this project:

```json
{"targets": 2, "activity": 2}
```

With `pageSize=1`, the project route returned both records across one opaque 211-byte cursor without duplication:

```text
op-c189da392c9f6d5f8a969586720a0da6  ssh-maintenance     g1  succeeded  complete
op-ce82f8e12d9f16deff36541e58dcb30c  docker-maintenance  g1  succeeded  complete
```

The two response bodies had SHA-256 hashes:

```text
3f3d5226b1b6cfa61d2730e1320fababbc1bcec04096fb960193fc3fef1561f1
ebc4a69dcbef47a9260bb62e788a782e58881bfc7a599606e2130f91a1f8890b
```

The decoded responses were scanned and contained no `endpoint`, `credentialRef`, `providerCredentialRef`, private key, conversation, Prompt, Workspace, or Artifact content.

## Real browser evidence

The in-app Chromium browser connected to the running Admin Web and Control Plane. It rendered the real two-row Maintenance list and operation detail Sheet; no mock or request interception was used.

- Manual visual checks covered `zh-CN` and `en-US`, light and dark themes, and `1280x720` desktop and `390x844` mobile viewports.
- Desktop document width equaled viewport width. Mobile document width was `390px`; the `356px` table viewport used only its intended horizontal `overflow: auto`, while the page itself did not overflow.
- A refresh produced only same-origin Control Plane Admin requests: Target, Lease, Profile, Maintenance Operation, and selected Target detail routes under `/v1/admin/...`; all responses were HTTP `200`.
- Browser console warnings/errors: zero.
- Local storage contained only locale and theme. Session storage contained only non-secret connection context. A value scan found no bearer/JWT, `credentialRef`, `providerCredentialRef`, or private key material.
- Reload restored locale/theme and non-secret connection context while returning to the disconnected screen with an empty bearer-token field.

## Source verification

The following checks passed with the repository-pinned Bun 1.3.14, Node 24.18.1, uv 0.12.5, Python 3.14.7, and Go 1.26.6 toolchains:

```text
apps/admin-web test                         # 2 files, 11 tests
apps/admin-web typecheck and production build
sdk/typescript test                        # 3 files, 33 tests
sdk/typescript typecheck and build
go test: control-plane server/store/authn/control-plane command packages
go test and vet: sdk/go ./...
platform:contracts:check                   # 122 schemas, 67 OpenAPI operations
platform:sdk:check and platform:sdk:consumers
platform:migrations:check
platform:go:check
repository typecheck, lint, format check, secret scan
git diff --check
```

Fresh consumer artifact hashes were:

```text
TypeScript sha256:3a4c0b03e12116787aff8a1f9fe5cbcff94d371017cb5c71bed589337e651240
Go module zip sha256:71a1091951cc2461ef401a4d467c97aabd37a0c8d3a74924da0979959690f221
```

An additional repository-wide Control Plane run was not green in out-of-slice paths: the existing `cloud-agentsctl` raw-artifact fixture lacks the already-required `Content-Disposition`, and `internal/migration` reported quota/session baseline failures. This slice changes neither path; the focused packages and migration bundle checks above are green.

## Teardown and evidence boundary

- Ports `4174`, `18086`, `18096`, and the temporary token bridge port `18087` were closed.
- The fresh local-dev state directory was removed, and label-filtered Docker inventory contained no `cloud-agents.dev-run` container.
- The two synthetic Target endpoints were intentionally left unprobed. No browser or Control Plane process connected to Docker, Kubernetes, or SSH, and no destructive Cleanup was requested.
- This slice proves a real persisted, paginated, authorized, bilingual project Maintenance Operations view for current Target operations. Worker/Lease operations will appear only after their own durable authorities are implemented; Cluster/Worker views, lifecycle actions, Images/Releases, and policies remain M3 work.
