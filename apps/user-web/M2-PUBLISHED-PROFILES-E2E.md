# M2 Published Environment Profile User API E2E

Date: 2026-09-04

Status: passed for the published-only, schedulable, redacted User API slice. This does not complete M2 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `361034f`.
- Control Plane: local-development binary on `127.0.0.1:18082` with a Worker on `127.0.0.1:18092`.
- Database: fresh PostgreSQL `17.6`, migrated from the current checkout to `schema_head=000042`, `applied=42`.
- Target: the real local OrbStack Docker Engine, reached only by Control Plane through a one-use mTLS proxy on `127.0.0.1:18443`.
- Locked toolchain: Go `1.26.6`, Node.js `24.18.1`, Bun `1.3.14`, Python `3.14.7`, and uv `0.12.5`.

## Contract and authority

- `GET /v1/tenants/{tenantId}/projects/{projectId}/environment-profiles` requires both project access and the separate `environment-profiles.list` User scope.
- The store query projects only Profile identity, description, provider kinds, CPU, and memory. It does not project Target, endpoint, credential, release, or policy fields.
- A Profile is returned only while its persisted status is `published` and at least one referenced Target in the same tenant/project is currently `ready`.
- The public response encoder emits only `EnvironmentProfileSummary`; the TypeScript and Go generated SDK decoders reject infrastructure fields on that model.
- The existing Admin Profile API remains protected by Admin-only `profiles.*` scopes. An ordinary User token received HTTP `403` from the Admin list route.

## Real HTTP and generated SDK verification

The test registered two Docker Targets through the Admin API. `docker-summary-ready` was probed through Control Plane against the real Docker Engine; `docker-summary-unready` intentionally remained unprobed.

```text
docker-summary-ready:ready
Docker API version: 1.54
Docker Engine version: 29.4.0
Docker OS/architecture: linux/arm64
docker-summary-unready:unprobed
```

The generated TypeScript SDK created and published Profiles against both Targets, retained a third Profile as a draft, and listed the User API. Only the Profile backed by the ready Target was visible. After disabling it through the Admin API, the User API returned an empty page.

```json
{
  "visibleBeforeDisable": [
    {
      "profileId": "summary-raw-ready",
      "version": 1,
      "status": "published",
      "availability": "available"
    }
  ],
  "visibleAfterDisable": 0,
  "forbiddenFields": [],
  "rawForbiddenFields": [],
  "userAdminStatus": 403
}
```

The second generated-SDK run persisted these control cases:

```text
summary-raw-draft:draft:docker-summary-ready
summary-raw-ready:disabled:docker-summary-ready
summary-raw-unready:published:docker-summary-unready
```

`rawForbiddenFields` was checked against the original HTTP response bytes, not only the SDK projection. The denied Admin request was made with the same ordinary User token used by the successful User API request.

## Source verification

```text
bun run platform:contracts:check                                             PASS, 120 schemas and 64 OpenAPI operations
bun scripts/generate-platform-json-sdks.ts --check                           PASS
bun test scripts/lib/platform-json-sdk.test.ts                               PASS, 3 tests
bun --filter @cloud-agents/cloud-agent-platform-sdk test                     PASS, 32 tests
bun --filter @cloud-agents/cloud-agent-platform-sdk build                    PASS
bun run platform:sdk:check                                                   PASS
bun run platform:sdk:consumers                                               PASS with fresh TypeScript and Go consumers
bun run typecheck                                                            PASS
bun run lint                                                                 PASS
bun run secret:scan                                                          PASS
go test ./services/control-plane/internal/environmentprofile ./services/control-plane/internal/store/postgres ./services/control-plane/internal/server PASS
go test -tags=localdev ./services/control-plane/internal/authn ./services/control-plane/cmd/cloud-agents-control-plane PASS
go test ./services/control-plane/cmd/cloud-agents-control-plane              PASS
go test ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1          PASS
git diff --check                                                             PASS
```

A broader `go test ./services/control-plane/... ./sdk/go/...` diagnostic is not claimed as passing: the unchanged migration evidence package exceeded its existing quota fixtures and 10-minute timeout, and the unchanged CLI Artifact download test expects no `Content-Disposition` while the SDK requires one. Neither failing path is used by or modified for this slice; all slice-owned and direct-consumer packages above passed.

## Teardown and evidence boundary

- Control Plane, Worker, PostgreSQL, and the mTLS proxy were stopped.
- Ports `18082`, `18092`, and `18443` had no listeners, the label-filtered development container count was zero, and the owned development state directory was removed.
- The temporary certificate directory was moved to Trash and is recoverable.
- This proves the User API contract, scope separation, ready-Target schedulability filter, persisted lifecycle filter, raw-response redaction, generated SDK behavior, and browser-safe Control Plane boundary.
- The User Web selector, Profile-based Lease creation/resolution, and removal of legacy User Web infrastructure inputs remain unimplemented and unproven.
