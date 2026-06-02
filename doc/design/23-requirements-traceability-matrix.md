# 23 · 需求追溯矩阵

本文把 [`requirements/cloud-agent-requirements-discovery.md`](../requirements/cloud-agent-requirements-discovery.md) 中提出的 ~50 个开放问题，逐一映射到设计文档中的决策与落点。用于需求评审时的逐条确认与合规审计时的追溯。

---

## 1. 产品定位（需求稿 §5）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q5.1 | MVP 首先服务研发场景，还是也覆盖业务自动化？ | **研发场景优先**（代码编写、Review、测试、部署）；业务自动化留 P3+ | [02 §2](./02-product-requirements.md) FR-1~11；[10 §1](./10-roadmap-and-open-questions.md) P0 MVP |
| Q5.2 | 项目管理员和组织管理员是否是同一批人？ | **分开角色**：组织管理员管全局策略/计费；项目管理员管项目内资源与成员 | [05 §2](./05-rbac-and-governance.md) 角色定义 |
| Q5.3 | 安全审核是否必须独立于项目管理员？ | **独立角色**（Auditor/Security），含安审终审权、审计日志只读、Skill 撤销权 | [05 §2](./05-rbac-and-governance.md) 审计员角色 |
| Q5.4 | 用户是否需要像使用本地 Codex/Claude Code 一样直接操作代码仓库？ | **是**——Agent 在沙箱内 clone 仓库、读写文件、跑命令、生成 Diff，但不依赖本地环境 | [02 §4](./02-product-requirements.md) 用户故事 1；[07 §2](./07-sandbox-isolation.md) Workspace 抽象 |
| Q5.5 | 非研发用户是否需要低代码/工作流式入口？ | **v1 不做**（列入非目标） | [02 §5](./02-product-requirements.md) 非目标；[10](./10-roadmap-and-open-questions.md) 后续阶段 |

## 2. "不是聊天机器人"的内涵（需求稿 §5.2）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q5.6 | 第一版是否必须支持后台异步任务？ | **是**——Run 本质是异步长任务（POST /runs → SSE 实时进度） | [03 §3](./03-system-architecture.md) 端到端时序；[09](./09-api-clients-and-data-model.md) API |
| Q5.7 | 是否需要任务模板（"修复 CI""审查 PR"）？ | **是**——通过 AgentDefinition 实现可复用模板 | [02 FR-2/FR-3](./02-product-requirements.md)；[06](./06-capabilities-skills-mcp-subagents.md) |
| Q5.8 | 是否需要 workflow 视图，还是先从单 Agent Run 开始？ | **先从单 Agent Run 开始**；workflow 编排留 P3+（`pi-subagents` 的 `/chain`、`/parallel` 为子 Agent 编排，非用户可视化 workflow） | [10 §1](./10-roadmap-and-open-questions.md) P0/P2；[02 §5](./02-product-requirements.md) 非目标 |
| Q5.9 | 成果物以什么形式交付？ | 代码 Diff、PR/MR、测试报告、文件产物、API response。详见 [24](./24-delivery-and-integration-catalog.md) | [09](./09-api-clients-and-data-model.md) API；[24](./24-delivery-and-integration-catalog.md) |

