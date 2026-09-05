# Persisted Worker health — 2026-09-05

Base `63ddf8b6a067ca146607e1e8ff8f83a128142aa5`, branch
`codex/cloud-agents-platform-p0`. ADMIN-M1 prerequisite slice; no milestone or Goal closure.

## Delivered authority

- Migration 000050 adds nullable observations/short-lived claims to the existing Lease table.
  Internal SECURITY DEFINER functions claim at most eight due routes with `FOR UPDATE SKIP LOCKED`.
  Claims expire after ten seconds; eligible ready, active, unexpired target-backed Leases are
  checked no more frequently than every twenty seconds unless generation/resource version changes.
  Network calls run outside database transactions, with five-second mTLS deadlines.
- Completion must match tenant, project, Lease, generation, resource version and an unexpired
  random claim. Stale/replayed results are discarded. Cancellation does not record process
  shutdown as Worker failure. Direct runtime-role writes remain denied; tenant RLS is unchanged.
- Production Control Plane starts/stops the observer with its existing Worker mTLS trust and
  database pool. No browser session is needed. No new dependency, interface or configuration
  layer. Local-dev mode intentionally has no observer because it has no production mTLS trust.
- Admin Worker projection adds optional `spec.health`: `online`, `unavailable`, or `expired`,
  with `checkedAt`, `expiresAt`, and optional `lastSuccessAt`. Database time determines state;
  freshness is at most sixty seconds, capped by Lease expiry. Only matching current Lease
  generation/resource-version observations are returned. Absence means not observed, not offline.
  Worker list responses are `Cache-Control: no-store`.
- Closed contract and generated Go/TypeScript SDK validate states, bounded timestamps, nested
  payloads and ready-only presence. Admin Overview, Worker table and Sheet expose localized
  health/counts/filters and dates; the existing polling effect refreshes visible Worker/Overview
  views every fifteen seconds. No browser-clock online inference or stored health state.
- Existing deployment `lastHealthAt`, lifecycle `state`, and configured resource limits retain
  their meanings. Health observation is **not a Worker generation/admission acknowledgement**,
  a successful Provider turn, Worker Drain, or live resource usage.
- Design-style/React/Ponytail reuse the existing Daytona-style panel, badge, summary buttons,
  native select, Sheet, SDK and polling path. User Web/API are unchanged for this Admin-only slice.

## Executed verification and boundaries

- Fresh owned PostgreSQL 17.6 container `cloud-agents-dev-501-83007` applied the actual product
  migrator: `schema_head=000050`, `applied=50`. Local CP readiness succeeded on port18085.
  Project `project-69729691521d4d098226e47b80b23589` and unprobed Docker Target `health-docker`
  were created through real APIs. Target endpoint/credential reference were deliberately
  unconfigured: **this run did not Probe or deploy through Docker, Kubernetes or SSH**.
- `TestWorkerHealthPostgresAndProcess` ran normally and repeatedly under `-race` (~103 seconds).
  It starts independent production Worker executables with fresh test PKI and runs the actual
  observer against PostgreSQL using the restricted runtime login. Admin reads use the running
  local CP, real RBAC/RLS/store and generated Go decoder, not store/auth/HTTP mocks.
- A ready Lease is explicitly inserted as an isolated SQL fixture pointing to that real Worker;
  its release digest/provider reference are test inputs, not an installed release or credential.
  Runtime is fail-closed `false` and never invoked. No Session, Provider, Prompt or Artifact is used.
- Actual persisted sequence: absent → online → second periodic success → stop Worker → unavailable
  retaining last successful check → stop observer → expired after actual sixty-second passage
  → replacement Worker/current route version → online. Expiry timestamps were not rewritten.
  Additional SQL fixture transitions verify stale generation/version, wrong tenant/project/token,
  expired/reclaimed/replayed claims, invalid tokens, two concurrent claimers for nine rows
  (no duplicate, at most eight per batch), canceled observer, RLS and direct-write denial.
  Those nine rows test database concurrency, **not nine deployed Workers**. Exact fixtures are
  deleted by test cleanup and standalone Worker processes/PKI are removed by test cleanup.
- Ordinary user token returned403 on the actual Worker Admin API. Admin Worker responses were
  checked for private endpoint/provider reference/Secret leakage. No production OIDC flow was
  qualified; the production startup hook was compiled/tested, while the actual loop was run by
  the integration test alongside the local-dev HTTP process.
