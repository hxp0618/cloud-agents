# 04 · pi 集成与多 LLM

本文是技术核心：如何把极简的 pi 改造成受治理的企业 Agent 运行时——**驱动方式（RPC 为主）**、**平台扩展（治理落点）**、**权限闸门接缝**、**LLM Gateway**、**会话**。

> ⚠️ 文中 pi 的命令/事件/API 名来自官方文档（高可信），但精确 TS 字段需在实施前对照 pi 源码 `rpc-types.ts` 与 `dist/*.d.ts` 核定，并固化成内部 `@polaris/pi-rpc` 类型库。

---

## 1. 驱动方式选型

pi 提供三种嵌入：

| 方式 | 形态 | 适用 | 取舍 |
|---|---|---|---|
| **RPC 模式** ⭐主 | `pi --mode rpc` 长驻子进程，stdin/stdout 行分隔 JSON | 交互式会话、长任务、需双向控制（改模型/中止/审批） | 进程级隔离、可双向驱动、契合沙箱边界 |
| **JSON 模式** | `pi --mode json "prompt"` 一次性，流式事件到 stdout | headless 自动化、CI、定时任务 | 简单但单向、一次性 |
| **SDK** | `createAgentSession()` 同进程嵌入 | 需要在 Worker 进程内深度集成时 | 与 Worker 同生命周期，隔离弱于子进程 |

**决策**：以 **RPC 模式为主**（沙箱内 pi 子进程 ↔ Orchestrator）。JSON 模式用于无状态一次性 Run。SDK 作为特定场景备选。

### 1.1 RPC 协议要点（务必正确实现）
- 传输：**stdin/stdout**，**行分隔 JSON（JSONL），不是 JSON-RPC**。`\n` 是唯一分隔符。
- ⚠️ **Node `readline` 不合规**（会按 U+2028/U+2029 切行）→ **自写行分割器**。
- 命令可带 `id` 用于关联；**事件无 `id`**。响应：`{"id","type":"response","command","success":bool,"error"?}`。
- 命令集（节选）：`prompt / steer / follow_up / abort / new_session / get_state / get_messages / set_model / cycle_model / get_available_models / set_thinking_level / compact / set_auto_compaction / set_auto_retry / bash / abort_bash / fork / clone / switch_session / get_session_stats / export_html`。
- 流式中再次 `prompt` 需带 `streamingBehavior: "steer" | "followUp"`。

```jsonc
// Orchestrator → pi (stdin)
{"id":"req-1","type":"prompt","message":"修复 login 的空指针并补测试"}
{"id":"req-2","type":"set_model","provider":"anthropic","modelId":"claude-..."}
{"id":"req-3","type":"abort"}
```

### 1.2 事件（与 SDK/JSON 同名）
`agent_start/end`、`turn_start/end`、`message_start/update/end`、`tool_execution_start/update/end`、`tool_call`（可拦截）、`tool_result`、`queue_update`、`compaction_start/end`、`auto_retry_start/end`、`extension_error`，以及扩展 UI 的 `extension_ui_request/response`。文本增量在 `message_update.assistantMessageEvent.type === "text_delta"` 的 `.delta`。

```jsonc
// pi → Orchestrator (stdout)
{"type":"tool_execution_start","toolCallId":"call_abc","toolName":"bash","args":{"command":"ls -la"}}
{"type":"message_update","message":{...},"assistantMessageEvent":{"type":"text_delta","delta":"正在"}}
{"type":"turn_end","message":{...},"toolResults":[]}
```

---

## 2. 平台扩展（Platform Extension）—— 治理的唯一落点

这是把"极简 pi"变"企业 Agent"的**linchpin**。它是一个由平台编写、注入**每个** pi 进程的 TypeScript 扩展（`-e` 加载或放进沙箱扩展目录），承载治理职责，并**受控装载** pi 官方能力包：

```mermaid
flowchart LR
    subgraph PE["平台扩展 (注入每个 pi 进程)"]
        Gate["① tool_call 权限闸门"]
        Emit["② 事件上报 → Event Hub"]
        Prov["③ 注册内部 Gateway provider"]
        Load["④ 受控装载官方包"]
    end
    Load --> MCP["pi-mcp-adapter (MCP)"]
    Load --> Sub["pi-subagents (子 Agent)"]
    MCP --> MCPS["MCP sidecars"]
    Sub --> ORC["派生子 pi 会话"]
    Gate -->|裁决回调| IAM[(IAM/PDP)]
    Emit --> EVT[(Event Hub)]
    Prov --> LLMGW[(LLM Gateway)]
    MCP -.工具调用过.-> Gate
    Sub -.工具调用过.-> Gate
```

