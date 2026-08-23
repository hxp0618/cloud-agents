# Runner ledger recovery result service matrix — 2026-08-23

- Status: `SLICE_G_FIXED_IMPLEMENTATION_PENDING_INDEPENDENT_REVIEW`
- Approved Slice F candidate: `e1cb598c0c950e6f0ce34ae38e629e5ec4c5438f`
- Slice F independent review: `39d5d758eab82f09d2d27593a7fdc2994b49a419` — `APPROVE, P0=0/P1=0/P2=0`
- Slice G code commit: `1d58d43e4fe0551f3aabaeebfccbcd04835ca26f`
- Slice G code tree: `10195a06066f95ea1faf00407a1d9a3286629436`
- Slice G control-plane subtree: `c1d678f708ec231b446a11e46572a11fccefc97c`
- Slice G branch: `codex/cloud-agents-p1-runner-recovery-result-20260823`
- Decision: [`D-047 / ADR-0023`](runner-ledger-recovery-contract-decision-20260822.md)
- Gate effect: none; every immutable and aggregate Gate remains open

This record covers only ADR-0023 Slice G. It connects the already independently approved recovery families to the
generated consumer loop, adds the ordinary typed terminal/divergent failure result, and closes the 17-row consumer
matrix. It does not add a new profile, change any generated registry, authorize production database writes, create an
HTTP/P2/provider surface, deploy, publish, release, merge main, or close a Gate.

## Closed caller state machine

Each loop iteration mints and consumes a fresh same-verifier preflight claim. A generated recovery admission may then
produce only one of these ordinary outcomes:

1. abort-terminal append, pending reconciliation, or retry handoff returns `reenter` with the unchanged durable ledger
   prefix;
2. committed reconciliation or one recovery execution returns an ordinary committed-entry fact bound to the exact
   migration, ledger head, ledger length, and generated entry order; or
3. terminal/divergent `return_failure` closes without mutation and returns the exact stable typed error.

No outcome is a permit. Every successful writer/handoff result retires its exact consumed recovery-admission use record
before it can re-enter. A committed outcome, including the final entry, must pass through one more fresh preflight; only
the complete-ledger no-op may return public success. This preserves separately reviewed writer authority and prevents
ordinary result reuse as a second transition.

The loop is bounded from the verified runtime rather than a caller counter:

```text
max_iterations = entry_count * (3 * ExecutionPolicy.MaxAttempts + 2) + 1
```

All arithmetic is checked and inclusive of the final complete-ledger reread. Missing/invalid policy, empty order, or
overflow returns `MIGRATION_EVIDENCE_RECOVERY_REQUIRED` before entering the loop.

## Typed failure result

`runnerLedgerReturnFailureResult` is package-private ordinary data. Its domain-separated canonical digest binds the
exact:

- terminal or divergent state, migration, attempt, stable code, redacted failure evidence, terminal outcome/digest,
  and terminal record digest;
- optional adjacent resolution outcome/digest/record digest with an explicit presence bit;
- execution-lineage, journal, runner-decision, schema-bundle, recovery, recovery-tail, consumer-fact, evidence-boundary,
  and permit identities; and
- ledger prefix/head/length, catalog, and verified runtime inputs.

The builder rereads the current candidate, active generation, journal, cursor, and recovery snapshot after admission.
Terminal failure accepts only `aborted_terminal`, `ambiguous_reconciled_pending`, or an exact adjacent
`resolved_pending`; divergent failure accepts only `ambiguous_divergent` or exact adjacent `resolved_divergent`.
Stored drift fails closed. Context/session/lock/close uncertainty dominates the typed result. The public error uses a
fixed message plus the durable stable error tuple and never returns raw database or fixture text.

The result contains no session, journal, registry, permit, retry, handoff, append, SQL, successor-generation, or other
mutation capability. Production-graph tests reject forbidden writer and external-side-effect edges in both the result
path and consumer orchestration.

## Complete matrix and focused conformance

The generated 17-row consumer matrix covers one complete no-op row, five entry-disposition rows, and eleven
recovery-disposition rows. The generated recovery selector still closes exactly twelve recovery pairs across those
entry/recovery dispositions. Focused fixtures exercise all four abort pairs, both reconciliation pairs, retry handoff,
all three inherited recovery-execution pairs, and both typed return-failure pairs.

The fixed code commit was checked with Node `24.13.1`, Bun `1.3.14`, and Go/gofmt `1.26.6 darwin/arm64`:

- final named Slice G normal suite, including the complete generated matrix, typed failure, canonical/drift,
  fresh-reentry, bounded-loop, public-loop, and production-graph tests: PASS in `94.884s`;
- risk-narrow race suite limited to recovery-admission use retirement and committed-reconciliation snapshot handoff:
  PASS in `67.819s`;
- `go vet ./internal/migration` and `go build ./internal/migration`: PASS;
- recovery registry generator and recovery Go profile generator: current;
- generation-lock generator/checker: current;
- changed Go files: `gofmt` clean; `git diff --check`: PASS.

The generation lock SHA-256 is
`eb5df821dd7723fcead07075333beb16237d5b7b40f03561d692cc8d35e57fdf`. Its only change from Slice F is the recovery
Go profile-suite input-manifest SHA-256,
`sha256:417c736efe2c92c86b9d6481d3984f0ab8ca58c2c2eb3c30ff50c3bd9483d37a`, because that suite binds the updated
production-graph test. Generated registries, generated Go profile bytes, the exact pair mapping, and historical v1
artifacts remain unchanged.

Key implementation SHA-256 values:

- consumer loop: `b51784386957a46994c5fa52167db75381f8f0c630f9903a7e600cbe8931fbd0`;
- recovery admission consumer: `4864e6a601860d39cf24b101075df76f5e68eee5cd625a21a1a306355969d9e1`;
- ordinary typed failure result: `b68db38c6068493aec9e2cd4c5f49615b9f009d9769c551906a053c0c56c76a9`;
- typed result/fresh-reentry tests: `d374a714fbe0157a68aa660b1d299657548b59bdf030a1203fa8af2fb2cce97d`;
- complete consumer matrix tests: `7c96650bccc51308e366db40250cc0143f30a7b1977ff16cfe9edc3f930c63c5`;
- recovery profile/production graph tests: `07aa249089e00f851ff7cd137a8e1fd0f779bbdf2f23a0df2984731fe3fc2136`;
- generation lock: `eb5df821dd7723fcead07075333beb16237d5b7b40f03561d692cc8d35e57fdf`.

No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, HTTP/P2/provider,
deployment, publication, release, or Gate check is claimed. Database and evidence behavior in the focused path use
in-process fixtures. This record does not claim production authority invocation or a live power-loss run.

## Independent review boundary

Slice G remains incomplete until a fixed candidate containing this record receives an independent read-only P0/P1/P2
verdict. An `APPROVE` verdict closes only ADR-0023's ordered local implementation/review boundary. It cannot merge the
candidate, authorize external side effects, or close any immutable or aggregate Gate.
