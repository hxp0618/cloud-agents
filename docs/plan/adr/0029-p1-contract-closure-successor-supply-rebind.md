# ADR-0029: P1 contract-closure successor and generator-supply rebind

- Status: Proposed
- Date: 2026-08-24
- Decision ID: D-052
- Depends on: ADR-0026, ADR-0027, ADR-0028,
  `g-contract-runtime-current-lineage-integration-independent-review-20260824.md`,
  and `g-contract-generator-supply-profile-independent-review-20260824.md`
- Decision owner: hxp0618
- Proposed implementation executor: Codex
- Gate effect: none; `G-CONTRACT`, `G-SUPPLY-CHAIN`, and every aggregate
  Gate remain `IN PROGRESS`/`OPEN`

## Context

Contract-closure profile v2 has exactly two formal missing criteria. Both now
have bounded implementation evidence and independent fixed-candidate reviews,
but neither review authorizes an in-place status change. Runtime review requires
a later versioned generated successor. Generator-supply v1 is
`REPLAY_VERIFIED_REVIEW_PENDING` by construction, its detached review approves
only its immutable fixed bytes, and ADR-0028 requires a new version for any
repair or current-source successor.

The current generator-supply projection is the staged Git tree minus 13 exact
late-bound paths. It replays 48 generated outputs and includes the closure
profile generator, closure v2 input/output, replay authority, plan inputs, and
all other non-excluded tracked bytes. Consequently, a direct closure-v3 change
would make supply v1 historical, while a supply-v2 replay performed before the
closure-v3 implementation would have to be repeated.

The entry audit
`docs/plan/p1/g-contract-successor-supply-rebind-entry-audit-20260824.md`
records the fixed predecessors and the invalid orderings.

## Proposed decision

Adopt one versioned, acyclic successor program. Prepare closure-v3 generator
authority and generator-supply-v2 authority in the same pre-replay candidate,
then perform one new dual-platform A/B replay. Late-bound profile and review
artifacts must be inserted only through exact predeclared exclusions.

The proposed shape includes the complete closure-v3 semantic source, schemas,
and generated candidate in the pre-replay projection. The core output set
expands once to include v3 and every deterministic manifest/SDK fanout. Closure
v3 retains the future supply-v2 review as pending.

A separate post-review binding registry—not closure v3 itself—may later consume
the exact supply-v2 review tuple. Its generator and schema are pre-replay
inputs, while only its future review tuple, output, and final review are exact
late-bound exclusions. It is outside contract bootstrap, fixture discovery,
SDK manifests, and the core output set. This prevents review self-reference
without replaying a second core projection.

This decision is non-Gate. A generated criterion may become
`SATISFIED_CANDIDATE`, but that classification is not a phase record,
aggregate signature, deployment approval, or Gate transition.

### Immutable predecessors

The implementation must byte-fence:

- all four immutable contract-closure v1 files already fixed by v2;
- contract-closure v2 source, source schema, output schema, and generated
  registry;
- generator-supply v1 source, both schemas, evidence manifest, generated
  profile, fixed projection receipt, replay summary and raw receipts;
- all 39 members declared by the fixed v1 evidence manifest, each verified by
  exact path, SHA-256, and size;
- the generator-supply v1 independent review document and exact
  `APPROVE_P0_0_P1_0_P2_0` verdict.

The fixed manifest/profile bytes are necessary but not sufficient: the
successor verifier must traverse the fixed manifest and reject any member
drift. Source, schemas, profile, and review are fenced separately where they
are outside that member set. No predecessor byte may be rewritten, regenerated
in place, reclassified, or absorbed by regenerating a v1 manifest/profile.

### Successor semantics

Generator-supply v2 must:

- have a new format/profile identity and strict v2 source/output schemas;
- bind the immutable v1 predecessor, fixed candidate lineage, and detached v1
  review digest;
- use a new projection and fresh native Darwin arm64/Linux amd64 A/B receipts;
- retain Linux arm64 as `NOT_CLAIMED`;
- remain `notGateClosure=true`, `ALL_GATES_OPEN`, and review-pending until
  its own independent review exists;
- preserve the existing legal, vulnerability-timestamp, signing, identity, and
  publication limitations.

Contract-closure v3 authority must:

- preserve closure v1/v2 byte-for-byte;
- retain the exact seven-item criterion inventory and derive `missing` only
  from criterion statuses;
- fix its complete semantic source before projection/replay;
- bind the reviewed runtime current-lineage candidate without claiming HTTP,
  OIDC/JWKS trust provisioning, project writing, or external effects;
- bind generator-supply-v1 predecessor evidence while retaining the future
  supply-v2 review criterion as pending;
- keep all statuses at candidate level and every Gate open.

The detached review-binding registry must:

- have its own strict identity/schema and never identify itself as a canonical
  contract-closure profile;
- consume only predeclared review path/SHA/verdict tuples;
- bind immutable closure-v3 and supply-v2 profile identities;
- derive an effective candidate view without mutating closure v3;
- remain review-pending, `notGateClosure=true`, and `ALL_GATES_OPEN` until a
  final independent review covers its existing bytes.

Its generated-state machine is exact:

- **pre-review:** review tuple and binding output both absent; normal core
  closure write/check handles canonical v3 and does not fabricate a binding;
- **post-review write:** one complete tuple is present, output is absent, and an
  explicit binding write atomically creates it;
- **post-review check:** tuple and output are both complete and the output is
  byte-current;
- **partial:** every other tuple/output combination fails closed.

The implementation must not claim an effective `missing=[]` until that
detached consumer can bind every required fixed review byte without
self-reference. Its exact schema is a required Slice A design output.

### Exact late-bound discipline

Before the projection is fixed, the implementation must enumerate every future
raw replay receipt, generated supply-v2 manifest/profile/lock, fixed independent
review record, detached review tuple/output, and final review path. Closure-v3
semantic source/schema/output are not late-bound exclusions. Wildcards and
directory-wide replay exclusions are forbidden.

The projection builder must prove:

- no unstaged tracked or untracked input;
- reconstructed Git tree equality;
- exact exclusion count, order, and path equality across TypeScript and the
  versioned wrapper;
- candidate-tree versus projection-tree difference contains only declared
  exclusions;
- no post-review file can alter replayed generator inputs or any core output.

### Ordered slices

The implementation order is mandatory:

1. **Slice A — contracts and DAG:** frozen predecessor maps; supply-v2,
   closure-v3, and detached-binding schemas/builders; exact late-bound paths;
   adversarial self-reference tests;
2. **Slice B — pre-replay implementation:** complete closure-v3 semantic
   source/output, supply-v2 authority, final expanded core output set, dormant
   detached consumer, post-assembly lock derivation, implementation record;
3. **Slice C — projection authority:** focused tests,
   format/lint/diff/secret checks, deterministic generation checks, projection
   reconstruction, immutable pre-replay commit/tree/archive;
4. **Slice D — native replay:** fresh Darwin arm64 A/B and Linux amd64 A/B under
   the versioned isolation wrapper;
5. **Slice E — assembled supply candidate:** replay summary, evidence manifest,
   supply-v2 profile, generation lock, no-output current check, distinct fixed
   commit/tree/diff;
6. **Slice F — assembled review:** closure-v3 pre-consumer semantics and
   supply-v2 replay/profile/security verdicts with P0/P1/P2;
7. **Slice G — detached consumer candidate:** bind exact approved review tuples
   in the predeclared non-bootstrap registry, derive the effective candidate
   view, run explicit binding write/check, regenerate/check the late-bound
   generation lock, run a final no-output current check, and fix a distinct
   commit/tree/diff;
8. **Slice H — final consumer review:** independently review the actual Slice G
   registry/lock bytes, derived missing view, and bootstrap separation with a
   new P0/P1/P2 verdict.

Performing a complete supply-v2 native replay before adding closure-v3 generator
authority is rejected because it would immediately invalidate the projection
and repeat the most expensive evidence step.

### Verification boundary

Focused verification may include:

- contract-closure, generator-supply, replay-wrapper, evidence-checker, and
  detached-binding, generation-lock unit tests;
- deterministic write/check and no-output post-assembly checks;
- archive safety and projection reconstruction checks;
- Darwin arm64 and Linux amd64 A/B native replay;
- formatting, lint, diff, and secret scans;
- independent fixed-candidate review.

Broad `go test ./internal/migration`, broad Bun suites, production database
tests, HTTP/P2/provider effects, deployment, publication, release, and Gate
operations remain outside scope.

## Rejected alternatives

### Rewrite generator-supply v1 or closure v2

Rejected because both are immutable reviewed predecessors.

### Mark the current generation lock review-approved

Rejected because the detached review digest is not part of the current
generated state and the lock is intentionally `independentReview=PENDING`.

### Add closure v3 and manually remove both missing entries

Rejected because it bypasses generated derivation, invalidates the current
supply projection, and turns bounded reviews into an unauthorized Gate claim.

### Replay supply v2 first, then implement closure v3

Rejected because the closure change would create a second projection and force
a redundant dual-platform replay.

### Exclude the editable closure-v3 semantic source

Rejected because supply replay would not bind the actual classification input,
and post-review source changes could fan out into core contract/SDK manifests.

### Reinterpret the v1 receipt as current after source changes

Rejected because it would weaken the reviewed staged-tree projection semantics.

## Consequences if accepted

- v1/v2 predecessors remain recoverable and immutable.
- One new native replay covers the final pre-review successor generator set.
- Closure-v3 source/output and every core fanout are supply-replayed.
- Review bytes remain detached and non-self-referential in a non-bootstrap
  registry.
- Slice H reviews the final consumer bytes rather than a not-yet-materialized
  candidate.
- Plan indexes and status trackers may be updated only as projection-aware
  successor inputs; they may not silently present v1 as current-source evidence.
- `G-CONTRACT`, `G-SUPPLY-CHAIN`, production operation, release, and every
  aggregate Gate remain open.

## Approval required

The owner must approve or reject the Proposed decision, immutable predecessor
set, exact late-bound discipline, and ordered Slices A-H. Until approval, no
successor code, generated profile, generation lock, replay, remote execution,
status transition, or Gate action is authorized by this ADR.
