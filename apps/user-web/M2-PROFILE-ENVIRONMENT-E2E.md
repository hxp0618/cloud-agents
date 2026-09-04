# M2 Profile-based User Environment E2E

Date: 2026-09-04

Status: passed for the Profile-to-environment User API, User Web boundary, and Session projection slice. This does not complete M2 or the Section 15 acceptance matrix.

## Build under test

- Branch: `codex/cloud-agents-platform-p0`, starting HEAD `0b87152`.
- User Web: independent Vite application on `127.0.0.1:4173`, proxying `/v1` to Control Plane.
- Control Plane: local-development binary on `127.0.0.1:18084` with its development Worker listener on `127.0.0.1:18094`.
- Database: fresh PostgreSQL 17.6, migrated from the current checkout to `schema_head=000043`, `applied=43`.
- Locked validation toolchain: Go `1.26.6`, Node.js `24.18.1`, Bun `1.3.14`, Python `3.14.7`, and uv `0.12.5`.

## Contract, authority, and persistence

- The User API adds `POST /v1/tenants/{tenantId}/projects/{projectId}/environments` and `GET /v1/tenants/{tenantId}/projects/{projectId}/environments/{environmentId}` with separate `environments.create` and `environments.get` scopes.
- The create body contains exactly `profileId` and `profileVersion`. Generated TypeScript and Go SDK tests assert the serialized body and reject Target, endpoint, credential, release, CPU, memory, storage, and network fields.
- PostgreSQL `create_user_environment_v1` accepts only a published Profile, selects a ready referenced Target in Profile order, and copies the fixed release digest, resource limits, and opaque Provider credential reference into the private Lease record. The browser never resolves or receives those values.
- The public `UserEnvironment` response contains only project, opaque environment identity, Profile identity/version, observed phase, stable error code, and expiry.
- Session creation stores the environment's immutable Profile identity/version and rejects a Provider not allowed by that Profile. Session responses expose the Profile pair instead of Target, credential, or endpoint configuration.
- Legacy User Target and Lease routes now require dedicated infrastructure scopes. The ordinary User token received HTTP `403` from both legacy routes and the corresponding Admin Target and Lease routes.

The live database showed that server-side resolution copied the selected Profile authority without exposing it through the User response:

```text
environment-9dc17f01dc7de66506c1071a3ca03804
profile=profile-m2-user-env:v1
target=docker-m2-user-env
release_match=true
provider_credential_ref_match=true
cpu_match=true
memory_match=true
```

The real Session API persisted the same Profile pair through the opaque environment binding:

```text
session-d99bd449-e27f-4a43-a6b6-fccde18f398f
environment=environment-9dc17f01dc7de66506c1071a3ca03804
environment_generation=1
profile=profile-m2-user-env:v1
state=active
```

## Real browser verification

- A real browser connected with an ordinary User token and loaded the published Profile catalog from Control Plane. The page displayed `profile-m2-user-env v1` with Codex and Claude Code available.
- Clicking **Prepare environment** returned HTTP `201`. The captured request body was exactly `{"profileId":"profile-m2-user-env","profileVersion":1}`.
- The page polled the persisted environment while it was provisioning and disabled Session creation until server state became ready.
- Browser storage contained only Control Plane connection context, project/Profile identity, opaque environment identity, Session/Execution identity, and an opaque event cursor. The bearer token field was empty after connection and remained empty after reload.
- The removed legacy infrastructure storage key was absent. Page text and storage contained no Docker/Kubernetes/SSH endpoint, kubeconfig, `credentialRef`, `providerCredentialRef`, release digest, CPU/memory setting, or storage/network policy reference.
- At `1280px` and `390px` viewport widths, `scrollWidth` equalled `clientWidth`; the Profile selector and workspace had no horizontal overflow.
- Reloading the page required the memory-only token again, then restored the selected Profile and environment from server state.
- After the isolated readiness fixture described below, the browser created the Session shown above. The UI rendered its Profile identity/version and did not render Target, Lease, Worker, endpoint, or credential metadata.

## Source verification

```text
bun run --cwd apps/user-web typecheck                                      PASS
bun run --cwd apps/user-web test                                           PASS, 3 files and 21 tests
bun run --cwd apps/user-web build                                          PASS
bun run --cwd sdk/typescript build                                         PASS
bun run --cwd sdk/typescript test                                          PASS, 3 files and 33 tests
go test ./sdk/go/gen/openapi/v1alpha1 ./sdk/go/gen/platform/v1alpha1       PASS
bun scripts/generate-platform-json-sdks.ts --check                         PASS
bun run platform:contracts:check                                           PASS, 122 schemas and 66 OpenAPI operations
bun run platform:sdk:consumers                                             PASS with fresh TypeScript and Go consumers
bun run typecheck                                                          PASS
bun run lint                                                               PASS
bun run secret:scan                                                        PASS
go test ./services/control-plane/internal/managedhost ./services/control-plane/internal/managedagent ./services/control-plane/internal/store/postgres ./services/control-plane/internal/server ./services/control-plane/internal/localmigration ./services/control-plane/cmd/cloud-agents-product-migrate PASS
go test -tags=localdev ./services/control-plane/internal/authn ./services/control-plane/cmd/cloud-agents-control-plane ./services/control-plane/cmd/cloud-agents-local-migrate PASS
bun test scripts/lib/platform-migration-sql.test.ts scripts/lib/platform-release.test.ts PASS, 49 tests
git diff --check                                                           PASS
```

The official AJV audit executed but remains the repository's documented non-Gate nonconformant check. All contract standards gates and generated-artifact drift checks passed with the locked toolchain.

## Readiness fixture and evidence boundary

- The browser, User Web, Control Plane, Worker listener, and PostgreSQL were stopped after verification. Ports `4173`, `18084`, and `18094` had no listeners, the exact owned PostgreSQL container was absent, and both owned temporary state directories were removed.
- The local-development server used for this slice did not have a deployment actuator configured, so it did not create a real target Worker.
- To test the downstream User Web gate and Session projection without claiming runtime deployment, the just-created isolated environment alone was advanced to `ready` in the disposable PostgreSQL database and assigned a deliberately unreachable fixture Worker endpoint. No other resource was changed.
- The browser then exercised the real authenticated Session API and durable Profile projection. It did not submit a Turn because the fixture endpoint was not a Worker.
- This proves the User API contract and scopes, server-side Profile resolution, generated SDK request boundary, User Web Profile selector, browser storage/redaction boundary, reload behavior, and Profile-aware Session persistence.
- It does not prove a real Worker deployment, Codex or Claude Code Turn, Approval/User Input, Artifact, or Docker/Kubernetes/SSH cleanup. Those remain required before M2 and Section 15 can pass.
