# ADR-0031: 底座先行，CloudAgents 作为上层消费者

- Date: 2026-09-05
- Decision ID: D-054
- Status: Accepted for product boundary and delivery priority; detailed implementation plan proposed
- Scope: 当前 Cloud Agents 仓库文档的整理、修复与对齐；用户明确授权在独立 worktree 调整后合并回当前分支。本次不实施代码、部署或迁移数据。

## 批准依据

用户明确要求：

> 底座是长期云工作区＋通用 Sandbox 基础设施＋客户节点接入平台，完成后继续做用户 cloudagents。

随后要求按此边界重新整理架构和实施顺序，并指出 Admin Web 页面可作为配套措施。
本 ADR 记录已明确的产品边界和优先级；编号文档中的技术拆解是本次形成的设计方案，不能把它或
HTML 全文标成已经逐项批准、实现或验收。

## 决定

1. 首要产品是可独立使用的基础设施底座：长期 Workspace、通用 Sandbox、客户节点接入，
   以及让它们可安全运行、恢复和维护的 API/SDK/CLI、存储、访问、调度、身份、策略和运维。
2. 用户侧 CloudAgents 在底座就绪后继续建设。Agent Session/Turn/Execution、Approval、历史、
   Provider cursor 和 Agent Checkpoint 属于应用层；底座不得依赖 Codex/Claude 凭据或对话才能使用。
3. Admin Web 是底座的配套运维入口，与每个基础能力切片同步交付。页面必须消费真实后端状态，
   但页面数量、视觉进展或策略 CRUD 不替代基础设施执行与恢复验收。
4. 已有公共 Go CP、PostgreSQL/RLS/RBAC、SDK、幂等、generation/fencing 和 Runtime 优先复用；
   现有 Agent 能力保留兼容，不为本次重排删除、重写或跨仓迁移。需要时可作回归负载，但不作为底座验收前置。
5. Workspace 数据生命周期与 Sandbox/Lease 计算生命周期分离。节点离线、停止运行实例或清理计算残留，
   都不自动授权删除长期工作区。AgentSession 关闭也不等于 Workspace 删除。
6. 节点级 RemoteWorker 与租约内执行 Worker/SandboxAgent 是不同角色；客户节点主动建立受控连接，
   平台不要求为此开放客户公网 SSH/Worker 入站端口。
7. 底座就绪后再推进用户 CloudAgents；Synara/T3 的既有集成需求保留为后续消费者范围，
   不要求先完成它们才能验收通用底座。

## 取代与保留

本决定仅取代 ADR-0006 及编号计划中以下产品方向和工作排序：

- 把 managed-agent/managed-host 视为底座的全部产品模型；
- 按旧 P2 Managed Agent → P3 Managed Host → P4 Standalone 作为当前建设先后；
- 把全部 Admin Web 延后，或以 ADMIN-M1～ADMIN-M4 单独驱动平台主线；
- 把长期 Workspace 当作一次 Lease 的附属资源，或把现有 Worker 视为客户节点 Agent。

现有三种消费模式、旧 API 兼容、安全合同、单一 writer、不可变制品和原 Gate 历史不删除、不重写。
旧平台完整 RC/Gate 的批准条件也不在本次降低；通用底座本地就绪与该 RC 是不同结论。
详细边界、方案、顺序与验收分别维护在 [01](../cloud-agents-platform/01-product-scope-and-authority.md)、
[02](../cloud-agents-platform/02-target-architecture.md)、[04](../cloud-agents-platform/04-extraction-and-migration.md)、
[05](../cloud-agents-platform/05-gates-and-acceptance.md)，状态只在 [06](../cloud-agents-platform/06-status-tracker.md) 推进。

## 技术方案与参考的边界

- [HTML 基准](../../coding_agent_cloud_infrastructure_design.html) 提供输入，但不整体提升为批准 authority。
  OpenSandbox 是优先验证的执行底座候选；先固定上游版本、契约、许可和真实行为，再决定替换范围。
  不把旧审计或上游文档当作当前能力证明，不预先 fork 或重建一套同类 runtime engine。
- 单 Region 起步；Region/Pool 是资源归属和调度模型，不意味着先拆微服务或实现多 Region active-active。
- 原 HTML 对 gVisor 的 V1/V2、SSH 的 V1/Phase 2 表述不一致，本次已对齐为 BASE-M* 顺序：通用 SSH 在
  BASE-M2，强隔离能力矩阵在 BASE-M4；不支持的 runtime tier 必须拒绝，不能广告为已实现。
  可信 runc 路径不能冒充不可信共享多租户隔离验收。
- 内存无损快照、直接 MicroVM、高密度 Warm Pool、跨 Region 数据复制和完整商业账单是后续增强，
  不替代当前所需的文件系统快照、基本容量准入、用量事实和独立恢复。

## 授权与完成语义

本次授权的是架构/计划重整，不是所有后续阶段的代码、真实节点接入、生产数据、部署或发布授权。
后续收到明确实现任务时，按总计划已有授权规则识别其范围，正常开发和验证不重复索取相同确认；
单独审批检查点仍保留。本文不更新其他任务、Goal 或自动化。

生产数据库写入、module/image/Release/npm/Registry 发布、部署、Beta/GA、脏 worktree 删除和
正式 Gate closure 继续适用原明确批准边界。Admin Cleanup 的资源名称和 generation 确认保持不变，
它是实际产品操作的安全交互，不泛化成每次开发操作都要确认。
