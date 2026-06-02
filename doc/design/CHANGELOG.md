# CHANGELOG

All notable changes to the Polaris design documents (`doc/design/`).

---

## v0.6 (2026-06-02)

### Added
- 🆕 **21-run-lifecycle-state-machine.md** — Run 生命周期状态机：UML 状态图（9 种状态 + 15 条转换）、状态定义表、各状态合法操作矩阵、6 个计时器、终态回收策略、事件映射、客户端 UI 状态映射
- 🆕 **22-gitops-configuration-as-code.md** — GitOps / Configuration as Code：仓库结构约定、Agent/MCP/Skill/策略 YAML Schema、双向同步架构（Webhook + Dry Run + 轮询）、冲突裁决规则（Git 优先）、CI Pre-Merge 校验流水线、导出与迁移、审计与回滚、与非 GitOps 模式共存策略
- 🆕 **23-requirements-traceability-matrix.md** — 需求追溯矩阵：原始需求调研稿 §5–§15 共 114 个开放问题逐条映射到设计文档决策与章节，含统计总表（15 个需求域全覆盖）
- 🆕 **24-delivery-and-integration-catalog.md** — 交付物与外部集成目录：10 类交付物清单与生命周期、L1–L5 五级集成（GitHub/GitLab → Jira/Slack/飞书 → CI/CD → 知识库）、集成安全控制、优先级路线图

### Changed
- 📝 **03-system-architecture.md** — 新增 §4 Agent 任务队列与优先级：排队模型（Mermaid）、P0–P4 五级优先级与 QoS、队列策略（权重/抢占/公平性）、准入控制、队列满行为、子 Agent 特殊处理、监控指标；原 §4–§6 顺延为 §5–§7
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
  - §10 变更日志新增 v0.6

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
