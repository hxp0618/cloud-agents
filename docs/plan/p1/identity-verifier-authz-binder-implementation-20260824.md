# P1 identity-verifier authz binder - 2026-08-24

- Status: **SLICE C IMPLEMENTED CANDIDATE — FIXED-CANDIDATE INDEPENDENT REVIEW PENDING**
- Parent: `d2e464be0f3e54aa25e55d6cca7d4f744b04bc1c`
- Branch: `codex/cloud-agents-p1-identity-verifier-slice-c-20260824`
- Decision: ADR-0025
- Gate effect: none; all Gates remain open

## Prerequisite lineage

Slice A's generated profile candidate is fixed and independently approved. Slice B's offline verifier candidate
`6e79edabbd68177023ff1ce8a848f4a7dd3307fd` is also fixed; its independent review commit
`d2e464be0f3e54aa25e55d6cca7d4f744b04bc1c` returned `APPROVE, P0=0/P1=0/P2=0`. Slice C starts from that review
commit. This record describes a working implementation candidate only: no Slice C commit or P0/P1/P2 verdict is
fixed yet.

## Implemented authority boundary

`authz.WithVerifiedOperation` is the sole production consumer of an opaque `*authn.VerifiedPrincipal`. It consumes
the principal once, rechecks the exact active trust generation, constructs a callback-live binder, and keeps the
generation lease until callback completion. A callback can return success only after it spends the binder with exact
tenant/resource/permission `Bind` and spends the resulting operation with `Execute`; no bind-only, execute-free, zero,
literal, copied, tampered, stale, escaped, second, or concurrent attempt can produce authority.

The operation exposes `Actor` only as a callback-live lookup key. `Execute` evaluates the exact bound actor, target
and permission against PostgreSQL-derived facts and invokes the protected typed callback without returning a reusable
allow decision. Active asynchronous execution is drained before the principal lease is released. Framework denial
errors are stable and do not contain actor, issuer, tenant, resource, permission, token, signature, or private-key
material.

The production dependency direction remains:

```text
store/postgres -> authz -> authn
```

The full control-plane production call graph is fail-closed and exact. It permits only the reviewed PostgreSQL bridge
and these eight public JWT-user paths:

- `CreateMembership`, `SuspendMembership`, `RevokeMembership`, `BindRole`, and `RevokeRoleBinding`; and
- `ClaimIdempotency`, `CompleteIdempotencySuccess`, and `CompleteIdempotencyFailure`.

All eight accept only `*authn.VerifiedPrincipal` as request authority. Target `SubjectRef` values remain lexical stored
data. Standalone `TenantReadCapability.Authorize`, public raw-subject authorization requests, and raw actor parameters
are absent. Exact AST closure freezes every production `WithVerifiedOperation`, binder, operation, `Actor`, `Execute`,
`Snapshot`, and bridge edge; a new caller, construction, alias, dot import, dynamic-dispatch import, or bridge edge
fails conformance until separately reviewed.

For every path, request validation, resource resolution, membership/role-binding fact reads, authorization evaluation,
the exact typed SQL operation, and commit, rollback, failed-commit, or unknown-outcome settlement stay inside the same
callback and active-generation lease. The matrix includes a real PostgreSQL row-lock case proving that generation
invalidation waits through the protected mutation and successful commit, plus cancellation proving rollback before
lease release.

## Mutable lock closure and immutable profile

The identity-verifier Go-profile pipeline now binds the Slice C authn/authz/PostgreSQL implementation, focused and
integration tests, structural/call-graph checks, and both PostgreSQL matrix scripts as mutable runtime conformance
inputs. It does not add those sources to the immutable registry/profile digest pipeline.

- `contracts/generation.lock.json` SHA-256:
  `abd7d2e99133df1341bd9601fa003d29b8753743340ab134ea78d6f7015ccbc2`
- generated registry file SHA-256, unchanged:
  `474bb31fa5721dd20fc5723b790f39d45fda5ac0392d9e5bb73cb0ecef3e0ccf`
- generated Go profile SHA-256, unchanged:
  `e3d9ed08b69b3a7f4ce0ac6d100ea49f577dafdf857ff33be32c4170c357b8de`

## Verification

Using Go `1.26.6`, Node `24.13.1`, and Bun `1.3.14`:

- `internal/authn`, `internal/authz`, and `internal/store/postgres` focused normal and race tests: PASS;
- the same three packages' vet and build checks: PASS;
- local PostgreSQL 15/16/17 membership/RBAC normal, race, and fault matrix: final PASS;
- local PostgreSQL 15/16/17 durable-coordination normal, race, and fault matrix, including typed statement-level
  rejection through all three public methods: final PASS;
- exact-application row-lock invalidation lease-through-commit and cancellation/rollback cases: PASS;
- generated registry/profile same-bits and the current contract lock: PASS; and
- focused TypeScript format, lint, contract-lock source closure, and diff checks: PASS.

An early development run exposed incomplete observer-role permission synchronization in the test harness. It was a
matrix calibration failure before the observer could establish the intended row-lock evidence, not a product-path
failure and not final evidence. The corrected final matrices above are the recorded result.

No broad `internal/migration` test, production database write, remote-host command, HTTP handler, OIDC discovery,
remote JWKS fetch, provider/P2 side effect, production trust-source adapter, deployment, publication, release, merge,
or Gate operation was run or authorized.

## Remaining boundary

The Slice C commit must still be fixed and independently reviewed with an explicit P0/P1/P2 verdict. Until that review
returns `APPROVE`, this implementation remains a candidate. Production trust provisioning and every external surface
remain unimplemented, and `G-SECURITY-P1` plus every aggregate Gate remain open.
