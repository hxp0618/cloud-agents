# Supervisor P1-B admission client

This package is the Supervisor-side consumer of the generated
`WorkerExecutionService` Proto/Connect contract. It implements two bounded
v1.0 binding profiles:

- sends exactly the generated negotiation/health capability profile;
- binds the server-issued negotiation identifier, expiry, protocol descriptor,
  and authenticated Worker identity in in-memory state;
- validates health responses against the original descriptor and clears an
  expired binding before making another RPC;
- `BindOperationAdmission` explicitly negotiates the Worker operation-
  admission capability without changing the default health-only `Bind`;
- returns stable `Unimplemented` errors for operation dispatch and durable
  receipt retrieval.

The caller supplies an already configured generated Connect client. This
package does not construct an endpoint, listener, TLS/mTLS configuration,
database lease, provider call, workspace/credential/artifact writer, or
receipt. The operation-admission profile is still local admission-only in the
current Worker slice; this Supervisor package does not dispatch it. The transport remains the authority
for the authenticated client identity. No production HTTP or external side
effect is implied.

The limits are shared with `services/worker` P1-A (`1 MiB` wire,
`64 KiB` payload, `64` repeated items, `1 KiB` strings, `256`-byte negotiation
identifiers). The two profiles are fixed in code and cannot be selected by an
arbitrary caller capability list at runtime.
