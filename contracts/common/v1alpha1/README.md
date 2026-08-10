# JSON contract profile v1alpha1

This directory freezes the bootstrap JSON behavior; it does not close `G-CONTRACT`.

## Unknown fields

- Mutation request schemas are closed (`additionalProperties: false`). Servers reject unknown request fields before
  any state change. In particular, `ProjectCreateRequest` accepts only client-authored fields; `tenantId` comes only
  from the path, while UID, tenant reference, state, resource version, and timestamps are server-owned.
- Response and watch schemas also describe a closed, validated typed projection. A generated SDK decoder must first
  retain the original JSON bytes (or an equivalent lossless unknown-member sidecar), then validate and expose the
  known projection. Re-encoding a response/watch object received from a newer compatible server must merge preserved
  unknown members unless the caller explicitly requests a lossy conversion.
- Preservation does not turn an unknown member into a trusted typed field. Authorization, tenancy, idempotency,
  canonical digests, and mutation decisions operate only on fields recognized by the selected contract version.

Thus schema validation and forward preservation are separate seams: unknown mutation input is rejected, while
unknown response/watch data is retained outside the typed projection. No schema simultaneously claims to both accept
and reject the same member.

## NamespaceRef canonicalization

Namespace references use the repository's strict RFC 8785 profile: exactly three string properties (`id`, `kind`,
`namespace`), NFC identifiers, no lone UTF-16 surrogates, JCS key ordering, UTF-8 bytes, SHA-256 digest text, and a
derived `urn:cloud-agents:ref:sha256:<hex>` identity. The bootstrap implementation is deliberately limited to this
shape; it is not advertised as a generic RFC 8785 implementation.
