# Cloud Agents Platform contracts

This tree is the P1 public contract authority established by ADR-0007. It is separate from the existing Portable
Runtime v2 wire and from every Synara/T3 legacy contract oracle.

## Authority map

| Surface                                   | Editable authority         | Derived artifacts                                   |
| ----------------------------------------- | -------------------------- | --------------------------------------------------- |
| common/platform JSON models               | JSON Schema 2020-12        | validators, TS/Go SDK models, semantic fixtures     |
| Managed Agent / Managed Host HTTP APIs    | OpenAPI 3.1 JSON documents | HTTP clients, server interfaces, bundled API docs   |
| Worker/Supervisor / Platform Adapter RPCs | Proto3 source              | descriptor set, ConnectRPC/gRPC clients and servers |

OpenAPI owns routes, methods, security, parameters, headers, status codes, media types, and operation IDs. It must
reference external JSON Schema files for data models. Proto owns its RPC messages and services; JSON fixture files next
to Proto sources are conformance vectors, not a second transport.

## P1-A bootstrap status

- JSON Schema, foundation OpenAPI, Proto source, and semantic fixtures are authored; checked-in schemas and 30 fixture
  cases pass the independently reviewed `Ajv2020` plus in-repo semantic bootstrap path. Mutation inputs reject unknown
  fields; response/watch unknown-field preservation remains a generated-reader sidecar seam described in
  `common/v1alpha1/README.md`, not a relaxation of mutation schemas.
- The three Go source modules and exact Go toolchain boundary are established.
- `contracts/generation.lock.json` binds the bootstrap source/tool/dependency digests and explicitly reports
  `BOOTSTRAP_VALIDATED`; generated SDKs, compiled Proto descriptors, ConnectRPC/gRPC code, full OpenAPI validation,
  official JSON Schema suite, and N/N-1 compatibility evidence remain incomplete until follow-up P1-A slices land.
- No contract in this tree proves a running Control Plane, Worker, Adapter, database, Provider turn, deployment, or
  release candidate. `G-CONTRACT`, `G-DATA`, `G-AUTHORITY-P1`, and `G-SECURITY-P1` remain open.

`x-cloud-agents-*` members are descriptive annotations unless a checked-in bootstrap semantic validator and named
fixture explicitly enforce them. Ajv accepting an annotation is never evidence that a server-side tenant lookup,
authorization decision, response unknown-field preservation, or path/body authority check exists.

Every generator must run from a checked-in immutable tool lock, record input and output digests, and leave a clean
tree after regeneration. Legacy Synara helpers and prose stay under provenance-bound `conformance/**/legacy-oracles`
or service `internal` migration candidates; they may not be imported as production contract authority.
