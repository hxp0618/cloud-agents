# G-CONTRACT post-H current-source successor entry audit

Date: 2026-08-25 Asia/Shanghai

## Result

`SUPERSEDING FIXED DESIGN REVIEW REQUIRED`

The fixed post-H baseline is internally consistent, but it is not a current
`G-CONTRACT` phase record and it does not authorize a Gate transition. The
smallest safe continuation is a new versioned, projection-aware successor that
replays the complete post-H source, assembles generator-supply v3, obtains an
independent supply review, and only then generates a current-source R5 phase
candidate for its own detached review and binding.

The first ADR-0030 candidate `78ac538...` received `REJECT, P0=0 / P1=3 /
P2=0` at review commit `11513d8...`. This superseding audit/ADR pair repairs
the lock-transition, terminal-review timing, and review-only-child findings.
It still does not authorize Slice A until the repaired fixed object receives an
independent approval. It performs no runtime, database, HTTP, provider,
deployment, publication, signing, release, or Gate action.

## Fixed entry identity

- branch: `codex/cloud-agents-platform-p0`;
- post-H commit: `16275f6cbf390c343a9ac00f9193e75eaad0094e`;
- post-H tree: `ca595b8e1258a8b78c4da3a545b2a31d8f62b531`;
- Slice G binding candidate:
  `a595bd93ceee9d352645b9be66db92517fffb092`;
- Slice H verdict: `APPROVE, P0=0 / P1=0 / P2=0`;
- Slice H review SHA-256:
  `bdbc8b530ccabd1f79be78e380455bac5ef7123a957879c34225855adcbbc18f`;
- current generation-lock SHA-256:
  `de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53`;
- current Gate-criteria SHA-256:
  `4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994`.

The rejected design lineage is preserved:

- candidate:
  `78ac538725b6bb000d0963021119b852df784248`;
- candidate tree:
  `0d3a744390a63792de002d33c989977aa6c84c09`;
- rejection review commit:
  `11513d8e6ae87d2c3352e73b0a471d2834a5af19`;
- review document SHA-256:
  `05b4b0032accbe121eb155ffe9eea9cb1b9ea2ade0c1ba631506e8b94c340f14`.

The audited P0 worktree was clean and its remote branch was exact when the
post-H fixed-object review was completed. The unrelated dirty portable-runtime
worktree remains outside this audit and must not be changed.

## What post-H proves

The current source contains an immutable, reviewed chain:

```text
generator-supply-v2 assembled candidate
  -> closure-v3 and supply-v2 detached reviews
  -> contract-review-binding-v1 tuple and registry
  -> final Slice H independent review
```

The chain establishes that:

- closure-profile v1/v2/v3 predecessor bytes remain reproducible;
- generator-supply v2 has a fixed Darwin arm64/Linux amd64 A/B replay,
  projection receipt, manifest, profile, and independent review;
- `contract-review-binding/v1` binds the fixed closure-v3 and supply-v2 review
  pair without changing either reviewed predecessor;
- the final Slice H review approved the actual tuple/registry/lock candidate;
- all outputs remain `notGateClosure=true` and `ALL_GATES_OPEN`.

It does not establish a new `G-CONTRACT` phase record. The existing R4 record
predates the current generator lock and is stale under its own invalidation
rules. The status tracker is also a historical projection input: it still says
formal Slice C is `NOT_STARTED`, even though the append-only Git history now
contains completed Slices C-H. Tracker prose is therefore not status authority.

## Deepest verified blocker

Generator-supply v2 fixes a staged-tree projection that excludes exactly 16
ordered late-bound paths. ADR-0030, a new phase-record generator, a new R5
record, or any tracker/index repair is outside that exact set. Adding any of
those bytes makes the v2 projection historical by construction.

The old v2 replay cannot be reinterpreted against a new tree. Editing its
source, profile, manifest, exclusion list, or reviewed lock in place would
violate the accepted D-052 immutable-predecessor boundary. Reusing an existing
excluded path would make the path identity dishonest and weaken the reviewed
projection semantics.

Therefore a fresh generator-supply v3 projection and native replay are
mandatory before a new current-source phase candidate can be admissible.

## Why the R5 output must be post-assembly

A pre-replay R5 output cannot truthfully bind all of the facts that define the
new current source:

- the fixed pre-replay projection commit and tree;
- the assembled supply-v3 candidate commit and tree;
- the final supply-v3 profile and evidence-manifest bytes;
- the independent supply-v3 review commit, path, digest, and verdict.

Embedding its own later commit or review digest would be self-referential.
Leaving all those values pending would merely create another precursor, not a
current-source phase record.

The safe ordering is to include the R5 semantic source, strict schema, typed
builder, deterministic renderer, checker, and tests in the pre-replay
projection, but generate the single canonical Markdown R5 record only after
the supply-v3 assembly and its detached review are fixed. A later R5 review
binds the commit that introduces that record. The R5 record never binds its own
commit or review.

The renderer validates a strict typed phase-record object before producing the
Markdown bytes. The checker rebuilds the object from fixed inputs and compares
the complete rendered bytes. No companion candidate JSON is persisted: the
versioned semantic source is the machine-readable input authority, while the
post-review binding registry is the machine-readable pre-terminal binding
authority. A separate versioned read-only checker consumes the later terminal
review and emits the terminal candidate state only to stdout. This avoids both
JSON/Markdown dual authority and a tracked post-review recursion.

