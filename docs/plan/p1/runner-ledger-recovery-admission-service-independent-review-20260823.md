# Runner ledger recovery admission service independent review — 2026-08-23

- Status: `APPROVE`
- Verdict: `P0=0`, `P1=0`, `P2=0`
- Fixed candidate: `23c3083b7d7b58089f2cb208b1381b2d510500ff`
- Candidate branch: `codex/cloud-agents-p1-runner-recovery-admission-20260823`
- Review branch: `codex/cloud-agents-p1-runner-recovery-admission-independent-review-20260823`
- Gate effect: none; this review does not start Slice C or close or advance any Gate

This is an independent read-only review of ADR-0023/D-047 Slice B. It approves only the fixed
same-verifier recovery-admission, fresh locked database observation, and action-specific
`close_without_mutation` path for the twelve generated unsupported consumer pairs. It does not
approve a recovery writer, public recovery result, database mutation, or external side effect.

## Fixed identity and scope

| Identity                               | Value                                                              |
| -------------------------------------- | ------------------------------------------------------------------ |
| fixed candidate                        | `23c3083b7d7b58089f2cb208b1381b2d510500ff`                         |
| fixed candidate tree                   | `75e50c3068682c16b60f1e287c8f96d97bab1af7`                         |
| Slice A base                           | `67210b7f194ec1591c06957ddfc86920a58af167`                         |
| code commit                            | `b7a996274f61fa9972ccd1e00bd8c83985faafbb`                         |
| fixed `services/control-plane` subtree | `556750fa193ebf788f7e8065b15f7357506a6d36`                         |
| lifecycle witness SHA-256              | `a784a973f126af039f72878b557adb9b1441021e402c36f251c1d60cd6621968` |
| evidence session SHA-256               | `3ef7d4ca7b3f224a6a1843b60d79524b1ed6006ec670894081c11fdb442ac5f5` |
| recovery claim SHA-256                 | `dc51c5cb0856f63c6a8b31a11ef79ca8ca1ed0b6ae26bf8c21a171704d29f5ba` |
| recovery permit SHA-256                | `e13b255fbfc453b04fc8b073db4ce683b8726fc04f0de28db0c0ea0401ab2aa9` |
| consumer service SHA-256               | `abd71c7c8cc01e1f34437506a908d35f166b8ed39be5d65b73767c9a31bea135` |
| generation lock SHA-256                | `b62d1884c37d8e2856969fa14ef6440ff3635c7c6aeb411d3e55ad36fca59492` |
| implementation matrix SHA-256          | `1f41f4106d4ea8c05c8a446bb1ac9c0c692fd76b4d4fa44809243cfa5d4e40eb` |

The candidate was clean, its upstream was `0/0`, and the remote candidate branch resolved to the
exact commit. The base-to-candidate diff is exactly 17 paths: one generation lock, twelve migration
code/test paths, and four documentation paths. This review did not edit the candidate.

## Authority and transition review

The retained concrete evidence session closes its old generation/session/journal graph before a
one-way `GenerationLease.ReacquireAdmission`. A package-private one-shot lifecycle witness binds the
candidate, exact generation, journal state, and a deep-cloned retry/ambiguous lifecycle chain. The
fresh admission inventory is replayed from the full root at revision zero, must match exactly one
registered generation, and is re-read again before claim consumption. Any disk-only retry or
ambiguous history without the live witness remains `EVIDENCE_RECOVERY_REQUIRED`.

The recovery claim and use record bind the same verifier, candidate, generation, recovery snapshot,
schema/state/session/journal digests, full-set/transcript/index facts, consumer subject, and exact
generated action. They are registry-backed, non-copyable, one-shot, and revoked on failure or
evidence-session close. A refresh replaces the old graph irreversibly; the old journal cursor and
registries cannot be reused after success or failure.

The database boundary uses one fresh dedicated session, migration-owner role/settings, advisory
lock, exact ledger prefix/head, catalog and authority projections, signed entry/plan/attempt limits,
and final session/authority/ledger/catalog rereads. It mints one of six distinct package-private
close-only permit types with direct pair counts `4/1/1/1/3/2`. The distinct recovery success-writer
profile is excluded, has no direct pair, and has no production caller. Literals, copies, wrong permit
types, missing registry records, changed full-root facts, context cancellation, and permit tamper all
fail closed while cleanup retains the exact registered database handle.

The only new public consumer transition is admission followed by `close_without_mutation`, after
which the existing entry or recovery operation still returns
`MIGRATION_PROJECTION_NOT_IMPLEMENTED`. Diff-scoped and AST call-graph review found no new
`BeginMigration`, SQL execution, transaction commit, ledger/evidence append, result writer,
HTTP/P2/provider, deployment, or publication edge. `AppendGeneration*` identifiers elsewhere in
`evidence_session.go` are unchanged successor paths and are not called by this Slice B graph.

## Fresh checks

The review used Node `24.13.1`, Bun `1.3.14`, and Go/gofmt `1.26.6`:

- fixed commit/tree/subtree, remote exact ref, clean state, upstream `0/0`, exact 17-path scope, and
  listed SHA-256 values: PASS;
- focused lifecycle/recovery-admission/profile/public-NI normal tests: PASS in `12.115s`;
- risk-narrow lifecycle/claim/permit/tamper tests under `-race`: PASS in `29.821s`;
- recovery registry and contract-lock tests: `23/23` PASS with `191` assertions;
- recovery registry generator, recovery Go generator, and generation-lock checker: PASS/current;
- migration-package `vet` and compile-only test: PASS;
- changed Go files under `gofmt -d`, `git diff --check`, forbidden-surface scan, and Gitleaks over
  `67210b7..23c3083`: PASS, two commits and approximately `150.53 KB`, no leaks.

The first TypeScript attempts from the dependency-less candidate worktree and one lock attempt with
the ambient Go `1.26.7` were environment failures and are explicitly NOT PASS. The recorded PASS
results above are the subsequent exact-toolchain runs using an existing read-only dependency tree;
the temporary dependency link was removed and did not alter candidate or review sources.

No full `internal/migration`, full shard suite, broad race, live PostgreSQL, production database, or
external-side-effect test was run or claimed. The concrete successful
`GenerationLease -> AdmissionInventory -> GenerationLease` refresh cannot be constructed by an
ordinary migration-package fixture without trusted evidencefs provisioning authority. This review
therefore verifies the production call graph and its bounded opaque components; it does not claim a
fabricated end-to-end authority constructor. No timeout, skipped check, or metadata-only result is a
PASS.

## Non-claims

This review does not modify the fixed candidate, start Slice C, implement or authorize any recovery
writer/result, or change any unsupported pair to success. It does not merge or open a pull request,
connect to a live or production database, execute SQL, append ledger/evidence, add
HTTP/P2/provider behavior, deploy, publish, release, or close a Gate.

The verdict approves only fixed Slice B candidate
`23c3083b7d7b58089f2cb208b1381b2d510500ff` as the reviewed read-only recovery-admission closure.
