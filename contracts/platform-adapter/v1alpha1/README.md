# Platform Adapter Proto v1alpha1

`platform_adapter.proto`, together with imported Worker kernel messages, is the only editable wire authority for the
out-of-process Platform Adapter surface. JSON files under `fixtures/` are semantic conformance vectors, not a second
HTTP/JSON transport.

`PlatformAdapterRegistryService` is hosted by the Supervisor/Control Plane; adapters are its mTLS-authenticated clients.
Registration returns no secret and supplies only an HTTPS callback endpoint without user-info, query or fragment.
`PlatformAdapterExecutionService` is hosted by the adapter; the Supervisor/Control Plane is its mTLS-authenticated
client and verifies the adapter server identity from the transport. Fixtures name both client and server identities
explicitly and never treat lease generation as either identity.

Registration and every execution-side request bind the server-issued protocol version, negotiation ID and expiry to
both mTLS identities. The receiver checks current authoritative negotiation state before capability, scope,
lease/generation, fencing, deadline and idempotency admission. Worker kernel message/list/string/transport limits and
canonical idempotency rules apply unchanged. P1-A defines no adapter implementation and performs no real scheduler,
volume, ingress, secret, billing, audit, or other platform side effect.

## Registration idempotency and recovery

`AdapterRegistrationRequest.canonical_registration_sha256` is the raw SHA-256 of RFC 8785 canonical JSON constructed
from exactly `adapterInstance`, `capabilities`, `expectedClientIdentity`, `expectedServerIdentity`, and `endpoint`.
Capabilities are rejected if unknown, then deduplicated by numeric enum value, sorted by ascending numeric enum value,
and emitted by canonical enum name. Namespace and workload identities use their validated normalized ProtoJSON names;
an optional certificate digest, when present, is included as canonical ProtoJSON base64. The endpoint includes the
validated HTTPS `connectUri` and `serverIdentity`.

The projection deliberately excludes `negotiation`, `deadline`, `registrationIdempotencyKey`, and the digest itself so
the same intent can be retried after a response loss with a fresh negotiation and deadline. The registry recomputes the
digest before mutation. Reusing a registration idempotency key with a different canonical digest fails closed as
`registration_idempotency_conflict`; raw request bytes, list order and duplicate capability entries cannot change that
decision.

`AdapterRegistrationReceipt` durably echoes both the idempotency key and accepted canonical digest.
`GetAdapterRegistrationReceipt` supports response-loss recovery by the same adapter: its key+digest+adapter lookup is
bound to a fresh, unexpired negotiation, `CAPABILITY_ADAPTER_REGISTRATION`, the expected registry server identity and
the mTLS-authenticated adapter client. A mismatched digest, adapter or client identity returns no receipt and performs
no registration mutation.

## Descriptor status and foundation reuse

The initial P1-A slice was `NOT_GENERATED`; current generated Proto/Connect code exists under [`sdk/go/gen/cloudagents/platformadapter/v1alpha1`](../../../sdk/go/gen/cloudagents/platformadapter/v1alpha1). `fixtures/descriptor.golden.json` is still an expected shape, not compiled descriptor evidence. Locked generation, compatibility, and fixed-candidate review remain required for `G-CONTRACT`; generated code alone closes no Gate.

This out-of-process extension protocol is not automatically the internal SandboxRuntime/OpenSandbox adapter or outbound customer-node protocol. Reuse only after mapping the actual roles and authority in the [foundation architecture](../../../docs/plan/cloud-agents-platform/02-target-architecture.md); do not expand this frozen wire's side-effect scope through prose.