## Required effective-state rule

Neither the Markdown record nor the tracker may promote itself. Effective
state must be computed by a versioned, read-only checker:

```text
effective candidate state = verify(
  frozen source and projection,
  immutable supply-v3 assembly and review,
  immutable ASSEMBLED lock snapshot,
  immutable R5 record and review,
  detached binding tuple, registry, and authorized PHASE_BOUND lock successor,
  terminal final review,
  current invalidation inputs
)
```

The terminal state authorized by this successor is
`REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE`. It is not `VERIFIED`, is not a Gate
signature, and does not change `G-CONTRACT` from `IN PROGRESS`.

The Slice J review document reviews only the fixed Slice I candidate; it cannot
claim a terminal state that depends on its own future commit. After the
single-path review child exists, the read-only checker verifies its exact
parent/path/blob/SHA/verdict/diff and may emit the terminal result. It writes no
tracked output.

The checker must fail closed for orphan review, partial tuple/output, wrong Git
parentage, merge parent, reordered slots, unknown fields, path aliases,
symlinks, self-review, extra review-child paths, rename/copy/mode drift, review
verdict drift, stale projection/profile, an unauthorized lock transition,
tracker promotion, or any unrecognized topology.

R5 binds the immutable Slice E `ASSEMBLED` lock commit/tree/blob/SHA/size/state.
The Slice I `PHASE_BOUND` lock binds that snapshot plus the R5 review and
tuple/registry bytes. This one exact transition is an authorized successor, not
R5 invalidation. Every other lock mutation remains invalidating drift.

## Exact successor late-bound set

The supply-v3 projection must exclude exactly these 17 paths in this order and
no wildcard:

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

The R5 source/schema/generator/tests and the phase-binding
source/schemas/generator/tests are pre-replay inputs, not exclusions. The R5
record is evidence-only, non-bootstrap, non-core, and excluded explicitly
because its fixed supply inputs do not exist until after assembly and review.

## Immutable predecessor fence

The successor must verify, without rewriting:

- all closure-profile v1/v2/v3 source, schema, and generated registry bytes;
- closure-v3 fixed review bytes and lineage;
- generator-supply v1 outer files, its complete 39-member evidence-manifest
  closure, projection/replay evidence, review, and candidate/review lineage;
- generator-supply v2 source/schemas, complete manifest/profile, replay summary,
  seven raw receipts, both Slice F reviews, and candidate/review lineage;
- `contract-review-binding/v1` source, three schemas, tuple, registry, Slice G
  candidate, Slice H review, and the complete
  `1ba7eda -> d7c7468 -> a595bd9 -> 16275f6` Git chain;
- the generation-lock v2 blob at the fixed post-H commit before advancing the
  same live path through the versioned v3 writer;
- R1-R4 and all of their independent reviews as immutable Gate history;
- every existing 49 core generator output blob unless an explicit pre-replay
  decision changes the core set.

Generator-supply v3 must bind the complete supply-v2 assembly and review-binding
closure, not only `tools/generator-supply/v2/profile.json`.

## Ordered continuation

The acyclic dependency order is:

```text
design review
  -> versioned contracts and predecessor fence
  -> complete pre-replay implementation
  -> fixed projection
  -> Darwin arm64 and Linux amd64 A/B replay
  -> supply-v3 assembly
  -> independent supply-v3 review
  -> generated R5 candidate
  -> independent R5 review
  -> detached tuple/registry and phase-bound lock
  -> terminal independent binding review
  -> read-only terminal state verification with no tracked output
```

Fresh Darwin arm64 and Linux amd64 A/B replay is required. Linux arm64 remains
`NOT_CLAIMED`. The replay must use the versioned isolation wrapper; v2 receipts
cannot be copied or cited as current v3 evidence.

## Explicit non-claims

This successor must not claim or change:

- any phase or aggregate Gate status;
- aggregate `G-BASELINE`, which still requires M1;
- `G-DATA` backfill/cutover/contract, deployed N/N-1, PITR/HA/failover, live
  whole-schema PostgreSQL, or filesystem `Done`;
- `G-AUTHORITY-P1` provider catalog/receipt writers, published executable
  projection, or complete runtime ownership;
- `G-SECURITY-P1` production trust, OIDC/JWKS, HTTP enforcement, live RLS/pool
  matrix, current secrets, or limit closure;
- `G-SUPPLY-CHAIN` Linux arm64, legal approval, external signing, release,
  complete distribution/image/chart/SDK descriptors, current SBOM/provenance/
  VEX/CVE/base-image closure;
- any P2-P6 aggregate, Platform RC, Exposure, Beta, GA, deployment, or release;
- abrupt crash, BMC hard-off, physical unplug, controller/cache-loss, or any
  remote-machine result not actually executed.

Ordinary `poweroff`/`reboot` remains classified under the accepted project
terminology, but any future evidence must still describe the observed clean
shutdown/restart mechanism accurately.

## Entry verdict

Preserve the rejected `78ac538... -> 11513d8...` lineage and review this
append-only superseding ADR-0030 object. If and only if the repaired review
returns `APPROVE, P0=0 / P1=0 / P2=0`, the continuing Platform goal authority
permits the ordered local/native implementation without another per-slice
approval. That authority ends after the terminal review-only child and its
read-only state check; it does not extend to a Gate transition or any external
side effect.
