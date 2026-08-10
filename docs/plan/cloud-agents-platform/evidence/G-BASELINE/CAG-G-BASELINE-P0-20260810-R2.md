# Gate record: `G-BASELINE-P0` / P0 / R2

- Evidence ID：`CAG-G-BASELINE-P0-20260810-R2`
- Record type：PHASE
- Phase / aggregate Gate：`G-BASELINE-P0`；aggregate `G-BASELINE` remains open
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R2`
- Status：`VERIFIED`
- DRI：hxp0618（owner）；Codex P0 executor
- Independent reviewer：Codex P0 baseline final-review explorer（两轮只读复核）
- Date：2026-08-10 Asia/Shanghai
- Supersedes：`CAG-G-BASELINE-P0-20260810-R1`（`INVALIDATED`）

## Fixed inputs

- `cloud-agents` baseline evidence commit：`66e2f127b60ccdb94e8e9087e126dbdb5d121ad7`
- Evidence commit tree：`a26acd36ab77f1be0e9a007786d85070affc6276`
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
- Normalized execution record SHA-256：
  - Synara：`a3783d9132620df822271f9c18915777f256c49986f47df7566a46f8ec63b93c`
  - T3：`d2fcd7c26bcb8ac1f9897096066f639a0460bfa038b6de8e943933a96e9280ee`
  - Runtime：`1a94f7903dee9a22e10f0cda44ea815e4ff5d6658bb9742da25eb8012ecdbc48`
- Synara raw evidence index SHA-256：`4dab2c69c991bbad6b7855e68901679e8086b61d1b199426a4c6417544f78f3f`
- Baseline audit v3 SHA-256：`06aded724479b87513b9d14df6728f0053e6f03c485739805b9ac53625a57a8c`
- Toolchains：local Node 24.13.1 / Bun 1.3.14；Linux Node 24.13.1 / Bun baseline 1.3.14 /
  pnpm 11.10.0 / Go 1.26.5 / PostgreSQL 15.18 UTF8
- Execution policy：D-024 local-first / cloud-final
- Deployment profile：none；no Provider credentials, publication, deployment, database migration, workload, endpoint,
  grant or production writer

## Exit criteria mapping

| Criterion                                                    | Result                                | Evidence                                                                                                              |
| ------------------------------------------------------------ | ------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| exact fixed refs, trees and archive identities               | PASS                                  | three normalized records；fresh archive/file/mode/link/shape checks                                                   |
| complete Protocol 2.2/2.3 golden and negative corpus         | PASS                                  | Git-bound `p0-protocol-golden-v3`；29 commands、7 message variants、4 correlation negatives、4 Stop outcomes          |
| Synara legacy mechanisms actually characterized              | PASS WITH KNOWN PRECONDITION FAILURES | 138 focused PASS；broad 934 PASS/1 precondition failure；4 concurrency cases stopped before assertions                |
| T3 embedded authority/auth/proof/allocation characterization | PASS                                  | main frozen install；7 files / 30 tests；5 targeted typechecks                                                        |
| T3 Cloud Agents bridge characterization                      | PASS                                  | feature frozen install；5 files / 39 tests；server typecheck                                                          |
| current Runtime/golden corpus characterization               | PASS                                  | Protocol 13、testkit 5、Runtime 33、three typechecks、format/lint and evidence audit                                  |
| greenfield Managed Host reference baseline                   | PASS AS SPEC/NEGATIVE ORACLE          | Git-bound lifecycle trace covers provision/failure/reap/fencing/idempotency/pairing/restart/orphan/resource lifecycle |
| normalized and raw evidence identity                         | PASS WITH RETENTION BOUNDARY          | Git records bind hashes；Synara 21-entry index independently verified 21/21；T3/Runtime log indexes verified          |
| real Codex/Claude and real T3 workspace/checkpoint behavior  | NOT RUN                               | belongs to `G-BASELINE-M1`；M1 remains paused                                                                         |
| production Managed Host create→ready→terminate               | NOT RUN                               | belongs to P3 implementation and `G-MANAGED-HOST`                                                                     |

`PASS WITH KNOWN PRECONDITION FAILURES` means that the frozen-source behavior was faithfully captured. It does **not**
mean that the four Synara concurrency assertions passed.

## Commands / evidence

Local evidence-commit audit:

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-baseline-evidence.mjs
```

