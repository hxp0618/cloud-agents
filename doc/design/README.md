# Cloud Agents 平台设计方案

> 代号 **Polaris**（北极星）—— 一个企业级、统一治理的云端 AI Agent 平台，基于开源项目 [pi (earendil-works)](https://github.com/earendil-works/pi) 构建。
>
> 文档版本：v0.2（草案） · 日期：2026-06-02

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
2. **控制面 / 数据面分离**：治理与编排（无状态、可水平扩展）vs. 真正跑 Agent 的隔离沙箱。
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
| [`07-sandbox-isolation.md`](./07-sandbox-isolation.md) | 每 Agent 沙箱、Workspace 后端抽象、资源/网络/密钥模型、双层防御、生命周期 |
| [`08-observability-and-sse.md`](./08-observability-and-sse.md) | 事件溯源、事件信封与分类、SSE 传输与断点续传、审计、计量 |
| [`09-api-clients-and-data-model.md`](./09-api-clients-and-data-model.md) | REST/SSE API 面、Agent Run API、SDK/CLI/桌面端、控制台信息架构、数据模型 ER |
| [`10-roadmap-and-open-questions.md`](./10-roadmap-and-open-questions.md) | 分阶段路线图、MVP 定义、风险、待决策项（含建议）、成功指标 |

---

## 8. 名词表（Glossary）

| 术语 | 含义 |
|---|---|
| **Agent 定义 (AgentDefinition)** | 一份可复用的 Agent 配置模板：系统提示词、可用模型、工具/Skill/MCP 集合、权限画像、沙箱画像 |
| **Agent 运行 (Run / AgentInstance)** | 一次具体执行，绑定一个沙箱 + 一个会话树 |
| **会话 (Session)** | pi 的 JSONL 树状对话状态，支持 fork/clone/branch |
| **平台扩展 (Platform Extension)** | 平台注入每个 pi 进程的核心 TS 扩展，承载权限闸门 / 事件上报 / Gateway provider，并受控装载 `pi-mcp-adapter`、`pi-subagents` |
| **能力清单 (Capability Manifest)** | Skill/MCP 声明其所需工具、路径、出网域名、密钥等的清单；沙箱据此做能力约束（capability-based enforcement） |
| **可见域 (Scope)** | 资源的可见/可用范围：私有 → 项目 → 业务组 → 组织 |
| **托管锁定 (Managed/Locked)** | 上级设定且下级不可覆盖的配置（借鉴 OpenCode MDM 语义） |
| **LLM Gateway** | 内部统一大模型网关，集中托管密钥、做模型白名单与成本计量 |

---

> 📌 **如何阅读**：决策者先看本 README + [01 差异化](./01-research-and-landscape.md) + [10 路线图](./10-roadmap-and-open-questions.md)；架构师从 [03](./03-system-architecture.md) 入手并下钻 04–08；产品/项目经理读 [02](./02-product-requirements.md) + [09 控制台信息架构](./09-api-clients-and-data-model.md)。
