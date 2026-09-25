# 05. Gate 与验收

2026-09-24 新增边界证据仍不关闭正式 Gate：r542 candidate `0.3.0-dev.542` 的四 Provider×三环境 contract-only 负向真实通过不兼容版本/digest、跨租户 `401`、旧 generation `409`（各 12/12），但没有正向执行；r545 仅新增 Pi×RemoteWorker capability-bound cross-node/reconcile 子路径（RTO `10971ms`、RPO `0`、撤销负向通过，未开启 Worker/Agent 故障注入）。r544 Agent 与 r546 Worker 进程故障均没有终态 recovery marker，按 fail closed 不计数。完整十二格、逐环境断连重连、Pi/deepseek Worker/Agent、Codex Kubernetes cross-node 与未知副作用对账仍必须以真实终端证据关闭；详情见 [06](06-status-tracker.md)。

r548–r550 不关闭任何 Gate：Codex×Docker transport 断连后错误成功（不计通过）；r549 Claude×RemoteWorker 在首次能力执行 `provider_unavailable`；Pi×Kubernetes 首次 Docker 正向未产生 MCP call，未进入 transport/revocation。三轮均保留日志 digest、精确清理与 fail-closed 状态；不得把失败或未到达的阶段计为真实验收。

2026-09-22 r468/r469/r476 将同一 r459 candidate 的正向/恢复证据扩展到四 Provider×三环境的十二个 MCP/Skill 子路径；r482/r480/r483 又在隔离 kind 三节点上真实完成 Claude、受控 gpt-5.5 deepseek-harness、受控 gpt-5.5 Pi 的 Kubernetes `cross-node-takeover/recovered`，而 Codex 唯一允许的 r467 重跑仅为 Kubernetes 同节点 `process-restart`。r490（受控 gpt-5.5 Codex）和 r491（受控 gpt-5.5 Claude）完整通过 Worker/Agent 双进程故障、MCP/Skill、事件续读和撤销；Pi/deepseek 因 adapter 没有持久化 user-input/approval callback，Worker/Agent 进程故障仍未覆盖。这些是逐格真实子证据，不代表正式 Gate 已关闭；日志、RTO/RPO、candidate digest 和模型 caveat 见 [06](06-status-tracker.md)。

r471（Docker+Kubernetes）与 r472（Docker+RemoteWorker）真实完成四 Provider 的边界矩阵：不兼容 MCP/Skill 版本/digest fail closed，跨租户读取为 HTTP `401`，旧 generation `1→2` 为 HTTP `409`，两轮 exit `0`。未知副作用恢复保持先拒绝盲目 replay、再以 checkpoint digest/outcome reconcile；`confirmed`、`not-applied` 和 `awaiting_reconciliation/side_effect_outcome_unknown` 均按真实事件流记录。r473/r474/r475 是未计数的前置或 harness 诊断，不得改变 Gate 状态。

本轮清理核对为 task-owned Compose/OpenSandbox/Kubernetes 临时资源和 fixture `0`，secret scan `0`；r471/r472 已真实覆盖四 Provider 的不兼容版本/digest、跨租户 `401`、旧 generation `409`，四 Provider×三环境的凭据撤销负向也已有 `12/12` 终端证据；未知副作用仍以 `awaiting_reconciliation/side_effect_outcome_unknown` fail closed，必须完成 digest/outcome reconcile。正式 Gate 仍要求 Pi/deepseek Worker/Agent restart、Codex Kubernetes 真跨节点、每个 Provider×环境完整断连重连组合，以及恢复后 Operation/Audit 对账全部有终端证据；静态 catalog、Mock、build/lint、手工启动和单节点 Kubernetes 不能替代这些证据。

2026-09-22 r463 只新增一条真实子验收：在家庭代理、dirty candidate `0.0.0-dev.r459` 和受保护 Pi 凭据临时副本下，OrbStack 单节点 Pi×Kubernetes 完成 capability-bound `process-restart/recovered`（attempt 2、`capabilityBound=true`、side effect `confirmed`、RTO `0ms`、RPO `0`、snapshot `0`），MCP/Skill/Artifact/事件续读及撤销负向均通过（正向 `8/1/1/verified/resumed`，撤销 `0/0/2`），脚本 exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260922-r463-pi-kubernetes-recovery-order.log` digest `sha256:326dc8a9423267ba59e89d788e529ec34534da29a190eed6236b95abef5cf64e`；fixture、Compose/OpenSandbox managed volume 清理为 0，secret scan 命中 0。该证据不关闭 Pi/Kubernetes cross-node、Worker/Agent、其它 Provider、逐项负向或正式十二格 Gate；相对路径 preflight r462 exit 2 不计数。

2026-09-21 r459 真实关闭 Codex×Docker 与 Codex×RemoteWorker capability-bound cross-node 子验收：未发布 candidate `0.0.0-dev.r459`（platform manifest `sha256:e558d3f9c1e7bb88a4fa23a9a4ea074996de84d2dd04842e379c50dd691d4774`、checksums `sha256:2f3ff3c9ffefd52a6cd3ef47537b77ae0167d72ee98bd7dff23d8bcb84243699`）在公司代理下通过 MCP call、签名 Skill load、Artifact、事件续读和撤销负向；RemoteWorker 源节点失效后为 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered`，side effect `not-applied` 经显式 reconcile 后恢复，RTO `10716ms`、`RPO` `0`、snapshot `3542528` bytes，恢复后 `mcp_requests=28`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`。日志 SHA-256 `sha256:112aa2ac94cddb52649f061bfa50d978b29c31234ee9f32e3e6e1202758cfb13`；migration `000100` 与生成链检查通过，脚本/Compose smoke exit 0，临时项目资源为 0。

该证据只登记 Codex Docker/RemoteWorker 子路径；Codex Kubernetes、其它 Provider 的十二格交叉组合、Worker/Agent 重启、版本不兼容/跨租户/旧 generation/未知副作用逐项矩阵及正式十二格 Gate 仍开放，不把静态 capability catalog、Mock、build/lint 或手工启动计为通过。

同一 r459 candidate 在 OrbStack `orbstack` 单节点 Kubernetes 上另有真实 Codex 正向/撤销证据：`mcp_requests=14`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`，撤销后 `runtime_requests=0`、`side_effects=0`、`events=2`；日志 SHA-256 `sha256:b3eaa94cc7403647af2247d98237a3208fa406a4492bfeed1db535961811f5cf`。整体脚本随后因未形成 pending-side-effect checkpoint 而 exit 1，故不计 Kubernetes recovery；本地 PostgreSQL 并发更新和 recovery session `409` 的重试也不计数。该证据只关闭 Codex×Kubernetes 正向/撤销子路径，不关闭跨节点、Worker/Agent 重启、未知副作用矩阵或正式十二格 Gate。

同一 r459 candidate 在公司代理和用户新提供的 Pi/deepseek-harness tenant-local 凭据临时副本下，Pi×Docker 与 deepseek-harness×Docker 均真实通过 MCP call、签名 Skill、Artifact、事件续读和撤销负向（各 `mcp_requests=8`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`；撤销后零请求/零副作用/2 events），脚本与 Compose smoke exit 0。日志分别为 `.tmp/mcp-skill-runtime-v1-20260921-r459-pi-docker.log`（`sha256:a1d1f4e8071595beb65cbea753e0bcef8252b401a05339ac3696bb0fb2d8845c`）和 `.tmp/mcp-skill-runtime-v1-20260921-r459-deepseek-docker.log`（`sha256:3e49902ccad8af833c27aabf04cce7302ede5415159020148fda506f037f2c7b`）。临时凭据目录及项目标签资源精确清零，原始凭据未修改；该证据只登记当前 candidate 的两个 Docker capability 子路径，不关闭 RemoteWorker/Kubernetes、重启/跨节点、逐项负向或正式十二格 Gate。

同一 r459 candidate 随后真实完成 Pi×RemoteWorker 与 deepseek-harness×RemoteWorker 的 capability-bound cross-node 子验收：两轮 Docker/RemoteWorker 均 MCP、Skill、Artifact、事件续读通过，RemoteWorker 源节点失效后 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered`、side effect `confirmed`、RPO `0`；Pi RTO `9724ms`/snapshot `23040` bytes，日志 digest `sha256:6de9b98e3de52b0589e12e2d93a9ff1f93d452af4ace39a6ba9edcfec6236160`，deepseek-harness RTO `7514ms`/snapshot `851456` bytes，日志 digest `sha256:bf8706b56ca57d46b2a0d133243405a04a0e131bfb605a27d1cde7c604ef5d5c`。恢复后两轮均 `mcp_requests=14`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`，撤销后零请求/零副作用/2 events，脚本与 Compose smoke exit 0；本轮凭据/物料清理、secret scan 和原始凭据保护通过。该结果只登记两个 Provider 的 RemoteWorker capability-bound cross-node 子路径，不关闭 Kubernetes、Worker/Agent 退出、逐项版本/租户/generation/未知副作用或正式十二格 Gate。

同一 r459 candidate 的 Pi×Kubernetes 与 deepseek-harness×Kubernetes 正向/撤销子路径也真实通过（各 `mcp_requests=8`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`；撤销后零请求/零副作用/2 events），日志 digest 分别为 `sha256:8f072bdb5e0adca9e1edee0fb65171ecf71a539be7f360ebd1c6c0d4475f8686` 与 `sha256:839f2b0bc492f282cf697614e54dc9858f3fde9e30a8cfa66798b519fd73b4db`。两轮 recovery 未形成 pending-side-effect checkpoint，整体脚本 exit 1，按 fail closed 不计 Kubernetes recovery/cross-node；因此不关闭任何整格或正式 Gate。

2026-09-21 r448 真实完成 Claude×RemoteWorker capability-bound cross-node 子验收：Docker/RemoteWorker 均 `mcp_requests=8/14`、`side_effects=1`、`skill=1`、Artifact verified、events resumed；Control Plane 为 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered/confirmed`、RTO `8630ms`、RPO `0`，撤销负向为零请求/零副作用/2 events，脚本与 Compose smoke exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260921-r448-claude-remote-worker-capability-cross-node.log` digest `sha256:3a146ad78c5614c524b400ac59edd5b79b74b49a29972aa110f843fb8c686b86`；r446/r447 的 fail-closed 前置/物料诊断不计数，版本不兼容、跨租户、旧 generation、Worker/Agent 重启、未知副作用和完整十二格仍开放。

Pi×Docker 与 deepseek-harness×Docker 的 r405/r406 已在公司代理、loopback MCP fixture 和合法签名 Skill 物料下真实通过（各 `mcp_requests=8`、`side_effects=1`、`skill=1`、Artifact verified、事件 resumed，撤销后 `runtime_requests=0`/`side_effects=0`）；r402/r403 的错误物料失败不计入。该结果只关闭两个 Docker capability 子路径，不关闭完整十二格或正式 Gate；RemoteWorker/Kubernetes、重启/跨节点和逐项负向仍需真实验收。

r407/r408 又真实通过 Pi/deepseek-harness × RemoteWorker 的同等正向、撤销、Control Plane 重启和跨节点 takeover；两轮 recovery 的 `capabilityBound=false`，因此只登记 RemoteWorker capability 子路径，不关闭能力绑定跨节点或完整十二格。

r409/r410 又真实通过 Pi/deepseek-harness × Kubernetes 的同等正向、撤销和 Control Plane process-restart；OrbStack 为单节点，未执行 Kubernetes cross-node takeover，故不关闭能力绑定跨节点或完整十二格。

Codex×Docker Worker recovery 子路径现有真实 exit-0 证据（候选 `0.0.0-dev.r398`、r399 日志）；当前候选的 Claude×Docker r414 与 Claude×Kubernetes r415（OrbStack 单节点）也通过 MCP/Skill/Artifact/事件续读、同节点 capability-bound process-restart/reconcile 和撤销负向，但不覆盖 Kubernetes cross-node 或 Worker/Agent 重启；其余 Provider/环境组合、逐项负向和完整十二格仍未关闭。

r422 的 Claude Docker Worker+Agent 组合中，Worker/Agent 进程恢复本身均 attempt 2/recovered，但后续 pending-side-effect 能力恢复以 `provider_unavailable` fail closed，未形成可计入的完整组合证据；不改变十二格或正式 Gate 状态。

r423 重试完整通过 Claude×Docker Worker/Agent capability recovery：MCP 8、side effect 1、Skill 1、Artifact verified、事件 resumed，Worker/Agent 恢复 attempt 2/recovered，Control Plane capability-bound process-restart/confirmed、RPO 0，撤销负向为零请求/零副作用；该结果只登记 Claude×Docker 子路径，十二格和正式 Gate 仍开放。

r427 完成 Pi×RemoteWorker 同节点 capability-bound recovery：Docker/RemoteWorker 各 MCP 8、side effect 1、Skill 1、Artifact verified、events resumed，RemoteWorker process-restart、撤销负向均通过；不关闭 Pi 跨节点/Kubernetes Worker/Agent 或十二格 Gate。