## 3. Agent 管理（需求稿 §6.1）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q6.1 | Agent 是由平台统一创建，还是用户也可以创建私有 Agent？ | **两者皆可**：管理员创建默认 Agent，用户可创建私有 Agent（可见域=private） | [05 §3](./05-rbac-and-governance.md) 可见域；[06](./06-capabilities-skills-mcp-subagents.md) AgentDefinition |
| Q6.2 | 私有 Agent 是否能提交为项目共享 Agent？ | **是**——通过提升可见域 + 审批（类似 Skill 发布流程） | [05 §3](./05-rbac-and-governance.md)；[06](./06-capabilities-skills-mcp-subagents.md) |
| Q6.3 | Agent 是否必须版本化？ | **是**——AgentDefinition 带版本，变更不可变 | [06 §1](./06-capabilities-skills-mcp-subagents.md)；[09](./09-api-clients-and-data-model.md) 数据模型 |
| Q6.4 | Agent 是否允许绑定多个默认 Skill/MCP？ | **是**——AgentDefinition 含工具/Skill/MCP 集合 | [06 §1](./06-capabilities-skills-mcp-subagents.md) AgentDefinition schema |
| Q6.5 | Agent 是否允许用户运行时临时覆盖模型、Skill、MCP？ | **允许，但在有效权限范围内**：用户可在发起 Run 时覆盖模型（从白名单选）、追加 Skill/MCP（需有使用权限） | [04 §2](./04-pi-integration-and-multi-llm.md)；[05](./05-rbac-and-governance.md) |
| Q6.6 | 项目默认 Agent 和组织默认 Agent 冲突时如何处理？ | **下级覆盖上级，除非上级锁定（managed:true）** | [05 §3](./05-rbac-and-governance.md) 继承与锁定 |

## 4. MCP 管理（需求稿 §6.2）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q6.7 | 谁可以添加 MCP server？ | 管理员（组织/业务组/项目）；普通用户不可自行添加 MCP server | [05 §2](./05-rbac-and-governance.md) 权限矩阵；[06 §3](./06-capabilities-skills-mcp-subagents.md) MCP 接入治理 |
| Q6.8 | MCP server 是组织级、项目级，还是用户私有级？ | **三级皆有**——分别对应组织/项目/私有可见域 | [05 §3](./05-rbac-and-governance.md)；[06 §3](./06-capabilities-skills-mcp-subagents.md) |
| Q6.9 | MCP 工具是整体授权，还是按 tool 授权？ | **按 tool 授权**（能力清单声明 + destructive 注解 + 策略匹配）；整体白名单/黑名单可选 | [06 §3](./06-capabilities-skills-mcp-subagents.md) capability manifest |
| Q6.10 | 读操作和写操作是否区分权限？ | **是**——`destructive` 注解区分；写操作默认 ask/deny | [06 §3](./06-capabilities-skills-mcp-subagents.md)；[04 §2.1](./04-pi-integration-and-multi-llm.md) 闸门 |
| Q6.11 | MCP 认证使用项目凭证、用户凭证，还是服务账号凭证？ | **服务账号凭证**（由平台托管，间接引用 `secret://`）；需用户级凭证时走 OAuth 代理 | [04 §2.2](./04-pi-integration-and-multi-llm.md)；[06 §3](./06-capabilities-skills-mcp-subagents.md) |
| Q6.12 | MCP 调用是否必须记录到事件流和审计日志？ | **是**——所有 MCP 工具调用经闸门，记录为 `tool_call` 事件 | [08 §2](./08-observability-and-sse.md) 事件分类；[08 §4.2](./08-observability-and-sse.md) 审计 |
| Q6.13 | 外部 MCP 和内部 MCP 是否采用不同审核流程？ | **是**——外部 MCP 强制人工审批；内部 MCP 可走快速通道 | [06 §3](./06-capabilities-skills-mcp-subagents.md) |

