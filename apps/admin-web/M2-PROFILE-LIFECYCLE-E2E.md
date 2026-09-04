# M2 Environment Profile Lifecycle E2E

Date: 2026-09-04

Status: passed for the M2 draft-to-published-to-disabled lifecycle slice only. This does not complete M2 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `f328539`.
- Admin Web: independent Vite application on `127.0.0.1:4174`, proxying `/v1/admin` to Control Plane.
- Control Plane: local-development binary on `127.0.0.1:18082` with a Worker on `127.0.0.1:18092`.
- Database: fresh PostgreSQL 17.6 with product migration result `schema_head=000042`, `applied=42`, `no_op=false`.
- Locked validation toolchain: Go `1.26.6`, Node.js `24.18.1`, Bun `1.3.14`, Python `3.14.7`, and uv `0.12.5`.

The stack was started from the current checkout with `scripts/cloud-agents-dev.sh`; a real project and Docker Target record were created through Control Plane before exercising Profile lifecycle operations.

## Contract, authority, and persistence

- Publish and disable are explicit Admin API transitions guarded by `profiles.act`, project authority, `expectedResourceVersion`, and an `Idempotency-Key`.
- The persisted state machine is `draft -> published -> disabled`. Published and disabled timestamps are database facts, and a disabled version cannot be republished with a new key.
- Publish verifies every referenced Target exists in the same tenant and project before changing state.
- Each successful transition updates the Profile and appends its `profile.publish` or `profile.disable` Audit fact in the same database transaction.
- The runtime login has `SELECT` plus bounded function execution but no direct table `UPDATE` privilege.

The generated TypeScript SDK drove a second Profile through the live HTTP server and returned this safe summary:

```json
{
  "created": ["draft", "1"],
  "createReplayEqual": true,
  "createConflict": 409,
  "published": ["published", "2"],
  "publishReplay": ["published", "2"],
  "disabled": ["disabled", "3"],
  "disableReplay": ["disabled", "3"],
  "historicalPublishReplay": ["disabled", "3"],
  "changedReplay": 409,
  "invalidTransition": 409,
  "userForbidden": 403,
  "auditActions": ["profile.create", "profile.disable", "profile.publish"]
}
```

The historical publish replay is intentionally the current disabled snapshot: the original key remains idempotent after a later valid state transition. Reusing that key with a changed request, or attempting a new transition from disabled, returned HTTP `409`. The ordinary User token returned HTTP `403` before lifecycle evaluation.

PostgreSQL verification after both the SDK and browser runs:

```text
api-lifecycle:disabled:rv=3:published=true:disabled=true
browser-lifecycle:disabled:rv=3:published=true:disabled=true
profile.create=2
profile.disable=2
profile.publish=2
42:head=000042
runtime_update=false
direct_update_exit=1
ERROR:  permission denied for table environment_profiles
description_unchanged=true
```

## Real browser verification

- The in-app browser connected with a fresh local Admin token and loaded real Target, Lease, and Profile authority through Control Plane.
- The Profile Sheet created `browser-lifecycle` version `1` with Codex and Claude Code, `2000` mCPU, `4096` MiB, Storage/Network policy references, a fixed SHA-256 release digest, a real project Target reference, and an opaque Provider credential reference.
- The draft detail showed resource version `1` and one `profile.create` Audit fact.
- The publish confirmation displayed the actual `profiles.act` scope and expected resource version `1`. Its submit button was disabled before impact acknowledgement and enabled only after the checkbox was selected.
- Publish returned `published`, resource version `2`, a persisted publish timestamp, and two Audit facts.
- The disable confirmation repeated the acknowledgement gate with expected resource version `2`. Disable returned `disabled`, resource version `3`, a persisted disable timestamp, and three Audit facts.
- A disabled version exposed no further lifecycle action.

A separate real browser session returned only this non-secret boundary summary from page APIs:

```text
adminRequestCount=5
adminPaths=/v1/admin/tenants/.../deployment-targets
           /v1/admin/tenants/.../deployment-targets/docker-browser-lifecycle
           /v1/admin/tenants/.../environment-leases
           /v1/admin/tenants/.../environment-profiles
           /v1/admin/tenants/.../environment-profiles/browser-lifecycle/versions/1
directInfrastructureRequests=[]
localKeys=[cloud-agents-admin-theme]
sessionKeys=[cloud-agents.admin-web.connection.v1]
sensitiveStorage=false
```

The page-origin console contained only Vite connection and React development notices; there were no warnings or errors. Request headers and storage values were not captured as evidence.

## Source verification

```text
bun scripts/generate-platform-json-sdks.ts --check                              PASS
bun scripts/check-platform-contract-standards.ts                                PASS, 118 schemas and 63 OpenAPI operations
bun --filter @cloud-agents/cloud-agent-platform-sdk test                        PASS, 31 tests
bun --filter @cloud-agents/cloud-agent-platform-sdk typecheck                   PASS
bun --filter @cloud-agents/cloud-agent-platform-sdk build                       PASS
bun --filter @cloud-agents/admin-web test                                       PASS, 6 tests
bun --filter @cloud-agents/admin-web typecheck                                  PASS
bun --filter @cloud-agents/admin-web build                                      PASS
bun test scripts/lib/platform-migration-sql.test.ts scripts/lib/platform-release.test.ts PASS, 45 tests
go test ./services/control-plane/internal/environmentprofile ./services/control-plane/internal/server ./services/control-plane/internal/store/postgres ./services/control-plane/internal/localmigration ./services/control-plane/internal/authn ./services/control-plane/cmd/cloud-agents-product-migrate ./services/control-plane/cmd/cloud-agents-control-plane ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1 PASS
go test -tags=localdev ./services/control-plane/internal/authn ./services/control-plane/cmd/cloud-agents-control-plane ./services/control-plane/cmd/cloud-agents-local-migrate PASS
bun run platform:sdk:check                                                      PASS
bun run platform:sdk:consumers                                                  PASS with fresh TypeScript and Go consumers
bun run typecheck                                                               PASS
bun run lint                                                                    PASS
bun run secret:scan                                                             PASS
git diff --check                                                                PASS
```

The full `bun run fmt:check` still reports only `apps/admin-web/visual-baseline/daytona-v0.190.0/actual/browser-evidence.json`, which is unchanged from the starting HEAD. Slice-owned handwritten TypeScript and TSX pass focused `oxfmt --check`; generated OpenAPI, JSON Schemas, SDKs, and migration products pass their generator and contract drift checks.

## Teardown and evidence boundary

- The Admin Web, one-shot local token bridge, Control Plane, Worker, and PostgreSQL were stopped.
- Ports `4174`, `18082`, `18083`, and `18092` had no listeners, the label-filtered development container count was zero, and the owned state directory was removed.
- This proves the real Profile publish/disable authority, optimistic concurrency, idempotency behavior, durable Audit, generated SDK, Admin Web confirmation interaction, ordinary-user denial, and browser-to-Control-Plane boundary.
- Published-only redacted User API summaries, the User Web selector, server-side Profile resolution during environment creation, and removal of legacy User Web infrastructure inputs remain unimplemented and unproven.