r428 完成 deepseek-harness×RemoteWorker 同节点 capability-bound recovery：Docker/RemoteWorker 各 MCP 8、side effect 1、Skill 1、Artifact verified、events resumed，process-restart 与 RemoteWorker 撤销负向均通过；十二格和正式 Gate 仍开放。

r429 完成 Pi×Kubernetes（OrbStack 单节点）同节点 capability-bound recovery：Docker/Kubernetes 各 MCP 8、side effect 1、Skill 1、Artifact verified、events resumed，Kubernetes process-restart 与撤销负向均通过；未执行 Kubernetes cross-node，十二格和正式 Gate 仍开放。

修复前 Worker recovery 的诊断仅保留脱敏 `mcp` 错误类别；独立 Docker `--network container:` 测试显示 owner 重启后 loopback fixture 不可达、重启 fixture 后恢复。该历史隔离行为不替代当前 r399 的真实子路径证据，也不扩展为其它十二格。

2026-09-20 MCP-SKILL-RUNTIME-V1 当前证据：r384 的 Claude×Docker 子路径已真实通过 MCP call（8 requests/1 side effect）、qualified Skill load、ArtifactCandidate、事件续读、Control Plane SIGKILL 后 capability-bound process-restart/reconcile（attempt 2 confirmed，RPO 0）及撤销后零请求/零副作用；Browser smoke 与 Admin boundary 同轮通过。随后同一 candidate 的 Claude×RemoteWorker 正向 MCP/Skill/事件续读、capability-bound process-restart/reconcile（attempt 2 confirmed，RPO 0）、撤销负向和跨节点 takeover（RTO 9722 ms、RPO 0、snapshot 1399296 bytes）也真实通过。Codex×Docker/RemoteWorker/Kubernetes 也真实通过 MCP/Skill/事件续读、撤销和恢复子路径，RemoteWorker 跨节点 takeover 为 RTO 9690 ms、RPO 0、snapshot 13800960 bytes，Kubernetes process-restart 为 attempt 2 confirmed，最终 Compose smoke 通过；r387 另真实通过 Codex×Docker Agent Runtime 进程退出后的 capability-bound recovery（attempt 2、recovered、MCP 40 requests、Skill 1、事件 resumed 6/post-terminal 2）。r413 的 Claude×Kubernetes 首次复跑在 Docker `file_change=failed` 阶段 fail closed，随后 r414 Claude×Docker 与 r415 Claude×Kubernetes 单节点重跑完整通过 MCP/Skill/Artifact/事件续读、同节点 capability-bound process-restart/reconcile 和撤销负向；r417、r418 又分别补齐 Pi×Docker 与 deepseek-harness×Docker capability-bound process-restart/reconcile（MCP/Skill/Artifact/事件续读、attempt 2 confirmed、RPO 0、撤销负向）。当前 candidate 的 Docker/Kubernetes/RemoteWorker contract-negative、跨租户 401 和 stale-generation 1→2/409 也真实通过，但不替代 Provider 正向；四 Provider Worker/Agent 重启、Kubernetes 跨节点和其它逐格组合仍不计入。该证据不关闭十二格、正式 Gate 或未知副作用恢复要求；制品、日志摘要、命令、清理及未覆盖项见 [06](06-status-tracker.md)。

## 0. 底座就绪验收 BASE-READY

