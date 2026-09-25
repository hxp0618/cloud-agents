# 07. Admin Web 需求与交互设计

2026-09-24 Admin 继续按真实证据投影：r542 的版本/digest、跨租户和旧 generation 拒绝只显示 opaque capability 元数据、稳定状态码与 Operation/Audit，不显示任何请求内容；r545 的 Pi×RemoteWorker `capabilityBound=true` 仅显示版本/digest、权限、兼容性、绑定关系、attempt、脱敏 RTO/RPO/计数与 recovery 状态。r544/r546 的进程故障超时/未形成 checkpoint 只保留 fail-closed 的稳定失败状态和审计关联，不能显示为 supported 或 recovery passed，也不得展示节点地址、主机路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。细节与日志 digest 见 [06](06-status-tracker.md)。

r548–r550 的 Admin 投影继续只允许稳定 opaque 结果：Codex transport fault 的错误成功只显示未通过/审计关联；r549 Claude `provider_unavailable` 与 Pi 未产生 MCP call 只显示稳定失败码、Provider/环境、版本/digest、绑定关系和 Operation/Audit。不得显示 transport 请求、Prompt、源码、工具输入输出、MCP 返回内容、节点地址、主机路径或 Secret，也不得把未到达的 transport/revocation 阶段显示为 supported。

2026-09-22 r467/r468/r469/r471/r472/r476/r480/r482/r483/r490/r491 的 Admin 投影继续沿用 opaque 最小边界：只展示 capability opaque ID、版本、digest、权限、兼容 Provider、状态、绑定关系、脱敏 attempt/recovery mode/RTO/RPO/计数及 Operation/Audit。版本/digest 不兼容、跨租户 `401`、旧 generation `409`、`awaiting_reconciliation/side_effect_outcome_unknown` 和未计数前置失败只保留稳定状态码与审计关联；不得显示 Prompt、Skill 源码、工具输入输出、MCP 返回内容、Secret、Token、主机路径、节点/Pod/namespace、Worker 地址或代理变量。`capabilityBound=true` 只代表该 candidate/Provider/环境已经有真实接管证据：r482/r480/r483 的三格 Kubernetes cross-node 与 r467 的 Codex 同节点必须分开显示，不能推断其它格或把受控 gpt-5.5 结果显示为默认模型支持。

四 Provider×Docker/RemoteWorker/Kubernetes 的正向/撤销子路径可按真实日志逐项显示 `passed`/`revoked`（撤销负向 `12/12`）；但完整格仍须等 Pi/deepseek Worker/Agent restart、Codex Kubernetes cross-node、完整断连重连和恢复后 Operation/Audit 对账证据；r473/r474/r475/r489 不得显示为成功。Admin 不读取用户主机任意路径，也不把 Secret 写入 Workspace Snapshot、日志、Event 或 Artifact。

2026-09-22 r463 的 Pi×Kubernetes 同节点 recovery 只允许进入既有 opaque 投影：显示 capability ID、版本/digest、权限、兼容性、绑定关系、attempt、脱敏恢复模式与 RTO/RPO/计数、状态和 Operation/Audit；不得显示 namespace、Pod/node、主机路径、凭据、代理变量、Prompt、Skill 源码、工具输入输出或 MCP 返回内容。`capabilityBound=true` 仅代表该 candidate/Provider/环境的真实同节点证据，不得推断 Kubernetes cross-node、Worker/Agent 或完整 supported；其它未验收矩阵继续显示 unsupported/未验收。

2026-09-21 r459 的 Admin 投影继续只允许 opaque capability ID、版本、digest、权限、兼容性、绑定关系、Operation/Audit、脱敏恢复模式与计数；Codex×Docker/RemoteWorker/Kubernetes 的 MCP/Skill outcome 可按真实验收状态显示，RemoteWorker `not-applied`/reconcile 只显示稳定状态码，不显示 side-effect 详情。不得展示 Worker/节点地址、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret；Codex Kubernetes cross-node 仍显示未验收，不能把 r467 同节点结果升级为 cross-node。

r459 的 Codex×Kubernetes 单节点正向/撤销结果可在同一 opaque 投影中显示（版本/digest、权限、绑定关系、Operation/Audit、`passed`/`revoked` 状态和脱敏计数）；由于 recovery 未形成 checkpoint，Admin 不得显示 Kubernetes recovery 或 cross-node supported。诊断只保留稳定失败码/未计数 Operation，不显示 PostgreSQL 错误细节、Pod/namespace/node、主机路径、Prompt、源码、工具输入输出、MCP 返回内容、代理变量或 Secret。

r459 当前 candidate 的 Pi×Docker 与 deepseek-harness×Docker 正向/撤销结果可在同一最小 opaque 投影中显示：只显示 capability opaque ID、版本、digest、权限、兼容性、绑定关系、`passed`/`revoked` 状态、脱敏计数和 Operation/Audit；不显示 Provider 凭据、代理变量、连接端点、Skill 路径/源码、Prompt、工具输入输出或 MCP 返回内容。该投影只覆盖本轮真实 Docker 子路径；RemoteWorker/Kubernetes、重启/跨节点仍按各自日志逐格显示，不能推断完整十二格 supported。

r459 的 Pi×RemoteWorker 与 deepseek-harness×RemoteWorker capability-bound cross-node 结果沿用同一最小投影：Admin/Operation/Audit 只显示 opaque ID、版本、digest、权限、兼容性、绑定关系、attempt、恢复模式、状态和脱敏 RTO/RPO/计数；不显示 DIND/Worker 地址、namespace、Pod/node、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret。`capabilityBound=true` 只表示该 Provider/环境/候选的真实接管证据，不能推断 Kubernetes、Worker/Agent 或完整 supported。

r459 的 Pi×Kubernetes 与 deepseek-harness×Kubernetes 正向/撤销子路径可显示 opaque MCP/Skill outcome、版本/digest、权限、兼容性、绑定关系、脱敏计数和 Operation/Audit；r480/r483 在受控 gpt-5.5 fixture 上的 cross-node takeover 只可显示对应 candidate/model 的 `passed` 与脱敏 RTO/RPO，不能升级为默认 gpt-5.6-luna 或所有模型支持。诊断只保留稳定失败码/未计数 Operation，不显示 namespace/Pod/node、主机路径、Prompt、源码、工具输入输出、MCP 返回内容、代理变量或 Secret。

r448 的 Claude×RemoteWorker 能力绑定跨节点证据沿用最小 opaque 投影：Admin/Operation/Audit 只展示 capability ID、版本、digest、权限、兼容性、绑定关系、attempt、恢复模式、状态和脱敏 RTO/RPO/计数；不展示 DIND/Worker 地址、namespace、Pod/node、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret。r446/r447 的 fail-closed 诊断只保留稳定失败码/未计数 Operation，`capabilityBound=true` 也不得推断其它 Provider、环境或完整 supported 状态。

Pi/deepseek-harness 的 r402/r403 capability 测试因物料配置错误未产生成功证据，Admin 不得据此显示成功；修正物料后的 r405 Pi×Docker、r406 deepseek-harness×Docker 已产生真实 opaque MCP/Skill outcome、Artifact 和事件续读，Admin 仅可显示 opaque ID、版本/digest、权限、状态、兼容性、绑定关系与 Operation/Audit，不显示 Prompt、源码、工具输入输出、MCP 返回内容或 Secret。无能力绑定的 Pi r404 baseline 仍不替代 MCP/Skill 验收；两个 Docker 子路径不代表 RemoteWorker/Kubernetes 或完整十二格通过。
最新 r407/r408 RemoteWorker 结果沿用相同 Admin 最小投影；Admin 只显示 opaque capability outcome、版本/digest、绑定关系、Operation/Audit 和 `capabilityBound=false` 的脱敏状态，不显示节点地址、endpoint、主机路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。
r409/r410 Kubernetes 结果继续沿用相同 opaque 投影；Admin 只显示 Provider/环境、版本/digest、绑定关系、Operation/Audit、恢复状态和 `capabilityBound=false`，不显示 namespace/Pod/node、endpoint、主机路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。
r414 Claude×Docker 与 r415 Claude×Kubernetes 单节点结果可在同一最小投影中显示 opaque MCP/Skill outcome、版本/digest、绑定关系、Operation/Audit、`capabilityBound=true` 的同节点恢复状态；r412 Pi recovery 失败、r413 Claude Kubernetes 首次失败及未覆盖的跨节点/Worker/Agent 组合不得显示为支持。

r422 Claude Docker Worker+Agent 组合的恢复探针随后收到 `provider_unavailable` 并 fail closed，Admin 只能保留脱敏 Operation/Audit 失败状态，不得把该轮显示为完整组合支持。

r423 的 Claude×Docker Worker/Agent 组合已完整通过；Admin 可显示 opaque MCP/Skill outcome、版本/digest、绑定关系、Operation/Audit 及脱敏的同节点恢复状态，仍不得展示 Prompt、源码、工具输入输出、MCP 返回内容或 Secret；其它环境和未通过组合继续隐藏为 unsupported/未验收。

r427 的 Pi×RemoteWorker 同节点恢复可沿用相同最小投影：只显示 opaque MCP/Skill outcome、版本/digest、绑定关系、Operation/Audit 和脱敏恢复状态；不显示节点地址、主机路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret，跨节点/Kubernetes 未验收组合不得显示为支持。

r428 的 deepseek-harness×RemoteWorker 同节点恢复沿用相同最小投影；Admin 只显示 opaque ID、版本/digest、权限、状态、兼容性、绑定关系和 Operation/Audit，不显示节点地址、主机路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret，跨节点/Kubernetes 未验收组合不得显示为支持。

r429 的 Pi×Kubernetes 单节点恢复沿用相同最小投影；Admin 只显示 opaque ID、版本/digest、权限、状态、兼容性、绑定关系和 Operation/Audit，不显示 namespace/Pod/node、主机路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret；Kubernetes cross-node 未验收，不得显示为支持。

Admin 仅记录本次 Codex×Docker Worker recovery 的 opaque capability 元数据、Operation/Audit 和通过状态；未通过的 Provider/环境组合不显示为支持。

Worker recovery 诊断仍只展示脱敏类别和长度，不展示 MCP 返回内容、工具输入输出、主机路径或 Secret；其它未通过的 Worker/Provider 组合，Admin 不得显示为已支持。

2026-09-20 增量：r384 Claude×Docker 真实子路径通过 MCP/Skill/Artifact/事件续读、同节点 Control Plane SIGKILL 恢复和撤销负向；Admin/User browser smoke 仍为 1440/390、双主题、User→Admin 403。随后 Claude×RemoteWorker，以及 Codex×Docker/RemoteWorker/Kubernetes 的 MCP/Skill/事件续读、撤销和恢复子路径也真实通过；r387 又通过 Codex Docker Agent Runtime 进程退出后的 capability-bound recovery（MCP/Skill/事件续读、撤销负向）；r417、r418 分别补齐 Pi×Docker 和 deepseek-harness×Docker capability-bound process-restart/reconcile。独立 Docker-only retry3 仍通过，但完整 Claude×Kubernetes retry4 在 Docker `file_change=failed` 阶段未形成完整 Artifact/recovery，保持 fail closed。当前 candidate 的 Docker/Kubernetes/RemoteWorker contract-negative、跨租户 401 和 stale-generation 1→2/409 真实通过，但不显示为 Provider 能力；Codex Worker recovery、Claude Kubernetes、Kubernetes 跨节点、四 Provider Worker/Agent 重启和其它 Provider 的完整组合仍不显示为通过。Admin/Audit 仍只保留 opaque ID、版本/digest、权限、状态、绑定关系与 Operation/Audit，不把未完成的其它组合显示为通过。失败诊断仍不增加路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、主机路径或 Secret。该证据不关闭十二格；其它 Provider、Worker 重启、Kubernetes 跨节点和逐项负向仍开放。制品、命令、清理和未覆盖项统一见 [06](06-status-tracker.md)。

