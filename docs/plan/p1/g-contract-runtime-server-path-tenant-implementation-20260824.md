# G-CONTRACT runtime server path and tenant authority implementation - 2026-08-24

- Status: **IMPLEMENTED CANDIDATE - FIXED-CANDIDATE INDEPENDENT REVIEW PENDING**
- Fixed parent: `73ba42cb8d5d17833dd96532b2a527f9ed7250f9`
- Branch: `codex/cloud-agents-p1-g-contract-runtime-server-20260824`
- Decision: ADR-0027
- Gate effect: none; `G-CONTRACT` and every aggregate Gate remain open

## Implemented boundary

`internal/server.ManagedAgentCreateProjectServer` is a transport-neutral, claim-only production boundary. It owns one
concrete `*postgres.DurableCoordinationService`; nil construction and nil/zero receiver use fail closed. Its exported
request contains only route tenant, request ID, idempotency key, and raw request-body bytes.

The production path is structurally frozen as:

```text
request fields
  -> pinned generated ValidateCreateProjectServerRequest
  -> unexported typed mapManagedAgentCreateProjectClaim
  -> concrete ClaimIdempotency(ctx, validated.TenantID, same principal pointer, claim)
```

The mapper selects the generated `ManagedAgentCreateProject` profile, copies the exact decoded body, preserves the
validated idempotency key, and maps validated request ID to `AuditFactID`. It neither accepts nor derives a tenant:
the only tenant passed to PostgreSQL is the generated validator's exact route value. The durable service remains the
single owner of authoritative profile binding and canonical request digest derivation.

No HTTP, router, bearer/OIDC/JWKS/provider, completion, project writer, operation, finalizer, outbox, raw-SQL,
goroutine, reflection, or unsafe surface was added.

## Fixed SDK and module graph

The control-plane module pins the published fixed-parent SDK exactly:

- version: `v0.0.0-20260823202540-73ba42cb8d5d`;
- module Sum: `h1:D6vOjWb61f+ydBqKH0BGxi+HXSxA/tgjfsMSi8ppsOQ=`; and
- GoModSum: `h1:qLQE6Q2bV2hZM0c7CZDZUx78EuODm+Vzl90AII5zYJs=`.

That SDK's module graph requires `golang.org/x/sys v0.45.0`, so the service module's existing direct `v0.44.0`
requirement was explicitly upgraded to `v0.45.0`. `GOWORK=off` resolution confirms the selected graph contains the
exact SDK version and `x/sys v0.45.0`. There is no `replace`. This is a predecessor pin, not a self-reference; later
current-source integration must re-pin an independently reviewed v2 predecessor SDK.

## Focused verification

Pinned Go `1.26.6` with `GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly` verifies:

- exact typed body/profile/idempotency/request-audit mapping: PASS;
- nil constructor/receiver and generated-validation-before-service behavior: PASS;
- AST closure for concrete dependency, exact arguments, validator/map/claim order, and forbidden surfaces: PASS;
- focused normal and race tests for `internal/server` and `internal/authn`: PASS;
- focused vet and build for `internal/server` and its direct runtime packages: PASS; and
- disposable PostgreSQL 15.18, 16.14, and 17.10 normal/race/fault matrix: PASS.

The real PostgreSQL cases prove:

- initial `created/pending` and same request digest replayed as `replay/pending` despite a different request ID;
- invalid generated input does not consume the principal before a subsequent valid claim;
- route-tenant/principal and body-organization/principal mismatch denial;
- nil, stale, and already-consumed principal denial;
- canceled context rollback; and
- exactly two intended idempotency/audit writes, with no rejected-path record/audit and no project, operation,
  finalizer, or runtime-server outbox write.

The matrix uses the existing exact digest-pinned local PostgreSQL images and `--pull=never`. It creates isolated
containers and databases, and its ownership label limits cleanup to those containers. No production database or
remote host was used.

## Remaining boundary

The implementation commit must still be fixed and independently reviewed with a P0/P1/P2 verdict. A later integration
must rebase on the independently reviewed generated closure-profile v2 lineage, publish/re-pin that predecessor SDK,
bind this criterion into the generated current closure profile, and receive another appropriate immutable review.

No broad `internal/migration` suite, HTTP/P2/provider effect, production database write, deployment, publication,
release, merge, status-tracker update, generation-lock/profile change, or Gate operation was run or authorized.
