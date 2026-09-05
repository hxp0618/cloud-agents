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
- `NewLocal` + `BindLocalDispatch` explicitly select the generated
  `cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1`
  profile through an opaque in-process Worker handle, then dispatch strict
  Probe/ValidateBinding attempts and replay detached, bounded receipts;
- the generic `New` constructor, `Bind`, and `BindOperationAdmission` retain
  their compatibility behavior and return stable `Unimplemented` errors for
  dispatch/receipt calls.

The generic caller supplies an already configured generated Connect client. The
local constructor instead accepts only a Worker-minted opaque handle; it never
accepts a URL, endpoint, selector, or caller capability list. This package does
not construct a listener, TLS/mTLS configuration, database lease, provider
call, workspace/credential/artifact writer, or durable receipt. The transport
remains the authority for the authenticated client identity. No production
HTTP or external side effect is implied.

The limits are shared with `services/worker` P1-A (`1 MiB` wire,
`64 KiB` payload, `64` repeated items, `1 KiB` strings, `256`-byte negotiation
identifiers). The two profiles are fixed in code and cannot be selected by an
arbitrary caller capability list at runtime.
