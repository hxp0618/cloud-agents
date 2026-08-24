# G-CONTRACT R5-B2 official-suite evidence closure profile v2

Date: 2026-08-24

## Scope

This slice implements ADR-0026/D-050 as a deterministic, generated, non-Gate `contract-closure-profile/v2` candidate.
It preserves all four v1 profile artifacts byte-for-byte and adds one candidate classification only:
`json-schema-2020-12-official-test-suite` now means fixed-corpus and validator-authority evidence completeness.

The profile does not claim Ajv generic conformance. It binds the reviewed Ajv audit at
`EXECUTED_NONCONFORMANT`, `conformanceClaim=false`, 1,241 of 1,299 passed, and 58 non-passing assertions. Those 58 are
not passes, waivers, or expected failures. The independent jsonschema-rs oracle remains 1,299/1,299, and current
schemas/fixtures retain exact cross-engine result parity.

## Generated contract

- editable source: `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v2.json`
- strict source schema: `contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v2.schema.json`
- strict generated schema: `contracts/platform/v1alpha1/schemas/contract-closure-profile-v2.schema.json`
- generated registry: `contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json`
- predecessor: immutable `contract-closure-profile/v1`, `predecessorMutation=forbidden`

The generator checks the four frozen v1 SHA-256 values before v2 check/write, preserves explicit v1 build/current
semantics, and exposes v2 as the active/default profile. The active missing list is derived from v2 criterion statuses;
manual removal is forbidden.

The exact v2 missing list remains:

1. `runtime-server-path-and-tenant-authority-enforcement`;
2. `remaining-generator-supply-chain-review`.

## Generation-lock boundary

The contract-generation lock binds both v1 and v2 generated outputs and records:

- independent oracle: 46 files, 383 cases, 1,299 assertions, 79 remotes, zero failures;
- Ajv audit: `EXECUTED_NONCONFORMANT`, false conformance claim, 1,241 passed, 58 non-passing;
- current parity: 58 schemas, two manifests, 77 fixture cases, exact cross-engine results;
- all four predecessor hashes and both exact missing criteria; and
- `notGateClosure=true` with every Gate still open.

## Focused verification

The focused suite covers deterministic/current v1 and v2 generation, predecessor mutation, Ajv conformant/claim-true
rejection, changed review/oracle/parity facts, manual missing removal, and stale generated v2 bytes. Normal Ajv audit
replay must succeed exactly; `--require-conformance` must still fail nonzero.

No HTTP/P2/provider surface, production database write, deployment, publication, release, main-branch merge, or Gate
closure is included.
