# Cloud Agents 计划与证据总入口

- Canonical root：`hxp0618/cloud-agents/docs/plan`
- Plan status：APPROVED
- Execution status：Platform P0 VERIFIED；P1 NOT STARTED（Entry satisfied）；M1/P2–P6 PAUSED
- Approved by user：2026-08-10
- Migration source：`hxp0618/synara@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- Source plan commit：`4433ebfcff882458822e90d9d79edb076c7ccc91`
- Migration manifest：[`migration-manifest.json`](migration-manifest.json)

## Source of truth

本目录是 Cloud Agents Runtime、公共 Go Control Plane、Worker/Supervisor、Synara/T3 consumer 集成计划与
Gate evidence 的唯一计划根。后续不再以 Synara 私有仓中的计划副本推进公共平台状态。

解释顺序：

1. 已接受的 [`ADR-0006`](adr/0006-public-cloud-agents-platform.md)；
2. [`cloud-agents-platform/01`–`06`](cloud-agents-platform/README.md)；
3. [`Synara × T3 总架构`](synara-t3-cloud-agent-integration-architecture.md)；
4. `legacy/` 历史计划；
5. `references/` 冻结参考合同；
6. 代码现状。

## 当前计划

| 文档                                                                                                     | 作用                                          |
| -------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| [`cloud-agents-platform/README.md`](cloud-agents-platform/README.md)                                     | 公共平台固定追踪入口                          |
| [`01-product-scope-and-authority.md`](cloud-agents-platform/01-product-scope-and-authority.md)           | 产品范围与单一 authority                      |
| [`02-target-architecture.md`](cloud-agents-platform/02-target-architecture.md)                           | Control Plane/Worker/Runtime 目标架构         |
| [`03-public-repository-and-release.md`](cloud-agents-platform/03-public-repository-and-release.md)       | 公共仓、module、制品与 release train          |
| [`04-extraction-and-migration.md`](cloud-agents-platform/04-extraction-and-migration.md)                 | Go CP inventory、迁移与 cutover               |
| [`05-gates-and-acceptance.md`](cloud-agents-platform/05-gates-and-acceptance.md)                         | Gate、same-bits、安全与验收                   |
| [`06-status-tracker.md`](cloud-agents-platform/06-status-tracker.md)                                     | 决策、阶段、record 与暂停现场                 |
| [`p0/README.md`](p0/README.md)                                                                           | P0 freeze、inventory、baseline 与 provenance  |
| [`synara-t3-cloud-agent-integration-architecture.md`](synara-t3-cloud-agent-integration-architecture.md) | Runtime + Platform + 双宿主总设计             |
| [`ADR-0005`](adr/0005-cloud-agent-external-runtime-candidate.md)                                         | immutable external Runtime candidate 历史决定 |
| [`ADR-0006`](adr/0006-public-cloud-agents-platform.md)                                                   | 完整公共 Go Control Plane 平台决定            |

## 历史与参考

- `legacy/` 保存迁移前的方向评估、快速供给、SDK、Go CP 和 sandbox 计划；它们是历史输入，不覆盖当前
  authority/Gate。
- `references/contracts/` 保存总架构直接依赖的 Synara Provider Host、Runtime Event 与 Worker protocol
  参考版本；公共 contract 完成后由生成/验证的公共 schema 取代。
- 迁移时未复制 Stage 4–8 报告、私有环境配置或其他 Synara 产品文档；历史计划中的此类引用固定到来源
  commit 的 GitHub URL。

## 执行边界

P0 已按两个 independently reviewed closure record 完成；P1 Entry 已满足但尚未开始实现。P1 仅允许在公共
仓实施 contracts、Go/TS SDK、数据模型、authority 与安全基础；不得由 P0 结论外推：

- P2–P6 Managed Agent/Host、Standalone 或 Synara/T3 cutover；
- M1 rc.2 或真实 Provider E2E；
- 数据库、module、image、Release、npm/Registry；
- 部署、Beta、GA；
- 删除任何脏 worktree。

所有新 decision、状态、evidence index 与 closure record 必须先进入本目录，再更新实现。
