# Gate record: `G-BASELINE-P0` / P0 / R4

- Evidence ID：`CAG-G-BASELINE-P0-20260823-R4`
- Record type：PHASE
- Phase / aggregate Gate：`G-BASELINE-P0`；aggregate `G-BASELINE` remains open
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`
- Status：`VERIFIED`
- DRI：hxp0618（owner）；Codex P0 closure-repair executor
- Independent reviewers：Codex P0 baseline fixed-input reviewer；Codex P0 Inventory-R3 cross-reviewer
- Date：2026-08-23 Asia/Shanghai
- Supersedes：`CAG-G-BASELINE-P0-20260810-R3` after fixed-input audit lineage review
- Gate effect：closes `G-BASELINE-P0` only；aggregate `G-BASELINE` remains open

## Why R4 supersedes R3

R3 correctly rebound the unchanged Synara/T3/Runtime behavior evidence to Inventory R3, but it retained the R2 baseline
evidence commit `66e2f127...` and audit SHA `06aded...` as if that audit had asserted the closed Platform P0 phase. At
`66e2f127...`, all three manifests still contained
`platformP0CharacterizationClosure.complete=false/status=INCOMPLETE`, and audit `06aded...` required and reported that
state. Commit `04fa503...` later changed the manifests to `true/COMPLETE` and changed the audit semantics, producing
audit SHA `409bf8...`. R3's own invalidation rule treats an audit-semantics change as invalidating, so R3 cannot be the
current closure authority.

R4 does not rerun or reinterpret product behavior. It separates two identities that R2/R3 conflated:

1. the immutable raw/normalized behavior evidence remains fixed at `66e2f127...` with its exact known failures and
   `NOT_RUN` boundaries；
2. the phase-closure semantics are fixed at candidate `1d442638...`, where all three manifests are
   `true/COMPLETE`, the audit validates that state, and the README is checked for the same current markers.

R1, R2 and R3 remain immutable historical files. The fixed candidate below received two independent read-only
`APPROVE, P0=0/P1=0/P2=0` reviews before this status transition.

## Fixed inputs

- Closure-semantics candidate commit：`1d442638d0c734fc3e277001561c1fba9992b650`
- Closure-semantics candidate tree：`f4840b2fb14fd325c296111fc64a2dab6f4635a8`
- Candidate parent / current reviewed platform lineage：`e3b8812f122c40fdf50ff586083839295119b16b`
- Candidate branch：`codex/cloud-agents-p0-baseline-r4-repair-20260823`
- Independent review commit/tree：`3f2f9f077c8122b767a2f2947013eab1f42fedc7` /
  `8abfdb950fcf3072e0291fdc1bcca50de940de52`
- Independent review record / SHA-256：
  [`CAG-G-BASELINE-P0-20260823-R4-independent-review.md`](CAG-G-BASELINE-P0-20260823-R4-independent-review.md) /
  `44db2df153bbfcc5fa0bd4c928bbdf9b207c60c4458ec61b2e2557c7d97d4c94`
- Inventory prerequisite record / decision SHA-256：
  - `CAG-G-INVENTORY-P0-20260810-R3`
  - `d865bb9fd1195342a7251bd6822130437753f5be4af8312d9d5c5ee4d5b71400`
  - `24a7f918636b7f0baafaa6c99ff1c04c9b8ad10163cbb568fd311718bb9342ee`
- Original behavior evidence commit/tree：`66e2f127b60ccdb94e8e9087e126dbdb5d121ad7` /
  `a26acd36ab77f1be0e9a007786d85070affc6276`
- Current closure audit / baseline README SHA-256：
  - `4adc7b8b9b8ea00dac6d5cf7496d7842643b36469a56e25ef2eb4e1dfae1e9e7`
  - `f545cf1a7297991859cdc585b65443dd1757345284ee9285c0fec02a96326aac`
- Current manifest SHA-256：
  - Synara：`0dbdf7e08a1b546644d689341fcdbc03e3176a0b56aa6015de86bf9576597d28`
  - T3：`69d278e056efcc98d730f8564ae91666bbc6fa5115ef4a3077c4504b5b415656`
  - Runtime/reference host：`d26109629740f199392b4bc390c3169e07dbbf9c66d1faa5d9517d4acb2f8bc8`
- Normalized execution record SHA-256：
  - Synara：`a3783d9132620df822271f9c18915777f256c49986f47df7566a46f8ec63b93c`
  - T3：`d2fcd7c26bcb8ac1f9897096066f639a0460bfa038b6de8e943933a96e9280ee`
  - Runtime：`1a94f7903dee9a22e10f0cda44ea815e4ff5d6658bb9742da25eb8012ecdbc48`
- Synara raw evidence index SHA-256：`4dab2c69c991bbad6b7855e68901679e8086b61d1b199426a4c6417544f78f3f`
- Runtime immutable rc.1 source/tree：`49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a` /
  `952996e6847f67c8ad9f5986f960c645d0b9c8f8`
- Current P0 Runtime corpus source/tree/archive：`c2c03584656a0db04de2f6b84113ac932459eae6` /
  `2b3e3dbded35d97565f387abff497e31cc498126` /
  `640f4a4fe01c4abf643b277ec69695b73f152ef8854af7f2e454ef5848933cbf`
- Protocol fixture source/tree：`0a984d02bda3c64f7dca1e9da7bed60e4d3f02f6` /
  `28cd7fd50d107ae169f1444364faa8fa3fd40562`
- Synara source/tree/archive：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0` /
  `ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc` /
  `cdcd4dd2d9571d45ab73f9cce36d43b4560f94d41aabc1291a7606d4d7427380`
