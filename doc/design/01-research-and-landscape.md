# 01 · 调研与定位：pi 能力分析、同类产品、差异化

本文回答三个问题：(A) pi 给了我们什么、复用什么、还要自建什么；(B) 市面上谁在做类似的事、各自的做法；(C) 我们的差异化白地在哪。

---

## A. pi 能力深挖

> 来源：pi 官方文档（`packages/coding-agent/docs/*.md`）、pi.dev 包页面与源码包结构。pi = "AI agent toolkit: coding agent CLI, unified LLM API, TUI & web UI libraries, Slack bot, vLLM pods"，MIT 许可、活跃维护。核心包：`pi-ai`（多 provider LLM 抽象）、`pi-agent-core`（Agent 循环 + 消息类型）、`pi-coding-agent`（CLI + interactive/print/rpc/json 四种模式）、`pi-tui`。
>
> ⚠️ **版本声明**：文中 `pi-mcp-adapter` v2.8.0 与 `pi-subagents` v0.27.0 为 **2026-06-02 调研时的最新版本**。实施前需 `npm view pi-mcp-adapter pi-subagents version` 确认最新版本，并核定其变更是否影响集成方案。

### A.1 直接复用 ✅

| 能力 | pi 提供的形态 | 我们如何用 |
|---|---|---|
| **多 LLM** | `pi-ai`：`getModel(provider, modelId)` / `stream()` / `complete()`；20+ provider（Anthropic、OpenAI、Gemini、Mistral、Bedrock、Vertex、Azure、DeepSeek、xAI、OpenRouter、Vercel/Cloudflare Gateway…）；统一消息/工具 schema；订阅制 OAuth（Claude Pro/Max、ChatGPT、Copilot）；密钥解析 `--api-key` → `auth.json` → env → `models.json` | 作为底层；前面再加内部 Gateway（见 04） |
| **Agent 运行时** | `pi-agent-core` 有状态 Agent + 工具执行 + 事件流；内置 7 工具 `read/bash/edit/write/grep/find/ls`（默认 4 个） | 沙箱内跑 pi 进程；工具调用全部过我们的闸门 |
| **SDK 嵌入** | `createAgentSession({model, tools, customTools, sessionManager, resourceLoader, cwd})` → `AgentSession`：`prompt()/steer()/followUp()/subscribe()`；`createAgentSessionRuntime()` 支持 `newSession/switchSession/fork/importFromJsonl` | 备用嵌入方式（同进程） |
| **RPC 模式** ⭐ | `pi --mode rpc`：**stdin/stdout 行分隔 JSON（非 JSON-RPC）**；命令带可选 `id`、事件无 `id`；命令含 `prompt/steer/follow_up/abort/new_session/get_state/get_messages/set_model/cycle_model/get_available_models/set_thinking_level/compact/bash/fork/clone/switch_session…` | **主嵌入方式**：后端以长驻子进程驱动 pi（见 04） |
| **JSON 流式** | `pi --mode json "prompt"`：首行 session 头 `{"type":"session","version":3,"id","cwd"}`，随后 `agent_start→turn_start→message_start→message_update*→message_end→turn_end→agent_end` | headless/一次性任务的事件源 |
| **扩展机制** ⭐ | TS 模块经 jiti 加载（免编译）；`pi.registerTool` / `pi.on("tool_call")`（**可 `return {block:true,reason}`，`event.input` 可改**）/ `tool_result`（可改结果）/ `pi.registerCommand` / `pi.registerProvider`（含自定义 OAuth provider）/ 丰富生命周期事件 | **平台扩展**的落点（见 04） |
| **权限往返** ⭐ | RPC 下 `ctx.ui.confirm/select/input/editor` 会发 `extension_ui_request` 并**阻塞**等待 `extension_ui_response`（按 `id` 匹配，支持 `timeout`） | 人工审批 / 自动决策的协议通道 |
| **Skill** | Agent Skills 标准：`SKILL.md`（YAML frontmatter：`name/description/allowed-tools/disable-model-invocation`…）+ Markdown；多目录发现；**渐进式披露**（启动只注入名称+描述，模型按需 `read` 全文）；`/skill:name` 强制触发 | 中央 Skill 库挂载进沙箱（见 06） |
| **会话持久化** | `~/.pi/agent/sessions/` 下 **JSONL 树状**（每条带 `id`+`parentId`，叶子=当前）；`SessionManager`：`create/open/continueRecent/inMemory/forkFrom/list`、`appendMessage`（自动落盘）、`buildSessionContext()`；支持 `-c/-r/--fork/--name` | 会话存对象存储；树状结构支撑分支/重跑/审计回放 |

