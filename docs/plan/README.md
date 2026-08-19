# Cloud Agents 计划与证据总入口

- Canonical root：`hxp0618/cloud-agents/docs/plan`
- Plan status：APPROVED
- Execution status：Platform P0 VERIFIED；P1 IN PROGRESS（P1-A2.2-impl-3 versioned lineage/quota profile remediation implementation/review approved；A2.3 generated registry/profile → append-only PostgreSQL kernel → service/claim/matrix implementation/review approved，full migration closure remains pending）；M1/P2–P6 PAUSED
- Approved by user：2026-08-10
- Migration source：`hxp0618/synara@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- Source plan commit：`4433ebfcff882458822e90d9d79edb076c7ccc91`
- Migration manifest：[`migration-manifest.json`](migration-manifest.json)

## Source of truth

本目录是 Cloud Agents Runtime、公共 Go Control Plane、Worker/Supervisor、Synara/T3 consumer 集成计划与
Gate evidence 的唯一计划根。后续不再以 Synara 私有仓中的计划副本推进公共平台状态。

解释顺序：

1. 已接受的 [`ADR-0006`](adr/0006-public-cloud-agents-platform.md) 至
   [`ADR-0014`](adr/0014-p1-lineage-quota-profile-v3.md)；
2. [`cloud-agents-platform/01`–`06`](cloud-agents-platform/README.md)；
3. [`Synara × T3 总架构`](synara-t3-cloud-agent-integration-architecture.md)；
4. `legacy/` 历史计划；
5. `references/` 冻结参考合同；
6. 代码现状。

## 当前计划

| 文档                                                                                                     | 作用                                            |
| -------------------------------------------------------------------------------------------------------- | ----------------------------------------------- |
| [`cloud-agents-platform/README.md`](cloud-agents-platform/README.md)                                     | 公共平台固定追踪入口                            |
| [`01-product-scope-and-authority.md`](cloud-agents-platform/01-product-scope-and-authority.md)           | 产品范围与单一 authority                        |
| [`02-target-architecture.md`](cloud-agents-platform/02-target-architecture.md)                           | Control Plane/Worker/Runtime 目标架构           |
| [`03-public-repository-and-release.md`](cloud-agents-platform/03-public-repository-and-release.md)       | 公共仓、module、制品与 release train            |
| [`04-extraction-and-migration.md`](cloud-agents-platform/04-extraction-and-migration.md)                 | Go CP inventory、迁移与 cutover                 |
| [`05-gates-and-acceptance.md`](cloud-agents-platform/05-gates-and-acceptance.md)                         | Gate、same-bits、安全与验收                     |
| [`06-status-tracker.md`](cloud-agents-platform/06-status-tracker.md)                                     | 决策、阶段、record 与暂停现场                   |
| [`p0/README.md`](p0/README.md)                                                                           | P0 freeze、inventory、baseline 与 provenance    |
| [`p1/README.md`](p1/README.md)                                                                           | P1 foundation 与 dependency review 证据         |
| [`synara-t3-cloud-agent-integration-architecture.md`](synara-t3-cloud-agent-integration-architecture.md) | Runtime + Platform + 双宿主总设计               |
| [`ADR-0005`](adr/0005-cloud-agent-external-runtime-candidate.md)                                         | immutable external Runtime candidate 历史决定   |
| [`ADR-0006`](adr/0006-public-cloud-agents-platform.md)                                                   | 完整公共 Go Control Plane 平台决定              |
| [`ADR-0007`](adr/0007-p1-contract-data-toolchain-foundation.md)                                          | P1 contract/data/toolchain foundation 决定      |
| [`ADR-0008`](adr/0008-p1-postgres-data-kernel.md)                                                        | P1 PostgreSQL data kernel 决定                  |
| [`ADR-0009`](adr/0009-p1-migration-bundle-runner.md)                                                     | P1 migration bundle/runner/trust 决定           |
| [`ADR-0010`](adr/0010-p1-postgres-projection-contract.md)                                                | P1 PostgreSQL authority/catalog projection 决定 |
| [`ADR-0011`](adr/0011-p1-membership-rbac-contract.md)                                                    | P1 Membership/RBAC authority 决定               |
| [`ADR-0012`](adr/0012-p1-versioned-lineage-quota-profile.md)                                             | P1 versioned lineage/quota profile 决定         |
| [`ADR-0013`](adr/0013-p1-durable-coordination-contract.md)                                               | P1 durable coordination registry/state 决定     |
| [`ADR-0014`](adr/0014-p1-lineage-quota-profile-v3.md)                                                    | P1 lineage/quota profile v3 决定                |

## 历史与参考

- `legacy/` 保存迁移前的方向评估、快速供给、SDK、Go CP 和 sandbox 计划；它们是历史输入，不覆盖当前
  authority/Gate。
- `references/contracts/` 保存总架构直接依赖的 Synara Provider Host、Runtime Event 与 Worker protocol
  参考版本；公共 contract 完成后由生成/验证的公共 schema 取代。
- 迁移时未复制 Stage 4–8 报告、私有环境配置或其他 Synara 产品文档；历史计划中的此类引用固定到来源
  commit 的 GitHub URL。

## 执行边界

P0 已由当前 `G-INVENTORY` R3 与 `G-BASELINE-P0` R3 两个 independently reviewed closure record 完成；
P1-A2.1b-impl-3 已由 `401206a` 完成；A2.2-impl-1 catalog contract 与 impl-2 data/read evaluator 已分别由
`f988e45`、`e36e1cf` 完成；A2.2-impl-3 mutation/service/matrix 已由 `de36ca3` 固定，`350b53c` 关闭
failed/unknown commit 的非零结果缺口，`afe6cb2 → 1ff7713 → 2dc443d` 再关闭 Membership/RoleBinding
admission authority、迁移闭包与当前 source-bound supply metadata。Independent implementation review 随后发现
Go 与 direct PostgreSQL 的 subject issuer language 不一致；append-only `000006` remediation candidate 已通过本地
PG15/16/17 matrix，但按冻结 ADR-0010 v1 whole-bundle 公式会使 lineage index 超出 16 MiB。用户已批准
[`ADR-0012`](adr/0012-p1-versioned-lineage-quota-profile.md) 的显式 v2 profile 方向；v1 历史兼容、v2
生成/绑定已由 `cd64dee` 提交并推送。首次独立复核发现 v1 显式空 profile 的降级边界，`f731c6b`
已 fail closed 修复并由 `610b1ab` 刷新 source-bound metadata；第二轮 `gpt-5.6-sol` 独立复核返回
`APPROVE, P0=0/P1=0/P2=0`，只关闭 A2.2-impl-3 remediation 的固定源码 implementation/review 层，详见
[`independent review`](p1/versioned-lineage-quota-profile-independent-review-20260818.md)。用户已于 2026-08-19
批准 A2.3 的 generated contract registry、closed state machines 与三切片顺序；`ff9ea33`/`a9826e4` 已固定前两
切片，`59ec260` 已固定 generated Go profile、append-only `000008` service/claim 与 PG15/16/17 normal/race/fault
matrix。用户又明确批准 ADR-0014 的 generated-manifest v3 profile 方向。独立审查对当时快照返回
`NOT APPROVE, P0=0/P1=1/P2=0`：quota/profile 与 append-only kernel 分项通过，但 generated Unicode
`organizationRef` 与 ASCII-only authorization `ScopeRef` 不一致。该 finding 已在后续候选中按 generated
operation-specific identity profile 收窄为 ASCII、最多 128 bytes、`exact_string_no_rewrite`，公共 Unicode
organization reference contract 保持不变；append-only `000009` 同时保留冻结 historical registry/profile pair，并以
versioned v2 service entry 使用 current pair。精确 remediation candidate 随后独立复核返回
`APPROVE, P0=0/P1=0/P2=0`，只关闭 A2.3 generated registry/profile、append-only kernel 和
service/claim/matrix 的 implementation/review slice，详见
[`A2.3 remediation independent review`](p1/durable-coordination-v3-remediation-independent-review-20260820.md)。
原 [`A2.3 v3 independent review`](p1/durable-coordination-v3-independent-review-20260819.md) 保留历史 verdict。Full
migration closure 仍 PENDING，HTTP/P2 side effect 不开放，且不关闭任何 Gate。原 entry audit 见
[`A2.3 blocker`](p1/durable-coordination-entry-blocker-20260818.md)。Inventory R2 因
66 个公开 target 的 ABI/authority 方向冲突被
R3 supersede，任何固定旧 decision digest 的下游证据不得继承。P1 仅允许在公共仓实施 contracts、Go/TS
SDK、数据模型、authority 与安全基础，以及 source modules/本地 ephemeral Postgres 验证；不得由 P0/P1
启动结论外推：

- P2–P6 Managed Agent/Host、Standalone 或 Synara/T3 cutover；
- M1 rc.2 或真实 Provider E2E；
- 生产数据库写入、module/image/Release/npm/Registry 发布；
- 部署、Beta、GA；
- 删除任何脏 worktree。

所有新 decision、状态、evidence index 与 closure record 必须先进入本目录，再更新实现。