- T3 main source/tree/archive：`8101cd044911c7dc2a2adf7c7a9ba7962abf57b6` /
  `e98f5650379f428bf5dcc6e7cae287c68fb8b080` /
  `87d78b9d2e9631ba10b84bad697afe271a0dac37d2b950aecc440577e61be1c2`
- T3 Cloud Agents source/tree/archive：`9584a266e91fa94354e8c07f79af3a5e01755d16` /
  `171624a2dbfb68f1d91f0a67175cbaf68f2947c2` /
  `dd9d7ab0c174dcf82ca377d96b8a5531e3dd166b401c414ad490bcfa97bc4dc1`
- Toolchains：local Node 24.13.1；recorded Linux Node 24.13.1 / Bun 1.3.14 / pnpm 11.10.0 /
  Go 1.26.5 / PostgreSQL 15.18 UTF8
- Deployment profile：none；no Provider credentials, publication, deployment, database migration, workload, endpoint,
  grant or production writer

## Exit criteria mapping

| Criterion                                                   | Candidate result                      | Evidence                                                                                                     |
| ----------------------------------------------------------- | ------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| valid fixed inventory prerequisite                          | PASS                                  | Inventory R3；decision SHA `24a7f918...`                                                                     |
| exact fixed refs, trees and archive identities              | PASS                                  | three normalized records；Git object/archive/file/mode/link checks                                           |
| complete Protocol 2.2/2.3 golden and negative corpus        | PASS                                  | Git-bound `p0-protocol-golden-v3`；29 commands、7 message variants、4 correlation negatives、4 Stop outcomes |
| Synara legacy mechanisms characterized                      | PASS WITH KNOWN PRECONDITION FAILURES | 138 focused PASS；broad 934 PASS/1 precondition failure；4 concurrency cases stopped before assertions       |
| T3 embedded and bridge characterization                     | PASS                                  | main 7 files/30 tests；feature 5 files/39 tests；targeted typechecks                                         |
| current Runtime/golden corpus characterization              | PASS                                  | Protocol 13、testkit 5、Runtime 33、three typechecks、format/lint and evidence audit                         |
| greenfield Managed Host reference baseline                  | PASS AS SPEC/NEGATIVE ORACLE          | lifecycle/failure/reap/fencing/idempotency/pairing/restart/orphan/resource traces                            |
| normalized and raw evidence identity                        | PASS WITH RETENTION BOUNDARY          | normalized hashes；Synara 21-entry index；T3/Runtime indexes                                                 |
| closure manifest/audit/README semantics agree               | PASS                                  | all manifests `true/COMPLETE`；audit and README SHA fixed；stale markers rejected                            |
| real Codex/Claude and real T3 workspace/checkpoint behavior | NOT RUN                               | belongs to `G-BASELINE-M1`                                                                                   |
| production Managed Host create→ready→terminate              | NOT RUN                               | belongs to P3 implementation and `G-MANAGED-HOST`                                                            |
| independent fixed-candidate review                          | PASS                                  | two read-only reviews；`P0=0/P1=0/P2=0`；review commit `3f2f9f0...`                                          |