## 5. Skill 管理（需求稿 §6.3）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q6.14 | 用户私有 Skill 是否只允许自己使用？ | **默认私有**；用户可提交发布到项目/业务组/组织（经安审） | [06 §2](./06-capabilities-skills-mcp-subagents.md) Skill 生命周期 |
| Q6.15 | 用户私有 Skill 能否访问项目资源？ | **需经项目管理员授权**——私有 Skill 运行在沙箱内，但若访问项目仓库/密钥需显式授权 | [06 §2](./06-capabilities-skills-mcp-subagents.md)；[05](./05-rbac-and-governance.md) |
| Q6.16 | Skill 发布到项目和发布到组织是否需要不同审批？ | **是**——组织域发布需组织安全审批人审批（更严格）；项目域由项目管理员审批 | [06 §2.3](./06-capabilities-skills-mcp-subagents.md) 安审工作流 |
| Q6.17 | Skill 是否允许包含脚本、二进制、依赖、外部网络访问？ | **允许脚本，强制安审 + 能力清单 + 沙箱**；纯指令型可走快速通道；二进制默认拒绝（需特审） | [06 §2.4](./06-capabilities-skills-mcp-subagents.md) 安审维度；[10 D6](./10-roadmap-and-open-questions.md) |
| Q6.18 | Skill 安全评估由自动扫描、人工审批，还是两者结合？ | **两者结合**——自动扫描（静态+LLM+能力清单）+ 人工复审（中/高风险触发） | [06 §2.4](./06-capabilities-skills-mcp-subagents.md) 安审流水线 |
| Q6.19 | Skill 被撤销后，历史 Run 如何展示？ | 历史 Run 仍可回放（Skill 内容已固化在会话 JSONL 中）；Skill 详情页标注"已撤销" | [06 §2.5](./06-capabilities-skills-mcp-subagents.md) 撤销与熔断 |
| Q6.20 | Skill 是否需要版本化和变更记录？ | **是**——已发布版本不可变；变更记录强制 | [06 §2.1](./06-capabilities-skills-mcp-subagents.md) 版本策略 |

## 6. 默认配置共享（需求稿 §6.4）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q6.21 | 共享范围是项目、业务组、组织，还是多层级？ | **四级**——私有→项目→业务组→组织，可继承可锁定 | [05 §3](./05-rbac-and-governance.md) 可见域 |
| Q6.22 | 默认 Agents/MCP/Skills 由谁配置？ | 各级管理员：组织管理员设组织默认；业务组管理员可在组织默认上追加；项目管理员在继承链上追加 | [05 §3](./05-rbac-and-governance.md) |
| Q6.23 | 用户能否隐藏或禁用项目默认配置？ | **不可禁用（若锁定）**；可隐藏/排序个人视图（纯 UI 偏好，不影响有效配置） | [05 §3](./05-rbac-and-governance.md) |
| Q6.24 | 用户私有配置能否覆盖项目默认配置？ | **不可**（若项目锁定了某项配置）；未锁定项可追加 | [05 §3](./05-rbac-and-governance.md) 冲突裁决 |
| Q6.25 | 项目默认配置变更是否影响历史 Run？ | **不影响已完成的 Run**（配置在 Run 启动时快照为 effective config）；新 Run 使用新配置 | [04 §1.2](./04-pi-integration-and-multi-llm.md) effective config 快照 |
| Q6.26 | 新成员加入项目时是否自动获得默认配置？ | **是**——effective config 解析时实时计算 | [05 §3](./05-rbac-and-governance.md) |

## 7. 权限与治理（需求稿 §7）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q7.1 | 是否需要组织、业务组、项目三级管理？ | **是**——四级作用域（组织→业务组→项目→用户） | [05 §1](./05-rbac-and-governance.md) 层级模型 |
| Q7.2 | 项目管理员是否可以管理所有项目资源？ | **是**（项目内全部资源：Agent/MCP/Skill/成员/Run 查看） | [05 §2](./05-rbac-and-governance.md) 权限矩阵 |
| Q7.3 | 安全审核人员是否独立于项目管理员？ | **是**——独立角色，跨域只读审计、安审终审 | [05 §2](./05-rbac-and-governance.md) |
| Q7.4 | 用户能否查看同项目其他人的 Run？ | **是**——项目成员可查看同项目所有 Run（会话内容） | [05 §2](./05-rbac-and-governance.md)；[14 §2](./14-data-retention-and-privacy.md) 隔离矩阵 |
| Q7.5 | 用户能否复用他人的 Run 结果或 Artifacts？ | **是**——同项目内可 fork/引用（通过会话 JSONL） | [09](./09-api-clients-and-data-model.md) |
| Q7.6 | 谁可以查看原始工具调用参数？ | 项目成员（含脱敏处理后的参数）；审计员可看完整参数 | [05 §2](./05-rbac-and-governance.md)；[08 §3](./08-observability-and-sse.md) 脱敏 |
| Q7.7 | 谁可以查看模型输入输出的完整内容？ | 项目成员；审计员可看完整历史 | [05](./05-rbac-and-governance.md)；[14](./14-data-retention-and-privacy.md) |
| Q7.8 | 服务账号是否需要单独权限模型？ | **是**——服务账号绑定特定项目/业务组 + 限定操作范围（如仅发起 Run）+ IP 白名单 | [05 §2.1](./05-rbac-and-governance.md) 服务账号 |

