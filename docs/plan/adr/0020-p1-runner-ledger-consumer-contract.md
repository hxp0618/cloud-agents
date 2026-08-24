# ADR-0020: P1 runner ledger consumer versioned contract

- Status: Accepted by owner approval on 2026-08-21
- Scope: generated consumer profile, complete-ledger `return_success` no-op, matrix, and independent review
- Depends on: ADR-0019 and the runner ledger consumer/writer entry blocker

## Decision

The next runner ledger slice adds a distinct generated `runner-ledger-consumer/v1` registry. The existing
`runner-ledger-preflight/v1` registry and its generated Go profile remain byte-identical and continue to classify
the same 17 exact disposition/recovery pairs. The consumer registry binds that immutable generated identity and
maps the 17 pairs to exactly three closed consumer actions:

- the single `complete_return_success` / `completed` / `return_success` pair maps to
  `return_success_noop`;
- the five empty or next-entry pairs map to `entry_not_implemented`; and
- the eleven retry, recovery, reconcile, or failure pairs map to `recovery_not_implemented`.

Unknown, caller-selected, copied, cross-profile, or unbound inputs never select the no-op action. They fail closed
without a second transition.

## Identity and authority boundary

The generated consumer profile binds:

1. the exact preflight registry, profile, state-machine, and policy digests;
2. the exact dispatch and preflight-fact subject digests produced by one consumed same-verifier claim;
3. the current candidate, generation, journal, runner-projection, and recovery identities already carried by that
   dispatch;
4. the complete ordered migration-prefix length, head, and domain-separated digest; and
5. the verified schema-bundle and manifest digests plus the exact empty-`Applied` no-op result.

The pure generated fact is ordinary, copyable data and owns no capability. The production no-op service may only
obtain its input by consuming one exact `runnerLedgerPreflightClaim` inside the same closed call. No production API
accepts a caller-provided ordinary dispatch as authority. The existing brand-new writer remains a separate,
independently revalidated authority chain and cannot consume the new ordinary fact.

## Ordered slices

### Slice A - generated profile only

Generate the registry and package-private Go profile/fact. There is no `Runner.Run` caller, database handle,
transaction, evidence mutation, writer token, or external surface in this slice. A v1 same-bits test mechanically
proves the preflight generated output did not change.

### Slice B - complete-ledger no-op only

After the existing writer preflight reports a non-empty or complete ledger, or after the current single-entry scope
rejects a wider verified bundle, the runner may invoke the already reviewed read-only ledger/catalog preflight and
consume its same-verifier claim. Only `return_success_noop` returns success. The result contains the verified
schema-bundle digest, manifest digest, final ledger head, and empty `Applied` and `AmbiguousRecovered` collections.
It performs no `BeginMigration`, SQL, ledger/evidence append, migration transaction, or commit.

Every entry or recovery action remains `MIGRATION_PROJECTION_NOT_IMPLEMENTED`. The ordinary dispatch never routes
or authorizes the existing brand-new writer. The existing writer continues only through its prior empty-ledger,
brand-new, single-entry/single-statement authority chain.

### Slice C - matrix and independent review

The fixed candidate must cover all 17 generated pairs, zero/copy/literal/cross-profile/second-consume faults,
complete result bindings, context and cleanup precedence, and forbidden call edges. An independent reviewer must
return a P0/P1/P2 verdict before the canonical status record is advanced.

## Explicit non-claims

This decision does not implement entry execution, retry, recovery, reconciliation, a fresh execution-session
permit, SQL, ledger or evidence mutation, HTTP/P2/provider surfaces, production database writes, deployment,
publication, release, or any Gate closure. Every such transition remains separately authorized future work.
