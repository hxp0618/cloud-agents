# 07. Admin Web 需求与交互设计

- 状态：DRAFT
- 日期：2026-09-05（底座配套重排；原需求日期 2026-09-03）
- 原实现参考：`codex/cloud-agents-platform-p0@a6dd495`；当前代码与验收须逐次核对，不能沿用此 ref 声称当前完成
- Daytona 参考基线：[`daytonaio/daytona@v0.190.0`](https://github.com/daytonaio/daytona/tree/v0.190.0)
- 前端技术栈：Vite + React + TypeScript

## 1. 文档目的

本文定义 Cloud Agents 的 User Web 与 Admin Web 产品边界，并给出 Admin Web 的功能、信息架构、
权限、安全、交互、接口和验收要求。

按 [ADR-0031 / D-054](../adr/0031-foundation-first-cloud-workspace-platform.md)，Admin Web 是长期云工作区、
通用 Sandbox 和客户节点底座的配套措施，与 BASE-M* 同步交付；完整用户 CloudAgents 后续接入。
本次更新产品对应关系和实施排序，不把所有 UI 细节、实现或部署标为已批准，也不改既有视觉和安全要求。

Admin Web 的页面布局、视觉风格、组件外观、响应式行为和交互反馈以 Daytona `v0.190.0` Dashboard 为
固定复刻基线。Cloud Agents 只替换品牌和业务资源语义，不复制 Daytona 的后端资源模型，也不把用户对话
与基础设施管理合并到同一个 Dashboard。

## 2. 产品目标

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

### 8.12 资源池与快照配套（BASE-M4/M5）

- Region/Pool/Node 只展示真实后端能力、容量与调度结果；不支持的 Runtime/network/storage 组合明确禁用并说明原因。
- Snapshot 仅展示归属、source Workspace、时间、容量、状态与恢复 Operation；不得读取快照内容。恢复须验证一致性和单写者 fencing。
- 复用 Storage、Maintenance、Audit 现有页面承载相关视图；有真实 API 和闭环再增加专门页面，不预建空壳。

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
  时，ADMIN-M1/ADMIN-M4 不得通过；
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

## 14. 配套实施顺序

当前顺序以 [04 的 BASE-M0～M5](04-extraction-and-migration.md#0-当前实施顺序底座先行) 为准。
每个基础能力都包含相关 Admin 元数据、必要操作和失败恢复反馈；契约/后端/API/SDK先形成真实行为，页面随之交付。
视觉与双语要求不降低，但不等待整站截图完成才开发下一个安全的后端切片。

| 既有 Admin 工作 | 当前承接位置 |
| --- | --- |
| Target/Lease/Operation/Audit、基础壳层与双语 | BASE-M0/M1 复用与补齐 |
| Agent Profile 与 User selector | 保留兼容；新底座 RuntimeProfile 在 BASE-M0/M4，用户 Agent 接入在 APP-M1 |
| Worker/Drain/升级/策略 | BASE-M1～M4 随真实生命周期、网络、客户节点和调度交付 |
| 独立鉴权/部署隔离、视觉和完整运维回归 | 从首条相关路径持续验证，BASE-M5 收口 |
| Codex/Claude Turn E2E | 既有兼容回归可运行；新用户产品完整验收归 APP-M1，不阻塞无 Agent 底座开发 |

### 14.1 历史 ADMIN-M1～ADMIN-M4 定义

下列旧里程碑保留用于解释现有报告，不再是当前平台推进顺序，也不要求按它们先完成用户 Agent 才推进底座。

本文使用 `ADMIN-M1`～`ADMIN-M4` 标识 Admin Web 里程碑；平台原有 `M1` 仍指 Portable Runtime。
两者不共享状态或授权，名称区分不解除任何暂停，也不构成新的实施或部署授权。

### ADMIN-M1：Admin Web 基础与 Target 运维

- 新增 `apps/admin-web`；
- 独立管理员鉴权和 Admin API；
- 1:1 复刻 Daytona `v0.190.0` Dashboard shell、Sidebar、页面布局和基础组件状态；
- 建立 `zh-CN` / `en-US` message catalog、语言选择、fallback 和持久化；
- Target 列表、注册、详情、Probe、Cleanup；
- Lease 运维列表；
- 操作确认和 Audit 最小闭环。

验收：Admin Web shell、Target 列表/详情和创建 Sheet 在 `zh-CN`、`en-US` 下通过 Daytona 视觉基线对比；
语言选择在刷新后恢复且不存在缺失翻译；管理员可完成 Docker/Kubernetes/SSH Target 注册到 Probe ready，
并能看到 operation/audit；普通用户 Token 调用相同 Admin API 返回 403。

### ADMIN-M2：Environment Profile 与 User Web 边界

- Profile 草稿、发布、禁用和不可变版本；
- User Web Profile selector；
- 按 Profile 服务端解析 Target、release、资源和凭据引用；
- 从 User Web 移除 Target/Lease 配置。

验收：用户请求和浏览器存储中均不存在 endpoint、`credentialRef`、`providerCredentialRef`；用户可以通过
已发布 Profile 创建真实 Codex/Claude Code Session/Turn。

### ADMIN-M3：Worker、升级和策略管理

- Cluster/Worker 运维视图；
- Drain/Resume、固定 digest Upgrade 和回滚；
- Images/Releases；
- Quota、Storage Policy、Network Policy；
- Maintenance Operation 统一页面。

验收：管理员能在至少一个 Docker、一个 Kubernetes 和一个 SSH 目标完成 Drain -> Upgrade -> Probe ->
Resume，并保留 generation、operation 和 audit 证据。

### ADMIN-M4：安全与真实 E2E

- User/Admin 独立部署和 OIDC audience/scope；
- 内容与 Secret 返回检查；
- Docker/Kubernetes/SSH 各一条真实部署、会话、终止和零残留清理路径；
- Codex 与 Claude Code 各至少一条真实 Turn；
- 失败、重试、并发冲突和刷新恢复验证。
- Daytona 基线页面在 `zh-CN` / `en-US`、light/dark 和桌面/移动 viewport 下完成视觉回归。

验收：Admin Web 全程只能看到运维元数据；User Web 只选择 Profile；终止后平台拥有的 Worker、容器/Pod、
Workspace volume 均按策略清理。

## 15. 实现验收标准

以下条件全部满足才可认为底座配套 Admin Web 实现完成。这些条件不作为设计文档完成或启动已授权实现的前置条件；
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

## 16. 已授权实现的 Goal 输入

收到实现任务后，在明确授权范围内从 04 最早未完成的 BASE 切片推进，并交付对应 Admin 配套；
只有用户明确指定一个独立 UI 修复时，才按该 UI 范围单独处理，不把它当成底座阶段已完成。
每次恢复核对当前 HEAD、dirty work、实际后端/API/SDK 和证据，复用已完成能力；
不得仅创建空页面、Mock 数据或没有后端 authority 的按钮。本次计划重排不修改现有 Goal/任务/自动化，
也不授予其他任务新权限；已有效的同范围授权无需重复确认。

## 17. 参考

- [Daytona `v0.190.0` Dashboard pages](https://github.com/daytonaio/daytona/tree/v0.190.0/apps/dashboard/src/pages)
- [Daytona `v0.190.0` Dashboard routing and permission wrappers](https://github.com/daytonaio/daytona/blob/v0.190.0/apps/dashboard/src/App.tsx)
- [Daytona `v0.190.0` Dashboard styles](https://github.com/daytonaio/daytona/blob/v0.190.0/apps/dashboard/src/index.css)
- [`apps/user-web`](../../../apps/user-web/README.md)
- [`DeploymentTarget` schema](../../../contracts/platform/v1alpha1/schemas/deployment-target.schema.json)
- [`CloudEnvironmentLease` schema](../../../contracts/platform/v1alpha1/schemas/environment-lease.schema.json)
- [`Managed Host` OpenAPI](../../../contracts/managed-host/v1alpha1/openapi.json)
