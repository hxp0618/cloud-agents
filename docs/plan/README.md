# Cloud Agents 文档与执行入口

当前平台实际状态只认 [06 当前状态](cloud-agents-platform/06-status-tracker.md)；第一阶段验收证据见 [phase-1 验收](../acceptance/phase-1.md)。产品与文档路由依据 [ADR-0032](adr/0032-infrastructure-admin-delivery-and-document-routing.md)。

## 活动入口

- [04 实施计划](cloud-agents-platform/04-extraction-and-migration.md)：唯一当前工作顺序与实施边界。
- [05 验收与 Gate](cloud-agents-platform/05-gates-and-acceptance.md)：验收标准、Gate 定义和关闭规则。
- [06 当前状态](cloud-agents-platform/06-status-tracker.md)：唯一的实际结果和 Gate 状态来源。
- [Cloud Agents 专项导航](cloud-agents-platform/README.md)：01–07 的职责划分。

## 规范来源

| 目录 | 用途 |
| --- | --- |
| [ADR](adr/README.md) | 架构决策、范围、约束和 authority |
| [references](references/README.md) | 契约、协议和接口原文 |

## 冻结审计记录

[evidence](cloud-agents-platform/evidence/README.md)、[p1](p1/README.md) 与 [standalone](standalone/README.md) 只保留被生成锁或 review digest 按字节绑定的记录，内容不可修改，不提供当前状态或下一步。完整过程记录（P0 基线、旧计划、逐轮报告、原始日志和截图）已在开源整理前归档，不随公开仓库发布。

## 维护边界

- 04 只维护计划，05 只维护验收规则，06 只维护实际结果，07 只维护 Admin Web 需求与设计。
- 不在活动文档追加逐轮命令、运行编号、digest 或修复过程；新的实际结果先形成可复现证据，再更新 06 和验收表。
