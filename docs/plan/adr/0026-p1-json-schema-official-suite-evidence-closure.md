# ADR-0026: P1 JSON Schema official-suite evidence closure

- Status: Accepted under the standing P1 execution approval on 2026-08-24
- Decision ID: D-050
- Scope: G-CONTRACT R5-B2 generated closure-profile v2 evidence classification
- Depends on: ADR-0007, G-CONTRACT R4, R5-A, and the R5-B1 Ajv audit independent review
- Decision owner: hxp0618
- Implementation executor: Codex
- Gate effect: none

## Context

The fixed Draft 2020-12 corpus has 46 mandatory files, 383 cases, 1,299 assertions, and 79 remote schemas. The
independent `jsonschema-rs 0.50.1` oracle passes all 1,299 assertions. The separately reviewed production-build-time
Ajv 8.20.0 audit executes the same corpus but is honestly `EXECUTED_NONCONFORMANT`: 1,241 assertions pass and 58 do
not pass. Its `conformanceClaim` is false, ordinary exact replay succeeds, and `--require-conformance` exits nonzero.

The v1 closure profile predates that audit and must remain byte-immutable. A new profile identity is required to
classify the current evidence without rewriting v1 or turning Ajv's result into a pass or waiver.

## Decision

`contract-closure-profile/v2` marks only `json-schema-2020-12-official-test-suite` as
`SATISFIED_CANDIDATE`. For this criterion, satisfaction means that the fixed corpus and validator-authority evidence
is complete. It does not mean that every participating validator conforms to the full corpus.

The candidate classification requires all of these facts together:

1. `jsonschema-rs 0.50.1` replays exactly 46 files, 383 cases, 1,299 assertions, and 79 remotes with zero failures;
2. Ajv 8.20.0 is independently reviewed at exactly `EXECUTED_NONCONFORMANT`, 1,241/1,299 passed and 58 non-passing,
   with `conformanceClaim=false`, byte-exact ordinary replay, and a nonzero conformance-required command;
3. the current digest-bound contract set has exact Ajv/in-repo and jsonschema-rs fixture-result parity; and
4. the R5-B1 fixed-candidate independent review is `APPROVE, P0=0/P1=0/P2=0` and is bound by its raw SHA-256.

Ajv's 58 non-passing assertions are neither passes, waivers, accepted failures, expected failures, nor an Ajv generic
conformance claim. Future changes to the fixed corpus, oracle, Ajv result, evidence meaning, or predecessor require a
new profile version.

The v2 source and generated registry bind the complete evidence tuple and the four immutable v1 artifact hashes.
Generation must assert those v1 hashes before v2 write/check. `predecessorMutation=forbidden`; v1 bytes and semantics
remain unchanged.

The exact v2 satisfied-candidate inventory is the prior four criteria plus this official-suite evidence criterion.
The exact remaining missing inventory is:

1. `runtime-server-path-and-tenant-authority-enforcement`;
2. `remaining-generator-supply-chain-review`.

The criteria in `05-gates-and-acceptance.md` are unchanged. This decision is an evidence-classification candidate,
not a Gate transition.

## Verification boundary

The v2 generator and generation lock bind both profile versions, source/output schemas, fixed corpus/profile, Ajv
audit source/runner/tests/output, independent Python checker/tests, current cross-engine parity, and fixed reviews.
Focused negative checks fail closed on predecessor mutation, an Ajv conformance claim, changed oracle totals, false
parity, changed review digest, handwritten missing removal, or stale generated v2 bytes.

## Explicit non-claims

- Ajv 8.20.0 is not claimed to conform generically to JSON Schema Draft 2020-12.
- The 58 non-passing assertions are not waived, hidden, or renamed as expected failures.
- No runtime/server or tenant-authority path is implemented by this decision.
- No remaining generator supply-chain review is satisfied by this decision.
- No HTTP/P2/provider effect, production database write, deployment, publication, release, merge, or Gate closure is
  authorized or performed.
