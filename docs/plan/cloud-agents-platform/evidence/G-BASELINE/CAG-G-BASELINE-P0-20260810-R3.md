# Gate record: `G-BASELINE-P0` / P0 / R3

- Evidence ID：`CAG-G-BASELINE-P0-20260810-R3`
- Record type：PHASE
- Phase / aggregate Gate：`G-BASELINE-P0`；aggregate `G-BASELINE` remains open
- Prerequisite record IDs：`CAG-G-INVENTORY-P0-20260810-R3`
- Status：`VERIFIED`
- DRI：hxp0618（owner）；Codex P0 executor
- Independent reviewer：Codex P1 inventory R3 explorer（复核 prerequisite rebind；沿用 R2 两轮行为证据复核）
- Date：2026-08-10 Asia/Shanghai
- Supersedes：`CAG-G-BASELINE-P0-20260810-R2`（`INVALIDATED`）

## Why R3 supersedes R2

Baseline R2 的固定行为证据、source/tree/archive、normalized digests、测试结果和限制均未改变，也没有重跑任何
产品测试。R2 仅因其固定 prerequisite `CAG-G-INVENTORY-P0-20260810-R2` 已被 R3 发现 target authority
错误并标记为 `INVALIDATED`，不能继续作为有效 Gate record。Baseline R3 将完全相同的行为证据重新绑定到
已验证的 `CAG-G-INVENTORY-P0-20260810-R3`；它不修饰、不提升、也不扩大 R2 的结论。

Supporting Inventory identity：

- Inventory record：`CAG-G-INVENTORY-P0-20260810-R3`
- Inventory evidence commit/tree：`5209ea09da3f4f092886dc0a36d1bf2974958438` /
  `d13c78d61baf5dfa81491bd9d32c65295a242665`
- Inventory decision SHA-256：`24a7f918636b7f0baafaa6c99ff1c04c9b8ad10163cbb568fd311718bb9342ee`
- Rebinding effect：prerequisite replacement only；baseline executions and normalized records unchanged

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
| valid fixed inventory prerequisite                           | PASS                                  | `CAG-G-INVENTORY-P0-20260810-R3`；decision SHA `24a7f918...`                                                          |
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
mean that the four Synara concurrency assertions passed. R3's prerequisite replacement does not convert any `NOT RUN`,
known failure, retention boundary, spec oracle, or characterization-only result into a stronger result.

## Commands / evidence

No product tests were re-run for R3. The following commands and results are the unchanged R2 evidence bound to baseline
evidence commit `66e2f127...`.

Local evidence-commit audit：

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-baseline-evidence.mjs
```

At evidence commit `66e2f127...`, audit v3 exited 0 and reported three Linux execution records, Synara
`EXECUTED_WITH_KNOWN_PRECONDITION_FAILURES`, T3 `PASS_CHARACTERIZATION_ONLY`, Runtime `PASS`, real Provider
`NOT_RUN`, and aggregate Gate `NOT_CLAIMED`. Runtime's earlier cloud execution ran the then-current audit v2；audit
v3 is an evidence/Git-object audit and was intentionally completed locally under D-024 rather than re-running product
tests on the cloud host.

Synara raw evidence verification：

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
6. No failure was waived, no fixed source was patched, and no execution was repeated or reclassified for R3.
7. Real Provider remains `NOT_RUN`；this record is not real Codex/Claude Turn, workspace mutation, checkpoint,
   deployment, release, or M1 acceptance evidence.

## Rollback / cleanup evidence

- All fixed archive materializations matched their source/archive content after tests, excluding dependency trees and
  explicitly recorded generated cache files.
- Synara temporary PostgreSQL `execution_test_*` schemas remaining：0.
- No real Provider, credential, production database, endpoint, grant, workload or volume was created.
- R3 created only a prerequisite-rebinding record；it did not materialize archives, run tests, or change runtime state.

## Invalidation

This record becomes `INVALIDATED` if `CAG-G-INVENTORY-P0-20260810-R3` is invalidated, or if any fixed source
ref/tree/archive, fixture commit/tree/blob, baseline evidence commit/tree, normalized record digest, Synara evidence
index, or baseline audit semantics changes. Loss or mismatch of the only raw evidence copy also invalidates replayability
until the same fixed source is re-characterized and a superseding record is created.

R2 cannot regain validity through this record：any downstream evidence that names
`CAG-G-BASELINE-P0-20260810-R2` or Inventory R2 as its fixed prerequisite must be regenerated or explicitly rebound to
R3. Downstream invalidation targets remain P1 entry and every P1–P6 record that relies on these before-side mechanisms
or reference-host oracles. `G-BASELINE-M1` has an independent input set and remains not started；aggregate
`G-BASELINE` cannot close until both phase records are valid.

## Sign-off

- DRI conclusion：the unchanged fixed-source Runtime/Synara/T3 characterization and greenfield reference-host oracle
  remain complete for P0 when rebound to valid Inventory R3.
- Reviewer conclusion：R3 carries forward only the behavior evidence and exact limitations independently reviewed for
  R2；it makes no claim that tests were rerun or that any known/not-run boundary changed.
- Closure decision：`G-BASELINE-P0 = VERIFIED`。Together with valid Inventory R3 this restores the Platform P0
  prerequisite chain, but does not close `G-BASELINE-M1` or aggregate `G-BASELINE`, and does not authorize M1, real
  Provider validation, release, deployment, Beta or GA.
