# Runner ledger recovery execution service matrix — 2026-08-23

- Status: `SLICE_F_FIXED_IMPLEMENTATION_PENDING_INDEPENDENT_REVIEW`
- Approved Slice E candidate: `f86e8ca698df8cb5cbedd3a5b8daf2854c342c27`
- Slice E independent review: `48ba3cc2402857233b35131201b39a2fd4f469d1` — `APPROVE, P0=0/P1=0/P2=0`
- Slice F code commit: `f95a2201cf23bfae4c72203adca0f10bf5ef31ab`
- Slice F code tree: `6e4fb03b635022d7a72e6578bd0aa48ce1a9fd2b`
- Slice F control-plane subtree: `4478aa6302144ea41aba95dd5df7eb9375e42213`
- Slice F branch: `codex/cloud-agents-p1-runner-recovery-execution-20260823`
- Decision: [`D-047 / ADR-0023`](runner-ledger-recovery-contract-decision-20260822.md)
- Gate effect: none; every immutable and aggregate Gate remains open

This record covers only ADR-0023 Slice F. It connects the generated
`runner-ledger-recovery-execution-admission/v1` profile to the separately versioned
`runner-ledger-recovery-success-writer/v1` profile for one inherited entry attempt. It does not add the Slice G typed
public success/failure result or caller matrix. It does not authorize a production database write, HTTP/P2/provider
behavior, deployment, publication, release, main merge, or Gate closure.

## Closed pair and identity mapping

Exactly three generated recovery pairs enter the execution-admission profile:

| Preflight disposition       | Recovery state        | Recovery action       | Attempt boundary                                           |
| --------------------------- | --------------------- | --------------------- | ---------------------------------------------------------- |
| `empty_brand_new`           | `brand_new_inherited` | `begin_next_attempt`  | same first entry, attempt `N+1`, previous terminal exact   |
| `partial_retry_or_recovery` | `brand_new_inherited` | `begin_first_attempt` | next entry, attempt 1, no previous terminal                |
| `partial_retry_or_recovery` | `brand_new_inherited` | `begin_next_attempt`  | same current entry, attempt `N+1`, previous terminal exact |

The execution-admission profile is profile 5 and has those three direct pairs. The success-writer is profile 6, has
zero direct consumer pairs, and accepts only a consumed, registry-backed profile-5 permit. Entry-v1 and recovery-v1
use distinct profile/registry identities, evidence request types, binders, state digest domains, cleanup digest
domains, permit canonicals, and transition registries. Neither writer can consume the other's permit or transition.

## One inherited attempt

For one fresh current-generation evidence session, the package-private path performs this sequence:

1. consume the exact profile-5 recovery execution-admission permit through the reviewed dual-registry cleanup
   provenance; literals, copies, foreign binders, changed records, and second use fail closed;
2. reread the verified runtime, selected entry, statement-plan closure, execution policy, locked ledger prefix,
   catalog predecessor, current candidate, current inherited generation, journal, cursor, recovery snapshot, and
   lineage continuation;
3. accept only a header-only `brand_new_inherited` boundary: attempt 1 with no continuation for
   `begin_first_attempt`, or attempt `N >= 2` with exact migration, previous/source terminal, and continuation for
   `begin_next_attempt`;
4. begin one migration transaction on the retained locked session, then preserve the inherited attempt index and
   previous terminal across every statement intent, intermediate, commit intent, and committed terminal;
5. execute the full verified multi-statement plan with the existing projection/catalog checks and dynamic durable
   cursor, including checkpoint and rotation handling;
6. insert exactly one ledger row and call database commit exactly once; and
7. after known commit, append exactly one committed terminal and classify the next-entry or complete boundary without
   converting an ordinary result into recovery authority.

Every evidence append is minted through the distinct recovery request and concrete recovery binder. The shared
state-machine mechanics remain ordinary implementation code, but the generated transition registry and all authority
seams select the writer kind explicitly. The immutable entry-v1 request, profile, domains, and first-attempt behavior
remain closed and are covered by unchanged known-success tests.

## Handoff, failure, and reopen closure