### A.2 复用 pi 官方/生态包，再加治理 🧩

pi **内核**刻意不内置 MCP / sub-agent（理念："Features that other tools bake in can be built with extensions, skills, or installed from third-party pi packages"），但官方/生态已提供**独立可选包**，`pi install npm:...` 安装：

| 能力 | pi 官方/生态包 | 我们如何用 |
|---|---|---|
| **MCP** | **`pi-mcp-adapter`（v2.8.0）**：用单个代理 `mcp` 工具懒加载 MCP server（省 token，单 server 可省 10k+ token），按需 `search/describe/connect/call`；支持 **OAuth、stdio/HTTP**、`directTools` 把指定工具提升为一等工具、`/mcp` 面板、从 Cursor/Claude Code 导入配置 | 平台**受控装载**该包；MCP server 作为带可见域/版本/能力清单的目录资源，调用过闸门、破坏性工具强制审批（见 06 §3） |
| **Sub-agent** | **`pi-subagents`（v0.27.0）**：内置 `scout/researcher/planner/worker/reviewer/context-builder/oracle/delegate`；`/run`、`/chain a "t1" -> b "t2"`、`/parallel`、`--bg` 后台、`--fork`、**worktree 隔离**、**递归/深度护栏**、acceptance gates、chain 文件、`pi-intercom` 子父通信 | 平台**受控装载**该包；子 Agent 权限默认收窄、不可提权、事件纳入运行树、并发/成本受配额（见 06 §4） |

> ⚠️ 两包均提示"can execute code…review source before installing"——故属 **Extension 级（全权限）→ 仅平台/管理员受控引入**，不开放给用户自由安装（见信任边界 [README §3](./README.md)）。

### A.3 真正需要自建 🔨

pi 及其生态包都不解决的企业能力：

| 缺口 | 我们的方案（详见） |
|---|---|
| **权限系统** | pi 无弹窗/无规则引擎，只有静态 `--tools` 白名单 → RBAC + 工具策略引擎，落在 `tool_call` 闸门（[05](./05-rbac-and-governance.md)） |
| **沙箱** | pi 明确交给宿主 → 每 Agent 容器/microVM + 出网白名单（[07](./07-sandbox-isolation.md)） |
| **中央治理/共享** | pi 是单机工具，无组织概念 → 目录 + 可见域 + 继承锁定（[05](./05-rbac-and-governance.md)） |
| **用户 Skill 安全评估** | 无 → 自动扫描 + 能力清单 + LLM 风险判定 + 人审 + 熔断（[06](./06-capabilities-skills-mcp-subagents.md)） |
| **统一可观测/审计/计量** | pi 有事件流但无企业审计/计量 → 事件溯源 + SSE + 审计 + 信用计量（[08](./08-observability-and-sse.md)） |
| **多端接入** | pi 是 CLI/TUI → Web 控制台 + 桌面端 + API + SDK（[09](./09-api-clients-and-data-model.md)） |

> **关键洞察**：pi 的 `tool_call` 阻塞钩子 + RPC `extension_ui_request/response` 往返，正好是把"极简 pi + 官方能力包"改造成"受治理企业 Agent"的唯一且足够的接缝——无需 fork pi 内核。

---

## B. 同类产品调研

> 图例：✅ 有且强 · 🟡 部分/弱 · ❌ 无

### B.1 OpenHands（All-Hands-AI）—— 最接近的竞品
- **架构**：事件溯源。append-only **EventLog** 是唯一事实源，回放即可重建任意会话；极简循环 `Agent → Action → Workspace 执行 → Observation → 循环`；LLM 经 LiteLLM。V1 把 `Runtime` 抽象成可插拔 `Workspace`（Local / Docker / RemoteAPI）。
- **可借鉴**：① **Workspace 后端抽象**（同一 Agent 代码切换隔离后端）；② V0 教训——多租户共享容器导致"邻居噪声"崩溃 → **一实例一沙箱**；③ 向沙箱镜像注入"动作执行 REST server"获得干净进程边界；④ 基于 LLM 的**风险打分（Low/Med/High）+ 可配置确认策略**；⑤ 密钥经 `LookupSecret` 间接下发，绝不过客户端。
- **企业**：Cloud 提供 RBAC、预算、用量报表；`enterprise/` 目录 source-available（非 MIT）。MCP 一等公民。微代理/Skill = markdown+frontmatter，触发类型 `always/keyword/manual`。

