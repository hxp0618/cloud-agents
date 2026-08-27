# Worker P1-A contract runtime kernel

This module provides a transport-neutral, in-memory implementation of the
generated `WorkerExecutionService` contract. It supports strict v1.0
negotiation, capability admission, identity-bound negotiation bindings, and
health checks. Negotiation identifiers are opaque, random by default, and
expire from in-memory state.

`ExecuteOperation` and `GetOperationReceipt` deliberately return
`Unimplemented`; they do not dispatch work, persist receipts, or perform any
other side effect. No PostgreSQL, provider, workspace, credential, artifact,
production dispatch, deployment, or release operation is included.

`NewHandler` exposes the generated Connect HTTP handler with 1 MiB read/send
limits. It is a decoded handler seam for in-process integration only: this
package does not provide an HTTP server, TLS/mTLS listener, or claim that
pre-decode limits are enforced by a network edge. Callers must provide an
explicit `IdentityProvider`; the default rejects requests without a transport
identity. Request-carried `expected_*` identities are peer constraints, never
authentication.

This is standalone Platform v0.1 code evidence only. Existing Gates remain
open.
