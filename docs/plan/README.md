# Cloud Agents 文档与执行入口

本目录按“活动入口、规范来源、审计材料”分层。当前平台实际状态只认 [06 最终状态汇总](cloud-agents-platform/06-status-tracker.md)；历史、证据和独立复核材料用于追溯，不改变当前状态。产品与文档路由依据 [ADR-0032](adr/0032-infrastructure-admin-delivery-and-document-routing.md)。

## 活动入口

- [04 实施计划](cloud-agents-platform/04-extraction-and-migration.md)：唯一当前工作顺序与实施边界。
- [05 验收与 Gate](cloud-agents-platform/05-gates-and-acceptance.md)：验收标准、Gate 定义和关闭规则。
- [06 最终状态汇总](cloud-agents-platform/06-status-tracker.md)：唯一的实际结果、12/12 矩阵和 Gate 状态来源。
- [Cloud Agents 专项导航](cloud-agents-platform/README.md)：按职责进入 01–07、证据和历史。

## 规范来源

| 目录 | 用途 |
| --- | --- |
| [ADR](adr/README.md) | 架构决策、范围、约束和 authority |
| [references](references/README.md) | 契约、协议和接口原文 |
| [legacy](legacy/README.md) | 旧方案与迁移背景，仅作历史参考 |
| [Synara/T3 专题](synara-t3-cloud-agent-integration-architecture.md) | 后续消费者集成与兼容边界 |

## 审计材料

| 目录 | 用途 |
| --- | --- |
| [evidence](cloud-agents-platform/evidence/README.md) | Gate closure、应用报告和不可变证据索引 |
| [history](cloud-agents-platform/history/README.md) | 旧计划、旧状态和迁移记录 |
| [p0](p0/README.md) | P0 基线、清单和溯源材料 |
| [p1](p1/README.md) | P1 实现、复核和 Gate 审计材料 |
| [standalone](standalone/README.md) | 独立实现/authority/review 记录 |
| [migration-manifest.json](migration-manifest.json) | 文档迁移机器输入，不是当前计划或状态 |

这些目录中的详细文件保留原始运行上下文、状态和机器输入；它们不构成默认阅读路径，也不替代 06。原始 evidence、history、p0、p1、standalone 和 independent review 文件按需读取，不复制到活动文档。

## 维护边界

- 04 只维护计划，05 只维护验收规则，06 只维护最终实际结果，07 只维护 Admin Web 需求与设计。
- 01–03 维护产品范围、目标架构和发布规范；不重复记录测试流水账。
- 规范来源保留决策和接口约束；审计材料保留唯一证据、独立复核和机器输入。
- 不从历史文件推导当前平台状态、下一步或新的 Gate；需要状态时回到 06，需要顺序时回到 04。
