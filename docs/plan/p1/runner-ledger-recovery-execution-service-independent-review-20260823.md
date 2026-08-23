# Runner ledger recovery execution service independent review — 2026-08-23

- Verdict: `APPROVE`
- Severity: `P0=0 / P1=0 / P2=0`
- Fixed candidate: `e1cb598c0c950e6f0ce34ae38e629e5ec4c5438f`
- Candidate tree: `eb9d21632c3e47a904e49fb023f8b7486e217c5c`
- Candidate control-plane subtree: `4478aa6302144ea41aba95dd5df7eb9375e42213`
- Code commit: `f95a2201cf23bfae4c72203adca0f10bf5ef31ab`
- Approved Slice E base: `f86e8ca698df8cb5cbedd3a5b8daf2854c342c27`
- Slice E independent review: `48ba3cc2402857233b35131201b39a2fd4f469d1`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-execution-20260823`
- Candidate matrix: [`runner-ledger-recovery-execution-service-matrix-20260823.md`](runner-ledger-recovery-execution-service-matrix-20260823.md)
- Candidate matrix SHA-256: `0343a1aa889662ca7cdf2d4b4d6a9d76b9a62cfcd705787e5875bbfd3d6d9a25`

This is an independent, read-only review of ADR-0023/D-047 Slice F. Approval applies only to the fixed candidate above
and does not authorize a merge, production database access or writes, HTTP/P2/provider behavior, deployment,
publication, release, Slice G implementation, or Gate closure.

## Fixed identity and scope

The candidate branch was clean, matched its remote branch exactly, and was `0/0` relative to its upstream at review
time. HEAD, tree, control-plane subtree, matrix hash, generation-lock hash, and reviewed implementation hashes matched
the supplied fixed identity. The diff from the approved Slice E candidate contains exactly 13 paths and does not
change any generated registry, schema, fixture, generated Go profile, SDK, or historical entry-v1 artifact.

Verified candidate hashes include:

- recovery evidence request/binder: `761437a41c423b656afe6e1e07f50481d8f81a3b52043f6a408ab89fae3eb051`;
- shared explicit-kind success kernel: `012fa0fc1e390b7997bb3e3e920089f1cc6862d3ebe264a61c1b2eb2fb736be7`;
- recovery success kernel: `0960578d96033cdc0e110f8737fe77c495e7e8a7596c20c776637eade04e893d`;
- recovery success tests: `f13def8cc315df56a2324e126ebb40f18016cc8e387467ab5cabcf162cf01e4d`;
- recovery profile/production-graph tests: `c93899783a9667c4d341b38a55bd6457de241e2b33145e454463aaf089e39dde`;
- generation lock: `e7dd9feaa3b971f41d09c612f9d5d016107ead6d51ab4bdf442feef5377dc777`.

The generation-lock diff changes only the recovery Go profile-suite input-manifest SHA-256 to
`sha256:3fa2341f8f9ce5e1cb307cc09aa079349a206b41590c5676c82aff2d453fe045`. Profile 5 has exactly the three generated
`brand_new_inherited` execution pairs, while profile 6 has zero direct consumer pairs and binds profile 5 as its
exact predecessor and permit source. Historical entry-v1 generated artifacts remain byte-identical.

## Identity and authority verdict

The review confirmed the following boundaries:

- recovery execution-admission and recovery success-writer retain distinct registry/profile/state-machine/policy
  digests, predecessor/permit identities, state and cleanup digest domains, evidence request types, binder methods,
  use records, and transition selection;
- profile 5 has one production consumer, in recovery-admission routing; profile 6 can be entered only after the exact
  profile-5 permit is claimed, and its evidence request has one production minter and one concrete binder call;
- copies, literals, foreign binders, registry drift, and second consumption cannot use or close the original authority;
  the hardened dual-registry cleanup path retains the original locked session for deterministic rollback/unlock/close;
- the common success mechanics expose only a read-only evidence interface. Every mutation seam switches on an
  explicit writer kind, so entry-v1 cannot consume recovery transitions or recovery requests, and recovery cannot
  consume entry-v1 requests or transitions;
- the current candidate, generation, journal, composite cursor, recovery digest/tail, runtime, execution policy,
  selected entry, statement-plan closure, ledger prefix, catalog predecessor, and retained database identity are
  reread before the recovery writer state is sealed.

## Attempt, mutation, and recovery verdict

The three admitted mappings preserve their exact attempt boundaries:

1. empty inherited retry starts the same first entry at attempt `N+1` with the exact predecessor terminal;
2. partial inherited first-attempt starts the next entry at attempt 1 without predecessor terminal;
3. partial inherited retry starts the current entry at attempt `N+1` with the exact predecessor terminal.

The dynamic attempt and optional previous-terminal digest are bound through statement intent, each intermediate,
commit intent, committed terminal, evidence request, state digest, snapshot, and result validation. Multi-statement
ordering and dynamic cursor/rotation remain in the reviewed shared kernel. It begins one transaction, executes every
selected signed statement once in order, inserts and reads back one ledger row, invokes the commit protocol once, and
appends one committed terminal only after a known commit and proven old-session close.

Unknown evidence append, rejected or ambiguous commit, post-commit close uncertainty, terminal append uncertainty,
or returned state/cursor contradiction returns `MIGRATION_EVIDENCE_RECOVERY_REQUIRED` and revokes the shared cursor.
No ordinary result becomes recovery authority. A successful execution is deliberately discarded by the current
public recovery caller, which still returns `MIGRATION_PROJECTION_NOT_IMPLEMENTED` until Slice G; a later fresh
preflight observes the durable journal rather than reusing the consumed permit.

The Slice E -> Slice F integration path was also reviewed: the old retry cursor is revoked by handoff, the successor
is header-only and current, re-entry uses a fresh wrapper/claim graph, and attempt/previous-terminal provenance is
preserved. No Slice F file adds HTTP, P2, provider, deployment, release, or Gate authority.

## Checks and evidence boundary

Fresh independent checks on the fixed candidate:

- HEAD/tree/control-plane subtree, matrix and lock SHA-256, clean worktree, upstream `0/0`, and remote exact branch:
  PASS;
- static review of generated pair counts, profile predecessor/permit binding, production callers, request minters,
  binder edges, explicit writer-kind transitions, DB/commit call counts, public result boundary, and forbidden imports:
  PASS;
- exact Go 1.26.6 tests
  `TestRunnerLedgerRecoverySuccessProductionGraphHasOnlyTypedAdmissionAndWriterEdges` and
  `TestRunnerLedgerRecoverySuccessUnknownAndPostCommitBoundariesRequireRecovery`: PASS in `13.178s`;
- changed-file `gofmt -d` and candidate-range `git diff --check`: PASS;
- candidate-range Gitleaks 8.30.1 evidence supplied for two commits and approximately `78.90 KB`: PASS, no leaks.

The review relied on the candidate record for the wider bounded evidence: the exact Slice F/profile/entry-v1 normal
suite passed in `41.244s`, the authentic handoff/re-entry case passed in `7.188s`, the final unknown/post-commit table
passed in `12.872s`, the tiny permit/request race suite passed in `26.147s`, and generator/current, vet, and build were
recorded as PASS. Those wider commands were not independently repeated.

The exploratory three-minute command remains **NOT PASS** because it reached its explicit timeout and is not evidence.
No full `internal/migration`, full shard run, broad race, live PostgreSQL, production database, live power-loss run,
deployment, publication, release, or Gate check was run or claimed.

## Final verdict

`APPROVE, P0=0/P1=0/P2=0` for fixed candidate
`e1cb598c0c950e6f0ce34ae38e629e5ec4c5438f` only. Slice F's local implementation/review boundary may proceed under
the existing approval, but Slice G, every external-side-effect boundary, and every Gate remain open and unauthorized.
