# 04. 基础设施、Admin Web 与 Anywhere Runtime 实施计划

## 文档清理与执行计划

这是本项目唯一的当前执行计划：文档收口 → §0 基础设施＋Admin Web → §0.4 Anywhere Runtime/SDK → 完整用户对话。实际状态和下一项只在 [06](06-status-tracker.md) 更新；下表保留已执行阶段的定义，不要求每次重做文档清理或重启已验收的 BASE。

| 顺序 | 工作与精确范围 | 完成条件 |
| --- | --- | --- |
| DOC-1 | 按 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md) 统一产品边界、授权识别与文档职责 | 所有活动入口都把基础设施＋Admin Web 作为第一阶段；文档集成依据用户最新明确合并授权，不外推代码实施或部署权限 |
| DOC-2 | 精简根/计划 README 与 CLAUDE；将 06 旧固定状态、04 旧迁移步骤、07 旧 ADMIN 里程碑移到下方指定历史记录；删除重复收口报告 | 默认入口没有第二套当前顺序、陈旧暂停指令或重复源码清单；历史约束可查但不自动加载 |
| DOC-3 | 核验本轮 diff、活动文档本地链接/锚点、HTML 结构、安全条款、被引用文件存在性及冻结输入字节 | 不修改契约/SQL/生成物/运行代码；只把实际通过的检查写入 06，不声称 runtime 或 Gate 验收 |
| DOC-4 | worktree 复核后按用户最新明确授权集成当前分支 | 重叠草稿先备份，无关未提交改动、运行代码和历史提交保留；核对完整文档 diff 与引用，只提交相关路径，不 push；实际结果只记录在 06 |
| BASE-M0～M5 | 下文的基础设施＋Admin Web 联合切片 | 对应后端、Admin 操作/状态/失败恢复、安全与真实验证同时达标；逐项完成 05 的 BASE-READY |
| APP-M1 | 先交付 Anywhere Runtime/SDK，再承接完整用户对话、任务、审批、历史和结果 | BASE-READY 后按 §0.4 推进；Runtime 子范围采用 ANYWHERE-RUNTIME-V1，不改写 BASE 完成条件 |

### 保留、清理与删除清单