- 状态：产品边界与既有视觉/双语/安全要求有效；新增技术细节为实施设计，未宣称已实现或验收
- 日期：2026-09-05（基础设施＋Admin 联合交付；原需求日期 2026-09-03）
- 原实现参考：`codex/cloud-agents-platform-p0@a6dd495`；当前代码与验收须逐次核对，不能沿用此 ref 声称当前完成
- Daytona 参考基线：[`daytonaio/daytona@v0.190.0`](https://github.com/daytonaio/daytona/tree/v0.190.0)
- 前端技术栈：Vite + React + TypeScript

## 1. 文档目的

本文定义 Cloud Agents 的 User Web 与 Admin Web 产品边界，并给出 Admin Web 的功能、信息架构、
权限、安全、交互、接口和验收要求。

按 [ADR-0032 / D-055](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)，长期云工作区、通用 Sandbox、客户节点与完整 Admin Web 共同组成第一阶段；完整用户 CloudAgents 对话在其后接入。
本次更新产品对应关系和实施排序，不把所有 UI 细节、实现或部署标为已批准，也不改既有视觉和安全要求。

Admin Web 的页面布局、视觉风格、组件外观、响应式行为和交互反馈以 Daytona `v0.190.0` Dashboard 为
固定复刻基线。Cloud Agents 只替换品牌和业务资源语义，不复制 Daytona 的后端资源模型，也不把用户对话
与基础设施管理合并到同一个 Dashboard。

## 2. 产品目标

依据 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)，基础设施与 Admin Web 是第一阶段的同一个完整交付对象；Admin 不是可后补附件。用户对话界面属于第二阶段。每个面向管理员的能力都需真实后端、对应页面流程和验收；两边缺任一闭环均未完成。

平台提供两个独立入口：

1. **User Web** 面向使用 Codex、Claude Code 等 Coding Agent 的普通用户，以对话和工作结果为中心。
2. **Admin Web** 面向平台管理员，以 Workspace/Sandbox、执行目标、资源池和客户节点的配置、
   监控、维护为中心，不要求存在 Agent 会话才可使用。

管理员准备基础设施和已验证的 RuntimeProfile/模板/策略，普通用户通过基础 API/SDK/CLI 选择公开规格、
创建或连接自己的 Workspace/Sandbox。后续 CloudAgents 再在其上创建 AgentSession。
现有 Agent `Environment Profile` 保留兼容；底座规格不强制绑定 Codex/Claude 或 Provider 凭据。

## 3. 核心原则

### 3.1 用户与管理员界面分离

- User Web 与 Admin Web 是两个独立构建、独立部署、独立路由和独立鉴权入口。
- 两者共享 Cloud Agents 品牌，但 Admin Web 使用 Daytona `v0.190.0` 的视觉与布局；不得共享页面导航或
  依赖前端隐藏按钮实现权限隔离。
- Admin Web 使用专用 Admin API 和管理员 OIDC audience/scope；普通租户 Token 不能调用 Admin API。

### 3.2 用户只消费环境规格

User Web 不出现以下输入项或可编辑字段：

- Docker API endpoint；
- Kubernetes API endpoint、kubeconfig 或 ServiceAccount 信息；
- SSH host、端口、用户名、私钥或 host key；
- `credentialRef`、`providerCredentialRef`；
- Worker 镜像、release digest；
- 底层 CPU、内存、存储、网络和调度实现参数。

用户只选择管理员发布的产品规格，并查看摘要和可用状态；后续可包括已开放的 region、安全等级和资源套餐。
这些是产品选择，不是底层 endpoint、凭据或 runtime 配置输入。底座 Workspace API/CLI 的内容访问权限只授予
对应用户，不因此将 Terminal/Files 放入管理员运维界面。

### 3.3 内容与基础设施隔离

Admin Web 在当前底座阶段始终不能读取或搜索：

- 用户对话内容和 Prompt；
- Session/Turn 的消息正文；
- Workspace 文件内容和源代码；
- Artifact 文件内容；
- Codex、Claude Code 等 Provider 凭据；
- Docker、Kubernetes、SSH 凭据原文。

管理员可以看到用于运维的 opaque ID、租户/项目归属、资源状态、时间、generation、错误码、资源用量和
审计事件。当前底座阶段不设计管理员查看用户内容的 break-glass 通道。

### 3.4 凭据只以引用出现

- Admin Web 可以创建或选择 `credentialRef`，但不能读取引用背后的 Secret bytes。
- API 响应和审计日志只能返回凭据引用、类型和可用状态，不返回私钥、Token、kubeconfig 或 Provider JSON。
- 前端不得把凭据值写入 `localStorage`、`sessionStorage`、URL、日志或错误详情。

### 3.5 Control Plane 保持资源权威

- 浏览器不直接访问 Docker API、Kubernetes API 或 SSH host。
- 注册、Probe、部署、升级、Drain、Cleanup 都通过 Admin API 提交给 Control Plane。
- UI 只展示服务端返回的 desired/observed state、generation、operation 和 stable error code。

### 3.6 Admin Web 视觉 1:1 复刻 Daytona

- Daytona `v0.190.0/apps/dashboard` 是 Admin Web 布局与视觉的唯一参考版本，不跟随 Daytona 后续版本。
- 复刻范围包括应用壳层、侧边栏、顶部区域、页面标题、内容宽度、资源表格、卡片、Tabs、Sheet、Dialog、
  Dropdown、表单、按钮、Badge、Tooltip、分页、空状态、加载状态、错误状态和明暗主题。
- 字体层级、字号、行高、间距、圆角、边框、阴影、颜色、hover/focus/active/disabled 状态和响应式断点均
  应从固定 tag 的源码或运行截图提取，不使用“相似即可”的自由设计。
- Cloud Agents 品牌、文案、图标语义和资源名称替换 Daytona 对应内容；Daytona logo、商标文案和品牌素材
  不进入产品。这些品牌替换不视为视觉偏差。
- User Web 不要求复刻 Daytona，继续以 Conversation 和本地 Coding Agent 使用体验为中心。

### 3.7 Admin Web 中英文切换

- Admin Web 最少支持简体中文 `zh-CN` 和英文 `en-US`，所有导航、标题、表单、按钮、状态、确认、Toast、
  空状态、错误提示和可访问性文本必须覆盖两种语言。
- 首次访问按浏览器语言选择：中文环境使用 `zh-CN`，其他环境使用 `en-US`；管理员手动选择后持久化该
  非敏感偏好，后续访问优先使用显式选择。
- 语言切换入口放在 Daytona 风格的账户/设置区域，不改变 Dashboard shell 布局。
- 日期、时间、数字和相对时间使用浏览器原生 `Intl`；资源 ID、stable error code、日志原文和 API 字段名
  不翻译。
- 缺失或无效 locale 回退到 `en-US`；缺失翻译在测试中失败，生产界面不能直接显示 message key。

## 4. 范围

### 4.1 User Web

下列现有 CloudAgents User Web 能力保留，新增产品工作安排到 APP-M1。底座阶段用有权用户的 CLI/SDK
完成通用 Workspace/连接验收，不要求先做完整用户应用，也不删除既有功能：

- Project 与 Workspace 上下文；
- Codex / Claude Code Agent 选择；
- Conversation、Session、Turn、Execution；
- 流式事件与执行状态；
- Approval；
- User Input；
- Cancel 与 Interrupt；
- Artifact 列表、预览和下载；
- 已发布 `Environment Profile` 的选择；
- 用户可理解的环境准备、运行、失败和释放状态。

### 4.2 Admin Web

Admin Web 负责：

- Docker / Kubernetes / SSH Deployment Target 注册与配置；
- 凭据引用绑定；
- 连通性检测与能力探测；
- Kubernetes cluster、Docker/SSH host 和 Worker 运行状态；
- Worker 镜像与 Runtime/Provider release；
- `Environment Profile` 草稿、校验、发布、禁用与版本；
- CPU、内存、并发和租户/项目资源配额；
- Workspace 存储和网络策略；
- Environment Lease 运维视图；
- Worker/Target 升级、Drain、恢复调度和清理；
- 失败 Operation、稳定错误码和审计记录。
- 简体中文与英文界面切换。

随底座契约与执行器交付，增加 Workspace/Volume/Snapshot 元数据、Sandbox 生命周期、访问 Grant/Port 状态、
RemoteWorker 注册与能力/身份状态、Region/Pool 容量与调度结果。不得先创建空页面或仅保存不生效的策略后
广告为可用；不支持的功能必须在 API admission 拒绝，并在页面明确说明。

### 4.3 不在当前范围

- Billing、Wallet、Spending；
- Marketplace；
- 用户聊天内容审查；
- 管理员源代码浏览器；用户自己的 Files API 不在此禁止范围；
- Provider 凭据明文查看或下载；
- 在浏览器中直接打开 Docker socket、kubeconfig 或 SSH shell；
- 因参考 Daytona 而复制其 Organization、Sandbox 或 Runner 后端模型；
- 尚未进入对应 BASE 阶段时创建空的 Region/Pool/Node 页面，或因为页面导航而额外拆微服务。

## 5. Daytona 参考映射

Daytona `v0.190.0` Dashboard 包含 Sandboxes、Snapshots、Volumes、Regions、Registries、Runners、Limits 和
Audit Logs 等资源页面，并为部分页面设置 owner/permission gate。Cloud Agents Admin Web 完整复刻该版本
的页面壳层、视觉系统、资源页面结构和操作反馈，再按下表替换业务资源语义。

| Daytona `v0.190.0` 概念 | Cloud Agents Admin Web | 处理方式 |
| --- | --- | --- |
| Sandboxes | SandboxSession；旧 Environment Lease 兼容视图 | 只做运维视图，不展示对话或代码，不把 Lease 改名冒充新模型 |
| Runners | RemoteWorkers / 执行 Workers | 分开节点与实例角色，展示注册、版本、心跳、容量和 Drain 状态 |
| Regions | Region / ResourcePool / Deployment Target | 单 Region 也可有多个池；随真实调度模型/API 交付 |
| Registries | Images & Releases | 管理 Worker 镜像、Runtime/Provider release 和 digest |
| Snapshots / Volumes | Workspace Volumes / Snapshots / Storage Policies | 区分真实卷/快照及其声明策略；按 BASE 阶段交付，不读取内容 |
| Limits | Quotas | 管理并发 Lease、CPU、内存和存储上限 |
| Audit Logs | Audit | 记录管理员操作和资源状态变化 |
| Lifecycle actions | Probe / Upgrade / Drain / Cleanup | 使用异步 Operation、状态反馈和影响确认 |

完整复刻的边界：

- 不沿用单个 Dashboard 同时承载用户工作流与管理员工作流的边界；
- 不复制 Billing、Wallet、Spending、Webhook 等当前无关页面；
- 不复制 Daytona API、数据库模型或鉴权模型；
- 不复制 Daytona logo、商标文案和品牌素材；
- 不因视觉复刻而直接复制 Daytona 前端源码，或引入 Next.js、Ant Design、Tailwind 和新的组件框架。

## 6. 角色与权限

### 6.1 角色基线

| 角色 | User Web | Admin Web |
| --- | --- | --- |
| `platform-user` | 使用对话、执行和 Artifact；选择已发布 Profile | 无访问权限 |
| `platform-operator` | 可作为普通用户使用自己的项目 | 查看基础设施；执行 Probe、Drain、Upgrade、Cleanup |
| `platform-admin` | 可作为普通用户使用自己的项目 | 注册和配置 Target、Profile、配额、存储网络策略及管理员权限 |
| `platform-auditor` | 无额外用户内容权限 | 只读查看资源元数据、Operation 和 Audit |

### 6.2 权限要求

