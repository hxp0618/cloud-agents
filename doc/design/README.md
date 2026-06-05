# Cloud Agents 平台设计方案

> 代号 **Polaris**（北极星）—— 一个企业级、统一治理的云端 AI Agent 平台，基于开源项目 [pi (earendil-works)](https://github.com/earendil-works/pi) 构建。
>
> 文档版本：v0.6.2（草案） · 日期：2026-06-05 · [变更日志](#10-变更日志)

> 📎 **与需求调研稿的关系**：本目录（`doc/design/`）是**设计方案**，对"该怎么做"给出一版可落地的提案；第一轮**需求调研与开放问题**见 [`../requirements/cloud-agent-requirements-discovery.md`](../requirements/cloud-agent-requirements-discovery.md)。两者互补——调研稿提出问题，本方案给出一种答案；最终以需求确认后的迭代为准。

---

## 1. 一句话定位

> **"企业版的 Claude Code / Codex"**：对标 codex、claude code 等"本地个人 AI Agent"的能力，但把所有内容（Agent / 模型 / MCP / Skill / 权限 / 沙箱）放到云端**统一治理**——同一项目 / 业务组的成员开箱即用、共享配置、无需重复设置、不依赖本地环境。

它**不是聊天机器人**，而是一个能在隔离沙箱中真正"动手干活"（读写文件、执行命令、调用工具、跑多步任务、派生子 Agent）的 Agent 运行与治理平台。

---

## 2. 为什么基于 pi（复用什么 vs 自建什么）

pi 是一个**极简内核 + 可选生态包**的 AI Agent 工具箱（TypeScript monorepo）：

- ✅ **直接复用（pi 已具备）**：多 LLM 统一接入（`pi-ai`，20+ provider）、Agent 运行时与工具循环（`pi-agent-core`）、三种嵌入方式（SDK / RPC / JSON 流式）、原生 Skill（Agent Skills 标准）、会话树持久化、以及**最关键的治理抓手**——`tool_call` 拦截钩子（可 `{block:true}`）+ RPC 的 `extension_ui_request/response` 往返协议。
- 🧩 **复用 pi 官方/生态包并加治理**：MCP 与 sub-agent 不在 pi 内核，但有**官方可选包**——`pi-mcp-adapter`（v2.8.0，单代理 `mcp` 工具懒加载 MCP server、OAuth、stdio/HTTP）与 `pi-subagents`（v0.27.0，内置 scout/researcher/planner/worker/reviewer/oracle 等、`/run`、`/chain`、`/parallel`、worktree 隔离、递归护栏）。我们**复用它们，再套上企业治理**，而非从零造轮子。
- 🔨 **真正自建（pi 不做、也无现成包）**：企业治理层（RBAC + 中央目录 + 可见域继承/锁定）、隔离层（每 Agent 沙箱 + 出网白名单）、安全评估流水线（用户发布 Skill）、统一 SSE 可观测 + 审计 + 计量、多端接入（Web/桌面/CLI/SDK）。**这才是本平台的核心增量。**

> 详见 [`01-research-and-landscape.md`](./01-research-and-landscape.md)（pi 能力清单 + 同类产品调研 + 差异化）。

---

## 3. 三层信任边界（贯穿全设计的核心约定）

pi 的扩展能力有三种载体，**安全等级与可发布性完全不同**，这是整套权限与安全评估设计的地基：

| 载体 | 本质 | 运行权限 | 谁能创作/发布 |
|---|---|---|---|
| **Extension（扩展）** | 任意 TypeScript，`pi.registerTool` 等（**`pi-mcp-adapter`、`pi-subagents` 亦属此级**） | **完整系统权限**（可执行任意代码） | 仅**平台/管理员**。平台核心能力（权限闸门、MCP、子 Agent、事件上报）都以"平台扩展/受控引入的官方包"形式存在 |
| **MCP Server** | 外部工具服务（stdio/HTTP），经 `pi-mcp-adapter` 接入 | 沙箱内 sidecar，能力受 manifest 约束 | 管理员审批接入；用户贡献需安全评估 |
| **Skill（技能）** | Markdown 指令 + 可选脚本 | **沙箱内**运行，受能力清单（capability manifest）约束 | **用户可创作**；发布到项目/业务组/组织需通过**安全评估流水线** |

> 结论：**用户产出的可发布单元是 Skill（安全、可审、受沙箱约束），绝不是 Extension（任意代码、全权限）。** `pi-mcp-adapter` / `pi-subagents` 这类官方包虽强大，但属 Extension 级 → 由平台/管理员受控引入并治理，不开放给普通用户自由安装。详见 [`06-capabilities-skills-mcp-subagents.md`](./06-capabilities-skills-mcp-subagents.md)。

---

## 4. 设计原则

1. **以 pi 为依赖，不 fork**（MIT、活跃维护）；增量以"平台扩展 + 受控官方包 + 外围服务"实现，必要时向上游回馈。
2. **控制面 / 数据面分离**：治理与编排（大部分服务无状态、可水平扩展；Orchestrator 持有可恢复的短生命周期运行状态）vs. 真正跑 Agent 的隔离沙箱。
3. **能力即目录（Capability-as-Catalog）**：Agent / 模型 / MCP / Skill 全部是带版本、带可见域（私有→项目→业务组→组织）、可继承可锁定的目录资源。
4. **双层防御**：进程内策略闸门（pi `tool_call` 钩子）+ 内核级沙箱（gVisor/microVM + 出网白名单）。
5. **事件溯源即可观测**：一条 append-only 事件流是唯一事实源，SSE 是它的实时投影；审计、计量、安全监控都从同一条流派生。
6. **API-First / 多端等价**：Web 控制台、桌面端、CLI、SDK 共享同一套 REST + SSE 契约。

---

## 5. 关键架构决策摘要

| 决策 | 选择 | 理由 |
|---|---|---|
| 如何嵌入 pi | **RPC 模式为主**（`pi --mode rpc`，长驻子进程，stdin/stdout 行分隔 JSON），SDK `createAgentSession()` 为辅 | 进程级隔离、可双向驱动、天然契合沙箱边界 |
| 权限落点 | pi `tool_call` 钩子 + RPC `extension_ui_request/response` 往返 | pi 唯一原生治理抓手，无需改动内核 |
| MCP / 子 Agent | **复用 `pi-mcp-adapter` / `pi-subagents` 官方包**，外套权限闸门 + 目录治理 + 审计 | 省去自建、契合 pi 生态、未来易随上游演进 |
| 多 LLM | `pi-ai` 之上加**内部 LLM Gateway** | 密钥集中托管（开发者看不到 key）、按角色/项目做模型白名单、成本计量、合规路由 |
| 隔离单元 | **一个 Agent 实例 = 一个沙箱**（非每用户） | 规避 OpenHands V0 多租户"邻居噪声"崩溃的教训 |
| 用户可发布单元 | **Skill**（经安全评估），而非 Extension | 见上文信任边界 |
| 部署形态 | 主：**单企业自托管 / VPC**（内部按业务组/项目多租户）；可选 SaaS 多租户 | 契合"企业统一管理、不依赖本地环境" |

---

## 6. 需求覆盖矩阵

### 6.1 原始需求覆盖

| # | 原始需求 | 主要落点文档 |
|---|---|---|
| 1 | 多 LLM | [04](./04-pi-integration-and-multi-llm.md) |
| 2 | Sub-agent | [06](./06-capabilities-skills-mcp-subagents.md) |
| 3 | 支持 MCP 与 Skill | [06](./06-capabilities-skills-mcp-subagents.md) |
| 4 | 完整页面管理所有功能 | [09](./09-api-clients-and-data-model.md)（控制台信息架构）+ [05](./05-rbac-and-governance.md) |
| 5 | 用户权限体系 | [05](./05-rbac-and-governance.md) |
| 6 | 管理员统一管理默认资源 + 用户自定义 Skill + 发布需安全评估 | [05](./05-rbac-and-governance.md) + [06](./06-capabilities-skills-mcp-subagents.md) |
| 7 | 不只是聊天机器人 | [02](./02-product-requirements.md) + [03](./03-system-architecture.md) |
| 8 | 独立沙箱（每 Agent 一隔离环境） | [07](./07-sandbox-isolation.md) |
| 9 | SSE 事件流 / 全链路可观测 | [08](./08-observability-and-sse.md) |
| 10 | 对标 codex/claude code，但企业统一管理、项目/业务组共享 | [01](./01-research-and-landscape.md)（差异化）+ 全局 |
| 11 | 支持 API 调用、桌面端等多端 | [09](./09-api-clients-and-data-model.md) |

### 6.2 安全与合规

| # | 领域 | 主要落点文档 |
|---|---|---|
| 12 | 安全架构与威胁模型（STRIDE） | [11](./11-security-and-threat-model.md) |
| 13 | 安全测试策略（沙箱逃逸/注入/安审对抗） | [12](./12-testing-strategy.md) |
| 14 | 数据留存与隐私合规（GDPR/脱敏/审计防篡改） | [14](./14-data-retention-and-privacy.md) |
| 15 | 合规框架映射（SOC 2/ISO 27001/等保） | [11 §9](./11-security-and-threat-model.md) |

### 6.3 运维与可靠性

| # | 领域 | 主要落点文档 |
|---|---|---|
| 16 | Day-2 运维手册（故障恢复/备份/升级/告警） | [13](./13-operations-manual.md) |
| 17 | 容量规划（资源估算公式/参考配置） | [18](./18-capacity-planning.md) |
| 18 | 成本优化（Caching/路由/预热池/分层存储） | [17](./17-cost-optimization.md) |
| 19 | 多区域部署（数据主权/DR/合规路由） | [19](./19-multi-region-deployment.md) |

### 6.4 平台工程化

| # | 领域 | 主要落点文档 |
|---|---|---|
| 20 | Prompt 版本管理与 A/B 测试 | [15](./15-prompt-management-and-evaluation.md) |
| 21 | Agent 评估框架与反馈闭环 | [15](./15-prompt-management-and-evaluation.md) |
| 22 | 通知系统（多渠道/审批升级/聚合） | [16](./16-notification-system.md) |
| 23 | GitOps / Configuration as Code | [22](./22-gitops-configuration-as-code.md) |
| 24 | Run 生命周期状态机（统一状态模型） | [21](./21-run-lifecycle-state-machine.md) |
| 25 | 需求追溯矩阵（需求问题→设计决策） | [23](./23-requirements-traceability-matrix.md) |
| 26 | 交付物与外部集成目录 | [24](./24-delivery-and-integration-catalog.md) |
| 27 | IDE 集成（VS Code/JetBrains 插件） | [20](./20-ide-integration-protocol.md) |

---

## 7. 文档导航

| 文档 | 内容 |
|---|---|
| [`01-research-and-landscape.md`](./01-research-and-landscape.md) | pi 能力深挖（得到什么/复用什么/自建什么）、同类产品调研、对比表、差异化定位 |
| [`02-product-requirements.md`](./02-product-requirements.md) | 角色画像、功能需求（FR）与非功能需求（NFR）、用户故事 |
| [`03-system-architecture.md`](./03-system-architecture.md) | 总体架构（控制面/数据面/客户端）、组件图、运行时序、技术栈、部署拓扑 |
| [`04-pi-integration-and-multi-llm.md`](./04-pi-integration-and-multi-llm.md) | 如何驱动 pi（RPC/SDK/JSON）、平台扩展、权限闸门、LLM Gateway、会话 |
| [`05-rbac-and-governance.md`](./05-rbac-and-governance.md) | 组织/业务组/项目/用户层级、角色与权限矩阵、可见域继承与锁定、统一治理、SSO/SCIM |
| [`06-capabilities-skills-mcp-subagents.md`](./06-capabilities-skills-mcp-subagents.md) | 信任模型、Skill 生命周期与安全评估流水线、MCP 接入治理、子 Agent 编排 |
| [`07-sandbox-isolation.md`](./07-sandbox-isolation.md) | 每 Agent 沙箱、Workspace 后端（K8s reconcile `Sandbox` CR）、凭据代理 sidecar（egress+桩换值）、双层防御、生命周期 |
| [`08-observability-and-sse.md`](./08-observability-and-sse.md) | 事件溯源、事件信封与分类、SSE 传输与断点续传、审计、计量 |
| [`09-api-clients-and-data-model.md`](./09-api-clients-and-data-model.md) | REST/SSE API 面、Agent Run API、SDK/CLI/桌面端、控制台信息架构、数据模型 ER |
| [`10-roadmap-and-open-questions.md`](./10-roadmap-and-open-questions.md) | 分阶段路线图、MVP 定义、风险、待决策项（含建议）、成功指标 |
| [`11-security-and-threat-model.md`](./11-security-and-threat-model.md) | 🆕 统一安全架构：信任边界图、STRIDE 威胁模型、攻击面枚举、安全控制矩阵、残余风险、事件响应、SDL |
| [`12-testing-strategy.md`](./12-testing-strategy.md) | 🆕 测试策略：金字塔、单元/集成/E2E 测试用例、安全测试（沙箱逃逸/注入/安审对抗）、兼容性、性能、混沌工程 |
| [`13-operations-manual.md`](./13-operations-manual.md) | 🆕 运维手册：故障恢复（Orchestrator 崩溃/会话同步）、备份 RPO/RTO、沙箱资源治理、滚动升级/drain、健康检查与 SLO 告警 |
| [`14-data-retention-and-privacy.md`](./14-data-retention-and-privacy.md) | 🆕 数据留存与隐私：数据分类与保留期限、GDPR 被遗忘权、跨作用域隔离、审计防篡改（哈希链）、脱敏规则全集、合规映射 |
| [`15-prompt-management-and-evaluation.md`](./15-prompt-management-and-evaluation.md) | 🆕 Prompt 管理与 Agent 评估：Prompt 版本化/A-B 测试/回滚、护栏、Agent 质量评估框架、LLM-as-Judge、用户反馈闭环 |
| [`16-notification-system.md`](./16-notification-system.md) | 🆕 通知系统：多渠道（In-app/Email/Slack/Webhook/PagerDuty）、审批超时升级链、配额/预算/安审通知、Digest 聚合、用户偏好 |
| [`17-cost-optimization.md`](./17-cost-optimization.md) | 🔮 P3+ 成本优化：Prompt Caching、语义缓存、模型分层路由（haiku/sonnet/opus）、沙箱预热池/复用/Spot 实例、存储分层、综合成本模型 |
| [`18-capacity-planning.md`](./18-capacity-planning.md) | 🔮 P3+ 容量规划：单沙箱资源画像、数据面/控制面节点估算公式、Orchestrator 瓶颈分析、自建 vLLM 容量、快速参考卡（POC→企业）|
| [`19-multi-region-deployment.md`](./19-multi-region-deployment.md) | 🔮 P5+ 多区域部署：中心辐射拓扑、数据驻留、跨区域一致性（CP/AP）、DR 策略、合规路由（EU/APAC）、延迟预算 |
| [`20-ide-integration-protocol.md`](./20-ide-integration-protocol.md) | 🔮 P3+ IDE 集成：VS Code/JetBrains 插件架构、OAuth PKCE 认证、对话面板/内联 Diff/审批对话框、本地目录桥接、与 Continue/Cursor 对比 |
| [`21-run-lifecycle-state-machine.md`](./21-run-lifecycle-state-machine.md) | 🆕 Run 生命周期状态机：UML 状态图、11 种状态+15 条转换、各状态合法操作表、计时器、事件映射、UI 映射 |
| [`22-gitops-configuration-as-code.md`](./22-gitops-configuration-as-code.md) | 🆕 GitOps / Configuration as Code：仓库结构、YAML Schema、双向同步架构、冲突裁决、CI Pre-Merge 校验、回滚 |
| [`23-requirements-traceability-matrix.md`](./23-requirements-traceability-matrix.md) | 🆕 需求追溯矩阵：114 条需求开放问题 → 设计决策的逐条对照（15 个需求域全覆盖） |
| [`24-delivery-and-integration-catalog.md`](./24-delivery-and-integration-catalog.md) | 🆕 交付物与集成目录：10 类交付物、L1–L5 五级集成（GitHub/Jira/Slack/CI/知识库）、安全控制、优先级路线图 |

---

## 8. 术语约定（Terminology Conventions）

为确保全文档一致，以下术语有明确约定：

| 术语 | 约定 | 说明 |
|---|---|---|
| **Skill 安审** | 统一使用「安审」指代 Skill 安全评估流水线 | 首提可用「安全评估（安审）」全称；后续统一用「安审」。区别于代码安全审查（PR review）和 MCP 安全审查 |
| **作用域** | RBAC 治理层级（组织→业务组→项目→用户） | 与「可见域」含义不同——见下方名词表 |
| **可见域** | 资源的可见/可用范围（私有→项目→业务组→组织） | 与「作用域」含义不同 |
| **闸门** | `tool_call` 钩子策略执行点（PEP） | 与「网关」（LLM/API Gateway）含义不同 |
| **子 Agent** | 中文正文统一使用「子 Agent」 | 英文正文用「sub-agent」（带连字符）；仅在代码标识符中用「subagent」（如 `subagentId`） |
| **pi-mcp-adapter / pi-subagents 版本** | 文中 `v2.8.0` / `v0.27.0` 为调研时版本（2026-06），实施前需确认最新版本 | 见 [01 §A.2](./01-research-and-landscape.md) 与 [01 §E](./01-research-and-landscape.md) |
| **沙箱 / Sandbox** | 中文正文统一「沙箱」；仅在代码标识符/配置字段中用 `sandbox` | 见 [07](./07-sandbox-isolation.md) |
| **产物 / Artifact / 交付物** | 中文统一「产物」指 Run 生成的文件（Diff/报告/包）；「交付物」指最终输出（PR/报告/API Response）；英文用 `artifact` | 避免三词混用 |
| **熔断 (Circuit Breaker)** | 自动安全机制：达到阈值后自动禁止某 Skill/Agent 继续运行 | 区别于「撤销 (Revoke)」——管理员手动操作 |
| **撤销 (Revoke)** | 管理员手动操作：将 Skill 从可见域中移除并通知使用者 | 区别于「熔断」——自动触发 |
| **GitOps** | 通过 Git 仓库 + PR/MR 驱动平台配置变更的工作流 | 见 [22](./22-gitops-configuration-as-code.md) |
| **Configuration as Code (CaC)** | 平台资源（Agent/MCP/Skill/策略）声明式 YAML 定义 | 见 [22](./22-gitops-configuration-as-code.md) |

---

## 9. 名词表（Glossary）

| 术语 | 含义 |
|---|---|
| **Agent 定义 (AgentDefinition)** | 一份可复用的 Agent 配置模板：系统提示词、可用模型、工具/Skill/MCP 集合、权限画像、沙箱画像 |
| **Agent 运行 (Run / AgentInstance)** | 一次具体执行，绑定一个沙箱 + 一个会话树 |
| **会话 (Session)** | pi 的 JSONL 树状对话状态，支持 fork/clone/branch |
| **平台扩展 (Platform Extension)** | 平台注入每个 pi 进程的核心 TS 扩展，承载权限闸门 / 事件上报 / Gateway provider，并受控装载 `pi-mcp-adapter`、`pi-subagents` |
| **能力清单 (Capability Manifest)** | Skill/MCP 声明其所需工具、路径、出网域名、密钥等的清单；沙箱据此做能力约束（capability-based enforcement） |
| **可见域 (Visibility Scope)** | 资源的可见/可用范围：私有 → 项目 → 业务组 → 组织 |
| **作用域 (Governance Scope)** | RBAC 治理层级：组织 → 业务组 → 项目 → 用户；决定权限策略的作用边界 |
| **托管锁定 (Managed/Locked)** | 上级设定且下级不可覆盖的配置（借鉴 OpenCode MDM 语义） |
| **LLM Gateway** | 内部统一大模型网关，集中托管密钥、做模型白名单与成本计量 |
| **Run 状态机** | Agent Run 从 `created` 到终态（`completed/failed/timeout/cancelled/crashed/rejected`）的完整状态模型 | 见 [21](./21-run-lifecycle-state-machine.md) |
| **GitOps 同步** | Git 仓库 YAML 变更 → Config Sync Service → Catalog 的自动同步链路 | 见 [22](./22-gitops-configuration-as-code.md) |

---

> 📌 **如何阅读**：决策者先看本 README + [01 差异化](./01-research-and-landscape.md) + [10 路线图](./10-roadmap-and-open-questions.md)；架构师从 [03](./03-system-architecture.md) 入手并下钻 04–08 + [21 状态机](./21-run-lifecycle-state-machine.md) + [19 多区域](./19-multi-region-deployment.md)；安全评审看 [11](./11-security-and-threat-model.md)（威胁模型）+ [12](./12-testing-strategy.md)（安全测试）+ [14](./14-data-retention-and-privacy.md)（隐私合规）；产品/项目经理读 [02](./02-product-requirements.md)（含量化 NFR）+ [09 控制台信息架构](./09-api-clients-and-data-model.md) + [15](./15-prompt-management-and-evaluation.md)（评估与反馈）+ [23 需求追溯](./23-requirements-traceability-matrix.md)；SRE/DevOps 看 [13](./13-operations-manual.md)（运维）+ [16](./16-notification-system.md)（通知基建）+ [17](./17-cost-optimization.md)（成本优化）+ [18](./18-capacity-planning.md)（容量规划）；QA/SET 看 [12](./12-testing-strategy.md)；IDE 插件开发者看 [20](./20-ide-integration-protocol.md)；平台工程师看 [22](./22-gitops-configuration-as-code.md)（GitOps）+ [24](./24-delivery-and-integration-catalog.md)（集成目录）。

---

## 10. 变更日志

> 详细变更见 [CHANGELOG.md](./CHANGELOG.md)。

| 版本 | 日期 | 变更 |
|---|---|---|
| **v0.6.2** | 2026-06-05 | 🔧 沙箱与凭据层设计增补（无新增文档，详见 [CHANGELOG](./CHANGELOG.md)）：[07](./07-sandbox-isolation.md) K8s 后端改为 reconcile 上游 `Sandbox` CR（`kubernetes-sigs/agent-sandbox`）+ 出网代理与密钥下发合并为「凭据代理 sidecar + 桩凭据」模型（Agent 永不持有真凭据）；[11](./11-security-and-threat-model.md) 新增「外部内容注入」攻击面（§3.4/§4.2/R2）+ 凭据外泄控制升级为桩凭据模型 + 新增残余风险 R9；[03](./03-system-architecture.md) §5 / [09](./09-api-clients-and-data-model.md) §1.3 配套（agent-sandbox 指引 + per-session env 校验） |
| **v0.6.1** | 2026-06-05 | 🔧 评审修订（无新增文档，详见 [CHANGELOG](./CHANGELOG.md)）：[08](./08-observability-and-sse.md) 事件分类补 `memory.*`/`delivery.*`/`run.timeout_warning`；[21](./21-run-lifecycle-state-machine.md) 审批拒绝语义修正（T7 → `running`，`deny` 仅挡此工具不终止 Run；状态 11/转换 15 不变）+ §4 标注 pause/resume 为 P3+ 未建模；[22](./22-gitops-configuration-as-code.md) 可见域字段 `scope`→`visibility`、`organization`→`org`、ToolPolicy `defaultMode` YAML 结构修正；[23](./23-requirements-traceability-matrix.md) `parentRunId`→`parentSessionId`、引言「~50」→「114」；[20](./20-ide-integration-protocol.md)+README IDE 阶段 `P5+`→`P3+`；[07](./07-sandbox-isolation.md) 阅读指南 §编号、[04](./04-pi-integration-and-multi-llm.md)/[24](./24-delivery-and-integration-catalog.md) 错别字修正 |
| **v0.6** | 2026-06-02 | 🆕 新增 [21](./21-run-lifecycle-state-machine.md)（Run 生命周期状态机：UML 状态图/11 状态+15 转换/合法操作矩阵）；🆕 新增 [22](./22-gitops-configuration-as-code.md)（GitOps：仓库结构/YAML Schema/双向同步/冲突裁决/CI 校验）；🆕 新增 [23](./23-requirements-traceability-matrix.md)（需求追溯矩阵：114 条需求开放问题→设计决策逐条对照）；🆕 新增 [24](./24-delivery-and-integration-catalog.md)（交付物与集成目录：10 类交付物/L1–L5 五级集成/安全控制）；📝 [03](./03-system-architecture.md) 新增 §4 Agent 任务队列与优先级策略；📝 [11 §9](./11-security-and-threat-model.md) 合规映射深化（SOC 2 TSC/ISO 27001 附录 A/等保 2.0/MITRE ATT&CK 映射）；📝 [16 §4.4](./16-notification-system.md) 补充 Webhook HMAC 签名验证细节；📝 README §6 覆盖矩阵扩充（11→27 项）；📝 README §8 术语表扩充（沙箱/sandbox、产物/artifact/交付物、熔断/撤销、GitOps/CaC）；📝 全文档交叉引用新增 21–24 的引用路径 |
| **v0.5** | 2026-06-02 | 🆕 新增 [17](./17-cost-optimization.md)（成本优化：缓存/路由/预热池/Spot/分层存储/成本模型）；🆕 新增 [18](./18-capacity-planning.md)（容量规划：沙箱画像/节点估算/Orchestrator 瓶颈/参考配置）；🆕 新增 [19](./19-multi-region-deployment.md)（多区域部署：拓扑/数据驻留/一致性/DR/合规路由）；🆕 新增 [20](./20-ide-integration-protocol.md)（IDE 集成：VS Code/JetBrains 插件/本地桥接）；📝 新增 [CHANGELOG.md](./CHANGELOG.md)（独立变更日志）；📝 README 新增 §8 术语约定（安审/作用域/闸门等统一）；🔧 `pi-mcp-adapter`/`pi-subagents` 版本标注为调研时版本；✅ 全文档交叉引用审核通过（35/35 有效） |
| **v0.4** | 2026-06-02 | 🆕 新增 [13](./13-operations-manual.md)（运维手册：故障恢复、备份 RPO/RTO、沙箱治理、升级策略、SLO 告警）；🆕 新增 [14](./14-data-retention-and-privacy.md)（数据留存与隐私：分类/保留期、GDPR 被遗忘权、跨域隔离、审计防篡改、脱敏规则全集、合规映射）；🆕 新增 [15](./15-prompt-management-and-evaluation.md)（Prompt 版本化/A-B/护栏、Agent 评估框架、LLM-as-Judge、反馈闭环）；🆕 新增 [16](./16-notification-system.md)（多渠道通知、审批升级链、Digest 聚合、用户偏好） |
| **v0.3** | 2026-06-02 | 🆕 新增 [11](./11-security-and-threat-model.md)（安全架构与威胁模型：STRIDE、攻击面、控制矩阵、残余风险、事件响应、SDL）；🆕 新增 [12](./12-testing-strategy.md)（测试策略：金字塔、安全测试用例、兼容性、性能、混沌工程）；📝 [02](./02-product-requirements.md) §3 NFR 全面量化（T/M/S 三级指标 + 阶段绑定） |
| **v0.2** | 2026-06-02 | 初版 10 篇设计文档 + README：调研→需求→架构→pi 集成→RBAC→能力层→沙箱→可观测→API→路线图 |

