# M1 Admin Target Cleanup Preview E2E

Date: 2026-09-03

Status: passed for the M1 Target Cleanup impact-preview slice only. Cleanup execution remains closed until durable Operation and Audit authority exists.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `0a44c77377551c4035b8ded9ced04adf33805c0d`.
- Admin Web: independent Vite application on `127.0.0.1:4174`, proxying `/v1/admin` to Control Plane.
- Control Plane: local development binary on `127.0.0.1:18081` with separate user and admin token files.
- Runtime authority: OrbStack Docker API through a temporary one-day mTLS forwarder on `127.0.0.1:18443`; credential bytes stayed in an isolated Control Plane credential directory.
- The live stack used Bun `1.3.14`, Go `1.26.6`, and compatible Node.js `24.19.0`. The deterministic SDK gate was separately replayed with repository-pinned Node.js `24.18.1`, Bun `1.3.14`, and Go `1.26.6`.

## Live authority and authorization

- Tenant: `tenant-local`.
- Project: `project-1884750648f30f1fbb70d5be24c969ab`.
- Target: `docker-cleanup-preview-e2e`, generation `1`, resource version `3`.
- Lease: `lease-cleanup-preview-e2e`, generation `1` while active.
- Worker container: `cloud-agents-2942b630f0a327f8`.
- Workspace volume: `cloud-agents-2942b630f0a327f8-workspace`.
- Admin registration returned a persisted Target. The real Probe returned phase `ready`, Docker API `1.54`, engine `29.4.0`, and platform `linux/arm64`.
- An ordinary local user token called the Admin cleanup-preview route and received HTTP `403` with stable problem code `AUTHORIZATION_DENIED`.
- Before the Lease existed, the Admin preview returned HTTP `200`, `canCleanup: true`, disposition `cleanup`, and the exact live container and workspace volume names.
- After the exact active Lease was persisted, the same preview returned `canCleanup: false` and disposition `blocked` for the same resources.
- After the test-owned Lease reached generation `2`, desired/observed phase `terminated`, and cleanup phase `complete`, the preview returned to `canCleanup: true` and disposition `cleanup`.
- The successful response carried `X-Resource-Version: 3` and `ETag: "3"`. Its body contained no Target endpoint, deployment credential reference, Provider credential reference, Worker credential reference, private key, or credential bytes.
- Removing the test-owned container and volume made the live preview return `workers: []`; no cached resource inventory was used.

## Browser verification

- The real Admin Web rendered both `No active Lease blockers` and `Blocked by active Lease` from successive generated Admin SDK responses.
- The detail view rendered the fencing pair `g1 · rv3`, Worker/Lease identity, and exact container and workspace-volume impact.
- The page states explicitly that execution remains closed until durable Operation and Audit authority is available; no Cleanup button performs deletion.
- Per-action CDP capture showed the browser request only
  `http://127.0.0.1:4174/v1/admin/tenants/tenant-local/projects/project-1884750648f30f1fbb70d5be24c969ab/deployment-targets/docker-cleanup-preview-e2e:cleanup-preview`.
  No browser request connected directly to port `18443`.
- `sessionStorage` contained only `cloud-agents.admin-web.connection.v1` with endpoint, tenant ID, and project ID. `localStorage` was empty and neither storage contained the bearer token.
- Browser console errors and warnings were empty after the successful flow. Desktop and `390x844` responsive layouts were visually checked.

## Source verification

```text
bun scripts/generate-platform-json-sdks.ts --check                              PASS
bun --filter @cloud-agents/admin-web test                                       3 passed
bun --filter @cloud-agents/cloud-agent-platform-sdk test                        28 passed
bun --filter @cloud-agents/cloud-agent-platform-sdk typecheck                   PASS
bun --filter @cloud-agents/admin-web build                                      PASS
bun --filter @cloud-agents/user-web build                                       PASS
bun run typecheck                                                               PASS
bun run fmt:check                                                               PASS, 161 files
bun run lint                                                                    PASS
bun run platform:sdk:check                                                      PASS with exact pinned toolchain
bun run platform:sdk:consumers                                                  PASS, fresh TypeScript and Go consumers
bun scripts/check-platform-contract-standards.ts                                PASS, 54 OpenAPI operations
go test ./services/control-plane/internal/dockertarget                           PASS
go test ./services/control-plane/internal/kubernetestarget                       PASS
go test ./services/control-plane/internal/sshtarget                              PASS
go test ./services/control-plane/internal/server                                 PASS
go test ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1             PASS
go test ./services/control-plane/cmd/cloud-agents-control-plane                  PASS
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane   PASS
go test -tags=localdev ./services/control-plane/internal/authn -run TestLocalVerifierSeparatesAdminScopes PASS
```

The full `internal/authn` package retains the starting-HEAD authority-call-graph freeze failure: production `ListDeploymentTargets` and `BeginManagedHostEnvironmentLeaseUpgrade` calls are absent from that test's golden list. This slice changes neither production call path nor the golden file.

## Teardown and evidence boundary

- The test-owned Lease reached `terminated/complete` before infrastructure teardown.
- The test-owned Worker container and workspace volume were removed; subsequent label-filtered container and volume inventories were empty.
- Admin Web, Control Plane, Worker, PostgreSQL, and the mTLS forwarder were stopped. Ports `18081`, `18091`, `18443`, and `4174` had no listeners, and no `cloud-agents-dev-*` container remained.
- The isolated certificate and credential directory was moved to `~/.Trash/cloud-agents-admin-cleanup-preview-e2e-20260903-3Dl9bN` and is recoverable.
- This proves a real Docker cleanup impact preview and active-Lease blocker, not Cleanup execution. Kubernetes/SSH live preview, durable Operation/Audit, Profiles, Worker lifecycle management, and the M4 Provider E2E matrix remain unproven.