- 前端路由守卫只用于用户体验；最终授权必须由 Admin API 执行。
- 列表、详情和每个写操作分别校验 scope，不能因为能进入 Admin Web 就获得全部权限。
- 危险操作至少区分 `target.write`、`target.probe`、`lease.operate`、`worker.drain`、
  `release.upgrade`、`cleanup.execute`、`profile.publish`、`quota.write` 和 `audit.read`。
- 被拒绝时返回稳定的 403 Problem，不泄露资源是否存在或 Secret 信息。

## 7. 信息架构

### 7.1 User Web 导航

```text
Projects
└── Conversations
    ├── Session / Turn / Execution
    ├── Approvals & User Input
    ├── Artifacts
    └── Environment Profile selector
```

基础设施页面从 User Web 删除。环境选择放在“新建 Conversation/Session”流程中，运行后只展示 Profile 名称、
版本、用户规格摘要和环境状态。

### 7.2 Admin Web 导航

```text
Overview
Infrastructure
├── Deployment Targets
├── Clusters & Workers
├── Environment Profiles
├── Images & Releases
├── Quotas
└── Storage & Network
Operations
├── Environment Leases
├── Maintenance
└── Audit
```

以上导航是已有兼容骨架。BASE-M1 增加 Workspace/Sandbox 元数据视图，BASE-M2 增加访问/策略状态，
BASE-M3 增加节点级 RemoteWorker，BASE-M4 增加 Region/Pool，BASE-M5 增加真实 Snapshot/Restore 和用量。
只在后端契约与行为存在时启用对应导航，不要求等到多 Region 才管理单 Region 的真实资源池。

## 8. 页面需求

### 8.1 Overview

目标：管理员进入系统后立即看到可用性和待处理故障，而不是营销信息。

必须展示：

- Target 总数及 `ready`、`probing`、`unavailable` 数量；
- Worker 在线、过期、draining、失败数量；
- Lease 的 provisioning、ready、terminating、failed、cleanup blocked 数量；
- 最近失败 Operation；
- 待升级 Worker/Lease 数量；
- 最近管理员操作。

所有卡片都可跳转到已带过滤条件的资源列表。

### 8.2 Deployment Targets

#### 列表

默认列：Name、Kind、Location/Labels、Observed Phase、Engine/API Version、OS/Architecture、Generation、
Last Probe、Active Leases、Actions。

支持按 kind、phase、label、location 过滤，支持按名称和 opaque ID 搜索。默认隐藏完整 endpoint；需要权限的
管理员可在详情页查看脱敏后的 endpoint 元数据。

#### 注册

统一注册流程只收集当前后端实际需要的字段：

- Target name；
- Target kind：`docker`、`kubernetes`、`ssh`；
- endpoint；
- `credentialRef`；
- 可选 location/labels（后端支持后启用）。

提交成功后 Target 初始为 `unprobed`，页面引导管理员执行 Probe，不把“注册成功”显示为“可部署”。

#### 详情

详情页包含：

- Overview：身份、kind、generation、phase 和探测事实；
- Capacity & Workers：关联 Worker、Lease 和资源占用；
- Configuration：endpoint 元数据和 credential reference；
- Operations：Probe、Drain/Resume、Upgrade、Cleanup；
- Audit：该 Target 的管理员操作和状态变化。

Docker、Kubernetes、SSH 使用同一详情骨架，只展示各自存在的探测事实，不创建三套独立页面。

### 8.3 Clusters & Workers

#### Cluster/Host 视图

- Kubernetes Target 展示 cluster API/version、namespace/工作负载摘要和 Worker 状态；
- Docker Target 展示 engine version、OS/architecture 和 Worker container 摘要；
- SSH Target 展示远端 Docker/运行时探测结果和 Worker 摘要；
- 不提供任意远程命令终端。

#### 执行 Worker 列表（已有 Lease 兼容视图）

默认列：Worker ID、Target、Lease、Release Digest、Generation、Heartbeat、State、CPU/Memory、Started At。

允许操作：

- 查看运行元数据和最近错误；
- Drain，停止接收新任务；
- Resume，恢复接收任务；
- Upgrade 到已批准 release；
- 对已终止且失联的残留 Worker 发起 Cleanup。

Worker 页面不能展示 Session/Turn 消息或 Workspace 文件。

#### RemoteWorker 节点（BASE-M3）

- 与 Lease 内执行 Worker 分开标识；一个节点可以承载多个 Sandbox，不以 leaseId 作为节点身份。
- 展示 ownerScope、Region/Pool、incarnation、runtime/arch/容量能力、心跳、证书有效期、版本和 Drain 状态。
- 支持创建 enrollment 意图、撤销未使用注册、节点 Drain/Resume、轮换/吊销和受控升级；
  一次性注册 Secret 由授权 CLI 领取，Admin Web 不显示、保存或重放 Secret bytes。
- 离线显示不可新调度和待对账状态；不能用“离线”推断 Sandbox 已删除或触发 Workspace 卷清理。

### 8.4 Images & Releases

- 列出允许部署的 Worker image、Runtime/Provider 版本、OCI digest、支持架构和验证状态；
- Profile 只能引用已批准且 digest 固定的 release；
- Upgrade 页面必须展示当前 digest、目标 digest、受影响 Target/Worker/Lease 数量和回滚目标；
- 不接受仅使用可变 tag 作为已发布 Profile 的执行输入。

P0 可以从已有 release manifest 读取，暂不建设完整镜像仓库代理。

### 8.5 Environment Profiles

`Environment Profile` 是管理员发布给用户的安全规格，不等同于 Deployment Target。

下表保留现有 Agent Profile 契约。底座 RuntimeProfile/模板按 02 的模型单独定义，不要求 `providerKinds`
或 `providerCredentialRef`；不得在新后端未交付时仅改页面标签，宣称现有 Profile 已支持通用 Sandbox。

#### 最小字段

| 字段 | 用户可见 | 说明 |
| --- | --- | --- |
| `id`、`name`、`version` | 是 | 不可变版本身份 |
| `description` | 是 | 面向用户的规格说明 |
| `status` | 是 | `draft`、`published`、`disabled` |
| `providerKinds` | 是 | 支持 Codex、Claude Code 等 |
| `cpuLimitMillis`、`memoryLimitBytes` | 仅规格摘要 | 用户不可编辑 |
| `storagePolicyRef`、`networkPolicyRef` | 仅摘要 | 用户看描述，不看底层配置 |
| `releaseDigest` | 可选展示短值 | 用户不可编辑 |
| `targetSelector` / `targetRefs` | 否 | Control Plane 调度输入 |
| `providerCredentialRef` | 否 | 服务端绑定，API 不向 User Web 返回 |

#### 生命周期

```text
draft -> published -> disabled
   \--------> deleted（仅从未发布且未被引用）
```

- 发布后版本不可原地修改；变更生成新版本。
- User Web 只列出 `published` 且当前可调度的版本。
- `disabled` 版本不能创建新 Lease，但不自动中断已有 Lease。

### 8.6 Quotas

管理员按平台、租户或项目设置：

- 最大并发 Lease；
- 最大并发 Execution；
- CPU、内存、存储上限；
- Lease 最大 TTL；
- 单次 Artifact 和总保留上限。

User Web 只显示可理解的额度和超限原因，不提供修改入口。P0 不实现计费。

### 8.7 Storage & Network

#### Storage Policy

- Workspace 存储类型与容量；
- 生命周期和保留期限；
- 新 Workspace 的独立保留/删除策略；旧 Lease 终止清理规则只用于原有绑定，不自动迁移；
- Snapshot/Artifact 后端引用；
- 是否允许复用已有 Workspace。

#### Network Policy

- 默认 egress 策略；
- 允许的域名/CIDR 或策略引用；
- ingress/preview 是否启用；
- DNS 和代理策略引用。

User Web 只能看到管理员编写的摘要，例如“8 GB workspace，允许公共互联网访问”，不能看到内部网络、
Secret 或 endpoint 配置。

容量、保留、网络及 snapshot backend 必须展示已验证执行能力；不能让固定 20Gi 卷对应任意容量摘要，
也不能把已保存的 deny/preview 配置显示成已执行。禁用不支持的发布/创建组合，并保留稳定错误码。

### 8.8 Environment Leases

Lease 页面用于基础设施运维，不是 Conversation 页面。

默认列：Lease ID、Tenant/Project、Profile、Target、Generation、Desired/Observed/Cleanup Phase、Worker、
Expires At、Stable Error Code。

详情只展示：

- 资源身份和归属；
- Profile、Target、Worker 绑定；
- generation 和 release digest；
- 生命周期时间线；
- 资源用量和稳定错误码；
- Upgrade、Terminate、Cleanup Operation。

不得展示 Prompt、对话消息、代码、Artifact 内容或 Provider 凭据。

### 8.9 Maintenance

集中展示异步运维操作：Probe、Provision、Upgrade、Drain、Resume、Terminate、Cleanup。

每条 Operation 至少包含：

- operation ID 和 idempotency key；
- resource ID、kind 和 generation；
- requested by、requested at；
- state：queued、running、succeeded、failed、cancelled；
- 当前步骤和稳定错误码；
- 影响范围摘要；
- 可用时的 Retry 操作。

### 8.10 Audit

记录以下事件：

- Target 注册、修改、Probe、Drain、Resume、Cleanup；
- Profile 创建、发布、禁用；
- 配额和存储网络策略修改；
- Worker/Lease 升级、终止和清理；
- 管理员权限变化；
- 被拒绝的管理员写操作。

Audit 事件包含操作者、动作、资源引用、generation、结果、时间和 request/operation ID；不得包含凭据值、
Prompt、消息正文、代码或 Artifact 内容。

### 8.11 长期 Workspace 与 Sandbox（BASE-M1/M2）

- Workspace 列表/详情只展示 opaque ID、归属、保留策略、Volume 绑定、状态和容量；不展示 Repo 内容、文件目录树或对话。
- Sandbox 视图区分长期 Workspace、临时计算实例与旧 Lease，显示 desired/observed state、generation、placement、Operation、失败原因和重试结果。
- Stop/TTL/Cleanup 的影响清单明确保留长期数据。Workspace/Volume 删除走独立权限、保留策略与明确影响确认；不能把 Admin 维护权限变为浏览用户 Files/PTY 的权限。

### 8.12 资源池与快照管理（BASE-M4/M5）

- Region/Pool/Node 只展示真实后端能力、容量与调度结果；不支持的 Runtime/network/storage 组合明确禁用并说明原因。
- Snapshot 仅展示归属、source Workspace、时间、容量、状态与恢复 Operation；不得读取快照内容。恢复须验证一致性和单写者 fencing。
- 复用 Storage、Maintenance、Audit 现有页面承载相关视图；有真实 API 和闭环再增加专门页面，不预建空壳。

### 8.13 Anywhere Runtime 运维（APP-M1）

本节仅用于后续 ANYWHERE-RUNTIME-V1，不向已完成的 BASE-ADMIN-V1 或旧 ADMIN-WEB-V1 追加条件。
复用 Runtime/Profile、Workspace/Sandbox、Worker、Maintenance、Operation/Audit 页面，不新增对话查看器或空壳页面。

- 显示真实 Provider/Harness release、兼容能力、关联 Workspace/Sandbox、placement、执行 attempt/generation、
  心跳、恢复阶段和失败原因；明确区分同节点重连、进程重启与跨节点接管。
- Checkpoint/Snapshot 只展示 opaque 引用、一致性点时间、版本兼容、摘要校验状态与恢复 Operation；
  不展示 Prompt、源码、对话、工具输入输出、原生 Provider cursor 或 Secret bytes。
- 提供相应权限下的 Drain、停止、失败对账和恢复/重试操作；显示影响范围、源/目标节点和数据保留结果。
  危险操作继续要求资源名称/generation 确认；没有 fencing、有效恢复点或目标容量时禁止强制挂载和盲目重放。
