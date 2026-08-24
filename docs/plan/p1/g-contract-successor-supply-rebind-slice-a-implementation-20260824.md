# G-CONTRACT successor/supply rebind Slice A implementation — 2026-08-24

## Boundary

This record implements only ADR-0029 / D-052 Slice A on fixed parent
`980563719664d92af80dd2930db020380011930a` in branch
`codex/cloud-agents-platform-p0`. It defines frozen predecessor checks,
versioned successor schemas and typed semantic validators, the exact future
late-bound DAG, and a detached review-binding state machine.

This slice does not create the closure-v3 source or generated registry, the
generator-supply-v2 source, replay receipts, evidence manifest or generated
profile, a generation lock, any review tuple or binding output, or any current
alias. No formal `missing` item changes in Slice A.

## Frozen predecessor authority

The Slice A verifier fixes and checks:

- all four contract-closure v1 files and all four contract-closure v2 files by
  exact path, SHA-256 and size;
- all six generator-supply v1 outer files;
- the fixed generator-supply v1 evidence manifest and all 39 members in exact
  UTF-8 bytewise path order, with every member independently checked by
  SHA-256 and size;
- the five generated generator-supply v1 profile identities;
- generator-supply v1 candidate/review commit, tree, parent, binary diff,
  review blob and normalized `APPROVE_P0_0_P1_0_P2_0` verdict;
- the reviewed v1 projection tree `4a70fb8b1e18801f4f02a753668ffe91b63b6275`,
  archive SHA-256
  `36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478`,
  archive size `46008320`, and fixed projection receipt SHA-256
  `1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4`;
- the runtime current-lineage candidate
  `b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e`, its exact parent/tree/diff,
  the two module files, and closed-pair review commit
  `fe59f0d4059632a102171d9c1eb77a4c147ae65e`.

Regular-file reads reject lexical escape, final or ancestor symlinks and
non-directory parents. Immutable reads use `O_NOFOLLOW` and compare descriptor
and path device/inode, size, modification time and change time before and after
the read.

Git provenance is intentionally a repository-only pre-projection check. The
canonical byte/semantic validators remain usable inside an already verified
`.git`-less projection archive. Slice C must call the repository lineage
composite before constructing that archive.

## Exact successor DAG

The projection authority fixes these 16 ordered future exclusions without a
wildcard:

1. generation lock;
2. generator-supply-v2 evidence manifest;
3. generator-supply-v2 profile;
4. replay summary;
5. Darwin A receipt;
6. Darwin B receipt;
7. Darwin isolation receipt;
8. Linux A receipt;
9. Linux B receipt;
10. Linux isolation receipt;
11. projection receipt;
12. closure-v3 independent review;
13. generator-supply-v2 independent review;
14. detached review tuple;
15. detached binding registry;
16. detached binding independent review.

No generator-supply-v1 evidence, rejected-executor receipt, closure-v3
source/schema/output, supply-v2 source/schema, or binding generator/schema is
an exclusion. The DAG API classifies topology only; every present state is
named `*_PRESENT_UNVERIFIED` until its owning semantic validator succeeds.

## Versioned successor contracts

Generator-supply v2 adds strict source/output schemas and a typed validator.
It binds the complete v1 predecessor and projection, declares exactly eight
fresh native replay receipt paths, retains Linux arm64 as `NOT_CLAIMED`, and
uses the five domain-separated
`cloud-agents/generator-supply/{source,artifact-set,evidence-manifest,profile,registry}/v2`
digests. The profile digest includes its complete evidence object. Receipt
presence alone is `REPLAY_RECEIPTS_PRESENT_UNVERIFIED`; only exact receipt
bytes, standalone/embedded manifest equality, all digests and all boundaries
can produce `ASSEMBLED_PROFILE_CURRENT`. There is no writer in Slice A.

Contract-closure v3 adds strict source/output schemas and a deterministic typed
builder. It carries criteria 0–4 byte-semantically from v2, classifies only the
reviewed bounded runtime criterion as `SATISFIED_CANDIDATE`, and retains the
generator-supply criterion as the sole `REVIEW_PENDING` and derived `missing`
item. The canonical source forbids reading its own source/output, future
supply-v2 profile/review, detached tuple/output, or final review. Runtime HTTP,
OIDC, JWKS, project writer, provider and external effects remain
`NOT_IMPLEMENTED`.

The detached binding registry has its own identity and cannot masquerade as a
canonical closure profile. Its complete semantic consumer validates the actual
closure-v3 source and registry and the complete supply-v2 source, predecessor,
receipts, manifests, profile and registry. Profile-shaped skeletal JSON is
rejected. Its exact state machine is:

- source plus tuple/output absent: `PRE_REVIEW_ABSENT`, including `--write`
  no-op;
- complete tuple plus output absent: explicit write required;
- complete tuple plus byte-current output: check succeeds;
- every other tuple/output combination: fail closed.

Publication uses an fsynced same-directory temporary regular file, hard-link
no-overwrite publication and directory `fsync`. The tuple binds both authority
files and both independent review commit/tree/parent/diff/blob lineages. An
effective `missing=[]` exists only in this detached candidate view and does not
mutate canonical closure v3.

## Focused verification

The final Slice A byte set passed only the bounded suites that own these
contracts:

| Scope                          |     Result |
| ------------------------------ | ---------: |
| immutable predecessor verifier | 10/10 PASS |
| exact successor DAG            |   6/6 PASS |
| generator-supply v2            | 10/10 PASS |
| contract-closure v3            |   5/5 PASS |
| detached review binding        | 13/13 PASS |
| total focused Vitest           | 44/44 PASS |

All 18 implementation/schema/test files pass `oxfmt`; all seven new JSON
schemas parse successfully; the exact 21-path staged candidate passes
`git diff --cached --check` and a redacted Gitleaks 8.30.1 staged scan with zero
findings. No broad Bun suite, migration Go suite, PostgreSQL matrix, remote
replay, production database, HTTP, provider, deployment or publication command
was run.

Independent module-level read-only reviews returned `APPROVE,
P0=0/P1=0/P2=0` for the predecessor fence, exact DAG, supply-v2 contract,
closure-v3 contract and detached binding state machine. These reviews cover the
working byte set but do not replace the required fixed-commit Slice A review.

## Gate and next-slice status

Every generated/declarative boundary remains `notGateClosure=true` and
`ALL_GATES_OPEN`. `G-CONTRACT`, `G-SUPPLY-CHAIN` and every aggregate Gate remain
open. Slice B is not authorized by this record alone: first fix and push the
Slice A candidate, then obtain an independent P0/P1/P2 review of that exact
commit. No production database write, HTTP/P2/provider effect, deployment,
release, publication, main merge or Gate transition is authorized.
