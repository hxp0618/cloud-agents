# Worker/Supervisor Proto v1alpha1

`kernel.proto` and `worker_supervisor.proto` are the only editable wire authority for this RPC surface. The JSON files
under `fixtures/` describe semantic conformance inputs and expected decisions; they are not an HTTP/JSON API and must
not be served as a second wire.

P1-A intentionally defines only negotiation, capability and health discovery plus an operation/attempt/receipt seam.
It does not authorize workspace, credential, ingress, volume, scheduler, Provider, or other workload side effects.

`WorkerExecutionService` is hosted by the Worker. The Supervisor is its mTLS-authenticated client; fixtures use
`authenticatedClientSpiffeId` for the caller and `authenticatedServerSpiffeId` for the Worker. Generation/fencing never
substitutes for either identity. Every request after `Negotiate` carries the exact server-issued protocol version,
negotiation ID and expiry. The receiver validates that tuple against authoritative negotiation state bound to both mTLS
identities; an echoed expiry cannot extend the authoritative expiry.

## Bounds and canonical idempotency

Before protobuf decode, clients and servers cap every request/response at 1,048,576 bytes. A negotiated descriptor may
lower but never raise that limit. `max_repeated_items` has a hard ceiling of 64 and `max_string_bytes` a hard ceiling of
1,024 UTF-8 bytes. More specific limits are: supported versions 8; capabilities/finalizers/results 64; identifiers and
idempotency keys 256 bytes; stable error codes and media types 128 bytes; `redacted_summary` 512 bytes; extension data
65,536 bytes. Oversize data is rejected before side effects, and implementations must configure their HTTP/2,
ConnectRPC and gRPC receive limits rather than relying only on post-decode validation.

`OperationEnvelope.canonical_request_sha256` is the raw SHA-256 of RFC 8785 canonical JSON built from exactly these
normalized ProtoJSON-name members: `operationId`, `idempotencyKey`, `scope`, `fencing` containing only `leaseId` and
decimal-string `generation`, UTC RFC 3339 `deadline`, enum-name `requiredCapability`, `command`, and ordered
`finalizers`. The raw fencing token and the digest field itself are excluded. Absent/default members are represented as
their validated semantic values; unknown fields are rejected before canonicalization. A receiver recomputes the digest
and compares all retries for an idempotency key before any side effect. This keeps a renewed transport/fencing secret
out of durable idempotency state while still binding lease generation and operation intent.

## Descriptor and role status

The original P1-A source/fixture slice was `NOT_GENERATED`; that is historical, not current repository status. Generated Worker Proto/Connect code now exists under [`sdk/go/gen/cloudagents/worker/v1alpha1`](../../../sdk/go/gen/cloudagents/worker/v1alpha1). `fixtures/descriptor.golden.json` remains an expected shape, not a generated binary descriptor. Reproducible generation and full compatibility/Gate closure still require the locked generator and fixed-candidate evidence; code presence alone does not close `G-CONTRACT`.

The separate Runtime streaming surface lives under [`../runtime/v1alpha1`](../runtime/v1alpha1). Neither existing protocol should be relabeled as outbound RemoteWorker enrollment/control without a versioned contract decision. The [foundation architecture](../../../docs/plan/cloud-agents-platform/02-target-architecture.md) separates these roles while preserving their current identity, bounds and fencing requirements.