- 副作用结果未知时显示脱敏原因并指向有权用户的处理流程，不能由 Admin 读取内容或代替用户批准工具动作。
  恢复结果、拒绝原因及旧 attempt 回执处置须可在 Operation/Audit 追溯，沿用 zh-CN/en-US、主题与可访问性要求。

### 8.14 MCP 与 Skill 运维（MCP-SKILL-RUNTIME-V1）

- Admin Web 通过生成 SDK 读取当前租户/项目的 MCP Server 与 Skill Bundle；列表和详情只展示 opaque ID、版本、digest、MCP transport/权限、Skill 兼容 Provider、只读挂载、绑定关系、状态以及 Operation/Audit 元数据。
- Admin 不展示或搜索 Prompt、Skill 源码、工具输入输出、MCP 返回内容、连接端点、主机路径、Token 或其他 Secret；分页、错误和跨租户响应仍由 Control Plane 权威返回。
- 撤销、版本变更和绑定调整必须携带幂等键及 expected resource version，并在 capability durable event 与现有 Operation/Audit 中可追溯；Runtime 重新解析失败时页面显示 fail-closed 原因，不提供盲目重试外部副作用的按钮。
- Runtime broker、Skill materializer 与 Provider 共享入口在实际注入或 Provider 启动前拒绝 opaque ID 归一化后的 MCP Token/Skill mount 环境变量名碰撞；Admin 只显示对应 opaque binding 与脱敏失败码，不显示环境变量名或 Token。
- Control Plane 与 Worker 都要求 operator-owned capability descriptor 在首个 JSON 值后立即 EOF；尾随第二个值按无效物料 fail closed，Admin 只显示脱敏失败状态，不回显 descriptor、凭据或签名材料。
- 当前页面已接入只读 opaque 元数据列表、项目级 Session/Execution 绑定关系，以及由现有 Managed Agent event stream 驱动的 capability Operation/Audit 表；表内只显示脱敏 admission、显式 opaque MCP outcome、解析拒绝及其 result。Runtime 已具备签名/digest 校验的只读 Skill Bundle materializer，operator-only 生成器已能生成受保护租户物料；候选 `0.3.0-dev.251` 的真实 Compose Admin Web smoke 已验证 MCP/Skill 列表最小 scope、重启和备份恢复，但没有 Provider 凭据，因此只证明管理链，不证明 Provider 格。该历史候选的 Codex Host-owned dynamic bridge 与 `mcp_servers={}` 结果已由后续 `.353/.354` 的原生 Host-managed MCP/结构化 Skill 实现取代；真实 Docker 模型调用仍以 discovery-only/exit 1 记录为准并 fail closed。Pi Skill 子路径虽已真实通过，但未进入 Control Plane/Worker event stream，因此 Admin 的真实 Provider 结果链和十二格验收仍未完成，不得以静态能力目录或页面存在标记支持。
- Candidate `0.3.0-dev.253` 已补充 Claude Docker 的真实 MCP Provider 子路径：Host-managed MCP 的不可预知结果经过 fresh approval、真实 `tools/call` 和 completed event 后由 Agent 原样返回；Runtime digest 为 `sha256:1f9a0b81606141d31e436cf32239b5e364ceebae7a2a93a8da85db0ef8f22516`。这次直连 Runtime 探针不经过 Control Plane/Worker，所以 Admin 仍不得展示该工具输入、返回内容或 Secret，也不得把它冒充持久 Operation/Audit 或整格验收；只有后续经真实 event stream 写入的 opaque succeeded/failed/revoked 结果才能进入本页。
- Candidate `0.3.0-dev.257` 已把同一 operator-owned capability descriptor 只读接到 Compose Control Plane 与 Worker，并给 Worker 增加显式 materialization 参数和 Skill `tmpfs`。真实 capability smoke 的 User/Admin 浏览器与 MCP/Skill catalog 创建通过，但 Claude execution 仍返回 `runtime_start_failed`，没有可供 Admin 展示的 `skill.load`、`mcp.call` 或 Provider outcome；fixture 又未启动其引用的 MCP upstream，因此页面不得把 catalog 存在或该失败冒充运行支持。脚本清理后测试资源为零。
- Candidate `0.3.0-dev.281` 已产生首条真实 Control Plane/Worker Claude capability 结果链：同一 execution 完成 fresh approval、一次 MCP 外部副作用、一次签名 Skill 加载、文件 Artifact、`mcp.call`/`skill.load`/`execution.complete` 持久事件和断线续读；Admin 仍只显示 opaque identity、状态和计数，不显示 Token、Header、请求体、MCP 返回内容、marker 或 Skill 源码。其后 follow-up 暴露共享 Skill 路径的进程清理竞态，现已改为每个 Runtime 进程独立只读 Skill 根并通过重叠进程回归；candidate `0.3.0-dev.282` 的真实重跑被 Worker image 外部 `npm install` 构建失败阻断，所以页面不得把该修复、单次正向链或 Claude×Docker 整格标为 supported，failed/revoked、重启恢复和其余十一格继续开放。
- deepseek-harness `0.1.2-rc.1` 的 adapter 已核实使用真实 `dsh-mcp-client`/`dsh-skill-filesystem` Cordis patch，并把 dsh custom root 精确指向受管 bundle 的 `skills/` 子目录；Admin 仍只应展示 opaque binding、版本/digest、权限、兼容性、状态与 Operation/Audit，不展示 dsh patch、Token 环境变量、连接端点、Skill 路径或工具结果。当前又有一条固定 `deepseek-v4-pro` 的独立 Docker Runtime 预检真实完成 `mcp.call`/`skill.load`、原生写文件和 ArtifactCandidate，使用本机 distribution artifact `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`（12,774,391 bytes）；fixture 仅记录 8 条脱敏 HTTP/RPC method，EOF 后 Skill 根为空，测试容器/临时凭据目录/live pointer 为 `0`。provider 已按真实 `skill({name})` 通知为 MCP 与单一 Skill Bundle 产生 opaque ID，Control Plane 可据此写入脱敏 succeeded/failed outcome；多 Bundle 无公开名称映射时不猜。该事件补丁仅有 TS/Go 定向测试，预检未经过 Control Plane/Worker，Admin 不得把它显示成真实持久 Operation/Audit 或十二格支持；原 `gpt-5.6-luna` 路由在无 capability baseline 即 malformed tool-call fail closed，故只登记机制和兼容性边界。
- 同候选的任务自有 kind v1.37.0 Helm smoke 已补充验证 Kubernetes Admin/User Web 与部署运维链（Admin 18 请求、User 6 请求、普通用户 Admin 403、重启、CA/服务身份轮换、无 Agent/Provider Secret），随后精确删除 kind/context/测试资源；没有 Provider 凭据，所以这不改变 Admin 的 Provider 结果链和十二格验收边界。
- 未发布 candidate `0.3.0-dev.283` 的真实 Compose smoke 在补齐固定 `node@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5` 后 exit 0，完成迁移、Capability/Admin 列表、User/Admin 浏览器隔离、过期 grant 负向、Control Plane/Access Gateway 重启、备份恢复和精确清理；Admin 继续只显示 opaque 元数据与脱敏状态。本轮无 Provider 凭据，不产生 Provider outcome，也不把部署/管理链显示为 MCP/Skill 支持。
- 同一 `.283` 候选随后以现有受保护凭据的临时副本和签名 Skill/MCP fixture 完成真实 Claude×Docker Control Plane/Worker 结果链；命令 exit 0，`capability_acceptance=passed`（`mcp_requests=8`、`side_effects=1`、`skill=1`）及 `event_stream_resume=passed`（`resumed_events=6`、`post_terminal_events=1`）。日志 `.tmp/mcp-skill-runtime-v1-20260915-r284-claude-docker.log`（`sha256:dc542461e9fe99daaa2812cd5c46eca98b7ae3cdc728067884b2ab554022b2b9`）和 secret scan 均通过，fixture/镜像/Compose/OpenSandbox 资源为零，原凭据源未修改；Admin 只显示 opaque outcome/计数，不显示 Token、输入输出、MCP 返回内容或 Skill 源码。该结果不关闭 Claude×Docker 整格，负向 capability 矩阵与其他十一格继续开放。
- Candidate `.284` 的真实 Admin/Runtime 负向链又验证 MCP 与 Skill 撤销：两个 revoke Operation/Audit 只带 opaque resource identity、`capability_revoked` 和 `CAPABILITY_UNAVAILABLE`，后续 Runtime 请求/外部副作用均为零；日志 `.tmp/mcp-skill-runtime-v1-20260915-r288-claude-docker-revoke.log`（`sha256:b31627069e0023fea2d3d44ff29af545d4a4a8fa3e71ed86999c80f322b079d0`）pattern scan、Compose/OpenSandbox 清理和脚本 exit 0 通过。页面继续不显示 Token、输入输出、MCP 返回内容或 Skill 源码；版本不兼容、跨租户、旧 generation、重启恢复和其余 Provider/环境格保持开放。
- Candidate `.285` 的无 Provider 凭据 Compose 负向又真实产生 MCP/Skill 的 opaque `*.fail`、`capability_version_mismatch` 事件，错误版本返回 `CAPABILITY_UNAVAILABLE` 且不打开 Runtime；跨租户元数据读取为 `401`。日志 `.tmp/mcp-skill-runtime-v1-20260915-r290-contract-negative.log`（`sha256:68f7e7a06b43f7ada97a96e03ed07000fdbb80696c55b8182af3e349902c83f2`）和零残留清理通过；Admin 投影仍不增加 Prompt、源码、工具输入输出、MCP 返回内容或 Secret。
- Candidate `.286` 的真实 Compose 共享 fencing 负向在 Sandbox generation 从 `1` 推进到 `2` 后，以带 MCP/Skill refs 的旧 generation 创建 Session，返回 `409 SESSION_CONFLICT` 且不打开 Runtime。日志 `.tmp/mcp-skill-runtime-v1-20260915-r292-stale-generation.log`（`sha256:6655117051c6742505d7e610ed05702d8cbb880b6c3f2277be271c1ef5a10d82`）、secret pattern scan 与零残留清理通过；Admin 仅呈现 opaque conflict/status，不展示 Sandbox 内部路径、物料或 Secret，也不把共享负向冒充逐格通过。
- Candidate `.287` 将相同 opaque contract-negative/stale-generation 检查真实扩展到 outbound RemoteWorker 与 Kubernetes：两者都返回 MCP/Skill `CAPABILITY_UNAVAILABLE`、跨租户 `401`，旧 generation `1` 在当前 `2` 下返回 `409` 且不打开 Runtime。RemoteWorker 日志 `.tmp/mcp-skill-runtime-v1-20260916-r297-remote-contract-negative.log`（`sha256:8a1eca15f6f4268789a3f870b07e578a0c6a5141a62f0e6993298bd98dc30cf4`），Kubernetes 日志 `.tmp/mcp-skill-runtime-v1-20260916-r300-kubernetes-contract-negative.log`（`sha256:89ab5ac743557b04331c249d2f4a89956fccf6e439d632796b09897cc4c055b7`）；secret scan 无命中，Compose 资源为零，任务自有 kind/OpenSandbox 前置资源已精确删除。Admin 仍只展示 opaque 状态、版本/digest、绑定及 Operation/Audit，不增加 Prompt、源码、输入输出、MCP 返回内容或 Secret；该共享证据不关闭任何 Provider×环境格。
- 同一 `.287` 制品的 r304 真实 Claude×RemoteWorker 运行进一步产生正向、断连续读和撤销结果：Docker/RemoteWorker 各为 `mcp_requests=8`、`side_effects=1`、`skill=1`、`resumed_events=6`、`post_terminal_events=1`；RemoteWorker 撤销后 Runtime 新增请求/副作用为 `0`、opaque capability events 为 `2`。日志 `.tmp/mcp-skill-runtime-v1-20260916-r304-claude-remote-worker-revoke.log`（`sha256:a690eb82d4754bf9b5bf8d0c822e757889e27765cd1c88ba60630289453165cd`）、secret scan 与零资源清理通过。Admin 只消费这些 opaque 计数/状态；同轮 restart/takeover Turn 未引用 MCP/Skill，所以不显示为 capability recovery，Claude×RemoteWorker 整格仍开放。
- `.288` 的 r305 在相同 opaque capability 结果通过后暴露 RemoteWorker Sandbox Exec/heartbeat 的 PostgreSQL `40P01`，最终以 `INTERNAL_ERROR` 失败；实现只把已回滚 deadlock 映射为 `RESOURCE_CONFLICT`，让调用端以新一次性授权有界重试。修复后的 `.289` 22 项 checksum 与 r306 完整 Compose smoke 通过，日志 `.tmp/mcp-skill-runtime-v1-20260916-r306-claude-remote-worker-revoke.log`（`sha256:b23b01ddcc15add7a9d93bf7783ed12ace3065374f7ee162cb92feabc2983915`），secret scan/资源清理为零。Admin 不展示数据库诊断或凭据；能力绑定恢复仍未覆盖，整格不关闭。

