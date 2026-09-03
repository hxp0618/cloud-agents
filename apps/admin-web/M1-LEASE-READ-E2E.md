# M1 Admin Environment Lease Read E2E

Date: 2026-09-03

Status: passed for the M1 Environment Lease list/detail slice only.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `b1683720297b7a9f696b5643d7451b991c811db6`.
- Admin Web: independent Vite application on `127.0.0.1:4174`, proxying `/v1/admin` to Control Plane.
- Control Plane: local development binary on `127.0.0.1:18081` with separate user and admin token files.
- Worker: local development binary on `127.0.0.1:18091`.
- Runtime used to establish the Target authority: OrbStack Docker API through a temporary one-day mTLS forwarder on `127.0.0.1:18443`.
- Toolchains used for source gates: Bun `1.3.14`, Node.js `24.18.1`, Go `1.26.6`, Python `3.14.7`, and uv `0.12.5`.

## Live authority and authorization

- Tenant: `tenant-local`.
- Project: `project-ad5ae78b37d9d6b8c6681ba4bc4a0f13`.
- Target: `docker-lease-e2e`.
- Lease: `lease-admin-e2e`.
- Admin Target registration returned HTTP `201`; the real OrbStack Probe returned HTTP `200`, phase `ready`, Docker API `1.54`, engine `29.4.0`, platform `linux/arm64`, and resource version `3`.
- The existing managed-host create API persisted the Lease with target generation `1`, release digest, provider credential reference, `1000` mCPU, `512` MiB, and phase `provisioning`.
- An ordinary local user token called `GET /v1/admin/.../environment-leases` and received HTTP `403` with stable problem code `AUTHORIZATION_DENIED`; the handler test verifies this rejection happens before a store call.
- The separate admin token called the Admin list and detail routes and received HTTP `200` from PostgreSQL-backed authority. The responses contained lifecycle and infrastructure references but no conversation, Prompt, code, Workspace, Artifact, Provider credential bytes, or deployment credential bytes.
- The Admin routes require the separate verifier-v1-compatible scopes `leases.list` and `leases.get`. POST create/terminate/upgrade requests remain closed on this Admin handler.

## Browser verification

- The real Admin Web rendered one Target and one Lease on Overview from Admin API responses.
- The Environment Leases page rendered the persisted list plus detail fields for desired/observed/cleanup phases, generation/resource version, Target, release digest, resource limits, provider credential reference, Worker readiness, and expiry.
- Clicking **Refresh** completed through Control Plane and displayed `Authority refresh completed.`.
- The test-owned Lease was terminated through the existing managed-host API. A subsequent Admin Web refresh changed the list and detail to generation `2`, resource version `3`, observed phase `terminated`, and cleanup phase `complete`.
- Disconnecting restored only endpoint, tenant ID, and project ID on the connection form; the bearer field remained empty.
- Browser console errors and warnings were empty. The Lease list/detail page was visually checked at desktop size and `390x844`.

## Source verification

```text
bun scripts/generate-platform-json-sdks.ts --check                              PASS
bun --filter @cloud-agents/admin-web test                                       3 passed
bun --filter @cloud-agents/admin-web typecheck                                  PASS
bun --filter @cloud-agents/admin-web build                                      PASS
bun --filter @cloud-agents/cloud-agent-platform-sdk test                        27 passed
bun --filter @cloud-agents/cloud-agent-platform-sdk typecheck                   PASS
go test ./sdk/go/gen/openapi/v1alpha1                                           PASS
go test ./services/control-plane/internal/server                                PASS
go test ./services/control-plane/cmd/cloud-agents-control-plane                 PASS
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane  PASS
go test -tags=localdev ./services/control-plane/internal/authn -run TestLocalVerifierSeparatesAdminScopes PASS
bun scripts/check-platform-contract-standards.ts                                PASS, 53 OpenAPI operations
oxlint affected paths --deny-warnings                                           PASS
bun run fmt:check                                                               PASS, 161 files
bun run lint                                                                    PASS
bun run platform:sdk:check                                                      PASS, all generated SDKs current
bun run platform:sdk:consumers                                                  PASS, fresh TypeScript and Go consumers
bun run typecheck                                                               PASS, including Admin Web and User Web
bun --filter @cloud-agents/user-web build                                       PASS
```

The full `internal/authn` package retains the starting-HEAD authority-call-graph freeze failure documented by the preceding Target slice. This slice does not change that production call graph or its frozen golden list.

## Teardown and evidence boundary

- The test-owned Lease reached `terminated/complete` before teardown.
- Admin Web, Control Plane, Worker, PostgreSQL, and the mTLS forwarder were stopped.
- Ports `18081`, `18091`, `18182`, `18443`, and `4174` had no listeners afterward.
- No `cloud-agents-dev-*` container and no container or volume labelled for `lease-admin-e2e` or `docker-lease-e2e` remained.
- The isolated certificate and credential directory was moved to `~/.Trash/cloud-agents-admin-lease-e2e-20260903-kdG2yL` and is recoverable.
- The local development Lease handler deliberately has no environment actuator, so this run proves persisted Lease creation and Admin list/detail/status feedback, not Worker deployment. It does not prove Kubernetes or SSH Targets, Admin Terminate/Upgrade, Target Cleanup, durable Operation/Audit views, Profiles, Worker lifecycle management, or the M4 Provider E2E matrix.
