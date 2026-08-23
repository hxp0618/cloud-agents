# G-CONTRACT R5-A formal closure profile implementation - 2026-08-24

- Status: **IMPLEMENTED CANDIDATE - INDEPENDENT REVIEW PENDING**
- Baseline: `9b4f19ce8ac96796c669ced37aa471a93b77ef1a`
- Scope: generated `contract-closure-profile/v1` and generation-lock derivation only
- Gate effect: none; `G-CONTRACT` remains `IN PROGRESS` and every Gate remains OPEN

## Generated authority

The editable authority is the strict Draft 2020-12 source schema plus the checked-in golden source at
`contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v1.json`. The deterministic generator
validates the exact closed seven-criterion v1 inventory, the allowed status enum, repository-contained regular
authority/evidence paths, independent-review bytes and review SHA-256 before producing
`contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json`.

The source, profile and registry use independent NUL-framed domain-separated SHA-256 digests. The generated output is
recorded as a generation-lock output rather than an input to its own manifest. The lock pipeline binds the source,
schemas, generator, tests, this record and every declared authority/evidence/review input; this avoids a circular
self-hash while still binding the generated output bytes and digest.

`scripts/lib/platform-contracts.ts` no longer owns a hand-written seven-item `missing` array. It validates the v1
source and derives `missing` from every criterion whose status is not `SATISFIED_CANDIDATE`. Manual deletion is
forbidden, and v1 validation rejects satisfying any criterion outside the approved four.

## Exact v1 classification

The following bounded contract candidates are `SATISFIED_CANDIDATE` and therefore absent from generated
`lock.missing`:

1. `openapi-3.1-semantic-validation`;
2. `generated-sdk-replay`;
3. `n-minus-one-compatibility`, limited to the reviewed contract-layer readers and fixtures; and
4. `response-watch-unknown-field-preservation`, limited to the reviewed generated JSON/Proto seams.

Each item binds existing authority/evidence paths and an exact `APPROVE, P0=0/P1=0/P2=0` review record digest. This
classification does not claim deployed rolling compatibility, production routes, runtime trust or a Gate closure.

The exact generated `missing` list remains:

1. `json-schema-2020-12-official-test-suite` because the production Ajv official-suite runner remains
   `NOT_RUN_NOT_CLAIMED`;
2. `runtime-server-path-and-tenant-authority-enforcement` because production trust provisioning and HTTP/server-path
   enforcement remain unimplemented; and
3. `remaining-generator-supply-chain-review` because current vulnerability, all-platform executable/wheel and
   immutable supply signatures remain absent.

## Verification and non-claims

Focused validation covers deterministic generation/current checks, exact status derivation, forbidden v1 status
expansion, review digest drift, evidence-to-pipeline input closure, the generation-lock pipeline, and the existing
identity/JSON/Proto generator tests. No database or broad migration test is required by this contract-only slice.

This slice does not implement an Ajv official-suite runner, a supply scanner, production trust, HTTP/P2/provider
surfaces, production database writes, deployment, publication or release. `status` remains `BOOTSTRAP_VALIDATED`,
`notGateClosure=true`, and all Gate statuses remain unchanged and OPEN.
