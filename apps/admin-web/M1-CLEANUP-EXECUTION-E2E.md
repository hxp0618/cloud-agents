# M1 Admin Target Cleanup Execution E2E

Date: 2026-09-03

Status: passed for the M1 confirmed Target Cleanup execution slice only. This does not complete M1 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `51ecbae5bdf8a810914df15c3896f4af333a5045`.
- Admin Web: independent Vite application on `127.0.0.1:4174`, proxying `/v1/admin` to Control Plane.
- Control Plane and Worker: local-development binaries on `127.0.0.1:18081` and `127.0.0.1:18091`.
- Database: fresh PostgreSQL with product migration result `schema_head=000040`, `applied=40`, `no_op=false`.
- Runtime authority: OrbStack Docker API through a temporary one-day mTLS forwarder on `127.0.0.1:18443`. Certificate and key bytes remained in the Control Plane credential directory.
- Toolchain: Go `1.26.6`, Node.js `24.18.1`, and Bun `1.3.14` for deterministic gates. The live Vite run used compatible Node.js `24.19.0` and Bun `1.3.14`.

## Live authority and authorization

- Tenant: `tenant-local`.
- Project: `project-c6cf4017b79a2f441527ea7cb81c229c`.
- Target: `docker-cleanup-exec-e2e`, generation `1`, resource version `3`.
- Real Probe authority: Docker API `1.54`, engine `29.4.0`, platform `linux/arm64`.
- An ordinary user token received HTTP `403` and `AUTHORIZATION_DENIED` from both the Admin cleanup-preview and cleanup POST routes. The owned container and volume remained present after the denied POST.
- Admin cleanup responses, Operation pages, and Audit pages contained no Target endpoint, deployment credential reference, Provider credential reference, Worker credential reference, private key, or credential bytes.

## Confirmed cleanup execution

The first owned orphan fixture used:

- Lease: `lease-cleanup-exec-e2e`, generation `1`.
- Container: `cloud-agents-664c2b3c12ce9403`.
- Workspace volume: `cloud-agents-664c2b3c12ce9403-workspace`.
- Preview fence: generation `1`, resource version `3`, impact digest `sha256:afee142c35558f4f9bafd5b7ac4b30b130ba7257e4073b46a94b0aefcbc5589d`.

The Admin POST with the same generation/resource version but a different digest returned HTTP `409` and `CLEANUP_IMPACT_CONFLICT`. Both resources remained. Control Plane persisted failed Operation `op-5c2e7e32bd9a159f49e368cb92120c1e` plus requested/failed Audit events with stable error `target-cleanup-impact-conflict`.

The Admin POST with the exact preview body returned HTTP `200` and succeeded Operation `op-d945138d7fddb2d3c9cc4c32ed74fb7f`, with impact summary `Cleaned 1 orphan workers and 2 resources`. Exact Docker inventory then showed zero matching containers and volumes.

Replaying the same body and `Idempotency-Key` returned the byte-identical Operation, including the same operation ID and timestamps. Docker remained empty and no duplicate terminal Audit event was created. A fresh preview returned `workers: []` and a new empty-impact digest.

## Browser verification

- A real Brave tab connected to the Admin Web with the ephemeral local admin token. The token stayed in React memory; the browser password-save prompt was explicitly rejected.
- The Target detail showed the existing failed and successful Cleanup Operations and their Audit records.
- Preview rendered an exact confirmation Sheet for `cloud-agents-aedb7f08fe1484ea`, lease `lease-cleanup-browser-e2e`, generation `1`, resource version `3`, and its container/workspace-volume names.
- `Confirm cleanup` was disabled before the acknowledgement checkbox and enabled after it was checked. Submitting produced succeeded Operation `op-4228535a2d604c7834b36244d94a091d`, refreshed Operations/Audit, and removed the exact two Docker resources.
- A second browser-owned fixture, `cloud-agents-7aa8e08f5c2b01af`, exercised the same flow with CDP network capture enabled. The captured requests were only the Admin Web origin's cleanup-preview GET, cleanup POST, Operations GET, and Audit GET under `http://127.0.0.1:4174/v1/admin/...`; there was no browser connection to `18443` or any Docker API path.
- Session storage contained only the saved Admin Web connection (`endpoint`, tenant ID, project ID) plus extension metadata. Local storage contained the theme and extension metadata. A value scan found no bearer/JWT value, `credentialRef`, `providerCredentialRef`, private-key marker, or `18443` endpoint.
- Page-origin console warnings/errors were empty. Injected password-manager extension messages were outside the application origin.
- The Cleanup Sheet was verified at the default desktop viewport. M4 still owns the complete light/dark and desktop/mobile visual regression matrix.

## Source verification

```text
bun scripts/generate-platform-json-sdks.ts --check                              PASS
bun scripts/check-platform-contract-standards.ts                                PASS, 114 schemas and 57 OpenAPI operations
bun --filter @cloud-agents/cloud-agent-platform-sdk test                        PASS, 30 tests
bun --filter @cloud-agents/cloud-agent-platform-sdk typecheck                   PASS
bun --filter @cloud-agents/cloud-agent-platform-sdk build                       PASS
bun --filter @cloud-agents/admin-web test                                       PASS, 5 tests
bun --filter @cloud-agents/admin-web typecheck                                  PASS
bun --filter @cloud-agents/admin-web build                                      PASS
bun test scripts/lib/platform-migration-sql.test.ts scripts/lib/platform-release.test.ts PASS, 43 tests
go test ./services/control-plane/internal/deploymenttarget ./services/control-plane/internal/server ./services/control-plane/internal/store/postgres ./services/control-plane/internal/localmigration PASS
go test ./services/control-plane/cmd/cloud-agents-product-migrate ./services/control-plane/cmd/cloud-agents-control-plane PASS
go test ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1             PASS
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane ./services/control-plane/cmd/cloud-agents-local-migrate PASS
bun run platform:sdk:check                                                      PASS with pinned toolchain
bun run platform:sdk:consumers                                                  PASS with fresh TypeScript and Go consumers
bun run typecheck                                                               PASS
bun run lint                                                                    PASS
```

The full `bun run fmt:check` still reports only `apps/admin-web/visual-baseline/daytona-v0.190.0/actual/browser-evidence.json`, which is unchanged from the starting HEAD. All slice-owned TypeScript/TSX files pass focused `oxfmt --check`, and `git diff --check` passes.

## Teardown and evidence boundary

- The final server-authoritative preview returned `workers: []`.
- Label-filtered Docker inventories contained zero platform-owned containers and zero platform-owned volumes for the Target.
- Admin Web, Control Plane, Worker, PostgreSQL, and the mTLS forwarder were stopped. Ports `18081`, `18091`, `18443`, and `4174` had no listeners, and the temporary PostgreSQL container was absent.
- The isolated certificate and credential directory was moved to `/Users/huang/.Trash/cloud-agents-admin-cleanup-exec-e2e-20260903-LI7VFY` and remains recoverable.
- This proves confirmed Docker Target Cleanup execution, conflict fencing, idempotent replay, durable Operation/Audit, and the browser-to-Control-Plane boundary. Kubernetes/SSH live Cleanup, Profiles, Worker lifecycle management, and the M4 Provider E2E matrix remain unproven.