| 文件或范围 | 处置 | 原因、依赖与恢复 |
| --- | --- | --- |
| `README.md`、`CLAUDE.md`、`docs/plan/README.md`、本目录 `README.md` | 保留入口，删除重复计划、历史进度长段与易过期的源码库存 | 入口只路由，不拥有第二份顺序或状态；原文可由 Git 提交 `ed7d3ac5` 恢复 |
| `01/02/03/04/05/06/07` | 保留并各司其职 | 当前架构、安全和执行规范；不按“文档多”删除有约束作用的正文 |
| `06` 的旧 P0/P1 状态/决策/Gate registry/checklist | 归档到 [历史状态快照](history/06-status-tracker-20260905.md)；活动 06 只存当前状态和入口 | 原固定状态与批准仍可追溯；不把旧“HTTP absent / PAUSED”当作当前源码或全仓禁令 |
| `04` 的旧 inventory/P0～P6/cutover/rollback 长段 | 归档到 [旧迁移计划](history/04-legacy-migration-plan.md) | 仅在实际涉及旧迁移/消费者时读取；其中数据迁移、回滚与删除的安全条件仍适用 |
| `07` 的旧 ADMIN-M1～M4 实施链 | 归档到 [旧 Admin 里程碑及固定验收](history/07-legacy-admin-milestones.md#admin-web-v1) | 平台默认主线用 BASE；明确的旧任务继续按 ADMIN-WEB-V1 验收，两套范围不互相追加或豁免 |
| `evidence/foundation-docs-realignment-20260905.md` | 删除重复的上轮文档整理报告，并移除索引链接 | 不是 Gate/E2E/生成器输入；本计划＋06 已承接结果；可从 `ed7d3ac5` 恢复 |
| `docs/coding_agent_cloud_infrastructure_design.html` | 保留完整架构参考，同步边界并指向本计划 | 图和示例不是当前 API、全部技术已选定或后续增强已获准的声明 |
| `docs/plan/synara-t3-cloud-agent-integration-architecture.md` | 保留后续消费者专题，移出默认执行阅读路径 | 不为当前第一阶段加载完整 T3/Synara 迁移；不得因此放宽已有消费者兼容要求 |
| 已有 ADR、`p0/`、`p1/`、`standalone/`、`legacy/`、`references/`、固定 `evidence/G-*/` 与 `apps/*/*E2E*` | 保留路径与原字节，按需查询，不做批量删除 | 部分文件由生成锁、closure profile、review digest 或来源 manifest 引用；缺文件/改字节会让生成或校验失败 |
| LICENSE、NOTICE、THIRD_PARTY_NOTICES、SOURCE_PROVENANCE、迁移 manifest、生成物/契约/SQL | 保留且本轮不改 | 法律、来源、ABI 和数据安全材料，不属于冗余说明 |
| 未列明的其他删除目标 | 先列精确文件、替代入口、反向引用和恢复来源，再判断 | 不按日期、文件名带 old、无近期访问或“历史已通过”就推定可删；涉及独立批准边界时仅暂停该删除动作 |

原稿主要风险及处理：`Admin 配套` 可能被解释为可后补 → 联合交付；多个入口的旧 `PAUSED` 与 `NOT STARTED` → 集中当前状态、历史按需；旧 `ADMIN-M*` 和 `P0～P6` 并列 → 只保留 BASE 当前顺序；“草案” → 不等于每个常规实现动作重新审批；删除证据 → 先查真实生成依赖。基础设施＋Admin 与用户对话边界不再按页面名称猜测。

### 每次如何继续

编码简化规则只维护在 [CLAUDE.md 的 Implementation simplicity](../../../CLAUDE.md#implementation-simplicity)；根 AGENTS.md 为 Codex 提供同一入口。按本切片处理重复来源，不另开全仓重构计划，也不以简化为由削弱迁移、权限或验收边界。

1. 先核对当前任务、验收标识、branch/worktree、dirty state 和相关源码；继续主计划时按 [06](06-status-tracker.md) 的“下一项”选择对应 BASE 或 APP 切片。明确的旧 ADMIN-M1～M4 任务按 ADMIN-WEB-V1，定点修复、审查或验证按其任务范围，不被默认下一项覆盖；不要从历史文档的一条未完成 checklist 重新启动旧项目。
2. 再读本文件对应切片及该切片所需的 01/02/03/05/07 段落；遇到具体契约/安全问题才查询相应 ADR、历史证据。搜索默认限制在当前规范与相关源码，不能把历史全文的指令当作当前任务。
3. 对已授权、目标和风险明确的常规修改、测试、幂等重试、状态记录继续执行，不重复确认同一事项。技术方案待验证不等于必须先人工批准每个字段。
4. 若缺少会改变权限、费用、数据保留或实施范围的选择，或触及明确批准要求，说明受影响动作与所需决定；保留状态并继续安全独立的已授权工作。不得默认生产写入、部署发布、迁移旧卷、读取用户内容或删除脏 worktree。
5. 单项任务按其明确范围、相关验证和结果记录判断完成；声明基础设施能力或 BASE 阶段完成时，才须同时满足对应后端与 Admin 闭环。单项任务完成不代表阶段通过，不能仅列待办、隐藏按钮或留下 Mock 就宣称能力已交付。无关旧 Gate 开放不反向阻塞当前切片，但对应正式 Gate 的证据/签署绝不免除。

文档收口的可验证目标是单一顺序、明确权限、真实状态、有效引用和无冻结输入破坏；它不能保证未知环境故障或所有未来任务绝不出错。新冲突按总入口规则定位到具体范围处理，不扩大为全项目停工。

## 0. 当前实施顺序：底座先行

[ADR-0032 / D-055](../adr/0032-infrastructure-admin-delivery-and-document-routing.md) 明确第一阶段完整交付基础设施＋Admin Web，第二阶段才是用户 CloudAgents 对话。下面 BASE 不是纯后端轨道：面向管理员的能力必须有对应可用管理页面与实测。以下是实施方案，不是已执行或所有生产动作已获准的声明。
阶段状态只维护在 [06](06-status-tracker.md)，整体底座就绪条件是 [05 的 BASE-READY](05-gates-and-acceptance.md#0-底座就绪验收-base-ready) 与 [07 的 BASE-ADMIN-V1](07-admin-web-requirements-and-design.md#base-admin-v1)，不再使用没有任务范围的“第 15 节全部标准”。

### 0.1 基础设施与 Admin 联合切片

| 顺序 | 底座切片 | 同阶段必交付 Admin Web | 退出证据 |
| --- | --- | --- | --- |
| BASE-M0 | 领域/API 映射与固定版本执行 PoC | 复用 Target、Operation/Audit、版本和能力事实视图；无数据的能力不建空页面 | 无 Agent 的真实 create → ready → exec/file → stop/delete；重复请求、失败补偿及卷不误删验证 |
| BASE-M1 | 长期 Workspace/Volume 与持久化生命周期调谐 | Workspace/Sandbox 分离列表；卷归属、保留规则、Operation/恢复状态 | 停止和重建保留代码；HTTP 断开、CP/Controller 重启后无需人工重发即可完成已接受操作 |
| BASE-M2 | 通用 Exec/PTY/Files、Preview/SSH 与访问隔离 | Port/Grant 元数据、到期/撤销、网络策略执行状态 | 无 Agent 客户端可连接和重连；跨租户/路径穿越/任意跳转被拒绝，网络规则实际生效 |
| BASE-M3 | 客户节点 RemoteWorker 接入 | 节点注册意图、owner、能力、心跳、Drain/Resume、版本与证书状态 | 仅 outbound/NAT 节点可承载同一 Workspace/Sandbox 流程；断连、重连、过期命令和旧 generation 正确处理 |
| BASE-M4 | Kubernetes 路径、资源池/容量调度与隔离等级 | Region/Pool/Node、RuntimeProfile、资源配额和调度失败原因 | Docker/Kubernetes/客户节点能力矩阵；容量不足、owner/runtime/arch 不匹配拒绝；强隔离实证 |
| BASE-M5 | 文件系统快照恢复、独立交付、计量与运维收口 | Snapshot/Restore、用量、失败积压、升级/回滚、恢复状态；管理页面整体回归 | 无 Agent 的完整底座矩阵、备份恢复/升级/故障演练与 Admin 验收；逐项通过 BASE-READY |
| APP-M1 | 四种 Agent 的 Anywhere Runtime/SDK，随后完整用户对话 | 相关 Provider、执行/恢复运维随切片交付；不加入对话/源码查看 | 按 §0.4 与 05 的 ANYWHERE-RUNTIME-V1，完成后再验收完整用户对话产品 |

BASE-M0～M5 共同组成第一阶段，不改名或重置旧 P0～P6、Portable Runtime M1、ADMIN-M* 的证据。M0 的执行候选 PoC 是技术验证，不宣告产品能力交付；从 M1 起每个面向管理员的能力都以真实后端＋Admin 流程作为同一个完成单元。
先 Docker 单 Region 验证产品语义，再扩展客户节点和 Kubernetes；不把只有 Docker 的结果声称为全部路径完成。
APP-M1 的新产品功能在 BASE-READY 后推进；已有 Agent 功能继续保留，必要的兼容/安全回归可以随底座进行。

### 0.2 各阶段的最小闭环

**BASE-M0：首先验证执行接缝。**

- 固定当前 source/dirty、既有 API/schema 和 OpenSandbox 候选版本、许可及能力差异；
  明确 Workspace、Sandbox、RemoteWorker 与旧 Lease/Worker/Profile 的映射，不按名字直接复用语义。
- 只定义首条链路必需的契约与迁移方案，复用现有生成链；不先生成全部未来资源的空 CRUD。
- 优先用 OpenSandbox adapter 验证真实 Docker Sandbox、Exec/Files、卷挂载和资源发现；
  验证不安装 Provider 也可执行，以及幂等重放、部分创建和异常清理。结果决定执行器复用/替换范围。
- 原 actuator 不删除，原 Lease 不迁移；新 PoC 只接触授权范围内的新测试资源。未通过则修复该接缝，
  或据实提出替代方案，不以此为由自动重建整套 sandbox engine。

**BASE-M1：先保证数据与操作不会随进程消失。**

- 新 Workspace/Volume 独立持久化；stop/TTL 释放计算、保留卷；删除工作区走独立授权与保留规则。
- 持久化 Operation/outbox 和 Controller 认领/重试真正接入部署路径；API 快速接受，状态可查询。
- 验证断开客户端、创建中重启、receipt 丢失、重复命令、失败回收及旧 generation；不依赖人工重发完成。
- 默认单写卷，测试旧写入者 fencing、同 Workspace 重建、越权挂载拒绝；升级复用卷不冒充任意跨节点迁移。
- Admin 必须区分 Workspace、Sandbox 与旧 Lease，清楚显示哪些数据会保留；策略值必须与执行值一致。

**BASE-M2：通用访问，而不是 Agent 工具的内部命令。**

- API/SDK/CLI 提供受限 Exec、PTY session、文件读写和端口发布；验证输出/缓冲/文件大小上限、
  reconnect cursor、路径/symlink 边界与内容访问所有权。
- 交付可独立运行的 Access Gateway；Preview 默认私有，SSH 短期凭据与固定 Sandbox 路由，
  expiry/revoke/generation rollover 后拒绝访问，不开放任意代理或客户主机 shell。
- 网络策略下发到实际执行路径，验证受控 DNS、metadata/宿主机/控制面/其他租户阻断和允许的外部访问。
- Admin 显示端点、Grant 和策略状态的脱敏元数据，不承载用户 Terminal/Files 内容；CLI/SDK 足以验证底座，
  不以完整用户 CloudAgents 页面作为本阶段前提。

**BASE-M3：主动连接的客户节点。**

- 实现 enrollment/CSR/mTLS 身份签发、轮换/吊销、节点能力/容量/版本上报、owner 约束和反向命令/访问通道。
- 至少一台仅允许 outbound 的真实测试节点完成创建、Exec/Files、连接、停止、重建；
  测试 NAT、断线恢复、幂等 command、deadline、incarnation/generation fencing。
- 离线停止新调度；重连先 reconcile，不盲目重放旧命令，也不因为离线删除 Workspace 或强挂卷。
- 客户节点默认只承载其所属租户；记录宿主管理员可读取本机数据的信任边界，不承诺对宿主管理员保密。

**BASE-M4：调度和执行矩阵。**

- 用同一基础 API 完成 Kubernetes 路径；声明每个 backend 支持的存储、访问、runtime 和恢复能力，
  不支持的组合在 admission 阶段拒绝，不退回另一安全等级。
- 建立最小 Region/ResourcePool/Node 模型与容量预留：硬过滤 owner、region、runtime、arch、卷可达性、
  节点健康、配额和 CPU/内存/存储；先用确定性选择，不先做成本预测和自动扩容。
- 区分共享不可信租户、可信单租户和专用节点；至少验证一个可用的强隔离 runtime 路径及其工具链/网络
  矩阵后，才能声称具备对应共享不可信租户能力。不能以设置 profile 名称代替实际隔离。
- 单 Region 多池足以退出；多 Region active-active、跨 Region 卷迁移、Warm Pool 和直接 MicroVM 不在当前必需项。

**BASE-M5：独立底座交付与恢复。**

- Workspace 文件系统快照、manifest、恢复到新卷/新 Sandbox、数据校验和保留清理；Secret 不进入快照。
  对无后端一致性快照能力的卷使用明确停写/离线快照，不冒充运行中一致性或内存恢复。
- 全新 Compose/Helm 安装与客户节点 bootstrap 文档；模板、runtime/worker/gateway 制品版本固定，
  支持声明的 N/N-1 升级、回滚、身份/证书轮换及可执行恢复 runbook。
- 持久化 CPU/内存分配时长、卷占用和支持的网络用量事实，含长任务 checkpoint、离线对账与可审计修正；
  不以 Prometheus 或 Provider token usage 作唯一事实源。完整价格、钱包和 invoice 后续实现。
- 测试 CP/Controller/Gateway/节点故障、备份恢复、限流/背压、容量和有界 soak；记录实际 P50/P95 与
  RPO/RTO，未经压测/演练不承诺 HTML 中的数字。根据适用风险执行既有安全/供应链检查。
- 收齐各阶段已交付的 Admin API 权限、危险操作确认、Operation/Audit、双语、可访问性和 Daytona 固定视觉验收；
  不能把历史截图或早期 Provider E2E 当作当前底座完整证明。

### 0.3 执行与完成约束

收到继续实施主计划的任务后，从 06 中最早未完成且在授权范围内的切片推进；BASE-READY 后进入 APP-M1，不重新打开已完成阶段。明确指定的后端、UI、契约、文档修复或审查/验证，按该任务范围完成，不自动扩成整个阶段；仍须验证其实际影响，不能据此豁免已受影响的 Admin 流程。
只有声明基础设施能力或 BASE 阶段完成时，才要求契约/后端、必要 SDK/CLI、相关 Admin 页面和真实验证共同满足；单项任务完成不代表阶段通过。后台可运行但管理闭环缺失，或页面可展示但依赖 Mock，均不能标记该能力完成。允许在同一联合切片内并行开发后端与页面，以及提前做安全独立的准备工作；不能把未完成的 Admin 工作整体移到下一阶段，也不能绕过依赖验收。

用户批准的产品边界已经记录，不重复要求确认同一边界；实现授权仍以当前任务和已有同范围授权为准。
需要新环境/凭据、改变数据保留、迁移现有卷或跨越单独批准要求时，只暂停受影响动作并说明需要什么，
继续可独立完成的在范围内工作。所有生产写入、部署/发布、脏 worktree 删除和正式 Gate 要求保持不变。

下文提供旧提取/cutover 方案的按需入口，供兼容与后续集成使用，不要求为新底座从头重做历史 inventory 或先完成真实 Agent/T3。

<a id="anywhere-runtime-plan"></a>

### 0.4 APP-M1：Anywhere Runtime 与 SDK

范围依据 [01 §1.3](01-product-scope-and-authority.md#13-anywhere-runtime-的产品目标)，接入和恢复语义见 [02 §0.6/0.7](02-target-architecture.md#06-agent-runtime-如何使用-workspacesandbox)。
`ANYWHERE-RUNTIME-V1` 覆盖下表 R1～R5；U1 是随后完整用户产品，不能把 Runtime 子范围完成当作整个 APP-M1 完成。
只定义当前切片必需的契约/迁移，复用现有生成链、Runtime、Provider、Controller、Worker、SDK 和 Admin 页面。

| 顺序 | 最小完整交付 | 退出条件 |
| --- | --- | --- |
| APP-M1-R1 | Codex/Claude Code 接通新 Workspace/Sandbox；统一 Agent 绑定、启动、事件、交互、结果及公共 SDK | Docker 上两个真实 Provider 经 SDK 完成持久 Workspace 的 Turn/文件/Artifact/后续 Turn；相关 Admin 状态、旧 Lease 与 no-Agent 回归。确认长任务超时策略，不能只复用旧 WorkerEndpoint 路径 |
| APP-M1-R2 | Pi 与 deepseek-harness Provider adapter、distribution 注册和固定版本制品 | 核验上游机器接口/许可/运行环境；四种 Provider 在同一公共契约下完成 Docker 真实流程，取消、交互与恢复能力据实声明；无 catalog-only 接入 |
| APP-M1-R3 | 可跨节点恢复的 Workspace 数据与状态引用 | 在原节点不可用时，从受验证的共享/复制存储或可达快照恢复到不同目标；校验归属、摘要、一致性点、旧 writer fencing、保留/清理与相关 Admin 操作；不得仅放宽 Target UID 校验 |
| APP-M1-R4 | 运行中 Turn 的持久认领、Agent Checkpoint、对账和跨节点接管 | CP/Agent/Worker/节点故障后，原执行可发现或新 attempt 安全接续；事件/交互/工具回执持久化，旧节点回归不能双写，副作用结果未知不盲重放；复用既有幂等/outbox/租约机制 |
| APP-M1-R5 | Docker、outbound RemoteWorker、Kubernetes 的完整 Provider/SDK/部署与运维验收 | 逐项通过 [05 的十二格及故障矩阵](05-gates-and-acceptance.md#anywhere-runtime-v1)，TS/Go 仓外消费、真实长任务、相关 Admin 双语/视觉/安全和 no-Agent 回归；记录实际 RTO/RPO 与限制 |
| APP-M1-U1 | 完整用户 CloudAgents 对话、任务、审批、历史、结果与恢复反馈 | 使用已验收的公开 Runtime/SDK 链路；另按用户产品任务验收，不混入本次 Runtime Goal 完成条件 |

R1 先贯通两个已有 Provider，R2 补新增 Provider，R3 为 R4 提供数据恢复前提；各切片同步补必要契约、SDK、Admin 与测试，
不将这些欠项统一推到 R5。环境/凭据暂缺时继续可独立实现和本地验证，缺失格保持开放，不删减 Provider 或故障条件。
R3 只以新测试 Workspace 验证，现有卷迁移另按授权办理；跨 Region 灾备和进程内存热迁移不属于 V1。

<a id="anywhere-runtime-goal"></a>

### 新 Goal 提示词：ANYWHERE-RUNTIME-V1

以下替换原 BASE 迁移提示词，供用户显式用于新的 Runtime 实施任务。保存或读取文档不创建/修改 Goal，不自动开始实现、部署或迁移其他任务；原 BASE 与 ADMIN-WEB-V1 的任务范围和证据保留。

```text
创建并持续推进 Goal：完成 Cloud Agents 的 ANYWHERE-RUNTIME-V1。

代码工作目录：/Users/huang/devel/project/huang/business/cloud-agents
工作分支：codex/cloud-agents-platform-p0；先核对 cwd、branch、HEAD、dirty/staged 和现有改动归属，不覆盖、回滚或提交无关修改。
先读 /Users/huang/devel/project/huang/business/cloud-agents/CLAUDE.md，执行其 Implementation simplicity 规则；AGENTS.md 仅路由到同一规范。
文档目录：/Users/huang/devel/project/huang/business/cloud-agents/docs/plan/cloud-agents-platform
先读 06 当前状态，再按 04 §0.4 的 APP-M1-R1～R5 执行；01 产品范围、02 接入/恢复架构、03 制品、05 ANYWHERE-RUNTIME-V1 和 07 §8.13 为对应约束。
04 是唯一计划，06 是唯一状态；记录实际 source/dirty、证据、下一项和阻塞，不维护第二套进度表，不从历史清单重启 BASE。

目标：Codex、Claude Code、Pi、deepseek-harness 通过统一 Provider/Agent Runtime 和公共 TypeScript/Go SDK，
在 Docker、outbound RemoteWorker 远程机器、Kubernetes 中使用长期 Workspace/Sandbox，支持真实任务、交互、事件续读、结果和故障恢复/转移。
先完成 R1 的两个已有 Provider 在新底座上的 Docker + SDK 纵向闭环，再补新增 Provider、跨节点数据恢复、运行中 Turn 接管及完整矩阵。
deepseek-harness 来源：https://github.com/deepseek-ai/deepseek-harness；固定版本并核验机器接口，不把 WebUI 启动或模型 endpoint 配置当作 Harness 接入。
保留 BASE-READY、BASE-ADMIN-V1、旧 ADMIN-WEB-V1 的原结论和现有 Agent/Lease 兼容；默认 Compose/Helm 仍可无 Agent 运行。
完整用户对话 UI 属于后续 APP-M1-U1；本 Goal 不扩展到 Synara/T3、Billing、Wallet、Marketplace、跨 Region 灾备或内存热迁移。

运行恢复必须持久化执行认领、事件/交互、工具意图/回执与可用 checkpoint；CP 重启不能因内存 owner 丢失直接判失败。
跨节点先证明旧 writer 已 fence，再恢复 Workspace 和兼容 Agent 状态，以新 attempt 接续；旧节点回归不得双执行或双写。
副作用结果未知必须显式待处理，不盲目重放。历史读回、同节点重连和文件快照不等于运行中 Turn 跨节点恢复。
相关 Admin 配置、能力/状态、失败恢复及 Operation/Audit 随每个切片完成，沿用 Daytona v0.190.0、zh-CN/en-US、双主题和可访问性要求。
User/Admin 身份、API 与内容权限分离；普通用户调用 Admin API 返回 403；Admin 不读取对话、源码、原生 cursor 或 Secret。
危险操作保留权限、影响清单、资源名称/generation 确认和审计。

授权本仓范围内的必要代码、契约/迁移源、生成 SDK、相关 Admin、文档修改及测试；允许为本 Goal 创建和精确清理本机临时 Docker/kind 测试资源、打包未发布的本地候选，不操作既有业务资源。
复用现有模块、Provider 协议、Controller/RemoteWorker、幂等/outbox/fencing、生成器和测试，不另造调度器、任务内核或通用框架。
涉及版本或迁移时，修复生成源和共享消费入口，不继续手工复制逐版本分支、白名单、路径、数量或摘要。
保留必要历史绑定和独立完整性校验，复用相关生成检查及回归测试；不把同构重复改成另一份手写表，也不另建通用框架。
每个切片完成实现与必要真实验证后自动继续下一项，不停在审计、方案或文档待办；提交时只包含本 Goal 的相关修改。
以 05 全部十二格、运行中故障矩阵、TS/Go 仓外真实调用、长任务、相关 Admin 和受影响底座回归作为完成条件。
记录固定版本、命令、数据摘要、实际 RTO/RPO、限制和清理结果；不以 Mock、build/lint、CLI 手工启动或历史不同制品证据冒充完成。
沿用仍有效的同动作/对象/环境授权；外部客户节点测试若无同范围授权或缺少凭据，只暂停对应动作并说明缺少什么，继续独立的本地工作。
不从历史记录读取或复用未授权的客户凭据；改变费用、数据保留、旧卷迁移或破坏性范围时单独处理具体决定。
不 push、不发布镜像/npm/Release、不操作生产、不删除脏 worktree、不修改其他任务；额外部署和正式 Gate closure 仍遵守既有明确批准要求。
同范围常规开发、验证和幂等重试不重复确认；未完成或受阻矩阵保持开放，不把 Goal 或整个 APP-M1 提前标为完成。
```

## 1. 兼容、迁移与回滚的按需入口

已有 Agent/Lease 调用方继续兼容；Workspace 数据归属和旧卷采用不能由文档更名隐式改变。实际涉及旧数据迁移、消费者 cutover 或破坏性回收时，读取 [旧迁移/回滚安全要求](history/04-legacy-migration-plan.md)，核验授权、恢复点、N/N-1 与单一 writer，再执行该范围任务。不要为了新的 BASE 工作重新运行旧 P0 inventory 或提前实施 Synara/T3。