Candidate `0.3.0-dev.290` 的 Kubernetes fixture 仅展示 opaque 测试状态：按 `opensandbox.io/id` 绑定 Runtime Pod、固定 SDK bundle、descriptor stdin 注入和清理结果；Admin 不增加 Pod 名称、容器名、进程号、主机路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。无 Provider 凭据的真实命令 exit 0 只得到 `capability_contract_negative=passed` 与 `capability_stale_generation=passed`，日志 `.tmp/mcp-skill-runtime-v1-20260916-r290-kubernetes-contract-negative.log` SHA-256 `sha256:15a7dbbc57ddcca19a7ebcfbcd8ab559150ea9e0c56b9d2dac41eb832c99e1a5`，资源清理为零；Claude Kubernetes 正向仍未运行，不改变 Admin 或十二格验收边界。

本轮恢复脚本只新增内部 capability-bound 验收断言，不改变 Admin 投影：恢复时仍只记录 opaque MCP/Skill ID、版本、digest、状态、绑定和 Operation/Audit，绝不展示 Prompt、Skill 源码、工具输入输出、MCP 返回值、Pod/主机路径或 Secret。由于没有新的真实 Provider recovery evidence，Admin 不得把该路径显示为 supported 或关闭任何格。

Candidate `0.3.0-dev.291` 只完成 22 项制品 checksum 与定向 capability/Control Plane 测试；Admin 继续不展示恢复 Turn 内容或 Secret，也不把 `capability_bound_recovery` 静态断言显示成 supported 状态。

Codex `@openai/codex@0.150.1` 的 Skill adapter 已按真实 app-server schema 接入 `skills/extraRoots/set`/`skills/list`：Admin 侧仍只展示 opaque Bundle ID、版本、digest、权限、状态、兼容性、绑定关系与 Operation/Audit；Runtime discovery 的本地 path、Skill 源码、Prompt、工具输入输出、MCP 返回内容和 Secret 均不进入 Admin 投影。受控 `HOME`/`CODEX_HOME` 证据、58 个 Codex tests 和 `.292` 22 项 checksum 只更新实现边界，不新增 supported cell 或解除“无 Provider 凭据不得宣称正向”的验收限制。

Candidate `0.3.0-dev.293` 进一步确认 system Skill 也只能来自受控 `CODEX_HOME`；Admin 投影仍不显示本地 path、源码、Prompt、工具输入输出、MCP 返回内容或 Secret。该候选 manifest `sha256:6d2e62174420da4083575cd6215009942d5c28ea79d054e6c9d320788d786385`、checksums `sha256:f7427791180c818b9c66a87ce10c0c60453dbc831324b0ceb7a32e46de7dc3f3` 已通过，且没有 Provider 正向凭据或 supported cell 更新。

Candidate `0.3.0-dev.294` 的 Admin 可消费 Codex MCP `mcp_tool_call` 与 opaque Server ID、单一 Bundle Skill `sourceItemType=skill` 与 opaque Bundle ID；页面仍只展示 ID、版本、digest、权限、兼容性、状态、绑定关系和 Operation/Audit，不展示本地路径、源码、Prompt、输入输出、MCP 返回内容或 Secret。其 manifest `sha256:6565fd94667d960e7c1399e0116340f81ba82a2153ac37abbde66ab07b8e2966`、checksums `sha256:105d299b1d57c3e13ea8dd753fa0279f594e89db475fec643ec690b0053e9ba1` 已通过；没有 Provider 正向凭据，仍不显示 supported。

Candidate `0.3.0-dev.295` 继续保持 Admin opaque 投影：MCP transport/JSON-RPC failure 只呈现脱敏状态/Operation/Audit，未知副作用不展示响应内容，也不提供自动重试按钮；manifest `sha256:fb7fde91f966d01279e6325f360a8709962f0340370ac5dd40656fc3d16a730`、checksums `sha256:f1b5d517f7b26f2a903897d604888195ae86d2cc167d23a539aeabfba86e52e6` 已通过。没有 Provider 正向凭据，Admin 不显示 supported。

`.291` 的无凭据 Compose 回归只新增共享 opaque 负向结果：不兼容版本、跨租户拒绝、旧 generation 拒绝、Operation/Audit、重启和备份恢复均可见，未展示 Prompt、源码、工具输入输出、MCP 返回内容或 Secret；它不改变 Admin 的 Provider supported 状态。

Kubernetes contract-only 重跑同样只向 Admin 贡献 opaque 负向状态、绑定和 Operation/Audit：Pod 名称、容器名、进程号、主机路径、Prompt、源码、工具输入输出、MCP 返回内容和 Secret 均不进入 Admin 投影；该结果不显示为 Provider supported。

## 9. 关键流程

### 9.0 底座独立使用（当前主线）

```text
管理员准备 Target/RemoteWorker + 已验证 RuntimeProfile/模板/策略
  -> 用户 API/CLI 创建长期 Workspace
  -> 创建 Sandbox，API 返回 Operation
  -> Controller 调谐到 ready
  -> 用户通过有权数据通道使用 Exec/PTY/Files/Preview/SSH
  -> 停止 Sandbox，保留 Workspace/Volume
  -> 重新创建 Sandbox，挂载原卷并恢复文件
  -> 独立 Workspace Snapshot/Restore 或 Delete 操作
```

Admin 全程只观察运维元数据与 Operation/Audit。以下 9.1/9.2 保留原 Agent 兼容流程，
APP-M1 改为引用已有 Workspace/Sandbox；AgentSession 关闭不删除工作区。

### 9.1 管理员准备环境

```text
注册 Deployment Target
  -> Probe 连通性与能力
  -> Target ready
  -> 选择固定 Worker release
  -> 配置资源/存储/网络策略
  -> 创建并发布 Environment Profile
  -> User Web 可选择该 Profile
```

任一步失败都保留稳定错误码和可重试操作，不把部分成功显示为 ready。

### 9.2 用户创建 Agent 会话

```text
用户选择 Agent + Environment Profile
  -> User API 提交 profile ID/version
  -> Control Plane 校验 published/available/quota
  -> 服务端解析 target、release 和 credential references
  -> 创建 Environment Lease 与 Worker
  -> ready 后创建 Session/Turn/Execution
  -> User Web 进入对话
```

User Web 请求中不得出现 Target endpoint、`credentialRef` 或 `providerCredentialRef`。

### 9.3 Drain 与升级

```text
管理员选择 Target/Workers
  -> 查看影响范围
  -> Drain，停止新调度
  -> 等待或终止活动 Lease
  -> Upgrade 到固定 digest
  -> Probe/健康检查
  -> Resume
```

升级失败时保持可识别的旧 generation 和回滚目标，不允许 UI 仅显示“Upgrade failed”。

### 9.4 Cleanup

Cleanup 前必须展示将删除的 Worker、容器/Pod、Workspace volume 和其他平台拥有资源。管理员确认资源名称
和 generation 后提交；Control Plane 执行并返回 Cleanup Operation。活动 Lease 存在时默认拒绝 Target Cleanup。

上述确认是产品中实际 Cleanup 操作的交互要求，不泛化为每次开发操作都需确认；也不授予开发代理删除资源、
操作生产数据库或执行部署发布的权限。

新底座的计算 Cleanup 默认不包含长期 Workspace volume；UI 必须列明保留资源。若操作确实涉及卷删除，
还须满足独立 Workspace 删除/保留策略及原名称/generation 确认，不能沿用旧 Lease 的隐式回收语义。

## 10. API 与后端要求

### 10.1 API 分层

| API | 调用方 | 允许的数据 |
| --- | --- | --- |
| User API | User Web / desktop | Conversation、Session/Turn、Artifact、已发布 Profile 摘要 |
| Admin API | Admin Web | Target、Worker、Lease 运维元数据、Profile、策略、Operation、Audit |
| Worker/Supervisor API | Control Plane 与执行组件 | 受 generation/fencing 保护的内部命令与 receipt |

底座新增 Workspace/Sandbox 用户管理 API 及受授权的数据接口；Admin API 只取其运维投影。
RemoteWorker 注册、身份与通道属于独立的内部接入协议，不能复用用户/管理员 bearer 作为节点身份。

Admin Web 不通过枚举租户公开 API 来拼装跨租户运维视图。

### 10.2 既有实现承接与底座增量

当前已有能力可复用：

- `DeploymentTarget` 的 Docker/Kubernetes/SSH kind；
- Target register/get/list/probe/cleanup；
- `CloudEnvironmentLease` list/create/get/upgrade/terminate；
- generation、desired/observed/cleanup phase；
- `credentialRef` 和 `providerCredentialRef` 引用语义；
- 生成的 TypeScript Platform SDK。

以下是原 Admin 切片的承接清单，不是当前缺失清单。当前源码已含专用 Admin 路由、Profile/策略、Drain/Resume、目录和 Operation/Audit 等路径；逐项核验并复用，不能再造同义 API，也不能把 CRUD 存在等同于完整后端执行或生产认证验收：

- 专用 Admin API 路由、scope 和生成 SDK；
- `EnvironmentProfile` 及发布版本；
- User API 的已发布 Profile 列表和按 Profile 创建环境入口；
- Worker/Target Drain、Resume 和运维列表；
- Image/Release catalog 的最小只读接口；
- Quota、Storage Policy、Network Policy 的管理接口；
- Operation 和 Audit 查询接口；
- 从 User API 移除基础设施写能力，或至少让普通用户 scope 无法调用。

底座新增 Workspace/Volume/Sandbox/RemoteWorker 的版本化契约、异步生命周期、访问和实际策略执行，按 [04](04-extraction-and-migration.md) 各阶段补齐；现有 Lease/Worker 与 Agent 接口继续兼容。当前/目标差异与证据统一记录在 [06](06-status-tracker.md)。

### 10.3 状态与并发

- 所有写操作使用 idempotency key；
- 对状态敏感的操作携带 expected generation/resource version；
- 409 明确区分 generation conflict、invalid transition 和 idempotency conflict；
- 长时间操作返回 Operation，不让浏览器请求一直阻塞；
- 列表支持分页、过滤和稳定排序；
- UI 轮询或订阅服务端状态，不能自行推断终态。

## 11. 前端架构

### 11.1 应用结构

保留现有独立应用；以下是职责结构，不要求把现有文件强行迁移成该目录布局：

```text
apps/
├── user-web/     # 对话与用户工作流
└── admin-web/    # 基础设施与运维控制台
```

`apps/admin-web` 继续使用：

- Vite；
- React；
- TypeScript；
- 生成的 Platform/Admin SDK；
- 原生 CSS 和 CSS variables 实现的 Daytona `v0.190.0` 等效视觉系统。

P0 不引入 Next.js、Ant Design、Tailwind 或新的状态管理框架。先使用 React state、浏览器原生控件和现有
依赖；只有出现已验证的重复需求后再提取共享组件包。