`PASS WITH KNOWN PRECONDITION FAILURES` preserves the observed frozen-source result. It does not turn the four Synara
concurrency assertions into passes. `PASS AS SPEC/NEGATIVE ORACLE` is not production lifecycle execution.

## Reproduction

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-baseline-evidence.mjs

shasum -a 256 \
  docs/plan/p0/baseline/README.md \
  docs/plan/p0/scripts/audit-baseline-evidence.mjs \
  docs/plan/p0/baseline/{synara-legacy,t3-embedded,reference-host-negative}.json \
  docs/plan/p0/baseline/{synara,t3,runtime}-linux-amd64-execution.json
```

The audit is bounded and read-only. It verifies four repositories, 39 fixed Git paths, ten fixture blobs, three Linux
execution records, current manifest closure markers, M1/aggregate boundaries, and the README markers. It does not run
Provider calls, migration suites, product tests, databases or deployments.

## Failures, restrictions and downstream propagation

1. R3's prerequisite rebind was necessary but insufficient because its fixed audit asserted the pre-closure manifest
   state. R3 therefore cannot remain current even though its behavior evidence is unchanged.
2. Synara's missing provider capability catalog source, the plan-code fixture failure and the four stopped concurrency
   assertions remain exactly recorded. No source was patched and no failure was waived.
3. Real Provider/SendTurn/workspace/checkpoint/reconnect behavior remains `NOT_RUN` under `G-BASELINE-M1`；aggregate
   `G-BASELINE` remains open.
4. Raw logs retain the recorded single-host boundary. Git-bound normalized records and indexes are tamper-evident but
   are not publication-grade retention.
5. Every downstream candidate or record that names Baseline R3 as an immutable prerequisite must be explicitly rebound
   to an approved R4 before it can inherit a current closure. No downstream Gate is upgraded by this candidate.

## Rollback / cleanup evidence

- The candidate changed only the baseline README and bounded audit semantics.
- No runtime, database, endpoint, grant, workload, volume, credential or production state was created.
- Main-worktree `.gitignore`/`.idea/**` changes and all unrelated worktrees remain untouched.

## Invalidation

If approved, R4 becomes invalid if Inventory R3 is invalidated, or if any fixed source ref/tree/archive, fixture
commit/tree/blob, normalized record digest, raw evidence index, manifest closure field, README boundary, audit semantics,
or closure-semantics candidate identity changes. Loss or mismatch of the only raw evidence copy invalidates replayability
until the same fixed source is re-characterized and a superseding record is independently reviewed.

## Sign-off

- DRI conclusion：the unchanged behavior evidence is complete for the Platform P0 characterization scope when bound to
  Inventory R3 and the current `true/COMPLETE` closure audit semantics.
- Reviewer conclusion：fixed identities, bounded audit, two README fault injections, Inventory R3 prerequisite and
  all no-upgrade boundaries passed with `P0=0/P1=0/P2=0`.
- Closure decision：`G-BASELINE-P0 = VERIFIED`。R3 becomes historical `INVALIDATED`；aggregate `G-BASELINE`, M1,
  downstream P1-P6 Gates, release, deployment, Beta and GA remain open or in progress.
