# Cloud Agents 公共平台计划与追踪入口

- 状态：APPROVED
- 日期：2026-08-10
- 实施状态：P0 VERIFIED；P1 IN PROGRESS（P1-A2.2-impl-3 versioned lineage/quota profile remediation implementation/review approved；A2.3 direction approved，contract/state-machine registry slice in progress）；M1/P2–P6 PAUSED
- 目标公共仓：`hxp0618/cloud-agents`
- 关联总设计：[`../synara-t3-cloud-agent-integration-architecture.md`](../synara-t3-cloud-agent-integration-architecture.md)
- 关联 ADR：[`ADR-0006`](../adr/0006-public-cloud-agents-platform.md)～[`ADR-0013`](../adr/0013-p1-durable-coordination-contract.md)

## 固定追踪根

本目录是后续 Cloud Agents Platform 计划、决策、阶段状态和 Gate evidence 的固定追踪根。后续不得另建一份
竞争性的 Control Plane 主计划；新设计先更新本目录的对应编号文档与 `06-status-tracker.md`，再同步总设计。
目录迁移、重命名或 source-of-truth 顺序变化必须用 ADR 记录。

## 一句话目标

`hxp0618/cloud-agents` 成为一个可独立部署的完整公共 Cloud Agents 平台：同一公共仓同时拥有 Portable
Runtime、Go Control Plane、Worker/Supervisor、公共协议与 SDK、生产数据模型、部署清单和 conformance；
Synara 与 T3Code 都只消费公共 API/SDK/制品，不再各自维护一份 Cloud Agent 控制面实现。

## 当前执行边界

用户已批准 ADR-0006～ADR-0013 与 D-001～D-040；P0 当前由 `G-INVENTORY` R3 和
`G-BASELINE-P0` R3 关闭，两者均 supersede 各自 R2，因此 P1 Entry 满足；Inventory R3 仅纠正 66 个 legacy
helper/contract target 的公开 ABI 与 authority 方向，Baseline R3 仅把未变化的行为证据重绑定到该前置；旧
decision digest 的下游证据不得继承。P1-A2.1a-impl-1 strict projection contract/fixture 已由 `b36f45a`
完成；P1-A2.1a-impl-2 PG15/16/17 adapters 与本地矩阵已由 `e2541c5` / `a0eac37` 完成；
P1-A2.1b-impl-3 已由 `401206a` 完成；A2.2-impl-1 contract/catalog 与 impl-2 data/read evaluator 已由
`f988e45`、`e36e1cf` 完成，production mutation/catalog publication 仍 fail closed。当前只进入
P1-A2.2-impl-3 mutation/service/review；independent review 发现 Go/direct PostgreSQL subject issuer language
不一致。Append-only `000006` remediation candidate 的本地 PG15/16/17 matrix 已通过，但 frozen ADR-0010
v1 whole-bundle reservation 会超过 16 MiB lineage-index maximum。用户已批准 ADR-0012 的显式 v2
lineage/quota profile；`cd64dee` 已落盘实现，`77de97e` 又补齐 stored admission replay 的 profile-aware
checkpoint ceiling，`94aef60` 固定 follow-up evidence，`f7baf95` 修复 signed-bundle test fixtures，
`04a61af` 记录全量 migration fixture closure，`8d5afdb` 刷新 pre-review source-bound dependency/SBOM
metadata，`261be84` 记录该刷新证据。首次 `gpt-5.6-sol` 复核发现 v1 显式空 profile 降级边界，
`f731c6b` 已修复，`610b1ab` 已刷新 remediation source-bound metadata。第二轮 `gpt-5.6-sol` 独立安全复核
已返回 `APPROVE, P0=0/P1=0/P2=0`，只关闭该 remediation 的固定源码 implementation/review 层；生产
runner/CLI、database mutation 与 immutable Gates 仍未授权。用户已于 2026-08-19 批准 A2.3 的 generated
contract registry、closed state machines 与三切片顺序；当前只推进 registry slice，不创建 `000007`、HTTP/P2
side effect 或关闭 Gate。审查记录见
[`versioned profile independent review`](../p1/versioned-lineage-quota-profile-independent-review-20260818.md)，
历史 contract/state/SQL/service 决策缺口见
[`A2.3 pre-entry blocker`](../p1/durable-coordination-entry-blocker-20260818.md)，精确容量与恢复边界见
[`subject issuer / quota blocker`](../p1/membership-rbac-subject-issuer-quota-blocker-20260817.md)。P1 仍只允许在公共仓实施 contracts、
Go/TS SDK、数据模型、authority 与安全基础，并允许创建三个 source module 与本地 ephemeral Postgres 测试。
仍不授权：

- 修改 Runtime M1 行为、Synara 或 T3Code 源码；
- 提前实施 P2–P6 Managed Agent/Host、Standalone 或宿主 cutover；
- 提交/推送当前未完成的 Codex attestation 修复；
- 重打或替换 immutable `cloud-agent-m1-rc.1`；
- 写入生产数据库，或发布 Go module、container image、Release，或执行部署；
- 恢复真实 Provider E2E。

当前代码现场：

