# ADR-0032: 基础设施与 Admin Web 共同交付，收敛文档执行入口

- Date: 2026-09-05
- Decision ID: D-055
- Status: Accepted for the user's product-boundary clarification and documentation consolidation; implementation details remain a plan.
- Scope: 当前 Cloud Agents 项目及现有文档 worktree；不修改其他项目、全局 skills、现有 Goal 或自动化。

## 用户决定

用户进一步明确：

> 基础设施和 admin web 是一起的，我们要区分用户对话和 admin web 的界面，所以是先做完基础设施和 admin web。

随后要求继续检查、补全文档、删除不必要文档，并形成可据以清理和执行的计划。

## 决定与取代范围

1. 第一阶段的交付对象是“基础设施＋完整 Admin Web”：长期 Workspace、通用 Sandbox、客户节点接入，以及对应配置、监控、维护、权限和审计界面。Admin Web 不是可选附件或后置工程。
2. 第二阶段才是用户 CloudAgents 对话产品。现有用户对话功能保留兼容，但新增对话功能不驱动第一阶段顺序。
3. 一个面向管理员的基础设施能力只有在真实后端、对应 Admin 操作/状态/失败恢复和相关验证都完成后，才可标为交付。CLI/SDK 的 no-Agent 实测不替代 Admin Web 验收；no-Agent 指不依赖对话/Provider，不指没有管理界面。
4. 取代 ADR-0031 中“Admin 配套”可能被解释为可后补的含义；保留其 Workspace/计算生命周期分离、节点/执行 Worker 区分、用户内容权限和后续消费者边界。既有 ADR 的原文与固定证据不改写。
5. 文档仍位于 `docs/plan`，不另建主计划：04 维护文档收口与产品实施顺序，06 维护当前状态，01/02/03/05/07 分别维护专项规范；README 和 CLAUDE 只导航。旧执行叙事移出默认读取路径，必要历史记录保留按需入口。
6. 删除经引用/生成依赖检查确认可替代的重复说明；保留法律/来源材料、版本契约、固定 Gate/评审/E2E 证据。删文档不能被用来绕过仍有效的约束。生成器绑定的文档若要退役，须另做版本化依赖迁移，不在本轮删除。
7. 用户指出旧 M1～M4 提示词仍引用已改写的 07 §15，现修正为固定验收路由：原任务使用 ADMIN-WEB-V1，新底座使用 BASE-READY 与 BASE-ADMIN-V1。旧真实 Codex/Claude 验收不移除，新 Workspace/Sandbox/RemoteWorker 不自动追加到旧任务。平台主线仍为 BASE；范围迁移须明确作用于目标任务，本次只记录文档规则和可供用户应用的提示词，不自动修改其他任务或 Goal。

## 授权与恢复

用户曾撤销上次合并，随后完成文档复核并明确要求“再次确认目标一致吗，一致的我们开始合并代码”。本次核对一致后，允许把已有 worktree 的文档修订及其必要的 Git 提交集成到当前 `codex/cloud-agents-platform-p0`；这是新的明确合并授权，不复用已撤销的旧授权。集成不包含运行代码变更，不授权 push、部署发布或修改其他任务。重叠原草稿先备份，无关 dirty work、并行代码改动、旧提交和 stash 保留。

本文不授权基础设施代码实施、真实客户节点接入、生产数据库写入、module/image/Release/npm/Registry 发布、部署、Beta/GA、删除脏 worktree 或正式 Gate closure。后续同范围明确实现任务无需重复确认已确定的产品边界；独立审批检查点仍须满足。

Admin Cleanup 的资源名称/generation 确认、危险操作影响说明、独立鉴权、Daytona 固定基线、中英文与可访问性要求不减。内容接口仅授予对应用户，不因 Admin 完整交付而开放用户对话、源码、文件或 Secret 的管理员读取权限。

执行计划见 [04](../cloud-agents-platform/04-extraction-and-migration.md)，状态见 [06](../cloud-agents-platform/06-status-tracker.md)。
