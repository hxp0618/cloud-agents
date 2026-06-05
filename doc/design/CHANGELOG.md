# CHANGELOG

All notable changes to the Polaris design documents (`doc/design/`).

---

## v0.6.2 (2026-06-05)

> 沙箱与凭据层设计增补（源自对 LAP / `kubernetes-sigs/agent-sandbox` 的调研对标），**无新增文档、无破坏性改动**。两项增补：K8s 沙箱后端改为 reconcile 上游 `Sandbox` CR；出网代理与密钥下发合并为「凭据代理 sidecar + 桩凭据」模型。

### Changed
- 📝 **07-sandbox-isolation.md §2** — Workspace 后端抽象新增 §2.1「K8s 后端：reconcile `Sandbox` CR」：`ContainerWorkspace`/`MicroVMWorkspace` 不再自建 K8s 编排，改为复用上游 [`kubernetes-sigs/agent-sandbox`](https://github.com/kubernetes-sigs/agent-sandbox)（SIG Apps，`agents.x-k8s.io/v1alpha1`）的 `Sandbox` CRD + 控制器；`Workspace` 接口映射到 CR 操作；原生能力映射表（`runtimeClassName` gVisor/Kata、`SandboxTemplate`=沙箱画像、`SandboxWarmPool`=预热池冷启<1s、定时删除=回收、pause/resume=[21 §4] P3+ 缺口）；附 `Sandbox` CR 示意 YAML；标注 `v1alpha1` 成熟度核定（直接依赖 vs 借鉴 CR 设计）
- 📝 **07-sandbox-isolation.md §3** — 新增 §3.1「凭据代理 sidecar」：将原本分开的「出网白名单代理」与「密钥间接下发」**合并为一个 sidecar**，采用更强的**桩凭据**模型——harness 只持 `stub_*`，真凭据仅活于 sidecar 进程内存、出网时换值，Agent 永不持有真凭据（即便 bypass-permissions 也偷不到）；明确与 [04 §4.2] LLM Gateway 短令牌路径的关系（推广到 git/MCP/包仓库等所有出网凭据）；凭据分轨（`GIT_TOKEN` 用后抹除 vs `GITHUB_TOKEN` 持续）+ 保留环境变量；⚠️ 待核定 HTTPS 在 wire 上换值的 TLS 终止（MITM/内部 CA）机制。同步 §3 解剖图、§1 原则表、§5 画像→`SandboxTemplate`、§6 生命周期（warm pool/定时删除/pause-resume）、§7 加固清单、§8 交叉引用
- 📝 **11-security-and-threat-model.md** — 凭据外泄控制全面升级 + 新增「外部内容注入」攻击面：
  - §2 信任边界图 + 表：B5/B6 收敛到「凭据代理 sidecar」（egress + 桩→真换值；真 key 仅注入 sidecar）
  - §3.4 新增 STRIDE 行「间接提示注入（外部内容）」——Agent 抓取的网页/仓库/issue/MCP 结果/文档携带面向 LLM 的注入指令
  - §3.5 数据外泄控制补「凭据零持有——外传也带不走真 key」
  - §3.6 B6 凭据子表重写（桩凭据模型 / 凭据外泄 / 凭据残留进程隔离 / 凭据映射滥用）
  - §4.2 内部攻击面新增「外部内容注入」行
  - §5 控制矩阵：「密钥泄露」控制升级为「桩凭据 + sidecar wire 换值」；新增「间接提示注入（外部内容）」行
  - §6 残余风险：R2 拓宽到 Agent 主动抓取的外部内容；新增 **R9「凭据代理 sidecar 被攻破」**（桩模型集中化的代价）
  - §9.4 MITRE T1552 缓解更新为「桩凭据（Agent 无真 key）+ sidecar 换值」
  - §10 需求对应补凭据零持有 / 外部内容注入条目
- 📝 **03-system-architecture.md §5** — 技术栈表「沙箱编排」行补注 K8s 后端复用 `kubernetes-sigs/agent-sandbox`（`Sandbox` CRD + 控制器），转交指向 [07 §2.1](./07-sandbox-isolation.md)
- 📝 **09-api-clients-and-data-model.md §1.3** — 新增「Run 创建输入校验（per-session env）」：≤50 keys / ≤16KB / key 正则 / 保留键名单 → 400（落实 [07 §3.1] 的前向引用，借鉴 LAP per-session env 约束）
- 📝 **README.md** — 版本号 v0.6.1 → v0.6.2；§7 文档导航 07 词条更新（Workspace 后端 reconcile `Sandbox` CR + 凭据代理 sidecar）；§10 变更日志新增 v0.6.2

### Note
- 本轮源自对 LAP（LiteLLM Agent Platform）文档与 `kubernetes-sigs/agent-sandbox` 上游的调研对标。**安全提示**：调研中发现 LAP 文档页内嵌面向 LLM 的注入指令（要求 fetch 外部 `llms.txt`），已抽象为「外部内容注入」威胁纳入 §3.4 / §4.2 / R2——这正是本平台 Agent 自身会遇到的攻击面。

---

## v0.6.1 (2026-06-05)

> 评审修订：对 v0.6 文档套件做内部一致性与正确性修复，**无新增文档**。

### Fixed
- 🔧 **08-observability-and-sse.md §3** — 事件分类（taxonomy）补全为名副其实的"共同 schema"：新增 `memory.*`（`loaded/appended/cleared`，跨 Run 记忆生命周期）与 `delivery.*`（`pr_created/comment_posted/issue_updated`，交付物与外部集成产出），并给 `run.*` 补 `timeout_warning`——消除 04/07/21/24 已引用但 08 未登记的事件缺口
- 🔧 **21-run-lifecycle-state-machine.md** — 修正审批拒绝语义：T7 由 `paused_for_approval → failed` 改为 `→ running`（普通 `deny` 仅挡此工具、返回 `{block:true}`，Agent 走替代路径继续运行）；`deny_and_abort` 与用户取消 → `cancelled`。同步 §1 状态图、§3 转换表、§5 计时器、§7 事件映射。与 [02 用户故事7]、[03 时序]、[04 §2.1 闸门]、[16 §3.3 deny vs deny_and_abort] 对齐。**状态数（11）与转换数（15）保持不变**
- 🔧 **21 §4** — 标注 `pause`/`resume` 为 P3+ 规划能力且当前 v1 状态机未建模 `paused` 状态；澄清 `failed` 行的 `resume` 实为 fork 重跑而非原地恢复
- 🔧 **22-gitops-configuration-as-code.md** — 可见域字段统一：`scope` → `visibility`、`organization` → `org`（含 `managedBy`），对齐 RBAC 源头文档 [05](./05-rbac-and-governance.md)，消除与 README §8「作用域(Governance Scope) vs 可见域(Visibility Scope)」的二义性；§2.1 ToolPolicy 的 `defaultMode` 从 `rules` 列表项移至与 `rules` 同级
- 🔧 **23-requirements-traceability-matrix.md** — Q9.8 `parentRunId` → `parentSessionId`（与 02/06/12 统一）；引言「~50 个开放问题」→「114 个」（与 §16 统计一致）
- 🔧 **20-ide-integration-protocol.md / README.md** — IDE 集成阶段标签 `P5+` → `P3+`，与文档 20 §9 路线图（P3 原型 / P4 完整 / P5 JetBrains）一致
- 🔧 **07-sandbox-isolation.md** — "如何阅读"角色导航 §编号错位修正（架构师/平台工程师路径 §1/§2 → §2/§3）
- 🔧 **04 / 24** — 错别字：04 §3.1「归归属模型」→「归属模型」；24 §9 表头繁体「优先級」→「优先级」

---

## v0.6 (2026-06-02)

### Added
- 🆕 **21-run-lifecycle-state-machine.md** — Run 生命周期状态机：UML 状态图（11 种状态 + 15 条转换）、状态定义表、各状态合法操作矩阵、6 个计时器、终态回收策略、事件映射、客户端 UI 状态映射
- 🆕 **22-gitops-configuration-as-code.md** — GitOps / Configuration as Code：仓库结构约定、Agent/MCP/Skill/策略 YAML Schema、双向同步架构（Webhook + Dry Run + 轮询）、冲突裁决规则（Git 优先）、CI Pre-Merge 校验流水线、导出与迁移、审计与回滚、与非 GitOps 模式共存策略
- 🆕 **23-requirements-traceability-matrix.md** — 需求追溯矩阵：原始需求调研稿 §5–§15 共 114 个开放问题逐条映射到设计文档决策与章节，含统计总表（15 个需求域全覆盖）
- 🆕 **24-delivery-and-integration-catalog.md** — 交付物与外部集成目录：10 类交付物清单与生命周期、L1–L5 五级集成（GitHub/GitLab → Jira/Slack/飞书 → CI/CD → 知识库）、集成安全控制、优先级路线图

### Changed
- 📝 **03-system-architecture.md** — 新增 §4 Agent 任务队列与优先级：排队模型（Mermaid）、P0–P4 五级优先级与 QoS、队列策略（权重/抢占/公平性）、准入控制、队列满行为、子 Agent 特殊处理、监控指标；原 §4–§6 顺延为 §5–§7；三面职责描述精确化（控制面"无状态"限定、客户端"无 provider 裸 key"限定）
- 📝 **04-pi-integration-and-multi-llm.md** — 密钥流向统一：provider 裸 key 只在 LiteLLM/Vault 侧解析，Polaris Gateway 只传 virtual key + 路由元数据
- 📝 **08-observability-and-sse.md** — 事件 taxonomy 新增 `run.*` 命名空间；`permission.*` 事件与 21 对齐（新增 `prompted/resolved/denied`）
- 📝 **09-api-clients-and-data-model.md** — API 代理模式端点补全（`/v1/chat/completions`、`/v1/messages`、`/v1/api-keys`）；cancel/abort 统一为 cancel
- 📝 **11-security-and-threat-model.md** — K8s PodSecurityPolicy → Pod Security Admission（PSP 已在 v1.25 移除）；README 链接修正（`../README.md` → `./README.md`）
- 📝 **06-capabilities-skills-mcp-subagents.md** — 修复 [10 §4.4] 断链（10 无 §4.4）
- 📝 **README.md** — 状态机数量修正（9 状态 → 11 状态）
- 🔧 修复 03 新增 §4（任务队列）导致的跨文档 section 引用漂移：19 (`03 §5→§6`)、22 (`05 §3→§4`)、23（Q6.11/Q7.8/Q8.4/Q8.5/Q8.9/Q9.7/Q11.3/Q14.11/Q14.12/Q14.14 共 12 处）；第二轮核查修复 23 中 27 处语义错位（05 §3→§4 可见域/继承、05 §2→§3.1 权限矩阵、06 §2.x 子节漂移、08 §2→§3/§5 等）
- 📝 **11-security-and-threat-model.md §9** — 合规映射全面深化：
  - SOC 2 TSC 逐条映射（CC1–CC9 + A/C/PI/P 共 22 条）
  - ISO 27001:2022 附录 A 技术控制映射（20 条 + 成熟度评估）
  - 等保 2.0 三级差距分析（7 个控制域）
  - MITRE ATT&CK 关键 Tactic/Technique 映射（13 条）
  - 合规成熟度总览表（5 框架 + 覆盖度 + 缺口 + 认证建议阶段）
- 📝 **16-notification-system.md §4.4** — Webhook 签名验证全面展开：HMAC-SHA256 签名生成/验证流程（含 TypeScript 示例代码）、防重放机制（timestamp + delivery ID 缓存）、与 GitHub/GitLab webhook 格式的兼容性说明
- 📝 **README.md**:
  - 版本号 v0.5 → v0.6
  - §6 需求覆盖矩阵从 11 项扩充为 27 项（4 个子表：原始需求/安全合规/运维可靠性/平台工程化）
  - §7 文档导航新增 21–24 四篇
  - §8 术语约定新增 6 条（沙箱/sandbox、产物/artifact/交付物、熔断/撤销、GitOps/CaC）
  - §9 名词表新增 2 条（Run 状态机、GitOps 同步）
  - 阅读指南更新（新增架构师/产品经理/平台工程师路径）
  - §9 名词表术语修正：`可见域 (Scope)` → `可见域 (Visibility Scope)`，新增 `作用域 (Governance Scope)` 条目，消除"Scope"一词的二义性
  - §10 变更日志新增 v0.6
- 🔧 全文档（01–10、21–24）新增 `💡 如何阅读` 角色导航：每篇文档末尾补充按角色（架构师/安全评审/SRE/产品/前端/平台工程师/集成工程师）的推荐阅读路径，与 11–20 风格一致

---

## v0.5 (2026-06-02)

### Added
- 🆕 **17-cost-optimization.md** — 成本优化策略：Prompt Caching、语义缓存、模型分层路由（haiku/sonnet/opus）、沙箱预热池/复用/Spot 实例、存储分层、综合成本模型
- 🆕 **18-capacity-planning.md** — 容量规划指南：单沙箱资源画像、数据面/控制面节点估算公式、Orchestrator 瓶颈分析、自建 vLLM 容量、快速参考卡（POC→企业）
- 🆕 **19-multi-region-deployment.md** — 多区域部署架构：中心辐射拓扑、数据驻留、跨区域一致性（CP/AP）、DR 策略、合规路由（EU/APAC）、延迟预算
- 🆕 **20-ide-integration-protocol.md** — IDE 插件集成协议：VS Code/JetBrains 插件架构、OAuth PKCE 认证、对话面板/内联 Diff/审批对话框、本地目录桥接、与 Continue/Cursor 对比

### Changed
- 📝 **CHANGELOG.md** — 从 README 中独立出变更日志文件
- 📝 **README.md** — 新增 §8 术语约定（安审/作用域/闸门等统一）；🔧 `pi-mcp-adapter`/`pi-subagents` 版本标注为调研时版本；✅ 全文档交叉引用审核通过（35/35 有效）

---

## v0.4 (2026-06-02)

### Added
- 🆕 **13-operations-manual.md** — Day-2 运维手册：故障恢复（Orchestrator 崩溃 / 会话同步）、备份 RPO/RTO、沙箱资源治理、滚动升级/drain 策略、健康检查与 SLO 告警、运维 Runbook
- 🆕 **14-data-retention-and-privacy.md** — 数据留存与隐私：D1–D5 数据分类 + R1–R∞ 保留策略、GDPR 被遗忘权（删除/匿名化 + SLA）、跨作用域隔离矩阵、审计防篡改（哈希链/默克尔树）、脱敏规则全集（21 条）、合规框架映射（GDPR/SOC2/数据驻留）
- 🆕 **15-prompt-management-and-evaluation.md** — Prompt 工程化：版本管理（不可变版本 + 模板变量 + 切换/回滚）、A/B 测试（灰度发布 + Auto-Promote/Rollback）、Prompt 护栏（Input/Sentry/Output 三明治）；Agent 评估框架（5 维指标 + 自动评估 + LLM-as-Judge + 基准测试集）；用户反馈闭环（反馈卡片 → 自动归类 → 路由改进）
- 🆕 **16-notification-system.md** — 通知系统：5 类场景（Run 生命周期 / 审批协作 / 配额成本 / 安全事件 / 系统通知）、审批超时 4 级升级链、5 渠道（In-app WebSocket/SSE、Email、Slack、Webhook、PagerDuty）、通知引擎 7 步流水线、Digest 聚合、用户偏好与静默时段

### Changed
- 📝 **02-product-requirements.md** §3 NFR — 从 9 行定性描述扩展为 10 张量化指标表，每项含 T/M/S 三级目标值 + 适用阶段
- 📝 **10-roadmap-and-open-questions.md** — 风险表新增安全威胁与测试覆盖 2 条风险；下一步建议关联新文档
- 📝 **README.md** — 文档导航 + 阅读指南覆盖 16 篇文档；新增 §8 术语约定

---

## v0.3 (2026-06-02)

### Added
- 🆕 **11-security-and-threat-model.md** — 统一安全架构：8 条信任边界全景图、STRIDE 威胁模型（逐边界分析）、攻击面枚举（外部/内部/供应链）、安全控制矩阵（18 条威胁→控制→验证）、8 项残余风险（R1–R8）+ 接受建议、安全事件响应（P0–P3 + SLA + 自动响应 + 流程）、安全开发生命周期 (SDL)、合规映射初版
- 🆕 **12-testing-strategy.md** — 测试策略：测试金字塔 + 覆盖目标、单元测试范例（权限/策略/能力清单/脱敏/RPC）、集成测试（API/闸门/沙箱）、E2E 用户旅程、安全测试用例集（沙箱逃逸 20+/提示注入 10+/安审对抗 20+/密钥泄露 7 条）、兼容性测试（pi RPC snapshot）、SSE 鲁棒性、性能/压力/混沌工程、CI/CD 流水线设计

---

## v0.2 (2026-06-02)

### Added
- Initial 10-design-doc suite:
  - **01-research-and-landscape.md** — pi 能力深挖 + 同类产品调研 + 对比表 + 差异化
  - **02-product-requirements.md** — 角色画像 + FR-1~11 + NFR（初始定性版）+ 用户故事
  - **03-system-architecture.md** — 控制面/数据面/客户端三分 + 运行时序 + 技术栈 + 部署拓扑
  - **04-pi-integration-and-multi-llm.md** — RPC 驱动 + 平台扩展 + 权限闸门 + LLM Gateway + 会话
  - **05-rbac-and-governance.md** — 四级作用域 + 角色权限矩阵 + 可见域继承锁定 + 中央目录
  - **06-capabilities-skills-mcp-subagents.md** — 三层信任边界 + Skill 安审流水线 + MCP 治理 + 子 Agent 治理
  - **07-sandbox-isolation.md** — Workspace 抽象 + 一 Agent 一沙箱 + 双层防御 + 生命周期
  - **08-observability-and-sse.md** — 事件溯源 + 事件信封/分类 + SSE 扇出 + 审计计量
  - **09-api-clients-and-data-model.md** — REST/SSE API + 控制台 IA + 多端客户端 + ER 数据模型
  - **10-roadmap-and-open-questions.md** — P0–P5 路线图 + MVP 定义 + 风险 + 待决策项 + 成功指标
- **README.md** — 总体导航、关键决策摘要、需求覆盖矩阵、名词表

---

## Versioning Policy

- **Major (X.0)** — 架构性变更或新增核心设计文档（≥3 篇）
- **Minor (0.X)** — 新增 1-2 篇文档或对现有文档的重大改写
- **Patch** — 术语修正、引用修正、小幅增补

Design docs are **living documents** — they evolve with implementation feedback. Each version is a snapshot of design decisions at a point in time.
