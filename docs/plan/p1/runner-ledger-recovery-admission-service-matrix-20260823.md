# Runner ledger recovery admission service matrix — 2026-08-23

- Status: `SLICE_B_FIXED_IMPLEMENTATION_AND_INDEPENDENT_REVIEW_APPROVED`
- Approved Slice A candidate: `67210b7f194ec1591c06957ddfc86920a58af167`
- Slice A independent review: `88f1ecc165866fa2e7da6af9141a4638c1aecdf4` — `APPROVE, P0=0/P1=0/P2=0`
- Slice A review record SHA-256: `127e1bf451f4e21186f611325a0f1d7a57df195c5954501dc3b6eb7ca344afa6`
- Slice B code commit: `b7a996274f61fa9972ccd1e00bd8c83985faafbb`
- Slice B code tree: `f4eb3aa69055ead8e00942221f8deea2de0cadc2`
- Slice B control-plane subtree: `556750fa193ebf788f7e8065b15f7357506a6d36`
- Slice B branch: `codex/cloud-agents-p1-runner-recovery-admission-20260823`
- Fixed Slice B candidate: `23c3083b7d7b58089f2cb208b1381b2d510500ff`
- Fixed Slice B candidate tree: `75e50c3068682c16b60f1e287c8f96d97bab1af7`
- Independent review: `4808d20e1d36f5f0bb6efe557ccc6347e955bab0` — `APPROVE, P0=0/P1=0/P2=0`
- Independent review record:
  [`runner-ledger-recovery-admission-service-independent-review-20260823.md`](runner-ledger-recovery-admission-service-independent-review-20260823.md)
- Independent review record SHA-256: `0244a6ec4b14a3cb5de27629ee725a64b2bb28f85756a4f2d4dc73ccc6501334`
- Decision: [`D-047 / ADR-0023`](runner-ledger-recovery-contract-decision-20260822.md)
- Gate effect: none; every immutable and aggregate Gate remains open

This record covers only ADR-0023 Slice B. It does not implement an abort, reconciliation, resolution, handoff,
execution, success, or failure writer. It does not authorize production database writes, HTTP/P2/provider behavior,
deployment, publication, release, main merge, or Gate closure.

## Closed read-only sequence

For one exact generated consumer pair, the package-private service performs this sequence:

1. retain the current same-verifier lifecycle receipts and irrevocably close the old generation lease;
2. reacquire revision-zero full-root admission for the same lineage and journal;
3. replay all registered lineages, generations, journals, segments, and objects, using the one-shot live lifecycle
   witness only for exact retry-receipt or ambiguous-boundary checks that disk bytes cannot self-prove;
4. recover every historical generation through the same verifier, bind the exact registered generation handoff, and
   reinstall the same logical journal/session only when schema, recovery, cursor, state, and generation facts match;
5. seal a registry-backed claim over the consumer fact, candidate, generation, full-set digest, transcript digest,
   index boundary, recovery boundary, and concrete session/journal identities;
6. open a fresh dedicated database session, apply the signed role/settings, acquire the signed advisory lock, and
   project the exact ledger, catalog, and authority boundary;
7. select the exact signed migration entry, statement-plan closure, attempt, and max-attempt budget;
8. reread idle transaction state, role, lock, authority, ledger, and catalog, then consume the evidence claim only
   after a second full-root replay returns the same boundary; and
9. mint one action-specific, registry-backed, non-copyable permit whose only operational method is
   `close_without_mutation`, then release/reset/close the exact registered database session.

The intermediate live lifecycle witness and its consumed evidence session are separately registry-backed and
one-shot. Literals, copies, registry loss, chain mutation, a foreign generation, a second consume, or a disk-only
retry/ambiguous record cannot substitute for the retained live lifecycle authority.

## Exact generated mapping

The immutable common registry still contains exactly twelve pairs. Slice B maps them only to six close-only permit
types; the distinct recovery success-writer profile has no direct pair and is never selected.

| Close-only permit family     | Pair count | Slice B result                                                       |
| ---------------------------- | ---------: | -------------------------------------------------------------------- |
| abort terminal               |          4 | exact admission, close without mutation, public result remains NI    |
| commit observation terminal  |          1 | exact admission, close without mutation, public result remains NI    |
| ambiguous resolution         |          1 | exact admission, close without mutation, public result remains NI    |
| retry lineage handoff        |          1 | exact admission, close without mutation, public result remains NI    |
| recovery execution admission |          3 | exact admission, close without mutation; success writer not selected |
| typed return failure         |          2 | exact admission, close without mutation, public result remains NI    |
| recovery success writer      |          0 | no direct consumer pair; `NOT_IMPLEMENTED`                           |

