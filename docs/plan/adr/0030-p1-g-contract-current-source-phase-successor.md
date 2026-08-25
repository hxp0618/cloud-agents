# ADR-0030: P1 G-CONTRACT current-source phase successor

- Status: Accepted under the continuing Platform goal execution authority on
  2026-08-25; Slice A entry requires an approving fixed-object design review
- Date: 2026-08-25
- Decision ID: D-053
- Depends on: ADR-0029/D-052,
  `g-contract-detached-review-binding-independent-review-20260824.md`, and
  `g-contract-post-h-current-source-successor-entry-audit-20260825.md`
- Decision owner: hxp0618
- Proposed implementation executor: Codex
- Gate effect: none; `G-CONTRACT`, `G-SUPPLY-CHAIN`, and every phase and
  aggregate Gate remain `IN PROGRESS`/`OPEN`

## Context

D-052 completed its ordered Slices A-H at fixed post-H commit
`16275f6cbf390c343a9ac00f9193e75eaad0094e`. Closure-profile v3,
generator-supply v2, the detached two-review binding registry, and the terminal
Slice H review are current and independently approved within their bounded
non-Gate scope.

That completion changes the source projection. `G-CONTRACT` R4 predates the
current generation lock and is historical under its own invalidation rule. The
status tracker also predates formal Slices C-H and cannot be reinterpreted as a
phase authority. D-052 has no Slice I and gives no permission to edit R4, the
v2 supply projection, the reviewed binding registry, or any predecessor bytes
in place.

The fixed generator-supply v2 projection excludes exactly 16 ordered paths.
ADR-0030 and every new phase-record byte are outside that set. Committing this
decision therefore makes v2 historical for current-source claims, while
preserving it as an immutable reviewed predecessor. A new versioned projection
and native replay are required.

The entry audit
`docs/plan/p1/g-contract-post-h-current-source-successor-entry-audit-20260825.md`
records the fixed identities, the self-reference analysis, and the exact
non-claims.

## Decision

Adopt a new versioned, acyclic post-H successor. It has three pre-replay
authorities and one versioned lock writer:

1. generator-supply profile v3;
2. a `G-CONTRACT-P1`-specific generated phase-record authority;
3. a detached phase-record review-binding authority;
4. generation-lock v3 assembled and phase-bound states.

The R5 semantic source, schemas, typed builder, deterministic Markdown
renderer, checker, binding source/schemas, and all focused tests are included
in the supply-v3 projection. The actual R5 record is generated only after the
supply-v3 profile and its independent review are fixed. R5 is then reviewed as
an immutable candidate, after which a detached tuple/registry binds the supply
and R5 review lineages. A final independent review terminates the chain. No
tracked output consumes that final review.

The highest derived state authorized by D-053 is
`REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE`. It is not `VERIFIED`, is not a Gate
closure signature, and is not authority to edit the tracker to a closed state.

## Fixed baseline

All successor builders and checkers must begin by reproducing:

- post-H commit/tree:
  `16275f6cbf390c343a9ac00f9193e75eaad0094e` /
  `ca595b8e1258a8b78c4da3a545b2a31d8f62b531`;
- Slice G candidate:
  `a595bd93ceee9d352645b9be66db92517fffb092`;
- Slice H review verdict: `APPROVE_P0_0_P1_0_P2_0`;
- Slice H review SHA-256:
  `bdbc8b530ccabd1f79be78e380455bac5ef7123a957879c34225855adcbbc18f`;
- generation-lock v2 SHA-256:
  `de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`;
- Gate-criteria SHA-256:
  `4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`.

Git ancestry alone is insufficient. Each declared predecessor must be checked
by exact path, mode, size, SHA-256, Git blob, and required candidate/review
lineage before it can contribute evidence.

## Versioned authorities

### Generator-supply profile v3

The pre-replay implementation will add:

- `tools/generator-supply/v3/source.json`;
- `generator-supply-profile-source-v3.schema.json`;
- `generator-supply-profile-v3.schema.json`;
- `scripts/lib/platform-successor-dag-v3.ts` and focused tests;
- `scripts/lib/platform-generator-supply-replay-v3.ts` and focused tests;
- `scripts/lib/platform-generator-supply-profile-v3.ts` and focused tests;
- `scripts/generate-platform-generator-supply-profile-v3.ts`;
- `scripts/replay-platform-generators-isolated-v3.sh`.

