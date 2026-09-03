# M1 Admin Target OrbStack E2E

Date: 2026-09-03

Status: passed for the M1 Deployment Target registration and Probe slice only.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `df82f9d2a07a58c003ec37d8248d1b9d7abbc67d`.
- Admin Web: independent Vite application on `127.0.0.1:4174`, proxying only `/v1/admin` to Control Plane.
- Control Plane: local development binary on `127.0.0.1:18081` with separate user and admin token files.
- Worker: local development binary on `127.0.0.1:18091`.
- Runtime: OrbStack Docker API through a temporary one-day mTLS forwarder on `127.0.0.1:18443`; credential bytes stayed in an isolated Control Plane credential directory.
- Toolchains used for source gates: Bun `1.3.14`, Node.js `24.18.1`, Go `1.26.6`, Python `3.14.7`, and uv `0.12.5`.

## Live authority and authorization

- Tenant: `tenant-local`.
- Project: `project-09b575e8eae8539cef034af940c91d09`.
- Target: `docker-admin-e2e`.
- An ordinary local user token called the Admin Target list route and received HTTP `403` with stable problem code `AUTHORIZATION_DENIED`.
- The separate admin token called the same route and received HTTP `200` with an initially empty authoritative list.
- The four routes require separate verifier-v1-compatible scopes: `targets.list`, `targets.get`, `targets.create`, and `targets.act`. Adopting the newer `target.write` / `target.probe` vocabulary requires a separately versioned Identity Verifier profile; the immutable v1 profile was not rewritten in this slice.
- Admin Target registration returned HTTP `201` and resource version `1`.
- Admin Probe returned HTTP `200`, phase `ready`, Docker API `1.54`, engine `29.4.0`, platform `linux/arm64`, and resource version `3`.
- Admin detail and list calls returned the persisted Target rather than frontend fixture data.

## Browser verification

- The real Admin Web connected with the ephemeral admin token and rendered one total Target, one ready Target, and zero Targets needing attention from Admin API responses.
- Target detail rendered the endpoint, opaque credential reference, generation, API version, engine version, platform, and last probe time returned by Control Plane.
- Clicking **Run probe** completed against OrbStack and advanced the persisted Target resource version from `3` to `5`.
- The registration dialog exposed Docker, Kubernetes, and SSH kinds but submitted through the generated Admin API client only.
- `sessionStorage` contained only `cloud-agents.admin-web.connection.v1` with endpoint, tenant ID, and project ID. It contained no bearer token or target credential reference.
- Browser resource timing contained only the Admin Target list, detail, and probe URLs. It contained no direct request to the Docker endpoint on port `18443`.
- Browser console errors and warnings were empty. Desktop and `390x844` responsive layouts were visually checked.

## Source verification

```text
bun scripts/generate-platform-json-sdks.ts --check                         PASS
bun --filter @cloud-agents/admin-web test                                  2 passed
bun --filter @cloud-agents/admin-web typecheck                             PASS
bun --filter @cloud-agents/admin-web build                                 PASS
bun --filter @cloud-agents/cloud-agent-platform-sdk test                   27 passed
go test ./sdk/go/gen/openapi/v1alpha1                                      PASS
go test ./services/control-plane/internal/server                           PASS
go test ./services/control-plane/cmd/cloud-agents-control-plane            PASS
go test -tags=localdev ./services/control-plane/cmd/cloud-agents-control-plane PASS
bun scripts/check-platform-contract-standards.ts                           PASS, 51 OpenAPI operations
```

The full `internal/authn` package retains a pre-existing authority-call-graph freeze failure from the starting HEAD: production `ListDeploymentTargets` and `BeginManagedHostEnvironmentLeaseUpgrade` calls are present but absent from that test's golden list. This slice did not modify either production call path or the golden file. The focused local token separation test passed.

## Teardown and evidence boundary

- The Admin Web, Control Plane, Worker, PostgreSQL stack, and mTLS forwarder were stopped.
- Ports `18081`, `18091`, `18443`, and `4174` had no listeners afterward.
- No `cloud-agents-dev-*` container remained, and the isolated temporary credential/test directory was moved to Trash.
- This evidence proves a real Docker Target register/list/detail/probe path and ordinary-token `403`. Admin Cleanup is deliberately not exposed until its contract can return the affected Worker/container/Pod/volume set and record Operation/Audit state. This does not prove Kubernetes or SSH Targets, Lease operations, durable Operation/Audit views, Profiles, Worker lifecycle management, or the M4 provider E2E matrix.
