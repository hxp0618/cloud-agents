# G-CONTRACT successor and generator-supply rebind entry audit

Date: 2026-08-24

## Result

`ENTRY DECISION ACCEPTED`

The next implementation cannot safely be a direct edit of
`contract-closure-profile/v2`, `generator-supply-profile/v1`, the generated
lock, or either current `missing` entry. The smallest safe continuation is a
new versioned successor design with one acyclic replay boundary. Every Gate
remains open.

This audit is read-only with respect to runtime, databases, providers, remote
hosts, deployment, and publication. D-052 subsequently accepts its recommended
non-Gate successor ordering under the continuing Platform goal authority.

## Fixed entry identity

- audited branch: `codex/cloud-agents-platform-p0`;
- audited HEAD and review commit:
  `129e9bc128de971b9f9623e82832e80830331126`;
- audited HEAD tree: `b30835163d757e236781af8c16c61736e1d452da`;
- generator-supply fixed candidate:
  `e5f981c8197cea7527a57c391e7198570f61b92c`;
- generator-supply candidate tree:
  `7fb98abf71066e8009581c658b41a299ae1a5c2c`;
- consolidation parent:
  `0a331fde18a909d37b64f11efe879df7bbc09d25`.

The late-bound generator-supply independent review returned
`APPROVE, P0=0, P1=0, P2=0`. Its document SHA-256 is
`86ec054debf15de71481d6f9ab965ca5c8f24a4f5a98f9e5e155e24df261cd47`.
That verdict approves only the fixed v1 candidate and does not rewrite its
generated status or close a Gate.

## Immutable predecessor bytes

The following v1 generator-supply bytes are now predecessor authority:

| Path                                                                              | SHA-256                                                            |
| --------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `tools/generator-supply/v1/source.json`                                           | `a14e177c72afb699b47446232625ba638c68da2bc7731e213ab432244924a2f9` |
| `tools/generator-supply/v1/generator-supply-profile-source-v1.schema.json`        | `6204f24913dccc98e80e415f00fce74e2bfa99b68df691b1669f0c00592002ab` |
| `tools/generator-supply/v1/generator-supply-profile-v1.schema.json`               | `6f51389646fdbcf8633b56495d1d128b92bec1958dbc1acb96afaf2d75ea2d64` |
| `tools/generator-supply/v1/evidence-manifest.json`                                | `4e6ec3c1b89a40c6dd9ee989997c7ec28d44730eac8387e065d8cc524b973bc7` |
| `tools/generator-supply/v1/profile.json`                                          | `dcd9c9da7cd28a254dbeb419a388875b843033c0ca522fc603cd29b30295f93b` |
| `docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md` | `86ec054debf15de71481d6f9ab965ca5c8f24a4f5a98f9e5e155e24df261cd47` |

The reviewed projection remains fixed at:

```text
tree                         4a70fb8b1e18801f4f02a753668ffe91b63b6275
archive SHA-256              36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478
archive size                 46008320 bytes
projection receipt SHA-256   1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4
core generated outputs       48
```

ADR-0028 requires these v1 bytes to remain immutable after review. A correction
or current-source replay must use a new version.

## Current unresolved contract state

`contract-closure-profile/v2` remains immutable current generated authority
with exactly two derived `missing` entries:

1. `runtime-server-path-and-tenant-authority-enforcement`;
2. `remaining-generator-supply-chain-review`.

The runtime criterion has a fixed current-lineage implementation and independent
review, but that review explicitly delegates classification to a later
versioned generated successor. The generator-supply criterion now has a fixed
v1 replay/profile review, but its generated profile and generation lock
intentionally retain `REPLAY_VERIFIED_REVIEW_PENDING` and
`independentReview=PENDING`.

There is no implemented `tools/generator-supply/v2`, no immutable-v1 verifier
for that registry, and no `contract-closure-profile/v3`. The current closure
generator's version type is limited to v1/v2. The current replay runner and
isolation wrapper require the exact 48-output set and contain only
`contract-closure-profile-v2.json`.

## Deepest verified blocker

The projection is the current staged Git tree minus 13 exact late-bound paths.
Every other tracked byte is part of the reviewed projection. Adding a v3
contract source/schema/output, changing its generator, updating plan indexes, or
adding a v2 supply registry therefore creates a new projection.

Three tempting orderings are invalid:

1. Mutating supply v1 or closure v2 violates predecessor immutability.
2. Adding closure v3 alone makes the reviewed v1 supply projection historical
   before any successor supply evidence covers the new generator.
3. Completing a full supply v2 replay first and adding closure v3 afterward
   immediately invalidates that new projection and forces a duplicate native
   replay.

Changing the v1 checker to reinterpret the old receipt as current HEAD would
also weaken reviewed semantics and is not an allowed repair.

## Recommended acyclic successor ordering

The proposed D-052 decision should freeze these ordered slices:

### Slice A — successor contracts and frozen predecessors

- add explicit byte-fence checks for generator-supply v1 and contract-closure
  v1/v2;
- verify every one of the 39 fixed v1 evidence-manifest members by exact path,
  SHA-256, and size rather than fencing only the manifest bytes;
- define strict versioned schemas and typed builders for supply v2, closure v3,
  and a detached post-review binding registry;
- define exact future late-bound paths, with no wildcard exclusion;
- retain `notGateClosure=true` and `ALL_GATES_OPEN`.

No generated `missing` entry changes in this slice.

### Slice B — one pre-replay implementation candidate

- add the complete closure-v3 semantic source, schemas, generated candidate,
  and supply-v2 authority together;
- keep closure v1/v2 immutable while registering v3 as a pre-replay core
  generator output;
- derive and freeze the expanded core output set, including every deterministic
  manifest/SDK fanout caused by v3, before replay;
- bind the reviewed runtime candidate and reviewed supply-v1 predecessor;
- retain the supply criterion as review-pending in canonical v3;
- include the detached binding-registry generator/schema in the projection, but
  not a future review tuple or generated binding.

### Slice C — focused generation and projection preflight

- run only focused schema/profile/lock/replay tests and deterministic
  write/check commands;
- build a fresh projection and prove exact archive reconstruction;
- fix candidate commit/tree/diff before native replay.

### Slice D — one dual-platform A/B replay

- execute Darwin arm64 A/B and Linux amd64 A/B on fresh roots;
- reuse the existing exact tool supply only if every executable, dependency,
  archive, and scanner byte remains bound;
- preserve Linux arm64 as `NOT_CLAIMED`;
- do not run broad Bun or Go migration suites.

### Slice E — late-bound supply-v2 assembly

- assemble replay summary, evidence manifest, supply-v2 profile, and generation
  lock after raw replay receipts are fixed;
- keep the supply-v2 profile review-pending and non-Gate;
- require a no-output post-assembly current check.

### Slice F — assembled supply and pre-consumer review

- independently rebuild and compare the projection;
- review canonical closure-v3 semantics, supply-v2 replay/profile/security, and
  the dormant detached-binding consumer;
- bind the assembled supply candidate commit/tree/diff;
- write the supply review record only to its exact predeclared excluded path;
- return explicit P0/P1/P2 verdicts without modifying the candidate.

### Slice G — detached classification consumer

Only after Slice F approval may the separately generated late-bound
review-binding registry consume the fixed supply-v2 review digest. It may derive
an effective `missing=[]` candidate view, but it must not mutate or masquerade
as canonical contract-closure v3, which retains its reviewed pre-consumer
bytes. The binding registry is outside contract bootstrap, fixture discovery,
SDK manifests, and the core replay output set.

The registry's schema and generator must already be projection inputs, and its
exact review tuple/output/final-review paths must already be exclusions. It must
not trigger a second native replay solely to insert review bytes.

Its state machine is fail-closed:

1. before review, the tuple and output are both absent and core write/check
   handles canonical closure v3 only;
2. explicit post-review write accepts one complete review tuple and atomically
   creates the initially absent binding output;
3. post-review check requires the complete tuple and byte-current output;
4. every partial tuple/output state fails.

Slice G must run the explicit binding write/check, generation-lock write/check,
and a final no-output current check after the new binding is present.

### Slice H — final fixed-consumer review

- fix a distinct post-consumer commit/tree/diff;
- independently validate review path/SHA/verdict bindings, effective missing
  derivation, generation-lock bytes, and bootstrap separation;
- return a new P0/P1/P2 verdict over bytes that actually exist;
- keep the registry review-pending and non-Gate until that verdict is recorded.

Even after Slice H approval, `SATISFIED_CANDIDATE` is not an immutable Gate
signature and does not change `G-CONTRACT` from `IN PROGRESS`.

## Allowed and forbidden scope

Allowed after owner approval:

- versioned generated registries/profiles and immutable predecessor fences;
- exact detached review digest binding;
- focused contract/profile/lock/replay checks;
- fresh native Darwin arm64 and Linux amd64 replay;
- independent read-only fixed-candidate review.

Not authorized:

- in-place mutation of generator-supply v1 or contract-closure v1/v2;
- manual deletion of a generated `missing` entry;
- HTTP/P2/provider behavior or production trust provisioning;
- production database writes;
- deployment, publication, release, signing, or authenticated identity claims;
- Gate record mutation or closure;
- broad Go migration or broad Bun test suites.

## Entry verdict

The repository entered D-052 implementation after the continuing Platform goal
accepted ADR-0029. At entry, the reviewed v1 evidence remains fixed-candidate
support, closure v2 remains unchanged, both formal criteria remain `MISSING`,
and every Gate remains open. Slice A must establish the successor contracts and
fences before any native replay.