- The integrated handoff test starts from a real durable `aborted_retryable` terminal, consumes Slice E to activate a
  header-only attempt-2 successor, revokes the old cursor, opens a fresh evidence-session wrapper, and then consumes
  Slice F. The resulting terminal retains attempt 2 and the exact predecessor terminal.
- A successful recovery execution currently returns through the existing public consumer as
  `MIGRATION_PROJECTION_NOT_IMPLEMENTED`, because Slice G is not open. The mutation is not retried through the same
  one-shot fact. A later fresh re-entry observes the completed durable journal and follows the existing complete-ledger
  no-op path.
- Before any mutation, context, input, ledger, catalog, session, permit, binder, or cursor contradiction closes the
  retained database resources and mints no writer successor.
- After append or commit may have happened, an unknown append outcome, ambiguous/rejected commit, close uncertainty,
  returned cursor/snapshot contradiction, or terminal append uncertainty returns
  `MIGRATION_EVIDENCE_RECOVERY_REQUIRED` and revokes the old cursor. It never reports ordinary success/failure.
- The exact three pairs support statement counts greater than one and physical segment rotation. Retry attempt and
  `PreviousAttemptTerminalDigest` remain dynamic through intent, intermediate, commit, terminal, snapshot, and state
  canonicalization.

## Focused conformance

The fixed code commit was checked with Node `24.13.1`, Bun `1.3.14`, and Go/gofmt `1.26.6 darwin/arm64`:

- exact Slice F/profile/entry-v1 focused normal suite: PASS in `41.244s`;
- exact authentic Slice E handoff → fresh re-entry → Slice F test: PASS in `7.188s` before the final aggregate focused
  run, which also included it;
- after adding explicit recovery-v1 commit-rejected and post-commit-close cases, the modified unknown/post-commit table
  passed in `12.872s`;
- tiny race suite limited to profile-5 permit and profile-6 request copy/literal/one-shot behavior: PASS in `26.147s`;
- `go vet ./internal/migration` and `go build ./internal/migration`: PASS;
- recovery Go generator and generation-lock checker under the exact toolchain tuple: current;
- changed Go files: `gofmt` clean; `git diff --check`: PASS.

One exploratory command covering a much larger pre-existing post-commit tamper matrix reached its explicit `3m`
bound without a failed assertion: **NOT PASS**. It was not rerun and is not used as Slice F evidence.

The generation lock SHA-256 is
`e7dd9feaa3b971f41d09c612f9d5d016107ead6d51ab4bdf442feef5377dc777`. Its only change from Slice E is the recovery
Go profile-suite input-manifest SHA-256,
`sha256:3fa2341f8f9ce5e1cb307cc09aa079349a206b41590c5676c82aff2d453fe045`, because the suite now binds the Slice F
production graph and profile tests. Generated recovery registries, generated Go profile bytes, the 12-pair mapping,
and historical entry-v1 artifacts remain unchanged.

Key implementation SHA-256 values:

- recovery evidence request/binder contract: `761437a41c423b656afe6e1e07f50481d8f81a3b52043f6a408ab89fae3eb051`;
- shared explicit-kind success kernel: `012fa0fc1e390b7997bb3e3e920089f1cc6862d3ebe264a61c1b2eb2fb736be7`;
- recovery success kernel: `0960578d96033cdc0e110f8737fe77c495e7e8a7596c20c776637eade04e893d`;
- recovery success matrix tests: `f13def8cc315df56a2324e126ebb40f18016cc8e387467ab5cabcf162cf01e4d`;
- recovery profile/production graph tests: `c93899783a9667c4d341b38a55bd6457de241e2b33145e454463aaf089e39dde`;
- generation lock: `e7dd9feaa3b971f41d09c612f9d5d016107ead6d51ab4bdf442feef5377dc777`.

No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, HTTP/P2/provider,
deployment, publication, release, or Gate check is claimed. Database and evidence behavior in the focused path use
in-process fixtures. This record does not claim production authority invocation or a live power-loss run.

## Independent review boundary

Slice F remains incomplete until a fixed candidate containing this record receives an independent read-only P0/P1/P2
verdict. Slice G must not begin on a `BLOCK` verdict. An `APPROVE` verdict closes only Slice F's local implementation
and review boundary; it cannot authorize production side effects or close any Gate.