The v3 source must bind the complete v2 closure: its source/schemas, full
manifest/profile, replay summary, seven raw receipts, both Slice F reviews,
the `contract-review-binding/v1` tuple/registry, Slice H review, and exact Git
lineage. Binding only the v2 profile is insufficient.

V3 uses a fresh projection and fresh Darwin arm64/Linux amd64 A/B native
replay. Linux arm64 remains `NOT_CLAIMED`. The existing platform generator
runner and 49 sorted core outputs may be reused byte-for-byte only if focused
checks prove that the core set is unchanged. The R5 phase machinery is
evidence-only and must not enter contract bootstrap, SDK discovery, or the core
output set.

The v3 profile remains `notGateClosure=true`, `ALL_GATES_OPEN`, and explicitly
records that legal approval, current vulnerability closure, external
signature/trust, publication, and full distribution-platform coverage are not
claimed.

### G-CONTRACT-P1 phase-record authority

The pre-replay implementation will add a Gate-specific authority under:

```text
tools/gate-phase-record/g-contract-p1/v1/
```

It includes a strict semantic `source.json`, source/model schemas, and a typed
builder/checker. Its CLI is
`scripts/generate-platform-g-contract-phase-record.ts`. A generic arbitrary-Gate
writer is forbidden.

The builder constructs and schema-validates one typed phase-record object, then
deterministically renders the canonical Markdown record:

```text
docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md
```

The record is the sole persisted R5 candidate. There is no companion candidate
JSON. The versioned source is the machine-readable semantic input, and the
later binding registry is the machine-readable effective-state output. The
checker rebuilds the typed object from fixed inputs and compares the entire
Markdown byte sequence, so it never treats hand-edited prose as authority.

The candidate writer must be explicit, exclusive-create, deterministic, and
no-op only when the existing bytes are exact. It must reject a partial/stale
topology, unknown field, reordered criterion, path alias, symlink, wrong Git
parent, dirty source, or changed supply profile/review. It must never overwrite
R1-R4 or any existing R5 byte.

R5 must bind:

- Inventory R3 and Baseline-P0 R4 as immutable prerequisites;
- R1-R4 and their reviews as immutable historical records;
- the fixed pre-replay projection commit/tree/archive/receipt;
- the assembled supply-v3 candidate commit/tree/diff;
- exact supply-v3 manifest/profile bytes;
- supply-v3 review commit/tree/parent/path/SHA/verdict;
- the current contract/SDK/toolchain/descriptor/generation-lock inputs;
- every `G-CONTRACT` exit row from
  `05-gates-and-acceptance.md` and its exact criteria-file SHA-256;
- explicit invalidation rules for every bound input.

Each criterion is one of:

- `SATISFIED_CANDIDATE`;
- `REVIEW_PENDING`;
- `OPEN_NOT_CLAIMED`;
- `NOT_APPLICABLE`.

No `OPEN_NOT_CLAIMED` or `REVIEW_PENDING` row may be omitted from the derived
missing set. R5 is generated with `Status=IN PROGRESS`,
`Independent reviewer=PENDING`, `notGateClosure=true`,
`gateStatus=ALL_GATES_OPEN`, and `closureDecision=NONE`. It does not bind the
commit that adds itself or its future review; the detached R5 reviewer and
binding tuple bind those later Git objects.

### Detached phase-record review binding

The same pre-replay candidate will add strict phase-binding source, tuple, and
registry schemas plus typed generator/checker code under
`tools/gate-phase-record/g-contract-p1/v1/`. The binding CLI uses the same
Gate-specific command surface and exposes explicit `--write-binding` and
`--check-binding` modes.

The tuple has two ordered, domain-separated review slots:

1. generator-supply-v3 assembled candidate -> supply-v3 review;
2. G-CONTRACT R5 candidate -> R5 review.

For each slot it binds candidate commit/tree/parent/fixed diff, exact review
child commit/tree/parent/path/blob/SHA-256, structured verdict and P0/P1/P2
counts, candidate-path absence in the reviewed parent, and reviewer separation.

The registry derives a machine-readable effective candidate view without
modifying R5. It must not identify itself as a canonical contract profile,
phase record, Gate signature, or release approval. It is excluded from
bootstrap, contract/SDK discovery, core outputs, and its own review authority.