依据 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)，第一阶段是基础设施＋完整 Admin Web，两者共同通过 BASE-READY 后再交付用户 CloudAgents 对话。
BASE-READY 是本次提出的底座产品就绪检查，不是新增审批工具、历史 Gate 重命名或发布批准。
实施顺序见 [04](04-extraction-and-migration.md#0-当前实施顺序底座先行)；实际状态在 [06](06-status-tracker.md) 登记。

no-Agent 指不依赖用户对话、Coding Agent Runtime 或 Codex/Claude 凭据，不指没有 Admin Web；RemoteWorker/SandboxAgent 等基础设施进程不在此处排除的 Coding Agent 范围。下面十二项必须同时满足；其中 11/12 与后端条目同为必需条件，CLI/SDK 实测不能替代管理页面验收：

1. 独立 API/SDK/CLI 创建 Workspace 和 Sandbox，真实 Exec/PTY/Files 可用；用户 CloudAgents、Synara/T3
   均不是底座安装或验收前提。
2. 停止、TTL 到期或替换 Sandbox 后 Workspace/Volume 保留；重新启动可读回测试文件及摘要。
   删除工作区是独立授权动作，按声明的快照/保留规则执行，不误删其他 Workspace 或用户已有资源。
3. API 接受后即使 HTTP 断连、CP/Controller 重启、receipt 丢失，持久化 Operation 仍能自动恢复到明确终态；
   相同幂等键不重复创建，部分分配被 adopt/补偿，失败清理保留 finalizer 并可观察。
4. Workspace 单写与 generation fencing、卷归属、挂载授权、旧节点重连和过期命令负向矩阵通过；
   不能只凭健康超时强制把未 fence 的卷交给新 writer。
5. Preview 私有默认、PTY/Files 可重连、SSH 短期授权；跨租户、路径/symlink 逃逸、任意目标代理、
   过期/吊销/旧 generation 访问均被拒绝，Gateway 重启恢复行为与缓冲上限有实测。
6. 客户节点只通过 outbound 接入，完成注册/轮换/Drain/断线/重连/对账；平台无客户公网入站 SSH 依赖。
   节点只接收 ownerScope 允许的任务，离线不误删数据。
7. Docker、Kubernetes、客户节点各有真实执行与恢复证据；能力矩阵包含 runtime/arch/存储/网络限制，
   不支持组合拒绝，不以 Probe、Mock 或单一路径替代全部矩阵。
8. 身份/RLS/RBAC、容量与配额准入、受控 DNS、metadata/宿主/控制面/跨租户网络隔离实际生效；
   共享不可信租户必须有对应强隔离实证，不能用可信 runc 路径冒充。Secret 不进页面、日志、receipt 或快照。
9. Workspace 文件系统快照恢复到新 Volume/Sandbox 并核对数据；数据库备份恢复、证书轮换、
   N/N-1 升级回滚、节点故障恢复和孤儿资源清理有可复现记录，不声称未验证的进程内存或跨 Region 恢复。
10. 基础用量事实、长运行 checkpoint、离线对账、修正审计可核对；健康、调谐积压、容量、失败与告警可观察，
    有实际延迟/soak/恢复测量和 runbook，不将工程目标直接写成 SLO 承诺。
11. Admin Web 对本阶段真实资源提供完整的配置、监控、维护、失败恢复及 Operation/Audit；管理员不读取用户内容，
    普通用户不能调用 Admin API；危险操作保留影响范围、资源名称和 generation 确认。
12. Admin 必交付页面完成 [07 的 BASE-ADMIN-V1](07-admin-web-requirements-and-design.md#base-admin-v1) 全部条件，包括双语、可访问性、视觉与身份部署隔离；
    用户侧 Agent 流程留在 APP-M1 验收，不把其尚未接入当作通用底座失败。

每项记录 source/dirty、backend/runtime/制品版本、输入、命令、实际结果和恢复边界。复用已有检查和证据路径，
仅重验被改动影响的结论，不为每个切片先建设新的证据生成器。证据在执行后如实记录，不能要求先有通过报告再开始实现。
BASE-M5 只有逐项满足以上条件才能标为就绪；未完成项保持开放，不降为隐藏按钮或文档占位。

### 0.1 与旧 Gate、Release 的关系

下文旧 P0～P6/Platform RC 的 record、签署、审批、安全和发布条件保持不变，不因底座优先而自动关闭或降低。
底座本地就绪不要求先完成 Synara/T3 或真实 Agent E2E，但也不代表通过仍包含这些条件的旧完整 Platform RC。
若以后发布独立底座 channel，其适用验收与曝光范围须显式记录并取得原有发布批准；本次不创建或批准该 channel。
APP-M1 按下节推进 Anywhere Runtime，再完成用户对话产品；后续消费者各自关闭适用 Gate。

原 Admin M1～M4 任务另按 [ADMIN-WEB-V1](07-admin-web-requirements-and-design.md#admin-web-v1) 验收；其中既有 Agent 的真实 Codex/Claude E2E 仍是原任务必需项，不因 BASE 的 no-Agent 范围而豁免。ADMIN-WEB-V1 与 BASE-READY 不互相替代，也不互为启动或完成的通用前置条件；只有明确迁移后的任务才改用 BASE 范围。

<a id="anywhere-runtime-v1"></a>

### 0.2 Anywhere Runtime 验收 ANYWHERE-RUNTIME-V1

这是 BASE-READY 之后 APP-M1 内的 Runtime/SDK 完成条件，不是新的正式审批 Gate，也不改写旧范围的完成结论。
切片顺序只在 [04](04-extraction-and-migration.md#anywhere-runtime-plan)，实际支持/缺失和证据只在 [06](06-status-tracker.md)。

**Provider × 环境：以下十二格均需真实通过。** 每格固定上游与平台制品、OS/arch、隔离、存储、网络和凭据方式；
至少由一个公共 SDK 完成新 Workspace/Sandbox → AgentSession → Turn → 增量事件 → 工具修改文件并核对摘要 →
结果/Artifact → 后续 Turn/会话恢复 → 关闭计算保留数据的完整链路。不是依靠手工进入容器启动 CLI。

| Agent Provider | Docker | 远程机器（outbound RemoteWorker） | Kubernetes |
| --- | --- | --- | --- |
| Codex | 必验 | 必验 | 必验 |
| Claude Code | 必验 | 必验 | 必验 |
| Pi | 必验 | 必验 | 必验 |
| deepseek-harness | 必验 | 必验 | 必验 |

不支持的额外能力或环境组合必须明确拒绝；但上表核心路径缺失、缺凭据或未实测时，该格保持未完成。
可使用经验证的适配实现补齐核心能力；不得靠把 Provider 标为 experimental/unsupported 从十二格中删除它。
原生 diff、内存 checkpoint 等附加能力分别声明 native/emulated/unsupported，不伪造 Provider 原生能力。

**恢复与故障转移：** 下列故障逐类登记四种 Provider 在三类环境中的适用实例和结果；共享机制的测试可复用，
但每一 Provider/环境组合都须有运行中 Turn 的真实恢复和跨节点接管证据，不能只测已结束 Turn。

| 故障/窗口 | 必须观察到的行为 |
| --- | --- |
| SDK/事件流断连，CP 请求响应丢失 | 幂等重试不新增 Turn；从已持久化 cursor 续读，重复事件可去重，无已确认事件缺口 |
| 运行中 CP/Controller 崩溃 | 重启认领和对账同一执行，继续接收事件/交互；不因内存 active map 丢失直接终结任务 |
| Agent 进程或 Worker 退出 | 已确认输入、持久化交互和安全 checkpoint 可恢复；以新 attempt 继续，旧回执不能覆盖结果 |
| 执行节点彻底不可用 | 在同 Region 的另一兼容节点恢复 Workspace/Agent 状态并接管；核对新节点 ID、数据摘要、后续工具结果及可观察恢复原因 |
| 旧节点恢复、延迟命令/回执到达 | 原 writer 已 fence；旧 generation 不得写卷、取密、发事件或提交终态；无双执行/双写 |
| 工具副作用前后崩溃、回执丢失 | 有幂等或可核对结果时安全恢复；结果未知时显式停在待处理状态，禁止自动重复外部副作用 |
| 快照/checkpoint 缺失、损坏或版本不兼容 | 校验失败并保留原数据，明确可恢复点与丢失窗口；不退回空目录继续冒充恢复 |
| Approval/User Input 等待中故障，授权过期或撤销 | 交互持久化、回答幂等并重新校验授权；跨租户/旧 attempt/过期 grant 拒绝 |

跨节点验收必须包含旧主机不能提供本地盘的情形，证明存储副本/远端快照可用与旧 writer 隔离；
Kubernetes Pod 在原节点重建、RemoteWorker 重连原节点、同 Target Docker Snapshot Restore 均不足以替代。
每类报告 RTO（故障至安全恢复执行）、RPO（可恢复一致性点及未确认窗口）、故障持续时间和恢复尝试；
区分已接受的控制事件与尚未 checkpoint 的 Provider/文件状态，不从单次通过推导生产 SLO。

**SDK、部署与运维：** TypeScript 与 Go 各在仓外安装固定候选，实际验证 Session/Turn、事件续读、取消/中断、
Approval/Input、Artifact、恢复状态与幂等错误；用户只使用公开 ID/引用，不依赖节点私网地址或内部服务包。
覆盖超过当前短 Turn 上限的任务、断连续接、超时/取消与残留进程清理；明确实际时长与资源策略，不承诺无限运行。
显式 Agent profile 在 Docker/远程节点/Kubernetes 可安装；默认 no-Agent 仍通过受影响回归。
[07 的 Runtime 运维要求](07-admin-web-requirements-and-design.md#813-anywhere-runtime-运维app-m1) 必须随实际能力完成，
含权限/租户隔离、故障原因、恢复 Operation/Audit、双语和相关视觉/交互验证，管理员仍不能读取用户内容或 Secret。

沿用现有测试、固定报告与证据目录，记录 source/dirty、命令、制品、输入、结果、未覆盖项和精确清理。
Mock、build/lint、探活或历史其他制品的成功不替代本节真实验收；全部必需项完成才可标记 ANYWHERE-RUNTIME-V1 VERIFIED。

<a id="mcp-skill-runtime-v1"></a>

### 0.3 MCP-SKILL-RUNTIME-V1 验收（进行中）

该 Goal 建立在已完成的 Anywhere Runtime 上，不重置 ANYWHERE-RUNTIME-V1 的历史结论，也不关闭正式 Gate。MCP Server 与 Skill Bundle 必须由同一份 JSON Schema/OpenAPI/Proto 生成链产生；公开 SDK 只接受 opaque 引用、版本和 digest。Runtime 在 Sandbox、outbound RemoteWorker、Kubernetes 中使用同一 binding/manifest digest，并保留租户/项目 RLS、短期 grant、网络 allowlist、撤销检查、generation fencing、Operation/Audit 和副作用对账。

四个 Provider × 三种环境仍是十二格必验：每格必须真实完成 MCP 调用或 Skill 发现/加载、增量事件、断连重连、Control Plane/Worker/Agent 重启、凭据撤销、版本不兼容、跨租户拒绝、旧 generation 拒绝与安全恢复。MCP 不可用时 fail closed；结果未知时不得盲目重放外部副作用。Admin 只展示 opaque ID、版本、digest、权限、兼容性、绑定关系及 Operation/Audit 元数据，不展示 Prompt、源码、工具输入输出、MCP 返回内容或 Secret。

当前实现已通过合同/生成/数据库迁移、Runtime manifest 校验与撤销 fail-closed、loopback MCP broker、operator-owned 短期 materialization FD、签名/digest 校验的只读 Skill Bundle、脱敏 capability event、Admin 元数据页和 Provider adapter 安全测试。Codex adapter 当前使用 app-server 原生 Host-managed `mcp_servers`、结构化 `SkillUserInput` 和原生 MCP/Skill event mapping；真实 Docker r353 只证明 initialize/initialized/tools/list，所给 `gpt-5.6-luna` 没有产生 MCP call、Skill、外部副作用或 Artifact，因此不算成功。Pi 的真实 Skill 子验收未经过完整矩阵且 MCP unsupported；deepseek 只有隔离 Runtime 预检和 adapter 机制。不得把这些协议、预检、catalog 或静态结果写成 supported。

Claude 的隔离 Docker Runtime 子验收另已在 candidate `0.3.0-dev.253` 通过真实 Host-managed MCP 调用：pinned Claude Agent SDK `0.3.207` / Claude Code `2.1.207` 保持 `settingSources: []` 与 `strictMcpConfig`，官方 MCP SDK `1.30.0` 服务端的不可预知 marker 经 `initialize`、`tools/list`、fresh approval、`tools/call`、completed event 后被 Agent 原样返回。实现只修复 MCP `content[]` 被通用 provenance 对象破坏的结果形状；固定 system prompt、Host approval、fencing 和审计继续把 MCP 内容视为不可信。Runtime digest 为 `sha256:1f9a0b81606141d31e436cf32239b5e364ceebae7a2a93a8da85db0ef8f22516`。该结果与此前 Claude Skill 子路径仍不覆盖 Control Plane/Worker、事件持久化、撤销、重启、恢复和其他两种环境，因此仍不满足单格或十二格完成条件。

Compose 的 Control Plane/Worker capability materialization、Worker 启动参数和 Skill `tmpfs` 已在 candidate `0.3.0-dev.257` 补齐；验收副本保持 UID 1000、`0400`。真实 capability smoke 的浏览器/Admin/重启前置链通过，但首个 Claude execution 返回 `runtime_start_failed`，没有 `skill.load`、`mcp.call`、Provider 终态或副作用。由于该 fixture 未启动其引用的 MCP upstream，这一轮只算不可用时 fail-closed 的负向证据；在以真实 upstream 重跑并取得 opaque succeeded/failed/revoked event、重启/撤销/恢复证据前，不得关闭 Claude×Docker，更不得填充十二格。

Candidate `0.3.0-dev.281` 已用真实 upstream 补充正向子链：同一个 Claude execution 经 Control Plane/Worker 完成一次需 fresh approval 的 MCP 外部副作用、一次签名 Skill 加载、约定文件 Artifact 和持久事件断线续读；fixture 只保留脱敏请求计数，实际结果为 `mcp_requests=8`、`side_effects=1`、`skill=1`、`resumed_events=6`、`post_terminal_events=1`。紧接 follow-up 因两个 Runtime 进程短时重叠并共享 `<skill-root>/<resourceId>` 而在启动阶段 fail closed，未满足连续 Session/重启恢复条件。实现现改为每个 Runtime 进程独立只读 Skill 根，并以重叠进程单元测试固定；candidate `0.3.0-dev.282` 的两次真实重跑均在 Worker image 外部 `npm install` 阶段失败，未进入验收链，所以不得把该修复或 Claude×Docker 整格标为通过。后续仍需同制品重跑 follow-up、撤销/不兼容/跨租户/旧 generation/重启恢复，并完成其余十一格。

Deepseek-harness adapter 已按 pinned `@deepseek-ai/dsh@0.1.2-rc.1` 的真实插件接口接入：`dsh-mcp-client` 的 streamable HTTP、环境变量 header、fail-closed startup 与 bounded reconnect，以及 `dsh-skill-filesystem` 的 default roots 禁用和只读自定义 roots，均由 Host-managed Cordis patch 生成；因 dsh custom roots 不递归，patch 挂载受管 bundle 的 `skills/` 子目录，且不含 Token 明文。定向 Provider tests、全仓 typecheck 和 pinned dsh 配置解析通过。另以本机当前 distribution artifact `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`（12,774,391 bytes）在旧依赖承载镜像中完成独立 Docker 预检：固定 `deepseek-v4-pro` 的真实 StartSession/SendTurn 成功完成 MCP `initialize`、`notifications/initialized`、`tools/list`、`tools/call`，签名/digest 校验 Skill 加载、原生写文件工具和 `ArtifactCandidate`；fixture 仅保留 8 条 HTTP/RPC method 计数，EOF 后受管 Skill 根为空，容器/临时凭据目录/live pointer 精确清理为 `0`，输出与 Artifact 无 Provider token/凭据命中。adapter 已按 pinned `dsh-tool-skill` 的真实 `skill({name})` schema 将 MCP 和单一 Skill Bundle 的 started/completed 通知绑定到 opaque ID，Control Plane 可据此写入 MCP/Skill succeeded/failed outcome；多 Bundle 无公开名称映射时保持不归属。该事件补丁通过 TS/Go 定向测试，但尚无真实 Control Plane/Worker 证据。预检仍不是十二格验收，也未覆盖事件续读、断连、撤销、版本不兼容、跨租户、旧 generation、重启和未知副作用恢复；原 `gpt-5.6-luna` 路由在无 capability baseline 即以 `input[3].name` malformed tool-call fail closed 且无 Artifact。`.282` 两次外部 npm registry `ECONNRESET` 保留为历史失败；registry 已恢复且 `.283` 无 Provider build/smoke 通过，但缺少新的受保护 Provider fixture，所以该 adapter/预检仍不关闭任何格。

同一 `0.3.0-dev.251` 候选的任务自有 kind v1.37.0 Helm smoke 也真实通过 PostgreSQL/000099、Capability Admin/User Web、outbound RemoteWorker、TLS/SSH、CA/服务身份轮换与旧根拒绝、Control Plane/Worker 重启、无 Agent/Provider Secret；脚本报告 `cleanup=zero`，随后 kind 集群、context、测试容器和临时目录均精确清除。该命令与 Runtime/release/deployment digest 已写入 06；因为未提供 Provider 凭据，它只能补充 Kubernetes 部署/管理链证据，不能替代十二格的 Provider Turn、MCP/Skill 事件、撤销、重连、跨租户、旧 generation 与恢复验收。

Runtime broker、Skill materializer 与 Provider 共享入口另已覆盖 opaque ID 归一化后的 MCP Token/Skill mount 环境变量名碰撞，并在实际注入或 Provider 启动前 fail closed；该单元证据不替代真实 Provider 调用。
Control Plane 与 Worker 对 operator-owned capability descriptor 都执行完整 JSON/EOF 校验；任何尾随第二个值均 fail closed。双路径 Go 回归、Worker 全套测试与 capability/Provider 定向 Vitest 已通过，但仍不替代十二格真实验收。

未发布 candidate `0.3.0-dev.283`（sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`，manifest `sha256:c3af132553b379f8cbdd5a5772f4453ce447f45db73a85c3ddce01e04e0d3e8a`）的 `sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r283-candidate` 在补齐固定 Node 基础镜像后真实 exit 0；`expired_grant_negative=passed`、User/Admin 浏览器与管理链、重启/备份恢复通过，最终 Compose 容器、网络、卷及 OpenSandbox runtime 均为 `0`。这是无 Provider 凭据的部署/管理证据，不包含 MCP call、Skill load、Provider outcome、撤销/重启恢复或十二格任何一格；首次缺固定镜像导致的 `foundation_runtime_unavailable` 已按 fail-closed 测试依赖边界保留。

同一 `.283` 候选用现有受保护凭据的临时副本和签名 Skill/MCP fixture 完成真实 Claude×Docker 正向子链：`CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r283-candidate /tmp/mcp-skill-runtime-v1-20260915-r284-fixture` exit 0，输出 `capability_acceptance=passed`、`mcp_requests=8`、`side_effects=1`、`skill=1`、`event_stream_resume=passed resumed_events=6 post_terminal_events=1`，follow-up 断言亦通过。证据日志为 `.tmp/mcp-skill-runtime-v1-20260915-r284-claude-docker.log`，SHA-256 `sha256:dc542461e9fe99daaa2812cd5c46eca98b7ae3cdc728067884b2ab554022b2b9`；凭据/token 扫描、fixture/镜像/Compose/OpenSandbox 清理均通过，原凭据源保持不变。该结果只推进 Claude×Docker 子链，不关闭整格；能力撤销、版本不兼容、跨租户、旧 generation、Control Plane/Worker/Agent 重启恢复、未知副作用对账和其余十一格仍是必验。

Candidate `.284` 在同一真实 Compose 协议上又完成能力撤销负向：使用本机固定可用 Node digest `sha256:6dac556d980b7f0e5498d08f08cee0ca67798b4ad6c23964a9214920e67758d0` 的命令 `CLOUD_AGENTS_COMPOSE_FOUNDATION_RELEASE_DIGEST=... CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r284-revoke-candidate /tmp/mcp-skill-runtime-v1-20260915-r285-fixture` exit 0；正向链为 `mcp_requests=8`/`side_effects=1`/`skill=1`/`resumed_events=6`/`post_terminal_events=1`，撤销 MCP 与 Skill 后各写入一条 opaque `capability_revoked` Audit，后续执行返回 `CAPABILITY_UNAVAILABLE`，Runtime 未新增请求/副作用。日志 `.tmp/mcp-skill-runtime-v1-20260915-r288-claude-docker-revoke.log` digest 为 `sha256:b31627069e0023fea2d3d44ff29af545d4a4a8fa3e71ed86999c80f322b079d0`，pattern scan、资源清理和脚本 exit 0 均通过。该结果只关闭撤销负向子路径，版本不兼容、跨租户、旧 generation、重启恢复和整格/其余十一格仍开放。

Candidate `.285` 另以 `CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1` 在无 Provider 凭据时真实验证 execution-time 版本重新授权与租户隔离：MCP/Skill 错误版本均返回 `CAPABILITY_UNAVAILABLE`，各追加 opaque `mcp.fail`/`skill.fail`、`capability_version_mismatch` 事件且不打开 Runtime；跨租户 MCP 元数据读取返回 `401`。完整 Compose exit 0、备份恢复及资源清理通过，日志 `.tmp/mcp-skill-runtime-v1-20260915-r290-contract-negative.log` digest 为 `sha256:68f7e7a06b43f7ada97a96e03ed07000fdbb80696c55b8182af3e349902c83f2`。这是共享 Control Plane 负向证据，不能替代十二格逐格版本/跨租户验收。

Candidate `0.3.0-dev.286`（manifest `sha256:e1615ccc14e1e634e5680b4a135bf6f98b3d9020f20bfc09382dc864625ac17`，checksums `sha256:02a88a200016f15c24fa655481c9dd93497bfaded8c8271459c067e1542f815b`，Control Plane arm64 `sha256:175b303ab4985edbcd199271bd8bef040be0c0d4bebf5fc6373d0feb090b796a`）在固定 Node digest 下真实复跑共享负向并推进旧 generation 检查：`capability_contract_negative=passed`，Sandbox Stop 将 generation 从 `1` 推进到 `2` 后，带 MCP/Skill refs 的旧 generation Session 创建返回 `409 SESSION_CONFLICT`，未打开 Runtime；命令 exit 0，日志 `.tmp/mcp-skill-runtime-v1-20260915-r292-stale-generation.log` digest 为 `sha256:6655117051c6742505d7e610ed05702d8cbb880b6c3f2277be271c1ef5a10d82`，完整 Compose/备份恢复、secret pattern scan 和精确清理通过。该证据只覆盖 Docker 共享 fencing，不关闭逐 Provider/环境旧 generation、重启恢复或任何完整 Gate。

未发布 dirty-source candidate `0.3.0-dev.287`（sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`，manifest `sha256:8847e33e6c98a1e596de38284d21e2784e566a01847227a2179e337b43d459ca`，checksums `sha256:0826b64fb9ac9c85c9e69a43740fb3204b92310bd3ca828a104f984554e5e46a`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:fdac0d683ce9f2ae890f1be00ba0dd01683f1a0567219e6ebc51c32953896bcb`；22 项 checksum 全通过）在 `CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY=1` 下将同一 contract-negative/stale-generation 入口真实扩展到 outbound RemoteWorker 与 Kubernetes。RemoteWorker 日志 `.tmp/mcp-skill-runtime-v1-20260916-r297-remote-contract-negative.log` SHA-256 `sha256:8a1eca15f6f4268789a3f870b07e578a0c6a5141a62f0e6993298bd98dc30cf4`，Kubernetes 日志 `.tmp/mcp-skill-runtime-v1-20260916-r300-kubernetes-contract-negative.log` SHA-256 `sha256:89ab5ac743557b04331c249d2f4a89956fccf6e439d632796b09897cc4c055b7`；两者均真实输出 `capability_contract_negative=passed`（MCP/Skill incompatible、跨租户 `401`）和 `capability_stale_generation=passed`（旧 generation `1`→当前 `2`、`409`），并完成 secret scan、Compose 资源零清理。Kubernetes 为任务自有 kind v1.37.0 三节点，临时安装的 OpenSandbox controller/CRD/权限与 kind/context/kubeconfig/Helm 目录已精确删除。该证据只证明共享合同负向跨三环境可复用，不关闭四 Provider×三环境任何格，也不满足本 Gate 所要求的 Provider 正向、撤销、重启/恢复、断连续读或未知副作用对账。

同一 `.287` 制品和当前 working-tree 脚本又以真实 Claude 凭据及签名 Skill/MCP 临时物料执行 RemoteWorker capability 链，r304 exit 0：Docker、RemoteWorker 各完成 `capability_acceptance=passed`（`mcp_requests=8`、`side_effects=1`、`skill=1`）与 `event_stream_resume=passed`（`resumed_events=6`、`post_terminal_events=1`），且只在最后启用的 RemoteWorker 环境撤销共享 MCP/Skill，后续返回 `capability_revocation_negative=passed runtime_requests=0 side_effects=0 events=2`。日志 `.tmp/mcp-skill-runtime-v1-20260916-r304-claude-remote-worker-revoke.log` SHA-256 `sha256:a690eb82d4754bf9b5bf8d0c822e757889e27765cd1c88ba60630289453165cd`；secret scan、临时物料与 Compose 资源零清理通过，原凭据源未修改。同轮通用 Claude RemoteWorker process-restart/cross-node-takeover 虽为 attempt 2、confirmed、RPO 0（跨节点 RTO 8642 ms），其 Turn 未携带 MCP/Skill refs，不能作为 capability-bound 恢复或未知副作用对账证据；Claude×RemoteWorker 整格及正式 Gate 保持开放。

当前源码的 `.288` candidate（manifest `sha256:12a98cef3254572778fd2309b56a3e8a4817a2bfca4adbec07c6a4350cb6b6f8`，checksums `sha256:7ba8f8b81df213e6cfb9c2ff8dd529587f8e98c4460c2d39ce8022af74f5167f`）虽 22 项 checksum 通过，r305 却在上述标记全部通过后于恢复最终 Sandbox Exec 遭遇 heartbeat 并发导致的 PostgreSQL `40P01`，以 `INTERNAL_ERROR`/exit 1 结束；失败日志 `.tmp/mcp-skill-runtime-v1-20260916-r305-claude-remote-worker-revoke.log` SHA-256 `sha256:1607a87446380ac301d5aad6b506879de76285c3e327e8102d8344850ba60f28`，资源仍清零。修复将已回滚的 deadlock 映射为现有 `RESOURCE_CONFLICT`，保留一次性授权并由调用端有界重试，不盲目重放。`.289`（manifest `sha256:3bf9331ebfdcbf179938d2a5815fd4534950189386d5e5f4376570ea8f3497a0`，checksums `sha256:55c3328c3e80e63ebd6bc6bd77ea7020e57cf484b34ebd32cea11e62fc3d4529`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:5112b0b5e3043d8bdb9220d0cba4e87c2ff5351e47673d0a9331f21cdf1da44a`）22 项 checksum 与定向 Go 测试通过；r306 完整 exit 0，日志 `.tmp/mcp-skill-runtime-v1-20260916-r306-claude-remote-worker-revoke.log` SHA-256 `sha256:b23b01ddcc15add7a9d93bf7783ed12ace3065374f7ee162cb92feabc2983915`，跨节点通用恢复 RTO 10770 ms、RPO 0，secret scan 和资源清理为零。能力绑定恢复仍未执行，故本 Gate 与该格仍开放。

Candidate `0.3.0-dev.290`（manifest `sha256:0854e5fdcc41cedb074ac27afa022e0a57dbe47a8bb4da1011e48b469082ca6b`，checksums `sha256:51ffe57ef7a6e65ed1aadd8e0dca95cdca4433978df803a696ec437490220f33`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:609fdba0ed6b4d534ded46aebe1ff9b8c7acee7f8a995b58065b5669423b0a9a`，22 项 checksum 全通过）把 Kubernetes capability fixture 接到 `opensandbox.io/id` 对应的真实 Runtime Pod：fixture 用固定 MCP SDK 单文件 bundle 传入 Pod，descriptor 经 stdin 传输，服务进程与 Worker 共享网络命名空间，结束时删除 Pod 内临时物料。无凭据命令 `CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME=1 CLOUD_AGENTS_COMPOSE_KUBERNETES_CONTEXT=orbstack CLOUD_AGENTS_COMPOSE_KUBECONFIG=/Users/huang/.kube/config CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY=1 CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260916-r290-k8s-candidate` exit 0，`capability_contract_negative=passed`、`capability_stale_generation=passed`；日志 `.tmp/mcp-skill-runtime-v1-20260916-r290-kubernetes-contract-negative.log` SHA-256 `sha256:15a7dbbc57ddcca19a7ebcfbcd8ab559150ea9e0c56b9d2dac41eb832c99e1a5`，namespace/Pod/Compose/临时 smoke 资源均清理为零。当前无受保护 Provider 凭据，未运行 Claude Kubernetes 正向调用；因此不关闭任何 Gate、Provider×环境格或能力绑定恢复要求。

恢复验收脚本已补上 capability-bound 同节点进程重启路径：Claude recovery Session/Execution 在初始执行、reconcile 后 attempt 2 均显式携带同一 MCP/Skill refs，Skill 先驱动只读 MCP marker，再制造可对账的文件副作用 checkpoint；审批仍走 fresh approval，未知结果继续禁止自动重放。该实现目前只有静态检查和现有无凭据负向覆盖，未取得新的真实 Provider recovery log，不得计入 APP-M1 或关闭任何十二格。

候选 `0.3.0-dev.291` 的 22 项制品校验通过：manifest `sha256:99018730ea4bce5463bebb287db59348e67dccd1977bc52ab30caa7b0f51eeba`、checksums `sha256:4b473f310812899162947f3b61c0ab70ae5499cc50fd761e3d7fdc38c8a3a224`；该命令没有真实 Provider 凭据，因此只能作为实现一致性证据，不改变 Gate 状态。

同候选 Compose 无凭据回归 exit 0，日志 `.tmp/mcp-skill-runtime-v1-20260916-r307-capability-recovery-contract-negative.log`（`sha256:e4457a49bf4f6c0ca5ae921981f00e036e0f96391af76e3c8107ece8071eef39`）记录了共享 contract-negative、stale-generation、浏览器、重启、备份恢复和零残留清理；它没有 Provider MCP/Skill 正向或恢复结果，Gate 仍开放。

`.291` 的 Kubernetes contract-only 重跑也 exit 0，日志 `.tmp/mcp-skill-runtime-v1-20260916-r308-capability-recovery-kubernetes-contract-negative.log`（`sha256:e6a48b9ae09b8ac0e8e5f8564681aab7cd2d849cec5c57623a0ce12729b4e555`）确认 Docker/Kubernetes 共享负向与旧 generation fencing；kind/OpenSandbox、Pod/namespace 和 Compose 资源均清零。没有 Provider 正向调用，不能计入十二格或关闭 Gate。

Codex pinned `@openai/codex@0.150.1` 的真实 app-server schema/RPC probe 已确认 `skills/extraRoots/set`、`skills/list` 和 `turn/start` 的 `SkillUserInput` 接口；受控 `HOME`/`CODEX_HOME` 下只发现 Host-mounted Skill，adapter 对空/畸形 discovery 和 GenerateText capability fail closed。该切片通过 Codex 58 tests、typecheck、oxfmt、diff check；Bundle opaque reference 没有单个 Skill selector，故不自动把所有 Skill 指令塞进每个 turn，也不把 discovery 当成模型使用。候选 `.292` 的 manifest `sha256:ef0d9bab0288d02dde870e3865e3f3137bc8ba89e974c60d950417dfe5cad076`、checksums `sha256:819416972502f1c8218ccdec0be84b4d8a0808082df2979c3bf21e952487706b`、Runtime `sha256:5de654a71b9e52b0762e9f4a5d9c235548fbfff2f497a3720c45a8edb58b10ab`、Control Plane arm64 `sha256:c0578bf9b1343076fa41d31ce11d0bb288930e734405b89ebd19e931312749f8` 已按 `sha256sum -c checksums.sha256` 通过；无 Provider 凭据，十二格和正式 Gate 保持开放。

最终 candidate `0.3.0-dev.293` 只增加 system Skill 必须位于受控 `CODEX_HOME` 的白名单校验；其 manifest `sha256:6d2e62174420da4083575cd6215009942d5c28ea79d054e6c9d320788d786385`、checksums `sha256:f7427791180c818b9c66a87ce10c0c60453dbc831324b0ceb7a32e46de7dc3f3`、Runtime `sha256:2d942bb1790116d3dbe49b41d29960ceafde009a544e47053dea197e90c3182e`、Control Plane arm64 `sha256:a7c01dee5bf5da07297871184b92898cecf993741aa68ba685139f6c3a6c6f37` 全部 checksum 通过；未运行 Provider 模型正向或能力恢复，Gate 继续开放。

Codex 的事件链现在能把实际 MCP call 的 `item.started/completed` 映射为 `mcp_tool_call` 与 opaque Server ID，并把单一 Bundle 的 Skill item 映射为 `skill.load` 所需的 `sourceItemType=skill`；多 Bundle 仍 fail closed/不猜。候选 `0.3.0-dev.294` 的 manifest `sha256:6565fd94667d960e7c1399e0116340f81ba82a2153ac37abbde66ab07b8e2966`、checksums `sha256:105d299b1d57c3e13ea8dd753fa0279f594e89db475fec643ec690b0053e9ba1`、Runtime `sha256:0aff32da78608c662bb3f2846de0af67d8dd85fb918f2452a309ef9b79959d33`、Control Plane arm64 `sha256:d733bf9e1d3ba21a4fe1c74f064199f61375240a65bcfe479131a903e1fa857a` 已通过 22 项 checksum；无 Provider 正向凭据，十二格和 Gate 仍开放。

Codex MCP transport/JSON-RPC 失败回归现在保持 started-but-not-completed，Provider Turn 立即终止并交给既有未知副作用 reconcile，避免模型在同一 Turn 盲目重试；`.295` candidate 的 manifest `sha256:fb7fde91f966d01279e6325f360a8709962f0340370ac5dd40656fc3d16a730`、checksums `sha256:f1b5d517f7b26f2a903897d604888195ae86d2cc167d23a539aeabfba86e52e6`、Runtime `sha256:3eb6d09ddc0786d52834ae55cb5a2a7b486429e55802af0b87a84cfa041e3078`、Control Plane arm64 `sha256:3bb7c54765708b40197f27944f3d7e82119963c7f5a136a63f09cd6c52f06114` 22 项 checksum 通过；无 Provider 正向凭据，Gate 继续开放。

Candidate `0.3.0-dev.301` 修复 Pi native cursor 失效后 authoritative-history fallback 丢失 Skill Bundle roots：恢复 attempt 2 继续接收同一受管 Skill roots；Pi 3 tests、Runtime broker/materializer/stdio 与 Provider API 91 tests、定向 Provider/SDK 54 tests、Pi typecheck、TS/Go 生成检查、Go test/vet、格式、diff check、HTML parser 与 22 项 checksum 通过。没有受保护 Provider 运行证据，不新增 supported cell，也不关闭十二格或 Gate。

同一 `.301` 候选的无凭据 Compose 合同负向命令 exit 0，真实通过 `capability_contract_negative`、`capability_stale_generation`、Admin/User browser smoke、Control Plane restart/backup restore 和 Compose smoke；Docker 容器/网络/卷为零。该证据仍不替代 Provider 正向、撤销、事件续读或 capability-bound recovery。

Candidate `0.3.0-dev.302` 修正 MCP/Skill 相同 opaque ID 的 capability audit 复合 identity；Managed Agent/Server/Store/Worker Go tests、`go vet`、Runtime/Provider/SDK 145 Vitest、TS/Go 生成检查、格式、diff check 与 22 项 checksum 通过。无受保护 Provider 运行证据，不新增 supported cell，也不关闭十二格或 Gate；`.302` Compose 真实负向回归 exit 0 且容器/网络/卷清零。

Candidate `0.3.0-dev.303` 修正 pending capability 审计的跨资源类型碰撞：MCP/Skill 复用相同 opaque ID 与 provider item ID 时，按 `(resource kind, capabilityResourceId, providerItemId)` 独立追踪，Skill 完成不会清掉 MCP 未完成状态。Managed Agent/Server/Store/Worker Go tests、`go vet`、Runtime/Provider/SDK 145 Vitest、TS/Go 生成检查、格式、diff check 与 22 项 checksum 通过；manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:c560f688412207ed57224af4461e5cc5a2d3fa155542de39e9d7fc26bfd199cd`、`sha256:d944cccd78f279b37ab5c2f880e6518272c30e8364161fdfeb49709755d2470e`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:9a13eae5ff4df27aedc963b76fc88fa64e763536abb67796d2d1b8635e767468`。无受保护 Provider 运行证据，不新增 supported cell，也不关闭十二格或 Gate；`.303` Compose 真实负向回归 exit 0 且容器/网络/卷清零。

Candidate `0.3.0-dev.305` 新增最小恢复回归：带 checkpoint 的 `ExecutionRunning` 在幂等重试携带同一显式 capability refs 时，仍从持久 Execution pin 重新解析 MCP/Skill binding 并在接管 attempt 的 Runtime open 中重新注入相同 manifest；生产 digest 校验拒绝 refs 不一致的重试。该测试只证明 Control Plane→Worker 协议恢复语义，不是 Provider、Docker/RemoteWorker/Kubernetes 真实重启验收，因此能力绑定恢复、十二格和 Gate 继续开放。Managed Agent/Server/Store/Worker Go tests、`go vet` 与 22 项 checksum 通过；manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:a5bfab9be34edbdc3245b86b930bca0a7e767a1ad6b11e388018776c8bd41739`、`sha256:b823fa14ca45c26760285e44d4d4121fdf27529645dcc6c54624b013d9a5799b`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:d2a7fdcb511b9b6267f3a933a84d13da8a6176c678f8b7b0fb5b6baed2553666`。

Candidate `0.3.0-dev.306` 的 Store 回归证明 capability refs 参与 Execution create mutation digest；同一 idempotency key 下 refs 不一致会被既有数据库冲突语义拒绝。该证据只收紧 pin/fencing 边界，不是 Provider、Docker/RemoteWorker/Kubernetes 真实重启验收，因此十二格、能力绑定恢复和 Gate 继续开放。manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:3f3d61f038924bf78e8ae1fb6c58b9da1f15a184f5292286a463428c103eb3f1`、`sha256:1a990ab5b22f61f0d7a37be205066aa1daf7e0bda5807c1141aa6db5f573a1fa`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:162c9d75337c2afd8c20f34f32501c7de6ca453c58451ad769ac124f91bf1a46`。

Candidate `0.3.0-dev.307` 只证明 Pi Skill 的 Provider→Runtime→Control Plane 事件接缝：受管 Skill session 成功后，`@earendil-works/pi-coding-agent@0.85.1` activity 归一为 `dynamic_tool_call`/`sourceItemType=skill`，并由既有 `skill.load` outcome 归账；Pi MCP 仍 fail closed。Provider API/Pi 100 Vitest、Pi/Provider API typecheck、格式、diff check 与 22 项 checksum 通过，但没有受保护 Provider 凭据或 Docker/RemoteWorker/Kubernetes 正向/恢复 log，因此不关闭任何十二格、aggregate Gate 或 release Gate；candidate `.tmp/mcp-skill-runtime-v1-20260915-r324-pi-skill-event-candidate` 的 manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:c0316def7d041c699ebd98652b9ffbb540910ade264e7d8e34bb068bead599e7`、`sha256:ea523d0d6bd8233277bf4f67c6f3a669efa4edcfb9044daaaa5191c13ff1d969`、`sha256:080cb8ecfc42d7b8e7319e7655e4050f391e0049fb971306a1765e4a67a28789`、`sha256:32fcc7f1c48900aa735ea6ae91a72cda199a7698a56602c371cac0437e4dcfac`。

Candidate `0.3.0-dev.309` 首次给出 Claude×Docker capability-bound process-restart 的真实证据。Control Plane transition 响应已从持久化投影补回 MCP/Skill refs；定向 Store/Managed Agent Go tests 和 22 项 candidate checksum 通过。受保护 fixture 命令 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST=1 CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate <protected-fixture-dir>` exit 0：MCP 调用、一次外部副作用、签名 Skill 加载、事件断线续读、Control Plane SIGKILL 后 attempt 2 恢复/confirmed 对账/RPO 0，以及撤销后零 Runtime 请求和零副作用均通过。日志 SHA-256 为 `sha256:ba9b32fc4d4e34530ca099441056c52fe0cb6910381add73573f9574510938a8`；secret scan、原凭据 digest、22 项 checksum、Compose/fixture/诊断资源零残留均通过。candidate manifest/checksums/Runtime/Control Plane arm64/Worker arm64 digest 分别为 `sha256:6d407d0b0403d3c6e1ea87ecaff6fbf549af1d5308d3accab630c2d555e209d5`、`sha256:24d071037cef1969f42195d5b27828dbed4e77b544e37b55c10be887d6e60164`、`sha256:080cb8ecfc42d7b8e7319e7655e4050f391e0049fb971306a1765e4a67a28789`、`sha256:d71dbb983a7f967a8e2cf86c8afe8996275aafc4876c11fe23b1ec9e6bce023d`、`sha256:4a6bddbeb39882c138026d548bc377d28058f03ece0835ac3a4017362e8616b4`。这仍不是 Claude×Docker 整格 closure：Worker/Agent 重启和该格全部版本/租户/generation/fail-closed 组合尚未在同一 Provider 格闭合；其余十一格、aggregate Gate、`G-SUPPLY-CHAIN`、`G-PLATFORM-RELEASE` 与 exposure Gate 均保持开放。

同一 `.309` candidate 的 Claude×RemoteWorker 真实命令加入 `CLOUD_AGENTS_COMPOSE_REMOTE_RUNTIME=1`、`CLOUD_AGENTS_COMPOSE_CROSS_NODE_RECOVERY=1`、`CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER=claudeAgent` 与 `CLOUD_AGENTS_COMPOSE_CROSS_NODE_ENVIRONMENT=remote-worker` 后，脚本末尾 `platform Compose smoke passed`，真实通过 MCP/Skill 正向（8 请求、1 次副作用、1 次 Skill）、事件续读（6/1）、Control Plane SIGKILL 后 capability-bound attempt 2 `process-restart/recovered/confirmed`、RPO 0，以及撤销后零 Runtime 请求/零副作用/2 个脱敏事件。日志 SHA-256 为 `sha256:261aad9ee72bdf04bca61bc94e06ca31a62ec5ff5eae968ad27d4c56e4dc8dc9`，credential/token/marker/tool-name scan 为 0，原凭据 digest 未变，Compose/RemoteWorker/fixture 临时资源为 0。同轮 cross-node 样例明确为 `capabilityBound=false`，不能计入能力绑定跨节点 Gate；因此 Claude×RemoteWorker 仍未达到整格 closure，Worker/Agent 重启、能力绑定跨节点、逐格版本/租户/generation/fail-closed 组合以及其余十一个格和所有正式 Gate 继续开放。

同一 `.309` candidate 的 Claude×Kubernetes r329 真实命令显式使用 `CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME=1`、`orbstack` context 与常规文件 kubeconfig，脚本 exit 0；Kubernetes Pod 内 MCP fixture 与真实 Claude 完成 MCP/Skill 正向（8 请求、1 次副作用、1 次 Skill）、事件续读（6/1）、Control Plane SIGKILL 后 capability-bound attempt 2 `process-restart/recovered/confirmed`、RPO 0，以及撤销后零 Runtime 请求/零副作用/2 个脱敏事件。日志 SHA-256 为 `sha256:d6c65ef24d4289a27f4f878227e6732580564720c50c21a259aafb4fd902c539`，credential/token/marker/tool-name scan 为 0，原凭据 digest 未变，Compose、Kubernetes namespaced/cluster-scoped 测试资源与 fixture 均为 0。本轮未启用 Kubernetes cross-node，且 Worker/Agent 重启和逐格版本/租户/generation/fail-closed 组合仍缺失；因此 Claude×Kubernetes 不达到整格 closure，其余十一格与所有正式 Gate 继续开放。

Candidate `0.3.0-dev.353` 的 22 项 checksum、Codex 48 tests/typecheck 和脚本静态检查通过；真实 Docker 命令与 digest 记录在 06，日志 SHA-256 为 `sha256:dc43706b5576c43b75cbdfe9fb5e577d649701ae4325c976dffc936281105c72`，执行 exit 1 且资源清理为零。该结果只证明 Codex 原生 MCP discovery 能到达 broker，以及模型未调用时平台 fail closed；Codex×Docker、Codex×RemoteWorker、Codex×Kubernetes 均未关闭。Claude 三环境也只有子路径，Pi MCP unsupported，deepseek 尚无 Control Plane/Worker 整格，所以十二格全部保持开放，MCP-SKILL-RUNTIME-V1、aggregate Gate、release Gate 和 exposure Gate 均不得关闭。

Candidate `0.3.0-dev.354` 把随后发现的 Codex `turn/start`/`review/start` 即时 Host 调用竞态修复纳入可校验制品；48/48 Codex 定向 tests、Provider typecheck、IDE build、`git diff --check`、Compose 脚本语法、HTML parser 与 22 项 checksum 通过。该候选没有新增真实 Provider×环境运行，`.353` 的 Codex Docker exit-1 日志仍是当前模型调用证据；因此不得关闭任何十二格、MCP-SKILL-RUNTIME-V1 或正式 Gate。

对 `.354` 的 Codex Docker 复跑在 Worker 镜像 `apt-get update` 阶段因 `deb.debian.org` 容器网络连接失败而 exit 1（日志 SHA-256 `sha256:53227ff4f88835d2f370c7402a918b89f60202dc358f331ffc8c110f63fdcb65`），最小 Debian 复现同样失败；没有 Provider 执行，不得把它算作 MCP/Skill 成功或失败。Compose 资源已清零，脚本语法仍通过。验收提示已改为一次 Host-owned `workspace.write_text_file`，与 Skill fixture 的禁止 shell 约束一致；十二格、fail-closed 规则和正式 Gate 不变。

`.354` 随后在局域网代理预热固定 Worker 依赖后完成真实启动，但 Codex execution 仍只有 reasoning/content/token usage/Result，MCP fixture 只有 `initialize`、`notifications/initialized`、`tools/list`，无 `tools/call`、Skill、Artifact 或副作用；`.356` 日志 SHA-256 为 `sha256:37f1301415812a6a3716ae559c6ad8bf38df20e5d2a8e272573f429fea4fb5e2`，脚本 exit 1 且资源清零。该证据确认 fail-closed discovery-only，不得关闭 Codex×Docker、十二格或正式 Gate。

## 1. Gate 总表

以下保留旧完整 Platform/消费者的正式 Gate 定义与批准要求，仅在执行对应 Gate 或该范围变更时适用；不是所有 BASE 日常工作必须先关闭的清单。BASE 联合交付的完成条件由上节定义，不新增一轮泛化审批，也不降低这里任何正式 closure 条件。

| Gate                 | 阻塞                | 退出证据                                                                                                                             |
| -------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `G-INVENTORY`        | P0                  | frozen ref 的全量 code/SQL/schema/build/deploy/generated manifest、分类、source/tree hash、authority、license/secret provenance 完整 |
| `G-BASELINE`         | P0/M1 phase records | P0 characterization 与 M1 真实 Provider baseline 分别关闭；aggregate 只在两个 record 同时有效时关闭                                  |
| `G-CONTRACT`         | P1                  | JSON Schema/OpenAPI 与 Proto/Connect/gRPC authority、TS/Go SDK、server validator、mapping/golden/negative fixtures 同源              |
| `G-DATA`             | P1                  | PG15–17 fixed-patch/digest matrix、forward migration、tenant RLS、idempotency/outbox/leader、本地 logical backup/restore、N/N-1      |
| `G-AUTHORITY`        | P1–P6 phase records | 三种模式 owner 唯一；无 Session/Turn/Lease/Workspace 双写                                                                            |
| `G-MANAGED-AGENT`    | P2                  | Session/Turn/Execution/Worker/Workspace/Artifact/Credential real E2E                                                                 |
| `G-WORKER-FENCING`   | P2/P3 phase records | stale generation 无法 heartbeat/ready/取密/发 endpoint/提交终态；revoke/reap 证据完整                                                |
| `G-MANAGED-HOST`     | P3                  | reference host 的 Lease/Generation/workload/volume/endpoint/grant/cleanup 与 signed descriptor conformance                           |
| `G-ADAPTER`          | P2–P4 phase records | built-in 与外部 Platform Adapter protocol conformance、mTLS、幂等、deadline、receipt                                                 |
| `G-SECURITY`         | P1–P6 phase records | tenant isolation、五类身份、SSRF/DNS、secret/log/cache、path/symlink、rate limit、downgrade、host cutover                            |
| `G-OPS`              | P4                  | DB/leader/outbox/retry/orphan/partial create、HA、backup/restore、upgrade/rollback、SLO/runbook                                      |
| `G-STANDALONE`       | P4                  | fresh Compose 与 Helm 在无 Synara 私有依赖下完成真实 Codex/Claude Turn 与 cleanup                                                    |
| `G-SYNARA-CUTOVER`   | P5                  | shadow/canary/single-writer/failback、legacy drain、重复公共源码删除                                                                 |
| `G-T3-INTEGRATION`   | P6                  | embedded 不回归；managed direct/relay proof-bound，Bearer 拒绝，真实 T3 E2E/soak                                                     |
| `G-SUPPLY-CHAIN`     | Release             | module/tag/image/chart/SDK/host descriptor digest、SBOM/provenance/signature/license/secret/CVE/VEX/base-image gate                  |
| `G-PLATFORM-RELEASE` | RC                  | 同一 platform manifest 的 Synara/T3/standalone E2E、install/upgrade/rollback 全闭合                                                  |
| `G-EXPOSURE`         | Beta/GA             | 用户范围、支持等级、channel、回滚、事故响应与人工批准                                                                                |

## 1.1 Progressive Gate 语义

跨阶段 Gate 不允许在早期阶段被一次性关闭。阶段只产生 immutable phase record，聚合 Gate 只在全部必需
record 同时有效时标记 `VERIFIED`：

| Aggregate Gate     | Required phase records                         |
| ------------------ | ---------------------------------------------- |
| `G-BASELINE`       | `G-BASELINE-P0`、`G-BASELINE-M1`               |
| `G-AUTHORITY`      | `G-AUTHORITY-P1` … `G-AUTHORITY-P6`            |
| `G-SECURITY`       | `G-SECURITY-P1` … `G-SECURITY-P6`              |
| `G-ADAPTER`        | `G-ADAPTER-P2`、`G-ADAPTER-P3`、`G-ADAPTER-P4` |
| `G-WORKER-FENCING` | `G-WORKER-FENCING-P2`、`G-WORKER-FENCING-P3`   |

phase record 状态只允许 `NOT STARTED`、`IN PROGRESS`、`VERIFIED`、`INVALIDATED`。每个 record 固定前置
record、authority scope、source/dirty/toolchain、contract/module/SDK/image/migration/manifest digest、原样命令、
逐项结果、DRI/独立 reviewer 与 downstream invalidation rule。阶段 Exit 只依赖该阶段 record，不声称未来阶段
已验证；Platform RC 才验证上述 aggregate Gate。

`G-BASELINE-P0` 只验证固定 ref 的 Synara legacy managed-agent、T3 embedded、可复用机制实际
characterization，以及 greenfield Managed Host 的 immutable spec/negative/reference-host oracle；它不得要求或
声称真实 Codex/Claude M1 行为。`G-BASELINE-M1` 才验证 Protocol 2.2/2.3、真实 Codex/Claude
happy/auth/rate-limit/unavailable/resume、SendTurn/workspace/checkpoint/reconnect 的同输入基线。P0 Exit 只依赖
`G-BASELINE-P0`；M1/Platform RC 才要求 aggregate `G-BASELINE`。

P1 的 contract、data、authority 与 security 边界以
[ADR-0007](../adr/0007-p1-contract-data-toolchain-foundation.md) 为决策 authority；ADR digest 或其中冻结的 wire、
schema、database、tenant isolation、identity 边界变化时，四个 P1 record 一并失效并重新复核。

失效规则至少为：contract/core/store 改动使 P1–P6 record 失效；Worker/adapter 改动使相关 P2–P6 record
失效；Standalone/security/ops 改动使 P4–P6 record 失效；Synara adapter/cutover 改动使 P5–P6 record
失效；T3 descriptor/proof/connection 改动使 P6 record 失效；任何重打包或 digest 变化使 same-bits record、
`G-SUPPLY-CHAIN` 与 `G-PLATFORM-RELEASE` 失效。旧 record 保留，不覆盖历史。

安全状态变化即使 bits 不变也会失效：新 applicable KEV/reachable Critical/High、waiver 到期、scanner DB
超过 24 小时、signer/trust root/release identity 撤销、base image EOL/revoke，都会把
`G-SUPPLY-CHAIN` 与引用它的 `G-PLATFORM-RELEASE` 标为 `INVALIDATED`，暂停未批准 exposure，并要求按新
数据库/信任根重扫、重新签署与重跑受影响 same-bits Gate。不得因 artifact digest 未变沿用旧安全结论。

## 1.2 P1 精确退出标准

### `G-CONTRACT`

- JSON Schema 是 management/agent/host JSON 数据模型的唯一 authority；OpenAPI 只定义 route、status、header
  与 schema ref，OpenAPI bundle 不包含漂移的内联副本；
- Worker/Platform Adapter 的 wire model 以 versioned Proto 为唯一 authority，并生成 Connect/gRPC server/client
  mapping；descriptor set、OpenAPI bundle、JSON Schema bundle 与 SDK digest 均进入 closure record；
- TS SDK、Go SDK、Go server validator 和映射层通过同一套 golden、negative、N/N-1 compatibility fixtures；至少覆盖
  unknown field、enum/version downgrade、stable error、watch cursor、idempotency key、deadline/cancellation 和
  显式共享语义类型的 Proto↔domain↔JSON mapping round-trip；不要求或允许为所有 Proto RPC 另建 JSON wire；
- 从全新外部 consumer 安装 exact-pinned SDK 后可编译并调用 fixture server；仓库外不得依赖 workspace/file/git
  dependency，生成后 diff 必须为零；
- 任一 schema、Proto、OpenAPI、generated SDK 或 mapping fixture digest 改变都会使该 record 失效。

### `G-DATA`

- Postgres `15`、`16`、`17` compatibility matrix 全部通过；每个 major 固定 patch version 与 OCI image digest，
  closure record 保存实际 `server_version` 和 digest；
- persistence 仅使用 `pgx/v5`、`pgxpool` 和手写 SQL；自动检查证明没有 GORM、`AutoMigrate`、ORM schema
  generation、Synara migration 编号或 legacy schema authority；
- 新 migration ledger 完成 `expand -> resumable backfill -> code cutover -> contract`，逐项验证 immutable
  checksum、Postgres advisory lock、重入、并发执行、crash/resume、checksum drift/unknown migration fail closed；
- 所有 tenant-owned table 具有 composite tenant FK、`ENABLE/FORCE RLS`；runtime role 非 owner、无
  `BYPASSRLS`，事务使用 `SET LOCAL` tenant context。缺失 context、伪造 tenant、跨 tenant join/insert/update/
  delete 和 connection-pool context 泄漏测试均拒绝；global table 仅限固定 allowlist；
- durable live-instance registry 的 heartbeat、schema compatibility range 和 drain state 可阻断 contract；N/N-1
  rolling matrix 通过。unknown、stale-but-not-expired，以及 expired 但没有同时证明同 incarnation/generation
  process termination + fencing + endpoint/credential revoke + claim/leader release 的 durable retirement receipt
  的实例均 fail closed；只有完整 retirement receipt 才能从 live set 排除；
- P1 在本地固定输入完成 logical backup/restore、migration replay、outbox/idempotency/leader 恢复并核对数据 digest。
  P1 还须验证 preflight 会在缺少匹配 release/schema digest 的 PITR restore point 或有效 restore-drill record 时
  fail closed；部署级 PITR restore point/drill、HA 与 failover 实证明确保留给 P4，不能用 P1 record 冒充
  `G-OPS`。

### `G-AUTHORITY-P1`

- Tenant/Organization/Project/Membership/basic RBAC、provider catalog、contract、migration、idempotency/outbox/
  leader/operation receipt 各自只有一个声明 writer 与一个持久化 authority；legacy Synara 只作为行为 oracle，
  不作为公共 write path 或 schema authority；
- JSON Schema/OpenAPI、Proto/Connect/gRPC、SDK/generated code 与数据库 migration 的 authority/derived-from 关系
  可机读并固定 digest，禁止手写双 authority 或 server/SDK 双写；
- tenant transaction context、migration owner/runtime role、global-table allowlist 和 durable live-instance registry
  的 owner 明确；故障、重试和回滚测试证明不会切换活动 aggregate writer；
- `G-AUTHORITY-P1` 仅覆盖 P1 foundation；Session/Turn/Worker claim、Lease/workload/pairing/T3 session 等后续
  writer 不在本阶段被提前声明为已验证。

### `G-SECURITY-P1`

- composite tenant FK 与 FORCE RLS 的正反向隔离矩阵通过，runtime role 非 owner、无 `BYPASSRLS`，migration
  owner credential 不进入 runtime 配置、日志、fixture 或制品；连接池复用不会泄漏 `SET LOCAL` tenant context；
- management、service account 与 workload identity 的 issuer/audience/subject/scope/tenant/project/version/expiry
  validation fail closed；签名 key rotation、unknown `kid`/algorithm、revoke 与 clock-skew negative fixtures 通过；
- secret、credential、token、pairing material 不进入 durable receipt、outbox、audit、log、trace、fixture、backup 或
  generated SDK；错误响应、watch cursor 和 stable error 不泄漏跨 tenant 标识或内部 SQL；
- SQL parameterization、request/body/decompression limit、rate limit、deadline/cancellation、watch backpressure、
  dependency/license/secret scan 通过；waiver 必须有 owner、范围和到期时间；
- P1 record 只证明 contract/data/auth foundation，不冒充 P2 Worker secret access、P3 Managed Host pairing、P4
  deployment hardening 或 P5/P6 host cutover/proof-session 安全结论。

## 2. Managed Agent 必测

- Session/Turn create、idempotent retry、interrupt、approval/user-input；
- Runtime crash、Worker replacement、Control Plane restart/outbox replay；
- Workspace edit、checkpoint、Artifact、credential revoke；
- late terminal、sequence gap、backpressure、resume cursor；
- Codex 与 Claude 真实 Turn、rate limit/auth/unavailable；
- 双 Tenant/Project/Provider 并发隔离和有界 soak。

## 3. Managed Host 必测

- CloudEnvironmentLease create/ready/terminate；
- P3 reference host 的 workload/volume/endpoint/grant/cleanup；
- signed HostWorkloadDescriptor allowlist、compatibility、expiry/revoke/downgrade；
- P6 T3 server/Runtime/Terminal/filesystem 同 Workspace；
- P6 direct 与 relay proof challenge/exchange；
- pairing token single-use/no-store、Bearer downgrade 拒绝；
- pairing ephemeral response 不进入 DB/WAL/backup/outbox/audit/log/trace/watch/webhook；丢失响应先 revoke 再
  remint，并发 consume 只有一个成功；`issued/delivery-attempted/consumed` receipt 不冒充 secret 已送达或
  session ready；
- T3 hidden ref/diff/revert、server/browser reconnect；
- old generation endpoint/session/grant/heartbeat 全拒绝；
- partial allocation、failed cleanup、orphan reaper；
- lease 内双 Provider/双项目隔离与 soak。

## 4. Standalone 必测

- 全新机器仅使用 public source/artifact；
- Compose bootstrap、health、真实 Turn、升级、回滚、卸载；
- Helm external Postgres/S3/OIDC、rolling upgrade、backup/restore；
- public images 不依赖 Synara registry/config/secret；
- docs 示例无真实 credential/endpoint；
- default-deny network/identity、non-root、read-only rootfs 与最小 capability。
- initial admin/OIDC bootstrap、显式 Compose/Kubernetes Provider secret、master-key rotation；
- local container actuator 不向 CP/Worker 暴露 Docker socket；

## 4.1 Security/operations 细项

- OIDC issuer/audience/clock skew/JWKS rotation、membership suspend/deprovision；
- signed `LeaseAuthorizationSnapshot` 的 audience/epoch/generation/TTL/refresh；CP↔Supervisor/T3 分区、
  signer rotation/revoke、stale snapshot、现存 HTTP/RPC/WebSocket 在 60 秒硬上限内 fence/close；
- service account、Supervisor/Worker/actuator identity issuance/rotation/revoke；
- Kubernetes ServiceAccount/RBAC/PodSecurity/seccomp/network/egress policy；
- at-rest envelope key rotation、broker grant、webhook signature/replay；
- quota/fairness/noisy-neighbor、outbox DLQ/replay/retry storm；
- metrics/log cardinality与 redaction、alert/runbook、PITR/灾备演练；
- P5/P6 host adapter/client、cutover routing 与 proof session 攻击面。
- Go/TS/OCI/CLI/chart/workflow vulnerability scan、scanner DB freshness、KEV/reachable Critical/High policy、
  VEX 与 time-bounded waiver；base image digest/signature/provenance/support window；

## 5. Closure record

每个 Gate 必须记录：

- evidence ID、DRI、独立复核人、日期；
- public repo、Synara、T3 commit 与 dirty state；
- Go/Node/Bun/pnpm/Provider CLI/SDK/Postgres/Kubernetes 版本；
- contract/module/image/chart/platform manifest digest；
- reference-host/T3 HostWorkloadDescriptor、producer signature/trust identity、image/bundle digest；
- SBOM/provenance/signature/VEX/vulnerability report、scanner/DB timestamp、waiver/base-image digest；
- 原样命令或 CI job、脱敏日志、artifact 地址；
- 每条 exit criterion 的 pass/fail；
- failure、waiver、rerun 与 rollback evidence；
- 签署结论。

没有 closure record 不得把 `IN PROGRESS` 改成 `VERIFIED`。

MCP-SKILL-RUNTIME-V1 的最新未发布 candidate `0.3.0-dev.296` 只记录代码与静态/定向验证：Codex namespace collision fail closed、MCP transport/JSON-RPC failure 保留 started event 并交由 checkpoint/reconcile，且 MCP/Skill outcome 使用 opaque capability ID；58 个 Codex tests、typecheck、格式、diff check 和 22 项制品 checksum 通过。manifest `sha256:8433198e69af58211549b1da694f58a367426b3383d39d7a50ad385876280c09`、checksums `sha256:4569d088a836ad7643190055fd1fdef29b047093926e11f23ee37bc94955fdf6`、Runtime `sha256:535dcd05674b510e51aa7e780047bbcbab59a83283905b8ed02d5c94d3c8b758`、Control Plane arm64 `sha256:95358ea62725becd10a811d6259a0e5044e52bd18c3846485e7e9fcaeb0d3814`。没有受保护 Provider 运行证据，不关闭任何十二格、aggregate Gate 或 release Gate。

Candidate `0.3.0-dev.297` 又补齐失败审计边界：Control Plane 对失败 Turn 中已完成的 capability item 记录对应 outcome，对 started-but-not-completed 的 MCP/Skill 记录 opaque `mcp.fail`/`skill.fail` 与 `capability_call_unknown`，并保留 checkpoint/reconcile 的未知副作用语义。Managed Agent 定向 Go tests、`go vet`、格式和 22 项制品 checksum 通过；manifest `sha256:6c029f21b2b462c9ead9e26520a82e4943ef2d4d341805b1c5ebddececc1f082`、checksums `sha256:0c225326de83ae40d70f212d26e227c0ada6d462b0882393e040cf782ffe5d48`、Runtime `sha256:535dcd05674b510e51aa7e780047bbcbab59a83283905b8ed02d5c94d3c8b758`、Control Plane arm64 `sha256:372253b80c511e342f9d128c211503e90b2fe53caba259d04de68909a4dce00e`。没有受保护 Provider 运行证据，不关闭任何十二格或 Gate。

## 6. Release 与 Exposure

顺序固定：

```text
source gates
  -> immutable candidate manifest
  -> same-bits standalone/Synara/T3 E2E
  -> digest recheck
  -> G-PLATFORM-RELEASE
  -> RC
  -> independent G-EXPOSURE
```

公开 GitHub/source 与当前 Runtime prerelease 是历史已发生状态。新的 Platform Go module、container、chart、
部署 channel、internal beta、public beta 和 GA 各自独立关闭 `G-EXPOSURE`；“不得公开”只约束尚未批准的
具体 Platform channel，不反向否认已公开的 Runtime source/prerelease。

Platform RC 必须同时关闭所有单阶段 Gate、上述四个 aggregate Gate、`G-SUPPLY-CHAIN` 与
`G-PLATFORM-RELEASE`；phase record 本身不能替代聚合 Gate 或 RC closure。

Candidate `0.3.0-dev.298` 修正失败审计的 item identity：pending MCP/Skill 以 capability ID 与 provider item ID 复合键跟踪，避免不同 capability 复用 provider item ID 时错误归因。Managed Agent tests、`go vet`、格式和 22 项制品 checksum 通过；manifest `sha256:5e0fd377624d77a6c9ac549ba5ce31ae6857765049a9297e138cf4791f81edff`、checksums `sha256:7044f7395a1c27148eb126b5e74c567fa24095e3a12e025b26e73429fc37ed81`、Runtime `sha256:535dcd05674b510e51aa7e780047bbcbab59a83283905b8ed02d5c94d3c8b758`、Control Plane arm64 `sha256:6c31072cb9778c7dbc5f9cf59645a1181342a21d09c90fe3f4bee8bf35e52b51`。没有受保护 Provider 运行证据，不关闭任何十二格或 Gate。

Candidate `0.3.0-dev.299` 收紧 pinned deepseek-harness `0.1.2-rc.1` 的 tool-result 失败语义：`tool/result` 携带 `isError`、error status 或 error object 时输出 failed activity 并终止 Provider Turn，不把 MCP 不可用伪装成 completed；4 个 deepseek tests、typecheck、格式和 22 项制品 checksum 通过。manifest `sha256:67c78ca498a92fdb34817e043ed1f2affef99564e5a03e1703a00c413b4fddff`、checksums `sha256:9b92ba070b414036e2c00112ace05a0c163dc0f7ca92bfb62aa2549462ea8b63`、Runtime `sha256:61d4c277411c0df02166252d71d98204e239e01805e85b9500ff55d4f7621707`、Control Plane arm64 `sha256:c1b96dcb4d55a610bb8e73422cda044a96cc130958abb98ac5a4a72a69012947`。没有受保护 Provider 运行证据，不关闭任何十二格或 Gate。

Candidate `0.3.0-dev.300` 进一步保证 deepseek tool-result failure 立即中止 Harness 并忽略后续通知，避免 MCP 不可用后继续接受新的工具活动；4 个 deepseek tests、typecheck、格式和 22 项 checksum 通过。manifest `sha256:ae40041c7188bd9531940ac3af721f0a92ce935ac7cc8624db0ac0685204b883`、checksums `sha256:5515086d584d334dd3afbbf465dc1b1409589692fed4db3bc7b9737f1cb59c8d`、Runtime `sha256:747aa7061b53d774f2a7133a07d62a72e5962ffd419563d42ee668fefac900be`、Control Plane arm64 `sha256:d74815dc0716db225943d5880d2ceb41fd45d321d386694a906e2743f84edbc1`。没有受保护 Provider 运行证据，不关闭任何十二格或 Gate。

Candidate `0.3.0-dev.375` 将 Codex MCP 工具暴露从 deferred 隐藏修正为显式 direct（`omit_tools_from=[]`），并以同一受保护 fixture、Codex `0.154.0` Worker image `sha256:61eeb7f971f5ad8e66f7e6307f1f2ba0fff28f81678c3d6a201024e436c76e9d` 真实复跑：MCP `tools/call`/副作用、签名 Skill、事件续读、撤销负向和 Compose 清理均通过；日志 `.tmp/mcp-skill-runtime-v1-20260919-r375-codex-direct.log` digest `sha256:f04c02e86272e383a591f3e46600328bd416eb1b2537f9a198f99b3b214f7aa5`。这仍不是完整 Gate：Worker/Agent 重启、跨节点、逐格负向及 RemoteWorker/Kubernetes 未闭合。

2026-09-20 r430 新增 deepseek-harness×Kubernetes 单节点真实子证据：同一验收命令在 Docker 与 OrbStack Kubernetes 均通过 MCP 8、Skill 1、Artifact verified、events resumed；Kubernetes Control Plane 重启后 `capabilityBound=true`、attempt 2、`process-restart/recovered/confirmed`、RPO 0，撤销负向为零请求/零副作用/2 events，脚本 exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260920-r430-deepseek-kubernetes-capability-recovery.log` SHA-256 `sha256:07a2ef15c5743963148730f0e46c79fa721fcde90e1f276b00590138473dde5f`，资源清理为 0；OrbStack 单节点，未证明 Kubernetes cross-node、Worker/Agent 退出或完整十二格，正式 Gate 继续开放。

2026-09-20 r431 在隔离 kind 三节点上重复验证 deepseek-harness Docker/Kubernetes acceptance、事件续读、同节点 capability-bound process-restart 与撤销负向；日志 `.tmp/mcp-skill-runtime-v1-20260920-r431-deepseek-kubernetes-cross-node.log` SHA-256 `sha256:696da53554fe8eaafcfe97e370cf08b4ce91985d93d749d7dd408465935b2541`。普通跨节点 takeover 虽恢复（RTO 32972ms、RPO 0），但 `capabilityBound=false`，不满足能力绑定跨节点 Gate；首次缺 CRD 由 preflight fail closed 且不计数，临时资源清理为 0。

2026-09-20 r437 修复并真实验证能力绑定跨节点注入：Kubernetes 恢复后 fixture/sync/cleanup 改用目标 `kubernetes_active_namespace`，OpenSandbox 0.2.0 CRD/RBAC preflight 保持 fail-closed。未发布 dirty candidate `0.0.0-dev.r398`（sourceCommit `549d5d6f0052d2c06dc90f7b97c1dc11cd28ce04`、sourceDirty=true；manifest `sha256:65cd73e0c92c15f7bb6bc7f62e2ebab9036dc0d02998f719b0f94fa6be61ebb5`、checksums `sha256:99b785379b408d24b1e5d50f87eb178071ab4b818edbe7222b2ad13fbfcc3bfe`）在隔离 kind 三节点完成 deepseek-harness Kubernetes cross-node：`capabilityBound=true`、attempt 2、`cross-node-takeover/recovered/confirmed`、RTO `100149ms`、RPO `0`、snapshot `880640` bytes；恢复后 acceptance 为 MCP `14`、Skill `1`、Artifact verified、events resumed，撤销负向为零请求/零副作用/2 events。日志 `.tmp/mcp-skill-runtime-v1-20260920-r437-deepseek-kubernetes-capability-cross-node.log` SHA-256 `sha256:5df2ccdb800490ce63a5e890dc0ff84a456d4070ac7d594350dfdf8614fce45f`，Compose/kind/fixture 资源为 0；正式 Gate、完整十二格、其它 Provider/环境和 Worker/Agent 仍开放。

2026-09-20 r442 仅满足 Pi×RemoteWorker 的 capability-bound cross-node 子验收：真实结果为 Docker `mcp_requests=8/side_effects=1/skill=1/artifact=verified/events=resumed`，RemoteWorker 恢复后 `mcp_requests=14/side_effects=1/skill=1/artifact=verified/events=resumed`，Control Plane 为 `attempt=2/cross-node-takeover/recovered/confirmed`、`capabilityBound=true`、RTO `8675ms`、RPO `0`，撤销负向为零请求/零副作用/2 events，脚本和 Compose smoke exit 0。候选 `0.0.0-dev.r441` 的 manifest/checksums 为 `sha256:1e5d44bf88f6c82634918044f0e4d8b64cda129d248afa38b4610aebbf9ae58a` / `sha256:ccea0543ff41a82c10c7b3aa54df24e9454a4f3ce1d536724e65212c5ca27d7c`，日志 `.tmp/mcp-skill-runtime-v1-20260920-r442-pi-remote-worker-capability-cross-node.log` digest `sha256:288ceccca1ac0859e8e9bff02c0cb6e699aa6fea283704a0587e18530a6eb59b`，目标 DIND/Compose/fixture 资源精确清零。目标 daemon 解析与重复 recovery guard 保持旧 generation fencing、Operation、Audit 和 fail-closed 语义；仍未覆盖整格所需的版本不兼容、跨租户、旧 generation、Worker/Agent 重启和未知副作用矩阵，因此正式 Gate 与完整十二格继续开放。

2026-09-21 r445 新增 deepseek-harness×RemoteWorker capability-bound cross-node 子验收：Docker acceptance `mcp_requests=8/side_effects=1/skill=1/artifact=verified/events=resumed`，RemoteWorker 恢复后 `mcp_requests=14/side_effects=1/skill=1/artifact=verified/events=resumed`，Control Plane 为 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered/confirmed`、RTO `9734ms`、RPO `0`、snapshot `850944` bytes，撤销负向为零请求/零副作用/2 events，脚本和 Compose smoke exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260921-r445-deepseek-remote-worker-capability-cross-node.log` digest `sha256:708db8de02635d561ced3580fbfeddd84a3fff8f3fccb7c5783f2d263f96c127`，descriptor/Skill digest 为 `sha256:960485b501072312ecebee0944d12446c45f3a7de884efdb96b27848823abf56` / `sha256:bef35cb1e0b292e7c26a79fefcf93e144c977842ac12043e1280841b9120b62e`，DIND/Compose/RemoteWorker/OpenSandbox/网络/卷/fixture 精确清零。r444 的 HTTP 499 follow-up 已按 fail closed 保留为未计数诊断；版本不兼容、跨租户、旧 generation、Worker/Agent 重启和未知副作用矩阵仍未闭合，正式 Gate 与完整十二格继续开放。

2026-09-23 验收边界更新：r525 Claude×Kubernetes 与 Pi×Kubernetes transport/revocation 真实通过，日志 SHA-256 分别为 `2d9aeacad111e4fb942a849a7519142173ab0da9ab28753e188cf8f5896c47d8`、`e1c1346fa4ed2386d38c2ac6b59c4530ad3c30dd779fa9620f1016b4bef78a4d`；deepseek-harness×Kubernetes、×Docker 在真实 MCP 请求后均 fail closed 为 `provider_unavailable`（`29b2fcbd4c8c4d0ffb1f98212bfbfbf52aa892498da65e5a49f680454e29e858`、`4ab6452607472e3efb83655caab54606bba3b3e7e85c18b9fb59653e8c631db2`），r527 Docker 重试仍失败（MCP 8、side effect 1，日志 SHA-256 `91a6599895205f793ac54572ea153d01d14789a604e58f178258ba3f0718e4e9`）。Codex×RemoteWorker r524 无 transport marker（`bdb9d43f2a3a6d5010f8c6fdcca1b23f8660ac7272256b49baa55e674e7558c0`），不计为通过；脚本在 transport-only 模式跳过通用 recovery，静态测试 32/32。不得据此关闭完整十二格或正式 Gate；Admin 仍只显示 opaque 元数据和 Operation/Audit，版本不兼容、跨租户、旧 generation、撤销及未知副作用必须维持 fail-closed/先对账后重试。

2026-09-19 新增真实 Codex×Docker 子证据：未发布 candidate `0.3.0-dev.373`（sourceCommit `549d5d6f0052d2c06dc90f7b97c1dc11cd28ce04`，sourceDirty=true；manifest `sha256:7c0b37dfd1b9b235f6c86948652b9d712f46ffeb822463f0a7286f5a61425c93`，checksums `sha256:fdd528ed77ec5ce69766b7325c9285368935c02e3e3b34deab9737c66b86878b`）使用 Codex `0.154.0` Worker image `sha256:e0f394a3ffe66576b03cc82cb9cc1dc21be5d350181e59e7057f7c00dd07b0b6`，并以 `approval-required` 运行真实 capability acceptance。结果为 MCP `tools/call`/1 次副作用、1 个签名 Skill、事件断线续读、撤销零请求/零副作用和 Compose smoke/清理通过；日志 `.tmp/mcp-skill-runtime-v1-20260919-r373-codex-docker.log` digest `sha256:979a398c41e8818115f198a8c61429e35c7a90f3f9048c5d0ff74287b563761b`。`full-access` 仍按安全 Hook fail closed，不把该格提升为完整 Gate；Worker/Agent 重启、跨节点接管、逐格负向矩阵与 RemoteWorker/Kubernetes 仍开放。

2026-09-24 r551 不改变 Gate 判定：本地 candidate `0.3.0-dev.551` 的唯一 Codex×Docker 真实运行在 contract negative 通过后于 Provider Turn 前 `provider_unavailable` fail closed，未产生 MCP/Skill acceptance、transport/reconnect、撤销或 recovery checkpoint；不能以 Admin smoke、静态测试或候选 checksum 替代十二格真实证据。旧 generation、跨租户和版本/digest 拒绝仍以 r542/r471/r472 的真实结果计数；未知副作用仍须先对账，任何未形成 checkpoint 的 recovery 不得计为通过。正式 Gate 与能力目录 supported 保持开放。

用户确认继续使用 `gpt-5.6-luna`；但当前 Provider 的真实 `/responses` 探针在两条网络路径均返回 `MODEL_NOT_ALLOWED`。因此 Gate 暂停在 Provider 前置可用性，不切换模型、不修改凭据、不重复启动会必然失败的真实矩阵；模型 allowlist 恢复后才可继续记录 Provider×环境证据。

验收脚本的 `capability_bound_recovery=0` selector 误触发默认 Docker recovery 已修复并由 32/32 回归锁定；这消除 harness 阻塞，但不替代 Provider 模型 allowlist 修复。

Gate 更新（2026-09-24）：r554 在 gpt-6-luna 下真实关闭 Codex×Docker 的 MCP/Skill acceptance 与未知副作用对账子路径。证据同时包含 `state=running` 的 pending checkpoint、断连重连、`replayed=0`、contract-negative、expired grant、事件恢复和清理结果；故不能把未知结果盲目重放。完整四 Provider×Docker/RemoteWorker/Kubernetes 十二格、重启/跨租户/版本兼容/旧 generation 及 Kubernetes recovery 仍未形成新的全量 Gate 通过记录，正式 Gate 保持 open。

Gate 更新（2026-09-25）：r559 在 gpt-6-luna 下真实关闭 deepseek-harness×Docker 的 transport-only 子路径：MCP 8、Skill 1、Artifact verified、events resumed、断连重连 `replayed=0`，撤销后 runtime requests/side effects 为 `0/0`，跨租户 MCP/Skill 为 `401`，旧 generation 为 `409`，Compose smoke 与精确资源清理通过。首次 r559 运行只停在 Worker upgrade preflight，未产生 Provider marker，不能计入验收；计数的是同一 candidate 的第二次运行。完整十二格、RemoteWorker/Kubernetes、Worker/Agent 重启和正式 Gate 仍保持 open。

Gate 更新（2026-09-25）：r561 在 gpt-6-luna 下真实关闭 Pi×Docker 的 transport-only 子路径：修复 `openai-completions` 到 `openai-responses` 的真实 endpoint 不匹配后，Pi MCP 8、Skill 1、Artifact verified、events resumed、断连重连 `replayed=0`、撤销后零请求/零副作用、跨租户 401、旧 generation 409 和 Compose smoke 均通过。该结果只计 Pi×Docker 子路径；其余未通过格、Worker/Agent 重启、能力绑定跨节点和完整 Gate 仍开放。

同一 r561 candidate 的 Pi×RemoteWorker 真实闭环进一步通过：Docker/RemoteWorker acceptance 与 Artifact/事件续读、Control Plane SIGKILL 后 `capabilityBound=true` 的 `cross-node-takeover/recovered/confirmed`（RTO `9695 ms`、RPO `0`）、transport `replayed=0`、撤销零请求/零副作用、跨租户 `401`、旧 generation `409` 和精确清理均通过。该结果只关闭 Pi×RemoteWorker transport/能力绑定跨节点子路径；Pi×Kubernetes、Worker/Agent 退出和完整 Gate 仍开放。

r561 Pi×Kubernetes transport-only 重跑虽到达 Docker/Kubernetes acceptance 与跨租户 `401`，但 Kubernetes 执行在 `running` 状态收到 `unexpected EOF`，wrapper exit 1，未形成 transport、撤销或旧 generation 的可计数终态；日志 SHA-256 `4215ee30a37ba89cc7afd50ba73174fe24848257e113d1e0ef6a18e25898b582`，资源清理为 0。acceptance-only 诊断也只产生 Skill、未产生 MCP call（日志 SHA-256 `083981fb3c904e8a07bb12bfe97c824b9eaca42de8c5587c05f5678fdb3f6dd7`）。因此历史 r525 Pi×Kubernetes transport/revocation 仍单独保留，r561 不新增该格支持，正式 Gate 继续 open。

同一 r559 candidate 的 deepseek-harness×Kubernetes 运行出现上述终端 capability marker，但 wrapper 最终 `exit 1`，因此按 fail closed 不关闭该格；deepseek-harness×RemoteWorker 则因 recovery Turn 未形成 pending-side-effect checkpoint fail closed，未关闭该格。完整 Gate 继续开放。

### Gate update（2026-09-25：MCP-SKILL-RUNTIME-V1 closeout evidence）

r568 关闭了 Codex×Kubernetes capability-bound cross-node 的真实子 Gate：隔离 kind 三节点先 fence 旧 writer，再删除/隔离源节点并用 portable snapshot 在目标节点恢复 Workspace/Sandbox 与 capability binding；attempt 2 为 `cross-node-takeover/recovered`，side effect outcome `confirmed`，RTO `95560 ms`、RPO `0`、snapshot `4096000 bytes`；harness 未输出独立 snapshot digest，只保留整份日志 SHA-256，恢复后的 MCP/Skill/Artifact/事件续读、撤销负向、跨租户/版本拒绝和旧 generation `409` 均有日志标记。日志 `.tmp/mcp-skill-runtime-v1-20260925-r568-codex-kubernetes-cross-node-closeout.log` SHA-256 `80702ec5d0d1b2378ecbbe13817c51be68e36776ebe0ad2b8234d11b62dd985d`。

r562–r566 没有关闭 Worker/Agent 故障 Gate：deepseek-harness×Docker、Codex×Docker、Pi×RemoteWorker 与 Codex×Kubernetes Agent 为 FAILED，Claude×Docker 为 BLOCKED；Kubernetes direct Sandbox 的 Worker fault 由脚本以无绑定 Worker target 明确判定为不适用。r567 只记录 OpenSandbox 前置缺失的未计数 preflight。五个用户指定的 transport 开放格子仍未全部形成终态，Pi×Kubernetes 的历史 r525 通过与最新 r561 `unexpected EOF` 失败继续分开，原始 `gpt-5.6-luna` 复验仍 BLOCKED 于 `MODEL_NOT_ALLOWED`。因此十二格能力与正式 Gate 仍为 OPEN；不得把 r568 或任何 partial marker 提升为实现验收完成。

r569 将 Codex×Kubernetes transport 子 Gate 明确记为 BLOCKED：真实 harness 在 OpenSandbox token 创建阶段因 `k8s.orb.local:26443` `unexpected EOF` 退出，未产生 Provider 或 transport marker。日志 `.tmp/mcp-skill-runtime-v1-20260925-r569-codex-kubernetes-transport-closeout.log` SHA-256 `6fffcddf07fe6f4d4ad3d92240b104713c63deb38c8aa770d8df202c649b587e`；不把外部 Kubernetes EOF 归因于 Runtime，也不重试。完整 transport Gate 继续 OPEN。

r570 补充 deepseek-harness×Kubernetes 的真实边界：正向 MCP/Skill/Artifact/事件续读和 contract-negative 通过，但 transport 阶段出现 `unexpected EOF`，未形成 transport/revocation/stale-generation 终态；按外部运行时 EOF BLOCKED 记账，不能用 partial acceptance 关闭该 Gate。日志 `.tmp/mcp-skill-runtime-v1-20260925-r570-deepseek-kubernetes-transport-closeout.log` SHA-256 `aabf4f190a84e01567d7546ee1d902f4beadf56e158a4fb38b9286bc97915539`。

Gate update（2026-09-25：r571 snapshot digest rerun）：为修复 r568 未输出独立 snapshot digest 的验收证据缺口，harness 只增加数据库 `workspace_snapshots.content_digest` 的精确读取和格式断言；随后唯一一次真实三节点 kind 重跑在 OpenSandbox controller/server 镜像拉取阶段因 `127.0.0.1:6152` 代理 connection refused 进入 `ImagePullBackOff`，未进入 Provider、snapshot restore 或 cross-node marker。按外部 registry/proxy 规则记为 BLOCKED，不重试；r568 的 cross-node PASS 仍保留但 digest 缺口仍未闭合，故 Codex×Kubernetes cross-node 完整子 Gate 与正式 Gate 均继续 OPEN。日志 `.tmp/mcp-skill-runtime-v1-20260925-r571-codex-kubernetes-cross-node-digest-closeout.log` SHA-256 `798fbb9346e41d6e3e6451ae8f7639b1c2e6621b6986be4132fbab026413e752`，kind/namespace/Pod/kubeconfig 清理为 0。

Gate update（2026-09-25：evidence consolidation）：r575 `UNBLOCK-OPENSANDBOX-RUNTIME-LIFECYCLE` 只把固定 OpenSandbox server 的 Docker `run → /health 200 → cleanup` 记为 `PASS (lifecycle preflight)`；sourceCommit `549d5d6f0052d2c06dc90f7b97c1dc11cd28ce04`、image digest `sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb`、preflight log SHA-256 `6641397fce9cb562b58f95705910692178908354bf3411512dba9f2682abb97f`，没有 Provider/Kubernetes harness/snapshot/npm 证据，因此不关闭任何 Provider×环境 Gate。

r582 `MCP-SKILL-RUNTIME-V1-CODEX-KUBERNETES-SNAPSHOT-CLOSEOUT` 现有完整真实证据：dirty sourceCommit `549d5d6f0052d2c06dc90f7b97c1dc11cd28ce04`、Worker image `sha256:573196d95aeac01fcf5d1b81235a48ed2379ccd56fd3f1cb6a922457f6fba9fc`、四个精确 npm 包 hash、kind 三节点、Codex、canonical snapshot `sha256:6673118d540d845887e887e12c286c8c0df4a8f05911363929eb34e88b5ae019`、raw archive `fcacb0c10cd9acc914ca5948a4bfd04bd1a89fd46610cc45ff124bcb3c98f4ab`、RTO `48802ms`、RPO `0`、harness log SHA-256 `335c8d4a3d0b03f57875c63ed9bee94da1c7097633475e689a897b3261ccf6e9`、cleanup 清零。故 Codex×Kubernetes cross-node snapshot 子 Gate 为 `PASS`；transport、Worker/Agent 和 aggregate/formal Gate 仍按 [06](06-status-tracker.md) 的逐格状态保持 OPEN。