### B.2 Dify（langgenius）—— 完整管理后台 + 应用化范式
- **方向**：LLM 应用 / workflow 平台；强项是**可视化管理后台、workflow 编排、RAG、运营面板、API**。
- **可借鉴**：完整的"管理控制台"信息架构与多租户运营视角（对应需求 4 的"完整页面管理"）。
- **差异**：偏 LLM 应用平台，**不是** Codex/Claude Code 风格的软件工程 Agent，也不基于 pi、无每 Agent 沙箱与本地 CLI 体验对标。

### B.3 OpenCode（sst）—— 中央配置范式
- **架构**：client/server 分离（`opencode serve` / `web`）；不存用户代码/上下文。
- **可借鉴（最强项=中央治理）**：① 单一组织中央配置，集成 SSO，可**禁用其它 provider 强制走内部网关**；② **macOS MDM 强制**：**托管键出现在解析后配置且用户/项目不可覆盖**；③ Cloudflare 实践——day 1 就全量走单一代理。

### B.4 Continue.dev —— Hub 共享与治理范式
- **架构**：**Hub（Mission Control）** = 可组合 **blocks**（models/rules/prompts/context/docs/**MCP**/data）拼成 **assistants**。
- **可借鉴（共享 Agent/Skill/MCP 治理）**：① 角色 Admin/Member；② **可见性 private/team/public** 分层 + 企业版按 block/model/version/vendor 精细允许 + **审计日志**；③ 密钥 `${{ secrets.NAME }}` 服务端解析。**未发现对用户发布 block 的自动安全扫描**（=我们的机会）。

### B.5 Goose（Block）—— 全 MCP 扩展 + 对抗式审查
- **可借鉴**：① 工具权限 + **Approve Mode** + **对抗式审查器（adversary reviewer）监视不安全动作** + 提示注入检测；② 并行子 Agent；③ Recipes（类 Skill，但明确警告外部 recipe 是信任风险）。

### B.6 企业管理视角：Devin / Cursor / Cody
- **Devin Enterprise**（架构最接近我们的沙箱模型）：**组织=自包含单元**；**Teamspace 部门级隔离**；**VPC 部署** + SSO；用量单位 **ACU**（VM 时间+推理+带宽归一化）—— **可作为我们的计量单位**。
- **Cursor Enterprise**：**SCIM 2.0**；**MDM 强制**（本地不可覆盖）；模型黑名单、花费上限。
- **Cody Enterprise**：模型黑名单；**Context Filters**（敏感代码不发第三方模型）；令牌 TTL。

### B.7 本地 CLI 对标：Codex & Claude Code（体验/功能对齐）
- **Codex CLI**：**内核级沙箱**（Seatbelt / Landlock+seccomp），三策略 `read-only/workspace-write/danger-full-access` + 命名权限档；**MCP 客户端+服务端**；**破坏性 MCP 工具恒需审批**；企业 `requirements.toml` 强制。
- **Claude Code**：子 Agent（独立上下文、默认只读）；Skill + **Plugin**（捆绑分发）；权限 `allow/deny/ask`；**沙箱与权限分离**；headless `--output-format stream-json`。
- 两者是**云端化、可统一治理**的对标对象——它们强在本地体验，弱在企业集中管理（正是我们要补的）。

---

## C. 对比表

| 维度 \ 系统 | OpenHands | Dify | OpenCode | Continue | Devin Ent. | Claude Code | Codex CLI | **Polaris（本方案）** |
|---|---|---|---|---|---|---|---|---|
| 隔离模型 | ✅ 每会话 Docker | 🟡 | ❌ 本地 | ❌ | ✅ 每组织 VM | ✅ Seatbelt/bwrap | ✅ Landlock/Seatbelt | ✅ **每 Agent 容器/microVM** |
| RBAC | 🟡 Cloud | ✅ 后台 | 🟡 靠 SSO/网关 | ✅ Admin/Member | ✅ 多组织 | 🟡 配置规则 | 🟡 toml | ✅ **组织/组/项目/用户四级** |
| 中央治理 | 🟡 | ✅ | ✅✅ 配置+MDM+网关锁 | ✅✅ Hub+审计+厂商白名单 | ✅ 企业账户+VPC | 🟡 plugin | 🟡 toml | ✅✅ **目录+可见域继承+锁定** |
| MCP | ✅ | 🟡 | ✅ | ✅ block | 🟡 | ✅✅ | ✅✅ 端+服 | ✅ **复用 pi-mcp-adapter + 治理** |
| Skill | ✅ 微代理 | 🟡 | 🟡 | ✅ blocks | ❌ | ✅✅ +Plugin | 🟡 | ✅ **+安全评估发布** |
| Sub-agent | ✅ 委派 | 🟡 workflow | ❌ | ❌ | ✅ | ✅✅ | 🟡 | ✅ **复用 pi-subagents + 治理** |
| SSE/可观测 | ✅ 事件溯源 | 🟡 | 🟡 | ❌ | 🟡 | ✅ stream-json | ✅ JSONL | ✅✅ **统一事件流+审计+计量** |
| 自托管 | ✅ VPC/K8s | ✅ | ✅ | 🟡 企业 | ✅ VPC | n/a | n/a | ✅ **自托管/VPC 优先** |
| API/headless | ✅ REST+SDK | ✅ | ✅ serve | 🟡 | ✅ | ✅✅ | ✅✅ | ✅ **REST+SSE+SDK** |
| 桌面端 | 🟡 web | ❌ web | 🟡 TUI/web | ❌ IDE | ✅ | ❌ | 🟡 | ✅ **瘦客户端桌面** |
| 用户发布 Skill 安全评估 | ❌ 仅 PR | ❌ | ❌ | ❌ 仅可见性 | ❌ | ❌ | ❌ | ✅✅ **自动扫描+人审流水线** |