> **为什么是扩展而非 fork**：pi 扩展能 `registerTool`、`on("tool_call")`（可 `block`）、`registerProvider`、`registerCommand`，足以实现全部治理需求；MCP/子 Agent 直接复用官方包 `pi-mcp-adapter`/`pi-subagents`。免改 pi 内核、可随 pi 升级。
>
> **安全前提**：扩展与这两个官方包都运行有**完整系统权限**（任意 TS）→ 因此**只有平台/管理员能编写扩展、受控引入官方包**；用户产出物是 Skill（见 [06](./06-capabilities-skills-mcp-subagents.md) 信任边界）。

### 2.1 ① 权限闸门（PEP）
```ts
// 伪代码：平台扩展
export default (pi: ExtensionAPI) => {
  pi.on("tool_call", async (event, ctx) => {
    // event.input 可改、可阻塞；对内置工具、MCP 工具、子 Agent 工具一视同仁
    const decision = await policyClient.authorize({
      runId, subject, scope,                 // 来自注入的运行上下文
      tool: event.toolName, input: event.input,
      skill: ctx.activeSkill, capabilityManifest,
    });
    emit("tool_call.requested", { tool: event.toolName, input: redact(event.input) });

    if (decision === "deny") {
      emit("tool_call.denied", { tool: event.toolName });
      return { block: true, reason: "策略拒绝：" + decision.reason };
    }
    if (decision === "ask") {
      // 走 pi 原生 UI 往返 → Event Hub → 客户端 SSE 审批
      const ok = await ctx.ui.confirm({ title: `允许执行 ${event.toolName}？`, timeout: 120000 });
      emit("permission.decided", { tool: event.toolName, approved: ok });
      if (!ok) return { block: true, reason: "用户拒绝" };
    }
    // allow → 放行
  });

  pi.on("tool_result", async (event) => emit("tool_call.result", summarize(event)));
};
```
- **PDP/PEP 分离**：裁决在控制面 IAM（Policy Decision Point），强制在扩展（Policy Enforcement Point）。
- **审批往返**：`ask` 时 `ctx.ui.confirm` 在 RPC 下发 `extension_ui_request` 并阻塞，Event Hub 把它桥接成客户端 SSE 的审批卡片，用户应答回 `extension_ui_response`。
- **破坏性兜底**：危险模式（`rm -rf`、写 `.env`、破坏性 MCP 工具）默认 `ask` 或 `deny`，不依赖模型自觉。
- ⚠️ **核定点**：需验证 `pi-mcp-adapter` 的 `mcp` 代理工具调用、`pi-subagents` 的派生工具调用是否**都经过 `tool_call` 钩子**——若是，则治理"零缝隙"；若某些路径绕过钩子，平台扩展需在包的集成点补挂拦截（见 [01 §E](./01-research-and-landscape.md)）。

### 2.2 ②③④ 其余职责
- **② 事件上报**：把 pi 事件 + 平台语义事件统一发往 Event Hub（见 [08](./08-observability-and-sse.md)）。
- **③ Gateway provider**：`pi.registerProvider("polaris-gateway", {...})` 让 pi 所有模型流量走内部网关（见 §4）。
- **④ 受控装载官方包**：装载并配置 `pi-mcp-adapter`（MCP 接入，见 [06 §3](./06-capabilities-skills-mcp-subagents.md)）与 `pi-subagents`（子 Agent，见 [06 §4](./06-capabilities-skills-mcp-subagents.md)）；其可用范围由 effective config（目录解析）决定，调用过闸门。

---

## 3. 会话与状态