国际化优先使用应用内类型化 message catalog、React context 和浏览器原生 `Intl`。在两种固定语言可以由
少量本地代码完整覆盖时，不新增 i18n 依赖；message key、locale 解析和 fallback 必须有测试。

### 11.2 部署边界

- 推荐独立域名，例如 `app.example.com` 与 `admin.example.com`；
- 两个应用分别构建镜像和发布；
- Admin Web 反向代理只暴露 Admin API；
- User Web 反向代理只暴露 User API；
- CSP、OIDC redirect URI、cookie/audience 和权限分别配置。

## 12. 视觉与交互规范

### 12.1 总体风格

Admin Web 不沿用当前 User Web 的 Modern Dark 视觉。界面以 Daytona `v0.190.0` Dashboard 为 1:1 复刻
对象，同时支持该版本的 light/dark theme。实现前必须从固定 tag 的 `apps/dashboard/src` 提取 design tokens、
布局结构和组件状态，并为 Cloud Agents 建立对应的视觉基线截图。

复刻优先级：

1. 应用壳层、侧边栏、内容区比例和响应式行为；
2. 字体、颜色、间距、圆角、边框和表格密度；
3. 页面标题、Toolbar、Tabs、Sheet、Dialog 和 Dropdown；
4. 加载、空、错误、权限拒绝和异步 Operation 状态；
5. hover、focus、active、disabled、展开和分页等交互细节。

### 12.2 布局

- 完整复刻 Daytona 的 Dashboard shell、可折叠左侧 Sidebar、分组导航、当前项、底部账户/设置区域和
  移动端 Sidebar 行为；
- 完整复刻 Daytona 的 Page Layout、Page Header、Page Intro、内容宽度、留白和滚动行为；
- 资源列表使用 Daytona 同密度的 Toolbar、Filter、Data Table、row action、pagination 和 selection；
- 创建和编辑流程优先使用 Daytona 同位置、同宽度和同交互的 Sheet/Dialog；
- 详情页沿用 Daytona 的标题、状态、metadata、Tabs 和操作区布局，仅替换 Cloud Agents 字段；
- Dashboard Overview 的卡片比例、排列、边框和状态信息密度与 Daytona 对应页面保持一致。

### 12.3 状态表达

- Badge、Alert、Empty、Skeleton、Spinner、Toast 和权限拒绝状态完整复刻 Daytona 对应组件；
- 状态同时使用文字、图标和颜色，不能只依赖颜色；
- Cloud Agents 状态映射到 Daytona 的 info、success、warning、destructive 和 neutral token，不另造一套颜色；
- 展示 desired state 与 observed state 的差异；
- 错误优先显示 stable error code 和可执行的恢复动作，原始诊断放在展开区域且必须脱敏。

### 12.4 操作反馈

- 创建资源、行操作、Dropdown、Sheet/Dialog、确认、Toast 和分页交互完整复刻 Daytona；
- 长时间操作仍使用 Cloud Agents Operation 模型，视觉上套用 Daytona 的 pending/success/error 反馈；
- Upgrade、Drain、Terminate、Cleanup 在 Daytona 确认交互上增加影响范围和 generation，不得为追求视觉一致
  删除安全信息；
- 键盘焦点、ARIA、对比度和 `prefers-reduced-motion` 至少保持当前可访问性要求。

### 12.5 视觉验收

- 固定 Daytona `v0.190.0`，保存 Dashboard shell、资源列表、详情、Sheet、Dialog、空状态、错误状态和
  light/dark theme 的参考截图；
- Admin Web 对应页面使用相同 viewport、theme 和组件状态生成截图；
- 以并排审查和自动截图 diff 验证，动态 ID、时间和业务文案区域可 mask；
- Sidebar 宽度、Header/Toolbar 高度、内容边距、表格行高、字体、颜色、圆角、边框和交互状态出现明显偏差
  时，对应 BASE 联合切片及 BASE-READY 不得标为通过；
- Cloud Agents 品牌替换和业务字段数量差异是允许差异，布局和组件风格不是允许差异。
- `zh-CN` 与 `en-US` 分别生成视觉基线；中文文案不得导致 Sidebar、Toolbar、Table、Sheet/Dialog、按钮和
  状态 Badge 溢出、遮挡或不可操作，也不得通过缩小字体破坏 Daytona 视觉比例。

## 13. 当前实现迁移

原 `InfrastructureWorkspace.tsx` 混合边界已进入历史迁移：当前已拆分 Admin Web 与 User Web 的
`EnvironmentWorkspace` / `AgentWorkspace`。以下要求保留作回归清单，不得要求重复重建已完成页面，
也不能据此认为完整底座已经交付：

1. 将 Target register/get/list/probe/cleanup、Lease 运维、endpoint、credential reference、release digest、
   CPU/内存和 upgrade/terminate 控件迁移到 `apps/admin-web`。
2. User Web 保留 `AgentWorkspace` 的 Session/Turn/Execution、审批、用户输入、取消、中断和 Artifact 能力。
3. 在 User Web 新增只读 `Environment Profile` selector，替代 Target/Lease 配置表单。
4. 用户选择 Profile 后由服务端创建/绑定 Lease；浏览器不解析 Target 或 Secret。
5. 完成 Admin API 权限切换后，普通用户 Token 不能继续调用基础设施写接口。

迁移期间可以短暂保留旧页面用于开发验证，但生产导航和普通用户权限必须先隐藏并拒绝基础设施写操作，
不能长期维护两套入口。

## 14. 联合实施顺序

