# G-CONTRACT current-source phase successor Slice A implementation — 2026-08-25

## Boundary

This record implements only ADR-0030 / D-053 Slice A on fixed parent
`cabce4981b22202446ef47b037ad41cb49e4e304`, the approving direct-child review
of the superseding design candidate. The implementation branch is
`codex/p1-phase-successor-slice-a`.

Slice A adds only versioned sources and schemas, frozen predecessor and DAG
verifiers, typed phase-record/phase-state contracts, generation-lock v3 state
transitions, and focused tests. It creates none of the exact17 late-bound
outputs (the already-tracked predecessor generation lock is unchanged). It
adds no writer, CLI, replay receipt, generated profile, evidence manifest,
phase record, review, review tuple, binding registry, or lock transition
output.

The fixed post-H P0 predecessor remains
`16275f6cbf390c343a9ac00f9193e75eaad0094e`. P0, Inventory R3, and Baseline-P0
R4 are not rerun or reinterpreted by this slice.

## Versioned authorities

Generator-supply v3 adds a strict source and source/profile schemas. The source
freezes eight predecessor groups and 39 direct predecessor files, traverses the
complete predecessor manifests, preserves the byte-identified sorted 49-path
core output set, and defines exactly 17 ordered late-bound exclusions. It
declares replay evidence only as `DECLARED_PRE_REPLAY`; no v3 replay has run in
Slice A. Linux arm64 remains `NOT_CLAIMED`.

The Gate-specific phase authority under
`tools/gate-phase-record/g-contract-p1/v1/` adds:

- one strict semantic source for `G-CONTRACT` / P1 / R5;
- source, model, two-review tuple, and pre-terminal binding-registry schemas;
- one typed deterministic R5 model/Markdown renderer contract;
- one read-only topology and effective-state checker contract.

The source fixes all five current `G-CONTRACT` exit rows and maps them onto the
seven formal criteria in the reviewed current-candidate registry. Criterion
status and `missing` are derived from that fixed registry; they are not
hard-coded status promotions. The current registry presently derives five
`SATISFIED_CANDIDATE` rows and `missing=[]`, while the phase record remains
`IN_PROGRESS`, independent review remains `PENDING`, Gate effect remains
`NONE`, and every Gate remains open.

Generation-lock v3 defines only two document states:

1. `ASSEMBLED`, bound to the immutable post-H generation-lock v2 Git object and
   the future supply-v3/projection identities;
2. `PHASE_BOUND`, the sole permitted successor, bound to exact R5, R5 review,
   review tuple, and binding-registry identities.

The Slice A lock module contains no filesystem writer or mutation capability.
The historical generation-lock v2 verifier reads the fixed post-H Git blob,
including its raw `status` and self-digest, rather than reopening the later
live lock path.

## Fail-closed lineage and state rules

The contracts reject wildcard or reordered exclusions, path aliases,
symlinks, partial topologies, unknown fields, immutable-member drift, Git
parent/tree/blob/mode/diff drift, self-review, ambiguous verdict text, and
byte-identical ABA replacement.

R5 construction parses and validates the fixed Slice E candidate's exact
`ASSEMBLED` lock-v3 blob. It also requires the supply review to be a unique
direct child with an exact structured zero-finding approval, then compares the
fixed review binding with the current tracked `HEAD` blob and one stable live
read.

Once R5 exists, inspection requires a typed `recordBuildInput`, rebuilds the
model internally from all current authorities, and compares the entire
rendered Markdown byte sequence. There is no caller-supplied expected-byte or
current-input callback bypass. Before reporting the R5-review/pre-binding
topology, the checker proves an exact R5-only candidate, a direct one-path
review child, the structured verdict, and fixed-Git/live review bytes. It may
report `COMPLETE_TUPLE_READY_TO_WRITE` only after validating the exact typed
two-lineage tuple and reviewer separation.

For the later Slice I topology, the checker reads the fixed `ASSEMBLED` Git
blob and the candidate `PHASE_BOUND` blob, reconstructs the only authorized
transition, verifies the exact three-path operation set, and checks all four
phase artifacts against both Git and live bytes. Terminal review verification
binds its exact commit, parent, tree, path, blob, SHA-256, size, mode, diff,
unique `## Verdict`, zero findings, reviewer separation, and live bytes. Only a
read-only result may later report `REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE`.

## Review-driven repairs

Independent read-only reviews were applied before fixing this candidate. The
final bytes close findings in these areas:

- derive R5 criteria from the fixed reviewed current-candidate registry;
- parse and semantically validate the fixed `ASSEMBLED` lock instead of
  accepting a caller-declared format/state;
- separate historical generation-lock v2 Git-object verification from the
  later live v3 lock path and use the actual v2 `status` field;
- make the `ASSEMBLED -> PHASE_BOUND` reconstruction and four-artifact binding
  mandatory rather than callback-selectable;
- require an exact structured verdict and complete terminal-review identity;
- remove expected-byte/current-input callback bypasses and bind all review and
  phase-artifact live bytes;
- close both early-return paths for supply-review and R5-review currentness.

The final cross-module read-only review returned
`APPROVE, P0=0/P1=0/P2=0`. It edited no file and ran no test.

## Focused verification

Only bounded owning suites were run:

| Scope | Result |
| --- | ---: |
| initial successor DAG and predecessor focus | 11/11 PASS |
| repaired lock-v3 and predecessor focus | 14/14 PASS |
| final phase-record and phase-state focus | 12/12 PASS, 52 assertions |

All ten new TypeScript files pass `oxfmt --check` and zero-warning `oxlint`.
All eight new JSON files parse and exactly equal canonical two-space
`JSON.stringify` bytes with one trailing newline. The final root and worktree
`node_modules` paths are absent.

No broad Bun suite, broad migration Go test, `go test ./...`, live PostgreSQL,
native replay, SSH, production database write, HTTP/P2/provider effect,
deployment, publication, signing, or Gate command was run.

## Gate and next-slice status

This implementation is a non-Gate Slice A candidate only. Every declared
boundary remains `notGateClosure=true`; `G-CONTRACT`, `G-SUPPLY-CHAIN`, all
phase Gates, and every aggregate Gate remain `IN PROGRESS`/`OPEN`.

Slice B may start only after this exact clean candidate is fixed and an
independent direct-child review returns `APPROVE, P0=0/P1=0/P2=0`. No main/P0
merge, production write, deployment, release, publication, signing, or Gate
transition is authorized by this record.
