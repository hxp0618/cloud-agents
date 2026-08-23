# G-CONTRACT runtime server path and tenant authority independent review

Date: 2026-08-24

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

This is an independent fixed-candidate code and behavior review. It approves
the bounded transport-neutral, claim-only runtime-server candidate. It does not
approve an HTTP or production-trust boundary, update the generated closure
profile, satisfy the current-source integration criterion, or close any Gate.

## Fixed lineage and scope

- candidate branch: `codex/cloud-agents-p1-g-contract-runtime-server-20260824`
- candidate commit: `5cb7cec191c410432605a7dcec9ea77a76b78a12`
- candidate tree: `5f07d3ecbc3bad698f815d6291ef1ded5421c730`
- parent: `73ba42cb8d5d17833dd96532b2a527f9ed7250f9`
- candidate diff SHA-256: `9529dbba643879b63e1860bcc42cf195fd1600e3cb233793638f1c454b4e324b`

The candidate changes exactly eight reviewed files:

1. `docs/plan/adr/0027-p1-runtime-server-path-tenant-authority.md`;
2. `docs/plan/p1/g-contract-runtime-server-path-tenant-implementation-20260824.md`;
3. `services/control-plane/go.mod`;
4. `services/control-plane/go.sum`;
5. `services/control-plane/internal/authn/runtime_server_external_test.go`;
6. `services/control-plane/internal/server/managed_agent_create_project.go`;
7. `services/control-plane/internal/server/managed_agent_create_project_test.go`;
8. `services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh`.

No contract, closure-profile, generation-lock, status-tracker, or Gate record
changes are present.

## Runtime authority review

The production server contains exactly one concrete
`*postgres.DurableCoordinationService` field. Its constructor rejects nil, and
the method rejects both a nil receiver and a zero server before dereference.
There is no service interface, callback or function field, dynamic lookup, or
test-only production injection seam.

After that receiver guard, `Claim` has one ordered path:

```text
ValidateCreateProjectServerRequest(route tenant, request ID, idempotency key, body)
  -> mapManagedAgentCreateProjectClaim(validated)
  -> concrete ClaimIdempotency(ctx, validated.TenantID, principal, claim)
```

The pinned generated validator source was independently inspected. It validates
the route tenant and request ID, validates the idempotency key, decodes the
closed project body, and returns `TenantID: tenantID` without substituting or
normalizing another authority. It has no principal input, so generated
validation failure cannot consume the principal. The server forwards the exact
input `*authn.VerifiedPrincipal` expression once to the concrete service.

The mapper accepts only the generated typed value. It selects the generated
`ManagedAgentCreateProject` profile, maps only the typed project fields,
preserves `validated.IdempotencyKey`, and fixes
`AuditFactID = validated.RequestID`. It has no tenant parameter or tenant field;
the service call's tenant is exactly `validated.TenantID`.

Manual source inspection, the focused AST closure test, and a forbidden-surface
scan found no interface/function injection, HTTP, bearer, OIDC, JWKS, provider,
project writer, completion, operation, outbox, raw SQL, goroutine, deferred
hidden work, reflection, or unsafe path. The server makes exactly one generated
validator call and exactly one `ClaimIdempotency` call in the required order.

## Fixed module boundary

All Go commands used pinned Go `1.26.6` with
`GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly`. The independently resolved
module graph was:

```text
github.com/hxp0618/cloud-agents/sdk/go v0.0.0-20260823202540-73ba42cb8d5d <nil replace>
golang.org/x/sys v0.45.0 <nil replace>
```

The pseudo-version time `2026-08-23T20:25:40Z` and suffix `73ba42cb8d5d`
exactly match parent commit `73ba42cb8d5d17833dd96532b2a527f9ed7250f9`.
The fetched module sums matched the candidate:

```text
Sum:      h1:D6vOjWb61f+ydBqKH0BGxi+HXSxA/tgjfsMSi8ppsOQ=
GoModSum: h1:qLQE6Q2bV2hZM0c7CZDZUx78EuODm+Vzl90AII5zYJs=
```

