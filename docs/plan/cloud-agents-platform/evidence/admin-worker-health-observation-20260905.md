# On-demand Worker health observation — 2026-09-05

Base `16d69d36820e34834edad836a87926a3f72effdd`; branch remains
`codex/cloud-agents-platform-p0`. This is a prerequisite slice for ADMIN-M1 Worker
availability, not milestone or Goal completion.

## Implementation

- `GET /v1/admin/tenants/{tenantId}/projects/{projectId}/workers/{leaseId}/health?expectedGeneration=N`
  requires `projects.get` and Admin `workers.list`, then uses the existing RBAC/RLS-backed
  Lease read. There is no client-selected endpoint, trust material, or credential reference.
- Only active, ready, unexpired target-backed Leases with identity/trust can be checked.
  The existing Control Plane mTLS Supervisor negotiates and checks the real Worker protocol,
  with a five-second transport deadline and idle-connection cleanup. No lifecycle mutation,
  Session, Prompt, Artifact read, or infrastructure API call is performed.
- A second authoritative Lease read rejects generation, resource version, route identity,
  phase, cleanup or expiry changes with 409. This is snapshot consistency, **not a Worker
  fencing acknowledgement**. Missing trust returns 503. Transport/remote failures only return
  `state: unavailable`, never raw error details or remote bytes.
- The new closed `WorkerHealthObservation` contract and generated Go/TypeScript SDK carry
  tenant/project/Worker identity, generation/resource version, serving/unavailable, and CP
  check time. Response is `Cache-Control: no-store`. SDKs reject response authority mismatch.
- Admin Worker Sheet has localized check/pending/result/error feedback. Starting Workers
  cannot invoke it. Requests are canceled when the Sheet unmounts or the connection changes;
  old request results are ignored. Nothing is stored in browser storage.
- Design-style uses the existing Daytona-style action block and native outline button;
  React/Ponytail reuse existing state, abort handling, SDK and mTLS code without dependencies.
  User API and User Web need no changes for this Admin-only read.

**Not delivered:** periodic/persisted heartbeat, online/expired Overview counters, Worker
session-admission Drain, or resource usage. Existing `lastHealthAt` remains the deployment
observation; this API does not rewrite it or relabel ready as online.

## Actual verification

- `TestAdminWorkerHealthRealMTLSKernel` builds and starts the independent production Worker
  executable with ephemeral CA/server/client certificates. There is no forbidden cross-service
  Go import. Its Runtime command is an unused fail-closed `false` executable: no Session is
  opened and no Provider readiness is implied. Serving, stopped Worker, wrong SPIFFE identity,
  missing identity/trust, invalid/duplicate/extra query parameters, expired/stale Lease,
  nine changes during health check, and denied Admin scope are exercised. Authorization and
  Lease snapshots in this test are doubles: **not persisted deployment or Provider E2E**.
- Go server/workerclient tests and race tests passed; Go generated platform/OpenAPI tests
  passed, including health client request validation and cross-tenant response rejection.
  TypeScript SDK/generator tests: 30 passed; Admin tests: 26 passed. Admin typecheck/build,
  scoped format/lint, Go module boundary and SDK regeneration checks passed. Existing bundle warning remains
  (525.20 kB single JS chunk).
- Contract standards passed with cached pinned uv 0.12.5, Python 3.14.7 and Bun 1.3.14:
  141 schemas, 89 OpenAPI operations, official-suite 1299 assertions and 14 Python tests.
  Initial uv 0.12.9 mismatch was resolved only by command-local PATH; no system replacement.
- Fresh local PostgreSQL/Control Plane at `127.0.0.1:18085` and Admin Vite at
  `127.0.0.1:4174`; project `project-d977e9362463c7cb1a7a76c8bcb476d0`.
  A fresh read-only mTLS gateway forwarded actual Docker `/_ping` and `/version`; Target
  `health-docker` became ready. SDK created `health-lease`, which stayed provisioning because
  local managed-host mode has no deployment actuator. No deployed Worker was fabricated.
- Against that actual database/API, ordinary-user health request returned403 and Admin
  request for the provisioning Worker returned409. Playwright/Brave verified 8 combinations
  (zh-CN/en-US, light/dark, 1440×900/390×844), disabled check, Enter-open/Escape-close/focus
  restoration, real nonblank page, no Vite overlay/document overflow/page errors, same-origin
  GET-only API traffic and no stored bearer. Only unset Quota and Quota Audit returned404.
  Browser plugin was unavailable. No mocked responses, product state injection, or direct database fixture writes.
- Inspected English desktop and Chinese dark mobile screenshots; corrected the new section's
  inset by reusing `action-block`. Daytona source HEAD reconfirmed
  `01c502bb1f1ff8f2885d0cd490e043736083dca8`. This is **not** full pinned screenshot-diff acceptance.
  Browser serving/unavailable/pending/error states still require a deployed ready Worker run.
- SDK termination returned generation2, terminated/complete, Worker projection0. Own dev
  stack/container `cloud-agents-dev-501-57880`, Vite and gateway stopped; state directory
  `.tmp/cloud-agents-dev.QYpzKX` and its disposable database/tokens were removed. Only generated
  PKI/credential directories were moved to private recoverable Trash
  `cloud-agents-worker-health-l1Gut0`. No pre-existing runtime resources were changed.

## Reproduce / next boundary

Run from repository root with the pinned runtime PATH:

```sh
go -C services/control-plane test -race ./internal/server ./internal/workerclient
go -C sdk/go test ./gen/platform/v1alpha1 ./gen/openapi/v1alpha1
bunx vitest run sdk/typescript/src/worker-health.test.ts sdk/typescript/src/platform.test.ts scripts/lib/platform-json-sdk.test.ts
bun scripts/generate-platform-json-sdks.ts --check
bun run --cwd apps/admin-web test
bun run --cwd apps/admin-web build
```

For live browser replay, use the fresh Docker Probe/local-stack preparation in
[Lease state evidence](admin-web-lease-states-20260905.md), create a new provisioning Lease,
then open Clusters & Workers → Worker Sheet: the check must be disabled. Call the new SDK
method `getAdminWorkerHealth(tenant, project, lease, requestId, generation)` with Admin and
ordinary tokens to reproduce409/403. Local read-only browser/setup scripts and eight
screenshots are retained at `/tmp/cloud-agents-worker-health.l1Gut0`; secret paths there
were moved to Trash and must be regenerated for a new run.

Earliest remaining ADMIN-M1 work is genuine Worker freshness/drain authority and the remaining
Overview/fixed-reference acceptance. Ready-Worker browser checks, full Docker/Kubernetes/SSH
deployment/cleanup and Codex/Claude Turns remain unqualified. No new permission or credential
is needed for the next source slice. Goal stays active.