## 8. 沙箱与运行环境（需求稿 §8）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q8.1 | "每个 Agent"是指每个 Agent 定义、每次 Run，还是每个 Sub-agent？ | **每次 Run（root）+ 可选 Sub-agent 独立沙箱** | [07 §1](./07-sandbox-isolation.md) 隔离粒度 |
| Q8.2 | Sub-agent 是否必须独立环境？ | **默认复用父沙箱（worktree 隔离）**；高风险场景可选独立沙箱 | [07 §1](./07-sandbox-isolation.md)；[06 §4](./06-capabilities-skills-mcp-subagents.md) |
| Q8.3 | 沙箱是否需要访问代码仓库？ | **是**——经 egress 白名单 + 部署密钥或 OAuth token（间接引用） | [07 §3](./07-sandbox-isolation.md) 工作区后端 |
| Q8.4 | 沙箱是否需要访问公网？ | **默认否**；必要时经 egress 白名单按域名+端口放行 | [07 §4.2](./07-sandbox-isolation.md) egress |
| Q8.5 | 沙箱是否需要访问内部服务？ | **可配**——egress 白名单含内部域名（如内部 npm 镜像、API 网关） | [07 §4.2](./07-sandbox-isolation.md) |
| Q8.6 | 沙箱是否需要 GPU 或特殊硬件？ | **v1 不需要**；P5+ 按需支持 | [10](./10-roadmap-and-open-questions.md) |
| Q8.7 | 沙箱运行结束后是否保留现场？ | **默认保留 5min**（可配）；失败保留 30min（便于排查） | [07 §5](./07-sandbox-isolation.md) 生命周期；[21 §6](./21-run-lifecycle-state-machine.md) 回收策略 |
| Q8.8 | 用户是否需要进入沙箱调试？ | **v1 不做**（通过事件回放 + 产物替代）；P3+ 可考虑 | [10](./10-roadmap-and-open-questions.md) |
| Q8.9 | 依赖缓存是否允许项目共享？ | **是**——项目级 npm/pip 缓存 volume（只读挂载），降低冷启动 | [07 §3.1](./07-sandbox-isolation.md) |
| Q8.10 | 是否需要不同语言/框架的环境模板？ | **是**——Sandbox Profile（镜像+预装工具+缓存） | [07 §2](./07-sandbox-isolation.md) Sandbox Profile |