The final binding review is terminal. A read-only verifier may consume it to
report `REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE`, but neither the tuple, registry,
lock, R5, nor tracker is regenerated afterward.

### Generation-lock v3

The implementation will add versioned lock-v3 logic and focused tests rather
than modifying the reviewed v2 builder in place:

- `scripts/lib/platform-contract-lock-v3.ts`;
- `scripts/lib/platform-contract-lock-v3.test.ts`;
- `scripts/generate-platform-contract-lock-v3.ts`.

The live path remains `contracts/generation.lock.json`. A second live contract
lock is rejected because it would widen contract discovery and create two lock
authorities. The v2 bytes remain recoverable and verifiable from the fixed
post-H Git blob.

The lock has explicit `ASSEMBLED` and `PHASE_BOUND` transitions. Each writer
must verify the prior Git/blob state, write only the expected next bytes, fsync
and rename safely, return success without output for exact current bytes, and
fail closed for stale, reordered, partial, candidate-only, review-only, or
tuple-ready/output-absent states. Historical assembled lock bytes remain fixed
in their Git commit when the later phase-bound lock advances the live path.

## Immutable predecessor fence

The v3 verifier must traverse and byte-fence:

- closure-profile v1 four-file immutable map;
- closure-profile v2 source, both schemas, and generated registry;
- closure-profile v3 source, both schemas, registry, fixed review, and lineage;
- generator-supply v1 outer files, all 39 manifest members, raw replay and
  projection evidence, profile/review, and candidate/review lineage;
- generator-supply v2 source/schemas, all manifest members, profile, replay
  summary, seven raw receipts, closure-v3 and supply-v2 Slice F reviews, and
  assembled candidate/review lineage;
- `contract-review-binding/v1` source, three schemas, tuple, registry, Slice G
  candidate, Slice H final review, and full late-bound chain;
- current generation-lock v2 Git blob and raw SHA/size;
- R1-R4 and every independent review without overwrite or reclassification;
- the existing 49 core outputs by exact ordered path, Git blob, SHA-256, size,
  and mode.

Unknown predecessor files, missing members, duplicate paths, mode drift,
symlinks, path normalization aliases, manifest reorder, digest-domain drift,
ABA replacement, or ancestry without exact byte equality must fail closed.

## Exact late-bound discipline

The v3 projection excludes exactly these 17 ordered paths and no wildcard:

1. `contracts/generation.lock.json`
2. `tools/generator-supply/v3/evidence-manifest.json`
3. `tools/generator-supply/v3/profile.json`
4. `tools/generator-supply/v3/evidence/replay.json`
5. `tools/generator-supply/v3/evidence/replay/darwin-a.json`
6. `tools/generator-supply/v3/evidence/replay/darwin-b.json`
7. `tools/generator-supply/v3/evidence/replay/darwin-isolation.json`
8. `tools/generator-supply/v3/evidence/replay/linux-a.json`
9. `tools/generator-supply/v3/evidence/replay/linux-b.json`
10. `tools/generator-supply/v3/evidence/replay/linux-isolation.json`
11. `tools/generator-supply/v3/evidence/replay/projection.json`
12. `docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md`
13. `docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md`
14. `docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md`
15. `tools/gate-phase-record/g-contract-p1/v1/review-tuple.json`
16. `tools/gate-phase-record/g-contract-p1/v1/registry.json`
17. `docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md`

All proposal, decision, implementation, source, schema, generator, test,
tracker, and plan-index bytes must be fixed before the projection. After the
projection is fixed, no non-exact17 tracked byte may change before the terminal
review. Candidate-tree versus projection-tree differences must equal this list
by count, order, and path.

The TypeScript topology authority and shell isolation wrapper must declare the
same exact list. Any wildcard, directory exclusion, unknown extra file, omitted
path, or reordered path fails closed.

## Generated state machines

Supply-v3 topology:

```text
DECLARED_PRE_REPLAY
  -> RAW_RECEIPTS_COMPLETE
  -> ASSEMBLY_READY_TO_WRITE
  -> ASSEMBLED_REVIEW_PENDING
  -> SUPPLY_REVIEW_PRESENT_UNVERIFIED
  -> SUPPLY_REVIEW_CURRENT
```

R5/binding topology:

```text
PRE_CANDIDATE_ABSENT
  -> R5_CURRENT_REVIEW_ABSENT
  -> R5_REVIEW_CURRENT_BINDING_ABSENT
  -> COMPLETE_TUPLE_READY_TO_WRITE
  -> PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT
  -> REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE
```

Only the exact next transition may write. Output without input, review without
candidate, registry without tuple, incomplete/reordered slots, wrong review
parent, self-review, candidate path present in the reviewed parent, stale
output, final-review path present before its fixed parent, symlink, unknown
field, or any unrecognized combination is a hard error.

The terminal review never causes another tracked transition. If any source,
projection, profile, review, security timestamp/database, trust root, waiver,
lock, schema, contract, SDK, migration, manifest, or Git-lineage input changes,
the old effective result becomes `INVALIDATED`; it is retained and superseded,
not overwritten.

Tracker drift is a checker failure, not a change in authority. The tracker may
route readers to the versioned checker and predeclared R5 path, but it must
remain `IN PROGRESS` and cannot turn a candidate into `VERIFIED`.

## Ordered slices

The dependency order is mandatory:

1. **Slice A - contracts and frozen predecessors**
   - add supply-v3, Gate-specific phase-record, binding, and lock-v3 contracts;
   - freeze all predecessor maps and exact17 topology;
   - add schema, self-reference, partial-state, Git-lineage, symlink, ABA, and
     immutable-member tests;
   - independently review the fixed Slice A candidate.
2. **Slice B - complete pre-replay implementation**
   - implement v3 replay/profile, phase-record renderer/checker, detached
     binder, lock transitions, and exact wrapper policy;
   - update tracker/index text only as non-authoritative future routing;
   - freeze the unchanged 49-output core set and all non-exact17 bytes;
   - independently review one clean pre-replay implementation candidate.
3. **Slice C - projection authority**
   - run focused deterministic write/check, schema, topology, format, diff, and
     redacted secret checks;
   - prove clean staged-tree reconstruction and exact exclusion equality;
   - fix the projection commit/tree/archive/receipt.
4. **Slice D - native replay**
   - run fresh Darwin arm64 A/B and Linux amd64 A/B under the v3 isolation
     wrapper;
   - preserve Linux arm64 as `NOT_CLAIMED`;
   - reject reuse or reinterpretation of v2 receipts.
5. **Slice E - assembled supply-v3 candidate**
   - assemble the replay summary, manifest, profile, and assembled lock;
   - run no-output current checks;
   - fix a distinct assembled candidate commit/tree/diff with every later path
     absent.
6. **Slice F - independent supply-v3 review**
   - independently reproduce projection, replay/profile, archive safety, and
     declared security boundaries;
   - add only the predeclared supply review in a direct review child;
   - return an explicit P0/P1/P2 verdict without changing the candidate.
7. **Slice G - generated R5 candidate**
   - run the explicit candidate writer from the fixed Slice F child;
   - bind the projection, assembled candidate, profile, and supply review;
   - enumerate every current `G-CONTRACT` criterion and preserve all open rows;
   - run no-output current checks and fix one R5 candidate commit/tree/diff.
8. **Slice H - independent R5 review**
   - independently reproduce the rendered record and every bound input;
   - verify criteria completeness, non-claims, invalidation, and current-source
     lineage;
   - add only the predeclared R5 review in a direct review child with an
     explicit P0/P1/P2 verdict.
9. **Slice I - detached phase binding**
   - generate the exact two-slot review tuple and binding registry;
   - advance the lock to `PHASE_BOUND`;
   - run final no-output current checks;
   - fix a distinct three-path candidate commit/tree/diff.
10. **Slice J - terminal binding review**
    - independently review the actual Slice I tuple, registry, and lock bytes;
    - reproduce both candidate/review Git lineages and effective-state result;
    - add only the terminal review with an explicit P0/P1/P2 verdict;
    - do not regenerate any tracked output afterward.

Automatic progression authority, after the ADR fixed-object review approves,
ends at Slice J. A later Gate transition requires a separate current criteria
audit and explicit immutable closure record; D-053 cannot be cited as that
authority.

## Focused verification boundary

Required focused checks include:

- predecessor path/SHA/size/mode/Git-lineage and manifest traversal;
- exact17 count/order and TypeScript/shell equality;
- projection reconstruction and archive safety;
- Darwin/Linux A/B same-bits, cross-platform, isolation, and raw receipt checks;
- domain-separated source/artifact/evidence/profile/registry digests;
- R5 Gate ID/phase/prerequisite/source/toolchain/criteria/invalidation binding;
- review tuple candidate/review lineage, path absence, structured verdict, and
  reviewer separation;
