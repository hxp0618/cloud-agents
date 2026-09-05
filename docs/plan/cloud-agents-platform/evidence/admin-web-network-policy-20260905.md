# Admin Web Network Policy slice — 2026-09-05

## Scope and authority

Source parent: `5bfee4485c5846d7b5bf649772f4059cb0a666eb`, branch `codex/cloud-agents-platform-p0`.

This slice adds Network Policy schemas, generated Go/TypeScript SDK methods, PostgreSQL tables/RLS and migration 000049, authenticated Admin CRUD/read/audit routes, the bilingual Admin page, Profile policy selection and the User API `networkSummary` projection. Any Profile reference makes a policy immutable. Mutation authority checks the expected resource version and idempotency key; audit records contain hashed actor identity and resource metadata, not user content.

Non-default execution semantics are intentionally **not claimed implemented**. Restricted/deny egress, allowlist/DNS/proxy references, ingress and preview can be recorded and attached to draft Profiles, but Profile publication and new environment creation fail closed. User published-profile queries exclude unsupported policies. Only default public egress without those options is currently deployable. Target adapter enforcement remains unfinished M3 work.

## Executed checks

- Focused Go tests passed for networkpolicy, environmentprofile, store/postgres, server, authn, localmigration and the product-migrate/control-plane entry points; generated Go SDK tests also passed.
- TypeScript SDK: 26 tests passed. Admin Web: 20 tests passed. User Web: 23 tests passed. Migration classifier/release package tests: 52 passed.
- `bun run platform:contracts:check` passed, including exact generated SDK checks.
- Admin Web and User Web independently passed their production builds. Admin build reports the existing-style single-bundle size warning; this is not a performance qualification.
- A broader `go -C services/control-plane test ./...` run is not a passing gate: `TestRunExecutionDownloadArtifactWritesRawBytes` fails because its response fixture lacks required `Content-Disposition`. Neither that fixture nor download validation was changed by this slice. Focused successes must not be reported as whole-suite success.
- That broad run also reached the migration package's default 10-minute cumulative timeout; the test active at the deadline was `TestRunnerLedgerEntryExecutionAdmissionUseRetirementIsExactAndOneShot`. This is not evidence of a migration 000049 execution failure: its packaged real PostgreSQL run passed below. The follow-up added the required download header to the Artifact fixture without relaxing production validation. The full CLI and generated OpenAPI SDK test packages passed, and the individual migration timeout-point test passed in 1.269 seconds with a 60-second limit. The whole-suite gate remains unqualified. Follow-up logs: `artifact-followup.log` and `migration-timeout-point.log` in the same temporary evidence directory.

## Real PostgreSQL / Control Plane / Docker run

Local candidate: `/tmp/cloud-agents-network-validation.mLQ7c6/fenced`, built with version `0.1.0-network-validation-fenced` and `--allow-dirty`. This was a local validation package; no push, remote image publication or hosted Release was performed.

The current `scripts/test-platform-compose.sh` passed against real PostgreSQL and the packaged Control Plane/Docker Worker. For browser inspection, a temporary copy paused after Profile publication, then resumed the remaining original script and cleanup. The temporary copy did not stub Admin responses.

Network-specific assertions exercised:

1. PUT creates a persisted Network Policy at resource version 1.
2. Same-key/same-body replay returns the identical resource and only one audit event.
3. A fresh key with stale expected resource version returns 409.
4. List and audit endpoints return persisted authority; an ordinary user token receives 403.
5. A deny policy can be saved and bound to a draft but publication returns `409 NETWORK_POLICY_UNAVAILABLE`.
6. The default-public Profile publishes; its User summary contains `networkSummary` and no infrastructure references.
7. Updating a referenced policy returns `409 NETWORK_POLICY_REFERENCED`.
8. The existing Docker deployment/recovery/upgrade/termination/restore smoke completes with `docker-workers=0`.

Run identity: Compose project `cloud-agents-compose-smoke-56344`, tenant `tenant-compose-smoke`, project `project-2fde604e9ef099aad6125e5a2181196c`. After cleanup, Docker container and volume inventories for that Compose project were empty; Worker containers for the project label were also absent. Only this test's owned resources were cleaned.

Logs: `/tmp/cloud-agents-network-validation.mLQ7c6/compose-fenced.log`, `contracts.log`, `go-all.log` and `admin-build.log`. Temporary paths may not survive host cleanup; reproduce using the committed script and a newly built local candidate.

This invocation had no real Provider credential argument. Its Kubernetes adapter fixture is not a real Kubernetes cluster, and it is not SSH or Codex/Claude real-Turn evidence.

## Browser checks

The real Admin page, served by Vite with its Admin API proxy to the above Control Plane, created `network-browser-validation` and read it back through the API with an audit event. Selecting the Profile-referenced policy disabled editing. The browser was reconnected after a source hot reload and successfully loaded the persisted catalog.

Checked immediate zh-CN/en-US switching; saved zh-CN restored on reload; an invalid saved locale fell back to en-US. On reload the token field was empty. Browser storage keys were limited to theme, locale and the existing non-secret admin connection record. The Admin origin/tenant/project are allowed Admin metadata; this is not a claim that User Web storage was newly E2E-qualified.

Captured the Network Policy page for both languages, both themes, desktop 1440×1000 and mobile 390×844. Mobile document width was 390 with no page-level horizontal overflow; wide resource tables retain their horizontal scroll container. This is rendered-page inspection, **not** completed Daytona baseline/pixel-diff acceptance.

Screenshots under `/tmp/cloud-agents-network-validation.mLQ7c6/`:

| File | SHA-256 |
| --- | --- |
| network-en-dark-desktop.png | bf69341482ac169752c89c393423136fb890acaa508649a89c7897504ec0d973 |
| network-en-dark-mobile.png | efcdd70262e52337366922c8f19f508fdb38d83773534ae6eebbd5d90e19aeed |
| network-en-light-desktop.png | bccfd176773bb2ba3b48f48c85f33faeb58d2f295aab6c6fff8f4f6b5a67b9e5 |
| network-en-light-mobile.png | 4f15cf47dfdf54210af0d1b253c974dac4fe4b478e9d936fc2aa2ffb3b7dc2b3 |
| network-zh-dark-desktop.png | 915be25bdf46245a9270dd509514f2e0331e84d14b254714179583f7c3cc098d |
| network-zh-dark-mobile.png | 1f35afb1ea97ee9920fc4d646cf490ed04a45ae3fa2214c088c3cafdf354a96a |
| network-zh-light-desktop.png | 80d44acc625a83c3c4a9adef3a6554f2466acb31ffea67bc3eb97094b36f43f8 |
| network-zh-light-mobile.png | b7f075567576b89fc56508bb10c14ce34c5c9dfc1954b7a1974c892f2bcbfea6 |

## Remaining acceptance

M3 target-side Network/Storage policy execution and all unmet earlier acceptance gates remain open. This record does not qualify M1 Daytona 1:1 acceptance, the full M3 Docker/Kubernetes/SSH maintenance matrix, M4 OIDC deployment isolation or real Codex/Claude E2E. The Goal remains active. This slice required no new credentials or destructive permission for pre-existing resources.
