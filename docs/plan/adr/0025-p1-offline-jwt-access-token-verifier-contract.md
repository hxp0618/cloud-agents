# ADR-0025: P1 versioned offline JWT access-token verifier contract

- Status: Accepted under the standing P1 execution approval on 2026-08-23
- Scope: generated verifier profile, offline asymmetric JWT/JWK verification, opaque principal, and authz binding
- Depends on: ADR-0007, ADR-0008, ADR-0011, G-CONTRACT R4, and G-SECURITY-P1 R1
- Decision owner: hxp0618
- Implementation executor: Codex
- Gate effect: none

The standing owner direction authorizes continued implementation of the approved P1 plan without another approval
round. ADR-0008 already places issuer, audience, subject, scope, tenant, version, expiry, rotation, and revocation
negatives in P1-A3. This decision narrows that approved work into three locally reviewable slices. It adds no HTTP
surface, remote discovery, provider effect, production trust root, database writer, deployment, publication, or Gate
closure.

## Context

The public contracts and SDKs preserve `SubjectRef` as an exact lexical identity tuple. The current authorization
evaluator and PostgreSQL services validate that tuple, but no production Go code verifies a JWT signature, binds a
signing key to an issuer, validates an access-token audience, or converts signed claims into a principal that is
non-mintable outside the verifier package and tamper-detecting under the reviewed call graph.
The current OpenAPI `BearerAuth` declarations are wire-format placeholders; they are not authentication evidence.

G-SECURITY-P1 R1 therefore correctly keeps production OIDC/JWT/JWKS verification open. A future HTTP adapter must not
turn arbitrary decoded claims into `authz.SubjectRef`, and a token must not choose its own algorithm, key source,
issuer, resource audience, tenant, project, scope, or revocation state.

