# Worker P1-A contract runtime kernel

This module provides a transport-neutral, in-memory implementation of the
generated `WorkerExecutionService` contract. It supports strict v1.0
negotiation, capability admission, identity-bound negotiation bindings, health
checks, and a versioned operation-admission seam. Negotiation identifiers are
opaque, random by default, and expire from in-memory state. Their
identifier-specific limit is 256 UTF-8 bytes (stricter than the general 1 KiB
string ceiling).

`AdmitOperation` is local admission only. It binds an operation attempt to the
configured Worker identity, negotiation binding, lease/generation authority,
normalized scope, and recomputed canonical request digest. Its claim retains
only references and SHA-256 digests; raw fencing tokens and command payloads
are not retained. The profile is
`cloud-agents/worker-operation-admission/v1alpha1` and advertises no external
side effects. Admission state is bounded and ephemeral; it is not a durable
receipt, execution lease, or authorization to run a provider.

`ExecuteOperation` and `GetOperationReceipt` deliberately return
`Unimplemented`; they do not dispatch work, persist receipts, or perform any
other side effect. The operation capability is therefore an admission-only
capability in this slice. No PostgreSQL, provider, workspace, credential,
artifact, production dispatch, deployment, or release operation is included.

`NewHandler` exposes the generated Connect HTTP handler with 1 MiB read/send
limits. It is a decoded handler seam for in-process integration only: this
package does not provide an HTTP server, TLS/mTLS listener, or claim that
pre-decode limits are enforced by a network edge. Callers must provide an
explicit `IdentityProvider`; the default rejects requests without a transport
identity. Request-carried `expected_*` identities are peer constraints, never
authentication.

This is standalone Platform v0.1 code evidence only. Existing Gates remain
open.
