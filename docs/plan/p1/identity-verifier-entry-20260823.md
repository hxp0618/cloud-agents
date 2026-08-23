# P1 offline JWT access-token verifier implementation entry - 2026-08-23

- Status: **ORDERED SLICES A-C APPROVED — GATES OPEN**
- Last updated: 2026-08-24
- Source: [`ADR-0025`](../adr/0025-p1-offline-jwt-access-token-verifier-contract.md)
- Baseline: `bf2bb8fa916a723bca2b64e156d2b3e64f374582`
- Scope: versioned generated verifier profile, offline pure verifier, opaque principal, authz binder, and independent review
- Gate effect: none

## 1. Why this is a separate repair

P1-A3 completed the generated public identity models and SDK/server seams. `SubjectRef` intentionally remains an exact
lexical identity tuple. That package did not and could not prove that request claims were signed by an issuer-owned
key, intended for this resource server, current, unrevoked, or tenant/scope bound.

G-SECURITY-P1 R1 independently confirmed that production OIDC/JWT/JWKS verification is absent. This entry addresses
the locally implementable cryptographic core without opening the currently unauthorized HTTP/provider/production
surfaces.

## 2. Frozen boundary

The v1 verifier is an RFC 9068 compact signed access-token profile with explicit `at+jwt` typing, exact `RS256`, one
snapshot-owned resource audience, closed claims/limits/clock inequalities, permanent key lineage, and domain-separated
RFC 8785 bindings. It rejects ID Tokens, encrypted/nested/general JSON JWTs, token-controlled key lookup, symmetric or
caller-selected algorithms, malformed or duplicate JSON, stale time/epoch/key facts, multi-audience tokens,
issuer/tenant/resource/project/permission mismatches, and revoked keys or token IDs.

Trust lineage/snapshots and the verification context remain package-private test inputs. The context owns the clock,
target tenant/resource/permission, while the active generation owns audience, epoch, keys, and revocation. The
verified principal is one-shot, and authorization holds the active-generation lease through PostgreSQL reads and the
exact typed operation plus transaction settlement; no reusable allow escapes the lease. No production constructor,
HTTP handler, OIDC discovery client, remote JWKS fetch, new database writer, or provider effect is added. A future
trust/context-source adapter requires its own versioned decision and review.

## 3. Ordered implementation

### Slice A - generated registry/profile

- add the two strict source/output profile schemas plus golden/negative fixtures and their manifest;
- add the deterministic registry generator and focused tests;
- generate the registry and package-private Go profile facts;
- bind separate registry/Go-output pipelines and all exact inputs/outputs in `contracts/generation.lock.json`;
- prove every historical generated identity/output unchanged; and
- keep verifier, principal, caller, network, database, and external dependency absent.

### Slice B - offline crypto kernel

- parse bounded duplicate-free compact JWS and package-private RSA JWK snapshots;
- verify only exact `RS256` using the Go standard library;
- validate active key lineage/interval/rotation/revocation, explicit type, issuer, single audience, exact time bounds,
  epoch, tenant/resource/project and required-permission binding;
- return an opaque redacted one-shot `VerifiedPrincipal` with no bearer-token retention; and
- keep snapshot minting package-private and production call graph empty.

### Slice C - authz binder and independent matrix

- remove public `authz.Request.Subject` and the PostgreSQL `Authorize(ctx, authz.Request)` bypass;
- remove standalone production `TenantReadCapability.Authorize`; a future read must execute its named typed read
  inside the sealed lease;
- replace raw-`SubjectRef` actor inputs across the read helper, five RBAC mutations, and three JWT-user idempotency
  durable-coordination methods with one-shot `*authn.VerifiedPrincipal`;
- run resource/fact reads, evaluation, the exact typed operation, and commit/rollback/unknown-outcome settlement inside
  one authz-owned callback-scoped operation and active-generation lease;
- preserve lexical `SubjectRef` for stored contract facts without treating it as request authentication;
- prove zero/literal/tamper/stale/cross-profile/one-shot/concurrent/callback-escape/default-deny behavior and exact
  context/RBAC binding, including invalidation before operation/during commit and raw-actor forbidden scans;
- run generated same-bits, focused normal/race, vet/build, secret and forbidden-surface checks; and
- obtain an independent fixed-candidate P0/P1/P2 verdict.

Slice C implementation evidence is recorded in
[`identity-verifier-authz-binder-implementation-20260824.md`](identity-verifier-authz-binder-implementation-20260824.md).
Its fixed candidate `d6ae9c789f5be06612764c06a5649f5ebd1557c7` is based on Slice B's independently approved review commit
`d2e464be0f3e54aa25e55d6cca7d4f744b04bc1c` and is independently
[approved](identity-verifier-authz-binder-independent-review-20260824.md) by review commit
`aa83e37112f6e80c8e4553f931c30c99043ce6a7` with `P0=0/P1=0/P2=0`. This completes the ordered A-C local
implementation/review package without changing any Gate.

## 4. Acceptance and stop conditions

Each slice stops before the next authority-bearing slice until its fixed candidate is independently approved. The
production dependency direction is `store/postgres -> authz -> authn`; Slice B does not touch authz/store and Slice C
owns both request/evaluator and the eight reviewed PostgreSQL seams. Any additional algorithm, token kind, claim
mapping, external JOSE dependency, trust/context provisioning, discovery/JWKS network path, or production consumer
requires a new explicit versioned boundary rather than widening v1.

Passing these slices is non-Gate evidence. Production OIDC/JWKS refresh, HTTP enforcement, current whole-schema tenant
isolation, current vulnerability/secret/limit evidence, accepted durability aggregation, deployment and publication
remain open; `G-SECURITY-P1` stays `IN PROGRESS`.