The verifier profile follows the security boundaries in
[RFC 8725](https://www.rfc-editor.org/rfc/rfc8725.html) and the JWT access-token validation rules in
[RFC 9068](https://www.rfc-editor.org/rfc/rfc9068.html). It deliberately verifies only signed JWT access tokens. It
does not accept an OpenID Connect ID Token as an API access token and does not implement OIDC discovery, authorization
flows, token exchange, introspection, or remote JWKS retrieval.

## Decision

### 1. One versioned generated verifier profile

Add the immutable identity `platform-identity-verifier/v1`. Editable JSON Schema source, one registry source document,
golden and negative fixtures, and generator code own the profile. The generated registry and generated package-private
Go facts are derived outputs. The generation lock binds every source, generator/library, test, output, profile digest,
registry digest, and declared non-claim.

The v1 profile freezes:

- compact JWS access tokens only, with exactly three base64url segments;
- explicit access-token type `at+jwt` or `application/at+jwt` using ASCII case-insensitive media-type comparison;
- `RS256` as the only accepted algorithm and RSA JWKs as the only key type;
- required RFC 9068 claims `iss`, `sub`, `aud`, `exp`, `iat`, `jti`, and `client_id`;
- required `scope` plus collision-resistant Cloud Agents claims for subject kind, tenant, optional project,
  security epoch, and token-profile version;
- exact decoded-string issuer, subject, audience, tenant, project, key ID, token ID, client ID, and scope semantics;
- bounded token, header, claim, key-set, string, array, lifetime, and clock-skew limits; and
- domain-separated digests for the profile, registry, trust snapshot, token input, and verified principal.

Any later algorithm, token type, claim mapping, or bound expansion requires a new profile identity. v1 bytes and
semantics remain immutable.

The collision-resistant claim names and token-profile value are exact:

| Meaning          | Exact claim/value                                                                        |
| ---------------- | ---------------------------------------------------------------------------------------- |
| Subject kind     | `https://schemas.cloud-agents.dev/claims/subject-kind`                                   |
| Tenant           | `https://schemas.cloud-agents.dev/claims/tenant-id`                                      |
| Optional project | `https://schemas.cloud-agents.dev/claims/project-id`                                     |
| Security epoch   | `https://schemas.cloud-agents.dev/claims/security-epoch`                                 |
| Token profile    | `https://schemas.cloud-agents.dev/claims/token-profile` = `cloud-agents-access-token/v1` |

The generated v1 limits are also identity, not implementation defaults: token 16 KiB; decoded protected header 1
KiB; decoded claims 12 KiB; JSON depth 4; trust snapshot 256 KiB; 32 lifetime key-lineage records per issuer/profile;
exactly one audience; 64 scopes; 4,096 revoked token IDs; `kid` 128 ASCII bytes; issuer/audience 512 Unicode scalars;
subject/client/token ID 256 Unicode scalars; tenant/project/scope item 128 ASCII bytes; token lifetime 3,600 seconds;
clock skew 60 seconds; and trust-snapshot validity 86,400 seconds. A smaller runtime request bound may be imposed, but
a larger one requires a new profile.

The lexical and collection rules are exact:

- every identity string is the decoded JSON string value, compared by Unicode scalar sequence with no trim,
  normalization, percent decoding, URI normalization, or case fold; different JSON escape spellings of the same
  decoded value are identical;
- issuer and the single resource audience use the existing closed absolute-URI lexical profile: an ASCII URI scheme
  followed by `:`, a valid UTF-8 suffix, no C0/DEL control, and only complete `%HH` escapes;
- `sub`, `client_id`, and `jti` are non-empty valid UTF-8 decoded strings within their scalar bounds; `client_id` and
  `jti` additionally reject C0/DEL controls, while `sub` deliberately preserves the current `SubjectRef` value
  profile;
- `kid`, tenant, project, and resource IDs use the current opaque-identifier grammar: a one-byte value is
  alphanumeric; a longer value has ASCII alphanumeric first/last bytes and ASCII alphanumeric or `._~-` interior
  bytes, within the declared byte bound;
- `aud` must be one non-empty JSON string; arrays, duplicates, empty values, and multiple audiences are rejected;
- `scope` is one non-empty JSON string split only on one ASCII SP. Leading, trailing, repeated, tab, newline, empty,
  duplicate, or more than 64 scope items are rejected. Each item is 5..128 ASCII bytes and matches
  `^[a-z][a-z0-9-]*\.(create|get|list|watch|update|delete|act|bind)$`; the principal sorts the unique set by unsigned
  UTF-8 bytes; and
- security epoch, snapshot generation, and every NumericDate are base-10 JSON integers, not floating point or exponent
  spellings. Epoch/generation are 1..9007199254740991 and NumericDate is 0..253402300799 inclusive, keeping every
  RFC 8785 number inside the exact I-JSON safe-integer range.

For one injected clock instant, `nowSecond = clock.UTC().Unix()`. v1 uses the exact half-open checks
`iat <= nowSecond + 60`, optional `nbf <= nowSecond + 60`, `nowSecond < exp + 60`, `iat < exp`,
`exp - iat <= 3600`, and optional `nbf < exp`. A key may verify a token only when
`key.notBefore <= iat < key.notAfter`. The immutable snapshot must satisfy
`snapshot.notBefore <= nowSecond < snapshot.expiresAt` and
`snapshot.expiresAt - snapshot.notBefore <= 86400`; snapshot validity receives no extra skew. An old enabled key may
remain available to verify an unexpired token issued inside its interval, but disablement or revocation fails closed.

All verifier-identity digest text is `sha256:<64-lowercase-hex>`. The byte formula is
`SHA-256(UTF8(domain) || 0x00 || payload)`. JSON payloads are RFC 8785 canonical UTF-8; the digest member named in the
table is absent from its own projection, not encoded as null or empty:

| Digest             | Exact domain                                                    | Exact payload projection                                                                                                                                                                                     |
| ------------------ | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Profile            | `cloud-agents/platform-identity-verifier/profile/v1`            | complete closed generated profile object except `profileDigest`                                                                                                                                              |
| Registry           | `cloud-agents/platform-identity-verifier/registry/v1`           | complete closed generated registry object, including `profileDigest`, except `registryDigest`                                                                                                                |
| Trust snapshot     | `cloud-agents/platform-identity-verifier/trust-snapshot/v1`     | closed immutable snapshot object, including profile/registry/previous-snapshot digests and full public-key, lineage, epoch, revocation, audience, generation and time facts, except `snapshotDigest`         |
| Token input        | `cloud-agents/platform-identity-verifier/token-input/v1`        | exact admitted ASCII compact-token bytes, not JSON                                                                                                                                                           |
| Verified principal | `cloud-agents/platform-identity-verifier/verified-principal/v1` | closed principal object, including the four prior digests, canonical lowercase token type, decoded identity/context strings, sorted scopes, key/token IDs, epoch and integer times, except `principalDigest` |

The principal payload contains no raw token, signature, unknown claim, JWK, mutex, active-generation cell, or
consumption state. Strict canonical base64url admission occurs before token-input hashing. Ordinary file hashes in
`generation.lock.json` retain the repository file-hash convention and are not verifier-identity digests. Every
set-valued projection array is duplicate-free and sorted by unsigned UTF-8 bytes; public keys and lineage records sort
by `kid`, while object member order comes from RFC 8785.

### 2. Closed algorithm and key boundary

RFC 9068 requires conforming implementations to support `RS256`; v1 supports exactly that mandatory interoperable
algorithm through the Go standard library. It does not add a JOSE runtime dependency.

The verifier rejects `none`, every HMAC algorithm, every unlisted asymmetric algorithm, a missing or unknown `kid`,
and any algorithm/key-type mismatch. The admitted JWK object is closed to `kty`, `kid`, `alg`, `use`, `key_ops`, `n`,
and `e`; the containing snapshot key record separately owns `enabled`, `notBefore`, and `notAfter`. Each accepted JWK
is exact-bound to:

- `kty=RSA`, `alg=RS256`, `use=sig`, and `key_ops=["verify"]`;
- one unique bounded ASCII `kid`;
- an unpadded canonical base64urlUInt RSA modulus with no leading zero octet, between 2048 and 4096 bits, and the exact
  exponent-65537 encoding `e="AQAB"`;
- an issuer-owned trust snapshot, its profile and security epoch, and an issuance-validity interval; and
- an enabled, non-revoked state.

Requiring both `use=sig` and `key_ops=["verify"]` is a deliberate closed-profile interoperability tradeoff. Although
RFC 7517 recommends that ordinary JWK producers normally use one or the other, v1 requires both and requires them to
agree; an issuer that omits either needs a new reviewed profile or a provisioning adapter and is not inferred into
v1. For one issuer/profile lineage, a `kid` is permanently bound to one RSA public key and may never identify
different key material. A later snapshot may retain the same `kid` only for the same public key; new material requires
a new `kid`. The package-private lineage builder enforces this for test generations, and a future authenticated
provisioner must prove the same history before admission.

Private RSA fields, symmetric material, duplicate key IDs, duplicate decoded JSON keys, `jku`, `jwk`, `x5u`, `x5c`,
`crit`, an unrecognized protected-header member, padded or non-canonical base64url, and key material supplied by the
token are rejected. No token header can trigger file, database, DNS, HTTP, or other network lookup.

### 3. Bounded strict parsing

The verifier parses UTF-8 JSON with duplicate decoded-key rejection, integer-only bounded numeric dates, one complete
top-level object, and no trailing input. It accepts no JWE, detached payload, unencoded payload, nested JWT,
multi-signature, or general JSON serialization.

The protected header is a closed object containing only `alg`, `kid`, and `typ`. `alg` and JWK members are
case-sensitive. `typ` is admitted by ASCII case-insensitive comparison and stored as the canonical lowercase
`at+jwt`. The claims object may carry additional signed issuer attributes, but they remain ignored ordinary data after
the complete object is parsed within the token size/depth bounds. Unknown claims never enter as interpreted principal
fields or authorization inputs; they are transitively committed only by the whole-token input digest. Every required
profile claim must have the exact expected type and closed syntax.

### 4. Exact access-token and request binding

Signature success is necessary but not sufficient. Verification consumes the package-private owned context defined
below and also requires:

1. token `iss` exactly equals the selected trust snapshot issuer;
2. the selected key belongs to that same snapshot, profile, issuer, epoch, exact algorithm, and issuance interval;
3. token `aud` is exactly one decoded string equal to the snapshot-owned current resource-server audience;
4. `typ` distinguishes the access token from an ID Token or another JWT kind;
5. `exp`, `iat`, optional `nbf`, maximum lifetime, fixed clock skew, key issuance interval, and snapshot validity satisfy
   the exact inequalities above against one owned clock instant;
6. neither `kid` nor `jti` appears in the active generation's revocation sets;
7. token security epoch exactly equals the active snapshot epoch; there is no caller-provided minimum epoch;
8. subject kind is one of `user`, `serviceAccount`, or `workload`, and issuer/subject use their exact decoded string
   values;
9. token tenant exactly equals the context-owned target tenant;
10. when the token carries a project, the context target must be a project resource with the exact same project ID;
    a project-bound token is rejected for tenant, organization, or an absent/different project target. A token without
    a project may reach a narrower project target, subject to the remaining scope and RBAC checks;
11. the context-owned required permission is present in the token scope set and later remains exact through the sealed
    authz request; and
12. the token-profile value, generated profile digest, and generated registry digest are exact.

Audience, tenant, project, permission, and scope comparisons never infer hierarchy or authorization. They prevent
substitution and weakening. The RBAC evaluator still resolves resource ancestry and grants from current database
facts.

### 5. Trust generation and verification context are owned authority

The crypto kernel consumes an owned, package-private trust snapshot. The snapshot contains the generated profile,
registry digest, issuer, exactly one current resource-server audience, RSA verification keys and permanent key
lineage, key intervals, generation, previous-snapshot digest, security epoch, revoked key/token IDs, and validity
window. It is ordinary validated data until a future, separately reviewed provisioning boundary authenticates its
source. No token, bearer header, arbitrary service caller, or HTTP field may supply or select these facts.

Every key, epoch, revocation, validity, or audience change creates a new immutable generation; snapshots are never
mutated in place. The package-private lineage owns the current-generation pointer and a read/exclusive lease. It marks
the old generation inactive under the exclusive lease and drains all in-flight authorization callbacks before
publishing the replacement. Verification and authorization may acquire only the current generation. Authorization
holds its read lease through context/principal revalidation, PostgreSQL fact reads, RBAC evaluation, the exact typed
protected operation, and transaction commit/rollback/unknown-outcome settlement. Invalidation therefore linearizes
either before the operation starts, causing fail-closed rejection, or after that operation and settlement finish. Once
invalidation returns, no principal, request, allow result, operation, or transaction from the old
digest/epoch/revocation state can start or remain active.

Generation begins at 1 and increases by exactly one. `previousSnapshotDigest` is absent at generation 1 and required
to equal the immediately preceding snapshot digest thereafter. The full, sorted lifetime key-lineage records make
`kid` reuse with different public material detectable; exceeding 32 lifetime records requires a new profile rather
than dropping history.

The package-private verification context owns one active trust generation, its clock, target tenant, target resource
level and opaque ID, and exactly one required permission using the scope grammar above. Tenant targets use the tenant
ID; organization/project targets use their own resource ID. This context is constructed from trusted resource-server
routing and tenant-capability facts, never from token claims or caller-selectable HTTP parameters. Slice B has only a
private test builder; a future production context/provisioning adapter requires a separately versioned decision.

Slice B intentionally exports no constructor that lets an arbitrary service, HTTP handler, or caller mint a trust
snapshot, lineage, clock, or verification context. Tests construct them inside package `authn`; black-box tests receive
them only through an `export_test.go` helper that is absent from production builds. A future OIDC/JWKS adapter must
introduce a separately versioned, authenticated provisioning and refresh boundary; it may not reuse test construction
or accept a URL/key from the token.

### 6. Opaque verified principal and authorization seam

Successful verification returns a pointer-only `VerifiedPrincipal` whose fields and constructor are package-private.
It binds the generated profile/registry, exact active-generation lease and snapshot digest, token digest, key ID,
issuer, subject kind/value, audience, client ID, sorted scopes, target tenant/resource/permission, optional token
project, security epoch, issued/not-before/expiry times, and token ID. Its canonical binding contains no bearer token or
raw JWT bytes.

The principal is request-scoped and one-shot. The authz binder atomically consumes it, reacquires its exact active
generation read lease, rechecks self-binding, profile/registry/snapshot digest, epoch, key/token revocation, snapshot
validity, token expiry, and context equality, and keeps that lease until the protected callback and any transaction
settlement end. A failed or completed consumption remains consumed. A second/concurrent consume, zero value, literal,
changed field, broken self-binding, cross-profile value, inactive generation, or failed verification cannot bind an
actor.

Slice C replaces, rather than supplements, the current production-shaped bypass. `authz.Request.Subject` and the
PostgreSQL `Authorize(ctx, authz.Request)` seam are removed, as are raw-`SubjectRef` actor parameters on RBAC mutation
and JWT-user durable-coordination paths. Every replacement protected store method accepts one
`*authn.VerifiedPrincipal`; an authz-owned callback binder creates an operation whose actor, target, permission,
self-binding, and callback-liveness fields are package-private. PostgreSQL resource resolution, membership/binding
reads, `authz.Evaluate`, the exact typed read/mutation, and transaction settlement all execute inside that one callback
and generation lease. No reusable `Allowed` decision may escape for a later database write or external action; only
the typed operation result/evidence may return after settlement. A retained request becomes invalid when the callback
returns.

`TenantReadCapability.Authorize` receives no standalone production replacement. The RBAC evaluator becomes an
internal step of a sealed `withVerifiedOperation` callback; focused tests may observe decision evidence through a
test-only harness. Any future protected read must name and execute its exact typed read inside the same lease instead
of asking for an allow result and acting later.

The migration covers every current non-test authorization caller, including the read helper, all five RBAC mutation
methods, and the three JWT-user idempotency durable-coordination methods. Their actor facts come only from the sealed
operation. `CreateMembershipInput.Subject`, `BindRoleInput.Subject`, database membership/binding rows, and other
lexical `SubjectRef` values remain stored target data, not authenticating actors. Machine coordination paths that
currently use independently versioned subject-digest/fencing profiles are not silently reclassified as JWT callers;
their separate authority claims remain unchanged.

The production dependency direction is fixed as `store/postgres -> authz -> authn`; `authn` must not import `authz` or
`store`. Shared `SubjectRef` lexical/canonical code may move to a neutral `internal/identity` package while `authz`
keeps a compatibility alias. Cross-package positive conformance lives in an external `package authn_test` test beside
`export_test.go`, avoiding a production constructor and import cycle. A forbidden-call-graph scan permits
`SubjectRef` in stored-fact/canonicalization code but rejects it in any production actor/authorize input.

The verifier returns stable typed categories such as malformed, unsupported profile/algorithm, unknown/revoked key,
invalid signature, issuer/audience/time/epoch/tenant/project/scope mismatch, revoked token, and internal failure.
Errors, logs, fixtures, and review records must never include token bytes, signature bytes, JWK private material, or
secret-bearing request headers.

## Ordered slices

### Slice A - generated contract and package-private profile only

Own only the two versioned verifier schemas, source/golden/negative fixtures and fixture manifest, registry and Go
generators/tests, generated registry, `internal/authn` profile facts, package scripts, JSON/contract-lock helpers, and
their generation-lock pipelines. Registry identity and generated-Go same-bits are separate pipelines. Prove all prior
generated outputs and historical profile identities byte-identical. Add no parser, signature verification, principal,
caller, HTTP route, database handle, or external dependency.

### Slice B - offline crypto kernel and opaque principal

Own only `internal/authn`: implement the strict compact-JWS/JWK parser, standard-library `RS256` verification,
package-private lineage/snapshot/context and active-generation lease, revocation/rotation/time/context binding, opaque
one-shot `VerifiedPrincipal`, stable redacted errors, and the complete negative matrix. Keep `authz`, `store`, trust
provisioning, and every production caller absent. Mutable runtime conformance is lock evidence but never input to the
immutable profile or registry digest.

### Slice C - authz binder, conformance matrix, and independent review

Own `internal/authz/rbac.go`,
`internal/store/postgres/{tenant_transaction.go,rbac.go,rbac_mutation.go,durable_coordination.go}`, their focused and
integration tests, plus the external `package authn_test` cross-package conformance test. Remove every raw-SubjectRef
actor/request bypass on the current JWT-user paths, bind the opaque principal to the callback-scoped authz
actor/operation, and wrap the whole `withTenantMutation` call so read/evaluate/typed operation/commit all remain inside
the active-generation lease. Cover issuer/audience/subject/scope/tenant/project/version/expiry/rotation/revocation,
exact context/RBAC binding, error redaction, one-shot and concurrent consumption, generated same-bits,
dependency/forbidden-surface scans, and independent fixed-candidate review. The slice does not add an HTTP handler or
trust-source adapter.

Each fixed candidate requires its own independent review before the next authority-bearing slice proceeds.

## Minimum conformance matrix

| Boundary           | Required cases                                                                                                                |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| Generated identity | stale source/output/lock, profile or registry digest mutation, previous generated outputs byte-identical                      |
| Compact JWS        | wrong segment count, padding, non-base64url, invalid UTF-8/JSON, duplicate decoded key, trailing input, oversize/depth        |
| Header             | missing/wrong `typ`, `alg=none`, HMAC/other alg, missing/unknown/duplicate `kid`, `jku`/`jwk`/`x5*`/`crit`/extra member       |
| JWK                | wrong issuer/profile/epoch, private/symmetric key, wrong kty/alg/use/key_ops, small/oversize modulus, exponent, duplicate key |
| Signature          | valid RS256, changed header/payload/signature, algorithm/key confusion, token-supplied key, malformed signature               |
| Claims             | iss/sub/aud/exp/iat/jti/client_id/scope/custom required claims, type/range/duplicate faults, ID-token substitution            |
| Time and rotation  | every exact half-open boundary, nbf, max lifetime, old/new overlap, outside key issuance interval, expired snapshot           |
| Revocation         | epoch mismatch, revoked key/jti, key-material reuse, generation replaced after verify, invalidate/authorize race              |
| Context            | forged context, audience substitution/multi-aud, tenant/resource/project substitution, scope weakening, profile mismatch      |
| Principal/authz    | zero/literal/tamper/stale/cross-profile, one-shot/concurrent consume, callback escape, exact decoded SubjectRef, default deny |
| Request bypass     | public SubjectRef request impossible, store accepts only verified principal, whole read/evaluate under active lease           |
| Operation lease    | invalidate after allow/before operation, during write/commit/settlement, no reusable allow, callback escape                   |
| Caller closure     | read helper, five RBAC mutations and three JWT-user idempotency paths reject raw SubjectRef actor inputs                      |
| Forbidden surfaces | no HTTP/fetch/DNS/file key lookup, OIDC discovery, provider/P2, database writer, deployment/publication/Gate closure          |

## Rejected alternatives

- Treat decoded JWT claims or `SubjectRef.Validate` as authentication proof: rejected; neither proves signature or
  issuer/key ownership.
- Accept an OpenID Connect ID Token at the resource server: rejected; it creates cross-JWT confusion.
- Read `jku`, `jwk`, `x5u`, or `x5c` from the token: rejected; token-controlled key discovery is not trust.
- Enable `HS256`, `none`, or caller-selected algorithms: rejected; it creates algorithm/key confusion and shared-key
  authority.
- Add a JOSE dependency before its own review: rejected for v1; the standard library is sufficient for the single
  frozen `RS256` profile.
- Fetch discovery/JWKS in the crypto kernel: rejected; provisioning, refresh, outage, SSRF, cache, and revocation
  authority require a separate versioned adapter.
- Keep public `authz.Request.Subject` beside a new binder: rejected; it would preserve a production-shaped bypass.
- Retrofit every stored `SubjectRef` fact in Slice B: rejected; stored identity data and verified request authority are
  separate boundaries. Slice C replaces only the request/store seam before any production request caller exists.

## Explicit non-claims

This decision does not implement or claim production OIDC login, authorization-code flow, discovery, remote JWKS
refresh, token issuance/exchange/introspection, DPoP, browser/session authentication, HTTP enforcement, provider/P2
side effects, production database writes, deployment, publication, release, or any Gate closure. It does not make a
test trust snapshot a production trust root. G-SECURITY-P1 and every aggregate Gate remain `IN PROGRESS`.