`go mod verify` reported all modules verified, and `go mod tidy -diff` produced
no diff. There is no `replace` or workspace fallback.

## Focused replay

The exact mapping, nil/zero failure, validation precedence, and AST tests all
passed individually:

```text
TestManagedAgentCreateProjectClaimMappingIsExact              PASS
TestManagedAgentCreateProjectServerFailsClosed                PASS
TestGeneratedValidationRejectsBeforeConcreteService           PASS
TestManagedAgentCreateProjectServerASTClosure                 PASS
```

Focused package verification also passed:

```text
go test -count=1 ./internal/server ./internal/authn        PASS
go test -race -count=1 ./internal/server ./internal/authn  PASS
go vet ./internal/server ./internal/authn                   PASS
go build ./internal/server ./internal/authn                 PASS
gofmt -d <three added Go files>                             no diff
git diff --check <parent> <candidate>                       PASS
```

No broad migration or full control-plane test suite was run.

## Disposable PostgreSQL matrix

The reviewer independently ran the candidate's local matrix with the pinned Go
binary and exact locally present image digests. The script used `--pull=never`,
loopback-only ephemeral ports, isolated databases, and its unique ownership
label. It did not contact a remote or production database.

| PostgreSQL | Server version | Image digest                                                              | Result                 |
| ---------- | -------------- | ------------------------------------------------------------------------- | ---------------------- |
| 15.18      | `150018`       | `sha256:6eb0add3b77c081df18aa518ce43df58fdcc40f2e6d868a6fd08038dc7acd425` | normal/race/fault PASS |
| 16.14      | `160014`       | `sha256:95206741a5b214807675e14165369d05b93a9cf692223b616d07cca227e74b0b` | normal/race/fault PASS |
| 17.10      | `170010`       | `sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193` | normal/race/fault PASS |

For every major version, both normal and race invocations of
`TestPostgresExternalManagedAgentCreateProjectServerConformance` passed against
the concrete service. The observed behavior proves:

- initial claim is `created/pending`;
- same body and idempotency key with a different request ID is
  `replay/pending`;
- invalid generated input leaves the same principal usable for a subsequent
  valid claim;
- route-tenant/principal and body-organization/principal mismatches are denied;
- nil, stale, and already-consumed principals are denied;
- canceled context returns `context.Canceled` and rolls back;
- exactly two intended idempotency records and two audit facts remain; and
- rejected paths add no record or audit, and no project, operation, finalizer,
  or runtime-server outbox write occurs.

The matrix ended with:

```text
durable-coordination-service: PG15/16/17 normal/race/fault matrix PASS
```

No owned test container remained after completion.

## Documentation and non-claims

ADR-0027 and the implementation record consistently describe a bounded
candidate and retain the later current-source integration and independent
profile update as unfinished work. They do not claim HTTP routing,
Bearer/OIDC/JWKS handling, production trust, production database use,
deployment, publication, release, merge, criterion closure, or Gate closure.

## Commands replayed

```text
git show -s --format='%H%n%P%n%T' 5cb7cec191c410432605a7dcec9ea77a76b78a12
git diff 73ba42cb8d5d17833dd96532b2a527f9ed7250f9 5cb7cec191c410432605a7dcec9ea77a76b78a12 | shasum -a 256
git diff --name-status 73ba42cb8d5d17833dd96532b2a527f9ed7250f9 5cb7cec191c410432605a7dcec9ea77a76b78a12
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly <go1.26.6> -C services/control-plane list -m all
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly <go1.26.6> -C services/control-plane mod verify
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly <go1.26.6> -C services/control-plane mod tidy -diff
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly <go1.26.6> -C services/control-plane test -count=1 ./internal/server ./internal/authn
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly <go1.26.6> -C services/control-plane test -race -count=1 ./internal/server ./internal/authn
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly <go1.26.6> -C services/control-plane vet ./internal/server ./internal/authn
GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly <go1.26.6> -C services/control-plane build ./internal/server ./internal/authn
PATH=<go1.26.6-bin>:<system-tools> services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh
```
