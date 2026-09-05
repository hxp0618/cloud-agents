# 底座优先文档对齐记录（2026-09-05）

- 类型：文档整理 / 静态核对；non-Gate。
- 输入源码：`40812b8945c8b53a8d71b6bc7ef7c757527bcaea`；同时保留并纳入当前 checkout 已有的 11 份文档草稿。
- 工作分支：`codex/foundation-first-docs-20260905`；目标分支：`codex/cloud-agents-platform-p0`。
- 授权：用户要求整理当前项目全部相关文档，在 worktree 调整后合并当前分支；不包含代码实施、跨仓改动、生产数据、部署或发布。

## 结果与阅读入口

[ADR-0031](../../adr/0031-foundation-first-cloud-workspace-platform.md) 记录产品优先级：
长期 Workspace + 通用 Sandbox + 客户节点接入；Admin Web 随底座切片配套，BASE-READY 后推进用户 CloudAgents。

| 内容 | 唯一维护位置 |
| --- | --- |
| 产品/应用/管理员 authority | [01](../01-product-scope-and-authority.md) |
| 生命周期、组件与既有实现衔接 | [02](../02-target-architecture.md) |
| 仓库、独立底座交付与既有发布治理 | [03](../03-public-repository-and-release.md) |
| BASE-M0～M5 → APP-M1 顺序 | [04](../04-extraction-and-migration.md) |
| no-Agent BASE-READY 与旧 Gate 的边界 | [05](../05-gates-and-acceptance.md) |
| 当前阶段状态与历史固定记录 | [06](../06-status-tracker.md) |
| 随底座交付的 Admin、安全交互与视觉验收 | [07](../07-admin-web-requirements-and-design.md) |

根 README、CLAUDE、贡献/安全/RC 文档、HTML 参考、Synara/T3 消费者设计及相关组件 README 同步引用上述入口，不另建主计划。
修正了旧 P1 暂停状态、未生成 Proto、无 mTLS/JWKS、仅内存内核等历史描述被误读为当前全仓事实的问题。
HTML 不再维持冲突的 V1/V2/V3 顺序、全部技术已接受标记、首期完整 User Web 或新增一套仓库/消息系统的默认要求。

## 覆盖与保留

整理时对固定 worktree 的 372 份 Markdown/HTML 文件分类：

- 40 份活动文档：其中 30 份调整；其余通用 Runtime 包、JSON 安全原语、fixture 和 Gate 模板已符合范围，保留。
- 26 份已有 ADR、304 份历史计划/固定证据/来源与许可记录：保持内容，不重写历史结论或证据字节。
- 2 份应用 HTML 入口：属于实现文件，不改动。
- 本记录是新增的第 31 份文档变更；未统计合并目标分支同时新增的其他任务文档，也未将它们覆盖。

已有 Lease API/卷清理语义不因文档改变；新的长期 Workspace 需显式采用/迁移旧卷。
保留生产数据库写入、部署发布、脏 worktree 删除及正式 Gate closure 的明确批准要求。
保留 Cleanup 的资源名称与 generation 确认，并明确它只约束实际产品 Cleanup 交互，不泛化为每次开发确认。
保留固定 Daytona 视觉基线、中英文、权限隔离和管理员不读取用户内容/Secret 的要求。
未放宽旧 Gate 的退出或签署要求；新 BASE 阶段记录也不自动关闭旧 Gate。

## 检查及证据边界

- 文档 diff 空白检查、活动文档本地链接/锚点检查，以及 HTML 标签嵌套、ID 唯一性检查通过。
- 改动仅限 Markdown/架构 HTML；既有 ADR、SQL、schema、生成物、代码、固定 Gate/E2E 证据未由本任务修改。
- 逐文件 SHA-256 核对确认原 checkout 的 11 份草稿与导入版本一致，合并前仍需复核并保留恢复备份。
- 未运行 OpenSandbox PoC、Go/前端运行时测试、真实节点接入、Provider E2E、部署或发布；不形成 BASE/Gate closure。
- BASE 阶段尚无新完成证据，状态保持未验收；下一项实施是 [BASE-M0](../04-extraction-and-migration.md#0-当前实施顺序底座先行)。

复核基础命令（在文档工作分支）：

```sh
git diff --check
git diff --name-only 40812b8945c8b53a8d71b6bc7ef7c757527bcaea
rg --files --hidden -g '*.md' -g '*.html' -g '!node_modules' -g '!.git' -g '!.tmp'
```