## 9. SSE 事件流与可观测（需求稿 §9）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q9.1 | 用户界面需要展示哪些事件？ | 消息文本（流式）、工具调用开始/结果、文件 Diff、终端输出、审批弹窗 | [08 §2](./08-observability-and-sse.md) 事件分类；[09](./09-api-clients-and-data-model.md) IA |
| Q9.2 | 管理员需要展示哪些事件？ | 上述全部 + 成本/Token 统计 + 安全标记 | [08 §4](./08-observability-and-sse.md) |
| Q9.3 | 安全审核人员需要展示哪些事件？ | 全部未脱敏事件 + 审计日志 + 安全标记 | [08 §4.2](./08-observability-and-sse.md)；[11](./11-security-and-threat-model.md) |
| Q9.4 | 哪些事件需要脱敏？ | 密钥、PII、内部 IP、密码参数。详见 [14 §5](./14-data-retention-and-privacy.md) 脱敏规则全集 | [08 §3](./08-observability-and-sse.md)；[14 §5](./14-data-retention-and-privacy.md) |
| Q9.5 | 是否保留原始事件？ | **是**——脱敏后的事件进入实时流 + 用户可见；原始事件保留在不可变审计存储（仅审计员可查） | [08 §3](./08-observability-and-sse.md) |
| Q9.6 | SSE 是否只是页面实时展示，还是也给 API 用户消费？ | **两者皆是**——SSE 同时服务于 Web 工作台和 API/SDK 用户（同一端点，RBAC 过滤） | [08 §2](./08-observability-and-sse.md)；[09](./09-api-clients-and-data-model.md) |
| Q9.7 | 断线重连是否必须恢复历史事件？ | **是**——`Last-Event-ID` 实现断点续传，窗口 ≥24h | [08 §2.2](./08-observability-and-sse.md) |
| Q9.8 | 是否需要跨 Sub-agent 的统一 trace？ | **是**——`parentRunId` / `subagentId` 构建运行树 | [08 §2](./08-observability-and-sse.md) 事件字段 |

## 10. Sub-agent（需求稿 §10）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q10.1 | Sub-agent 是用户显式选择，还是由 Agent 自动编排？ | **两者皆可**——Agent 可自动派生；用户可通过 `@researcher` 等命令显式触发 | [06 §4](./06-capabilities-skills-mcp-subagents.md) |
| Q10.2 | 是否允许用户创建自定义 Sub-agent？ | **是**——复用 AgentDefinition（Sub-agent 即普通 Agent，仅权限收窄） | [06 §4](./06-capabilities-skills-mcp-subagents.md) |
| Q10.3 | Sub-agent 是否能使用不同模型？ | **是**——在有效模型白名单内自由选择（如 worker 用 Sonnet、scout 用 Haiku） | [06 §4](./06-capabilities-skills-mcp-subagents.md)；[04](./04-pi-integration-and-multi-llm.md) |
| Q10.4 | Sub-agent 是否能使用不同 MCP/Skill？ | **是**——但不可超过父 Agent 的有效权限集 | [06 §4](./06-capabilities-skills-mcp-subagents.md) |
| Q10.5 | Sub-agent 是否有独立权限？ | **权限 ⊆ 父 Agent 权限** | [06 §4](./06-capabilities-skills-mcp-subagents.md)；[11 §3.4](./11-security-and-threat-model.md) |
| Q10.6 | Sub-agent 的结果如何汇总？ | 作为 `tool_result` 回灌父 Agent 上下文 | [06 §4](./06-capabilities-skills-mcp-subagents.md) |
| Q10.7 | Sub-agent 失败时 Root Agent 如何处理？ | 父 Agent 收到失败结果，可重试或走替代路径 | [06 §4](./06-capabilities-skills-mcp-subagents.md) |
| Q10.8 | 是否需要展示 Sub-agent 树或时间线？ | **是**——运行树视图（树形 + 时间线） | [08 §2](./08-observability-and-sse.md)；[09](./09-api-clients-and-data-model.md) IA |
| Q10.9 | 并发和成本是否需要用户确认？ | **是**——深度/扇出/并发上限可配；成本配额在启动时确认 | [06 §4](./06-capabilities-skills-mcp-subagents.md) 护栏 |

