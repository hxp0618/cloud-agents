# 04. 文档收口与基础设施＋Admin Web 实施计划

## 文档清理与执行计划

这是本项目唯一的当前执行计划：先完成下表的文档收口，再按 §0 实施“基础设施＋Admin Web”，最后才推进用户对话。实际状态和下一项只在 [06](06-status-tracker.md) 更新；不在多个 README 再维护进度表。

| 顺序 | 工作与精确范围 | 完成条件 |
| --- | --- | --- |
| DOC-1 | 按 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md) 统一产品边界、授权识别与文档职责 | 所有活动入口都把基础设施＋Admin Web 作为第一阶段；文档集成依据用户最新明确合并授权，不外推代码实施或部署权限 |
| DOC-2 | 精简根/计划 README 与 CLAUDE；将 06 旧固定状态、04 旧迁移步骤、07 旧 ADMIN 里程碑移到下方指定历史记录；删除重复收口报告 | 默认入口没有第二套当前顺序、陈旧暂停指令或重复源码清单；历史约束可查但不自动加载 |
| DOC-3 | 核验本轮 diff、活动文档本地链接/锚点、HTML 结构、安全条款、被引用文件存在性及冻结输入字节 | 不修改契约/SQL/生成物/运行代码；只把实际通过的检查写入 06，不声称 runtime 或 Gate 验收 |
| DOC-4 | worktree 复核后按用户最新明确授权集成当前分支 | 重叠草稿先备份，无关未提交改动、运行代码和历史提交保留；核对完整文档 diff 与引用，只提交相关路径，不 push；实际结果只记录在 06 |
| BASE-M0～M5 | 下文的基础设施＋Admin Web 联合切片 | 对应后端、Admin 操作/状态/失败恢复、安全与真实验证同时达标；逐项完成 05 的 BASE-READY |
| APP-M1 | 用户 CloudAgents 对话、任务、审批、历史和结果 | 基础设施＋Admin Web 已就绪；现有应用只做必要兼容/安全回归，不抢占第一阶段 |

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

1. 先核对当前任务、验收标识、branch/worktree、dirty state 和相关源码；用户要求继续 BASE 主计划时，再按 [06](06-status-tracker.md) 的“下一项”选择工作。明确的旧 ADMIN-M1～M4 任务按 ADMIN-WEB-V1，定点修复、审查或验证按其任务范围，不被默认下一项覆盖；不要从历史文档的一条未完成 checklist 重新启动旧项目。
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
| APP-M1 | 用户 CloudAgents 接入已就绪底座 | 沿用底座运维入口；不加入对话/源码查看 | Codex/Claude 真实 Turn、Approval、历史与 Artifact 在持久 Workspace 上完成，底座回归通过 |

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

收到继续实施主计划的任务后，从最早未完成且在授权范围内的 BASE 切片推进。明确指定的后端、UI、契约、文档修复或审查/验证，按该任务范围完成，不自动扩成整个阶段；仍须验证其实际影响，不能据此豁免已受影响的 Admin 流程。
只有声明基础设施能力或 BASE 阶段完成时，才要求契约/后端、必要 SDK/CLI、相关 Admin 页面和真实验证共同满足；单项任务完成不代表阶段通过。后台可运行但管理闭环缺失，或页面可展示但依赖 Mock，均不能标记该能力完成。允许在同一联合切片内并行开发后端与页面，以及提前做安全独立的准备工作；不能把未完成的 Admin 工作整体移到下一阶段，也不能绕过依赖验收。

用户批准的产品边界已经记录，不重复要求确认同一边界；实现授权仍以当前任务和已有同范围授权为准。
需要新环境/凭据、改变数据保留、迁移现有卷或跨越单独批准要求时，只暂停受影响动作并说明需要什么，
继续可独立完成的在范围内工作。所有生产写入、部署/发布、脏 worktree 删除和正式 Gate 要求保持不变。

下文提供旧提取/cutover 方案的按需入口，供兼容与后续集成使用，不要求为新底座从头重做历史 inventory 或先完成真实 Agent/T3。

### 主执行任务迁移提示词

