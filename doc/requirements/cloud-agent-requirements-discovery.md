# Cloud Agent 第一轮需求调研与讨论稿

日期：2026-06-02

## 1. 文档定位

本文档用于第一轮需求搜集和讨论，不是最终需求规格，也不是技术设计方案。

本文档刻意不定义以下内容：

- 数据库表结构。
- API endpoint 细节。
- 最终技术栈。
- 最终部署架构。
- 最终权限矩阵。
- 最终页面信息架构。
- 最终 SLO 数值。
- 最终 agent、MCP、skill 的资源模型。

本文档当前只做三件事：

- 汇总同类产品和开源项目的能力边界。
- 梳理 pi 可以作为基础能力候选的部分。
- 提出需要继续访谈、澄清和验证的需求问题。

## 2. 已知需求输入

用户当前提出的目标是：基于 [pi](https://github.com/earendil-works/pi) 实现一个企业级 cloud agent。

已知需求点：

- 支持多 LLM，复用 pi 相关能力。
- 支持 sub-agent。
- 支持 MCP 和 skill。
- 提供完整页面管理所有功能。
- 有具体用户权限体系。
- 管理员/项目管理员可以统一管理默认 agents、MCP、skill。
- 用户可以自定义自己的 skill。
- skill 发布需要通过安全评估。
- 产品不只是聊天机器人。
- 每个 Agent 有独立隔离环境。
- 通过 SSE 事件流实现全链路可观测。
- 对标 Codex、Claude Code 等本地个人 AI agents。
- 企业差异在于统一管理、项目/业务组共享、不重复配置、不依赖本地环境。
- 支持 API 调用、桌面端等入口。

## 3. 同类产品与开源项目调研

调研结论：目前没有找到一个开源项目完整覆盖“基于 pi + 企业统一治理 + 独立沙箱 + MCP/skill 安全发布 + sub-agent + 全链路 SSE 观测 + API/桌面端”的全部要求。

目前更像是几个方向的组合：

| 产品/项目 | 方向 | 可参考能力 | 需要注意的差异 |
| --- | --- | --- | --- |
| [OpenHands](https://github.com/OpenHands/OpenHands) | 开源 AI 软件开发 agent 平台 | 云端/本地开发 agent、沙箱、GUI、SDK、企业部署与 RBAC 方向 | 不是基于 pi；企业能力和开源边界需要进一步核实 |
| [Dify](https://github.com/langgenius/dify) | LLM app / workflow 平台 | 可视化管理、workflow、RAG、agent、API、运营后台 | 更偏 LLM 应用平台，不是 Codex/Claude Code 风格的软件工程 agent |
| OpenAI Codex | 商业云端 coding agent | 云端异步任务、独立环境、仓库任务、日志和测试证据 | 闭源 SaaS，不解决企业自托管统一治理问题 |
| Claude Code on the web | 商业云端 coding agent | 浏览器入口、云端并行任务、GitHub 集成、隔离执行 | 闭源 SaaS，不基于 pi，企业可控性有限 |
| pi 生态 | 本地/嵌入式 agent harness | 多模型、工具、事件、skill、extension、MCP adapter、sub-agent package | 需要补齐企业管理、权限、沙箱、页面、API、审计和共享能力 |

第一轮判断：

- OpenHands 是最接近“云端 coding agent + 企业部署”的参照。
- Dify 是最接近“完整管理后台 + agent/workflow 应用化”的参照。
- Codex / Claude Code 是体验和能力对标对象。
- pi 更适合作为 agent runtime 候选基础，而不是完整企业平台。

## 4. pi 能力观察

根据 pi 文档，以下能力与当前需求相关：

- SDK：pi 可以通过 SDK 嵌入其他应用，适合成为云端 runtime 的候选。
- RPC mode / JSON event stream：pi 支持通过事件流暴露 agent 运行过程，适合与平台 SSE 观测需求对齐。
- 多模型 provider：pi 支持多个模型供应商和自定义 provider，符合多 LLM 需求。
- Skills：pi 支持 Agent Skills 标准，符合用户可自定义 skill 和企业共享 skill 的方向。
- Extensions：pi extension 可以扩展工具、命令、事件和权限门禁，但也带来执行任意代码的安全风险。
- MCP adapter：pi 有 MCP adapter 包，符合接入 MCP server 的方向。
- Sub-agents：pi 有 subagents 包，可以作为多 agent 协作的能力参考。

需要继续确认的问题：

- pi SDK 和 RPC mode 哪个更适合云端多租户运行。
- pi 的事件格式是否足够稳定，是否需要平台侧做事件标准化。
- pi skill 与企业 skill 审核/发布/撤销流程如何衔接。
- pi extension 是否允许进入企业共享能力池，还是先只允许受控内置 extension。
- pi MCP adapter 的 tool 暴露方式是否适合企业权限治理。
- pi sub-agent 的上下文继承、并发、失败处理、成本控制是否满足企业场景。

## 5. 产品边界待澄清

### 5.1 目标用户

需要明确第一批用户是谁：

- 普通研发。
- Tech Lead / 架构师。
- 项目管理员。
- 业务系统运营人员。
- 安全审核人员。
- 平台管理员。
- 通过 API 调用的内部系统。

待讨论问题：

- MVP 首先服务研发场景，还是也覆盖业务自动化场景？
- 项目管理员和组织管理员是否是同一批人？
- 安全审核是否必须独立于项目管理员？
- 用户是否需要像使用本地 Codex/Claude Code 一样直接操作代码仓库？
- 非研发用户是否需要低代码/工作流式入口？

### 5.2 产品不是聊天机器人

当前需求明确“不只是聊天机器人”。需要进一步定义“不是聊天机器人”意味着什么。

候选理解：

- Agent 可以运行长任务，而不是只回复消息。
- Agent 可以读写代码、调用工具、生成 artifacts。
- Agent 可以被 API、Webhook、CI 触发。
- Agent 可以作为团队共享能力，而不是个人聊天会话。
- Agent 可以有任务状态、审批、审计、成本和结果交付。

待讨论问题：

- 第一版是否必须支持后台异步任务？
- 是否需要任务模板，例如“修复 CI”“审查 PR”“生成迁移方案”？
- 是否需要 workflow 视图，还是先从单 agent run 开始？
- 成果物以什么形式交付：文字报告、patch、PR、文件、工单、API response？

### 5.3 对标对象

当前对标包括 Codex、Claude Code 等个人 AI agents，但企业场景强调统一管理。

需要拆开讨论：

- 本地个人 agent 哪些能力必须保留？
- 云端企业平台哪些能力必须新增？
- 哪些本地体验不应该复制到企业平台？

候选对标维度：

- 任务委派能力。
- 代码仓库理解能力。
- 工具调用能力。
- 运行环境隔离能力。
- 实时进度可视化。
- 多 agent 并发。
- 结果可审计和可复现。
- 团队共享配置。

## 6. 企业统一管理需求域

### 6.1 Agent 管理

需要讨论 agent 在平台中到底是什么。

可能包含：

- 名称、描述、用途。
- 默认模型或模型策略。
- 默认工具。
- 默认 MCP。
- 默认 skills。
- 运行权限。
- 可见范围。
- 版本和发布状态。
- 适用项目或业务组。

待讨论问题：

- agent 是由平台统一创建，还是用户也可以创建私有 agent？
- 私有 agent 是否能提交为项目共享 agent？
- agent 是否必须版本化？
- agent 是否允许绑定多个默认 skill/MCP？
- agent 是否允许用户运行时临时覆盖模型、skill、MCP？
- 项目默认 agent 和组织默认 agent 冲突时如何处理？

### 6.2 MCP 管理

MCP 是企业能力接入的重要入口，也可能是高风险入口。

需要讨论：

- 谁可以添加 MCP server？
- MCP server 是组织级、项目级，还是用户私有级？
- MCP 工具是整体授权，还是按 tool 授权？
- 读操作和写操作是否区分权限？
- MCP 认证使用项目凭证、用户凭证，还是服务账号凭证？
- MCP 调用是否必须记录到事件流和审计日志？
- 外部 MCP 和内部 MCP 是否采用不同审核流程？

### 6.3 Skill 管理

当前需求明确：用户可以自定义自己的 skill，发布 skill 需要安全评估。

需要讨论：

- 用户私有 skill 是否只允许自己使用？
- 用户私有 skill 能否访问项目资源？
- skill 发布到项目和发布到组织是否需要不同审批？
- skill 是否允许包含脚本、二进制、依赖、外部网络访问？
- skill 安全评估由自动扫描、人工审批，还是两者结合？
- skill 被撤销后，历史 run 如何展示？
- skill 是否需要版本化和变更记录？

### 6.4 默认配置共享

当前需求强调同一项目/业务组人员可以共同使用，不需要重复配置。

需要讨论：

- 共享范围是项目、业务组、组织，还是多层级？
- 默认 agents/MCP/skills 由谁配置？
- 用户能否隐藏或禁用项目默认配置？
- 用户私有配置能否覆盖项目默认配置？
- 项目默认配置变更是否影响历史 run？
- 新成员加入项目时是否自动获得默认配置？

## 7. 权限与治理待澄清

第一轮不建议固化角色矩阵，但需要明确权限对象和权限动作。

可能的权限对象：

- 用户。
- 组织。
- 业务组。
- 项目。
- Agent。
- MCP。
- Skill。
- 运行任务。
- 沙箱。
- 代码仓库。
- Secret。
- Artifact。
- 审批单。

可能的权限动作：

- 查看。
- 创建。
- 编辑。
- 发布。
- 审批。
- 运行。
- 停止。
- 复用。
- 分享。
- 导出。
- 删除。
- 调用工具。
- 访问 secret。
- 查看原始日志。
- 查看脱敏日志。

待讨论问题：

- 是否需要组织、业务组、项目三级管理？
- 项目管理员是否可以管理所有项目资源？
- 安全审核人员是否独立于项目管理员？
- 用户能否查看同项目其他人的 run？
- 用户能否复用他人的 run 结果或 artifacts？
- 谁可以查看原始工具调用参数？
- 谁可以查看模型输入输出的完整内容？
- 服务账号是否需要单独权限模型？

## 8. 沙箱与运行环境待澄清

当前需求明确“每个 Agent 一个隔离环境”。第一轮需要先明确隔离边界，而不是立即确定实现方式。

需要澄清：

- “每个 Agent”是指每个 agent 定义、每次 root run，还是每个 sub-agent run？
- sub-agent 是否必须独立环境？
- 沙箱是否需要访问代码仓库？
- 沙箱是否需要访问公网？
- 沙箱是否需要访问内部服务？
- 沙箱是否需要 GPU 或特殊硬件？
- 沙箱运行结束后是否保留现场？
- 用户是否需要进入沙箱调试？
- 依赖缓存是否允许项目共享？
- 是否需要不同语言/框架的环境模板？

风险点：

- 独立环境会提高安全性，但会增加启动延迟和成本。
- 共享缓存会提升性能，但会带来隔离和污染风险。
- 允许公网访问会提升可用性，但会带来数据泄露风险。
- 保留沙箱现场有助于复现，但会增加存储和合规压力。

## 9. SSE 事件流与可观测待澄清

当前需求明确需要 SSE 事件流和全链路可观测。

需要讨论事件覆盖范围：

- Agent 思考和消息输出。
- 工具调用开始、过程、结束。
- Shell 命令和退出码。
- 文件读写。
- MCP 调用。
- Skill 加载和拒绝。
- Sub-agent 创建、进度和结束。
- 沙箱创建、准备、销毁。
- 审批请求和审批结果。
- 成本和 token 使用。
- 错误、重试、超时。
- Artifact 生成。

待讨论问题：

- 用户界面需要展示哪些事件？
- 管理员需要展示哪些事件？
- 安全审核人员需要展示哪些事件？
- 哪些事件需要脱敏？
- 是否保留原始事件？
- SSE 是否只是页面实时展示，还是也给 API 用户消费？
- 断线重连是否必须恢复历史事件？
- 是否需要跨 sub-agent 的统一 trace？

## 10. Sub-agent 需求待澄清

当前需求明确支持 sub-agent，但需要明确 sub-agent 的业务价值。

可能场景：

- planner 规划任务。
- worker 执行任务。
- reviewer 审查结果。
- researcher 查资料。
- security reviewer 检查风险。
- 多个 worker 并行处理不同模块。
- 后台 agent 继续监控长任务。

待讨论问题：

- sub-agent 是用户显式选择，还是由 agent 自动编排？
- 是否允许用户创建自定义 sub-agent？
- sub-agent 是否能使用不同模型？
- sub-agent 是否能使用不同 MCP/skill？
- sub-agent 是否有独立权限？
- sub-agent 的结果如何汇总？
- sub-agent 失败时 root agent 如何处理？
- 是否需要展示 sub-agent 树或时间线？
- 并发和成本是否需要用户确认？

## 11. API、桌面端与集成待澄清

当前需求包括 API 调用和桌面端。

API 方向待澄清：

- API 是只创建任务，还是也管理 agents/MCP/skills？
- API 是否支持流式事件消费？
- API 是否支持服务账号？
- API 是否需要回调 Webhook？
- API 是否用于 CI/CD？
- API 是否需要 OpenAPI 文档和 SDK？

桌面端方向待澄清：

- 桌面端是完整控制台，还是开发者快捷入口？
- 桌面端是否需要本地文件系统访问？
- 桌面端是否只是远端任务客户端？
- 桌面端是否需要把远端 patch 应用到本地仓库？
- 桌面端是否需要离线能力？

外部集成待澄清：

- GitHub/GitLab。
- Jira/Linear。
- Slack/飞书/企业微信。
- CI/CD。
- 内部知识库。
- 内部权限系统。
- 内部审计系统。

## 12. 安全评估需求待澄清

当前明确 skill 发布需要安全评估。第一轮需要明确评估对象是否只限 skill。

可能需要评估的对象：

- Skill。
- MCP server。
- MCP tool。
- Agent。
- Extension。
- Sandbox template。
- 外部 package。
- Workflow。

可能的安全评估维度：

- 是否访问 secret。
- 是否访问文件系统。
- 是否访问公网。
- 是否调用内部系统。
- 是否执行脚本。
- 是否引入依赖。
- 是否包含可疑 prompt。
- 是否可能绕过权限。
- 是否可能泄露数据。

待讨论问题：

- 哪些资源必须安全评估后才能共享？
- 用户私有 skill 是否也要扫描？
- 自动扫描失败是否允许人工放行？
- 安全评估由谁审批？
- 高风险资源是否需要双人审批？
- 已发布资源发现风险后如何撤销？

## 13. 运行结果与交付物待澄清

Agent 的价值最终体现在交付物。

可能的交付物：

- 文字总结。
- 代码 diff。
- Patch 文件。
- Pull Request / Merge Request。
- 测试报告。
- 运行日志。
- 截图。
- 生成文件。
- 工单更新。
- API response。
- 可复现运行记录。

待讨论问题：

- 第一版最重要的交付物是什么？
- 用户是否需要下载 artifacts？
- 是否需要直接创建 PR/MR？
- 是否需要评论到 issue 或工单？
- 是否需要人工确认后再发布结果？
- 失败任务是否也需要生成报告？

## 14. 质量、成本与复现待澄清

### 14.1 质量

需要讨论：

- 如何判断一个 agent 成功完成任务？
- 是否需要固定评测集？
- agent 新版本发布前是否需要评测？
- 是否收集用户反馈？
- 是否统计 PR merge 率、人工修改率、回滚率？

### 14.2 成本

需要讨论：

- 是否按组织、项目、用户、agent 统计成本？
- 是否需要预算上限？
- sub-agent 并行是否需要成本确认？
- 是否允许管理员限制高成本模型？
- 超预算时是停止、降级模型，还是等待审批？

### 14.3 复现

需要讨论：

- 是否要求 run 可复现？
- 复现需要保留哪些信息？
- 历史 run 是否允许重放？
- 重放时是否允许再次调用外部写入型工具？
- 历史 artifacts 保留多久？

## 15. 数据治理与合规待澄清

需要讨论：

- 数据是否允许发送给外部模型供应商？
- 哪些项目必须使用私有模型或企业网关？
- prompt、tool result、日志、artifact 是否需要分级脱敏？
- 不同租户或项目的数据是否必须物理隔离？
- 运行日志和 artifacts 保留多久？
- 用户是否可以删除自己的私有 skill 和 run？
- 企业是否需要审计导出？
- 是否有数据驻留要求？

## 16. 第一轮需求访谈问题清单

### 16.1 产品定位

- 第一版优先解决哪个场景：编码任务、业务自动化、运维任务，还是知识工作？
- 最关键的用户角色是谁？
- “完整页面管理所有功能”中，哪些页面是第一版必须有？
- 企业共享配置的最小可用范围是项目还是业务组？

### 16.2 Agent 能力

- Agent 是否必须能修改代码？
- Agent 是否必须能创建 PR/MR？
- Agent 是否必须支持后台长任务？
- Agent 是否必须支持用户中途干预？
- Agent 是否必须能调用内部系统？

### 16.3 MCP 和 Skill

- MCP 是平台管理员配置为主，还是项目管理员配置为主？
- 用户私有 MCP 是否允许？
- 用户私有 skill 是否默认可运行？
- skill 发布到项目需要谁审批？
- skill 发布到组织需要谁审批？

### 16.4 权限与安全

- 是否已有企业 SSO 和组织/项目结构来源？
- 是否已有安全审核流程？
- 哪些操作必须人工审批？
- 哪些日志用户自己可以看，哪些只有管理员能看？
- 是否允许 agent 访问生产系统？

### 16.5 运行环境

- 第一版是否必须完全云端执行？
- 是否允许依赖用户本地环境？
- 沙箱是否需要访问公网？
- 沙箱是否需要访问内部网络？
- 是否需要为不同项目维护不同环境模板？

### 16.6 API 和桌面端

- API 第一版服务谁：内部系统、CI/CD，还是外部客户？
- 桌面端第一版的核心价值是什么？
- 桌面端是否需要本地仓库集成？
- 是否需要 Webhook 通知任务完成？

## 17. 后续文档建议

建议将后续工作拆成几份互相独立的文档，避免第一轮讨论被单一技术方案锁定：

- `requirements-discovery.md`：继续维护开放问题、调研结论、候选需求。
- `personas-and-scenarios.md`：用户角色、场景、用户旅程。
- `capability-map.md`：能力地图和优先级。
- `security-and-governance-questions.md`：安全、权限、审批、审计问题。
- `mvp-scope.md`：在需求确认后再定义 MVP 范围。
- `architecture-options.md`：在需求确认后再比较架构选项，而不是提前给出单一方案。

## 18. 参考资料

- pi GitHub: https://github.com/earendil-works/pi
- pi SDK: https://pi.dev/docs/latest/sdk
- pi RPC Mode: https://pi.dev/docs/latest/rpc
- pi JSON Event Stream Mode: https://pi.dev/docs/latest/json
- pi Skills: https://pi.dev/docs/latest/skills
- pi Extensions: https://pi.dev/docs/latest/extensions
- pi Providers: https://pi.dev/docs/latest/providers
- pi Development: https://pi.dev/docs/latest/development
- pi MCP Adapter: https://pi.dev/packages/pi-mcp-adapter
- pi Subagents: https://pi.dev/packages/pi-subagents
- OpenHands GitHub: https://github.com/OpenHands/OpenHands
- Dify GitHub: https://github.com/langgenius/dify
- OpenAI Codex announcement: https://openai.com/index/introducing-codex/
- Claude Code on the web: https://claude.com/blog/claude-code-on-the-web