## 11. API、桌面端与集成（需求稿 §11）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q11.1 | API 是只创建任务，还是也管理 Agents/MCP/Skills？ | **全部**——REST API 覆盖 Run + Catalog CRUD + 管理 | [09 §1](./09-api-clients-and-data-model.md) API 面 |
| Q11.2 | API 是否支持流式事件消费？ | **是**——SSE 端点（`GET /v1/runs/{id}/events`） | [09 §1](./09-api-clients-and-data-model.md) |
| Q11.3 | API 是否支持服务账号？ | **是**——服务账号令牌 + 限定作用域 | [09 §1](./09-api-clients-and-data-model.md)；[05 §2.1](./05-rbac-and-governance.md) |
| Q11.4 | API 是否需要回调 Webhook？ | **是**——见 [16 §4.4](./16-notification-system.md) + [24](./24-delivery-and-integration-catalog.md) | [16](./16-notification-system.md)；[24](./24-delivery-and-integration-catalog.md) |
| Q11.5 | API 是否用于 CI/CD？ | **是**——核心场景之一（PR 审查、CI 触发 Agent Run） | [02 §4](./02-product-requirements.md) 用户故事 5 |
| Q11.6 | API 是否需要 OpenAPI 文档和 SDK？ | **是**——OpenAPI 3.1 + TS/Python SDK | [09 §4](./09-api-clients-and-data-model.md) SDK |
| Q11.7 | 桌面端是完整控制台，还是开发者快捷入口？ | **瘦客户端**——发起、观察、审批、接收 Diff；Agent 跑在云端 | [20](./20-ide-integration-protocol.md)；[09 §3](./09-api-clients-and-data-model.md) |
| Q11.8 | 桌面端是否需要本地文件系统访问？ | **可选**——"本地目录桥接"（双向同步沙箱工作区 ↔ 本地目录） | [20 §5](./20-ide-integration-protocol.md) 本地目录桥接 |
| Q11.9 | 是否需要离线能力？ | **v1 不做**——平台核心价值依赖云端 | [02 §5](./02-product-requirements.md) 非目标 |

## 12. 安全评估（需求稿 §12）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q12.1 | 哪些资源必须安全评估后才能共享？ | Skill（从私有发布到项目/业务组/组织）；MCP server 接入；外部官方包升级 | [06 §2](./06-capabilities-skills-mcp-subagents.md)；[06 §3](./06-capabilities-skills-mcp-subagents.md) |
| Q12.2 | 用户私有 Skill 是否也要扫描？ | **是**——创建时自动轻量扫描（静态+能力清单）；发布时完整安审 | [06 §2.4](./06-capabilities-skills-mcp-subagents.md) |
| Q12.3 | 自动扫描失败是否允许人工放行？ | **不允许**——自动扫描未通过的资源不可发布；需修改后重新提交 | [06 §2.3](./06-capabilities-skills-mcp-subagents.md) |
| Q12.4 | 安全评估由谁审批？ | 按可见域决定：项目域→项目管理员+安全组；组织域→组织安全审批人 | [06 §2.3](./06-capabilities-skills-mcp-subagents.md) |
| Q12.5 | 高风险资源是否需要双人审批？ | **是**——高风险（含脚本+出网+写 FS）需双人审批；低风险自动通过 | [06 §2.3](./06-capabilities-skills-mcp-subagents.md) |
| Q12.6 | 已发布资源发现风险后如何撤销？ | 管理员/安全组一键撤销；运行中 Agent 即时停用；通知所有使用者 | [06 §2.5](./06-capabilities-skills-mcp-subagents.md)；[11 §7.2](./11-security-and-threat-model.md) |

## 13. 运行结果与交付物（需求稿 §13）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q13.1 | 第一版最重要的交付物是什么？ | 代码 Diff + PR/MR 创建 + 文字报告 | [24](./24-delivery-and-integration-catalog.md) |
| Q13.2 | 用户是否需要下载 Artifacts？ | **是**——产物（报告、patch 文件、构建输出）可下载 | [09](./09-api-clients-and-data-model.md) |
| Q13.3 | 是否需要直接创建 PR/MR？ | **是**——Agent 可在用户确认后直接创建 PR | [24](./24-delivery-and-integration-catalog.md) |
| Q13.4 | 是否需要评论到 Issue 或工单？ | **P2+ 引入**——通过集成目录对接 Jira/Linear | [24](./24-delivery-and-integration-catalog.md) |
| Q13.5 | 是否需要人工确认后再发布结果？ | **是**（默认）——破坏性变更（PR 创建、生产部署）需人工确认 | [04 §2.1](./04-pi-integration-and-multi-llm.md) 闸门 ask |
| Q13.6 | 失败任务是否也需要生成报告？ | **是**——失败报告含：错误原因、部分产物、最后状态 | [21 §7](./21-run-lifecycle-state-machine.md) 事件映射 |