以下是把原 Admin M1～M4 任务改为当前 BASE 主线的替换输入，**会扩大原任务的交付范围**。仅在用户明确将它应用于目标任务时生效；保存或读取本文不自动发送消息、更新 Goal、开始实现或恢复已撤销的合并授权。尚未迁移的原任务继续使用 ADMIN-WEB-V1，未完成项不能被标为已验收。

以下提示词用于本轮文档集成后承接主执行任务；文档和代码均取自当前项目，避免继续依赖临时 worktree。先核对 06 的集成记录及 07 的两个验收标识；提示词本身不授权额外合并，也不自动迁移旧任务。

```text
将本任务从原 ADMIN-M1～M4 范围明确迁移为基础设施＋Admin Web 的 BASE 主线。

代码工作目录：/Users/huang/devel/project/huang/business/cloud-agents
工作分支：使用当前 codex/cloud-agents-platform-p0，不创建新分支，不覆盖或提交无关 dirty work。
本次文档来源：/Users/huang/devel/project/huang/business/cloud-agents/docs/plan/cloud-agents-platform
使用该来源的 04 实施计划、05 BASE-READY、07 BASE-ADMIN-V1；不要混用其他 checkout 的旧同名文档。
实施状态只记录在该项目的 docs/plan/cloud-agents-platform/06-status-tracker.md，标注所用文档版本和验收标识，不在其他 worktree 复制进度表。

目标：完成长期 Workspace、通用 Sandbox、outbound 客户节点接入及完整 Admin Web。
执行顺序：只按 04 的 BASE-M0～M5；默认完成终点为 BASE-READY 与 BASE-ADMIN-V1 全部通过。
保留已有 Agent/Lease/Profile/User Web 兼容性，复用已有 Admin 实现与仍有效的证据，不重新从零实现。
原 ADMIN-M1～M4 状态按原范围保留；没有完成的旧验收不能因迁移被标为通过。
新底座不依赖 Coding Agent Runtime 或 Provider 凭据；真实 Codex/Claude 可作既有兼容回归，
新用户 CloudAgents 的完整接入归 APP-M1，不把它作为 BASE 完成条件，也不在本任务自动扩展应用新功能。

继续遵守原 Daytona v0.190.0 固定 1:1、Vite/React/TypeScript、原生 CSS、生成 SDK要求，
以及 zh-CN/en-US 全覆盖、即时切换、持久化、Intl、fallback、双主题/桌面移动视觉与可访问性要求。
不引入原提示词禁止的框架、第三种语言或不必要依赖；不扩展到 Synara/T3、Billing、Wallet、Marketplace。
User/Admin API 和身份分离，普通用户 Token 调用 Admin API 返回 403；Admin 不读取用户内容或 Secret。
保留所有危险操作的权限、影响清单、资源名称/generation 确认、Operation 和 Audit。

每次恢复核对 cwd、branch、HEAD、dirty/staged、后端/API/SDK 与证据，从最早未完成且已授权的 BASE 切片推进。
完成必要 contract、后端、SDK、Admin 流程和真实验证，不用 Mock、空页面、构建或 Helm lint 冒充完成。
只提交本切片相关文件，自动继续下一项范围内工作；定点修复完成不代表整个阶段通过。
沿用原任务仍有效的同动作、同对象、同环境授权；迁移范围不扩大其资源或权限。
新环境/真实客户节点、凭据、费用、数据保留/旧卷迁移或破坏性操作需要新决定时，只暂停受影响动作，
请求具体授权并继续安全独立的已授权工作；同范围验证和幂等重试不重复确认。
生产写入、部署发布、脏 worktree 删除、正式 Gate 仍保留明确审批要求。
不要 push、发布镜像或创建 Release，不自动合并文档或修改其他任务，除非用户另行明确授权。
```

## 1. 兼容、迁移与回滚的按需入口

已有 Agent/Lease 调用方继续兼容；Workspace 数据归属和旧卷采用不能由文档更名隐式改变。实际涉及旧数据迁移、消费者 cutover 或破坏性回收时，读取 [旧迁移/回滚安全要求](history/04-legacy-migration-plan.md)，核验授权、恢复点、N/N-1 与单一 writer，再执行该范围任务。不要为了新的 BASE 工作重新运行旧 P0 inventory 或提前实施 Synara/T3。
