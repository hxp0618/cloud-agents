# M2 Environment Profile Draft E2E

Date: 2026-09-04

Status: passed for the M2 immutable Environment Profile draft slice only. This does not complete M2 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `873df0d`.
- Admin Web: independent Vite application on `127.0.0.1:4174`, proxying `/v1/admin` to Control Plane.
- Control Plane: local-development binary on `127.0.0.1:18082`.
- Database: fresh PostgreSQL 17.6 with product migration result `schema_head=000041`, `applied=41`, `no_op=false`.
- Locked validation toolchain: Go `1.26.6`, Node.js `24.18.1`, Bun `1.3.14`, Python `3.14.7`, and uv `0.12.5`.

## Contract and authority

- `EnvironmentProfile` is a project-scoped, immutable version resource. This slice admits only `draft` creation plus list/get reads.
- The persisted spec contains provider kinds, CPU/memory limits, Storage/Network policy references, fixed release digest, Target references, and an opaque Provider credential reference.
- Admin routes require `profiles.list`, `profiles.create`, `profiles.get`, or `audit.list` in addition to project authority. The local ordinary-user token received HTTP `403` from the same collection route.
- Creation writes one profile row and one `profile.create` Audit row in the same database transaction. The runtime login can call the bounded function but cannot directly update the immutable table.

## Real HTTP and PostgreSQL verification

The repeatable local harness built the migration and Control Plane binaries, started a fresh PostgreSQL container, applied all 41 product migrations, created a real project, and called the generated Admin SDK contract over HTTP.

```text
migration={"schema_head":"000041","applied":41,"no_op":false}
create:201 replay:201 conflict:409 list:200 get:200 audit:200 user:403
profile=ep-a9d9ab4566e86ed4401022bf57655957/1 status=draft resourceVersion=1
authority=profiles:1 audit:1 migrations:41 runtimeUpdate:denied
```

- Replaying the same body and `Idempotency-Key` returned a byte-identical HTTP `201` response.
- Reusing that key with a changed description returned HTTP `409` and did not add a profile or Audit row.
- List, detail, and profile Audit returned HTTP `200`; an ordinary user token returned HTTP `403`.
- The first live Audit request exposed and then verified the root-cause fix for a consumed verified principal: detail and Audit now receive independently verified principals.

## Browser verification

- A real in-app browser first connected with the ordinary-user token and rendered `This token is valid but lacks the required Admin API scope or project authority.` with `Connection failed`.
- A refreshed ephemeral admin token connected to the same real project. The Environment Profiles page initially showed zero versions.
- The create Sheet submitted `development-browser` version `1`, both Codex and Claude Code, `2000` mCPU, `4096` MiB, Storage/Network policy references, a fixed SHA-256 release digest, two Target references, and opaque Provider credential reference `provider-default`.
- Control Plane returned the persisted draft `ep-8537601978457e3974907c80ffe2a6cd`. The table count changed to one, the detail Sheet showed resource version `1`, and Audit showed one succeeded `profile.create` event.
- Closing and reopening the detail after re-authentication reloaded both the profile and Audit from Control Plane. Page-origin console errors were empty.
- Browser resource entries used only origin `http://127.0.0.1:4174` and `/v1/admin/...` paths. No request connected directly to Docker, Kubernetes, or SSH.
- Local storage contained only `cloud-agents-admin-theme`. Session storage contained only `cloud-agents.admin-web.connection.v1` with fields `endpoint`, `projectId`, and `tenantId`. A value scan found no bearer/JWT, `credentialRef`, `providerCredentialRef`, kubeconfig, SSH/Target reference, or policy reference.
- The bearer was cleared from React form state after connection and was never written to browser storage. Re-authentication after the short-lived local token rotated confirmed the memory-only behavior.

## Source verification

```text
bun scripts/generate-platform-json-sdks.ts --check                              PASS
bun scripts/check-platform-contract-standards.ts                                PASS, 117 schemas and 61 OpenAPI operations
bun --filter @cloud-agents/cloud-agent-platform-sdk test                        PASS, 31 tests
bun --filter @cloud-agents/cloud-agent-platform-sdk typecheck                   PASS
bun --filter @cloud-agents/cloud-agent-platform-sdk build                       PASS
bun --filter @cloud-agents/admin-web test                                       PASS, 6 tests
bun --filter @cloud-agents/admin-web typecheck                                  PASS
bun --filter @cloud-agents/admin-web build                                      PASS
bun test scripts/lib/platform-migration-sql.test.ts scripts/lib/platform-release.test.ts PASS, 44 tests
go test ./services/control-plane/internal/environmentprofile ./services/control-plane/internal/server ./services/control-plane/internal/store/postgres ./services/control-plane/internal/localmigration ./services/control-plane/internal/authn ./services/control-plane/cmd/cloud-agents-product-migrate ./services/control-plane/cmd/cloud-agents-control-plane ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1 PASS
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane ./services/control-plane/cmd/cloud-agents-local-migrate PASS
bun run platform:sdk:check                                                      PASS with locked toolchain
bun run platform:sdk:consumers                                                  PASS with fresh TypeScript and Go consumers
bun run typecheck                                                               PASS
bun run lint                                                                    PASS
bun run secret:scan                                                             PASS
git diff --check                                                                PASS
```

The full `bun run fmt:check` still reports only `apps/admin-web/visual-baseline/daytona-v0.190.0/actual/browser-evidence.json`, which is unchanged from the starting HEAD. Slice-owned handwritten TypeScript, TSX, and CSS pass focused `oxfmt --check`; generated OpenAPI and JSON Schemas pass their generator drift and contract-standard checks.

## Teardown and evidence boundary

- Admin Web, Control Plane, PostgreSQL, and the one-shot local token bridge were stopped. Ports `4174`, `18082`, and `18083` had no listeners, and the label-filtered PostgreSQL container inventory was empty.
- This proves real immutable draft create/list/get, idempotency conflict, project-scoped Admin authorization, generated SDK consumption, durable Audit, browser rendering, and browser-to-Control-Plane isolation.
- Profile publish/disable transitions, published-only User API summaries, the User Web selector, server-side Profile resolution during environment creation, and removal of legacy User Web infrastructure inputs remain unimplemented and unproven.