---

## D. 差异化白地（我们独占的组合）

没有任何一家把下面这些**同时**做好——这就是 Polaris 的核心楔子：

1. **组织级"共享可运行的 Agent 能力"**（不止共享配置）。Continue/Claude Code 共享的是"定义"；Devin 按组织隔离但无 Skill/MCP 市场；Dify 偏应用编排。**Polaris 共享的是受治理的 Agent+MCP+Skill 目录——同项目成员可见同一套 Agent 定义，各自发起独立 Run（一 Run 一沙箱，不存在多人共用同一会话导致上下文交叉）。把"能力目录"+"每 Agent 沙箱"+"完整 RBAC"+"项目/业务组作用域"四件事捏在一起，无人做过。**
2. **用户发布 Skill 的安全评估**。Continue Hub / Claude plugin 市场只有可见性与版本白名单；Goose 警告外部 recipe 不安全；pi 包是全权限任意代码。**没人在"用户发布的 Skill 进入组织可见前跑自动安全扫描+人审"。这是我们独占。**
3. **多租户中"每 Agent"（而非每用户）沙箱**。OpenHands 用血泪证明共享容器=邻居噪声崩溃；Devin 每组织 VM 但闭源。开源 + 基于 pi + 每 Agent 微沙箱 + 配额计量 = 差异化。
4. **与 RBAC 绑定的统一 SSE 可观测**。把 OpenHands 事件溯源 + Continue 审计治理合一。
5. **"业务组"作为一等作用域**（介于团队与组织之间），做"项目→业务组→组织"三级继承 + OpenCode 式"托管不可覆盖"覆盖语义。

> 一句话差异化：**codex/claude code 让每个开发者在本地各自配置、各自为政；Polaris 让一个企业把 Agent 能力当作"集中托管、按业务组/项目共享、安全可审、隔离运行"的基础设施——开箱即用、不依赖本地环境。**

---

## E. 调研可信度与待核实项

- **高可信**（pi 官方文档/包页面）：RPC 传输与命令/事件名、`extension_ui` 权限往返、`--mode rpc|json`、会话 JSONL schema、provider 列表与密钥解析、"core 不内置 MCP/sub-agent 但有官方包 `pi-mcp-adapter`/`pi-subagents`"。
- **已核实更正**：早期"pi 无 MCP/sub-agent，须从零自建"的判断**已修正**——二者均有官方可选包，应**复用并治理**。
- **待核实**：`message_update/turn_end` 事件精确 TS 字段（读 `dist/*.d.ts` 与 `rpc-types.ts`）；`pi-mcp-adapter`/`pi-subagents` 的事件/钩子与平台扩展闸门的精确集成点（其工具调用是否都经 `tool_call`，需源码核定）；Dify/Continue/Cursor 具体分层以官方最新文档为准。
- **实施前动作**：拉取 pi 源码 + 安装这两个包，核对其工具暴露与事件是否完全经 `tool_call` 钩子（决定治理是否"零缝隙"），落成内部 `@polaris/pi-rpc` 类型与集成测试。

---

> 💡 **如何阅读**：决策者看 §C–§D（竞争对比 + 差异化定位）；架构师看 §A（pi 能力清单 + 复用/自建边界）；产品经理看 §B（同类产品调研） + §C（对比表）。
