# Test entry points

The [test policy](../CLAUDE.md#tests-and-verification) is authoritative. Package
tests run through their `test` script (Vitest); repository tools use
`bunx vitest run test/scripts`, Python tools use `bun run test:python`, and the
pinned Web flow uses `bun run test:e2e`. Build workspace SDK distributions before
consuming packages. No entry point treats empty discovery as success.

`e2e/` contains cross-module flows and their fixtures. Provider/infrastructure
harnesses remain opt-in for their relevant changes and require their documented
environment; the small Web suite does not replace isolation or recovery tests.
Public protocol and migration fixture corpora remain with their contract sources:
they also feed generators and distributable protocol contracts.
The published `cloud-agent-testkit` implementation remains in its package `src/`;
its own tests live in `test/` like the other packages.

## Go package visibility exceptions

Public SDK behavior lives in `sdk/go/test/`; the private HTTP transport remains
in `sdk/go/gen/openapi/v1alpha1/http_test.go` to exercise `roundTrip`, response
limits and credential handling. The public managed-agent consumer lives in
`services/control-plane/test/managedagent_test.go`.

Public service behaviors (grants, coordination, lifecycle, probes, cursors,
strict JSON, quotas, enrollment, RBAC and policy checks) also belong in
`services/control-plane/test/`. Keep their fixture references relative to the
repository, and retain explicit PostgreSQL skips when its test URL is absent.

The remaining 128 same-package service files use private production symbols or
shared same-package test helpers. A per-file declaration/reference check found
22 files that could move with imports and local fixture/source-path adjustments;
those now join the existing public consumer in the service `test/` directory.
Keep the remaining tests beside their package until a public behavior can
replace the private assertion without exporting internals, duplicating source,
or adding a test adapter. Shared helper consumers can move together in a later
behavior-focused slice. No production API was exported for this move.

| Location                                                                | Files | Package access still required                                                                           |
| ----------------------------------------------------------------------- | ----: | ------------------------------------------------------------------------------------------------------- |
| `services/control-plane/internal/server/`                               |    34 | `decodeDeploymentTargetPageToken`, `projectHTTPVerifierFake`, health harness                            |
| `services/control-plane/internal/store/postgres/`                       |    32 | `mapCompatibilityRecoveryError`, `newFakeConnection`, shared DB fixtures                                |
| `services/control-plane/internal/authn/`                                |     8 | `errorScopeMismatch`, `claimTenantID`, `testPrivateKey`, `export_test.go`                               |
| Access Gateway, Docker/Kubernetes/SSH, Foundation and Worker client     |    15 | `cleanupManagedWorker`, `applyResource`, `newTestSSHHost`, isolation helpers                            |
| coordination, managedagent, migrationcore, localmigration, remoteworker |    13 | `durableExecutionKey`, `normalizeRyuExponent`, `fakeConnector`, certificate fields                      |
| Remaining Control Plane internal packages                               |     6 | `authorizationRequest`, `allGeneratedOperations`, `validIdentifiers`, private OpenSandbox client fields |
| `services/control-plane/cmd/`                                           |     7 | `parseConfig`, `boundSandboxPTYBinaryFrame`, certificate request parsing                                |
| `services/worker/`                                                      |    13 | `canonicalOperationEnvelope`, `newAdmissionFixture`, `client.done`, `cloneDescriptor`                   |

Three external authn files also stay with that package:
`binder_external_test.go`, `postgres_external_test.go` and
`runtime_server_external_test.go` use test-only exports from `export_test.go`.
Those exports do not exist when authn is imported from a separate test directory.
Service shell fixtures live in `services/control-plane/test/`.

Tests are excluded from service binaries and published Runtime/SDK packages.
The deployment verification archive intentionally retains the harnesses used to
verify its own candidate; it is not a service binary or SDK distribution.