## 14. 质量、成本与复现（需求稿 §14）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q14.1 | 如何判断一个 Agent 成功完成任务？ | 基于 Agent 评估框架：任务成功率、工具调用准确率、用户评分。详见 [15 §B](./15-prompt-management-and-evaluation.md) | [15](./15-prompt-management-and-evaluation.md) |
| Q14.2 | 是否需要固定评测集？ | **是**——P3+ 建立基准测试集（Benchmark），含代码任务/安全审查/文档生成 | [15 §B.3](./15-prompt-management-and-evaluation.md) |
| Q14.3 | Agent 新版本发布前是否需要评测？ | **是**——通过 A/B 测试（灰度发布）或基准测试验证 | [15 §A.3](./15-prompt-management-and-evaluation.md) A/B 测试 |
| Q14.4 | 是否收集用户反馈？ | **是**——反馈卡片（评分 + 分类 + 自由文本）→ 自动归类 → 路由改进 | [15 §C](./15-prompt-management-and-evaluation.md) 反馈闭环 |
| Q14.5 | 是否统计 PR merge 率、人工修改率、回滚率？ | **是**——作为 Agent 效率指标（P3+） | [15 §B.1](./15-prompt-management-and-evaluation.md) 评估指标 |
| Q14.6 | 是否按组织、项目、用户、Agent 统计成本？ | **是**——四级成本归因（用户 × 项目 × 业务组 × 模型） | [08 §4.2](./08-observability-and-sse.md) 计量；[02 §3.7](./02-product-requirements.md) NFR |
| Q14.7 | 是否需要预算上限？ | **是**——按项目/业务组配预算上限；超限触发告警/阻止 | [08 §4.2](./08-observability-and-sse.md)；[02 §3.7](./02-product-requirements.md) |
| Q14.8 | Sub-agent 并行是否需要成本确认？ | **是**——发起 Run 时若 agent 可能派生 ≥3 个子 Agent，提示预估成本范围 | [06 §4](./06-capabilities-skills-mcp-subagents.md) |
| Q14.9 | 是否允许管理员限制高成本模型？ | **是**——模型白名单 + 成本上限（按项目/用户） | [04 §1.2](./04-pi-integration-and-multi-llm.md) 模型白名单 |
| Q14.10 | 超预算时是停止、降级模型，还是等待审批？ | **可配**：停止（默认）/ 降级到低成本模型 / 审批继续 | [08 §4.2](./08-observability-and-sse.md) |
| Q14.11 | 是否要求 Run 可复现？ | **尽力可复现**（best-effort）：保留完整会话 JSONL + effective config + 沙箱镜像版本；不保证模型输出的确定性 | [08 §2.3](./08-observability-and-sse.md) 回放 |
| Q14.12 | 复现需要保留哪些信息？ | 会话 JSONL + effective config 快照 + 沙箱镜像 tag + pi 版本 + 模型版本 | [08 §2.3](./08-observability-and-sse.md) |
| Q14.13 | 历史 Run 是否允许重放？ | **是**——会话回放（只读）；重跑（fork + 重新执行） | [09](./09-api-clients-and-data-model.md) |
| Q14.14 | 重放时是否允许再次调用外部写入型工具？ | **否**——回放模式仅展示历史事件；重跑模式是新的 Run，需重新过闸门审批 | [08 §2.3](./08-observability-and-sse.md) |
| Q14.15 | 历史 Artifacts 保留多久？ | 按保留策略：默认 90d，重要产物可标记保留 | [14 §1.2](./14-data-retention-and-privacy.md) 保留策略 |

