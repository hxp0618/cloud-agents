# Standalone Platform v0.1 — Worker operation-admission P1 kernel

- Slice: local, transport-neutral `OperationAttemptEnvelope` admission
- Date: 2026-08-27 (Asia/Shanghai)
- Base: `a72ca9b15a5dd2292d623800432b93376c0be080`
- Candidate: recorded after the implementation commit is frozen

## Scope

This slice adds a versioned, in-memory admission seam to
`services/worker`. `AdmitOperation` binds a decoded operation attempt to the
configured Worker identity, the authenticated client and server-issued
negotiation binding, the operation-dispatch capability, a fixed lease and
generation authority, a normalized namespace reference, a bounded deadline,
supported command shape, unique finalizers, and a recomputed canonical SHA-256
request digest. Exact retries return a detached replay claim; later attempts
for one immutable operation must use a strictly greater attempt number while
retaining operation intent and lease generation. Conflicting
operation/idempotency/digest/fencing identities fail closed. Admission state
is bounded and process-local.

The claim contains only normalized references, timestamps, and digests. The
raw fencing token, command payload, and request protobuf are not retained. The
profile is `cloud-agents/worker-operation-admission/v1alpha1`; its metadata
explicitly declares `ExternalSideEffects=false`.

## Files owned by this slice

- `services/worker/operation_admission.go`
- `services/worker/operation_admission_test.go`
- `services/worker/service.go`
- `services/worker/README.md`
- `services/worker/doc.go`
- `services/worker/supervisor/README.md`
- `services/worker/supervisor/client.go`
- `services/worker/supervisor/client_test.go`
- `services/worker/go.mod`
- `services/worker/go.sum`

The dependency checksum entries only make the existing generated common
identity normalizer available under standalone `GOWORK=off` module checks.

## Fail-closed boundary

The implementation rejects nil/cancelled contexts, oversized decoded wire
messages, unknown protobuf fields (recursively), missing or malformed
identities and negotiation bindings, unnegotiated capabilities, missing or
stale fencing authority, invalid scopes/deadlines/commands/finalizers,
malformed extension payloads, missing or non-32-byte canonical digests, and
idempotency conflicts. It performs no database, network, provider,
workspace, credential, artifact, receipt, deployment, or release operation.
`ExecuteOperation` and `GetOperationReceipt` remain stable
`NOT_IMPLEMENTED`/`Unimplemented` no-ops.

The existing Supervisor `Bind` profile remains health-only. The fixed
`BindOperationAdmission` method is the only way this slice negotiates the
operation-dispatch capability; it is a separate, named profile and still does
not dispatch operations. Health checks echo the capabilities of whichever
fixed profile was bound.

## Verification contract

From `services/worker` with the pinned Go 1.26.6 toolchain:

```text
GOWORK=off GOFLAGS=-mod=readonly go test ./... -count=1 -timeout=5m
GOWORK=off GOFLAGS=-mod=readonly go test -race ./... -count=1 -timeout=5m
GOWORK=off GOFLAGS=-mod=readonly go vet ./...
GOWORK=off GOFLAGS=-mod=readonly go mod tidy -diff
```

The focused tests cover the golden RFC 8785 projection and digest, replay and
conflict behavior, caller-mutation isolation, raw-token non-retention,
unknown-field recursion, scope NFC normalization, deadline/fencing/identity/
capability failures, bounded extension validation, capacity, cancellation,
and the unimplemented writer paths. These checks are compile/unit evidence
only; they do not establish production Worker, HTTP/TLS, database, provider,
deployment, release, or Gate evidence.

## Explicit non-claims

This slice does not change any D-053-MIG-000014.r1/r2 source/profile/schema/
manifest/SQL/catalog/archive/review bytes, does not alter canonical or
production migration runners, and does not authorize production database
writes, HTTP/P2/provider calls, deployment, publication, release, or Gate
closure. The existing D-053-MIG-000014.r2 localdev/read-only authority and its
independent review remain unchanged.
