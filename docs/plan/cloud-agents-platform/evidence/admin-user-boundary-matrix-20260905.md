# Real local Admin/User rejection matrix — 2026-09-05

Source base `40812b8945c8b53a8d71b6bc7ef7c757527bcaea`, existing
`codex/cloud-agents-platform-p0`. New runnable check:
`scripts/test-admin-user-boundary.ts`. No application, contract, SDK or backend
behavior changed. Ponytail guidance reused native fetch/assert and the existing
generated SDK/dev stack instead of adding another test framework.

## Executed checks

Fresh `scripts/cloud-agents-dev.sh` built the current Control Plane/Worker and
started real PostgreSQL and local signed-token authentication. State directory
`.tmp/cloud-agents-dev.0hmLug`; CP `http://127.0.0.1:18085`, Worker18095;
CLI-created project `project-5ece4928939d2307ea763576b9970294`, tenant-local.
No existing project, target or credentials were used.

The script reads current managed-host OpenAPI paths, not a hand-maintained
route list. Generated SDK positive controls first verify that the ordinary
user can read its project and the administrator can list Targets. This prevents
an expired/invalid user token or unavailable server from passing the403 gate.

Final result:

```json
{"operations":38,"requests":76,"authenticatedUser":403,"anonymous":401,"rejectedUserInfrastructureFields":9}
```

- All38 documented Admin GET/POST/PUT operations returned403 for the ordinary
  authenticated user and401 without Authorization. The real HTTP/JWT/scope
  handlers were used, not fake verifiers or stores.
- Every rejection exactly matched the fixed Problem object (including stable
  code, non-retryable flag, title/type, status and request ID), with no extra
  resource/content fields. Cache-Control was no-store and X-Request-ID matched.
  Unexpected bodies are not printed by the checker.
- Admin writes use undecodable JSON and opaque nonexistent resource IDs, so
  scope must be rejected before body validation or resource lookup. The
  checker refuses newly introduced HTTP methods until their safety is reviewed.
- In the verified Profile-empty project, User environment creation rejected
  all9 extra fields with400/INVALID_REQUEST: endpoint, credentialRef,
  providerCredentialRef, targetId, releaseDigest, cpuLimitMillis,
  memoryLimitBytes, kubeconfig, sshHost. These are deliberate hostile API
  inputs, not requests made by User Web. No valid provisioning request ran.
- The initial76-case matrix and the final85 negative cases both passed.
- Independent generated SDK reads afterwards confirmed zero Targets, Leases,
  Workers, Releases, Profiles, Storage Policies, Network Policies and
  Maintenance Operations. No Probe or Provider call ran.
- Focused Go HTTP scope/authority/User Environment/Profile tests passed with
  `-count=1`; those unit tests include fake stores and remain narrower evidence
  than the real HTTP matrix. User Web Vitest23 tests passed. Scoped oxfmt,
  oxlint and diff checks passed.

## Reproduce

Start an owned local dev stack, create a disposable project with its CLI,
then run (substitute the actual state path and generated project ID):

```sh
bun scripts/test-admin-user-boundary.ts http://127.0.0.1:18085 \
  STATE/control-plane.token STATE/control-plane-admin.token tenant-local PROJECT_ID
go test ./services/control-plane/internal/server \
  -run 'Test(Admin.*(Forbidden|Scope|Authority)|PublishedEnvironmentProfileHTTP|UserEnvironmentHTTP)' -count=1
bun run --cwd apps/user-web test
```

This run used Go1.26.6, Node24.18.1 and Bun1.3.14. Token files are read at run
time and never stored in the evidence. The checker is restricted to127.0.0.1
HTTP, does not follow redirects and requires an empty Profile collection for
the User API injection cases. Stop only the owned dev parent after testing.

## Cleanup and limits

Stopped owning dev script77855; shutdown removed container
`cloud-agents-dev-501-77855` and `.tmp/cloud-agents-dev.0hmLug`, including the
temporary project/database and ephemeral tokens. These disposable records were
not backed up. No18085/18095 listeners remain. No pre-existing resources were
removed. All unrelated dirty files and staged HTML blob
`2adb0cc1c5649e39534a2171a5c25aabedf1fe30` were preserved.

This proves the tested rejection boundary in local-development authentication.
It does not prove production OIDC audience separation, all successful Admin
response redaction, browser request/storage behavior, cross-tenant ownership,
denied-write Audit durability, three-transport deployment/cleanup or Provider
Turns. Those remain separate current-evidence requirements.

The updated requirements now describe BASE-M0-M5 while the active Goal still
specifies ADMIN-M1-M4. Scope-switch authority has not been received; this shared
security requirement is in both scopes. No new foundation model or lifecycle
was implemented, no plan was rewritten, and the Goal is not complete. The
next common security gap to inspect is denied Admin-write Audit authority;
full implementation/acceptance ordering still needs the user's scope decision.
