# P1-A3 SDK / Identity / Closure implementation entry - 2026-08-20

- Status: **ADR-0007 APPROVED - ORDERED IMPLEMENTATION ENTRY**
- Baseline ref: `44524fba42b3e43b15746e3b0c6e431f26a1dd07`
- Branch: `codex/cloud-agents-platform-p1`
- Decision authority: ADR-0007 sections 1-4, 7, 10 and 11
- Scope: generated public SDK and identity conformance followed by bounded closure review
- This entry does not authorize HTTP routes, P2, provider or other external side effects, production database writes,
  deployment, publication, release, or Gate closure

## 1. Current boundary

P1-A1 and P1-A2 established the editable JSON Schema/OpenAPI/Proto roots, strict semantic fixtures, three Go module
boundaries, PostgreSQL kernels, generated internal registries, and typed internal services. The public Go SDK still
contains only its module declaration and package documentation; no TypeScript SDK package exists. The generation
lock therefore still reports generated SDK replay, Proto descriptor/breaking checks, N/N-1 readers, and response/watch
unknown-field preservation as missing.

ADR-0007 already fixes the relevant authority: JSON Schema 2020-12 owns public JSON models; OpenAPI owns only HTTP
routes and response metadata; Proto3 owns Worker/Supervisor and Platform Adapter wire messages; generated artifacts
are derived and must carry exact contract, generator, configuration, and output digests. This record introduces no
new wire or identity decision. It only orders implementation of that approved boundary.

## 2. Frozen constraints

1. JSON Schema and Proto remain the only editable data-model authorities. Generated Go/TypeScript files, server
   validators, descriptors, and clients may not become a second hand-edited source.
2. `NamespaceRef` uses strict keys, ASCII lowercase namespace/kind, NFC id, RFC 8785 bytes, SHA-256 digest text, and
   the derived URN exactly as ADR-0007 specifies. `SubjectRef` preserves issuer and subject bytes exactly and uses its
   separately versioned canonical profile. No trim, lowercase, URI rewrite, percent decode, lossy Unicode-to-ASCII
   mapping, or operation-specific authorization narrowing may enter either public identity type.
3. Mutation input validators reject unknown fields. N/N-1 response/watch readers preserve unknown JSON values only
   in an explicit sidecar and may not use that sidecar to broaden mutation admission or reinterpret known fields.
4. The public Go module is `github.com/hxp0618/cloud-agents/sdk/go`; the private, not-for-publication TypeScript
   package identity is `@synara/cloud-agent-platform-sdk`. Neither may import a service implementation or use
   `replace`, `workspace:`, `file:`, a Git dependency, or a cross-repository path.
5. SDK generation uses checked-in source, configuration, templates, tests, and exact toolchain pins. Each language
   has a generated manifest binding the contract manifest, generator source manifest, configuration digest, and
   output tree digest. Regeneration must be byte-identical.
6. The first slices are pure contract/identity work. They add no server route, database write, worker action,
   provider call, session/turn/execution path, credential operation, deployment, publication, or release channel.
7. Every slice remains non-Gate evidence. Only a later immutable closure record reviewed independently against all
   exit criteria may change a Gate, and this entry does not pre-authorize that change.

## 3. Ordered implementation slices

### Slice A - generated common identity profile

Generate Go and TypeScript `NamespaceRef` and `SubjectRef` models, strict validators, canonical bytes, digest helpers,
and NamespaceRef URN helpers from the versioned common schemas and canonical/negative fixtures. Add deterministic
language manifests and generation-lock pipelines. Both implementations must replay the same fixtures, including
Unicode scalar, NFC, exact key set, code-point bounds, canonical escaping, digest mismatch, and issuer exactness.

This slice deliberately excludes HTTP clients, management resource models, Proto output, N/N-1 readers, and Gate
closure. It is the minimal trustworthy identity base for later mapping and service seams.

### Slice B - generated JSON contract SDK and server seam

Generate the approved common/platform JSON models, mutation validators, response/watch sidecar readers, and the
Managed Agent/Managed Host OpenAPI client and Go server-validation seam. Replay golden, negative, N/N-1, stable error,
pagination/cursor, idempotency, cancellation/deadline, and path/body authority cases across TypeScript, Go, and the
server mapping. Use a fixture server only; do not add or enable a production HTTP route.

### Slice C - Proto/consumer closure and independent review

Generate descriptor sets plus ConnectRPC and gRPC-compatible client/server mappings for the existing Worker and
Platform Adapter Proto authorities. Keep all calls local to conformance fixtures and mTLS negative tests; no worker or
adapter side effect is authorized. Verify `GOWORK=off` Go consumers and exact packed TypeScript consumers from fresh
temporary directories without workspace/file/Git dependencies. Record generation same-bits, module/import DAG,
mapping, security, secret, dependency/license, and forbidden-surface scans for independent review.

## 4. Acceptance and explicit non-claims

The A3 implementation package is complete only when the fixed records include:

- deterministic generated manifests and byte-identical regeneration for each SDK/descriptor/server seam;
- Go/TypeScript/server replay of the same versioned golden, negative, and N/N-1 fixtures;
- exact-pinned external consumer pack/install/build/call evidence with no local dependency escape;
- Proto descriptor/breaking and Connect/gRPC compatibility evidence;
- module/import, cancellation/deadline, unknown-field, stable-error, identity, secret, dependency/license, and
  forbidden-surface evidence;
- an independent review tied to immutable source and output digests.

Passing an individual slice does not close `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, `G-SECURITY-P1`,
`G-SUPPLY-CHAIN`, or any aggregate Gate. It does not claim a published SDK, running public API, deployed service,
production identity, production database mutation, Platform RC, Beta, or GA.