At evidence commit `66e2f127...`, audit v3 exited 0 and reported three Linux execution records, Synara
`EXECUTED_WITH_KNOWN_PRECONDITION_FAILURES`, T3 `PASS_CHARACTERIZATION_ONLY`, Runtime `PASS`, real Provider
`NOT_RUN`, and aggregate Gate `NOT_CLAIMED`. Runtime's earlier cloud execution ran the then-current audit v2; audit
v3 is an evidence/Git-object audit and was intentionally completed locally under D-024 rather than re-running product
tests on the cloud host.

Synara raw evidence verification:

```bash
cd /root/cloud-agents-p0-evidence/synara
sha256sum -c evidence-index.sha256
```

The independently repeated check returned 21/21 `OK`. T3 and Runtime retain their command/result and log SHA indexes
at `/root/cloud-agents-p0-evidence/t3` and `/root/cloud-agents-p0-evidence/runtime-current`.

## Failures, retries, and restrictions

1. Synara's fixed source lacks `packages/contracts/src/providerCapabilityCatalog.json`. The broad suite reached 934
   passes before one test stopped on that precondition；three selected concurrency runs stopped before their target
   assertions for the same reason.
2. The tenant-pinned worker test stopped before claim semantics because fixture `plan_code=test` was absent from
   `saas_plans`, violating `fk_tenants_plan_code`.
3. The suspend-completion substitute only characterizes single-winner/fencing and is not evidence for explicit resume,
   Recovery Bundle receipt/provider cursor binding or replay-once.
4. T3 dependency installation encountered official-registry timeouts and ineffective mirror attempts. The record keeps
   every retry；only the final exit-zero frozen/ignore-scripts installs determine PASS.
5. Raw logs remain on one operator-selected Linux host. Git-bound normalized records and indexes make content identity
   tamper-evident, but this record does not claim permanent or publication-grade artifact retention.
6. No failure was waived, and no frozen source was patched to turn a precondition failure into a passing baseline.

## Rollback / cleanup evidence

- All fixed archive materializations matched their source/archive content after tests, excluding dependency trees and
  explicitly recorded generated cache files.
- Synara temporary PostgreSQL `execution_test_*` schemas remaining：0.
- No real Provider, credential, production database, endpoint, grant, workload or volume was created.
- Existing unrelated `.idea/**` staged state and `.gitignore`/`.idea/misc.xml` worktree changes retained their prior
  hashes and were excluded from the evidence commit.

## Invalidation

This record becomes `INVALIDATED` if any fixed source ref/tree/archive, fixture commit/tree/blob, evidence commit/tree,
normalized record digest, Synara evidence index, or baseline audit semantics changes. Loss or mismatch of the only raw
evidence copy also invalidates replayability until the same fixed source is re-characterized and a superseding record is
created.

Downstream invalidation targets：P1 entry and every P1–P6 record that relies on these before-side mechanisms or
reference-host oracles. `G-BASELINE-M1` has an independent input set and remains not started；aggregate `G-BASELINE`
cannot close until both phase records are valid.

## Sign-off

- DRI conclusion：fixed-source Runtime/Synara/T3 characterization and the greenfield reference-host oracle are
  complete for the P0 phase boundary.
- Reviewer conclusion：after adding and independently verifying the 21-entry Synara evidence index, no P0/P1 blocker
  remains；the phase may be `VERIFIED` with the stated precondition and raw-retention boundaries.
- Closure decision：`G-BASELINE-P0 = VERIFIED`。This closes Platform P0 together with `G-INVENTORY`, but does not
  close `G-BASELINE-M1` or aggregate `G-BASELINE`, and does not authorize M1, release, deployment, Beta or GA.