| Surface            | Ref / state                                           | 边界                                  |
| ------------------ | ----------------------------------------------------- | ------------------------------------- |
| Public Runtime     | `cloud-agent-m1-rc.1@49e8cdc6...`                     | immutable prerelease；不是 M1 closure |
| Codex 修复         | `fix/codex-runtime-isolation-null-policy@49e8cdc6...` | 未提交、未推送、无 rc.2               |
| Synara fresh       | `feat/cloud-agent@2f15f7437...`                       | clean、已推送                         |
| T3 fresh           | `feat/cloud-agent@9584a266e...`                       | clean、已推送                         |
| Synara full/native | `codex/cloud-agent-external-runtime@2c50b1eb5...`     | clean、已推送；Go CP 保留             |
| 原 Synara          | `codex/saas-tenancy-user@b86d30b1...`                 | 四个既有 Stage 6 脏文件未触碰         |

## 计划文档索引

| 文件                                                                         | 负责回答                                                      |
| ---------------------------------------------------------------------------- | ------------------------------------------------------------- |
| [`01-product-scope-and-authority.md`](01-product-scope-and-authority.md)     | 什么必须公共；Synara/T3/Runtime/Control Plane 分别拥有什么    |
| [`02-target-architecture.md`](02-target-architecture.md)                     | 可独立部署平台的服务、API、状态机、数据和安全拓扑             |
| [`03-public-repository-and-release.md`](03-public-repository-and-release.md) | 公共仓目录、Go/TS module、制品、部署 profile 和 release train |
| [`04-extraction-and-migration.md`](04-extraction-and-migration.md)           | 现有 Go CP 如何分类、迁移、切换、回滚和删除重复来源           |
| [`05-gates-and-acceptance.md`](05-gates-and-acceptance.md)                   | 每阶段 Gate、same-bits、E2E、供应链与 exposure 口径           |
| [`06-status-tracker.md`](06-status-tracker.md)                               | 决策、阶段状态、open question、DRI 和 evidence 追踪           |
| [`evidence/README.md`](evidence/README.md)                                   | Gate evidence 目录与保存规则                                  |
| [`templates/gate-closure-record.md`](templates/gate-closure-record.md)       | 统一 closure record 模板                                      |

## Source of truth 顺序

发生冲突时按以下顺序解释：

1. 已批准的 ADR-0006～ADR-0012；
2. 本目录的 `01`–`06`；
3. 总设计中标记为 2026-08-10 Revision 的章节；
4. 总设计附录中的历史实现证据；
5. 代码现状。

代码现状不能反向修改 authority 或把未完成实现变成既定设计。

## 产品形态

公共平台支持三种明确模式：

| 模式               | Control Plane authority                   | Turn/Workspace authority | 消费者                            |
| ------------------ | ----------------------------------------- | ------------------------ | --------------------------------- |
| `embedded-runtime` | 无                                        | 宿主                     | 本地 T3/Synara 等直接启动 Runtime |
| `managed-agent`    | 公共 CP 持有 Session/Turn/Execution       | 公共 Worker/Workspace    | Synara native 与其他 Agent GUI    |
| `managed-host`     | 公共 CP 持有 Environment Lease/Generation | lease 内 T3 server       | T3 managed/cloud                  |

三种模式共享 Runtime/Provider 实现，但绝不共享或复制同一个 Turn/Workspace 的写入权威。

## 里程碑总览

下表只是 [`06-status-tracker.md`](06-status-tracker.md) 的只读摘要；发生差异时以 `06` 为准，不在此表
独立推进状态。

| 里程碑      | 目标                                                                | 当前状态    |
| ----------- | ------------------------------------------------------------------- | ----------- |
| M1          | Portable Runtime 七包与 embedded 双宿主；真实 Provider/M1 Gate open | PAUSED      |
| P0          | 公共 Go 代码 inventory、provenance、authority/contract freeze       | VERIFIED    |
| P1          | 公共 CP foundation：auth/project、Postgres/outbox、API/SDK          | IN PROGRESS |
| P2          | Managed Agent Plane：Session/Turn/Execution/Worker/Workspace        | NOT STARTED |
| P3          | Managed Host core：CloudEnvironmentLease + public reference host    | NOT STARTED |
| P4          | Standalone deploy：Compose/Helm、upgrade/rollback/ops               | NOT STARTED |
| P5          | Synara 迁移到公共 CP                                                | NOT STARTED |
| P6          | T3 managed 接入公共 CP                                              | NOT STARTED |
| Platform RC | 同 digest 跨宿主 E2E 与 release closure；前置 P0–P6                 | BLOCKED     |

## 跟踪规则

- 计划状态的唯一可编辑来源是 [`06-status-tracker.md`](06-status-tracker.md)；本页总览只同步展示；
- 每个实施项必须有 DRI、固定 ref、输入/输出、Gate、evidence 路径和回滚边界；
- 状态只允许 `DRAFT`、`PROPOSED`、`APPROVED`、`NOT STARTED`、`PAUSED`、`IN PROGRESS`、`BLOCKED`、
  `VERIFIED`、`INVALIDATED`、`RELEASED`、`RETIRED`；
- `VERIFIED` 必须引用 closure record；`RELEASED` 必须引用 immutable artifact；
- source、test、commit、push、prerelease、deployment、beta、GA 分开记录；
- P0 之后的任何阶段仍须满足本目录 Entry/Gate，并由状态追踪显式推进；不得把 P0 批准外推为发布或部署授权。