- exclusive/no-op/next-state lock and binding writers;
- mutation cases for unknown fields, reorder, extra Gate, self-review, symlink,
  ABA, status promotion, omitted waiver expiry, review drift, and tracker drift;
- focused format/lint/diff and redacted secret scans.

The existing native replay is performed once for v3. Broad Bun suites, broad
`go test ./internal/migration` or `go test ./...`, live PostgreSQL, production
migration, and unrelated test repetition are forbidden unless a new concrete
failure makes one narrowly necessary.

Ignored dependencies and temporary replay roots are not evidence. Any locally
created root `node_modules` must be removed after focused verification without
touching tracked or unrelated dirty bytes.

## Gate and side-effect boundary

D-053 authorizes only isolated worktrees/branches, versioned local/native
implementation, deterministic generation, focused checks, fixed-object review,
and evidence pushes.

It does not authorize:

- changing `G-CONTRACT`, `G-SUPPLY-CHAIN`, or any phase/aggregate Gate status;
- production database writes or production migration invocation;
- HTTP, OIDC/JWKS, P2, provider, workload, credential, or trust effects;
- deployment, publication, external signing, release, main merge, Beta, or GA;
- public npm/module/image/chart channels;
- rewriting closure-v3, supply-v2, binding-v1, R1-R4, or reviewed history;
- force-push, rebase/squash of evidence lineage, reflog expiry, GC, or prune;
- deleting unrelated branches/worktrees or touching the dirty portable-runtime
  worktree;
- presenting R5, `missing=[]`, the binding registry, or the terminal review as
  a Gate closure record.

## Rejected alternatives

### Keep supply-v2 current after ADR-0030

Rejected because every new decision/implementation byte is outside v2's fixed
exact16 exclusion set.

### Edit v2 exclusions, profile, receipts, or reviewed binding in place

Rejected because the reviewed predecessors are immutable and the old receipts
prove a different projection.

### Put R5 in the pre-replay core output set

Rejected because it cannot bind the later projection commit, assembled supply
profile, and independent supply review without self-reference. Such an output
would be another review-pending precursor, not current-source R5.

### Persist both candidate JSON and Markdown

Rejected because the strict semantic source plus deterministic typed renderer
already provides machine validation. A companion candidate file would add a
second persisted representation and require a new dual-authority equivalence
contract. The binding registry is the sole machine-readable effective view.

### Use tracker status as phase authority

Rejected because tracker text is a derived index. Drift must fail a check, not
promote or invalidate evidence by itself.

### Rewrite R5 after its review

Rejected because any byte change invalidates the fixed review. Review binding
must be detached.

### Generate another binding after the terminal review

Rejected because it creates unbounded review recursion. The final review is
consumed only by a read-only verifier.

### Reuse v2 receipts or run only one platform/copy

Rejected because the source projection and evidence identity change. Supply-v3
requires fresh Darwin arm64 and Linux amd64 A/B replay.

### Interpret candidate completion as Gate closure

Rejected because candidate evidence, even with `missing=[]`, is not an
immutable Gate signature and does not satisfy unrelated Data, Authority,
Security, Supply, release, or aggregate criteria.

## Consequences

- v1/v2/v3 closure and supply/binding history remains immutable and recoverable.
- One fresh v3 replay covers every new semantic generator and checker byte.
- R5 binds actual assembled supply/review facts without binding its own future
  commit or review.
- Two detached reviews cover the supply and R5 candidates before the phase
  binding is generated; a terminal review covers the actual binding bytes.
- Tracker/index updates are fixed before projection and remain non-authoritative.
- All Gates and external side effects remain open or absent.

## Decision status

The continuing Platform goal authorizes the recommended versioned continuation
without per-slice approval. D-053 accepts the exact17 topology, immutable
predecessor fence, current-source R5 ordering, and ordered Slices A-J.

Execution begins only if an independent review of the fixed ADR-0030 candidate
returns `APPROVE, P0=0 / P1=0 / P2=0`. A rejection or any nonzero P0/P1 finding
requires an append-only superseding design candidate and new review. Acceptance
does not authorize a Gate transition or any production/external effect.
