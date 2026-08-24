# G-CONTRACT closure/supply successor R1 assembly-writer independent review

Date: 2026-08-25

## Verdict

`APPROVE`

| Severity | Findings |
| -------- | -------: |
| P0       |        0 |
| P1       |        0 |
| P2       |        0 |

This independent fixed-object review approves only the R1 assembly-writer
candidate identified below. It does not approve or install a formal Slice C
projection, run formal Slice D native replay, authorize Slice E, produce a
successor lock or detached review binding, close a Gate, or authorize a
production database, HTTP/P2/provider, deployment, publication, release, or
main-merge effect. All Gates remain `IN PROGRESS`/OPEN.

The approved R1 object removes the implementation blocker, but formal Slice C
is still `NOT_STARTED`. Its projection must be constructed from the final
clean review-child `HEAD`/tree after this review record and its index updates
are committed and fast-forwarded into `codex/cloud-agents-platform-p0`.
Candidate `c547f04f15b86a6b33f73ea633837fd8db6cc00b` remains the reviewed
implementation candidate and an ancestor, not the projection checkout. Once
the review-child commit/tree is fixed, all non-exact16 tracked bytes are frozen
throughout formal Slice C, formal Slice D, and Slice E materialization; only
the predeclared exact 16 changes are permitted. Slice C/D status or attestation
must not be written back to non-excluded README/tracker bytes. Slice D must not
precede the resulting fixed projection and Slice E remains `NOT_AUTHORIZED`.

## Fixed candidate identity

- branch reviewed: `codex/review-successor-supply-r1-c547f04`;
- parent: `720fb6086940b5f08fb309eb6e4a31df723b5151`;
- candidate: `c547f04f15b86a6b33f73ea633837fd8db6cc00b`;
- candidate tree: `339962e5d000560caab3e004e66f7f3c2d362f18`;
- parent-to-candidate full-index binary diff SHA-256:
  `ac3a4d319554e495a6632706cfb62a55b9b694e1c2573dd115fb7c52d847cede`;
- candidate path count: exactly `5`;
- live `origin/codex/cloud-agents-platform-p0` pointed exactly to the candidate
  during review;
- this review path was absent from the candidate, so the candidate does not
  self-review;
- the review worktree remained clean until this separate record was authored.

The following SHA-256 values are file-content digests and sizes are candidate
byte counts.

| Path                                                                                    | SHA-256                                                            |   Bytes |
| --------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ------: |
| `docs/plan/cloud-agents-platform/06-status-tracker.md`                                  | `2eeb549063d5d82373bd8cfe84a1c891f8a1e762ab2d281966a9f6ec29d2edab` | 138,002 |
| `docs/plan/p1/README.md`                                                                | `25e2c41305db07857798a2b1d3122ef9112ea09513d8dae29b32f544800983fb` |  65,330 |
| `docs/plan/p1/g-contract-successor-supply-rebind-r1-assembly-writer-repair-20260825.md` | `149ac5657f1404714b805b1f2166d65402c50816f2cbc5f4d364564d20457bf3` |  16,544 |
| `scripts/lib/platform-generator-supply-profile-v2.test.ts`                              | `4473af000e3e91050af88abcdb53a486b1861b8f1e728729205307eec4a51caa` |  33,744 |
| `scripts/lib/platform-generator-supply-profile-v2.ts`                                   | `dc1173e67c5fd83bbb632020c274f9fed9c2a6503238545a5fd91474a4308727` |  65,856 |

## Historical rejection and P1 closure

The historical candidate `96d72c966bd86ed29abb301cb0ff5bb1fb8ce43e`
remains rejected by its fixed-object review with
`REQUEST_CHANGES, P0=0/P1=1/P2=0`. That review found that the writer captured
schema identities but discarded the bytes, then built Ajv by rereading
lexical schema paths. A schema-parent A → B → A sequence could therefore
validate with B while the terminal identity fence observed A.

Candidate `c547f04f15b86a6b33f73ea633837fd8db6cc00b` closes that formal P1:

1. both schemas are read stably and copied into owned byte snapshots;
2. one Ajv instance is constructed from the captured pair and reused for
   source and output validation without an assembly-time lexical schema
   reread;
3. the original schema/source identities remain in the cumulative authority
   fence;
4. a deterministic test replaces the complete schema parent A with a
   materially rejecting B and restores A around both source and output
   validation phases; and
5. invalid JSON and reject-all captured output schemas fail before any late
   receipt or assembly output is created.

## Complete R1 authority review

The review covered the complete R1 implementation authority, not only the
two-file repair diff:

- exactly seven external raw receipts are copied on ingress, checked as the
  exact closed path set, and validated before deriving one canonical summary;
- caller-owned buffers cannot alter prepared authority: SharedArrayBuffer
  inputs are rejected, private snapshots are used internally, and public map
  reads return copies;
- the canonical summary plus seven raw receipts form the exact ordered eight,
  followed by one evidence manifest and one profile for exactly ten
  append-only destinations;
- the same cumulative v1 predecessor snapshot covers all six outer immutable
  files, all 39 evidence members, and semantic profile/review reads throughout
  the transaction;
- source/schema, raw/source topology, prepared semantics, destination parents,
  and every already-published output remain fenced before and after each
  publish and at the terminal currentness check;
- exact same bytes are an idempotent no-op, divergent bytes conflict,
  no-replace publication handles a competing winner, an exact published prefix
  resumes without rewrite, and temporary cleanup refuses to unlink a changed
  lexical path; and
- production CLI code invokes only `writeGeneratorSupplyV2Assembly`, which
  supplies no mutation hooks; hooks remain behind the explicit test-only
  writer entrypoint.

The immutable v1 profile and its writers are unchanged. The legacy
`contracts/generation.lock.json` is neither rewritten nor replaced.

## Fixed-object verification

The exact seven focused Vitest files were:

1. `scripts/check-platform-contract-standards.test.ts`;
2. `scripts/lib/platform-contract-closure-profile-v3.test.ts`;
3. `scripts/lib/platform-contract-lock.test.ts`;
4. `scripts/lib/platform-generator-supply-profile-v2.test.ts`;
5. `scripts/lib/platform-generator-supply-replay-v2.test.ts`;
6. `scripts/lib/platform-successor-dag.test.ts`; and
7. `scripts/lib/platform-successor-predecessor.test.ts`.

They returned `7/7 files, 105/105 tests PASS`.

| Check                                        | Result                                                                                                                                          |
| -------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| Exact five candidate files, `oxfmt 0.62.0`   | `PASS`                                                                                                                                          |
| Two candidate code files, `oxlint 1.77.0`    | `PASS`                                                                                                                                          |
| Exact parent-to-candidate `git diff --check` | `PASS`                                                                                                                                          |
| Narrow nine-file TypeScript diagnostic       | no R1-file diagnostics; 11 inherited diagnostics in unchanged modules: `platform-generator-supply-profile.ts` 6, `platform-json-semantics.ts` 5 |
| Supply / binding state                       | `DECLARED_PRE_REPLAY` / `PRE_REVIEW_ABSENT`                                                                                                     |
| CLI exact-arity negatives                    | extra `--check-v2` and assembly with 0, 2, or 4 arguments all rejected                                                                          |
| Exact late-bound exclusions                  | 16; only `contracts/generation.lock.json` present                                                                                               |
| Legacy generation lock                       | 237,214 bytes; SHA-256 `29cd59f1f69e35a6c0fd312524883b6a90be6fe09616dd21864ed9ce52c96101`; unchanged                                            |
| Gitleaks 8.30.1 exact candidate range        | one exact commit; zero findings                                                                                                                 |
| Repository-root dependency topology          | `node_modules` absent after verification                                                                                                        |

The historical out-of-scope
`scripts/lib/platform-generator-supply-profile.test.ts` diagnostic was not
rerun. Its previously recorded `18/28 PASS` result and ten known immutable-v1
replay/wheelhouse binding mismatches remain excluded Slice B history, not
evidence for or against this candidate.

No broad Bun suite, Go or migration test, SSH, native replay, formal Slice C/D
operation, late-output generation or installation, database, HTTP/P2/provider,
deployment, publication, release, merge, or Gate operation was performed by
this review.

## Progression boundary

The fixed R1 implementation prerequisite is approved. The next permitted
lineage action is to commit this review record and its three index updates,
then fast-forward that clean review-child commit into
`codex/cloud-agents-platform-p0`. Formal Slice C may begin only from that final
clean review-child `HEAD`/tree, with
`c547f04f15b86a6b33f73ea633837fd8db6cc00b` retained as its reviewed
implementation ancestor. After the review-child commit/tree is fixed, all
non-exact16 tracked bytes remain frozen throughout formal Slice C, formal
Slice D, and Slice E materialization; only the predeclared exact 16 changes are
permitted, and C/D status or attestation may not be written into non-excluded
README/tracker bytes. The fixed projection must precede formal Slice D
Darwin/Linux native replay. Slice E remains unauthorized until those ordered
authorities and reviews are complete. The diagnostic C/D projection and
receipts remain stale/non-admissible and may not be reused.