After successful close, the existing consumer still returns the stable
`MIGRATION_PROJECTION_NOT_IMPLEMENTED` entry/recovery operation. Stored corruption and context/filesystem/session/
lock/projection/cleanup uncertainty continue to take precedence over that result.

## Authority and cleanup invariants

- Claim, use record, evidence boundary, and permit bind all eight generated recovery identities, the selected action,
  candidate owner/binding, generation, schema and recovery digests, full-root revision/full-set/transcript/index facts,
  database identity, role/lock, ledger/catalog/authority projections, exact entry, plan closure, attempt, and max budget.
- The six action-specific permit types expose no transaction, SQL, evidence append, handoff, writer, or result method.
  The success-writer selector has no production caller.
- Copy/literal/cross-profile/stale/foreign/registry-missing/second-use inputs fail closed. Drift of the live permit still
  consumes the trusted registry record and closes the exact retained session; cleanup uncertainty dominates.
- Evidence-session close revokes remaining recovery claims/use records. Once a fact enters admission, it cannot be
  reused as a second action.
- The concrete refresh path contains no `Runner.Run`, transaction, SQL, evidence append, reservation, successor
  activation, HTTP, P2, provider, deployment, or release edge.

## Focused conformance

The local bounded checks cover:

1. all twelve generated pairs and exact action/type counts `4/1/1/1/3/2`, with success-writer count zero;
2. disk-only retry/ambiguous recovery-required behavior, exact live receipt/boundary acceptance, and mismatch as
   evidence corruption;
3. witness/evidence/claim/permit literal, copy, registry, cross-profile, drift, second-use, and cleanup behavior;
4. full-root/session/transcript drift between claim and consume;
5. exact signed ledger/catalog/role/lock/session/entry/plan/max-attempt rereads;
6. pre-database and post-consume cancellation with exact session cleanup;
7. the existing public 1-entry retry plus eleven recovery rows still returning `NOT_IMPLEMENTED` with zero writer,
   transaction, SQL, ledger insert, commit, or evidence append calls; and
8. AST-enforced lifecycle consumers, generated-selector consumers, close-only production callers, and forbidden
   writer/external edges.

The following fixed-toolchain checks passed locally:

- focused recovery/lifecycle/consumer/production-graph normal tests: PASS;
- the same bounded risk scope under `-race`: PASS in `277.851s`;
- recovery registry plus contract-lock tests: `23/23` PASS with `191` assertions;
- recovery registry generator, recovery Go generator, and generation-lock writer/checker: current;
- contract bootstrap: `118` JSON files, `52` schemas, and `71` fixture cases validated; this is explicitly non-Gate;
- migration-package `vet`/`build`, module `tidy -diff`, and module verification: PASS;
- generated recovery profiles and all 24 historical runner v1 artifacts: same-bits;
- changed Go files: `gofmt` clean; `git diff --check`: PASS.

The generation lock SHA-256 is
`b62d1884c37d8e2856969fa14ef6440ff3635c7c6aeb411d3e55ad36fca59492`. Its only generated-lock change from
Slice A is the recovery Go profile pipeline input-manifest SHA-256, now
`sha256:7522340db809c6f6250b9a2e087e0c3a85218772d976b5ce43954d9077acae19`, because the profile test now admits only
the reviewed Slice B read-only consumers and rejects writer edges.

No full `internal/migration`, full shards, broad race, live PostgreSQL, production database, or external-side-effect
test was run or is claimed. The successful concrete `GenerationLease -> AdmissionInventory -> GenerationLease`
refresh cannot be constructed by an ordinary migration-package fixture without the trusted evidencefs provisioning
authority; this record therefore claims the production call graph and the independently tested opaque components,
not a fabricated end-to-end authority constructor.

## Independent review closure

The independent read-only reviewer re-resolved the fixed commit/tree/control-plane subtree, remote branch, clean state,
exact scope and bound hashes; inspected the same-verifier replay, full-root refresh, fresh locked session, claim/permit
one-shot and cleanup boundaries; reran the bounded normal/race, contract/current, static, same-bits and secret scopes;
and returned `APPROVE, P0=0/P1=0/P2=0`.

This verdict closes only ADR-0023 Slice B implementation/review and satisfies Slice C Entry. It does not implement or
authorize an abort/reconciliation/resolution/handoff/execution/failure writer, production database writes,
HTTP/P2/provider behavior, deployment, publication, release, main merge, or any Gate closure.
