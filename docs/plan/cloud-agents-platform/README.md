# Cloud Agents 专项文档导航

第一阶段完整交付基础设施＋Admin Web，之后推进用户 CloudAgents 对话。当前决定见 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)。

## 唯一执行入口

从 [04 文档清理与实施计划](04-extraction-and-migration.md) 和 [06 当前状态/下一项](06-status-tracker.md) 开始。本页不维护第二份计划或进度；按任务只读所需专题。

| 文档 | 职责 |
| --- | --- |
| [01 产品范围与 authority](01-product-scope-and-authority.md) | 基础设施/Admin 与用户对话的能力、归属与兼容边界 |
| [02 目标架构](02-target-architecture.md) | Workspace/Sandbox/RemoteWorker、调谐、访问和旧 Lease 衔接 |
| [03 仓库与发布](03-public-repository-and-release.md) | 复用现有模块、独立交付、版本与制品安全 |
| [04 清理与实施计划](04-extraction-and-migration.md) | 文档清理顺序、具体处置清单和 BASE 联合切片 |
| [05 验收](05-gates-and-acceptance.md) | 基础设施＋Admin 的 BASE-READY，以及适用的旧正式 Gate |
| [06 状态与下一项](06-status-tracker.md) | 当前进度、已测边界、实际阻塞与固定证据链接 |
| [07 Admin Web 要求](07-admin-web-requirements-and-design.md) | 完整管理功能、安全交互、Daytona、双语与 UI 验收 |
| [证据规则](evidence/README.md) | 应用/阶段报告与正式 Gate 的不同要求 |

解释顺序、常规自主执行与明确授权边界统一采用 [总入口](../README.md#source-of-truth)。历史状态和迁移资料仅从 06/04 的按需入口查询，不是新任务的默认上下文。