## 15. 数据治理与合规（需求稿 §15）

| # | 开放问题 | 决策 | 落点文档 |
|---|---|---|---|
| Q15.1 | 数据是否允许发送给外部模型供应商？ | **默认允许（经 LLM Gateway）**；敏感项目可路由到自建 vLLM（ZDR） | [04 §1](./04-pi-integration-and-multi-llm.md)；[14 §6](./14-data-retention-and-privacy.md) |
| Q15.2 | 哪些项目必须使用私有模型或企业网关？ | 由项目配置决定——敏感项目标记 `requirePrivateModel=true`，强制走自建/私有模型 | [04 §1.2](./04-pi-integration-and-multi-llm.md) |
| Q15.3 | Prompt、Tool Result、日志、Artifact 是否需要分级脱敏？ | **是**——发射即脱敏（21 条规则覆盖密钥/PII/内部 IP/密码参数）；原始数据保留在审计存储 | [14 §5](./14-data-retention-and-privacy.md) 脱敏规则全集 |
| Q15.4 | 不同租户或项目的数据是否必须物理隔离？ | **逻辑隔离 → 可配物理隔离**：默认逻辑隔离（RBAC + 存储分区）；敏感项目可选独立存储 | [14 §2](./14-data-retention-and-privacy.md) 隔离矩阵 |
| Q15.5 | 运行日志和 Artifacts 保留多久？ | 默认 90d（可配 30d–365d）；审计日志 ≥1 年（可配到 7 年） | [14 §1.2](./14-data-retention-and-privacy.md) |
| Q15.6 | 用户是否可以删除自己的私有 Skill 和 Run？ | Skill：可删除（仅私有/draft）；Run 会话：不可删除（合规要求），仅管理员可清理过期会话 | [14 §3](./14-data-retention-and-privacy.md) |
| Q15.7 | 企业是否需要审计导出？ | **是**——支持按时间范围/项目/用户导出审计日志（JSON/CSV） | [14 §4](./14-data-retention-and-privacy.md) |
| Q15.8 | 是否有数据驻留要求？ | **取决于部署形态**：自托管/VPC 默认满足；多区域部署见 [19](./19-multi-region-deployment.md) | [19](./19-multi-region-deployment.md) |

---

## 16. 统计

| 需求域 | 开放问题数 | 已决策 | 待用户确认 |
|---|---|---|---|
| 产品定位 (§5) | 5 | 5 | 0 |
| "不是聊天机器人" (§5.2) | 4 | 4 | 0 |
| Agent 管理 (§6.1) | 6 | 6 | 0 |
| MCP 管理 (§6.2) | 7 | 7 | 0 |
| Skill 管理 (§6.3) | 7 | 7 | 0 |
| 默认配置共享 (§6.4) | 6 | 6 | 0 |
| 权限与治理 (§7) | 8 | 8 | 0 |
| 沙箱与运行环境 (§8) | 10 | 10 | 0 |
| SSE 事件流 (§9) | 8 | 8 | 0 |
| Sub-agent (§10) | 9 | 9 | 0 |
| API/桌面端/集成 (§11) | 9 | 9 | 0 |
| 安全评估 (§12) | 6 | 6 | 0 |
| 运行结果/交付物 (§13) | 6 | 6 | 0 |
| 质量/成本/复现 (§14) | 15 | 15 | 0 |
| 数据治理/合规 (§15) | 8 | 8 | 0 |
| **合计** | **114** | **114** | **0** |

> ✅ 原始需求调研稿中的 114 个开放问题均已在设计文档中做出决策。部分决策标注了"待 D9 确认"表示需用户拍板的合规目标，但不影响技术方案的完整性。

---

> 📎 **相关文档**：原始需求调研稿见 [`../requirements/cloud-agent-requirements-discovery.md`](../requirements/cloud-agent-requirements-discovery.md)；各决策的详细展开见对应落点文档。
