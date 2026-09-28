# Cloud Agents 专项文档导航

本专项把产品/架构/发布规范、实施计划、验收规则、最终结果和 Admin Web 设计分开维护。当前平台实际状态只认 [06 最终状态汇总](06-status-tracker.md)；产品与文档路由见 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)。

## 活动文档

| 文档 | 唯一职责 |
| --- | --- |
| [01 产品范围与 Authority](01-product-scope-and-authority.md) | 产品边界、能力归属和兼容范围 |
| [02 目标架构](02-target-architecture.md) | Workspace、Sandbox、RemoteWorker、调谐、访问和恢复架构 |
| [03 仓库与发布](03-public-repository-and-release.md) | 仓库边界、制品、版本、来源和发布规范 |
| [04 实施计划](04-extraction-and-migration.md) | 当前文档清理、BASE/Anywhere Runtime 实施顺序 |
| [05 验收与 Gate](05-gates-and-acceptance.md) | 验收标准、Gate 定义和关闭规则 |
| [06 最终状态汇总](06-status-tracker.md) | 12/12 矩阵、适用故障结果、清理/安全汇总和 Gate 状态 |
| [07 Admin Web 要求与设计](07-admin-web-requirements-and-design.md) | Admin Web 需求、交互、安全边界和验收 |

## 按需材料

- [evidence 索引](evidence/README.md)：Gate closure、应用报告和 canonical evidence 链接；原始证据不在活动文档复制。
- [history 索引](history/README.md)：旧计划与旧状态；历史事实不代表当前状态。
- [templates 索引](templates/README.md)：Gate 记录模板，不保存实际结果。
- [总入口](../README.md)：全局目录分层和维护边界。
- [ADR 索引](../adr/README.md)、[P0 索引](../p0/README.md)、[P1 索引](../p1/README.md)、[standalone 索引](../standalone/README.md)、[legacy 索引](../legacy/README.md)、[references 索引](../references/README.md)：规范或审计材料分类入口。

详细 evidence/history/p0/p1/standalone/independent-review 文件保留原始内容，只按需访问；它们不提供默认下一步、全局暂停或当前 Cloud Agents 状态。需要当前状态时回到 06，需要顺序时回到 04。
