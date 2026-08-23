# ADR-0027: P1 transport-neutral runtime server path and tenant authority

- Status: Accepted under the standing P1 execution approval on 2026-08-24
- Scope: `managedAgentCreateProject` generated request validation and claim-only PostgreSQL admission
- Depends on: ADR-0007, ADR-0008, ADR-0013, ADR-0025, and fixed parent `73ba42cb8d5d17833dd96532b2a527f9ed7250f9`
- Decision owner: hxp0618
- Implementation executor: Codex
- Gate effect: none

This decision supplies a bounded runtime-server candidate for the G-CONTRACT path-and-tenant criterion. It does not
declare that criterion satisfied, update the generated closure profile, close a Gate, or authorize HTTP, provider,
production-database, deployment, publication, or release effects.

## Context

The generated Go SDK already owns strict `CreateProject` server-input validation, including the route tenant, request
identifier, idempotency key, and exact closed request body. The control-plane coordination package owns the generated
`managedAgentCreateProject` profile and its canonical request projection. The PostgreSQL durable coordination service
owns the protected claim transaction and consumes an opaque verified-principal pointer.

No production server boundary currently joins those three authorities. A future HTTP adapter must not be allowed to
replace route-tenant authority with a tenant from the body, principal, configuration, or provider response. It must
also not bypass the generated validator, mint or copy a principal, submit a caller-authored digest, or expand a
claim-only admission into project, completion, operation, or outbox effects.

## Decision

### 1. Transport-neutral request and concrete service

Add an exported transport-neutral request with exactly four inputs:

- `RouteTenantID string`;
- `RequestID string`;
- `IdempotencyKey string`; and
- `Body []byte`.

The server holds exactly one concrete `*postgres.DurableCoordinationService`. Its constructor rejects nil, and the
method also fails closed for a nil or zero server. An interface, callback, function field, dynamic service locator,
reflection bridge, or test-only production injection seam is not admitted.

### 2. One ordered claim path

After the fail-closed receiver check, `Claim` has one business sequence:

1. call the pinned generated `openapiv1.ValidateCreateProjectServerRequest` with the four exact request fields;
2. pass its returned typed value to one unexported exact mapper; and
3. call concrete `ClaimIdempotency(ctx, validated.TenantID, principal, claim)`.

Generated validation failure returns before the service or principal can be consumed. The exact input principal
pointer is forwarded once; the server does not construct, copy, unwrap, retain, or reinterpret it.

The mapper selects `coordination.ManagedAgentCreateProject()`, copies only the generated typed body fields, preserves
the exact validated idempotency key, and fixes `AuditFactID` to the validated `RequestID`. It does not call
`BindManagedAgentCreateProject`: the concrete durable service remains the single authoritative binder. The tenant
argument is only `validated.TenantID`, which was produced from `RouteTenantID`; body, configuration, provider output,
and principal data cannot substitute it.

### 3. Claim-only side-effect boundary

This server may invoke only `ClaimIdempotency`. It does not call completion, project mutation, operation, finalizer,
or outbox APIs. It imports no `net/http`, bearer/OIDC/JWKS/provider package, raw SQL facility, reflection, or `unsafe`,
and starts no goroutine. HTTP routing, authentication header parsing, production trust provisioning, provider/P2 work,
and project creation remain separate unimplemented and separately reviewable boundaries.

### 4. Fixed-parent SDK pin

The service module uses `GOWORK=off` and pins the published SDK pseudo-version corresponding exactly to the fixed
parent:

`github.com/hxp0618/cloud-agents/sdk/go v0.0.0-20260823202540-73ba42cb8d5d`

The fetched module checksum is `h1:D6vOjWb61f+ydBqKH0BGxi+HXSxA/tgjfsMSi8ppsOQ=` and its `go.mod` checksum is
`h1:qLQE6Q2bV2hZM0c7CZDZUx78EuODm+Vzl90AII5zYJs=`. The SDK requires `golang.org/x/sys v0.45.0`; minimum-version
selection therefore raises the control-plane's explicit direct requirement from `v0.44.0` to `v0.45.0`. No `replace`
or workspace fallback is allowed.

This pin is deliberately the immutable predecessor pin for this slice, not a self-reference. A later integration on
top of an independently reviewed closure-profile v2 predecessor must publish and re-pin that reviewed predecessor's
SDK version before it can claim current-source integration.

## Verification boundary

Focused unit and AST tests freeze exact mapping, constructor/receiver failure, generated-validation precedence,
validator-to-mapper-to-claim order, the concrete field and exact claim arguments, and forbidden surfaces. The opt-in
disposable PostgreSQL 15/16/17 matrix uses only exact locally present images with `--pull=never`; normal and race runs
prove created and same-digest replay with different request IDs, tenant and organization mismatches, nil/stale/consumed
principals, canceled context, and absence of unintended project, operation, finalizer, outbox, idempotency, or audit
writes.

No production database is an admissible test target. The matrix owns and removes only containers carrying its unique
run label.

## Explicit non-claims

- No HTTP handler, router, middleware, bearer parsing, OIDC discovery, JWKS retrieval, or provider adapter exists.
- No project writer or completion path is implemented or called.
- No production database write, deployment, publication, release, or main-branch merge is authorized.
- No generated closure profile, generation lock, contract manifest, status tracker, or Gate record changes here.
- The runtime-server criterion and every Gate remain open until current-source integration and independent review.