- TypeScript SDK:42 tests; Admin:27; release-bundle packaging:17. Focused Go store/server/migration/
  production-command tests, race tests, generated Go SDK tests and Go vet passed. SDK regeneration
  and module-boundary checks passed with Go1.26.6. Contract standards passed:142 schemas,
  89 OpenAPI operations, official-suite1299 assertions, fourteen Python tests.
  Initial module check used system Go1.27.1; rerun with the pinned PATH passed.
  The release packaging test's previous49 count was updated to50 and all seventeen tests passed.
- SDK, Admin and User builds passed. Admin retains the existing single-chunk warning
  (530.02kB minified); no chunking framework or dependency was added.
- Read-only Playwright/Brave capture passed eight combinations of zh-CN/en-US, light/dark,
  1440×900/390×844: real nonblank page, online health and dates, counts/filter navigation,
  Enter/ESC/focus restoration, no page errors/document or summary-button overflow, same-origin
  GET-only API traffic and no stored bearer. Earlier captures also displayed actual unavailable
  health. Only unset Quota/Quota Audit returned404. A late capture after the test had deleted
  its Lease timed out; it was rerun while the integration fixture was alive.
  No browser response mocks or injected product state. Browser CLI/plugin was unavailable;
  existing Playwright/Brave was used without installing dependencies.
- Inspected desktop Worker detail and Chinese dark mobile health captures. Fixed Daytona source
  HEAD reverified as `01c502bb1f1ff8f2885d0cd490e043736083dca8`. These captures **do not qualify
  complete Daytona fixed screenshot-diff acceptance**.
- Cleanup verified: own dev parent/Vite stopped, container `cloud-agents-dev-501-83007` absent,
  `.tmp/cloud-agents-dev.2HjII3` absent, and ports4174/18085/18095 no longer listening. Its
  disposable database and temporary tokens were deleted, not retained for recovery; sanitized
  screenshots/JSON remain. No pre-existing container, Pod, workspace or credential was changed.

## Reproduce

Use pinned Go1.26.6/Bun1.3.14/Node24.18.1. Start a fresh disposable dev stack (not an existing
user database) with CP on `127.0.0.1:18085`, Worker on `127.0.0.1:18095`, and Admin Vite4174.
Create a project in `tenant-local`/`organization-local` with the existing CLI, then register
Docker Target ID/name `health-docker` using the Admin SDK. Probe is not required for this test
fixture; the target must not point to resources requiring cleanup.

Supply the disposable database's runtime/migration URLs through these environment variables;
do not print their passwords or reuse production URLs. The test refuses a database containing
any Lease. It deletes only its own ten named fixture rows.

```sh
export CLOUD_AGENTS_HEALTH_TEST_RUNTIME_DATABASE_URL='postgres://...'
export CLOUD_AGENTS_HEALTH_TEST_MIGRATION_DATABASE_URL='postgres://...'
export CLOUD_AGENTS_HEALTH_TEST_PROJECT_ID='project-ID-from-create'
export CLOUD_AGENTS_HEALTH_TEST_DEV_STATE='/absolute/path/to/fresh/dev-state'
go -C services/control-plane test -race ./internal/server \
  -run '^TestWorkerHealthPostgresAndProcess$' -count=1 -v
```

While the test logs `online` and before its cleanup, in another terminal:

```sh
# PLAYWRIGHT_MODULE points to an already installed Playwright module if not on Node's path.
node apps/admin-web/visual-baseline/daytona-v0.190.0/capture-worker-health.mjs \
  /absolute/path/to/owned/output /absolute/path/to/control-plane-admin.token \
  /absolute/path/to/control-plane.token project-ID-from-create
```

The read-only capture script is checked in. This run's captures/JSON remain under
`/tmp/cloud-agents-worker-heartbeat.eV7o3h/browser` and its `scrolled/browser` subdirectory.
No secrets are stored there. Follow the dev script's normal shutdown to remove its owned
database and temporary state. Migration050 is forward-only; no down-migration or migration
of an existing deployment was attempted.

## Earliest remaining work

ADMIN-M1 still needs genuine Worker draining/remaining Overview authority and complete fixed
Daytona shell/resource acceptance. No draining count is fabricated from Target scheduling state.
ADMIN-M3 usage/lifecycle qualification and ADMIN-M4 independent OIDC, real Docker/Kubernetes/SSH
deployment/zero-residue cleanup and Codex/Claude Code flows remain unqualified by this slice.
No new user credential, permission, or pre-existing-resource cleanup approval is needed for the
next source slice. Goal remains active.