- pi 会话是 **JSONL 树状**（每条 `id`+`parentId`，叶子=当前）；`SessionManager` 提供 `create/open/forkFrom/inMemory/appendMessage（自动落盘）/buildSessionContext()`。
- **持久化策略**：沙箱内会话文件实时同步到**对象存储**（按 Run 归档）；Run 结束后沙箱可销毁而会话可回放。
- **分支/重跑/审计**：树状结构天然支持"从某步 fork 重跑"（调参对比、人审回溯）。会话树映射到控制台的"运行回放"视图（见 [08](./08-observability-and-sse.md)）。
- **续跑/恢复**：Orchestrator 记录 Run/RPC 子进程状态；崩溃后可由会话文件 `--fork/--session` 恢复到最近叶子继续。
- **隔离要求**：会话明文中**不得**含密钥；密钥经间接引用，闸门对 `read`/`bash` 触碰密钥路径做策略限制。

---

## 4. 多 LLM：pi-ai + 内部 LLM Gateway

### 4.1 为什么要 Gateway（而非让 pi 直连）
pi-ai 本身已支持 20+ provider 与统一 schema、密钥解析（`--api-key` → `auth.json` → env → `models.json`）。但企业要的是**集中治理**，所以在 pi-ai 之上加一层内部 **LLM Gateway**：

| Gateway 价值 | 说明 |
|---|---|
| **密钥零暴露** | provider 真 key 只存 Vault、只在 Gateway 用；开发者/客户端/沙箱永不接触 |
| **模型白名单** | 按 组织/业务组/项目/角色 限制可用模型与默认模型；运行时 `set_model` 经白名单校验 |
| **成本/延迟计量** | 每次调用记 token/费用/时延 → 事件流 → 计量计费（归一信用单位） |
| **合规路由** | 敏感项目路由到 ZDR/私有/自建模型；区域数据驻留 |
| **限流/缓存/降级** | 配额、速率、prompt 缓存、provider 故障切换 |

借鉴 OpenCode "强制走内部网关" 与 Cody Gateway。

### 4.2 pi 如何指向 Gateway
两种方式（推荐组合）：
1. **自定义 provider**（首选）：平台扩展 `pi.registerProvider("polaris-gateway", { baseUrl, authHeader, models, oauth? })`，Gateway 暴露 OpenAI/Anthropic 兼容接口；模型选择经 `set_model`/默认配置走它。
2. **环境/auth 注入**：沙箱内把 `*_BASE_URL` 指向 Gateway、注入短期作用域令牌（非真 key）。

```mermaid
flowchart LR
    Pi["pi 进程"] -->|polaris-gateway provider| GW["LLM Gateway"]
    GW -->|白名单校验| Pol[(模型策略)]
    GW -->|真 key 取自 Vault| V[(Vault)]
    GW --> P1["Anthropic"] & P2["OpenAI"] & P3["自建 vLLM"] & P4["..."]
    GW -->|token/成本事件| EVT[(Event Hub)]
```

### 4.3 模型治理数据流
- Catalog 维护 `ModelProvider`（接入的厂商）与 `ModelAllowlist`（各作用域可用模型/默认/上限）。
- 解析"某用户在某项目的有效模型集" → 注入 pi 的 `get_available_models` 视图。
- 运行时切换：客户端/模型→ RPC `set_model` → 扩展校验是否在有效集 → 否则 `deny` 并提示。

---

## 5. 实施清单（pi 集成相关）

- [ ] 拉取 pi 源码 + 安装 `pi-mcp-adapter`/`pi-subagents`，核定 RPC 命令/事件清单、扩展 API 类型、**两包工具调用是否都经 `tool_call`**，产出 `@polaris/pi-rpc`（含**自写行分割器**）。
- [ ] 实现 `@polaris/platform-extension`：闸门 / 事件上报 / gateway provider / 受控装载并配置两个官方包。
- [ ] Orchestrator 的 RPC 驱动器：进程管理、命令编解码、事件路由、超时/中止/恢复。
- [ ] LLM Gateway：兼容接口、白名单校验、Vault 取 key、计量事件。
- [ ] 会话同步：沙箱 JSONL ↔ 对象存储；回放映射。
- [ ] 注入机制：把 effective config（模型/工具/Skill/MCP/子Agent/运行上下文/密钥引用）安全注入沙箱与扩展。
- [ ] 兜底：危险工具默认策略、`set_model` 白名单、密钥路径保护、子 Agent 深度/扇出/成本上限。

> 关联：闸门策略来源见 [05](./05-rbac-and-governance.md)；MCP/子 Agent/Skill 见 [06](./06-capabilities-skills-mcp-subagents.md)；事件 schema 见 [08](./08-observability-and-sse.md)。