当前顺序以 [04 的 BASE-M0～M5](04-extraction-and-migration.md#0-当前实施顺序底座先行) 为准。
每个面向管理员的基础能力必须同时具备真实契约/后端/API/SDK、Admin 管理操作、状态、错误和恢复反馈。后端与页面可在同一联合切片内并行推进，但未完成页面流程不得把该能力标为完成或整体推迟到用户对话阶段。视觉、双语和可访问性均计入对应页面验收。

| 既有 Admin 工作 | 当前承接位置 |
| --- | --- |
| Target/Lease/Operation/Audit、基础壳层与双语 | BASE-M0/M1 复用与补齐 |
| Agent Profile 与 User selector | 保留兼容；新底座 RuntimeProfile 在 BASE-M0/M4，用户 Agent 接入在 APP-M1 |
| Worker/Drain/升级/策略 | BASE-M1～M4 随真实生命周期、网络、客户节点和调度交付 |
| 独立鉴权/部署隔离、视觉和完整运维回归 | 从首条相关路径持续验证，BASE-M5 收口 |
| Codex/Claude Turn E2E | 既有兼容回归可运行；新用户产品完整验收归 APP-M1，不阻塞无 Agent 底座开发 |

旧 ADMIN-M1～M4 定义及固定验收在 [ADMIN-WEB-V1](history/07-legacy-admin-milestones.md#admin-web-v1) 查询；旧报告不改名、不改结论。它们不提供平台默认下一步，也不把 Agent Turn 设为 BASE 前置条件；仍以原 M1～M4 为范围的有效任务按该固定验收执行，不因归档失去原范围或被自动切换为 BASE。

## 15. 实现验收标准

本节按任务的明确范围选择验收，不能把后续标准追加为旧任务的完成条件：

| 固定标识 | 适用任务 | 完成含义 |
| --- | --- | --- |
| `ADMIN-WEB-V1` | 原 User/Admin 拆分任务的 M1～M4，即 ADMIN-M1～M4 | 原 Target/Lease/Profile/执行 Worker 管理与真实 Agent E2E 完成；不代表新底座就绪 |
| `BASE-ADMIN-V1` | 当前主计划的 BASE-M0～M5 | 新 Workspace/Sandbox/RemoteWorker 管理与基础设施联合验收；还须满足 05 的 BASE-READY |
| `ANYWHERE-RUNTIME-V1` | APP-M1 的 Runtime/SDK 切片 | 本文 §8.13 的相关 Admin 闭环与 [05 的完整验收](05-gates-and-acceptance.md#anywhere-runtime-v1)；不替代完整用户对话 UI 或旧验收 |

### ADMIN-WEB-V1

完整固定条件见 [旧 Admin 任务验收基线](history/07-legacy-admin-milestones.md#admin-web-v1)。原提示词明确列出 User/Admin 拆分的 M1～M4，却只引用“第 15 节全部标准”时，该引用指向 ADMIN-WEB-V1，不追随本节后续新增的 BASE 条件。
不得因此删去原任务要求的 Docker/Kubernetes/SSH 与真实 Codex/Claude Session/Turn 验收；也不得要求该旧任务补齐 outbound RemoteWorker 或独立 Workspace/Sandbox 才能完成。

### BASE-ADMIN-V1

以下条件全部满足才可认为基础设施管理 Admin Web 实现完成。这些条件不作为设计文档完成或启动已授权实现的前置条件；
设计文档完成不代表实现已通过验收，也不自动授予实施权限。

1. `apps/user-web` 与 `apps/admin-web` 可独立构建和部署。
2. 普通用户只能访问自己获授权的 Workspace/Sandbox 和公开规格，以及已有/后续应用层的对话、执行与 Artifact。
3. User Web 页面、网络请求和浏览器存储不包含基础设施 endpoint 或 credential reference。
4. 管理员能管理 Docker/Kubernetes Target 与客户 RemoteWorker；旧 SSH Target 注册/Probe 保持兼容。
5. 管理员能发布实际可执行的底座规格；用户不依赖 Agent 即可创建并连接长期 Workspace/Sandbox。
6. 管理员能区分节点 RemoteWorker、Sandbox 执行 Worker 和旧 Lease，完成 Upgrade、Drain、Resume 和 Cleanup。
7. 所有危险操作有 generation 校验、影响确认、Operation 状态和 Audit 记录。
8. Admin API 不返回用户消息、Workspace/Artifact 内容或任何 Secret bytes。
9. 普通用户 Token 调用 Admin API 必须被服务端拒绝。
10. Docker、Kubernetes、outbound 客户节点均有真实部署、数据保留/恢复和精确清理证据；
    新用户 CloudAgents 的 Codex/Claude Turn 验收归 APP-M1，不用已有 Turn 记录替代本项。
11. 键盘操作、焦点、对比度、错误状态和 `prefers-reduced-motion` 满足基本可访问性要求。
12. Admin Web 的应用壳层、导航、列表、详情、表单、Sheet/Dialog、状态和交互通过 Daytona `v0.190.0`
    固定截图的视觉回归；除品牌和业务字段差异外，不存在未经批准的布局或风格偏差。
13. Admin Web 支持 `zh-CN` 与 `en-US` 即时切换、刷新恢复和 locale fallback；两种语言没有缺失 key、未翻译
    的界面文案或布局溢出，并分别通过 light/dark、桌面/移动视觉回归。

## 16. 执行入口（不单独维护 Goal 计划）

收到继续实施主计划的任务后，在明确授权范围内按 06 进入 04 最早未完成的 BASE 或 APP 切片，以实际能力＋对应 Admin 工作流为同一个交付单元。
原 ADMIN-M1～M4 任务在收到明确范围迁移指令前仍采用 ADMIN-WEB-V1；“继续原任务”或读取新版文档不自动扩大其范围。BASE-READY 后的新 Runtime 任务可使用 [04 的 Anywhere Runtime Goal 提示词](04-extraction-and-migration.md#anywhere-runtime-goal)，只应用明确指定的范围、顺序与完成标准，保留仍有效的安全和操作授权边界。
明确指定的后端、UI、契约、文档修复或审查/验证按该任务范围完成，并覆盖实际受影响的流程，不自动扩成整个阶段；单项任务完成不代表 BASE 阶段通过。阶段完成仍须满足后端＋Admin 联合验收。
每次恢复核对当前 HEAD、dirty work、实际后端/API/SDK 和证据，复用已完成能力；
不得仅创建空页面、Mock 数据或没有后端 authority 的按钮。本次计划重排不修改现有 Goal/任务/自动化，
也不授予其他任务新权限；已有效的同范围授权无需重复确认。

后续任务引用固定验收标识和所用文档版本，不再单独引用可变章节号。改变固定标识的资源范围或完成条件时须新增版本并显式迁移适用任务，不能原地把 ADMIN-WEB-V1 扩成 BASE，也不能把旧任务改称完成来掩盖未做的旧验收。

本轮 `.296` candidate 没有扩大 Admin 展示面：Codex MCP/Skill 事件只归因到 opaque capability ID，namespace collision、transport/JSON-RPC failure 和未知副作用仍只表现为 Operation/Audit 状态，不向 Admin 暴露工具输入输出、MCP 返回内容、Prompt、源码或 Secret。候选 manifest `sha256:8433198e69af58211549b1da694f58a367426b3383d39d7a50ad385876280c09`、checksums `sha256:4569d088a836ad7643190055fd1fdef29b047093926e11f23ee37bc94955fdf6`；无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

`.297` 失败审计仍只扩大 Admin 可见的 opaque Operation/Audit 状态：已完成 capability item 的 outcome 与 started-but-not-completed 的 `mcp.fail`/`skill.fail`、`capability_call_unknown` 可查询，但不返回工具输入输出、MCP 返回内容、Prompt、源码、started-event 原文或 Secret。无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

`.298` 仅修正失败审计的 opaque item identity 归属，Admin 仍只显示 Capability ID、Operation/Audit 状态和脱敏 digest，不显示工具输入输出、MCP 返回内容、Prompt、源码或 Secret；无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

`.299` 只记录 deepseek tool-result fail-closed 的 opaque Operation/Audit 结果；Admin 不显示 provider error 原文、工具输入输出、MCP 返回内容、Prompt、源码或 Secret。无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

`.300` 只记录 deepseek Harness 中止和后续通知丢弃的 opaque Operation/Audit 状态；Admin 不显示 provider error 原文、工具输入输出、MCP 返回内容、Prompt、源码或 Secret。无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

`.301` 只记录 Pi 恢复路径继续携带受管 Skill Bundle root 的 opaque Operation/Audit 边界；Admin 不显示本地 Skill 路径、源码、Prompt、工具输入输出、MCP 返回内容或 Secret。Runtime broker/materializer/stdio 与 Provider API 91 tests、定向 Provider/SDK 54 tests、TS/Go 生成检查、Go test/vet 和 Admin 边界检查通过；无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

同一 `.301` 候选的无凭据 Compose 负向回归只向 Admin 提供 `capability_contract_negative`、`capability_stale_generation`、重启/备份恢复及 browser smoke 的 opaque 状态；命令 exit 0 且容器/网络/卷为零，不展示本地 Skill 路径、源码、Prompt、输入输出、MCP 返回内容或 Secret，也不显示 Provider supported。

`.302` 仅收紧 capability Operation/Audit 的 opaque identity：Admin 可区分同名 MCP/Skill 的状态、结果和 `capability_call_unknown`，但不展示 Prompt、源码、工具输入输出、MCP 返回内容、路径或 Secret；Go/TS 接缝和 Compose 负向回归通过，无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

`.303` 延续 opaque Admin 边界：pending capability 审计按资源类型、opaque capability ID 和 provider item ID 独立归因，Admin 只显示对应 Operation/Audit 状态、版本和 digest；不展示 Prompt、源码、工具输入输出、MCP 返回内容、路径或 Secret。Go/TS 接缝、Managed Agent 回归与 Compose 负向回归通过，无受保护 Provider 运行，Admin/十二格/Gate 结论不变。

`.305` 的恢复回归只重用已持久化的 opaque MCP/Skill refs、版本、digest 和 manifest digest；Runtime open 与 Admin/Audit 均不新增 endpoint、host path、Token、Prompt、源码、工具输入输出或 MCP 返回内容。该协议级检查不改变 Admin 展示面，也不构成 Provider/环境真实恢复验收。

`.306` 的 digest 回归只验证 opaque MCP/Skill refs 参与 Execution 幂等材料；Admin 继续只展示 opaque ID、版本、digest、权限、状态、兼容性、绑定关系和 Operation/Audit，不展示内部路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。

`.307` 的 Pi Skill 事件接缝仍只扩大 opaque Operation/Audit 状态：`dynamic_tool_call`/`sourceItemType=skill` 及 `skill.load` outcome 只带 capability ID、provider item ID 和状态，不返回受管 Skill 路径、Bundle 源码、Prompt、工具输入输出、MCP 返回内容或 Secret；Pi MCP 继续 fail closed。Provider API/Pi 100 Vitest、typecheck、格式、diff check 和 22 项 checksum 通过，但无受保护 Provider 或 Admin/环境正向验收，Admin/十二格/Gate 结论不变。candidate manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:c0316def7d041c699ebd98652b9ffbb540910ade264e7d8e34bb068bead599e7`、`sha256:ea523d0d6bd8233277bf4f67c6f3a669efa4edcfb9044daaaa5191c13ff1d969`、`sha256:080cb8ecfc42d7b8e7319e7655e4050f391e0049fb971306a1765e4a67a28789`、`sha256:32fcc7f1c48900aa735ea6ae91a72cda199a7698a56602c371cac0437e4dcfac`。

`.309` 的真实 Claude×Docker 恢复验收继续保持 Admin 最小投影：start/settle/cancel/interrupt 即时响应只补回已持久化的 opaque MCP/Skill refs，Admin/Operation/Audit 仅显示 capability ID、版本、digest、状态、attempt、恢复模式和脱敏 outcome；不展示 endpoint、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、credential 或 Token。真实日志已证明 MCP/Skill 正向、事件续读、Control Plane SIGKILL 后 attempt 2/confirmed/RPO 0、撤销零请求/零副作用；日志 SHA-256 `sha256:ba9b32fc4d4e34530ca099441056c52fe0cb6910381add73573f9574510938a8`，credential/token/MCP return secret scan 为 0，原凭据未变，Compose/fixture/诊断资源清理为 0。candidate manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:6d407d0b0403d3c6e1ea87ecaff6fbf549af1d5308d3accab630c2d555e209d5`、`sha256:24d071037cef1969f42195d5b27828dbed4e77b544e37b55c10be887d6e60164`、`sha256:080cb8ecfc42d7b8e7319e7655e4050f391e0049fb971306a1765e4a67a28789`、`sha256:d71dbb983a7f967a8e2cf86c8afe8996275aafc4876c11fe23b1ec9e6bce023d`。该结果不扩大 Admin 数据面，也不关闭十二格或正式 Gate。

同一 `.309` candidate 的 Claude×RemoteWorker r328 实测继续复用相同 Admin 投影：真实 MCP/Skill 正向、事件续读、capability-bound Control Plane process-restart 和撤销负向只增加 opaque capability ID、版本、digest、attempt、恢复模式、状态与脱敏 outcome，不展示节点地址、endpoint、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、credential 或 Token。日志 SHA-256 `sha256:261aad9ee72bdf04bca61bc94e06ca31a62ec5ff5eae968ad27d4c56e4dc8dc9`，credential/token/marker/tool-name scan 为 0，原凭据未变，Compose/RemoteWorker/fixture 临时资源为 0。同轮跨节点恢复是 `capabilityBound=false`，Admin 不把它显示成 MCP/Skill 恢复成功；十二格和正式 Gate 状态不变。

同一 `.309` candidate 的 Claude×Kubernetes r329 实测也保持相同 Admin 最小投影：Kubernetes MCP/Skill 正向、事件续读、capability-bound Control Plane process-restart 和撤销负向只显示 opaque capability ID、版本、digest、attempt、恢复模式、状态与脱敏 outcome，不展示 namespace/Pod/node、endpoint、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、credential 或 Token。日志 SHA-256 `sha256:d6c65ef24d4289a27f4f878227e6732580564720c50c21a259aafb4fd902c539`，credential/token/marker/tool-name scan 为 0，原凭据未变，Compose/Kubernetes/fixture 临时资源为 0。本轮未执行 Kubernetes cross-node，Admin 不推断未发生的接管；十二格和正式 Gate 状态不变。

Candidate `0.3.0-dev.353` 没有扩大 Admin 数据面。Codex 原生 MCP discovery 成功但模型未发起 MCP/Skill 调用时，Admin 只显示 opaque capability ID、版本、digest、绑定、Operation/Audit 状态和稳定失败码；不显示 `mcp_servers` 配置、loopback 地址、Skill 路径、Prompt、工具输入输出、MCP 返回内容、credential 或 Token。真实 Docker 日志 SHA-256 为 `sha256:dc43706b5576c43b75cbdfe9fb5e577d649701ae4325c976dffc936281105c72`，exit 1、资源清理为零；界面不得把 discovery 显示为 supported 或 successful invocation，十二格和正式 Gate 继续开放。

Candidate `0.3.0-dev.354` 只修复 Provider Host 内部的即时 `turnId` 竞态并重新打包当前源码，不扩大 Admin 投影。Admin 继续只显示 opaque ID、版本、digest、权限、状态、兼容性、绑定、Operation/Audit 和稳定错误码；不显示 app-server 参数、endpoint、路径、Prompt、源码、工具输入输出、MCP 返回内容、credential 或 Token。该候选没有新的真实模型调用证据，页面不得把 `.353` 的 discovery-only 结果提升为 supported；十二格和正式 Gate 保持开放。

`.354` 的 Codex Docker 复跑只在 Worker image build 阶段因 `deb.debian.org` 外部连接失败而停止，日志 SHA-256 为 `sha256:53227ff4f88835d2f370c7402a918b89f60202dc358f331ffc8c110f63fdcb65`；Admin 不得展示为 Provider 运行结果；日志只保留稳定失败码和候选/Operation 关联，不展示 apt 细节、路径、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。验收脚本的 Skill 动作改为 Host-owned `workspace.write_text_file` 一次，Admin 数据面与十二格/Gate 状态不变。

`.356` 在代理预热后真实启动了 Codex Docker 链，但由于没有 `tools/call`、Skill、Artifact 或副作用，Admin 只能显示 opaque capability 绑定与稳定的 discovery-only/失败状态；日志 SHA-256 为 `sha256:37f1301415812a6a3716ae559c6ad8bf38df20e5d2a8e272573f429fea4fb5e2`。Admin 不展示模型内容、MCP 返回内容、Prompt、路径、代理变量或 Secret，不能把该次运行标为 supported；十二格/Gate 状态不变。

## 17. 参考

- [Daytona `v0.190.0` Dashboard pages](https://github.com/daytonaio/daytona/tree/v0.190.0/apps/dashboard/src/pages)
- [Daytona `v0.190.0` Dashboard routing and permission wrappers](https://github.com/daytonaio/daytona/blob/v0.190.0/apps/dashboard/src/App.tsx)
- [Daytona `v0.190.0` Dashboard styles](https://github.com/daytonaio/daytona/blob/v0.190.0/apps/dashboard/src/index.css)
- [`apps/user-web`](../../../apps/user-web/README.md)
- [`DeploymentTarget` schema](../../../contracts/platform/v1alpha1/schemas/deployment-target.schema.json)
- [`CloudEnvironmentLease` schema](../../../contracts/platform/v1alpha1/schemas/environment-lease.schema.json)
- [`Managed Host` OpenAPI](../../../contracts/managed-host/v1alpha1/openapi.json)

2026-09-19 Admin 投影验收边界：Codex×Docker 的真实 MCP/Skill 子链只向 Admin/Operation/Audit 暴露 opaque capability ID、版本、digest、权限、状态、兼容性、绑定关系及恢复/撤销的稳定结果；本轮日志与浏览器 smoke 不改变页面数据面。Prompt、Skill 源码、工具输入输出、MCP 返回内容、endpoint、主机路径、代理变量和 Secret 均未进入 Admin 或 Artifact；其余十一格与完整故障矩阵继续保持开放。

Candidate `0.3.0-dev.375` 的 direct MCP 暴露修复只改变 Runtime 内部工具可见性，不扩大 Admin 数据面；页面仍只显示 opaque capability 元数据及 Operation/Audit 稳定状态，不显示 deferred/direct 配置、Prompt、源码、工具输入输出、MCP 返回内容、路径、代理变量或 Secret。

r430 的 deepseek-harness×Kubernetes 真实结果继续使用相同最小 Admin 投影：只记录 opaque capability ID、版本、digest、权限、兼容性、绑定、Operation/Audit 状态、attempt、恢复模式和撤销的稳定计数；不显示 namespace、Pod/node、endpoint、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret。OrbStack 单节点结果不能在 Admin 中推断 Kubernetes cross-node 或完整 supported。

r431 的隔离 kind 三节点深度验收继续保持该投影：普通 cross-node takeover 的 `capabilityBound=false`、RTO/RPO 和 snapshot 只作为脱敏 Runtime 恢复结果，不得显示为 MCP/Skill 能力绑定成功；缺少 CRD 的 preflight 失败也只显示稳定失败码。Admin 不新增节点地址、namespace、Pod、endpoint、路径、Prompt、源码、工具输入输出、MCP 返回内容、代理变量或 Secret。

r437 的 deepseek-harness Kubernetes 能力绑定跨节点验收也保持同一最小投影：Admin/Operation/Audit 只显示 opaque capability ID、版本、digest、权限、兼容性、绑定关系、attempt、恢复模式、状态和撤销的稳定计数；不显示恢复目标 namespace/Pod/node、endpoint、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret。`capabilityBound=true`、RTO/RPO、snapshot size 和 `mcp_requests` 只作为脱敏状态字段；其它 Provider/环境和完整十二格仍不得推断为 supported。

r442 的 Pi×RemoteWorker 跨节点证据继续使用相同 opaque 投影：Admin/Operation/Audit 只展示 capability ID、版本、digest、权限、兼容性、绑定关系、attempt、恢复模式、状态和脱敏的 RTO/RPO/计数；不展示 DIND/Worker 地址、namespace、Pod/node、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret。`capabilityBound=true` 只表示本次已验证的能力绑定恢复，不得由页面推断其它 Provider、环境或完整 supported 状态。

r445 的 deepseek-harness×RemoteWorker 跨节点证据保持相同最小投影：Admin/Operation/Audit 只展示 opaque capability ID、版本、digest、权限、兼容性、绑定关系、attempt、恢复模式、状态和脱敏的 RTO/RPO/计数；不展示 DIND/Worker 地址、namespace、Pod/node、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret。r444 的 HTTP 499 仅作为稳定失败码/未计数 Operation 结果保留，r445 的 `capabilityBound=true` 也不得推断其它 Provider、环境或完整 supported 状态。

2026-09-23 Admin 投影继续只接受稳定 opaque 结果：r525 Claude×Kubernetes、Pi×Kubernetes 的 transport/revocation 通过只记录版本、digest、权限、兼容性、绑定、状态及 Operation/Audit；deepseek-harness r525/r527 的 `provider_unavailable` 和 Codex×RemoteWorker r524 的无 marker 只记录稳定失败码，不展示 Provider 返回、工具输入输出或 Secret。`capability_bound_recovery=0` 的 transport-only guard 与 32/32 静态验证不改变 Admin 数据面；版本不兼容、跨租户、旧 generation 和未知 side effect 继续以拒绝/待对账状态呈现，不得推断为 supported。

2026-09-24 r551 Admin 投影继续只允许稳定的 opaque 结果：Codex×Docker 的不兼容/跨租户拒绝可展示为版本、digest、权限、兼容性、绑定和 Operation/Audit 状态；`provider_unavailable` 只能展示稳定失败码和未形成 checkpoint 的状态，不能展示 Provider 返回、Prompt、源码、工具输入输出、MCP 返回内容、网络代理、主机路径或 Secret。残留 OpenSandbox server 管理卷无本轮归属，不进入 Admin 数据面；未到达 transport/recovery 的阶段不得推断为 supported。

模型 allowlist 阻塞期间，Admin 只投影稳定的 `MODEL_NOT_ALLOWED`/`provider_unavailable` 失败码和未形成 checkpoint 的 Operation/Audit 状态；不显示 Provider 返回原文、模型代理细节、Prompt、工具输入输出或 Secret，也不把未执行的 MCP/Skill 阶段标为 supported。

Compose selector 修复不扩大 Admin 数据面；transport-only 与 capability-bound recovery 的选择仍只产生稳定 Operation/Audit 状态，未执行的阶段保持不可见且不推断为 supported。

2026-09-24 r554 的 Admin 投影边界保持不变：仅允许展示 MCP/Skill opaque ID、版本、digest、权限、状态、兼容性、绑定关系以及 Operation/Audit 的稳定状态（包括 `provider_unavailable`、pending reconciliation 和 recovery outcome）。不得展示 Prompt、Bundle 源码、工具输入输出、MCP 返回内容、Secret、节点地址、主机路径或代理细节；本次真实结果也不把未执行的 Provider/环境阶段标为 supported。

2026-09-25 r559 deepseek-harness×Docker 只增加真实 opaque 结果：能力 ID、版本/digest、权限、兼容性、绑定、MCP/Skill 状态、transport/revocation/stale-generation Operation/Audit 和脱敏计数。Admin 不展示 DSH route、模型名、`tool-fs` 配置、Host persona、endpoint、主机路径、Prompt、Bundle 源码、工具输入输出、MCP 返回内容、代理变量或 Secret；第一次未进入 Provider 的 Worker upgrade preflight 只保留稳定 Operation 失败状态，不得显示为 Provider failure 或 supported。完整十二格与正式 Gate 仍开放。

2026-09-25 r561 Pi×Docker 只增加真实 opaque 结果：能力 ID、版本/digest、权限、兼容性、绑定、MCP/Skill 状态、transport/revocation/stale-generation Operation/Audit 和脱敏计数。Admin 不展示 Pi API 路由、模型名、endpoint、主机路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret；未覆盖的 RemoteWorker/Kubernetes、重启和跨节点阶段不得由本格结果推断为 supported。

同一 r561 的 Pi×RemoteWorker 结果只投影 opaque capability ID、版本/digest、权限、兼容性、绑定、`capabilityBound`、脱敏 RTO/RPO/attempt、transport/revocation/stale-generation Operation/Audit 和稳定计数。Admin 不展示 RemoteWorker 地址、目标节点、快照路径、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret；Pi×Kubernetes 与 Worker/Agent 退出仍不得由该结果推断为 supported。

r561 Pi×Kubernetes transport 重跑仅投影 opaque capability 元数据、版本/digest、权限、兼容性、绑定关系、已到达的 acceptance 计数和 `unexpected EOF` 的稳定未计数 Operation/Audit 失败状态；不得把 wrapper exit 1 或未形成的 transport/revocation/stale-generation 终态显示为 supported。acceptance-only 诊断中未产生 MCP call 也只记录稳定失败码；Admin 不展示 Kubernetes namespace/Pod/node、Provider 返回、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量、主机路径或 Secret。历史 r525 Pi×Kubernetes transport/revocation 的 opaque 通过记录保持独立，不由 r561 失败覆盖或外推。

r559 的 deepseek-harness×Kubernetes 仅出现终端 capability marker 但 wrapper 最终 exit 1，因此 Admin 只投影 opaque capability 元数据、版本/digest、权限、兼容性、绑定、未计数 transport/revocation/stale-generation Operation/Audit 和稳定失败状态；不得把该轮显示为 supported，也不显示 Provider 返回、Prompt、工具输入输出、MCP 返回内容、endpoint、节点地址或 Secret。RemoteWorker recovery 未形成 checkpoint 的轮次同样只显示稳定 fail-closed 状态。

2026-09-25 closeout projection：r568 的 Codex×Kubernetes cross-node 只向 Admin/Operation/Audit 暴露 opaque capability ID、版本/digest、绑定状态、attempt、恢复模式、脱敏 RTO/RPO、snapshot size、snapshot digest 是否已由 harness 输出、稳定副作用 outcome、事件/撤销计数和 Operation 状态；不展示 kind 节点名、namespace、Pod、Worker 地址、snapshot 内容、Prompt、Skill 源码、工具输入输出、MCP 返回内容、代理变量或 Secret。r562–r566 的 FAILED/BLOCKED 只投影稳定失败码、未形成 checkpoint/recovery 的状态和审计引用，不得显示为 supported；Kubernetes direct Sandbox Worker fault 的 NOT APPLICABLE 也只显示适用性状态，不推断存在 Worker Pod。Pi×Kubernetes 历史 r525 PASS 与 r561 `unexpected EOF` FAILED 必须作为独立审计记录保留。

r569 Codex×Kubernetes transport-only 只投影稳定的 `unexpected EOF`/外部 Kubernetes BLOCKED Operation 状态；没有 Provider、MCP、Skill、Artifact 或 transport marker 时，不展示任何 supported 状态，也不展示 token endpoint、namespace、Pod、节点、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。

r570 deepseek-harness×Kubernetes 只投影正向 acceptance 的 opaque 计数和 transport `unexpected EOF` 的 BLOCKED Operation/Audit；未形成 transport、撤销或旧 generation 终态时，不显示 supported，也不显示 fixture、Pod、namespace、节点、Prompt、源码、工具输入输出、MCP 返回内容或 Secret。

2026-09-25 r571 Admin projection：harness 已增加 opaque snapshot digest 的精确读取和格式校验，但真实重跑在 OpenSandbox 镜像拉取阶段因本机代理 `127.0.0.1:6152` connection refused BLOCKED，未产生新的 digest、Provider 或 cross-node 状态。Admin/Operation/Audit 只显示稳定的 `BLOCKED`、外部 registry/proxy 失败码和审计引用；不得把 r568 缺 digest 的 PASS 投影为完整 cross-node supported，也不得展示 registry URL、代理细节、节点、namespace、Pod、snapshot 内容、Prompt、Skill 源码、工具输入输出、MCP 返回内容或 Secret。r568 与 r571 必须作为独立记录保留。 日志 `.tmp/mcp-skill-runtime-v1-20260925-r571-codex-kubernetes-cross-node-digest-closeout.log` SHA-256 `798fbb9346e41d6e3e6451ae8f7639b1c2e6621b6986be4132fbab026413e752`。

2026-09-25 evidence consolidation projection：r575 的 OpenSandbox lifecycle 只允许显示 opaque `PASS / lifecycle-preflight`、固定版本/digest、health 状态和 cleanup 审计引用；不显示 registry URL、配置路径、Kubernetes kubeconfig、endpoint、主机路径或 Secret，也不把 readiness 显示为 Provider supported。Worker image precheck 同样只可显示固定 image digest、四个 package version/digest 和 precheck 状态，不能替代 Provider acceptance。

r582 Codex×Kubernetes snapshot closeout 可向 Admin/Operation/Audit 暴露 opaque provider/environment、capability binding 状态、attempt、恢复模式、脱敏 RTO/RPO、snapshot size、canonical snapshot digest、事件/撤销计数、side-effect outcome 和 evidence/log 审计引用。raw archive、Workspace/Sandbox 内容、PVC/PV、kind 节点、namespace、Pod、Worker 地址、registry/controller alias、Prompt、源码、工具输入输出、MCP 返回内容、代理变量、npm tarball 内容和 Secret 均不得进入页面或用户可读 receipt。r582 的 `PASS` 只适用于 Codex×Kubernetes capability-bound cross-node snapshot 子路径；transport 五格、Worker/Agent 的 `FAIL/BLOCKED/NOT APPLICABLE` 与其它 Provider×environment gap 必须以 06 的逐格状态投影，不能由该记录外推 supported。
