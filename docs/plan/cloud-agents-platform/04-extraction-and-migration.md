# 04. 基础设施、Admin Web 与 Anywhere Runtime 实施计划

2026-09-24 继续沿用同一 Control Plane/Runtime 注入、generation fencing、Operation/Audit 与副作用对账语义：r542 candidate `0.3.0-dev.542` 的四 Provider×Docker/RemoteWorker/Kubernetes contract-only 负向真实覆盖不兼容 MCP/Skill 版本或 digest、跨租户 `401`、旧 generation `1→2` 的 `409`（12/12），不新增 supported 结论。r545 在该 candidate 上补齐 Pi×RemoteWorker capability-bound cross-node 子路径（`cross-node-takeover/recovered/confirmed`、RTO `10971ms`、RPO `0`、MCP/Skill/Artifact/事件续读与撤销负向通过），但关闭了 Worker/Agent 故障注入，不外推为进程退出验收；精确制品、命令、日志 digest 与清理见 [06](06-status-tracker.md)。

r548–r550 继续按 fail closed 记账：Codex×Docker transport 在断连后错误返回成功（`be07dfb25294e955a4a02f21279abdfb3e4db1cbfd72bfde1bb4ac41d45d9ad1`），r549 Claude×RemoteWorker 首次能力执行 `provider_unavailable`（`8e6352ed62888d8e54cb8e5d5f34a4c4481a93183cb3744778fd291e8315b961`），Pi×Kubernetes 在 Docker Skill 后未产生 MCP call（`926e0c2eee0be398725512e70c84763c6d5c577fc76479e52f3c1c1a1d63eed3`）；三轮均不推进能力目录、Recovery 或迁移阶段。未知副作用仍须先完成 checkpoint digest/outcome 对账，未形成的 transport/recovery 不得推断为成功。

r544 的 Pi×RemoteWorker Agent 故障和 r546 的 Worker 故障均按 fail closed 保留：前者等待 capability-bound side-effect checkpoint 超时，后者未持久化 pending-side-effect checkpoint，并伴随 RemoteWorker heartbeat 错误；均没有 acceptance/recovery 终态，不得重放未知副作用或推进迁移阶段。OpenSandbox `Exec` 已在取得 command ID 后断流时改为查询 status、取得 ID 前断流继续拒绝重放；`go test ./services/control-plane/internal/opensandbox ./services/control-plane/internal/kubernetestarget ./services/control-plane/cmd/cloud-agents-remote-worker ./services/control-plane/internal/managedagent` 通过。

2026-09-22 r468/r469/r476 在同一未发布 dirty candidate `0.0.0-dev.r459` 上补齐 Claude×Kubernetes、deepseek-harness×Kubernetes（OrbStack `orbstack` 单节点）和 Claude×RemoteWorker 的真实 MCP/Skill 子路径。随后隔离 kind 三节点 r482/r480/r483 又分别完成 Claude、受控 gpt-5.5 deepseek-harness、受控 gpt-5.5 Pi 的 Kubernetes `cross-node-takeover/recovered`；Codex 的唯一允许重跑 r467 仅登记 Kubernetes 同节点 `process-restart`，不外推为 cross-node。Worker/Agent 方面，r490（受控 gpt-5.5 Codex）与 r491（受控 gpt-5.5 Claude）完整通过双进程故障、MCP/Skill、事件续读和撤销；Pi/deepseek 没有持久化 user-input/approval callback，仍明确未覆盖。精确日志、RTO/RPO、候选 digest 和模型 caveat 见 [06](06-status-tracker.md)。

r471/r472 对同一 candidate 做 contract-only 负向验收，复用现有 Control Plane/Runtime binding、权限、版本和 generation fencing：四 Provider 在 Docker+Kubernetes 及 Docker+RemoteWorker 均拒绝伪 `9.9.9`/digest 不兼容引用（MCP/Skill），跨租户读取返回 HTTP `401`，旧 generation `1` 对当前 `2` 返回 HTTP `409`；日志 SHA-256 分别为 `sha256:71d898d9b7efb8f226d05c5eb92b25d2c9d548df033aafe205ed7035a51d34ba` 与 `sha256:ccf6ce72ea4a60977c7ce6e4aa675d37bc7d9967eef4f0c3f1db4d3752fe96c3`，两轮 exit `0`。这些路径不改变能力目录状态，也不泄漏 Secret；未知副作用仍必须先进入 `awaiting_reconciliation/side_effect_outcome_unknown`，完成 checkpoint digest/outcome 对账后才允许恢复；未新增调度器、任务内核或第二套 fencing。

r468/r469/r476 的恢复先持久化 pending-side-effect checkpoint；Control Plane 失效后盲目 replay 均保持 `awaiting_reconciliation/side_effect_outcome_unknown`，只有探测并提交 checkpoint digest/outcome 后才允许 reconcile 和 attempt 2。`confirmed` 与既有 r459 RemoteWorker 的 `not-applied` 均有真实证据，未知结果不会重放外部副作用。r473（Claude Docker `Write/file_change=failed`，未生成 ArtifactCandidate）、r474（RemoteWorker cross-node 前置不满足）、r475（通用恢复断言冲突）和原始 Claude fixture 的 r489 均保留为未计数/部分计数诊断。所有本轮 task-owned Compose/OpenSandbox/Kubernetes 临时资源和 fixture 清理为 `0`，secret scan 命中 `0`；Pi/deepseek Worker/Agent restart、Codex Kubernetes cross-node、逐格断连/撤销组合和正式 Gate 仍开放。

2026-09-22 r463 修复 capability-bound recovery 的共享调用顺序：每个启用 Provider 在撤销 capability 前完成 recovery，避免撤销后的 `resolveRuntimeCapabilityBindings()` 把有效 recovery 误判为 `CAPABILITY_UNAVAILABLE`；未新增调度器、任务内核或 fencing。未发布 dirty candidate `0.0.0-dev.r459` 在家庭代理下使用受保护 Pi 凭据临时副本真实完成 OrbStack 单节点 Pi×Kubernetes 同节点 `process-restart/recovered`（attempt 2、`capabilityBound=true`、side effect `confirmed`、RTO `0ms`、RPO `0`、snapshot `0`），正向/撤销分别为 `mcp_requests=8`/`runtime_requests=0`、`side_effects=1`/`0`、`skill=1`、`artifact=verified`、`events=resumed`/`2`，脚本/Compose smoke exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260922-r463-pi-kubernetes-recovery-order.log` SHA-256 `sha256:326dc8a9423267ba59e89d788e529ec34534da29a190eed6236b95abef5cf64e`；descriptor/Skill digest 为 `sha256:c648a9070820595b96b91fc84284b2852175d1b26529c303882bdb498420ef74` / `sha256:bef35cb1e0b292e7c26a79fefcf93e144c977842ac12043e1280841b9120b62e`。回归检查 `sh -n scripts/test-platform-compose.sh`、`git diff --check`、`bunx vitest run scripts/lib/platform-release.test.ts` 为 31/31；secret scan 命中 0，fixture/Compose/OpenSandbox managed volume 精确清理为 0。该结果不外推到 Kubernetes cross-node、Worker/Agent、其它 Provider 或完整十二格/Gate。

2026-09-21 r459 将 durable runtime transcript checkpoint/settle 的消息上限从 64 扩展到 128：新增 migration `000100_expand_managed_agent_runtime_transcript.sql`，同步更新平台迁移 SQL 白名单、生成的 foundation migration package 与 focused tests。未发布 candidate `0.0.0-dev.r459` 的 platform manifest/checksums/migration tar/product schema digest 分别为 `sha256:e558d3f9c1e7bb88a4fa23a9a4ea074996de84d2dd04842e379c50dd691d4774` / `sha256:2f3ff3c9ffefd52a6cd3ef47537b77ae0167d72ee98bd7dff23d8bcb84243699` / `sha256:71af7cdb3e150ccd6d6b4f4f0b5d1019ed9027aeb803e7be8249596bf0b55489` / `sha256:fcb8dcbd70b620efd5175b640aaff4d4d88dc956e22ecaf9cd723f8acbb96a87`；未新增第二套调度器、任务内核或 fencing 语义。

同日 r459 在公司代理下真实完成 Codex×Docker 与 Codex×RemoteWorker capability-bound cross-node：MCP/Skill/Artifact/事件续读、撤销负向和精确 Compose 清理通过；RemoteWorker 为 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered`，未知 side effect 先判定 `not-applied`，再显式 reconcile，禁止盲目重放，RTO `10716ms`、RPO `0`、snapshot `3542528` bytes。日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-codex-remote-worker-capability-cross-node.log` SHA-256 `sha256:112aa2ac94cddb52649f061bfa50d978b29c31234ee9f32e3e6e1202758cfb13`；该结果只新增 Codex Docker/RemoteWorker 子路径，不外推到其它 Provider、Codex Kubernetes、Worker/Agent 重启或完整十二格/Gate。

同一 candidate 在 OrbStack `orbstack` 单节点 Kubernetes 上真实完成 Codex 正向注入、MCP/Skill/Artifact、事件续读和撤销负向（`mcp_requests=14`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`；撤销后零请求/零副作用/2 events）。日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-codex-kubernetes-capability.log` SHA-256 `sha256:b3eaa94cc7403647af2247d98237a3208fa406a4492bfeed1db535961811f5cf`。本轮 recovery 没有形成 pending-side-effect checkpoint，整体脚本因此 fail closed；并发更新与 recovery session `409` 重试不计 recovery，不能外推为 Kubernetes cross-node 或完整十二格成功。

同一 r459 candidate 在公司代理和用户新提供的 `tenant-local.pi.json`/`tenant-local.deepseek-harness.json` 受保护临时副本下，真实完成 Pi×Docker 与 deepseek-harness×Docker：两者均 `mcp_requests=8`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`，撤销后 `runtime_requests=0`、`side_effects=0`、`events=2`，脚本与 Compose smoke exit 0。Pi 日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-pi-docker.log` SHA-256 `sha256:a1d1f4e8071595beb65cbea753e0bcef8252b401a05339ac3696bb0fb2d8845c`；deepseek 日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-deepseek-docker.log` SHA-256 `sha256:3e49902ccad8af833c27aabf04cce7302ede5415159020148fda506f037f2c7b`。临时凭据目录、fixture 和项目标签下容器/网络/卷/OpenSandbox 资源均精确清理为 0，原始凭据未修改；该结果只推进当前 candidate 的两个 Docker 子路径，不外推到 RemoteWorker/Kubernetes、重启/跨节点或完整十二格。

同一 r459 candidate 继续在公司代理下真实完成 Pi×RemoteWorker 与 deepseek-harness×RemoteWorker capability-bound cross-node：两者 Docker/RemoteWorker 均完成 MCP call、签名 Skill、Artifact、事件续读和撤销负向；RemoteWorker 源节点失效后均为 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered`，side effect `confirmed`，RPO `0`。Pi RTO `9724ms`、RPO `0`、snapshot `23040` bytes，日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-pi-remote-worker.log` SHA-256 `sha256:6de9b98e3de52b0589e12e2d93a9ff1f93d452af4ace39a6ba9edcfec6236160`；deepseek-harness RTO `7514ms`、RPO `0`、snapshot `851456` bytes，日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-deepseek-remote-worker.log` SHA-256 `sha256:bf8706b56ca57d46b2a0d133243405a04a0e131bfb605a27d1cde7c604ef5d5c`。恢复后两轮 RemoteWorker 均为 `mcp_requests=14`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`，撤销后均为零请求/零副作用/2 events，Compose smoke exit 0；受管 descriptor/Skill 由现有 materialization generator 生成，Pi/deepseek descriptor digest 分别为 `sha256:3f3be3f478c91927993fb2d1066951430499a786f877bfc3798c760997592802` / `sha256:b494d011b8a05ff98ea425fc8b641d0c3de5fae27f949ded8ff2a1e11e201a2c`，Skill digest 为 `sha256:bef35cb1e0b292e7c26a79fefcf93e144c977842ac12043e1280841b9120b62e`。临时凭据/descriptor 目录已删除，五个本轮 Compose 项目及其容器、网络、卷均精确清理为 0，credential string scan 为 `3`、日志命中为 `0`；该结果只新增两个 Provider 的 RemoteWorker capability-bound cross-node 子路径，不外推到 Kubernetes、Worker/Agent 退出或完整十二格/Gate。

同一 r459 candidate 在 OrbStack `orbstack` Kubernetes 上又真实完成 Pi×Kubernetes 与 deepseek-harness×Kubernetes 的正向 MCP/Skill/Artifact/事件续读及撤销负向：两轮均 `mcp_requests=8`、`side_effects=1`、`skill=1`、`artifact=verified`、`events=resumed`，撤销后 `runtime_requests=0`、`side_effects=0`、`events=2`。Pi 日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-pi-kubernetes.log` SHA-256 `sha256:8f072bdb5e0adca9e1edee0fb65171ecf71a539be7f360ebd1c6c0d4475f8686`；deepseek 日志 `.tmp/mcp-skill-runtime-v1-20260921-r459-deepseek-kubernetes.log` SHA-256 `sha256:839f2b0bc492f282cf697614e54dc9858f3fde9e30a8cfa66798b519fd73b4db`。两轮随后因未形成 pending-side-effect checkpoint 的 recovery 按 fail closed exit 1；不登记 Kubernetes recovery/cross-node，亦不把正向/撤销子路径提升为整格或 Gate 通过。Kubernetes namespace/Pod、Compose 项目资源与临时物料均精确清理为 0。

2026-09-21 r448 在修正 Claude Bundle 的 `.claude-plugin/plugin.json` 与 qualified Skill 目录后，复用 dirty candidate `0.0.0-dev.r441` 和家庭代理真实完成 Claude×RemoteWorker capability-bound cross-node：Docker/RemoteWorker 均 MCP、Skill、Artifact、事件续读通过；源节点失效后 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered/confirmed`、RTO `8630ms`、RPO `0`、snapshot `346112` bytes，撤销后零请求/零副作用/2 events，Compose smoke exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260921-r448-claude-remote-worker-capability-cross-node.log` SHA-256 `sha256:3a146ad78c5614c524b400ac59edd5b79b74b49a29972aa110f843fb8c686b86`，descriptor/Skill digest 为 `sha256:6df24a1125c6c0c14196593b704c22136da54648625ba2895c4585d38751da7f` / `sha256:6691df0715ff977a8a1e0866e15948fb48879b358677b8290c34a329a6db6901`，Compose/RemoteWorker/DIND/OpenSandbox/网络/卷/fixture 均精确清理为 0。r446（Bundle provenance 不完整）与 r447（registry 前置连接拒绝）均按 fail closed 不计数；该结果只新增 Claude×RemoteWorker 能力绑定跨节点子路径，不外推到其它 Provider/环境、Worker/Agent 退出、逐项负向或完整十二格/Gate。

2026-09-20 最新边界：Pi×Docker 与 deepseek-harness×Docker 的 r402/r403 测试物料错误地指向示例 MCP 域名，且仍传入家庭代理，零 fixture 请求不能归因于 Provider adapter。修正为公司代理、loopback fixture 和合法 Skill 物料后，r405 Pi×Docker、r406 deepseek-harness×Docker 已真实通过 MCP/Skill/Artifact/事件续读及撤销负向（各 8 requests/1 side effect/1 Skill，撤销后 0 requests/0 side effects）。该结果只推进两个 Docker 子路径，RemoteWorker/Kubernetes 与完整十二格仍开放。
随后 r407 Pi×RemoteWorker、r408 deepseek-harness×RemoteWorker 也真实通过同一 MCP/Skill/Artifact/事件续读及撤销链，并通过 Control Plane 重启与跨节点 takeover；两轮 `capabilityBound=false`，不宣称能力绑定跨节点成功。Kubernetes、Worker/Agent 重启和完整十二格仍开放。
在 OrbStack 单节点 context 下，r409 Pi×Kubernetes、r410 deepseek-harness×Kubernetes 也真实通过 MCP/Skill/Artifact/事件续读、撤销负向和 Control Plane process-restart；两轮 `capabilityBound=false`，未执行 Kubernetes cross-node takeover，故仍不关闭完整十二格。

当前候选复核：r414 Claude×Docker 与 r415 Claude×Kubernetes（OrbStack 单节点）均真实通过 MCP/Skill/Artifact/事件续读、同节点 capability-bound process-restart/reconcile 和撤销负向；r415 不覆盖 Kubernetes cross-node 或 Worker/Agent 重启。r412 Pi recovery 因先撤销 capability 而得到 409/404，r413 Claude Kubernetes 首次复跑在 `file_change=failed` fail closed；两者均不计为 recovery 通过，详见 [06](06-status-tracker.md)。
随后 r417 在显式 Pi recovery 选择下真实完成 Pi×Docker capability-bound process-restart/reconcile（MCP/Skill/Artifact/事件续读、attempt 2 confirmed、RPO 0、撤销负向），不外推到 Pi 的 Worker/Agent、RemoteWorker/Kubernetes 或能力绑定跨节点；deepseek capability-bound recovery、四 Provider Worker/Agent 重启和 Kubernetes cross-node 仍开放。
紧接着 r418 真实完成 deepseek-harness×Docker capability-bound process-restart/reconcile（同一 MCP/Skill/Artifact/事件续读、attempt 2 confirmed、RPO 0、撤销负向）；四 Provider Worker/Agent 重启、RemoteWorker/Kubernetes 能力绑定跨节点和逐格负向仍开放。
Claude Docker Worker+Agent 组合 r422 的两次进程恢复均真实返回 attempt 2/recovered（Worker/Agent 为 `same-node-reconnect`），但后续 pending-side-effect Turn 收到 `provider_unavailable` 并 fail closed，故不计完整组合通过；其日志与清理见 [06](06-status-tracker.md)。
r423 重试在同一公司代理和候选下完整通过 Claude×Docker Worker/Agent capability recovery（MCP/Skill/Artifact/事件续读、Worker/Agent attempt 2/recovered、Control Plane capability-bound process-restart/confirmed、撤销负向和 Compose smoke）；该结果只关闭 Claude×Docker 子路径，不外推到其它环境或完整十二格。
r427 又补齐 Pi×RemoteWorker 同节点 capability-bound recovery：Docker/RemoteWorker MCP、Skill、Artifact、事件续读和撤销负向通过，RemoteWorker `process-restart` 通过；不外推到 Pi 跨节点/Kubernetes Worker/Agent 或完整十二格。
r428 随后补齐 deepseek-harness×RemoteWorker 同节点 capability-bound recovery：Docker/RemoteWorker MCP、Skill、Artifact、事件续读、process-restart 和撤销负向均真实通过；不外推到 deepseek-harness 跨节点/Kubernetes Worker/Agent 或完整十二格。
r429 又补齐 Pi×Kubernetes（OrbStack 单节点）同节点 capability-bound recovery：Docker/Kubernetes MCP、Skill、Artifact、事件续读、process-restart 和撤销负向均真实通过；不外推到 Kubernetes cross-node 或完整十二格。

新增真实证据：`0.0.0-dev.r398` 在 Codex×Docker Worker-only recovery 中通过 Worker 重启后的 MCP fixture 显式重绑定、MCP/Skill、Artifact、事件续读、撤销负向和 Compose 清理；只推进该子路径，完整矩阵继续开放。

## 文档清理与执行计划

这是本项目唯一的当前执行计划：文档收口 → §0 基础设施＋Admin Web → §0.4 Anywhere Runtime/SDK → 完整用户对话。实际状态和下一项只在 [06](06-status-tracker.md) 更新；下表保留已执行阶段的定义，不要求每次重做文档清理或重启已验收的 BASE。

MCP-SKILL-RUNTIME-V1 当前仍按真实矩阵推进：r384 Claude×Docker/RemoteWorker capability 子路径，以及 Codex×Docker/RemoteWorker/Kubernetes 的 MCP/Skill、事件续读、撤销和恢复子路径已真实通过；r387 又以真实候选修复并通过 Codex×Docker Agent Runtime 进程退出恢复（attempt 2、recovered、capability-bound、MCP 40 requests、Skill 1、事件续读和撤销负向）。恢复 helper 的 qualified Skill admission 已修复，Worker/Agent recovery helper 现已参数化 Codex/Claude 并修正 Worker 分支的 reconcile 接缝；恢复 transcript 允许跨 command/request ID 但继续 execution/generation fencing。r413 的 Claude×Kubernetes 首次复跑曾在 Docker `file_change=failed` fail closed，随后 r414 Claude×Docker、r415 Claude×Kubernetes 单节点已重新通过完整子路径；当前仍不覆盖 Kubernetes 跨节点、Worker/Agent 重启、逐格负向和其它 Provider 的 capability-bound recovery。当前 candidate 的 Docker/Kubernetes/RemoteWorker 共享 contract-negative、跨租户拒绝和 stale-generation 1→2/409 已真实通过，但不替代 Provider 正向；Pi/deepseek capability-bound recovery、四 Provider Worker/Agent 重启和完整矩阵仍需真实验收并做 Admin 对账。
Worker-only 诊断目前仅有脱敏 `mcp` 错误类别；Docker `--network container:` 隔离测试证明 owner 重启会使 loopback fixture 暂时不可达、重启 fixture 后恢复，但尚未形成真实 Worker recovery 通过证据，因此不把该实验外推为 Runtime 修复。

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

<a id="mcp-skill-runtime-plan"></a>

### 0.5 MCP-SKILL-RUNTIME-V1 当前实施切片

该切片紧接 ANYWHERE-RUNTIME-V1，复用同一 Control Plane、Runtime、Worker、RemoteWorker、Kubernetes、fencing、幂等、Operation/Audit 和生成 SDK，不新增调度器或任务内核。执行顺序固定为：

1. JSON Schema/OpenAPI/Proto 正式化 MCP Server、Skill Bundle、引用、短期授权、撤销和 capability 事件，并生成 TS/Go SDK。
2. Control Plane catalog、tenant/project 绑定、RLS、版本/digest/幂等/撤销和 Session/Execution 显式引用；Runtime 打开前重新解析并 fail closed。
3. Worker/Foundation Runtime 统一注入 manifest；只允许 Host-managed MCP broker、短期凭据、网络 allowlist 和签名 digest 校验的只读 Skill Bundle，禁止主机任意路径和 Secret 持久化。
4. 逐个验证 Codex、Claude Code、Pi、deepseek-harness 的真实 native/emulated/unsupported 接口；仅凭真实运行结果更新能力目录。
5. Admin 展示 opaque 元数据、绑定和 Operation/Audit；完成 Docker、RemoteWorker、Kubernetes 十二格真实验收与故障矩阵后，才允许在 06 登记支持。

当前未完成项不得以文档、Mock、静态 catalog、build/lint 或手工 CLI 启动替代；Runtime 已提供 loopback broker、operator-owned 短期 materialization FD、签名/digest 校验和只读 Skill Bundle materializer。新增 `bun run runtime:capabilities:generate -- --config ABSOLUTE_JSON --output-directory ABSOLUTE_DIR` 作为 operator-only 物料生成器：配置与源文件必须是受保护普通文件，MCP Token 不走命令行，Skill bundle digest/Ed25519 签名在写入 `<tenant>.capabilities.json` 前校验，并同步写入 `<signingKeyId>.pub`；该工具不读取用户 Workspace 路径。公开 Go CLI `cloud-agentsctl session create` 与 `execution execute` 现在复用生成 OpenAPI DTO 接受 `--mcp-server-refs-json` / `--skill-bundle-refs-json` 数组，直接把显式 opaque 引用提交给现有 Session/Execution API；该入口不改变真实 Provider/环境验收边界。候选 `0.3.0-dev.251` 的实际 Compose smoke 已通过 PostgreSQL/迁移、Capability Admin 列表、Admin/User Web、Worker 启动、Control Plane 重启、过期 grant 负向检查和备份恢复，脚本最终清理容器/卷/网络为零；该无 Provider 凭据运行仍不计作十二格 Provider 通过。Codex adapter 当前使用 app-server 原生 Host-managed `mcp_servers` 指向 loopback broker，并把受管 discovery 结果作为结构化 `SkillUserInput` 发送；原生 MCP/Skill 活动只映射 opaque capability ID，transport/JSON-RPC 失败保留 started 状态并终止 Turn。`0.154.0` 配合所给 `tenant-local 配置中的模型` 的真实 Docker r353 只完成 initialize/initialized/tools/list，没有产生 MCP call、Skill、外部副作用或 Artifact，因此 Codex×Docker 保持 unsupported。Pi `0.85.1` 已在隔离 Docker Runtime 中以真实模型通过签名 Skill 发现/加载和文件副作用子路径，但 MCP 接口实证不存在且完整故障矩阵未通过。真实生产凭据来源、上游版本或环境仍缺失时保持对应格开放。

Claude 的真实 Docker MCP Provider 子路径已在 dirty-source candidate `0.3.0-dev.253` 通过：保留 `settingSources: []`、`strictMcpConfig`、固定不可信内容策略和 fresh durable approval，仅让 `PostToolUse` 保持 MCP 标准 `content[]` 形状，修复通用 provenance 对象触发 Claude Code `e.reduce` 失败的根因。官方 MCP SDK `1.30.0` 服务端返回不可预知 marker，实测 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`、approval、completed event 和 Agent 原样返回 marker 闭环；Runtime/release-manifest digest 分别为 `sha256:1f9a0b81606141d31e436cf32239b5e364ceebae7a2a93a8da85db0ef8f22516` / `sha256:670dbec83b1792e14e99cafa4af92e61d7fe796ae929a121d353e5cfc0509b6d`。该子路径和此前 Claude Skill 子路径仍未经过 Control Plane/Worker、持久事件、撤销、重启、恢复或 RemoteWorker/Kubernetes，不能关闭 Claude×Docker 整格。

2026-09-15 的 Compose 接线切片已补齐 Managed Agent override：Control Plane 与 Worker 都只读挂载同一 operator-owned capability materialization，Worker 显式接收 `--capability-materialization-directory`，签名 Skill 的展开目录使用 UID 1000 的内存 `tmpfs`；验收副本通过既有工具容器设为 UID 1000、`0400`，不放宽为 world-readable。dirty-source candidate `0.3.0-dev.257`（sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`）的 Runtime/deployment/release-manifest digest 分别为 `sha256:1f9a0b81606141d31e436cf32239b5e364ceebae7a2a93a8da85db0ef8f22516`、`sha256:8d62c694c6df65b38e901c6dca11280c62dabcb4b3b2f28884cedd5af0d1ad01`、`sha256:232e33db76e286786178505c8d9bf36d73e79a25c79b51464de47da5a9cab370`。真实命令 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 sh scripts/test-platform-compose.sh <release> <protected-fixture>` 已通过 User/Admin 浏览器 smoke、Capability Admin 创建和重启前置链，但首个 Claude execution 仍以 `runtime_start_failed` 失败，没有 `skill.load`、`mcp.call`、Provider 结果或文件副作用；本轮 fixture 也未启动它引用的 MCP upstream，因此只能记录 fail-closed 负向结果，不能关闭 Claude×Docker 或外推其他格。脚本清理后匹配 Compose 容器、网络、卷、OpenSandbox runtime 和 smoke 目录均为 `0`。

后续同命令在 dirty-source candidate `0.3.0-dev.281` 上已首次走通真实 Control Plane→Worker→Runtime→Claude→Host-managed MCP/Skill：固定 MCP SDK `1.30.0` 的 loopback fixture 与 Worker 共用 Sandbox 网络命名空间，日志只记录 HTTP method/path 和 RPC method，不记录 Header、请求体、Token 或返回内容；单一 execution 取得 `side_effects=1`、`skill=1`、`mcp_requests=8`，并在 `execution.complete` 边界完成事件断线续读（`resumed_events=6`、其后异步 Audit `post_terminal_events=1`）。这证明 Claude×Docker 的 MCP 调用、fresh approval、Skill 加载、文件 Artifact 和持久事件子链，但同轮紧接 follow-up 在前一 Runtime 尚未清理共享 Skill 路径时出现 `runtime_start_failed`，因此仍不关闭整格。根因修复让每个 Runtime 进程在既有受管 `tmpfs` 下使用独立只读 Skill 根；重叠进程回归测试已通过。未发布 candidate `0.3.0-dev.282` 的 Runtime/deployment/release-manifest digest 为 `sha256:64dfa9f780c729ef82b5c2980e1f75775832b76324705f6b91fcf0a5fc9caabc`、`sha256:4ab49ba4b090f44441c656137f7840654b0f9b9652d68348fb284e29770fca47`、`sha256:4bc977a20accedc98ed8f47287709a2515c98103de618ceecdf3a0039e691eaa`；两次重跑均在 Worker image 的外部 `npm install` 构建步骤失败，未进入 Runtime，故该修复尚无新的真实 follow-up 通过证据，其他十一格及撤销/跨租户/旧 generation/恢复矩阵继续开放。

同一切片已核实 deepseek-harness `0.1.2-rc.1` 的实际 MCP/Skill 机制：adapter 以 Cordis patch 注入官方 `@deepseek-ai/dsh-mcp-client` 和 `@deepseek-ai/dsh-skill-filesystem`，只引用 Host 环境中的短期 Token、loopback broker 与签名只读 Skill roots；因 dsh custom roots 不递归，patch 精确挂载受管 bundle 的 `skills/` 子目录，并固定 fail-closed startup、有限重连和关闭默认 Skill roots。定向 3 tests、全仓 typecheck 与 pinned dsh 配置解析通过。随后以本机当前 `packages/cloud-agent-distribution/dist/stdio.mjs`（SHA-256 `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，12,774,391 bytes）和旧依赖承载镜像完成隔离 Docker 预检，固定 `deepseek-v4-pro` 的真实 StartSession/SendTurn 依次触发 MCP `initialize`、`notifications/initialized`、`tools/list`、`tools/call`，并成功加载签名/digest 校验 Skill、执行原生写文件工具和产生 `ArtifactCandidate`；8 条 fixture HTTP/RPC 记录、容器内 Skill 根 EOF 后为空、临时容器/凭据目录/live pointer 精确清理为 `0`，扫描输出无 Provider token/凭据命中。adapter 又按 pinned `dsh-tool-skill` 的真实 `skill({name})` schema 补齐 started/completed 的 opaque capability correlation；单 Bundle 可安全归属，多 Bundle 无公开名称契约时不猜，Control Plane 相应持久化 MCP/Skill succeeded/failed outcome。该事件补丁已有 TS/Go 定向测试，但没有经过真实 Control Plane/Worker；其余预检边界不变。原 `tenant-local 配置中的模型` 路由在无 MCP/Skill baseline 即因 `Missing required parameter: 'input[3].name'` 无 Artifact 失败，因此只记录 adapter 机制与模型路由兼容性边界，不关闭任何 Provider×环境格。`.282` 外部 npm registry `ECONNRESET` 是历史构建失败；registry 已恢复且 `.283` 无 Provider build/smoke 已通过，当前真实 Control Plane/Worker 提升仍等待新的受保护 Provider fixture，十二格边界不变。

同候选的 Kubernetes 部署链也已在任务自有 kind v1.37.0 真实通过：`CLOUD_AGENTS_HELM_CONTEXT=kind-ca-mcp-skill-helm-20260914 sh scripts/test-platform-helm.sh /tmp/cloud-agents-mcp-skill-release.mcp251/release` 报告 schema `99:000001-000099`、Admin/User Web、outbound RemoteWorker、TLS/SSH、CA/服务身份轮换与旧根拒绝、重启、无 Agent/Provider Secret 和 `cleanup=zero`；随后精确删除 kind 集群并确认无测试 context、容器或临时目录残留。Runtime/release/deployment digest 分别为 `sha256:8b4c824256b5d23963690f4ab8178bc4c1c78ee7cc3a1e51c147b704313fc1fd`、`sha256:18899ecfbd78fb626186298f7a31c9b7142d8764657f477f45fc0856491900cc`、`sha256:9960ab2e295d409551e688b9712055ad14eda72823b6bbc38db539fac4dcbd32`；这只证明无 Provider 凭据的 Kubernetes 部署/管理链，不把任何 Provider×环境格登记为通过。

Runtime broker、Skill materializer 与 Provider 共享入口都会在 Provider 启动前拒绝 opaque ID 归一化后发生碰撞的 MCP Token 或 Skill mount 环境变量名，避免两个合法引用在实际注入时落到同一 Host 槽位。
Operator-owned materialization 在 Control Plane 与 Worker 两条读取路径都必须消费完整 JSON 后立即到达 EOF；尾随第二个值按无效物料 fail closed，避免凭据/签名入口只校验首段对象。

2026-09-15 的未发布 dirty-source candidate `0.3.0-dev.283` 已用现有发布脚本生成并完成 22 项 checksum 校验（manifest `sha256:c3af132553b379f8cbdd5a5772f4453ce447f45db73a85c3ddce01e04e0d3e8a`，checksums `sha256:aea3e3a1794c841139c17e00f3fb1cc4c2b6b41d73f9642b1a468f695fca21e2`；Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Deployment `sha256:4ab49ba4b090f44441c656137f7840654b0f9b9652d68348fb284e29770fca47`）。固定 `node@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5` 测试镜像补齐后，`sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r283-candidate` exit 0，完成无 Provider 的 Compose 部署/管理链（浏览器、迁移、Capability/Admin 列表、过期 grant、重启、备份恢复）并精确清理容器/网络/卷/OpenSandbox 为 `0`；本轮没有 Provider 凭据，所以不能替代任何 MCP/Skill 真实格或把 Admin/部署 smoke 写成支持。

同一 `.283` 候选随后使用 `.tmp/r5-creds-local-20260912` 中现有受保护凭据的临时副本和新生成的签名 Skill/MCP fixture，执行 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r283-candidate /tmp/mcp-skill-runtime-v1-20260915-r284-fixture`，真实完成 Claude×Docker 的 Control Plane→Worker→Runtime→Provider 子链：`capability_acceptance=passed`、`mcp_requests=8`、`side_effects=1`、`skill=1`，事件断线续读 `resumed_events=6`、`post_terminal_events=1`，脚本与 follow-up 断言 exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260915-r284-claude-docker.log` 的 SHA-256 为 `sha256:dc542461e9fe99daaa2812cd5c46eca98b7ae3cdc728067884b2ab554022b2b9`；四个 Provider API key、MCP token 均未命中日志，fixture、固定 Node 镜像和 Compose/OpenSandbox 资源精确清理为 `0`，原始用户凭据目录未改动。该证据推进了 Claude×Docker 正向子链并验证独立 Skill 根修复，但不关闭整格或其余十一格；能力专属撤销、不兼容、跨租户、旧 generation、进程重启/恢复和未知副作用对账仍开放。

未发布 candidate `0.3.0-dev.284`（sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`，platform manifest `sha256:12ac46f582aa2c4fa34bb0790b7ef6ad986510c0e8ba3223ed4fff96e9ef6b04`，checksums `sha256:ae74666e5b8b27cafe0b98f2ad0961ce7d2273cb37314be6ab3e27ba6b4021d7`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:1a8dcf547783403cdccfc1fb0c80b9ada1dea739356a6d5ed49dcddbcae8f9db`）的真实命令在本机以固定可用 Node digest `CLOUD_AGENTS_COMPOSE_FOUNDATION_RELEASE_DIGEST=sha256:6dac556d980b7f0e5498d08f08cee0ca67798b4ad6c23964a9214920e67758d0` 运行：正向链仍为 `mcp_requests=8`、`side_effects=1`、`skill=1`、`resumed_events=6`、`post_terminal_events=1`；随后 MCP 与 Skill 都撤销，两个只含 opaque `mcp.revoke`/`skill.revoke`、`capability_revoked` 的 Audit 事件写入，后续执行返回 `CAPABILITY_UNAVAILABLE`，fixture 新增请求和外部副作用均为 `0`，输出 pattern scan、脚本 exit 0、Compose/OpenSandbox 清理均通过。日志 `.tmp/mcp-skill-runtime-v1-20260915-r288-claude-docker-revoke.log` SHA-256 为 `sha256:b31627069e0023fea2d3d44ff29af545d4a4a8fa3e71ed86999c80f322b079d0`；该轮只关闭能力撤销的 Docker 负向子路径，不关闭 Claude×Docker 整格，版本不兼容、跨租户、旧 generation、能力专属重启/恢复和其余十一格仍开放。

未发布 candidate `0.3.0-dev.285`（platform manifest `sha256:124dc87e6deeafd95be9682dc6a8fc11f719c876acb247fc3d2eea8d187d3251`，checksums `sha256:4a6729653979717b36e3577baeb57444de18e0cf1ee0c6af7be5f4aa51f5ce63`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:74f12c483caceb22ec62d8298b08e36814256dcbf680dbfdd4d859caf013d7a6`）新增不依赖 Provider 凭据的真实 Control Plane 负向入口。命令 `CLOUD_AGENTS_COMPOSE_FOUNDATION_RELEASE_DIGEST=sha256:6dac556d980b7f0e5498d08f08cee0ca67798b4ad6c23964a9214920e67758d0 CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r285-contract-negative-candidate` exit 0：MCP 与 Skill 的错误版本在 execution-time 重新授权时均返回 `CAPABILITY_UNAVAILABLE`，分别写入 opaque `mcp.fail`/`skill.fail`、`capability_version_mismatch` 事件，未打开 Runtime；同租户 Token 读取 `tenant-other` 的 MCP 元数据返回 `401`。日志 `.tmp/mcp-skill-runtime-v1-20260915-r290-contract-negative.log` SHA-256 为 `sha256:68f7e7a06b43f7ada97a96e03ed07000fdbb80696c55b8182af3e349902c83f2`，完整 Compose、备份恢复和零残留清理通过。该共享 Control Plane 证据不替代各 Provider/环境格的版本/跨租户验收，也不关闭任何格。

未发布 dirty-source candidate `0.3.0-dev.286`（sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`，platform manifest `sha256:e1615ccc14e1e634e5680b4a135bf6f98b3d9020f20bfc09382dc864625ac17`，checksums `sha256:02a88a200016f15c24fa655481c9dd93497bfaded8c8271459c067e1542f815b`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:175b303ab4985edbcd199271bd8bef040be0c0d4bebf5fc6373d0feb090b796a`）复用同一固定 Node digest 真实运行 `CLOUD_AGENTS_COMPOSE_FOUNDATION_RELEASE_DIGEST=sha256:6dac556d980b7f0e5498d08f08cee0ca67798b4ad6c23964a9214920e67758d0 CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r286-stale-generation-candidate`，exit 0；在停止并推进 Sandbox generation 后，带 MCP/Skill refs 的旧 generation `1` Session 绑定返回 `409 SESSION_CONFLICT`，当前 generation 为 `2`，没有打开 Runtime 或产生副作用。日志 `.tmp/mcp-skill-runtime-v1-20260915-r292-stale-generation.log` SHA-256 为 `sha256:6655117051c6742505d7e610ed05702d8cbb880b6c3f2277be271c1ef5a10d82`，`capability_contract_negative`、旧 generation 拒绝、完整 Compose/备份恢复与零残留清理均通过；这是 Docker 共享 fencing 证据，不替代十二格逐格旧 generation/重启恢复验收，也不关闭任何格。

未发布 dirty-source candidate `0.3.0-dev.287`（sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`，manifest `sha256:8847e33e6c98a1e596de38284d21e2784e566a01847227a2179e337b43d459ca`，checksums `sha256:0826b64fb9ac9c85c9e69a43740fb3204b92310bd3ca828a104f984554e5e46a`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:fdac0d683ce9f2ae890f1be00ba0dd01683f1a0567219e6ebc51c32953896bcb`）的 22 项 checksum 全通过。启用 `CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY=1` 后，同一 contract-negative/stale-generation 协议在 outbound RemoteWorker 与 Kubernetes 真实运行：RemoteWorker 日志 `.tmp/mcp-skill-runtime-v1-20260916-r297-remote-contract-negative.log`（`sha256:8a1eca15f6f4268789a3f870b07e578a0c6a5141a62f0e6993298bd98dc30cf4`）和 Kubernetes 日志 `.tmp/mcp-skill-runtime-v1-20260916-r300-kubernetes-contract-negative.log`（`sha256:89ab5ac743557b04331c249d2f4a89956fccf6e439d632796b09897cc4c055b7`）均输出 `capability_contract_negative=passed`（MCP/Skill incompatible、跨租户 `401`）、`capability_stale_generation=passed`（旧 generation `1`→当前 `2`、`409`）和 `platform Compose smoke passed`；secret scan 无命中，Compose 容器/网络/卷均为 `0`。Kubernetes 使用任务自有 kind v1.37.0 三节点及临时 OpenSandbox controller prerequisite，测试后 operator、CRD、kind cluster、context、kubeconfig 和 Helm 临时目录均精确清理。该证据证明统一 Control Plane/Runtime 合同负向可跨三环境复用，不关闭任何 Provider×环境格，也不替代 Provider 正向调用、撤销、重启/恢复、断连续读和未知副作用对账。

同一 `.287` 制品配合当前 working-tree 验收脚本继续真实运行 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 CLOUD_AGENTS_COMPOSE_REMOTE_RUNTIME=1 CLOUD_AGENTS_COMPOSE_CROSS_NODE_RECOVERY=1 CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER=claudeAgent CLOUD_AGENTS_COMPOSE_CROSS_NODE_ENVIRONMENT=remote-worker sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260915-r287-contract-matrix-candidate PROTECTED_TEMP_DIR`。脚本现从 Admin 投影取得 RemoteWorker Sandbox 的真实 Runtime ID，在该 Runtime 网络命名空间启动同一 MCP fixture，并只在本轮最后启用的环境执行 revoke，避免 Docker 先撤销共享 binding。r304 exit 0：Docker 与 RemoteWorker 均输出 `capability_acceptance=passed`（各 `mcp_requests=8`、`side_effects=1`、`skill=1`）和 `event_stream_resume=passed`（各 `resumed_events=6`、`post_terminal_events=1`）；RemoteWorker 撤销后输出 `capability_revocation_negative=passed runtime_requests=0 side_effects=0 events=2`。日志 `.tmp/mcp-skill-runtime-v1-20260916-r304-claude-remote-worker-revoke.log` SHA-256 为 `sha256:a690eb82d4754bf9b5bf8d0c822e757889e27765cd1c88ba60630289453165cd`，secret scan 无命中，临时物料及 Compose 容器/网络/卷均为 `0`，原凭据源未修改。同轮已有通用 Claude RemoteWorker process-restart 与 cross-node-takeover 恢复（attempt 2、confirmed、RPO 0，后者 RTO 8642 ms），但这些恢复 Turn 没有 MCP/Skill refs；因此只推进 Claude×RemoteWorker 的能力正向、事件续读和撤销子路径，不关闭整格，能力绑定的重启/接管、未知副作用对账及其余格继续开放。

当前源码先生成未发布 dirty-source candidate `0.3.0-dev.288`（manifest `sha256:12a98cef3254572778fd2309b56a3e8a4817a2bfca4adbec07c6a4350cb6b6f8`，checksums `sha256:7ba8f8b81df213e6cfb9c2ff8dd529587f8e98c4460c2d39ce8022af74f5167f`，22 项全通过）。r305 已通过与 r304 相同的 Docker/RemoteWorker capability、续读、撤销及通用两种恢复标记，但恢复后的最终 Sandbox Exec 与 heartbeat 形成 PostgreSQL `40P01`，返回 `INTERNAL_ERROR`，整轮 exit 1；失败日志 `.tmp/mcp-skill-runtime-v1-20260916-r305-claude-remote-worker-revoke.log` SHA-256 为 `sha256:1607a87446380ac301d5aad6b506879de76285c3e327e8102d8344850ba60f28`，清理仍为零。根因修复复用 RemoteWorker command deadlock 判定，只把已整体回滚的 `40P01` 映射到现有 `RESOURCE_CONFLICT`，由调用端以新的一次性授权做有界重试，不在服务端静默重放命令。修复后的未发布 candidate `0.3.0-dev.289`（manifest `sha256:3bf9331ebfdcbf179938d2a5815fd4534950189386d5e5f4376570ea8f3497a0`，checksums `sha256:55c3328c3e80e63ebd6bc6bd77ea7020e57cf484b34ebd32cea11e62fc3d4529`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:5112b0b5e3043d8bdb9220d0cba4e87c2ff5351e47673d0a9331f21cdf1da44a`）22 项 checksum 全通过；r306 以同一命令完整 exit 0，日志 `.tmp/mcp-skill-runtime-v1-20260916-r306-claude-remote-worker-revoke.log` SHA-256 `sha256:b23b01ddcc15add7a9d93bf7783ed12ace3065374f7ee162cb92feabc2983915`，跨节点通用恢复 RTO 10770 ms、RPO 0、snapshot 1496064 bytes，secret scan、临时物料和资源清理均为零。恢复 Turn 仍未绑定 MCP/Skill refs，因此不改变上一段的整格开放边界。

当前源码生成未发布 candidate `0.3.0-dev.290`（manifest `sha256:0854e5fdcc41cedb074ac27afa022e0a57dbe47a8bb4da1011e48b469082ca6b`，checksums `sha256:51ffe57ef7a6e65ed1aadd8e0dca95cdca4433978df803a696ec437490220f33`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3025e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:609fdba0ed6b4d534ded46aebe1ff9b8c7acee7f8a995b58065b5669423b0a9a`）22 项 checksum 全通过；Kubernetes fixture 接线改为按 `opensandbox.io/id` 解析真实 Runtime Pod，在同一 Pod 网络命名空间用 Bun 打包的固定 MCP SDK fixture、stdin 传输 descriptor 后启动，并回收 Pod 内临时文件。命令 `CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME=1 CLOUD_AGENTS_COMPOSE_KUBERNETES_CONTEXT=orbstack CLOUD_AGENTS_COMPOSE_KUBECONFIG=/Users/huang/.kube/config CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY=1 CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260916-r290-k8s-candidate` exit 0，真实 Kubernetes contract-negative/stale-generation 均通过；日志 `.tmp/mcp-skill-runtime-v1-20260916-r290-kubernetes-contract-negative.log` SHA-256 `sha256:15a7dbbc57ddcca19a7ebcfbcd8ab559150ea9e0c56b9d2dac41eb832c99e1a5`，临时 namespace、Pod、Compose 资源和 smoke 目录均为零。由于当前没有可用的受保护 Provider 凭据副本，本轮没有运行 Claude Kubernetes 正向调用，不能把该接线或负向结果写成能力通过；Provider 正向、能力绑定恢复和未知副作用对账仍开放。

当前 working-tree 又把 Claude 的同节点 Control Plane 进程重启恢复接到相同的 MCP/Skill refs：恢复 Turn 在启动时复用 opaque MCP/Skill 引用，先由受管 Skill 驱动一次只读 `acceptance_marker` MCP 调用，再进入带 `sleep` 的文件副作用 checkpoint；重启后的 attempt 2 继续带同一 refs，并在对账前保持 fail-closed，未知结果不得盲目重放。该路径尚未用新的受保护 Provider 凭据实跑，跨节点接管仍保持未覆盖；不能据此关闭任何 Provider×环境格或 Gate。

当前源码的未发布 candidate `0.3.0-dev.291` 已重新构建 22 项制品并通过 `sha256sum -c checksums.sha256`：manifest `sha256:99018730ea4bce5463bebb287db59348e67dccd1977bc52ab30caa7b0f51eeba`、checksums `sha256:4b473f310812899162947f3b61c0ab70ae5499cc50fd761e3d7fdc38c8a3a224`，Runtime `sha256:c26a5a5c49b4fcd6b71be29d8c21acd3021e4b2f2034a78ad5bb9f49f022bb1e`，Control Plane arm64 `sha256:ef7feea01edd8df50fceb89535c68fe2a78b10700d1bc15e24c0f855aeb9d7ac`；仅完成构建/校验和定向测试，未产生新的 Provider 正向或恢复 evidence。

同一 `.291` 候选的无凭据 Compose 回归命令 `CLOUD_AGENTS_COMPOSE_FOUNDATION_RELEASE_DIGEST=sha256:6dac556d980b7f0e5498d08f08cee0ca67798b4ad6c23964a9214920e67758d0 CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260916-r291-capability-recovery-candidate` exit 0；真实输出 `capability_contract_negative=passed`（不兼容 MCP/Skill、跨租户 401）、`capability_stale_generation=passed`（1→2、409）、浏览器 smoke、重启/备份恢复和 `platform Compose smoke passed`，日志 `.tmp/mcp-skill-runtime-v1-20260916-r307-capability-recovery-contract-negative.log` SHA-256 `sha256:e4457a49bf4f6c0ca5ae921981f00e036e0f96391af76e3c8107ece8071eef39`，Compose 资源与 smoke 目录均为零；该证据不替代 Provider 正向或 capability-bound recovery。

同一 `.291` 候选在显式 `orbstack` Kubernetes context 下再次真实运行 contract-only 入口：Docker 与 Kubernetes 均输出 `capability_contract_negative=passed`、`capability_stale_generation=passed`，命令 exit 0；日志 `.tmp/mcp-skill-runtime-v1-20260916-r308-capability-recovery-kubernetes-contract-negative.log` SHA-256 `sha256:e6a48b9ae09b8ac0e8e5f8564681aab7cd2d849cec5c57623a0ce12729b4e555`，Compose、kind/OpenSandbox、Pod/namespace 和 smoke 临时物料均精确清理为零。该结果仍只覆盖共享负向/fencing，不关闭 Kubernetes Provider 正向或 capability-bound recovery。

当前 Codex adapter 又以 registry pinned `@openai/codex@0.150.1` 的真实 app-server 协议核对 Skill：`codex app-server generate-json-schema` 生成的 v2 schema 明确提供 `skills/extraRoots/set`、`skills/list` 及 `turn/start.input` 的 `type=skill` 形状；实际 initialize → `skills/extraRoots/set` → `skills/list` RPC 在受控 `HOME`/`CODEX_HOME` 下发现 Runtime-mounted Skill，未再加载用户主机 `/Users/huang/.agents/skills`。实现只传递 Runtime `skills/` 根，强制 discovery，malformed/空结果 fail closed，并在 GenerateText 拒绝 MCP/Skill；由于公开 Bundle 引用没有单个 Skill name/path selector，未把 Bundle 内全部指令盲目注入每个 turn。Codex package 58 tests、typecheck、oxfmt 和 `git diff --check` 通过；这是真实协议/隔离证据，不是 Provider 模型正向或十二格支持证据。

未发布 dirty-source candidate `0.3.0-dev.292`（sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`，manifest `sha256:ef0d9bab0288d02dde870e3865e3f3137bc8ba89e974c60d950417dfe5cad076`，checksums `sha256:819416972502f1c8218ccdec0be84b4d8a0808082df2979c3bf21e952487706b`，Runtime `sha256:5de654a71b9e52b0762e9f4a5d9c235548fbfff2f497a3720c45a8edb58b10ab`，Control Plane arm64 `sha256:c0578bf9b1343076fa41d31ce11d0bb288930e734405b89ebd19e931312749f8`）22 项 checksum 全通过；无 Provider 凭据，未运行 Codex 模型正向、能力绑定恢复或新的十二格验收，现有 supported cell 数量不变。

随后只收紧 Codex Skill 路径白名单：system Skill 也必须位于受控 `CODEX_HOME`，外部绝对路径即使自报 `scope=system` 也拒绝；58 tests/typecheck/oxfmt/diff check 仍通过。与最终源码一致的未发布 candidate `0.3.0-dev.293`（manifest `sha256:6d2e62174420da4083575cd6215009942d5c28ea79d054e6c9d320788d786385`，checksums `sha256:f7427791180c818b9c66a87ce10c0c60453dbc831324b0ceb7a32e46de7dc3f3`，Runtime `sha256:2d942bb1790116d3dbe49b41d29960ceafde009a544e47053dea197e90c3182e`，Control Plane arm64 `sha256:a7c01dee5bf5da07297871184b92898cecf993741aa68ba685139f6c3a6c6f37`）22 项 `sha256sum -c checksums.sha256` 通过；仍无 Provider 凭据，未新增 supported cell。

Codex 事件映射继续补齐：MCP dynamic tool 的 `item.started/completed` 现在输出 canonical `mcp_tool_call`、opaque `capabilityResourceId`，单一 Skill Bundle 的 `skill` item 输出 opaque Bundle ID 和 `sourceItemType=skill`；多 Bundle 没有公开名称映射时不猜。Codex 58 tests、typecheck、格式和 diff check 通过。最终未发布 candidate `0.3.0-dev.294`（manifest `sha256:6565fd94667d960e7c1399e0116340f81ba82a2153ac37abbde66ab07b8e2966`，checksums `sha256:105d299b1d57c3e13ea8dd753fa0279f594e89db475fec643ec690b0053e9ba1`，Runtime `sha256:0aff32da78608c662bb3f2846de0af67d8dd85fb918f2452a309ef9b79959d33`，Control Plane arm64 `sha256:d733bf9e1d3ba21a4fe1c74f064199f61375240a65bcfe479131a903e1fa857a`）22 项 checksum 全通过；没有 Provider 正向凭据，未新增 supported cell。

Codex MCP transport/JSON-RPC 失败现在不向模型返回可重试的伪造 tool result，而是立即终止 Provider Turn，保留已发出的 started event 供现有 checkpoint/reconcile 进入未知副作用路径；502 回归验证第二次运行没有 completed event，禁止盲目重放。最终未发布 candidate `0.3.0-dev.295`（manifest `sha256:fb7fde91f966d01279e6325f360a8709962f0340370ac5dd40656fc3d16a730`，checksums `sha256:f1b5d517f7b26f2a903897d604888195ae86d2cc167d23a539aeabfba86e52e6`，Runtime `sha256:3eb6d09ddc0786d52834ae55cb5a2a7b486429e55802af0b87a84cfa041e3078`，Control Plane arm64 `sha256:3bb7c54765708b40197f27944f3d7e82119963c7f5a136a63f09cd6c52f06114`）22 项 checksum 通过；无 Provider 正向凭据，supported cell 不变。

### 新 Goal 提示词：MCP-SKILL-RUNTIME-V1

```text
创建并持续推进 Goal：MCP-SKILL-RUNTIME-V1。
项目目录：/Users/huang/devel/project/huang/business/cloud-agents；分支：codex/cloud-agents-platform-p0。
先读取 CLAUDE.md、docs/plan/cloud-agents-platform/01-product-scope-and-authority.md、02-target-architecture.md、03-public-repository-and-release.md、04-extraction-and-migration.md、05-gates-and-acceptance.md、06-status-tracker.md、07-admin-web-requirements-and-design.md。
目标是让 Codex、Claude Code、Pi、deepseek-harness 通过统一 Control Plane、Runtime 和公开 TypeScript/Go SDK，在 Docker、outbound RemoteWorker、Kubernetes 中安全使用 MCP Server 和 Skill Bundle：复用现有生成链、fencing、Operation/Audit、幂等和副作用对账；MCP 使用短期授权、最小权限、网络 allowlist 和撤销检查；Skill 使用签名/digest 校验后的只读 Bundle；不读取用户主机任意路径，不把 Secret 写入 Snapshot、日志、Event 或 Artifact。
按“contracts/生成 SDK → Control Plane/DB → Runtime 注入 → 四 Provider adapter → Admin/十二格真实验收”推进，不停在方案或文档待办；未经真实运行不要把 capability 标为 supported。不得 push、发布镜像/npm/Release、操作生产、迁移业务卷、删除无关 dirty work 或关闭正式 Gate。
```

Candidate `0.3.0-dev.301` 修复 Pi native cursor 失效后 authoritative-history fallback 丢失 Skill Bundle roots 的问题：两次 session factory 调用都保留同一受管 Skill root，避免恢复 attempt 2 静默退回无 Skill 会话。Pi 3 tests、Runtime broker/materializer/stdio 与 Provider API 91 tests、定向 Provider/SDK 54 tests、Pi typecheck、TS/Go 生成检查、Go test/vet、格式、diff check、HTML parser 与 22 项 checksum 均通过；候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r318-pi-resume-skill-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:3686c8720239d89e3439eed5db2a07f83d0ba6c462bcf5581f2510c277c44107`、`sha256:5185eaa86b01894593c6de99aea5b7be3138d11326db3b71b4e28a824604afe9`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:352d2f7db4e89b4a9439827309b900854c79232e646577c06581f177b66f90e9`；无受保护 Provider 凭据，不新增 supported cell。

同一 `.301` 候选运行 `CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260916-r318-pi-resume-skill-candidate` exit 0；真实通过 Admin/User browser smoke、`capability_contract_negative=passed`、`capability_stale_generation=passed`、Control Plane restart/backup restore 与 Compose smoke，Docker 容器/网络/卷为零。该无凭据证据不新增 Provider×环境 supported cell。

Candidate `0.3.0-dev.302` 修正 Control Plane capability outcome/pending audit 的 identity 归属：MCP Server 与 Skill Bundle 即使复用相同 opaque ID，也按 `(resource kind, resource ID)` 复合键分别记录成功与 `capability_call_unknown`，不再丢失或错归因。Managed Agent/Server/Store/Worker Go tests、`go vet`、Runtime/Provider/SDK 145 Vitest、TS/Go 生成检查、格式、diff check 与 22 项 checksum 通过；候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r319-capability-kind-identity-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:a6aa821b2df4bfd07fe0e925c1e59ef88332d676b249174206f18c1133367d9f`、`sha256:eaceb025f5ef8b86d150e8b06624e622d9e7550db0330fe7753894b0f1d0cf80`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:e8dc9490aa7289c3385ff6b6feefe5464d967bb8650855bb13c943c83da82d06`；无受保护 Provider 凭据，不新增 supported cell。

Candidate `0.3.0-dev.303` 将 pending capability 审计键补为 `(resource kind, capabilityResourceId, providerItemId)`：当 MCP 与 Skill 同时复用 opaque ID 及 provider item ID 时，Skill 的完成事件不再清掉 MCP 的 started 状态，`capability_call_unknown` 仍能准确落到 MCP binding。Managed Agent/Server/Store/Worker Go tests、`go vet`、Runtime/Provider/SDK 145 Vitest、TS/Go 生成检查、格式、diff check 与 22 项 checksum 通过；候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r320-pending-kind-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:c560f688412207ed57224af4461e5cc5a2d3fa155542de39e9d7fc26bfd199cd`、`sha256:d944cccd78f279b37ab5c2f880e6518272c30e8364161fdfeb49709755d2470e`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:9a13eae5ff4df27aedc963b76fc88fa64e763536abb67796d2d1b8635e767468`；无受保护 Provider 凭据，不新增 supported cell。

同一 `.303` 候选运行 `CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260916-r320-pending-kind-candidate` exit 0；真实通过 Admin/User browser smoke、过期 grant、`capability_contract_negative`、`capability_stale_generation`、Control Plane restart/backup restore 与 `platform Compose smoke passed`，日志对应 Compose 资源清理后 containers/volumes/networks 均为 `0`。无受保护 Provider 凭据，不新增 supported cell。

Candidate `0.3.0-dev.305` 补齐能力绑定恢复的协议级回归：幂等重试从已持久化的 `ExecutionRunning` 读取并校验同一显式 MCP/Skill refs，在 checkpoint 存在且 claim 可接管时重新解析 binding，并把同一 manifest digest、MCP binding 和只读 Skill binding 重新送入 Runtime open，随后完成 attempt；生产 SQL 先校验包含 capability refs 的 mutation digest，再返回已持久化 pin。该检查复用现有 Durable Runtime、claim、fencing 和 Worker 协议 fake，不是 Provider/环境真实验收，不能关闭“能力绑定重启/恢复”或十二格。Managed Agent/Server/Store/Worker Go tests、`go vet` 与 22 项 checksum 通过；候选目录 `.tmp/mcp-skill-runtime-v1-20260915-r322-recovery-capabilities-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:a5bfab9be34edbdc3245b86b930bca0a7e767a1ad6b11e388018776c8bd41739`、`sha256:b823fa14ca45c26760285e44d4d4121fdf27529645dcc6c54624b013d9a5799b`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:d2a7fdcb511b9b6267f3a933a84d13da8a6176c678f8b7b0fb5b6baed2553666`；无受保护 Provider 凭据，不新增 supported cell。

Candidate `0.3.0-dev.306` 增加 PostgreSQL 生命周期核对：`ExecutionCreateMutationDigest` 对无 capability refs 与带 MCP/Skill refs 的同一请求生成不同 digest，确保 `create_managed_agent_execution_v2` 复用既有幂等冲突而不会接受不一致 pin；Store/Managed Agent Go tests、`go vet`、格式、diff check 与 `.306` 的 22 项 checksum 通过。候选目录 `.tmp/mcp-skill-runtime-v1-20260915-r323-capability-recovery-digest-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:3f3d61f038924bf78e8ae1fb6c58b9da1f15a184f5292286a463428c103eb3f1`、`sha256:1a990ab5b22f61f0d7a37be205066aa1daf7e0bda5807c1141aa6db5f573a1fa`、`sha256:0ca286c105545f07f4f8af888b3b0c0c467bc1ef7d837383bd372e0a5f78cce5`、`sha256:162c9d75337c2afd8c20f34f32501c7de6ca453c58451ad769ac124f91bf1a46`；无受保护 Provider 凭据，不新增 supported cell。

Candidate `0.3.0-dev.307` 补齐 Pi Skill 事件接缝：固定 `@earendil-works/pi-coding-agent@0.85.1` 在受管 Skill session 成功建立后发出不含路径/源码的 started/completed activity，canonical Runtime v2 归一为 `dynamic_tool_call`、`sourceItemType=skill`、`supportMode=native` 和 opaque `capabilityResourceId`，Control Plane 可按既有 `skill.load` outcome 归账；Pi 的 MCP 接口仍经实证不存在并保持 fail closed。Provider API/Pi 100 Vitest、Pi/Provider API typecheck、格式、diff check 与 22 项 checksum 通过；候选目录 `.tmp/mcp-skill-runtime-v1-20260915-r324-pi-skill-event-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:c0316def7d041c699ebd98652b9ffbb540910ade264e7d8e34bb068bead599e7`、`sha256:ea523d0d6bd8233277bf4f67c6f3a669efa4edcfb9044daaaa5191c13ff1d969`、`sha256:080cb8ecfc42d7b8e7319e7655e4050f391e0049fb971306a1765e4a67a28789`、`sha256:32fcc7f1c48900aa735ea6ae91a72cda199a7698a56602c371cac0437e4dcfac`；无受保护 Provider 凭据，不新增 supported cell。

Candidate `0.3.0-dev.309` 修复真实恢复链暴露的共享持久化缺口：PostgreSQL 的 start/settle/cancel/interrupt transition 过去保留数据库中的 MCP/Skill refs，却没有在即时 Execution 响应中带回；现在四条 transition 统一从既有 authoritative projection 补回 refs，并校验 Execution ID、generation、state、resource version 后返回，不新增第二套状态或恢复内核。`GOTOOLCHAIN=go1.26.6+auto go test ./services/control-plane/internal/store/postgres ./services/control-plane/internal/managedagent` 通过。候选目录 `.tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate`，sourceCommit `209d309417597071c23d66857d8169a4e6e2c1db`、sourceDirty=true；manifest/checksums/Runtime/Control Plane arm64/Worker arm64/Deployment/TS SDK/Go SDK digest 分别为 `sha256:6d407d0b0403d3c6e1ea87ecaff6fbf549af1d5308d3accab630c2d555e209d5`、`sha256:24d071037cef1969f42195d5b27828dbed4e77b544e37b55c10be887d6e60164`、`sha256:080cb8ecfc42d7b8e7319e7655e4050f391e0049fb971306a1765e4a67a28789`、`sha256:d71dbb983a7f967a8e2cf86c8afe8996275aafc4876c11fe23b1ec9e6bce023d`、`sha256:4a6bddbeb39882c138026d548bc377d28058f03ece0835ac3a4017362e8616b4`、`sha256:4ab49ba4b090f44441c656137f7840654b0f9b9652d68348fb284e29770fca47`、`sha256:da3f3def05b2a4c0d0e95c129a3bf2da8bb03f1a4c3cbe793e39ca1793032d39`、`sha256:06fb5d735c3012c84e7a9bf7489bdc726c5df948d988b0fedb6e5c10fc2e75b3`；22 项 `sha256sum -c checksums.sha256` 全通过，未发布。

同一 `.309` 候选使用受保护 Claude 凭据副本与新生成的签名 Skill/MCP fixture，执行 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST=1 CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate <protected-fixture-dir>`，真实 exit 0。结果包含 `capability_acceptance=passed`（`mcp_requests=8`、`side_effects=1`、`skill=1`）、`event_stream_resume=passed`（`resumed_events=6`、`post_terminal_events=1`）、Control Plane SIGKILL 后 `capabilityBound=true`/attempt 2/`recoveryState=recovered`/`sideEffectOutcome=confirmed`/RPO 0，以及撤销后的 `runtime_requests=0 side_effects=0 events=2`。日志 `.tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate/compose-real-claude-capability-attempt3.log` SHA-256 为 `sha256:ba9b32fc4d4e34530ca099441056c52fe0cb6910381add73573f9574510938a8`；全部候选日志的 Provider credential/MCP token 命中均为 0，MCP 返回 marker 未进入最终日志，原凭据 SHA-256 保持 `fd860e13d3d1bf2592cbda1c716c5908c64777754410fb6efd864ca37986aa43`。Compose 容器/网络/卷、fixture、诊断目录和诊断镜像均精确清理为 0。首次两轮构建因 `deb.debian.org` 连接失败与 npm `ECONNRESET` 停在 Worker image build；聚焦日志确认是宿主代理指向容器内 `127.0.0.1`，仅把一次诊断构建代理映射为 `host.docker.internal` 预热相同不可变层，未修改 Dockerfile 或发布镜像。该证据推进 Claude×Docker 的正向、事件续读、Control Plane 进程恢复和撤销子路径；Worker/Agent 重启、逐 Provider 版本/租户/generation、RemoteWorker/Kubernetes 及其他 Provider 仍未闭合，十二格与正式 Gate 保持开放。

同一 `.309` 候选随后执行 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST=1 CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 CLOUD_AGENTS_COMPOSE_REMOTE_RUNTIME=1 CLOUD_AGENTS_COMPOSE_CROSS_NODE_RECOVERY=1 CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER=claudeAgent CLOUD_AGENTS_COMPOSE_CROSS_NODE_ENVIRONMENT=remote-worker sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate <protected-fixture-dir>`。持久日志最后一行是脚本末尾的 `platform Compose smoke passed`，因此脚本本身 exit 0；外层日志捕获命令在脚本返回后误用 zsh 只读变量 `status` 的返回码不计入被测脚本。Claude×RemoteWorker 真实输出 `capability_acceptance=passed mcp_requests=8 side_effects=1 skill=1`、`event_stream_resume=passed resumed_events=6 post_terminal_events=1`、Control Plane SIGKILL 后 `capabilityBound=true`/attempt 2/`process-restart`/`sideEffectOutcome=confirmed`/RPO 0，以及 `capability_revocation_negative=passed runtime_requests=0 side_effects=0 events=2`。同轮普通跨节点样例为 `capabilityBound=false`，RTO `12914 ms`、RPO `0`、snapshot `1546240` bytes，只保留既有 Runtime 恢复证据，不能冒充能力绑定跨节点接管。日志 `.tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate/compose-real-claude-remote-worker-capability-r328.log` SHA-256 为 `sha256:261aad9ee72bdf04bca61bc94e06ca31a62ec5ff5eae968ad27d4c56e4dc8dc9`；Provider credential value、MCP token、MCP 返回 marker 与 MCP tool name 命中均为 `0`，原凭据 SHA-256 仍为 `fd860e13d3d1bf2592cbda1c716c5908c64777754410fb6efd864ca37986aa43`。Compose 容器/网络/卷、RemoteWorker 测试资源、fixture 与 token guard 均清理为 `0`。该证据只补齐 Claude×RemoteWorker 的 Control Plane capability-bound process-restart 子路径；Worker/Agent 重启、能力绑定跨节点接管、逐格版本/租户/generation、Kubernetes 与其他 Provider 仍开放。

同一 `.309` 候选又在本机 `orbstack` context（Kubernetes `v1.35.6+orb1`，OpenSandbox CRD/manager `0.2.0`）执行 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent CLOUD_AGENTS_COMPOSE_REAL_PROVIDER_TEST=1 CLOUD_AGENTS_COMPOSE_CAPABILITY_ACCEPTANCE=1 CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME=1 CLOUD_AGENTS_COMPOSE_KUBERNETES_CONTEXT=orbstack CLOUD_AGENTS_COMPOSE_KUBECONFIG=/Users/huang/.kube/config sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate <protected-fixture-dir>`，脚本 exit 0。Claude×Kubernetes 真实输出 `capability_acceptance=passed mcp_requests=8 side_effects=1 skill=1`、`event_stream_resume=passed resumed_events=6 post_terminal_events=1`、Control Plane SIGKILL 后 `capabilityBound=true`/attempt 2/`process-restart`/`recovered`/`confirmed`/RPO 0，以及 `capability_revocation_negative=passed runtime_requests=0 side_effects=0 events=2`。日志 `.tmp/mcp-skill-runtime-v1-20260917-r326-recovery-refs-candidate/compose-real-claude-kubernetes-capability-r329.log` SHA-256 为 `sha256:d6c65ef24d4289a27f4f878227e6732580564720c50c21a259aafb4fd902c539`；credential value、MCP token、返回 marker/tool name 命中均为 `0`，原凭据 SHA-256 未变。Compose 容器/网络/卷、Kubernetes namespace/ClusterRole/ClusterRoleBinding、Pod 内 fixture 和本地受保护 fixture 均清理为 `0`。本轮没有启用 Kubernetes cross-node，且仍未覆盖 Worker/Agent 重启、逐格版本/租户/generation/fail-closed 组合或其他 Provider；因此只登记 Claude×Kubernetes 的 Control Plane capability-bound process-restart 子路径，不关闭整格、十二格或正式 Gate。

Candidate `0.3.0-dev.353` 以最小改动复用 Codex app-server 原生协议：Host-managed MCP 直接写入受控 config override，Skill 使用已验证 mount 的结构化 `SkillUserInput`，不再维护 dynamic bridge；48 个定向 tests、typecheck、格式、shell syntax、diff check 和 22 项制品 checksum 通过。真实 Docker 命令、候选与完整 digest 见 06；日志 `.tmp/mcp-skill-runtime-v1-20260918-r353-candidate/compose-real-codex-docker-r353.log`（`sha256:dc43706b5576c43b75cbdfe9fb5e577d649701ae4325c976dffc936281105c72`）exit 1，fixture 只有 initialize/initialized/tools/list，没有 MCP call、Skill、外部副作用或 Artifact，清理为零。该失败保持 fail-closed 并固定当前模型路由兼容性边界，不新增 supported cell；下一切片仍需真实解决 Codex 模型调用，并完成 Pi/deepseek 的 Control Plane/Worker 和十二格故障矩阵。

Candidate `0.3.0-dev.354` 仅修复 Codex app-server 在 `turn/start` 响应后同一事件循环立即发起 Host 工具调用时的 `turnId` 竞态：Runtime 在解析 JSON-RPC 响应前登记 `turnId`，不改变 MCP/Skill 权限或动态 Workspace 工具边界。由当前 dirty source 重新生成的本地候选使用命令 `GOTOOLCHAIN=go1.26.6+auto CLOUD_AGENTS_GO=go CLOUD_AGENTS_NODE=/Users/huang/devel/soft/nvm/versions/node/v24.18.1/bin/node bun scripts/cloud-agents-platform-release.ts --version 0.3.0-dev.354 --output-dir .tmp/mcp-skill-runtime-v1-20260918-r354-candidate --allow-dirty`；候选 manifest/checksums/Runtime/contracts/Control Plane arm64/Worker arm64/Deployment/TS SDK/Go SDK SHA-256 分别为 `sha256:09c7f97918cdecb525572eb3f6f2506eb2cde31e80803dec8806e8e64d10cac9`、`sha256:1cc3081f9f709754f155534bf36eda2caae8a48681270b11213a6d8dbd1ab79e`、`sha256:1f31062b58d11bba8634e063ab2f4134e419a375397aa150a622b7631c3f7580`、`sha256:ccc9f34eedc2df1d4092e65fa43ec7365277a8fa12e79530c26144205ace9de3`、`sha256:3172ef5a820630625f12ba3425b1ee06e8a7985a9e1a2297b18315f402ee34e0`、`sha256:5426ee1553f41abf6663e6af0cb43a1f1a01007fbb36adbfd4cd4e89c490de84`、`sha256:aa1933cab1df3e9ef16ce835df6f0e3748d235cd5a7e72883d2a1c1eec729c23`、`sha256:b1d6ad94c22910ec62c1fa3dcd1c724ac5c5d286b3569c78a1723c0b21e51363`、`sha256:897cd6a2edb0fcbae49597ae25f849cc8beb58ea9cadc04a554c4831a02127b0`、`sha256:06fb5d735c3012c84e7a9bf7489bdc726c5df948d988b0fedb6e5c10fc2e75b3`，`shasum -a 256 -c checksums.sha256` 的 22 项均为 OK。该候选未发布；真实 Codex Docker 结果仍以 `.353` 的 discovery-only/exit-1 日志为准，不新增 supported cell，十二格与正式 Gate 继续开放。

`.354` 的一次真实复跑在 Worker image build 阶段因 Docker 容器访问 `deb.debian.org` 的外部网络连接失败而 exit 1，未到达 Provider；日志 `.tmp/mcp-skill-runtime-v1-20260918-r354-candidate/compose-real-codex-docker-r355.log` 的 SHA-256 为 `sha256:53227ff4f88835d2f370c7402a918b89f60202dc358f331ffc8c110f63fdcb65`，同一失败由最小 Debian 容器复现，故不计为产品回归或 Codex 运行证据。测试脚本随后把受管 Skill 的验收动作改为 Host-owned `workspace.write_text_file` 一次，消除与 fixture 对“禁止 shell”的冲突；该改动不扩大 Runtime 权限。资源、受保护凭据和十二格/Gate 边界保持不变。

通过用户提供的局域网代理仅预热固定 Worker 依赖后，`.354` 的 Codex Docker 真实链已越过构建并启动 Control Plane、Worker、Runtime 和 browser smoke；日志 `.tmp/mcp-skill-runtime-v1-20260918-r354-candidate/compose-real-codex-docker-r356.log` digest 为 `sha256:37f1301415812a6a3716ae559c6ad8bf38df20e5d2a8e272573f429fea4fb5e2`。真实 Codex execution 只触发 MCP `initialize`/`notifications/initialized`/`tools/list`，没有 `tools/call`、Skill、Artifact 或副作用，脚本按 fail-closed exit 1；这确认当前模型路由仍不实际使用 Host-managed 能力，不关闭 Codex×Docker 或其它格。代理变量、凭据副本和 Compose 资源均未进入制品并已清理。

同一 `.302` 候选运行 `CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES=1 CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=claudeAgent sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260916-r319-capability-kind-identity-candidate` exit 0；真实通过 Admin/User browser smoke、过期 grant、`capability_contract_negative=passed`、`capability_stale_generation=passed`、Control Plane restart/backup restore 与 Compose smoke，容器/网络/卷均为零。该无凭据证据不新增 Provider×环境 supported cell。

### Claude qualified Skill 关联与共享挂载安全（2026-09-19）

未发布 candidate `0.3.0-dev.384` 已在复用既有 Runtime/Controller/fencing 的前提下完成 Claude×Docker MCP/Skill 子路径：Bundle provenance 根闭集拒绝 Claude 自动加载旁路组件，Landlock bootstrap 提供校验后只读挂载，Host admission 只接受 qualified Skill；真实 MCP/Skill/Artifact、事件续读、Control Plane SIGKILL 后同节点恢复和撤销负向均通过。此前恢复脚本的 unqualified Skill 提示词已修正为 qualified 名称；Compose cleanup 也从全局 baseline 差集改为本次 Runtime ID 精确归属和 server 标签卷核验。仍未把该子路径扩展为完整整格或十二格：RemoteWorker/Kubernetes、其它 Provider、Worker/Agent 重启、跨节点能力接管、逐项版本/租户/generation/未知副作用矩阵继续开放。版本、digest、命令、日志和清理见 [06](06-status-tracker.md)；没有新增调度器、迁移旧卷、生产操作、提交、发布或正式 Gate 关闭。

## 1. 兼容、迁移与回滚的按需入口

已有 Agent/Lease 调用方继续兼容；Workspace 数据归属和旧卷采用不能由文档更名隐式改变。实际涉及旧数据迁移、消费者 cutover 或破坏性回收时，读取 [旧迁移/回滚安全要求](history/04-legacy-migration-plan.md)，核验授权、恢复点、N/N-1 与单一 writer，再执行该范围任务。不要为了新的 BASE 工作重新运行旧 P0 inventory 或提前实施 Synara/T3。

Candidate `0.3.0-dev.297` 补齐 Control Plane capability failure audit：Provider Turn 失败时，已完成的 MCP/Skill item 仍写入成功/失败 outcome；已 started 但没有 completed 的 capability 写入 opaque `mcp.fail`/`skill.fail`，错误码为 `capability_call_unknown`，只保存脱敏事件摘要和 started-event digest，继续由 checkpoint/reconcile 决定未知副作用，不把未开始的 binding 误标为失败。Managed Agent 定向 Go tests、`go vet`、格式和 `git diff --check` 通过。候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r314-capability-failure-audit-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:6c029f21b2b462c9ead9e26520a82e4943ef2d4d341805b1c5ebddececc1f082`、`sha256:0c225326de83ae40d70f212d26e227c0ada6d462b0882393e040cf782ffe5d48`、`sha256:535dcd05674b510e51aa7e780047bbcbab59a83283905b8ed02d5c94d3c8b758`、`sha256:372253b80c511e342f9d128c211503e90b2fe53caba259d04de68909a4dce00e`，22 项 checksum 通过；无受保护 Provider 凭据，十二格与 Gate 状态不变。

Candidate `0.3.0-dev.296` 收口本轮 Codex Host-managed capability 边界：MCP namespace 归一化碰撞现在 fail closed，避免两个 opaque 引用落到同一工具槽位；MCP transport/JSON-RPC 失败终止当前 Provider Turn、保留 started event 交给 checkpoint/reconcile，不返回可盲目重放的伪造结果；事件归因继续只使用 opaque `capabilityResourceId`。58 个 Codex tests、typecheck、格式和 `git diff --check` 通过。候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r313-codex-final-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:8433198e69af58211549b1da694f58a367426b3383d39d7a50ad385876280c09`、`sha256:4569d088a836ad7643190055fd1fdef29b047093926e11f23ee37bc94955fdf6`、`sha256:535dcd05674b510e51aa7e780047bbcbab59a83283905b8ed02d5c94d3c8b758`、`sha256:95358ea62725becd10a811d6259a0e5044e52bd18c3846485e7e9fcaeb0d3814`，22 项 checksum 通过；本轮无受保护 Provider 凭据，十二格与 Gate 状态不变。

Candidate `0.3.0-dev.298` 修正失败审计的 item identity：pending MCP/Skill 以 `capabilityResourceId` 与 provider item ID 复合键跟踪，避免不同 capability 复用 provider item ID 时错误完成或错误失败；Managed Agent tests、`go vet`、格式和 22 项 checksum 通过。候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r315-capability-failure-audit-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:5e0fd377624d77a6c9ac549ba5ce31ae6857765049a9297e138cf4791f81edff`、`sha256:7044f7395a1c27148eb126b5e74c567fa24095e3a12e025b26e73429fc37ed81`、`sha256:535dcd05674b510e51aa7e780047bbcbab59a83283905b8ed02d5c94d3c8b758`、`sha256:6c31072cb9778c7dbc5f9cf59645a1181342a21d09c90fe3f4bee8bf35e52b51`；无受保护 Provider 凭据，十二格与 Gate 状态不变。

Candidate `0.3.0-dev.299` 收紧 pinned deepseek-harness `0.1.2-rc.1` 的 tool-result 失败语义：`tool/result` 携带 `isError`、error status 或 error object 时输出 failed activity 并终止 Provider Turn，不把 MCP 不可用伪装成 completed；4 个 deepseek tests、typecheck、格式和 22 项 checksum 通过。候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r316-deepseek-tool-failure-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:67c78ca498a92fdb34817e043ed1f2affef99564e5a03e1703a00c413b4fddff`、`sha256:9b92ba070b414036e2c00112ace05a0c163dc0f7ca92bfb62aa2549462ea8b63`、`sha256:61d4c277411c0df02166252d71d98204e239e01805e85b9500ff55d4f7621707`、`sha256:c1b96dcb4d55a610bb8e73422cda044a96cc130958abb98ac5a4a72a69012947`；无受保护 Provider 凭据，十二格与 Gate 状态不变。

Candidate `0.3.0-dev.300` 进一步保证 deepseek tool-result failure 立即中止 Harness 并忽略后续通知，避免 MCP 不可用后继续接受新的工具活动；4 个 deepseek tests、typecheck、格式和 22 项 checksum 通过。候选目录 `.tmp/mcp-skill-runtime-v1-20260916-r317-deepseek-tool-abort-candidate`，manifest/checksums/Runtime/Control Plane arm64 digest 分别为 `sha256:ae40041c7188bd9531940ac3af721f0a92ce935ac7cc8624db0ac0685204b883`、`sha256:5515086d584d334dd3afbbf465dc1b1409589692fed4db3bc7b9737f1cb59c8d`、`sha256:747aa7061b53d774f2a7133a07d62a72e5962ffd419563d42ee668fefac900be`、`sha256:d74815dc0716db225943d5880d2ceb41fd45d321d386694a906e2743f84edbc1`；无受保护 Provider 凭据，十二格与 Gate 状态不变。

2026-09-19 执行切片记录：Codex Host-managed MCP/Skill 在 Docker 的真实 acceptance 已通过（candidate `0.3.0-dev.373`，Codex `0.154.0`，MCP 1 次副作用、Skill 1 次加载、事件续读、撤销负向、Compose/备份恢复/清理通过）。验收 helper 使用 `approval-required`，以兼容完整 Host Hook 对 `full-access`/`bypassPermissions` 的外部 MCP fail-closed 规则；未改变 Runtime 的权限边界。此切片只推进 Docker 正向子路径，下一项仍是 Worker/Agent 重启、RemoteWorker/Kubernetes 及其余 Provider/故障矩阵，不迁移旧卷、不发布候选、不关闭 Gate。

后续 candidate `0.3.0-dev.375` 又把 Codex MCP 的 `omit_tools_from` 固定为空列表，避免 deferred tool-search 配置遮蔽受控工具；同一 Docker 真实 MCP/Skill、事件续读、撤销负向和资源清理再次通过。该配置切片不改变凭据、网络 allowlist、审批或 fencing 边界，仍不关闭十二格或正式 Gate。

2026-09-20 r430 使用 deepseek-harness 新 tenant-local 配置副本与公司局域网代理完成 Kubernetes（OrbStack 单节点）能力绑定恢复：Docker 与 Kubernetes 均真实完成 MCP 8 requests、1 次副作用、1 个签名 Skill、Artifact verified 和事件续读；Kubernetes Control Plane SIGKILL 后 attempt 2 为 `capabilityBound=true`、`process-restart/recovered`、`sideEffectOutcome=confirmed`、RPO 0；撤销负向为 `runtime_requests=0 side_effects=0 events=2`，Compose smoke 通过。日志 `.tmp/mcp-skill-runtime-v1-20260920-r430-deepseek-kubernetes-capability-recovery.log` SHA-256 `sha256:07a2ef15c5743963148730f0e46c79fa721fcde90e1f276b00590138473dde5f`；Compose/Kubernetes/fixture 资源均精确清理为 0。该证据不外推 Kubernetes cross-node、Worker/Agent 进程退出或完整十二格/Gate。

2026-09-20 r431 在隔离 kind 三节点上复用同一注入协议完成 deepseek-harness Docker/Kubernetes capability acceptance、同节点恢复和撤销负向；普通 Kubernetes cross-node takeover 真实恢复但 `capabilityBound=false`（RTO 32972ms、RPO 0、snapshot 2693120 bytes），因此仍不得宣称能力绑定跨节点。日志 `.tmp/mcp-skill-runtime-v1-20260920-r431-deepseek-kubernetes-cross-node.log` SHA-256 `sha256:696da53554fe8eaafcfe97e370cf08b4ce91985d93d749d7dd408465935b2541`；首次缺 CRD 的运行由新增 preflight fail closed，所有 kind/Compose/fixture 临时资源均清理。

2026-09-20 r437 修正跨节点恢复后的 Kubernetes 注入边界：恢复 Sandbox 切换 `kubernetes_active_namespace` 后，MCP fixture、事件同步和清理均使用目标 namespace，不再查询已删除的源 namespace；Kubernetes 入口继续先校验 OpenSandbox 0.2.0 CRD/RBAC，未满足即 fail closed。未发布 dirty candidate `0.0.0-dev.r398`（sourceCommit `549d5d6f0052d2c06dc90f7b97c1dc11cd28ce04`、sourceDirty=true；manifest `sha256:65cd73e0c92c15f7bb6bc7f62e2ebab9036dc0d02998f719b0f94fa6be61ebb5`、checksums `sha256:99b785379b408d24b1e5d50f87eb178071ab4b818edbe7222b2ad13fbfcc3bfe`；OpenSandbox controller chart `0.2.0` `sha256:0e37b51da3f4e36a1d71c8bebbb41a58ba7f0aaf6d47916513fb2beb2d7a8925`）。隔离 kind 三节点真实 deepseek-harness Kubernetes cross-node takeover 现为 `capabilityBound=true`、attempt 2、`recovered/confirmed`、RTO `100149ms`、RPO `0`、snapshot `880640` bytes；恢复后的 Kubernetes acceptance 为 MCP `14`、Skill `1`、Artifact verified、events resumed，撤销为零请求/零副作用/2 events。日志 `.tmp/mcp-skill-runtime-v1-20260920-r437-deepseek-kubernetes-capability-cross-node.log` SHA-256 `sha256:5df2ccdb800490ce63a5e890dc0ff84a456d4070ac7d594350dfdf8614fce45f`，Compose/kind/Pod/namespace/fixture 资源均为 0。该证据只推进 deepseek-harness×Kubernetes 能力绑定跨节点子路径，不外推到其它 Provider、RemoteWorker/Docker cross-node、Worker/Agent 或完整十二格/Gate。

2026-09-20 r442 修复 RemoteWorker 跨节点 capability fixture 的目标 Docker daemon 边界，并避免已完成的 cross-node capability recovery 被后续通用 process-restart 覆盖。目标 Sandbox 在独立 DIND daemon 上运行时，fixture 通过目标 daemon 解析 Runtime ID，并从目标节点可见的只读 `/dind-config` 注入已打包物料；不读取用户主机任意路径，也不把凭据写入 Workspace、日志、事件或 Artifact。未发布 dirty candidate `0.0.0-dev.r441` 的 manifest/checksums digest 为 `sha256:1e5d44bf88f6c82634918044f0e4d8b64cda129d248afa38b4610aebbf9ae58a` / `sha256:ccea0543ff41a82c10c7b3aa54df24e9454a4f3ce1d536724e65212c5ca27d7c`；Runtime、Deployment、Control Plane arm64、Worker arm64、TS SDK、Go SDK digest 依次为 `sha256:0a84f1309741da7f686f4b5282f4c4c55b81388a0f8a5c99d56fc8b8f0d163a4`、`sha256:a247b1a7009cca2b1308afd9469f1f6e55ba99abb81e2eb3ec8c8d306aed9282`、`sha256:381da71952ae7aa7535b2d2f3c2837e1fb47988233df83b01bd90847d024fb50`、`sha256:3c0e4b8160daef5ba54dbb06c48b78d71249f00cff60db5dff06d88629a1fd22`、`sha256:d81324ca491151c3e0290ca88977870a65bec41b2d4f0e76276bcdf4f1616be2`、`sha256:06fb5d735c3012c84e7a9bf7489bdc726c5df948d988b0fedb6e5c10fc2e75b3`；Node verifier 与 22 项 checksum 均通过。Pi×RemoteWorker 实跑在恢复后完成 MCP `14`、Skill `1`、Artifact verified、events resumed，`capabilityBound=true`、attempt 2、`cross-node-takeover/recovered/confirmed`、RTO `8675ms`、RPO `0`、snapshot `20992` bytes，撤销后为零请求/零副作用/2 events，Compose smoke exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260920-r442-pi-remote-worker-capability-cross-node.log` SHA-256 为 `sha256:288ceccca1ac0859e8e9bff02c0cb6e699aa6fea283704a0587e18530a6eb59b`，所有本轮容器、DIND、OpenSandbox、网络、卷和 fixture 均精确清理为 0。该结果只登记 Pi×RemoteWorker 的 capability-bound cross-node 子路径；完整整格仍需逐项版本/跨租户/旧 generation、Worker/Agent 退出和未知副作用矩阵，不关闭十二格或正式 Gate。

2026-09-21 r445 复用同一目标 daemon 注入协议在 deepseek-harness×RemoteWorker 上真实完成恢复后的能力链：Docker/RemoteWorker 均有 MCP、Skill、Artifact、事件续读，RemoteWorker 源节点失效后 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered/confirmed`、RTO `9734ms`、RPO `0`、snapshot `850944` bytes，撤销后零请求/零副作用/2 events，Compose smoke exit 0。r444 的单次 HTTP 499 follow-up 按 fail closed 不计数；r445 日志 `.tmp/mcp-skill-runtime-v1-20260921-r445-deepseek-remote-worker-capability-cross-node.log` SHA-256 `sha256:708db8de02635d561ced3580fbfeddd84a3fff8f3fccb7c5783f2d263f96c127`，目标 DIND、Compose、OpenSandbox、网络、卷和 fixture 均精确清理为 0。该证据只新增 deepseek-harness×RemoteWorker 能力绑定跨节点子路径，不外推到其它 Provider/环境、Worker/Agent 或完整十二格/Gate。

2026-09-23 剩余验收继续按 fail closed 记账：同一 r525 candidate 的 Claude×Kubernetes 与 Pi×Kubernetes transport/revocation 真实通过（分别日志 SHA-256 `2d9aeacad111e4fb942a849a7519142173ab0da9ab28753e188cf8f5896c47d8`、`e1c1346fa4ed2386d38c2ac6b59c4530ad3c30dd779fa9620f1016b4bef78a4d`），均含 MCP 8、side effect 1、Skill 1、Artifact verified、events resumed、transport reconnect 与撤销零请求/零副作用/2 events。deepseek-harness×Kubernetes（`29b2fcbd4c8c4d0ffb1f98212bfbfbf52aa892498da65e5a49f680454e29e858`）和 deepseek-harness×Docker（`4ab6452607472e3efb83655caab54606bba3b3e7e85c18b9fb59653e8c631db2`）均在真实 MCP 路径失败并保持 `provider_unavailable`/无 acceptance；r527 Docker 重试在 `mcp_requests=8/side_effects=1` 后仍 `provider_unavailable`（日志 SHA-256 `91a6599895205f793ac54572ea153d01d14789a604e58f178258ba3f0718e4e9`），不升级能力目录。Codex×RemoteWorker r524 transport 尝试无 transport marker（日志 SHA-256 `bdb9d43f2a3a6d5010f8c6fdcca1b23f8660ac7272256b49baa55e674e7558c0`），不计数；`capability_bound_recovery=0` 时跳过通用 recovery 的脚本 guard 已补齐，静态测试 32/32。上述结果不关闭十二格或 Gate；版本/租户/generation 拒绝、重启与未知副作用继续沿用既有 fencing、Operation/Audit、先对账后重试语义。

2026-09-24 r551 复用既有 Runtime/Controller/Worker/fencing，不新增调度器或迁移路径。Codex×Docker 唯一真实运行在 Admin/Web smoke、过期授权和 contract negative（不兼容 MCP/Skill、跨租户 401）后因 `provider_unavailable` 未形成 checkpoint，故不扩展任何 Provider×环境支持结论；本轮 OpenSandbox Exec 只确认断流后的 status reconcile，不把残留的无归属 server 管理卷纳入清理。候选、日志 digest、命令与四包 Go/Bun 回归结果见 [06](06-status-tracker.md)，版本/租户/generation、重启和未知副作用仍按 fail-closed/先对账后重试执行。

同日的无副作用 Provider 探针确认 `tenant-local 配置中的模型` 在 loopback proxy 与直连的 `/responses` 均被拒绝为 `MODEL_NOT_ALLOWED`；按用户决定保留该模型，不修改凭据或 adapter，不把后续真实验收失败归因于 Runtime。Provider allowlist 恢复前，继续沿用现有 contract-negative、generation fencing、Operation/Audit 和未知副作用先对账边界。

同时修复 Compose 验收脚本的 recovery selector guard：关闭 capability-bound recovery 时不再隐式执行默认 Docker recovery；32/32 静态回归通过，后续 transport-only 运行可到达其真实 Provider 阶段。

2026-09-24 的 gpt-6-luna candidate r554 验证了 Codex×Docker transport-only：manifest `16a395b1acea81c537a86315fb7acfdd922d2d07120993ab87d96eda7785cbbb`、checksums `8fd2d03ae616311343165b403e6572d1bd08ee16c2fa343fa9883ecf0da128b0`，本地 overlay image `sha256:719e9d41cb0ae7a7849de7401f0b18a7716bd2979a20140d32bb6588f3735804`。修复 Codex unknown MCP result 的 `item.updated` 映射并在 pending side effect 分支停止 claim heartbeat 后，真实重跑达到 acceptance（MCP14/Skill1/side effect1/Artifact/events）及 transport recovery（replayed=0/reconnect=passed），Compose exit 0。该 candidate 为 dirty 本地候选且 overlay label 沿用 r547，不能作为发布制品或其它 Provider/环境的完成证明。

2026-09-25 的 r559 修复并验证 deepseek-harness 的真实 SDK 路由：`@deepseek-ai/dsh-llm-pi-ai@0.1.2-rc.1` 使用 `openai-responses`/`/v1/responses`，关闭默认 `tool-fs` 的可选 sandbox escalation，保留只读 Skill 与 Host-managed `str_replace_editor` Workspace 写入。以 gpt-6-luna 受保护凭据在 Docker transport-only 真实通过 MCP 8、Skill 1、Artifact、事件续读、断连重连（`replayed=0`）、撤销零请求/零副作用、跨租户 401 和旧 generation 409；r559 候选/命令/日志 digest 与第一次未进入 Provider 的 preflight 边界见 [06](06-status-tracker.md)。该结果不扩大到 RemoteWorker/Kubernetes、Worker/Agent 重启或完整 Gate。

2026-09-25 的 r561 修复并验证 Pi 的真实 API 路由：Pi pinned SDK `0.85.1` 的配置从 `openai-completions` 改为 `openai-responses`，因为用户提供的 gpt-6-luna endpoint 对前者返回 400、对后者返回 200。以同一 dirty-source candidate 在 Docker transport-only 真实通过 Pi MCP 8、Skill 1、Artifact、事件续读、断连重连（`replayed=0`）、撤销零请求/零副作用、跨租户 401 和旧 generation 409；日志 SHA-256 `6755596a5f429d0e33ef4ae646f3caaf1d6a44352b873400c886a99c822bfe0f`。该结果仅新增 Pi×Docker transport 子路径，RemoteWorker/Kubernetes、Worker/Agent 重启和完整 Gate 仍开放。

同一 r561 candidate 的 Pi×RemoteWorker 实测在 Docker baseline 后完成跨节点接管：`capabilityBound=true`、attempt 2、`cross-node-takeover/recovered/confirmed`、RTO `9695 ms`、RPO `0`，恢复后 MCP 14、Skill 1、Artifact 与事件续读通过，transport 断连重连 `replayed=0`，撤销/跨租户/旧 generation 分别为 `0/0`、`401`、`409`。该证据关闭 Pi×RemoteWorker 的 transport 与能力绑定跨节点子路径，仍不覆盖 Pi×Kubernetes、Worker/Agent 退出和完整 Gate。

r561 Pi×Kubernetes 的修正 wrapper 重跑先得到 Docker/Kubernetes acceptance（MCP 8、side effect 1、Skill 1、Artifact verified、events resumed）与跨租户 `401`，但 Kubernetes transport 阶段在执行 `running` 时收到 `unexpected EOF`，没有 transport/revocation/stale-generation 终态；wrapper exit 1，日志 `.tmp/mcp-skill-runtime-v1-20260925-r561-pi-kubernetes-transport-rerun.log` SHA-256 `sha256:4215ee30a37ba89cc7afd50ba73174fe24848257e113d1e0ef6a18e25898b582`，资源清理为 0，按 fail closed 不计 r561 Kubernetes transport。随后 transport 关闭的 acceptance-only 诊断只收到 Skill、没有 MCP call，亦 exit 1（日志 SHA-256 `sha256:083981fb3c904e8a07bb12bfe97c824b9eaca42de8c5587c05f5678fdb3f6dd7`）；该诊断不推翻历史 r525 的 Pi×Kubernetes transport/revocation 证据，也不扩大能力目录或迁移阶段。

r559 的 deepseek-harness×Kubernetes 单节点运行出现 MCP 8、Skill 1、Artifact、事件续读、断连重连 `replayed=0`、撤销零请求/零副作用、跨租户 `401`、旧 generation `409` 等终端标记并打印 Compose smoke，但 wrapper 最终 `exit 1` 且未给出可安全归因的单一 teardown 原因；按 fail closed 只保留部分终端标记，不计为该格 transport 通过。日志 SHA-256 `6c1278be2b8431a24e2a0f6ad65ca77899d49fe5813d926e0aaef142eeedc315`。同候选 deepseek-harness×RemoteWorker 因 recovery Turn 没有 pending-side-effect checkpoint fail closed，日志 SHA-256 `eba791a50976f8b96772be544d5489ab67193d78d2fa1a5e29fcbe92de3134d1`；不把该轮计为 RemoteWorker transport。

### 2026-09-25：MCP-SKILL-RUNTIME-V1 closeout evidence（r562–r568）

本轮只补真实验收证据，不重新实现 Contracts、SDK、Runtime、Provider adapter 或 Admin。r568 在隔离 kind `v0.33.0` / Kubernetes `v1.37.0` 三节点集群完成 Codex×Kubernetes capability-bound cross-node：先 fence 旧 writer、删除/隔离源节点，再从 portable snapshot 在目标节点恢复 Workspace/Sandbox 与 capability binding；attempt 2 为 `cross-node-takeover/recovered`，side effect outcome 为 `confirmed`，RTO `95560 ms`、RPO `0`、snapshot `4096000 bytes`；本轮 harness 未输出独立 snapshot digest，只保留整份日志 SHA-256 作为可复核证据。恢复后 MCP/Skill/Artifact/事件续读、撤销负向、旧 generation `409` 与跨租户/不兼容版本 `401` 均形成真实标记，日志 `.tmp/mcp-skill-runtime-v1-20260925-r568-codex-kubernetes-cross-node-closeout.log` SHA-256 `80702ec5d0d1b2378ecbbe13817c51be68e36776ebe0ad2b8234d11b62dd985d`，任务资源精确清理为 0；未知副作用先确认后结算，没有盲目 replay。

Worker/Agent 故障候选保持 fail closed：r562 deepseek-harness×Docker Worker/Agent FAILED（SHA-256 `b1e4dd001917e605cb438f06c1f47fbfd821b5fc8f4baf4fad9415ee6bdbd88b`）；r563 Codex×Docker Worker/Agent FAILED（`provider_unavailable`，SHA-256 `8bf04267ce2663096b8f349a655d5cac2fc47c576a1fe831716f5060d2fe684a`）；r564 Claude×Docker Worker/Agent BLOCKED（`provider_unavailable`，SHA-256 `d05d0a374a7c2c1e3bf245702b9d875b82ed2f4e06529e9a97a1540b1637ecda`）；r565 Pi×RemoteWorker Worker/Agent FAILED（等待 capability-bound recovery side-effect checkpoint 超时，SHA-256 `39e6b29eec77d762db210c15cb2202a9294034e08edc1e72b883981955e2eedf`）；r566 Codex×Kubernetes Agent FAILED（controlled fault 后 `unexpected EOF`，SHA-256 `4167eb92f7905c460c0e04a150d9bdab49dd162a277a7e994a7be0b833a9a4d7`）。r567 仅是新 kind 集群缺 OpenSandbox 0.2.0 CRD/RBAC 的 preflight，SHA-256 `ee5eacdf0592faa2ebe71002fe2b0b51bc6e0ab03533238414f8ce1ca7c8f777`，未进入 Provider，不计作真实格子尝试。Kubernetes direct Sandbox 没有与 Execution 绑定的 Worker fault target；脚本 `scripts/test-platform-compose.sh:4927` 明确 fail closed 返回 `Kubernetes direct Sandbox has no bound Worker fault target`，因此 Kubernetes Worker 故障项记录为不适用，并由 r566 的真实 Kubernetes Agent 运行与控制面 target 证据保留边界。

断连重连仍只按已形成的 `replayed=0` 证据计数。当前仍开放的 transport 格子是 Codex×RemoteWorker、Codex×Kubernetes、Claude×RemoteWorker、deepseek-harness×RemoteWorker、deepseek-harness×Kubernetes；Pi×Kubernetes 的历史 r525 通过与 r561 `unexpected EOF` 失败保持分开。原始 `tenant-local 配置中的模型` 复验仍因此前真实 `MODEL_NOT_ALLOWED` 路由阻塞而未计数；正式 Gate 与 capability catalog supported 标记继续等待批准。

r569 对 Codex×Kubernetes transport-only 做了唯一一次实际 Provider 环境尝试：harness 在创建 OpenSandbox service-account token 时向 `k8s.orb.local:26443` 收到 `unexpected EOF`，未进入 Provider、MCP/Skill/Artifact、事件续读、transport/revoke/stale-generation 终态；按外部 Kubernetes/网络问题 BLOCKED 记账，不重试。日志 `.tmp/mcp-skill-runtime-v1-20260925-r569-codex-kubernetes-transport-closeout.log` SHA-256 `6fffcddf07fe6f4d4ad3d92240b104713c63deb38c8aa770d8df202c649b587e`，task-owned 资源清理为 0，secret-pattern scan 为 0。

r570 对 deepseek-harness×Kubernetes transport-only 完成了一次新的真实尝试：Docker/Kubernetes contract-negative、MCP、Skill、Artifact 和事件续读均通过，但 transport 阶段执行仍为 `running` 时发生 `unexpected EOF`，随后 fixture container 不存在；未产生 transport、撤销或旧 generation 终态，按外部运行时 EOF BLOCKED 记账，不重放未知副作用。日志 `.tmp/mcp-skill-runtime-v1-20260925-r570-deepseek-kubernetes-transport-closeout.log` SHA-256 `aabf4f190a84e01567d7546ee1d902f4beadf56e158a4fb38b9286bc97915539`，task-owned 资源和 secret-pattern scan 均为 0。

2026-09-26 r584 重新验证 Codex×Kubernetes transport-only 的外部前置：候选 `0.3.0-dev.584`（dirty sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`，manifest/checksums `dd3736acae3ca3c0319e6ca1b5a2669ad852908df40dd00f4c37de6a77e81a21` / `aa25c45bf1517fff72b95bd2700258aac3d41bf6d0548186254fb240833b3276`）在本机 Surge 代理下进入 Compose 基础 smoke，但 `curl` 处理 `409` 后结束，未进入 Provider 或 Kubernetes transport marker。日志 `.tmp/mcp-skill-runtime-v1-20260926-r584-codex-kubernetes-transport-rerun.log` SHA-256 `ba665781aa00f6a12f31d3353e404e30ac8219bab6f1ca876830b25cdea43736`，资源清零；按 harness/preflight BLOCKED 记账，不把它写成 partial PASS，也不重复同一轮。

随后修复了真实 Codex follow-up approval 的命令匹配：harness 现在匹配 Provider 实际请求的 `/bin/bash -lc 'cat -- <artifact>'`。同一 candidate 的唯一实际修正重跑通过脚本前置与基础 Compose smoke，但在 OpenSandbox service-account token 创建阶段再次收到 `unexpected EOF`，没有 Provider、MCP/Skill、Artifact、事件续读或 transport marker。修正重跑日志 `.tmp/mcp-skill-runtime-v1-20260926-r584-codex-kubernetes-transport-fixed.log` SHA-256 `27ba1a8613284ade7d4f4446b80e6083314096b5be628eff4076cc2a4a1c82fe`，Compose/OpenSandbox 任务资源清零；按外部 Kubernetes 前置 BLOCKED 记账，Codex×Kubernetes transport 和正式 Gate 继续 OPEN。

同一 harness 修正的 Codex×Docker transport 回归在 Provider 启动前被基础 Admin Worker upgrade 断言拦截（`Admin API did not close the Worker upgrade operation`），没有新增 Provider 或 capability marker。日志 `.tmp/mcp-skill-runtime-v1-20260926-r584-codex-docker-transport-fixed.log` SHA-256 `991dd550b5644cb620e237a79be71625f443d018d81513a5672502d814c1ce36`，任务资源清零；既有 Codex×Docker PASS 不被这次 preflight partial 取代。

2026-09-25 r571 对 Codex×Kubernetes cross-node 的 snapshot digest 采集修复只做一次真实重跑。harness 已补充从同一 `workspace_snapshots.content_digest` 读取并把 `sha256:<64 hex>` 传入最终断言；但隔离三节点 kind 集群在 OpenSandbox controller/server 启动前即因镜像仓库代理 `127.0.0.1:6152` connection refused 进入 `ImagePullBackOff`，没有进入 Provider 或 cross-node marker，也没有 snapshot digest 可记录。该外部 registry/proxy 阻塞按规则记为 BLOCKED，不循环重试；r568 PASS 与“缺独立 digest”边界保留。集群、namespace、Pod、临时 kubeconfig 均已清理，日志 `.tmp/mcp-skill-runtime-v1-20260925-r571-codex-kubernetes-cross-node-digest-closeout.log` SHA-256 `798fbb9346e41d6e3e6451ae8f7639b1c2e6621b6986be4132fbab026413e752`。

### 2026-09-25 evidence consolidation：OpenSandbox lifecycle 与 Codex×Kubernetes snapshot

> r582 的 snapshot digest 结论仅保留为历史审计；后续确认 restored digest 是 source digest 的复制，已由 2026-09-26 harness repair 记录 superseded，不能作为当前独立 cross-node PASS。

`UNBLOCK-OPENSANDBOX-RUNTIME-LIFECYCLE` 的 r575 最终证据只关闭固定镜像的 Docker 生命周期预检：sourceCommit `549d5d6f0052d2c06dc90f7b97c1dc11cd28ce04`、sourceDirty=true；镜像 `sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb`（image ID `sha256:85b5686f2ad863058d3863550c18761f6bb57d0a87c024d1e77ee49d72e78810`）以固定配置启动，`docker run=0`、`/health=200`、容器清理为 absent。预检日志 `.tmp/opensandbox-r575-fixed-preflight/preflight-summary.log` SHA-256 `6641397fce9cb562b58f95705910692178908354bf3411512dba9f2682abb97f`；container log SHA-256 `2d6d9550d86b3b634016717af43629c93058a35257407defd7f4326d4b7b9350`。该证据的 provider、Kubernetes harness、snapshot、npm/package digest 均为 N/A，不能标记 Provider 或 Kubernetes 验收通过。

`MCP-SKILL-RUNTIME-V1-CODEX-KUBERNETES-SNAPSHOT-CLOSEOUT` 的 r582 使用 dirty sourceCommit `549d5d6f0052d2c06dc90f7b97c1dc11cd28ce04`（候选 manifest SHA-256 `79010b82b85ec2d4c22294edd754b1034c06ddb3f7a3a86ba6e2f7ae1e0909b3`、checksums SHA-256 `478c9efa70dfdd235ca6ab3004cd24ffe1d499f0632b44fdcf8b2b0c10a56e42`）、Worker image `sha256:573196d95aeac01fcf5d1b81235a48ed2379ccd56fd3f1cb6a922457f6fba9fc` 和四个精确 npm tarball SHA-256（详见 [06](06-status-tracker.md)）。在 kind 三节点 Kubernetes 上，Codex cross-node takeover 为 `capabilityBound=true`、attempt 2、`cross-node-takeover/recovered`、side effect `confirmed`、RTO `48802ms`、RPO `0`；canonical snapshot `sha256:6673118d540d845887e887e12c286c8c0df4a8f05911363929eb34e88b5ae019` 与恢复 Workspace digest 相同，raw archive SHA-256 为 `fcacb0c10cd9acc914ca5948a4bfd04bd1a89fd46610cc45ff124bcb3c98f4ab`。harness log SHA-256 `335c8d4a3d0b03f57875c63ed9bee94da1c7097633475e689a897b3261ccf6e9`，evidence JSON SHA-256 `7c7911ae46cf17468b9a1ca9e1ea829e4b7855a9e1f8e4cd3e0297ea9cc9cac5`；kind cluster、task containers/networks 与 task-owned volumes 清零，保留无关 volume 不计入。该条关闭 Codex×Kubernetes cross-node snapshot 子路径，不关闭 transport 五格、Worker/Agent 矩阵或正式 Gate。

2026-09-25 实现边界收敛：统一 Host-managed Runtime broker 只接受 `streamable-http`。Control Plane 绑定解析、Runtime materialization generator、Go validator 和 Provider API materialization 在 Provider 启动前拒绝 `sse`/`stdio`，避免把声明存在但尚未实现的传输类型送入 Provider；公共资源 catalog 仍保留其它传输枚举作为元数据，supported 状态继续以真实验收为准。

`MCP-SKILL-RUNTIME-V1-ONE-CELL-TRANSPORT-RECOVERY-CLOSEOUT` 的初始审计记录（r583 之前）在 `sourceCommit=29afe9b9103cac99088f637e02a8cd9bb3f48d58`、`sourceDirty=true` 的工作树上只完成共享边界切片和定向回归，尚未新建 candidate/image/package；随后 r583 恢复前置并完成 Codex×RemoteWorker 真实格，见下文。没有把静态测试或历史 PASS 外推为迁移完成，父级 Gate 继续 OPEN。

### 2026-09-26：MCP-SKILL-RUNTIME-V1-UNBLOCK-AND-CLOSEOUT 前置收口

- OpenSandbox 前置只执行了一次 TokenRequest。OrbStack `orbstack` API 为 HTTPS，`k8s.orb.local` 已加入 `NO_PROXY`；API `/version`、OpenSandbox 0.2.0 CRD/ClusterRole、ServiceAccount 与 `can-i` RBAC 检查通过；kind 无集群（本轮为 OrbStack 单节点）。controller/server/runtime image 均固定为 digest：server `sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb`、controller `sha256:a9a5f73c1785ebd955336ffa313973a35c1a1b662cb7afc4ea82d92021b3532a`、execd `sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd`、egress `sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a`。唯一 TokenRequest 成功，token 仅保留 SHA-256 `6754f9c894f011383bf07ed35576ac3cfad7403314bb2365aae07c855478aa97`。
- Codex Docker Worker upgrade 共享 harness 已保留 `cat --` 精确命令匹配，新增 Operation 返回体诊断和回归用例；无 Provider preflight 的 HTTP 200 Operation 为 `MaintenanceOperation/target.upgrade/resourceGeneration=1/succeeded/complete`，exit 0。日志 `.tmp/mcp-skill-runtime-v1-20260926-unblock-codex-docker-upgrade-preflight.log` SHA-256 `ce65654ba6c92bd6e421830ef3d911960a357e44d8daf052acd46c228714e1a4`，Compose/Worker/网络/卷/smoke 目录均清零。
- 依赖前置通过后仅尝试一次 Codex×Docker transport/reconnect；contract-negative 与跨租户 `401` 通过，Provider 返回脱敏 `provider_unavailable`（fixture requests `12`、side effects `0`），未形成 MCP call、Skill、Artifact、transport 或恢复 marker，exit 1。日志 `.tmp/mcp-skill-runtime-v1-20260926-unblock-codex-docker-transport.log` SHA-256 `3e035011e29a9f8bae01b4e1ee415e394739e7ba072d16f1c143250ee49c2026`，资源清零；按 fail-closed 停止，不启动 Codex×Kubernetes，不更新任何 Provider supported 状态。
- 随后按批准的外部 Provider 临时 fixture `gpt-5.6-sol` 各执行一次 Codex×Docker 与 Codex×Kubernetes transport/reconnect，均 exit 0，完成 MCP、Skill、Artifact、事件续读、断连恢复、撤销、旧 generation、跨租户和未知副作用 reconcile；这两格只登记为 `codex/gpt-5.6-sol`。该历史 fixture 未修改；其 allowlist compatibility gap 仍 OPEN，不能代表当前 `provider-credentials/tenant-local.*.json`，替代模型结果不写成原模型 supported。汇总 evidence JSON `.tmp/mcp-skill-runtime-v1-20260926-unblock-evidence.json` SHA-256 `a8bcc3163c8be313ab0d35c30554a27c25d9564172914832aded8534d2b59601`。
- 修复 r580 snapshot harness 根因：raw tar SHA-256 不等于 Go `snapshotArchiveDigest` 的 canonical digest。`scripts/lib/portable-snapshot-digest.mjs` 现在直接解析恢复 tar（含 GNU `@LongLink`、ASCII typeflag 与 byte-order sort）后独立计算 semantic digest；回归 `2 passed`，r580 archive 得到 semantic `sha256:7a290d217223b55c788233c8d38febfaf7589d49f6ff776cc006d9a2d6767671`，raw `1f5d6c6c3313850f6ff6a3869109993ebb99cad67243b03df14d85f2a77145bb` 仅作为独立 raw 字段。r582 旧证据因把 source digest 复制为 restored digest 而 superseded，不再作为独立 cross-node digest 依据。修复日志 `.tmp/mcp-skill-runtime-v1-20260926-snapshot-digest-harness-repair.log` SHA-256 `1bf71b61f9941a2c35c37fe6a14483d2208e25ee806a372abe2d25702b5d4269`。
## 2026-09-25：Codex × RemoteWorker transport/reconnect closeout（r583）

- 在 registry manifest inspect 与 access-gateway arm64 build 恢复后，只生成并使用一个 dirty candidate：`0.3.0-dev.583`，`sourceCommit=29afe9b9103cac99088f637e02a8cd9bb3f48d58`、`sourceDirty=true`。candidate manifest SHA-256 `0d1ed55092a19f6411baec3ce4e8ea4ebef47128d0a4161f3285f82d37623bf9`，checksums SHA-256 `2994dec53fd03dc44e1efc9c377f4aa45c17737b79e0a1c06a2958106e50a24e`；Runtime `sha256:696ae9ee9812afbac80bb64640b6a6e066070aaf97b0211d1f66e8d1e20f09c8`、RemoteWorker arm64 `sha256:fac428bbde2274699b641be918e6f1999a6ee4549fcdaf745fe2099578c671fd`、Worker arm64 `sha256:dc9af2fd6b834376f523e2663c2480a2a126d45accf07dca145a3d9a38e9e823`、Control Plane arm64 `sha256:31b9f3e9fda684cc2071420ec72a7469441efc36460f42ac977c1e648a594553`；access-gateway base `gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab`，local preflight image `sha256:3a3a56d2201eb151465b3141933df443469f87a37272626d79cba1be85d5737a`。npm/package digest 不适用，candidate checksums 已逐项验证 25/25。
- 实际命令为 `sh scripts/test-platform-compose.sh .tmp/mcp-skill-runtime-v1-20260925-r583-codex-remote-worker-candidate .tmp/mcp-skill-runtime-v1-20260925-r560-pi-docker-YnMRRJ`，配合 `CLOUD_AGENTS_COMPOSE_REAL_PROVIDERS=codex`、`CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY=1`、`CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY_ENVIRONMENT=remote-worker`、`CLOUD_AGENTS_COMPOSE_CROSS_NODE_RECOVERY=1` 和 loopback proxy；完整无凭据命令保存在 `.tmp/mcp-skill-runtime-v1-20260925-r583-codex-remote-worker-evidence.json`。
- provider/environment 为 `codex/remote-worker`。MCP `28`、Skill `1`、Artifact `verified`、事件续读 `6 resumed + 2 post-terminal`；transport `fault_requests=1`、`replayed=0`、`reconnect=passed`；revocation `runtime_requests=0/side_effects=0/events=2`；旧 generation `1→2` 返回 `409`。Control Plane SIGKILL 后 old writer fence 生效，attempt `2` 为 `cross-node-takeover/recovered`，side-effect outcome `not-applied`，RPO `0`，RTO `7551 ms`，snapshot `3567616` bytes，digest `sha256:ac11134493106b48367686eed8c209809764e6bcddcac14d7b0bbe29b6ce78b1`。
- 结果日志 `.tmp/mcp-skill-runtime-v1-20260925-r583-codex-remote-worker-transport.log` SHA-256 `cff6904c91b832716bb17278868ffa8f6117eee018d09765c09b706033f1e978`；派生 evidence JSON SHA-256 `e486642e2225b314e8af2470639eac5904c7ab743b22038a6d04f06e56c14b0c`。Compose、RemoteWorker、OpenSandbox owned resources 清理为 `0`；未覆盖 Codex×Kubernetes、其它 provider/environment、Worker/Agent fault 和完整十二格，父级 `MCP-SKILL-RUNTIME-V1` 仍 OPEN。

### 2026-09-26：UNBLOCK-AND-CLOSEOUT Provider 根因复核

历史 r584 Codex×Docker Provider 尝试后的脱敏复核确认：该次受保护 fixture 的 tenant-local model 在代理与直连的 `/v1/models` 都不存在（HTTP 200、28 个模型），两路 `/v1/responses` 均为 HTTP 400 `MODEL_NOT_ALLOWED`。保持原始 fixture 不变，只用任务范围副本探测当前允许的 `gpt-5.6-sol`，同一 endpoint 返回 HTTP 200；因此根因在外部 Provider model allowlist，不在 Runtime、harness 异步等待、代理、TLS 或 Docker cleanup。原始模型未被改写，Provider 格记录 BLOCKED，不把修复探针当作验收 PASS。脱敏日志 `.tmp/mcp-skill-runtime-v1-20260926-codex-provider-repair.log` SHA-256 `a8e509413a4e9a045f7cb838bbc0bf48bd4f9eda3b151931b2fb1e89b5056721`，汇总 evidence JSON SHA-256 `705b22cf44807c34a5f089438095c4b363d81303eab5fda7a3172090370d2e10`；探针未创建 task-owned 资源。

Kubernetes transport/reconnect 只执行一次：在 `orbstack` 上用修复后的 `NO_PROXY`/HTTPS API 进入 OpenSandbox runtime 并到达 Provider 层，contract-negative 与跨租户 `401` 通过，随后同一未修改模型得到 `provider_unavailable`，没有 transport/reconnect/revoke/stale-generation marker。日志 `.tmp/mcp-skill-runtime-v1-20260926-unblock-codex-kubernetes-transport.log` SHA-256 `deceb1ecf0bd302c6ff8651616be703371fc97041785784f9f36375400f7a8a9`，task-owned 容器/网络/卷/smoke 目录均为 0；既有 OpenSandbox 资源保留。该格按外部 Provider BLOCKED 记账。

### 2026-09-26：批准模型临时 fixture 的两格验收

历史 Provider source audit confirmed that `tenant-local.codex.json` is selected after the absent `tenant-compose-smoke.codex.json`; the Codex adapter passes its `model` and `baseURL` through unchanged and writes `wire_api=responses`. The historical tenant-local credential was not modified. Because the external endpoint allowlist omits that model, a separate task-owned fixture used the already approved `gpt-5.6-sol`; this result is recorded only for `codex/gpt-5.6-sol`, never as current tenant-local model support. Docker and Kubernetes each ran once and both completed MCP/Skill/Artifact/event, transport reconnect, revocation negative, stale generation and unknown-side-effect reconciliation checks with exact cleanup. Source audit and both logs/evidence hashes are in [06](06-status-tracker.md).

### 2026-09-26：Provider 模型来源修正

当前工作区的 `provider-credentials/tenant-local.*.json` 四份配置均以 `model` 字段提供 `gpt-6-luna`。验收脚本和回归检查按 `tenant-local.$provider.json` 读取模型，不在代码或文档中固定模型名；此前其它模型的运行记录仅作为历史 fixture 证据，不能外推为当前 tenant-local 模型支持。只读模型可用性证据 `.tmp/mcp-skill-runtime-v1-20260926-tenant-local-model-source.json` （SHA-256 `5f0299e1afa4936844256262c47e90401a0c3ed989593938a96546701250c0e7`）显示代理/直连均 HTTP 200、28 个模型且当前模型存在；随后 `/v1/responses` 代理/直连也均 HTTP 200 且有 response id。该证据明确 `providerAcceptance=not_run`、`supportedClaim=false`。

### 2026-09-26：当前 tenant-local `gpt-6-luna` 两格真实 closeout

模型事实以 `provider-credentials/tenant-local.*.json` 为唯一来源；本轮不替换模型、不修改原始凭据，也不把历史替代模型 fixture 合并到当前支持结论。候选目录 `.tmp/mcp-skill-runtime-v1-20260926-r584-candidate` 为 dirty source，manifest/checksums SHA-256 分别为 `dd3736acae3ca3c0319e6ca1b5a2669ad852908df40dd00f4c37de6a77e81a21` / `aa25c45bf1517fff72b95bd2700258aac3d41bf6d0548186254fb240833b3276`；当前汇总 evidence JSON `.tmp/mcp-skill-runtime-v1-20260926-current-gpt6-luna-closeout-evidence.json` SHA-256 `586b3fc50c1d992b5bed4faf80ec896e2f50e52e53ea649fae229c154f998959`，完整无凭据命令与模型来源记录在 `.tmp/mcp-skill-runtime-v1-20260926-current-gpt6-luna-closeout-commands.sh`（SHA-256 `13a12195999376b5c4e79b1a5caad7c9159579cd95503369e4c324dc374a3d98`）。

- OpenSandbox/Worker 前置不重复：固定 server/controller/execd/egress digest 与唯一 TokenRequest、Codex Docker Worker upgrade preflight 均沿用已保存的通过证据；本轮命令文件明确记录未重复它们。
- Codex×Docker 当前 tenant-local 模型只执行一次真实 transport/reconnect，wrapper exit `0`、cleanup 通过；MCP `14`、Skill `1`、Artifact `verified`、事件续读通过、`reconnect=passed`、`replayed=0`、撤销 `0/0/2`、旧 generation `409`、未知副作用 reconcile 通过。日志 `.tmp/mcp-skill-runtime-v1-20260926-current-gpt6-luna-docker-transport-r5.log` SHA-256 `830bb8cbf99daa4be8bd6f4553bc6df39a46e57e6c5be7e870d3dd854d3c6849`。
- Codex×Kubernetes 当前 tenant-local 模型只执行一次真实 transport/reconnect，wrapper exit `0`、task-owned cleanup 通过；MCP `26`、Skill `1`、Artifact `verified`、事件续读通过、`reconnect=passed`、`replayed=0`、撤销 `0/0/2`、旧 generation `409`、未知副作用 reconcile 通过。cross-node recovery 为 attempt `2`、`cross-node-takeover/recovered`、RPO `0`、RTO `130885 ms`；semantic snapshot digest `sha256:20bbc4fe365e6a66f29945db0ed3b50081aa0ee648c0956599be36ee17c1dc32`，raw archive SHA-256 `b0a18577cf8d9d8092f60e676e070b1e11856109f4216abc890e4fddbc333abc`，restored archive SHA-256 `f4f17acc02078b7d933fa7fba177bc6320a6a84760ed4a30201028a6e2852afe`。日志 `.tmp/mcp-skill-runtime-v1-20260926-gpt6-k8s-r3/harness.log` SHA-256 `93f5c2ab74a9af0a951a019c91de18ad1691136035dd73509f40d7bda426d3c9`，cross-node evidence SHA-256 `e12059bee2d55c74410e52b9afb796f3872be39dca2f260f22f48f9ffd8db7a9`，cleanup log SHA-256 `2c829b3a7ef8dd23c00bb0a2c9b92ce41afbbca5129fc5a8f7123a5da0a694d9`。

这两项是当前 `gpt-6-luna` 的真实 `providerAcceptance=passed` 与 `supportedClaims`，只覆盖 Codex×Docker 和 Codex×Kubernetes；其它 Provider×环境、Worker/Agent 故障和完整十二格仍按 06 的逐格状态执行，父级 `MCP-SKILL-RUNTIME-V1` Gate 保持 `OPEN`。

### 2026-09-27：当前 tenant-local 模型 Codex transport/reconnect 子路径

四份 `tenant-local.$provider.json` 的 `payload.model` 由探针动态读取；四个 Provider 的 `/v1/models` 均 HTTP 200、配置模型存在，最小 `/v1/responses` 均 HTTP 200 且有 response id。探针不记录凭据、Authorization、请求体或响应体；来源与状态见 `.tmp/mcp-skill-runtime-v1-20260927-r600-r601-evidence.json`（SHA-256 `64af8ff1e4a191880564c3099d70260fbbc5c294c17767136d5772992022661b`）。

在同一 dirty candidate `.tmp/mcp-skill-runtime-v1-20260926-r584-candidate` 上，当前 Codex tenant-local 模型的 Docker 与 OrbStack Kubernetes transport/reconnect 各执行一次：两轮 wrapper exit `0`，MCP `14`、Skill `1`、Artifact `verified`、事件续读通过、`reconnect=passed`、`replayed=0`、撤销 `0/0/2`、旧 generation `409`、未知副作用先 reconcile 后完成；日志分别为 `.tmp/mcp-skill-runtime-v1-20260927-r600-codex-docker-transport.log`（SHA-256 `45e7839900b35a12a740b07ae51a022114c6d87e9be734141054494f19a82333`）与 `.tmp/mcp-skill-runtime-v1-20260927-r601-codex-kubernetes-transport.log`（SHA-256 `194b0c15c08d0227b1c9cf5be1262c72c107e1420a6b7c2fe6b1e73d834d56d2`）。任务拥有资源均清零；Kubernetes 使用既有 OpenSandbox 资源，未删除无归属 volume。

这两条只关闭当前模型的 transport/reconnect 子路径；没有启动 Worker/Agent fault 或 capability-bound cross-node recovery，因此不改变 capability catalog supported 状态，也不关闭十二格或正式 Gate。

### 2026-09-27：Claude×RemoteWorker 当前 tenant-local 模型 recovery repair boundary

Claude×RemoteWorker 使用 `provider-credentials/tenant-local.claudeAgent.json` 动态读取当前模型；无副作用探针为 `/v1/models=200`、模型存在、`/v1/responses=200`，不记录凭据或请求/响应内容。命令记录为 `.tmp/mcp-skill-runtime-v1-20260927-r602-r604-claude-remoteworker-commands.sh`（SHA-256 `146bb4e505237fbc5be06e318f304c382327c243df267065303c71b20f49d58e`）；候选仍为 dirty `.tmp/mcp-skill-runtime-v1-20260926-r584-candidate`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`。

r602 在 source stop/snapshot/restore 后停在 `running/recovering`、attempt 2、`pendingSideEffect=true`、`pendingInteractionCount=1` 并达到 10m recovery timeout（日志 SHA-256 `69ad52b51e4077ce893373a897f49af3169bf73ff8e9e5d250637162c12d43cb`）。修复后 r603 暴露 approval driver 将 Claude Bash 强制设为 `approval-required`，r604 暴露 recovery blocked replay 与初始请求复用同一 idempotency key，导致脚本把旧请求结果误判为 replay bypass；两次修复分别统一 capability-bound approval driver、保留 provider runtime mode，以及为 blocked/final recovery 使用独立 request/idempotency key。r603 日志 SHA-256 `d339bd570348e7c5e7b8775e4dc388ef53bf8cbcda7ca4bedb89003381bce897`，r604 日志 SHA-256 `850ca816bda048678f9633055818e501c76341cdc0652dedda984b9155c73249`。

本轮真实验收不计为 Claude×RemoteWorker PASS：r604 在最新 harness 修复后未再重跑，最小验证为 `sh -n` 与 `git diff --check` 通过；证据 JSON `.tmp/mcp-skill-runtime-v1-20260927-r602-r604-claude-remoteworker-evidence.json` SHA-256 `4627c6fe11a3ed9e8b712bbea483fdbabf2ec344270b8859c47d86a25d51d80e`。不更新 capability catalog supported，不关闭 Provider Gate；下一次真实验收必须使用新 request/idempotency key 并覆盖完整 recovery 边界。r602/r603/r604 task-owned Compose 资源均由 harness 清理，未删除无归属 volume。

### 2026-09-27：deepseek-harness×RemoteWorker 与 Pi×Kubernetes 当前 tenant-local transport 边界

当前模型均由受保护 `tenant-local.$provider.json` 动态读取；不记录凭据、Authorization、请求体或响应体。deepseek-harness×RemoteWorker 的 r606 在 Docker baseline 完成 MCP/Skill/Artifact/事件续读后，Recovery Turn 以 `succeeded` 提前结束，没有 pending-side-effect checkpoint；原因是 deepseek-harness adapter 禁用 `tool-fs`、要求 `str_replace_editor`，而共享 recovery prompt 要求 Bash。日志 SHA-256 `d3209b1310082d60b6bd8a191f4f901d751150f9397b875552f722353233bd84`，evidence JSON SHA-256 `8cc3ac98df07ee927b9c37371f1b71fcd50ebdb49e8751a0944b03cb2b9f4fd2`；该单元不计 PASS。

Pi×Kubernetes 的 r607 在 OrbStack `orbstack` 上完成 Docker/Kubernetes acceptance、MCP/Skill/Artifact、事件续读、transport reconnect、`replayed=0`、撤销零请求/零副作用、旧 generation `409`、跨租户拒绝和 Compose smoke cleanup，wrapper exit 0。日志 SHA-256 `b7b4f2b395ec2961d35dd43a4f6604cf63f3c09e3aee3b6d104f660fffc2b262`，evidence JSON SHA-256 `b544a23a1b8d4ee7086fce967fa32ac17552d5b98a7fb5cc51a15d206458a99f`；本轮未启用 capability-bound/cross-node recovery，故只关闭该 transport/reconnect 子路径，不改变 capability catalog supported 或正式 Gate。

### 2026-09-27：deepseek-harness recovery prompt repair

针对 r606 的明确根因，harness 现按 deepseek-harness adapter 的 Host-managed contract 生成一次 `str_replace_editor create` recovery 调用，不再要求 Bash；`tool-fs` 仍保持禁用，artifact digest 与未知副作用对账边界不变。`sh -n`、`git diff --check` 与 `bunx vitest run packages/cloud-agent-provider-deepseek-harness/src/index.test.ts`（1 file / 7 tests）通过。r606 之后未再启动真实验收；evidence JSON 更新为 SHA-256 `6c0dbd6644a1f74c208c5c930736b4c4c6c70ee53828756fc781b20f80d7fe8c`，下一次真实运行仍需重新形成 checkpoint 后才可计数。

### 2026-09-27：deepseek-harness×Kubernetes 当前 tenant-local transport/reconnect 子路径

r608 使用 `tenant-local.deepseek-harness.json` 动态读取当前模型，在 OrbStack `orbstack`、修复后的 `NO_PROXY`/HTTPS API 路径下完成 Docker/Kubernetes acceptance：MCP 各 `8`、Skill `1`、Artifact `verified`、事件续读通过；Kubernetes transport reconnect `passed`、`replayed=0`，撤销为 `runtime_requests=0/side_effects=0/events=2`，旧 generation `409`，跨租户拒绝，wrapper exit `0`，task-owned cleanup 为 `0`。日志 `.tmp/mcp-skill-runtime-v1-20260927-r608-deepseek-kubernetes-transport.log` SHA-256 `5179a5a0976eebca6365ed1912a3d5aefc152a505121ac7cab1ec1b742394aad`，命令 SHA-256 `2445280a821e05499f0d52a5470d833838cff049a1dc7f8384b803319ad80da5`，evidence JSON SHA-256 `acc816d4dcedda01351eddeb4e2844a314dab302715047c4202dd41dac414aac`。

本轮未启用 capability-bound、Worker/Agent fault 或 cross-node takeover，因此没有 RPO/RTO、snapshot digest 或恢复后 binding 证据；该记录只关闭 transport/reconnect 子路径，不更新 capability catalog supported，不计为完整 Provider PASS，父级 Gate 继续 `OPEN`。既有 OpenSandbox 资源保留。

### 2026-09-27：Claude×RemoteWorker 当前 tenant-local 完整真实验收通过

r611 在同一 dirty candidate 上读取 `tenant-local.claudeAgent.json` 的当前模型并完成 ClaudeAgent×RemoteWorker 完整真实格，wrapper exit `0`。contract-negative 与跨租户 MCP/Skill `401` 通过；Docker baseline 为 MCP `8`、Skill `1`、Artifact `verified`、events `resumed`；RemoteWorker 恢复后为 MCP `14`、Skill `1`、Artifact `verified`、events `resumed`，事件续读通过，transport reconnect `passed`、`replayed=0`，撤销为 `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，snapshot version negative 通过。

Control Plane SIGKILL 后 source writer 已 fence，destination takeover 为 attempt `2`、`cross-node-takeover/recovered`、side effect `confirmed`、RPO `0`、RTO `9733 ms`、snapshot `349696` bytes，snapshot digest `sha256:54295560263246927de1f667b694b98ca1e7cbdbe2eecf208aeae89b847f8b56`；恢复后 Session/Sandbox 与 MCP/Skill binding 重新注入并完成 acceptance。日志 `.tmp/mcp-skill-runtime-v1-20260927-r611-claude-remoteworker.log` SHA-256 `133a440ef8bc523700b59d711f8734c818ae9fa8b470652f335a402a879ccdf1`，命令 SHA-256 `146bb4e505237fbc5be06e318f304c382327c243df267065303c71b20f49d58e`，evidence JSON SHA-256 `d47acdb22efc7085b78ca3161236d14eae79fe4c3ce171b9663b6b6b02f96901`。

该格满足完整 Provider×Environment 真实验收边界，`claudeAgent/remote-worker` 可登记为 supported；不外推到其它格，也不关闭十二格、aggregate/release/feature Gate。r602–r610 的失败与 recovery harness 修复保留在 evidence 的 prior diagnostics 中。

### 2026-09-27：deepseek-harness×RemoteWorker recovery repair boundary

r612 在 Docker baseline 完成 contract-negative、MCP/Skill/Artifact 与事件续读，但 recovery Turn 仍以 `succeeded` 结束，没有 pending-side-effect checkpoint；日志 SHA-256 `05bfb243a51ed1cf8967d49cd887ddd0eceb44bc2b5863f79b48a5d6e727a2f8`。根因从共享 Bash prompt 修复为 `str_replace_editor create` 后，r613 使用 adapter 要求的绝对 Workspace 路径，Docker baseline 与事件续读再次通过并观察到 Artifact candidate，但 create 在注入故障前完成，仍未形成 pending-side-effect checkpoint；wrapper exit `1`，日志 SHA-256 `1985d41aa3eb26c2316c5bffebca243ddbe617eaa2cadafa18997a5baba6bf5e`，命令 SHA-256 `dfd0d8c78dcea679bf96ea4469fbc4d2d616a14ea8477d335cc2982259f5c17b`。

该格保持 `supported=false / not PASS`，不把 Docker partial acceptance 写成 Provider PASS；evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-deepseek-remoteworker-evidence.json` SHA-256 `aed6d8afa21c40ad6871b42f3f458bd0e72539e56be4602a330e31815a613ae2`。r613 task-owned resources 已清理，未删除无归属 volume；后续仍需修复 recovery side-effect timing 或选择可验证的 pending checkpoint，再进行下一次受控真实单元。

### 2026-09-27：Pi×Kubernetes full recovery preflight boundary

r614 在启动 Provider 前被 fail closed：当前可复用的 `orbstack` 只有一个 Ready 节点，且本机没有 kind cluster；脚本稳定拒绝 `Kubernetes cross-node recovery requires Kubernetes Runtime, a kind cluster, and distinct source/destination nodes`，wrapper exit `2`。命令 SHA-256 `e47192cdb70cc6c3bbf90351999b7da9d58bb8b04ce4ee4b8a74b069f6c012c6`，日志 SHA-256 `d1358761342cbbcd4dc9e1497e0b0caf44c75c45926aa5feb8ab67e921c0d054`，未创建 task-owned 资源。

该格保持 `supported=false / not PASS`；r607 的 transport/reconnect 子路径仍不外推为完整 Provider supported。没有安全的现有双节点集群可复用，本轮不创建 kind 集群；缺失的 Kubernetes cross-node 环境作为独立前置 BLOCKED 记录，继续推进其它不依赖该环境的格。

### 2026-09-27：Codex×Docker 当前 tenant-local 模型完整真实格（r615）

本轮只执行一个新的 `codex/docker` Provider×Environment 单元：此前 r600 只覆盖 transport/reconnect；r615 在同一 dirty candidate 上补 capability-bound Docker process recovery。模型由受保护的 `provider-credentials/tenant-local.codex.json` 动态读取，实际模型记录为 `gpt-6-luna`；`GET /v1/models` 与最小 `POST /v1/responses` 均为 HTTP 200，配置模型存在且凭据、Authorization、请求体、响应体均未记录。候选 sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，manifest/checksums SHA-256 为 `dd3736acae3ca3c0319e6ca1b5a2669ad852908df40dd00f4c37de6a77e81a21` / `aa25c45bf1517fff72b95bd2700258aac3d41bf6d0548186254fb240833b3276`。

实际命令记录在 `.tmp/mcp-skill-runtime-v1-20260927-r615-codex-docker-bound-commands.sh`（SHA-256 `d3c40644b5166e038893163e7d4ada4c32aef7b18e0e010d5fdee3c2e65b32bd`），wrapper exit `0`；MCP `14`、Skill `1`、Artifact `verified`、事件续读通过，transport reconnect `passed`、replayed `0`，版本/digest 不兼容与跨租户拒绝通过，撤销后 Runtime 请求/副作用 `0/0` 且有 `2` 个审计事件，旧 generation 返回 `409`，未知副作用先 reconcile 后完成。Control Plane SIGKILL 后 capability-bound recovery 为 attempt `2`、`process-restart/recovered`、side effect `confirmed`、RPO `0`；Docker 不适用 cross-node snapshot，因此 snapshot/RTO 字段为 N/A/0。日志 SHA-256 `c7f16ddbe528051c1699ac244d0b7e02cb4abcac37bbdbdd476fc6ec08551156`，evidence JSON SHA-256 `b6e3f590d1f197069bd78920913268f9cb56ddcddbd5b53579090e9bff0f1960`，Compose task-owned cleanup 为 `0`。

该单元可登记为当前 `codex/docker` supported；Codex×Kubernetes 仍只有 transport 子路径或环境前置证据，不能由本轮外推。aggregate/release/feature Gate、其余 Provider×Environment、适用 Worker/Agent 与 Kubernetes cross-node recovery 继续按 06 的 Gap Ledger 保持 `OPEN`。

### 2026-09-27：ClaudeAgent×Docker 当前 tenant-local 模型完整真实格（r616）

本轮只执行一个新的 `claudeAgent/docker` 单元，补齐当前 tenant-local 模型在 Docker 上的 capability-bound process recovery。模型由 `provider-credentials/tenant-local.claudeAgent.json` 动态读取，实际模型为 `gpt-6-luna`；`/v1/models` 和最小 `/v1/responses` 探针均 HTTP 200，凭据、Authorization、请求体、响应体未记录。dirty candidate sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`，manifest/checksums SHA-256 `dd3736acae3ca3c0319e6ca1b5a2669ad852908df40dd00f4c37de6a77e81a21` / `aa25c45bf1517fff72b95bd2700258aac3d41bf6d0548186254fb240833b3276`。

命令文件 `.tmp/mcp-skill-runtime-v1-20260927-r616-claude-docker-bound-commands.sh` SHA-256 `e26ce92272f172dd3e4a96ea34975ecead0dc995cbdcf969e01f19a4b46292d7`，wrapper exit `0`；MCP `8`、Skill `1`、Artifact verified、事件续读、transport reconnect/replayed `0`、版本/digest mismatch fail closed、跨租户拒绝、撤销后 Runtime 请求/副作用 `0/0`、旧 generation `409`、未知副作用 reconcile 均通过。Control Plane SIGKILL 后 capability-bound recovery 为 attempt `2` `process-restart/recovered`、side effect `confirmed`、RPO `0`；日志 SHA-256 `bda9b840a0aa2b268d71c4675269d9550e5cdc34e78bda735a85b26f9a6ad863`，evidence JSON SHA-256 `4c77a95517470d4932acde732833503f9df9108fd6ddefad672976e21221c7db`，Compose cleanup 为 `0`。

该单元登记为当前 `claudeAgent/docker` supported；此前 r611 已独立关闭 ClaudeAgent×RemoteWorker。ClaudeAgent×Kubernetes 及其它 Provider/environment、适用 Worker/Agent 与 cross-node recovery 仍按 Gap Ledger 保持 `OPEN`。

### 2026-09-27：Pi×Docker 当前 tenant-local 模型完整真实格（r617）

r617 是当前 `pi/docker` 的一次新完整真实验收。模型由 `provider-credentials/tenant-local.pi.json` 动态读取，实际模型为 `gpt-6-luna`；`/v1/models` 与 `/v1/responses` 探针均 HTTP 200，凭据、Authorization、请求体、响应体未记录。候选仍为 dirty sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`，manifest/checksums SHA-256 `dd3736acae3ca3c0319e6ca1b5a2669ad852908df40dd00f4c37de6a77e81a21` / `aa25c45bf1517fff72b95bd2700258aac3d41bf6d0548186254fb240833b3276`。

命令文件 `.tmp/mcp-skill-runtime-v1-20260927-r617-pi-docker-bound-commands.sh` SHA-256 `1367a500029b0ed0fc22802afcffbdc81fd395c98f321ef5694f89c261945df1`，wrapper exit `0`；MCP `8`、Skill `1`、Artifact verified、事件续读、transport reconnect/replayed `0`、MCP/Skill 版本/digest mismatch fail closed、跨租户拒绝、revoke 后 Runtime 请求/副作用 `0/0`、旧 generation `409`、未知副作用 reconcile 均通过。capability-bound recovery 为 attempt `2` `process-restart/recovered`、side effect `confirmed`、RPO `0`；日志 SHA-256 `27c010cc20d50766bfbb975fe7c3cf9325c7a80618c4673b61ce4fcaaddd3222`，evidence JSON SHA-256 `62f4c0c669feb4aaaa0a67513d2436383759929ed476f1fff5e9db7cd9be3076`，Compose cleanup 为 `0`。

该单元登记为当前 `pi/docker` supported；Pi×Kubernetes 仍只有 transport 子路径与缺双节点 kind 的前置边界，不能由本轮外推。其余格与 aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：deepseek-harness×Docker 当前 tenant-local recovery BLOCKED（r618）

r618 只执行一次当前 `deepseek-harness/docker` 单元。模型动态来自 `provider-credentials/tenant-local.deepseek-harness.json`，实际模型 `gpt-6-luna`；只读探针 `/v1/models`/`/v1/responses` 均 HTTP 200，未记录凭据或请求/响应 body。Docker contract-negative、MCP `8`、Skill `1`、Artifact verified 和事件续读通过，但 capability-bound recovery Turn 在副作用完成后已为 `state=succeeded`，没有可供 fault 注入捕获的 pending-side-effect checkpoint，wrapper exit `1`。

这是 r606/r613 同一已确认根因：deepseek-harness adapter 只允许 `str_replace_editor`，单次绝对路径 create 在 Control Plane fault 前完成。已有绝对 workspace path 修复和定向 adapter/Vitest 验证，本轮最小真实重试仍失败；按 Repair First 规则记录为稳定 `BLOCKED`，不把 Docker partial、Artifact 或 readiness 计为 supported。命令 SHA-256 `fb3e6b748b0d5cd795177a55d7c5844e2dfc3509521599131e43174aa64f0804`，日志 SHA-256 `cec68273a49c5f28da380fa06751990afe5cdc2ae7cd2164e87cf724b4a55d19`，evidence JSON SHA-256 `c68cf972251dde62c1a8d88190532fb33e1d4a6c1286e3e21d3a45341c287f2c`，task-owned cleanup 为 `0`。需要可重复的 pending-side-effect checkpoint 方案后再重试；其它独立格继续推进，parent Gate 保持 `OPEN`。

### 2026-09-27：Pi×RemoteWorker 当前 tenant-local 完整 cross-node 格（r620）

r619 在 Provider 启动前因额外 Docker capability-bound 预恢复造成 project concurrent lease quota `409`，无 Provider marker；按 Repair First 关闭该无关预恢复后，r620 保留 RemoteWorker cross-node recovery 本身并完整通过。模型从 `provider-credentials/tenant-local.pi.json` 读取 `gpt-6-luna`，探针 `/v1/models`/`/v1/responses` 均 HTTP 200，凭据与 body 未记录。

r620 命令 SHA-256 `0eb32eb42f2cdd6ac3844f2dd4f0954858ea96b0640d013c53eaf29952ae3ed0`，wrapper exit `0`；Docker baseline MCP `8`、RemoteWorker MCP `14`，两侧 Skill `1`、Artifact verified、events resumed；transport reconnect/replayed `0`、版本/digest mismatch、跨租户、撤销、stale-generation 和 unknown-side-effect reconcile 通过。旧 writer fence 后 attempt `2` 为 `cross-node-takeover/recovered`，side effect `confirmed`、RPO `0`、RTO `9734 ms`、snapshot `28160 bytes`、digest `sha256:53b3c5c9a84b21e4ee3709a86dbd04571f3a9b44a310b7e092650744e152aeb5`；snapshot-version-negative 通过。日志 SHA-256 `ede3e1030e1049d26785792064e8395f85b48479191724383acfaa9442a7a131`，evidence JSON SHA-256 `c8dc58d0bdb5603327f30abfdf01cb2a74e0ac146f29dfad474274ad525ebd6c`，task-owned cleanup 为 `0`。

该单元登记为当前 `pi/remote-worker` supported；Pi×Kubernetes 仍受双节点 kind 前置限制，deepseek-harness recovery 边界保持独立，aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Codex×Kubernetes 当前 tenant-local 完整格（r621）

本轮只推进一个新的 `codex/kubernetes` Provider×Environment 单元。candidate 为 `.tmp/mcp-skill-runtime-v1-20260926-r584-candidate`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，模型由受保护的 `provider-credentials/tenant-local.codex.json` 动态读取，实际模型为 `gpt-6-luna`；`GET /v1/models` 与最小 `POST /v1/responses` 均 HTTP 200，凭据、Authorization、请求体和响应体未记录。

首次任务专属三节点 kind 前置因未预载脚本引用的 digest 固定 OpenSandbox controller/server，在 Provider 启动前 rollout 超时（日志 `.tmp/mcp-skill-runtime-v1-20260927-r621-codex-k8s.log` SHA-256 `e4f78f3c7186c76068968368063a652ac8441ff523d71b36c524b5c2a0f1df1d`），不计 Provider 结果。随后只向该任务集群三个节点预载并按 digest 建立 controller/server 引用，Worker image probe 和 TokenRequest/CRD/RBAC/ServiceAccount 前置通过；修复后命令 `.tmp/mcp-skill-runtime-v1-20260927-r621-codex-k8s-repaired-commands.sh` SHA-256 `c257495cda123598ca8be90c01d956d4309b258f477764eb791012aef0c75c49`，wrapper exit `0`。

真实结果：Docker baseline 与 Kubernetes recovery 后均完成 MCP、Skill、Artifact 和事件续读；Kubernetes acceptance MCP `26`、Skill `1`、Artifact `verified`、events `resumed`，transport reconnect `passed`/replayed `0`，版本/digest 不兼容和跨租户拒绝通过，revoke 后 Runtime 请求/副作用 `0/0` 且有 `2` 个事件，旧 generation `409`，未知副作用先 reconcile 后结算。旧 writer fence、source node 失效、destination takeover 后 attempt `2` 为 `cross-node-takeover/recovered`，side effect `confirmed`，RPO `0`、RTO `83912 ms`；raw snapshot `4003840` 字节 SHA-256 `d48217e5409298558673ef7a6b9c63d3c4862d1d0a9a40f5d02c8396df3eebcb`，semantic/restored digest `sha256:bd29f4094a46e759eba9818882a8c2fae0dc2839d6e94521017a635b8b024cb7`，restored archive SHA-256 `113300a5b03f537a9b9e207a91afe555aa68e1e4a3bc9e0e28132394a3522076`，两节点为 `ca-mcp-r621-codex-worker`/`ca-mcp-r621-codex-worker2`。恢复后 Workspace/Sandbox 与 MCP/Skill binding 重新注入并完成 acceptance。

修复后日志 `.tmp/mcp-skill-runtime-v1-20260927-r621-codex-k8s-repaired.log` SHA-256 `72bb9262d19b79d8f78545933ac2b9c98d8747a89cd8f0974ecb0de5b41a5eb7`，cross-node evidence JSON SHA-256 `91880ed8c3d044479fe2cec6c7c029076810db041bc49b67e0c5a986c10b9477`，snapshot archive SHA-256 `d48217e5409298558673ef7a6b9c63d3c4862d1d0a9a40f5d02c8396df3eebcb`；固定 OpenSandbox images 为 server `sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb`、controller `sha256:a9a5f73c1785ebd955336ffa313973a35c1a1b662cb7afc4ea82d92021b3532a`、execd `sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd`、egress `sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a`。task-owned Compose/Kind namespace/Pod/PVC/cluster/containers 清理为 `0`；既有 OrbStack OpenSandbox controller 与无归属 volume 未删除。

该单元现在可登记为当前 `codex/kubernetes` supported；这只关闭该 Provider×Environment 格，不能外推到其它 Provider、Worker/Agent fault 或十二格 Gate。aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Pi×Kubernetes 当前 tenant-local 完整格（r622）

r622 是当前 `pi/kubernetes` Provider×Environment 的一次完整真实验收。模型由受保护的 `provider-credentials/tenant-local.pi.json` 动态读取，实际模型为 `gpt-6-luna`；`GET /v1/models` 与最小 `POST /v1/responses` 均 HTTP 200，凭据、Authorization、请求体和响应体未记录。候选 sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true。

三节点 kind 任务集群预载固定 OpenSandbox controller/server digest 后，TokenRequest、CRD、RBAC、ServiceAccount、Worker image probe 和 Kubernetes API/NO_PROXY 前置通过。命令 `.tmp/mcp-skill-runtime-v1-20260927-r622-pi-k8s-commands.sh` SHA-256 `053297aba259b3ad04c287c354505e61649b4b9ec88e40aeb70c7ebb3d6d7e6c`，wrapper exit `0`；Kubernetes acceptance MCP `14`、Skill `1`、Artifact `verified`、events `resumed`，transport reconnect `passed`/replayed `0`，版本/digest 不兼容、跨租户拒绝、revoke `0/0` 且有 `2` 个事件、旧 generation `409`、未知副作用先 reconcile 后结算均通过。

old writer fence、source node 失效、destination takeover 后 attempt `2` 为 `cross-node-takeover/recovered`，side effect `confirmed`，RPO `0`、RTO `82529 ms`；raw snapshot `40960` 字节 SHA-256 `39bb2567799b6bb5cd5a31f45c5e7398e9827ffb86db45a76943e7843ae8ee27`，semantic/restored digest `sha256:31cd578ba54dab122f8bf3239f97108999b4cb7c14c67333ef796e40c233498d`，restored archive SHA-256 `0078d854a832da615c8c4c51e38e599c018ba74e2b50cad93d5c5726c926b260`；恢复后 Workspace/Sandbox 与 MCP/Skill binding 重新注入并完成 acceptance。日志 SHA-256 `e2470e5380057dc8f157ba09e4ba392c3a729e3069955b8c5684e9a9bdc24035`，cross-node evidence JSON SHA-256 `8b2d417e0a7ab8beeaf5427bb33d6793724c583a88ce0509429db250b1b49b8d`，task-owned Compose/Kind namespace/Pod/PVC/cluster/containers 清理为 `0`；既有 OrbStack OpenSandbox controller 与无归属 volume 未删除。

该单元登记为当前 `pi/kubernetes` supported；不能外推到其它 Provider、未完成 Worker/Agent fault 或 aggregate/release/feature Gate，后者继续 `OPEN`。

### 2026-09-27：ClaudeAgent×Kubernetes 当前 tenant-local 完整格（r623）

r623 完成当前 `claudeAgent/kubernetes` Provider×Environment 一次完整真实验收。candidate 为 `.tmp/mcp-skill-runtime-v1-20260926-r584-candidate`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；模型由受保护的 `provider-credentials/tenant-local.claudeAgent.json` 动态读取，实际模型为 `gpt-6-luna`。`GET /v1/models` 与最小 `POST /v1/responses` 均 HTTP 200，配置模型存在；凭据、Authorization、请求体和响应体未记录。

三节点 kind 任务集群预载固定 OpenSandbox controller/server digest 后，TokenRequest、CRD、RBAC、ServiceAccount、Worker image probe 与 Kubernetes API/NO_PROXY 前置通过。固定 image digest 为 server `sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb`、controller `sha256:a9a5f73c1785ebd955336ffa313973a35c1a1b662cb7afc4ea82d92021b3532a`、execd `sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd`、egress `sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a`。命令 `.tmp/mcp-skill-runtime-v1-20260927-r623-claude-k8s-commands.sh` SHA-256 `e413f78953c6c00e1e625d40032399d4b022bce4f8490f87d1ff4edd5e5ba951`，wrapper exit `0`。

Docker baseline 与 Kubernetes recovery 后均完成 MCP、Skill、Artifact 和事件续读；Kubernetes acceptance MCP `14`、Skill `1`、Artifact `verified`、events `resumed`，transport reconnect `passed`/replayed `0`，版本/digest 不兼容和跨租户拒绝通过，revoke 后 Runtime 请求/副作用 `0/0` 且有 `2` 个事件，旧 generation `409`，未知副作用先 reconcile 后结算。old writer fence、source node 失效、destination takeover 后 attempt `2` 为 `cross-node-takeover/recovered`，side effect `confirmed`，RPO `0`、RTO `83756 ms`；raw snapshot `389120` 字节 SHA-256 `fb222fce1c15d6b48baf87d9951f4bb9358237dca6c3118854b61a3b5800a562`，semantic/restored digest `sha256:2abcd4ee1bd0d5efe7599286d0cb20ec8d1d29205b454675a25c816b0183c1bb`，restored archive SHA-256 `c7e79ee47ea8be66f2ba86ec0096aaf49930ec7f99686941030eca1676b46924`；恢复后 Workspace/Sandbox 与 MCP/Skill binding 重新注入并完成 acceptance。

日志 `.tmp/mcp-skill-runtime-v1-20260927-r623-claude-k8s.log` SHA-256 `6515af7e678ebc9abc2236db0a80d7b69f356aba2697ae0877ea1d654e8059da`，cross-node evidence JSON SHA-256 `fd95153123352a9868b9575bb03b3f8127b9d062b3dcadf4ad5c99c395b3c39b`，Kind delete log SHA-256 `a0a1c7b3e0a2ee8da5d7d5c94fe7d79eaf5ad14cdff138346fa257e4476860da`。task-owned Compose/Kind namespace/Pod/PVC/cluster/containers 清理为 `0`；`kind get clusters` 为空，既有 OrbStack OpenSandbox controller 保持 Running，未删除无归属 volume。

该单元现在可登记为当前 `claudeAgent/kubernetes` supported；这只关闭该 Provider×Environment 格，不能外推到 deepseek-harness recovery 或其它未闭合格。aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Codex×RemoteWorker 当前 tenant-local 完整格（r624）

r624 补做当前模型来源修正后的 `codex/remote-worker` Provider×Environment 完整真实验收。candidate 为 `.tmp/mcp-skill-runtime-v1-20260926-r584-candidate`，version `0.3.0-dev.584`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；模型由受保护的 `provider-credentials/tenant-local.codex.json` 动态读取，实际模型为 `gpt-6-luna`。无副作用探针 `/v1/models=200` 且 configured model present，`/v1/responses=200` 且有 response id，未记录凭据、Authorization、请求体或响应体。manifest/checksums SHA-256 为 `dd3736acae3ca3c0319e6ca1b5a2669ad852908df40dd00f4c37de6a77e81a21` / `aa25c45bf1517fff72b95bd2700258aac3d41bf6d0548186254fb240833b3276`。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r624-codex-remoteworker-commands.sh` SHA-256 `c2b35cac91b13135b8501a0ffea738a2d35d87d32db5db61041484de4f0eb8a7`，wrapper exit `0`，日志最终 `platform Compose smoke passed`。Worker image 为 `cloud-agents-worker:r572-precheck-20250925@sha256:573196d95aeac01fcf5d1b81235a48ed2379ccd56fd3f1cb6a922457f6fba9fc`；本轮未发布或替换 RemoteWorker package。

Docker baseline 与 RemoteWorker recovery 后均完成 MCP、Skill、Artifact 和事件续读；Docker MCP `14`、RemoteWorker MCP `28`，两侧 Skill `1`、Artifact `verified`、events `resumed`；transport reconnect `passed`/replayed `0`，版本/digest 不兼容和跨租户拒绝通过，revoke 后 Runtime 请求/副作用 `0/0` 且有 `2` 个事件，旧 generation `409`，snapshot-version-negative 通过，未知副作用先 reconcile 后结算。old writer fence、source node 失效、destination takeover 后 attempt `2` 为 `cross-node-takeover/recovered`，side effect `not-applied`，RPO `0`、RTO `10791 ms`，snapshot `3876352` 字节，digest `sha256:a5b3a2838bcc136763333ae0a2261eb1d797c1920d8b45b3c165a2ade4119998`。

日志 `.tmp/mcp-skill-runtime-v1-20260927-r624-codex-remoteworker.log` SHA-256 `fc991d41164f97fd533a374de9a9cdeb98ca060ad5d7ecb5cf38d5fe071bdbbd`，evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r624-codex-remoteworker-evidence.json` SHA-256 `598a0722bda43a5f897ab50a550c5e72d7b4ed9188f90358b9d1c15913522843`。Compose、RemoteWorker、OpenSandbox owned resources 清理为 `0`，未删除无归属 volume。

该单元现在可登记为当前 `codex/remote-worker` supported；这是模型来源修正后的新证据，不外推到 deepseek-harness 未闭合格、其它 fault cells 或正式 Gate。aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：deepseek-harness×RemoteWorker 当前模型完整格（r641）

r641 在修复 recovery timing 后完成当前 `deepseek-harness/remote-worker` Provider×Environment 单元。r639 已通过主路径但未形成 pending checkpoint；根因进一步定位为测试 delay 只覆盖文件工具，而 recovery 先返回 managed MCP/Skill result。修复将受限数值 delay 传入共享 `FoundationRuntime` 启动命令，并在该测试环境变量存在时延迟所有 managed tool result；默认环境变量为空，不改变正常运行。定向 Vitest `2 files / 43 tests`、managedagent Go 单测、`sh -n` 与 `git diff --check` 通过。

candidate version `0.3.0-dev.640`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`，sourceDirty=true；模型从受保护 `provider-credentials/tenant-local.deepseek-harness.json` 动态读取，`/v1/models=200` 且配置模型存在，`/v1/responses=200` 且有 response id。Worker image `cloud-agents-worker:local-r640`，image ID `sha256:5c6706082b554eed8352eb84b30e8b6fc7740383dc96645fbe2fc84aee93ddf4`。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r641-deepseek-remoteworker-commands.sh` SHA-256 `3f43f0bbfff32b058c8bfd316d87afa8911220952ba6251096417534df07424e`，wrapper exit `0`。Docker baseline MCP `8`、RemoteWorker MCP `16`，两侧 Skill `1`、Artifact `verified`、事件续读通过；transport reconnect `passed`/replayed `0`，版本/digest mismatch、跨租户 `401`、撤销 `runtime_requests=0/side_effects=0/events=2`、stale generation `409`、unknown-side-effect 先 reconcile 后继续均通过。old writer fence、source node 失效、destination takeover 后 attempt `2` 为 `cross-node-takeover/recovered`，side effect `not-applied`，RPO `0`、RTO `8611 ms`，snapshot `848896` bytes，snapshot digest `sha256:370e0320d232b07c0b90aebade7e1ff9ae61bd851b73a9fb7814c72740380982`；恢复后 MCP/Skill binding 重新注入并完成 acceptance。

日志 SHA-256 `c9749626c897297af4212cc94a38d44ea6e0913d6de802da21865100cd20d88a`，evidence JSON SHA-256 `d1f14d5b440030d5526cc13ce10df63d40c580baad02d0fb8d2693167999f228`，cleanup 为 `0`。该格现在登记为当前 `deepseek-harness/remote-worker` supported；deepseek Docker/Kubernetes、适用 Worker/Agent faults 和 aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：deepseek-harness×Docker 当前 tenant-local 完整格（r656）

r656 完成当前模型来源修正后的 `deepseek-harness/docker` Provider×Environment 单元。模型由受保护的 `provider-credentials/tenant-local.deepseek-harness.json` 动态读取，实际模型为 `gpt-6-luna`；`/v1/models=200` 且 configured model present，`/v1/responses=200` 且有 response id，未记录凭据、Authorization、请求体或响应体。candidate `0.3.0-dev.652`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；Worker image `cloud-agents-worker:local-r654`，image ID `sha256:1d37c4a47290d7938d64d904a90bc2af3d461b070759a82a118bb95c2019924b`。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r656-deepseek-docker-bound-commands.sh` SHA-256 `3b1de336d353f774f5ea80551225ff852ae0f8bde69bae1f2674535e506a0228`，wrapper exit `0`，最终 Compose smoke 通过。Docker baseline MCP `8`、Skill `1`、Artifact verified、events resumed；transport reconnect passed/replayed `0`，版本/digest 不兼容和跨租户拒绝通过，revoke 后 Runtime 请求/副作用 `0/0` 且有 `2` 个事件，旧 generation `409`，未知副作用先 reconcile 后 replay。Control Plane SIGKILL 后 capability-bound recovery 为 attempt `2`、`process-restart/recovered`、side effect `confirmed`、RPO `0`；Docker 不适用 cross-node snapshot，RTO/snapshot 为 `0/0`。日志 SHA-256 `37e63746598f0ec583dce657bc646823b5a4543fe909bb33e843a58d4ffe3cc9`，evidence JSON SHA-256 `ce457c003da8ed0637484d6053446a2d5924ce142f39c8d5736ce8ee9b844e9a`，task-owned cleanup 为 `0`。

本轮修复将 recovery delay 限定为 prompt 明确包含 recovery 的测试请求，避免恢复后普通 baseline 受延迟影响；默认未设置 delay 时生产行为不变。该单元登记为当前 `deepseek-harness/docker` `supported=true / PASS`；deepseek-harness×Kubernetes、适用 Worker/Agent fault matrix 与 aggregate/release/feature Gate 仍保持 `OPEN`。


### 2026-09-27：deepseek-harness×Kubernetes 当前 tenant-local 完整格（r659）

r659 完成当前 `deepseek-harness/kubernetes` Provider×Environment 单元。模型从受保护的 `provider-credentials/tenant-local.deepseek-harness.json` 动态读取，实际为 `gpt-6-luna`；`GET /v1/models` 与最小 `POST /v1/responses` 均 HTTP 200，配置模型存在且返回 response id，凭据、Authorization、请求体和响应体未记录。candidate version `0.3.0-dev.659`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；manifest/checksums SHA-256 为 `dc6e67a3822e044081dda114a748c9aae9091d64cc07043b7cc484923a2b909f` / `64066e80705b030e97b379f72ef8940b16159deed4b797d86b42608196de64c9`。

本轮修复将受限的 deepseek recovery delay 传入 Kubernetes Worker Deployment 和 Runtime 启动命令，并延长 pending checkpoint 轮询窗口；定向 managedagent/kubernetestarget Go 测试、deepseek Vitest、`sh -n` 和 `git diff --check` 通过。固定 OpenSandbox digest、TokenRequest、CRD、RBAC、ServiceAccount 与 Kubernetes API/NO_PROXY 前置通过。Worker image `cloud-agents-worker:local-r659` image ID `sha256:81b2a62534a3e672d3464747a164415881e184d3215e6cf4603f502632e12a83`，Worker arm64 SHA-256 `c65e8f643c3ae33f7bee7d8015299dcd9325072e65ae25708a94b9dd5caaa944`，runtime SHA-256 `dab22f40ca7492f083dea970f8a0ffa8cee0a8bccc08c0ee07c13edc4e02eeb5`。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r659-deepseek-k8s-commands.sh` SHA-256 `a7a2e34b9caa47b1791e38ef2597a828fbfb03be66828f5834894c0a5c15931a`，wrapper exit `0`。Docker baseline MCP `8`、Kubernetes MCP `16`，Skill `1`、Artifact `verified`、events `resumed`；transport reconnect `passed`/replayed `0`，版本/digest 不兼容和跨租户拒绝通过，revoke 后 Runtime 请求/副作用 `0/0` 且有 `2` 个事件，旧 generation `409`，未知副作用先 reconcile 后继续。

old writer fence、source node 失效、destination takeover 后 attempt `2` 为 `cross-node-takeover/recovered`，side effect `confirmed`，RPO `0`、RTO `83828 ms`；snapshot `890880` 字节，raw SHA-256 `386e3402255b566a60faf6687d17b2041996acf6bfd5ff1184a3d0843c9ab8f9`，semantic/restored digest `sha256:4ea81d24471a51114e4a190e35118b42f796620d23f6319c14c704fb49ab87d6`，restored archive SHA-256 `23f0c1d42db16a6713e2aa1c5472bf8450e0ddf9aebee3ce9a6703c0d20445d3`；恢复后 Workspace/Sandbox 与 MCP/Skill binding 重新注入并完成 acceptance。日志 SHA-256 `780f340718eefc80582c9f267b32efb38925f379764a3aac5695e072f279f178`，cross-node evidence JSON SHA-256 `16fdf64191b92547f7a933b65fbd8227feeec0e85a1c608f26ef493389e87fc8`，脱敏 evidence JSON SHA-256 `17dacdff038a8a95aba10df2bf40089c3d23e9a556a4d926afedfb773f2f585b`，task-owned cleanup 为 `0`。

该单元现在登记为当前 `deepseek-harness/kubernetes` `supported=true / PASS`，从而十二个 Provider×Environment 单元均有独立当前模型真实证据；这不关闭适用 Worker/Agent fault cells 或 aggregate/release/feature Gate，后者继续 `OPEN`。


### 2026-09-27：deepseek-harness×Docker Worker fault BLOCKED（r660）

r660 只推进一次当前 `deepseek-harness/docker` Worker fault 单元，模型仍从受保护的 `tenant-local.deepseek-harness.json` 读取 `gpt-6-luna`，无副作用探针为 `/v1/models=200`（模型存在）和 `/v1/responses=200`（有 response id）。contract-negative 通过；fault 前 MCP/Skill 已观察到，但该故障单元不把 partial acceptance 计为 Provider PASS。

Repair First 先修正 DeepSeek 与 recovery 工具契约：DeepSeek 的系统提示禁止 shell，因此恢复提示改为绝对 Workspace 路径的 `str_replace_editor create`；随后增加受限 delay hook、source-name fallback，并重建 candidate/Worker。三次真实尝试仍在 `str_replace_editor` 完成后结束，均未形成 `pendingSideEffect=true` 或 active Bash checkpoint；没有进入 Worker takeover、reconcile、attempt 2 或 recovery marker，未盲目 replay。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r660-deepseek-docker-worker-fault-evidence.json` SHA-256 `397a20d3ec7b986932629810cc481f7b2d303f6c87fe92ab024e757da6750cc8`；最终 candidate `0.3.0-dev.663`，manifest/checksums SHA-256 `ccab33ef46f4e533b4f6aafd2b2a68c0ef3fcbaf1f8a1a79e3e91d7d46c42dc1` / `ba5d6bd07dc2209007259121c0cdc46e45fe12aba98a3b8fb913ef6ec2201ca9`，Worker image `cloud-agents-worker:local-r663` image ID `sha256:b9ca3a5188477ab8a7a31da092aef14358b278cfea9f6ac72bf3e2a4ec085843`。最终命令/log SHA-256 为 `d07b726c239353af657a6b562ae9d5dfd5e3d5a925b3395f3f515b9b0bc007ad` / `b75f76274f7a53715653e4a31fa2e8cba2ccd3e71bf4d3f7ec0d5573fc69e69e`，wrapper exit `1`，cleanup 为 `0`。

该 fault cell 记录为 `BLOCKED`，不改变十二个 Provider×Environment 当前模型 PASS，也不把 partial acceptance 或 readiness 写成 supported；需要可观察、可控的 pending-side-effect checkpoint 后再重试。其它独立 Worker/Agent fault cells 和 aggregate/release/feature Gate 继续 `OPEN`。


### 2026-09-27：Pi×Docker Worker fault 当前模型通过（r664）

r664 在修复并预拉取 `gcr.io/distroless/static-debian12:nonroot` 前置后，完成当前 `pi/docker` Worker fault 单元。模型从受保护的 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`；`/v1/models=200` 且配置模型存在，`/v1/responses=200` 且有 response id，未记录凭据或 body。初次运行只在镜像 EOF 预检停止，不计 Provider 结果；预拉取成功后才执行唯一一次真实 fault 单元。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r664-pi-docker-worker-fault-commands.sh` SHA-256 `e6fd526e3fe103e2155d02843d31cf44bd82e36bfaf67454653b131f52762d48`，wrapper exit `0`；candidate `0.3.0-dev.663`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，manifest/checksums SHA-256 `ccab33ef46f4e533b4f6aafd2b2a68c0ef3fcbaf1f8a1a79e3e91d7d46c42dc1` / `ba5d6bd07dc2209007259121c0cdc46e45fe12aba98a3b8fb913ef6ec2201ca9`。Worker image `cloud-agents-worker:local-r663` image ID `sha256:b9ca3a5188477ab8a7a31da092aef14358b278cfea9f6ac72bf3e2a4ec085843`，Worker arm64 SHA-256 `8ccf720dfb20fd34629651e0f8628421638e1c78acc54d4675648ffae8c933e5`，runtime SHA-256 `f7a2eeb5dde2fd251dc90e3f3982e42de5791cb6eb75f35eaf93ddea893ec867`。

Docker acceptance MCP `8`、Skill `1`、Artifact `verified`、events `resumed`；transport reconnect `passed`/replayed `0`，版本/digest 不兼容和跨租户拒绝通过，revoke 后 Runtime 请求/副作用 `0/0` 且有 `2` 个事件，旧 generation `409`，未知副作用先对账。Worker fault recovery 形成 pending side-effect checkpoint，side effect 对账为 `confirmed`，attempt `2` `process-restart/recovered`，RPO/RTO `0/0`；日志 SHA-256 `60e42418a282d5c17bbf5e34a1b0a8d43479aba3dcaaf51a008dbc460d7ff111`，脱敏 evidence JSON SHA-256 `a0e5df35c204a193cf594c2b3834c66b6afb18c94e18ae32fa4bd0e5b077fa32`，task-owned cleanup 为 `0`。

该单元登记为当前 `pi/docker` Worker fault `PASS`；r660 DeepSeek Worker fault 仍独立 `BLOCKED`，其它适用 Worker/Agent fault cells 与 aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Pi×Docker Agent fault BLOCKED（r665）

r665 推进当前 `pi/docker` Agent Runtime fault 单元。模型从受保护 `provider-credentials/tenant-local.pi.json` 动态读取，实际模型为 `gpt-6-luna`；`/v1/models=200` 且配置模型存在，`/v1/responses=200` 且有 response id；contract-negative 通过，未记录凭据、Authorization、请求体或响应体。candidate `0.3.0-dev.663`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r665-pi-docker-agent-fault-commands.sh` SHA-256 `0dab09b3f34de3b4ce67876fcbc1c71137eaa6fac579e826298a3d99156505fd`，日志 `.tmp/mcp-skill-runtime-v1-20260927-r665-pi-docker-agent-fault.log` SHA-256 `4126035581f052dbf143ba21d6ad86836932bec3431d3be7b885444fb9de9349`，wrapper exit `1`。Execution 创建并进入 running/checkpoint sequence `1`，随后 Agent Runtime fault recovery 未完成 attempt `2`；日志只形成 `capability process recovery failed provider=pi`，没有 `process_side_effect_recovery=passed`、recovered marker、reconcile 结论或 Provider acceptance PASS。未盲目 replay。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r665-pi-docker-agent-fault-evidence.json` SHA-256 `8a93580d9f35a713d63678a4d8f70543eab8deb672b7f16aa8339182045b0a1e`；task-owned Compose/OpenSandbox cleanup 为 `0`。该 Agent fault cell 记录为 `BLOCKED`，不改变 `pi/docker` Provider×Environment PASS，也不把 partial checkpoint 写成 supported；需要可观察的 Agent Runtime process recovery、attempt `2` 和完成态证据后再重试。其它独立 fault cells 与 aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Codex×Docker Agent fault BLOCKED（r666）

r666 推进当前 `codex/docker` Agent Runtime fault 单元。模型从受保护 `provider-credentials/tenant-local.codex.json` 动态读取，实际模型为 `gpt-6-luna`；`/v1/models=200` 且配置模型存在，`/v1/responses=200` 且有 response id；contract-negative 通过，未记录凭据、Authorization、请求体或响应体。candidate `0.3.0-dev.663`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r666-codex-docker-agent-fault-commands.sh` SHA-256 `46a7d8218bf5d78f64bca65b0bc761187cb55c50c022d9a4e3f4950ec8eef23e`，日志 SHA-256 `794b6d2a0b7b0926e8351e4547278dc4dcede8fd21c9bd5c93cf31b721f00f89`，wrapper exit `1`。Execution 创建并进入 interaction checkpoint polling，随后 Agent Runtime fault recovery 未完成 attempt `2`；日志只形成 `capability process recovery failed provider=codex`，没有 recovered marker、reconcile 结论或 Provider acceptance PASS。未盲目 replay。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r666-codex-docker-agent-fault-evidence.json` SHA-256 `612244753665a1c80eec81faf25c5d301fc5a2765145a90e184a2b5bc0bcf4f3`；task-owned Compose/OpenSandbox cleanup 为 `0`。该 Agent fault cell 记录为 `BLOCKED`，不改变 `codex/docker` Provider PASS；需要可观察的 Agent Runtime process recovery、attempt `2` 和完成态证据后再重试。其它独立 fault cells 与 aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Codex×Docker Worker fault BLOCKED（r667）

r667 推进当前 `codex/docker` Worker fault 单元。模型从受保护 `provider-credentials/tenant-local.codex.json` 动态读取，实际模型为 `gpt-6-luna`；`/v1/models=200` 且配置模型存在，`/v1/responses=200` 且有 response id；contract-negative 通过。candidate `0.3.0-dev.663`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，未记录凭据、Authorization、请求体或响应体。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r667-codex-docker-worker-fault-commands.sh` SHA-256 `896e04751c675a30a36da3b18d9411f46fb62e8256c446844695ba55fa3d045c`，日志 SHA-256 `3b1e7bf098cf6ece6596d7c3972a93f6dd3a05477c855f479d9a93a2ffed1eeb`，wrapper exit `1`。Worker fault 已到达 attempt `2` / `recoveryState=recovered`，但恢复执行返回 `provider_unavailable`，稳定错误类别为 `mcp`，未形成完整 acceptance PASS；没有盲目 replay。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r667-codex-docker-worker-fault-evidence.json` SHA-256 `be01e2d4b0740b8741ca892052b708a82738dc10617a8401974a59be5732bed5`；task-owned Compose/OpenSandbox cleanup 为 `0`。该 fault cell 记录为 `BLOCKED`，不改变 `codex/docker` Provider PASS；需修复并重新验证恢复后的 MCP 路径，aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Codex×Docker Worker fault 修复后 fault sub-cell 通过（r668；整体运行未收口）

针对 r667 的根因修复了 Docker Worker fault 后的 MCP fixture network namespace：Worker 重启后无论交互或 side-effect checkpoint 模式都重启 task-owned fixture，使恢复后的 Codex MCP 路径指向新 Worker。`sh -n` 与 `git diff --check` 通过。

r668 仍使用当前 `tenant-local.codex.json` 模型和 candidate `0.3.0-dev.663`；命令 SHA-256 `896e04751c675a30a36da3b18d9411f46fb62e8256c446844695ba55fa3d045c`，日志 SHA-256 `f08f0d48aabf153106b3f9b33075b8c5b06f9939c199fb137894525c3ffeb04d`。Worker fault-specific evidence 为 `worker_exit_survival=passed`、`capability_process_recovery=passed`、attempt `2`、`recoveryState=recovered`、`recoveryMode=process-restart`；同一 fault phase 的 MCP `14`、Skill `1`、Artifact `verified`、events `resumed` 通过。脱敏 evidence JSON SHA-256 `484697403276dc83562cd24700d61399ed56fc5c258df43f08279b269e666ce5`，task-owned cleanup `0`。

随后脚本在独立的正常 Codex follow-up 交互轮询中无终态并被停止，未覆盖 transport recovery、revoke-negative、stale-generation 和最终 wrapper success。因此 r668 只记为 `fault sub-cell PASS / overall run incomplete`，不提升为 Provider PASS 或 formal Gate closure；r667 的 MCP recovery blocker 已由修复后的 fault-specific 证据消除，但其余覆盖边界仍 `OPEN`。

### 2026-09-27：Pi×Docker Agent fault 修复后通过（r669）

r665 的根因是 Runtime 由 `#!/usr/bin/env node` 脚本启动，原 Agent fault kill guard 只接受 `/proc/$pid/cmdline` 首 token 为 `/usr/local/bin/cloud-agent-runtime`，误拒绝解释器 cmdline。修复为在受 PID file 限制下检查 cmdline 含精确 `/usr/local/bin/cloud-agent-runtime` token；`sh -n` 与 `git diff --check` 通过。

r669 使用当前 `tenant-local.pi.json` 模型与 dirty candidate `0.3.0-dev.663`；模型探针 `/v1/models=200`、configured model present、`/v1/responses=200`、response id present。命令 `.tmp/mcp-skill-runtime-v1-20260927-r669-pi-docker-agent-fault-repaired-commands.sh` SHA-256 `0dab09b3f34de3b4ce67876fcbc1c71137eaa6fac579e826298a3d99156505fd`，日志 SHA-256 `dd3c0f23b2a0c26f904364b17c939a2a0ae7e29aa8decd8a2645ddf03d7f5677`，wrapper exit `0`。Agent fault process-side-effect recovery、attempt `2`、`recoveryState=recovered`、`recoveryMode=process-restart`、side-effect reconciliation `confirmed`；MCP `8`、Skill `1`、Artifact `verified`、events `resumed`，transport reconnect `passed`/replayed `0`，revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `1→2/409`，Compose smoke 通过。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r669-pi-docker-agent-fault-repaired-evidence.json` SHA-256 `26a642df08a839fc49909beab45e738d2f040a7aa7add9277bf4c412b0cfb5b2`；task-owned cleanup 为 `0`。该 fault cell 登记为 `PASS`，只覆盖 Pi×Docker Agent fault；Codex Agent fault、Codex Worker fault 的独立边界和其它 faults/Gates 继续按各自证据处理。

### 2026-09-27：Codex×Docker Agent fault 修复后通过（r670）

r666 的 Agent fault 失败根因与 Pi 一致：Runtime 是 `#!/usr/bin/env node` 脚本，旧 kill guard 误拒绝解释器 cmdline。修复为在受 PID file 限制下检查 cmdline 含精确 `/usr/local/bin/cloud-agent-runtime` token；`sh -n` 与 `git diff --check` 通过。

r670 使用当前 `tenant-local.codex.json` 模型与 dirty candidate `0.3.0-dev.663`；模型探针 `/v1/models=200`、configured model present、`/v1/responses=200`、response id present。命令 `.tmp/mcp-skill-runtime-v1-20260927-r670-codex-docker-agent-fault-repaired-commands.sh` SHA-256 `46a7d8218bf5d78f64bca65b0bc761187cb55c50c022d9a4e3f4950ec8eef23e`，日志 SHA-256 `a1cfa4c90806ee2aa9cdf6fcb085f5a8ac607d0ca0007587c0d00b5669cd5850`，wrapper exit `0`。Agent process exit recovery、attempt `2`、`recoveryState=recovered`、`recoveryMode=process-restart`；MCP `14`、Skill `1`、Artifact `verified`、events `resumed`，transport reconnect `passed`/replayed `0`，revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `1→2/409`，Compose smoke 通过。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r670-codex-docker-agent-fault-repaired-evidence.json` SHA-256 `29248a0b95e55bb1cd96163a754dbf5c1f0052e1bd95b9582060d50e3d620bec`；task-owned cleanup 为 `0`。该 fault cell 登记为 `PASS`，只覆盖 Codex×Docker Agent fault；Codex Worker fault 的独立整体边界、DeepSeek Worker fault 和其它 faults/Gates 继续按各自证据处理。

### 2026-09-27：ClaudeAgent×Docker Worker fault BLOCKED（r671）

r671 推进当前 `claudeAgent/docker` Worker fault 单元。模型从受保护 `provider-credentials/tenant-local.claudeAgent.json` 动态读取，实际模型为 `gpt-6-luna`；`/v1/models=200` 且配置模型存在，`/v1/responses=200` 且有 response id；contract-negative 通过，未记录凭据、Authorization、请求体或响应体。candidate `0.3.0-dev.663`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r671-claude-docker-worker-fault-commands.sh` SHA-256 `e3956164ce2a735b42a59e2d7648377f61b00580c1b4a913dbeb11502abb87d4`，日志 SHA-256 `0f20813977557f0fd22261072720ef4b63922bce45b4c997b87efce9fb9dd50c`，wrapper exit `1`。Worker fault target 已重启，恢复阶段最终 marker 校验失败并输出 `recovered Worker execution result changed`；未形成完整 fault acceptance PASS，未盲目 replay。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r671-claude-docker-worker-fault-evidence.json` SHA-256 `f6563a0c386bdeea8499576722fe13d8d59fc93a47225fd87a64635fcaea81a0`；task-owned Compose/OpenSandbox cleanup 为 `0`。该 fault cell 记录为 `BLOCKED`，不改变 `claudeAgent/docker` Provider PASS；需诊断 marker 丢失根因并重新取得完整恢复证据，aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：ClaudeAgent×Docker Agent fault 修复后通过（r672）

r672 使用共享 Agent Runtime PID guard 修复：在 PID file 限制下接受 interpreter-backed `/usr/local/bin/cloud-agent-runtime` cmdline token；`sh -n` 与 `git diff --check` 通过。当前模型从受保护 `provider-credentials/tenant-local.claudeAgent.json` 动态读取，探针 `/v1/models=200`、configured model present、`/v1/responses=200`、response id present。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r672-claude-docker-agent-fault-commands.sh` SHA-256 `30d8213695468fe25c062d4d54209d073600fe64411d52c0997d7f333d972582`，日志 SHA-256 `2c1f17bddae32addc637a09437dd6e6fbe344ef8b0beabd83ad8e6be067bb72e`，wrapper exit `0`。Agent process exit recovery、attempt `2`、`recoveryState=recovered`、`recoveryMode=process-restart`；MCP `8`、Skill `1`、Artifact `verified`、events `resumed`，transport reconnect `passed`/replayed `0`，revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `1→2/409`，Compose smoke 通过。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r672-claude-docker-agent-fault-evidence.json` SHA-256 `c0becc42484806ca19c7867fc0eab1befe291d07eab2f1d55ba3defb6c8561e5`；task-owned cleanup 为 `0`。该 fault cell 登记为 `PASS`，只覆盖 ClaudeAgent×Docker Agent fault；Claude Worker fault 的 marker blocker、其它 faults/Gates 继续按各自证据处理。


### 2026-09-27：Pi×RemoteWorker Worker fault BLOCKED（r673）

r673 使用 dirty candidate `0.3.0-dev.663`；模型从受保护的 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`。无副作用模型探针和 contract-negative 已通过。该单元命令 SHA-256 `31f8a0dfcbbaa379b9441a5508ac26a91782c397725c968b365134b833a137cc`，日志 SHA-256 `986f84f132a79a41e57aaacfee22bdade71e4aa0bc84d90a7d10bd78704a53da`，wrapper exit `1`。真实 RemoteWorker Worker fault 执行在故障注入前未形成 capability-bound `pendingSideEffect` checkpoint，等待超时后关闭；未进入 Worker takeover、attempt 2、reconcile 或 replay，故障证据不计 PASS。

脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r673-pi-remoteworker-worker-fault-evidence.json` SHA-256 `e4f224570e844ddff4fee2d9b17a77383510d7a33788ce611a7d7569e661125a`，task-owned Compose/RemoteWorker/OpenSandbox cleanup 为 `0`。该 fault cell 独立登记为 `BLOCKED`，不改变 `pi/remote-worker` Provider×Environment `PASS / supported=true`；其它独立 fault cells 与 aggregate/release/feature Gate 继续 `OPEN`。


### 2026-09-27：Codex×RemoteWorker Agent fault 前置失败（r674）

r674 的两次尝试均在 User Environment 创建前置停止：响应只到 `stableErrorCode` 字段边界，环境没有进入 `observedPhase=ready`，因此没有启动 Codex Agent fault，也没有 Provider、recovery 或 replay 结论。命令 SHA-256 `ad6535be18dad865eaed5dbf21ffe88d5f49c5a603bc8452793407387eaabd57`，日志 SHA-256 `c811f37c1faef6454f58d3b214b248bc89a787d5376d078cde8e3e71b4dba615`，脱敏 preflight evidence SHA-256 `2c662828787f32ab6921e2be49dc7f3d15d2edf218b08a9d44083d6b737845c1`，task-owned cleanup 为 `0`。该结果保持为 `PREFLIGHT_FAILED`，不改变 `codex/remote-worker` Provider×Environment `PASS / supported=true`，也不把前置失败写成 fault BLOCKED；需修复 User Environment 前置后再推进该 fault cell。


### 2026-09-27：deepseek-harness×Docker Agent fault 前置失败（r675）

r675 在 Agent fault 前置阶段停止：User Environment 创建响应带有 `stableErrorCode`，没有进入 ready，故没有启动 Deepseek Agent fault、recovery 或 replay。模型来源仍为受保护的 `provider-credentials/tenant-local.deepseek-harness.json`；命令 SHA-256 `9d0b9e711dfb208e6c91bdfbb4c365e8c68c96a638ed904c1b6e64fe5d95ba6b`，日志 SHA-256 `555b0a43e4e61830b41e1e2bec172e2b01a38196f9c46867f4ef8ca3cd0baa11`，脱敏 preflight evidence SHA-256 `dfa61689f6d64e00672ac95d170765a496ec2cc243c50850af6e3a5738f07ff3`，cleanup 为 `0`。该结果保持为 `PREFLIGHT_FAILED`，不改变 `deepseek-harness/docker` Provider×Environment `PASS / supported=true`，也不把前置失败写成 fault BLOCKED；需先修复 Docker User Environment 前置。

### 2026-09-27：deepseek-harness×Docker Agent fault 最终 BLOCKED（r675 retry）

r675 首次尝试因 Docker User Environment `stableErrorCode` 前置失败；增加非敏感错误码诊断后重试，前置已通过并进入真实 Agent fault。Skill、MCP 与 `str_replace_editor` 在同一同步 provider callback 中完成，runtime 只观察到 checkpoint sequence `1`，没有可恢复的 `pendingSideEffect`，因此未启动 Agent takeover、attempt 2 或 replay。wrapper exit `1`，cleanup `0`；不得将已完成文件变更当作 pending side effect PASS。

模型仍从受保护的 `provider-credentials/tenant-local.deepseek-harness.json` 读取，实际 `gpt-6-luna`。命令 SHA-256 `9d0b9e711dfb208e6c91bdfbb4c365e8c68c96a638ed904c1b6e64fe5d95ba6b`，最终日志 SHA-256 `27a97d3bb7d73a7e5bf60021957cbca24eb6f5d56f2230c8187005048387022a`，脱敏 evidence JSON SHA-256 `6bf06ef28dbe17a9c364fa0ae0d4f865d3fbf2e65f3ca51b63d9e79844e0eaac`。该 fault cell 保持 `BLOCKED`，`deepseek-harness/docker` Provider×Environment 仍为 `PASS / supported=true`，aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Codex×RemoteWorker Agent fault 最终 BLOCKED（r674 retry）

前两次 r674 只在 User Environment 前置停止；本次在加入安全 `stableErrorCode` 诊断后前置通过，基线 contract-negative 通过，随后真实 Agent fault 在 capability-bound user input 等待处超时。未形成 attempt `2`、recovered、reconcile 或 replay，wrapper exit `1`，cleanup `0`，因此不能计 PASS。模型仍从受保护 `provider-credentials/tenant-local.codex.json` 动态读取，实际为 `gpt-6-luna`。

命令 SHA-256 `ad6535be18dad865eaed5dbf21ffe88d5f49c5a603bc8452793407387eaabd57`，最终重试日志 SHA-256 `bf08fb034ae78e993e8b1b886eab3be19316512727229c923bff7f22db1aedce`，脱敏 evidence JSON SHA-256 `be8c9fde19a8dff478e04fbc8c50bf1d1cb84f2b166bbafdeb12b66845778de8`。该 fault cell 记录为 `BLOCKED`，不改变 `codex/remote-worker` Provider×Environment `PASS / supported=true`，aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：ClaudeAgent×RemoteWorker Agent fault BLOCKED（r676）

r676 使用受保护的 `provider-credentials/tenant-local.claudeAgent.json` 动态模型来源，实际为 `gpt-6-luna`。User Environment 前置、Docker 基线和 RemoteWorker contract-negative 通过；真实 Agent fault 在 capability-bound user input 等待处超时，未进入 Agent Runtime kill、attempt `2`、recovered、reconcile 或 replay。wrapper exit `1`，cleanup `0`，不能计 PASS。

命令 SHA-256 `3351bfd633d5cf358d64a0d5504e1228159654452946fc3e19538627821a6e65`，日志 SHA-256 `714477689e3eadd0d3b939b54a4da2a274d41e702992ac964b672fc22d92e8af`，脱敏 evidence JSON SHA-256 `fb4995a7cce75747a1eef90ac0f9a1549f40369d885d082db745720259539505`。该 fault cell 独立记录为 `BLOCKED`，不改变 `claudeAgent/remote-worker` Provider×Environment `PASS / supported=true`，aggregate/release/feature Gate 继续 `OPEN`。


### 2026-09-27：Pi×RemoteWorker Agent fault BLOCKED（r677）

r677 使用 dirty candidate `0.3.0-dev.663`；模型从受保护的 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`。无副作用模型探针、Docker baseline 和 RemoteWorker contract-negative 通过。真实 Agent fault 在 capability-bound side-effect checkpoint 等待处超时，未形成 `pendingSideEffect=true`，未执行 Agent Runtime kill、attempt `2`、reconcile 或 replay；RemoteWorker source heartbeat 同期返回 `INTERNAL_ERROR`。wrapper exit `1`，cleanup `0`，不能计 PASS。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r677-pi-remoteworker-agent-fault-commands.sh` SHA-256 `66550be87bed3fcc59f7de0235d096c8bfc02f483771429d4d88b27532ae4897`，日志 SHA-256 `21b9bd815ade008e692f4edb645c0c49d179147e9ad383a3585a57468bed16fa`，脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r677-pi-remoteworker-agent-fault-evidence.json` SHA-256 `f4144803e11ee269dfd95a6a32f0d4f0a36277f8b28c4b29159123fc4003e73a`。该 fault cell 独立记录为 `BLOCKED`，不改变 `pi/remote-worker` Provider×Environment `PASS / supported=true`；aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Pi×RemoteWorker Agent fault 修复后重试仍 BLOCKED（r679）

针对 r677 的 RemoteWorker source heartbeat `INTERNAL_ERROR`，修复 `executePendingSandboxExec` 的有界 heartbeat renewal，并通过 `go test ./services/control-plane/cmd/cloud-agents-remote-worker`、`gofmt`、`git diff --check`。r678 首次重试只触发旧 Worker image 的候选物料 attestation 前置失败，未进入 Provider；r679 移除旧 image override 后进入真实路径。r679 使用 dirty candidate `0.3.0-dev.678`，sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，模型由受保护 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`；Docker baseline、RemoteWorker contract-negative 通过，source heartbeat 同期持续 HTTP 200。

命令 `.tmp/mcp-skill-runtime-v1-20260927-r679-pi-remoteworker-agent-fault-repaired-commands.sh` SHA-256 `84ed39b50fe89ac230ea5ff3b85d065262117d0d0008fca6be5b4bbee6b410d2`，日志 SHA-256 `2126e118fafbe2c2a6b382e3a739f165d3c56b4edd613359ec44245717d6093c`，wrapper exit `1`；脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r679-pi-remoteworker-agent-fault-repaired-evidence.json` SHA-256 `30597919aa61d13e18db0790427da7e8a595b1e6d90e54a1be74ab523182e7a0`。managed PTY Agent run 在 capability-bound `pendingSideEffect` checkpoint 可观察前结束，未执行 Agent Runtime kill、attempt `2`、reconcile 或 replay；该 fault cell 保持 `BLOCKED`，不改变 `pi/remote-worker` Provider×Environment `PASS / supported=true`，aggregate/release/feature Gate 继续 `OPEN`。


### 2026-09-27：Pi×RemoteWorker Agent fault 进程终止修复后仍 BLOCKED（r695）

r695 针对 r679 的 managed PTY claim-expiry 边界继续 Repair First：候选 Worker 使用 r694 的 arm64 image，并依次尝试了递归后代进程终止、进程组终止和已验证的 PTY 路径；未改变结果。模型由受保护 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`；模型探针 `/v1/models=200`、配置模型存在、`/v1/responses=200` 且有 response id。Docker/RemoteWorker contract-negative 和 Docker capability acceptance 通过，RemoteWorker source heartbeat 保持 HTTP 200。

真实 Agent fault 在 60 次 claim-expiry 轮询后超时；没有可观察的 capability-bound `pendingSideEffect` checkpoint，因此未执行 Agent kill 后的 attempt `2`、reconcile 或 replay。wrapper exit `1`，cleanup `0`。候选 `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；Worker image digest `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e`。命令、日志和脱敏 bounded evidence JSON SHA-256 分别为 `0a114120e70c3b5b4ee942c634cfe7c2ceb2c1921d1ca2882ea0f59def1a5f3e`、`7c9aaeb3de5c085dfd042271ea624a16c87c0faa0135746b791e6c7e59154bce`、`69b01d2c42fd52f52b3f9fbe6704272848b011996169face77af32804a0b3ecd`；原始 harness checkpoint evidence 未生成。

该 fault cell 继续 `BLOCKED`，不改变 `pi/remote-worker` Provider×Environment `PASS / supported=true`，不把 claim-expiry timeout 写成 recovery PASS；Gap Ledger、aggregate/release/feature Gate 继续 `OPEN`。


### 2026-09-27：ClaudeAgent×Docker Worker fault 修复后通过（r696）

r671 的根因是 Claude Worker takeover 后最终响应没有保留 recovery marker；本轮将 interaction recovery prompt 收紧为最终响应只能包含 marker。r696 使用 candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，模型从受保护 `provider-credentials/tenant-local.claudeAgent.json` 动态读取，实际为 `gpt-6-luna`；模型探针 `/v1/models=200`、模型存在、`/v1/responses=200` 且有 response id。Worker image digest 为 `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e`。

wrapper exit `0`；Docker MCP `8`、Skill `1`、Artifact `verified`、events `resumed`；Worker exit survival、attempt `2`、`process-restart/recovered`、recovery marker 和 side-effect `confirmed` 均通过。transport reconnect `passed`/replayed `0`，revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，unknown-side-effect reconcile 通过，Compose smoke 通过，cleanup `0`。命令、日志和脱敏 evidence JSON SHA-256 分别为 `059264524a1b645523d9e6f1cbf5d782d7d322d1420874469843b2231156f5ed`、`b7542534f52ef06987b1d01b4110a3e7e05a3cb6d0abe2e678e61b55cd0afde6`、`92a0a3038fa02ff2a9753326cfb57e1c3b31a7a0265b0620280d79aba6777e20`。

该 fault cell 现为 `PASS`；不改变其它 Provider fault cells 或 aggregate/release/feature Gate 的 `OPEN` 状态。


### 2026-09-27：Codex×Docker Worker fault 完整收口（r697）

r697 使用当前 candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，模型从受保护 `provider-credentials/tenant-local.codex.json` 动态读取，实际为 `gpt-6-luna`；模型探针 `/v1/models=200`、模型存在、`/v1/responses=200` 且有 response id，Worker image digest 为 `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e`。

wrapper exit `0`；Docker MCP `14`、Skill `1`、Artifact `verified`、events `resumed`；Worker exit survival、attempt `2`、`process-restart/recovered`、recovery marker 和 capability-bound recovery 均通过。transport reconnect `passed`/replayed `0`，revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，unknown-side-effect reconcile 通过，Compose smoke 通过，cleanup `0`。命令、日志和脱敏 evidence JSON SHA-256 分别为 `9051ea0e38c52e3b2c8c240414fa2a43f43eeb6d62e389c3ed6452fe700ea31f`、`4a4f269f0422ac053dfbcc51138241d9411187dfa04ab2af16e95df48c91a362`、`d70549fe5015cb102b232e603254625a364bdfc3d41ed23f274acaaf89efe6ce`。

该 fault cell 现为 `PASS`；r668 仅 fault sub-cell/整轮未收口的历史边界由本轮覆盖，不改变其它 fault cells 或 aggregate/release/feature Gate 的 `OPEN` 状态。

### 2026-09-27：Pi×RemoteWorker Worker fault heartbeat-renewal retry BLOCKED（r698）

r698 使用 candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；模型从受保护 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`。模型探针 `/v1/models=200`、配置模型存在、`/v1/responses=200` 且有 response id；Docker/RemoteWorker contract-negative、Docker capability acceptance 通过，source RemoteWorker heartbeat 为 HTTP 200。Worker image digest 为 `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e`。

真实 Worker fault 在 RemoteWorker 重启后仍保持 active turn，未观察到可安全接管的 capability-bound `pendingSideEffect` checkpoint；关闭请求返回 `409`，未进入 Worker takeover、attempt `2`、reconcile 或 replay。wrapper exit `1`、cleanup `0`。命令 `.tmp/mcp-skill-runtime-v1-20260927-r698-pi-remoteworker-worker-fault-heartbeat-renewal-commands.sh` SHA-256 `f8d6636b81b558a71a333840b7d2a7519a8d7cf1ec0746005ff46b58397f3428`，日志 SHA-256 `67a5cfddfc75ff29e72078bc6663f14cbde003789d9886bb2cec76bb37716ed6`，脱敏 evidence JSON `.tmp/mcp-skill-runtime-v1-20260927-r698-pi-remoteworker-worker-fault-heartbeat-renewal-evidence.json` SHA-256 `54562d8d2872a74dd8fa21d50da6ac96fcd05c436c23925521621a69ba565998`。

该 fault cell 记录为 `BLOCKED`，不改变 `pi/remote-worker` Provider×Environment `PASS / supported=true`；禁止把 active-turn/close-409 记录写成 recovery PASS，Gap Ledger、aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Pi×RemoteWorker Worker fault bounded repair attempts r699–r701

r699 保留 r698 的 request-ID 根因修复，并观察到 capability-bound `pendingSideEffect` checkpoint 与 Worker restart；Docker capability-bound recovery 通过。但 RemoteWorker OpenSandbox 的只读 artifact digest probe 返回 WebSocket `invalid status code`，未执行 attempt `2`、reconcile 或 replay。命令/log/evidence SHA-256 分别为 `f8d6636b81b558a71a333840b7d2a7519a8d7cf1ec0746005ff46b58397f3428`、`f3b4c33213a6fd6cbbb76f9d020ae231f4a07e1194c833eb0a5dbde2a7088e97`、`d4789c19060e7b3d4e7c4673cdca2107f19defca8f2800e92778bb9afad1936f`，wrapper exit `1`、cleanup `0`。

r700 为 artifact probe 增加最多五次有界重试后，checkpoint 与 Worker restart 均观察到，但旧 execution claim 在 60 次轮询后仍未 expiry；attempt `2`、reconcile、replay 未执行。命令/log/evidence SHA-256 为 `f8d6636b81b558a71a333840b7d2a7519a8d7cf1ec0746005ff46b58397f3428` / `24afb063fc89352e4f8976a057257559bf019ea3648e71976e89a7e96dac0958` / `cca7b69a49ae38a1bf496304defaddec9495c69cc4168c26bcff35c9fa1e5678`。

r701 在 Worker 重启后保留 35 秒 claim-fence 窗口；artifact probe 在五次尝试后仍受同一 OpenSandbox WebSocket `invalid status code` 阻塞，未确认安全接管或 replay。命令/log/evidence SHA-256 为 `f8d6636b81b558a71a333840b7d2a7519a8d7cf1ec0746005ff46b58397f3428` / `0d57d79a09b1b6abfbd3de034dfc9cda57511ecf0fb5347c28dbbb53b63b6eef` / `af004fec683f2704e2b1193412bf36e2d8aee1a7c9e51b2f9c3191b75c4f491e`。r698–r701 继续记录为 fault `BLOCKED`，不改变 `pi/remote-worker` Provider×Environment `PASS / supported=true`，formal Gate 仍 `OPEN`。

### 2026-09-27：Pi×RemoteWorker Worker fault 修复后完整通过（r708）

r702–r707 是同一 fault cell 的有界修复诊断：r702/r704 暴露 claim fence 与 durable state 边界，r706/r707 已完成本地测试数据库 PTY 删除并进入 claim expiry，但文件探针 `NOT_FOUND` 稳定码尚未归类；这些 partial runs 均 wrapper `1`，不计 recovery PASS。对应 evidence SHA-256 为 r702 `521dc2eb87682f93d0a84ba058c5383d82ed9d2ca9be374f060678a530b467cc`、r704 `a3858adb9adfcfe44242d0f3cef102528b77cf3a8836237df50c0ecadc090d21`、r706 `3690d0b8e738168fdbd77fda13ca6788a901224b3e5f47547c30925a917947fd`、r707 `5206e8eb48403371067b0fa8b0629d4f51d85abd4a373ec35f1c41c8efab7d48`。

r708 使用同一 dirty candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`，模型从受保护 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`；模型探针 `/v1/models=200`、配置模型存在、`/v1/responses=200` 且有 response id，Worker image digest 为 `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e`。RemoteWorker Worker exit survival、PTY deletion/claim fencing、attempt `2` `process-restart/recovered`、capability-bound marker 和 unknown-side-effect reconcile `not-applied` 通过；RemoteWorker cross-node takeover/recovered/confirmed 通过，attempt `2`、RPO `0`、RTO `9795 ms`、snapshot `72704` bytes、snapshot digest `sha256:300766ebc41abc668b4a9ffd2cd9f4290684a030ae576132e9823d4b3b04ac82`，恢复后 MCP/Skill binding、事件续读通过。

RemoteWorker MCP `14`、Skill `1`、Artifact `verified`、events `resumed`；transport reconnect `passed`/replayed `0`，revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，Compose smoke 通过，wrapper exit `0`、cleanup `0`。命令、日志和脱敏 evidence JSON SHA-256 分别为 `f8d6636b81b558a71a333840b7d2a7519a8d7cf1ec0746005ff46b58397f3428`、`64b21e33dc1281e6276ba1aa69814795bb3983ebd6c05f9b4e1bfbe70cac57be`、`51a803451243bf3783ca964e4c9809982aee96031f52b312e527a9c025a7ee96`。当前 `pi/remote-worker` Worker fault 为 `PASS`；r698–r707 仅保留为修复历史，不改变其它 fault cells 或 aggregate/release/feature Gate 的 `OPEN` 状态。

### 2026-09-27：Pi×RemoteWorker Agent fault r709–r710 bounded repair history

r709 在 r708 Worker PTY/claim 修复尚未接入 Agent 分支时重现了 managed PTY claim-expiry 超时；wrapper `1`，未执行 attempt `2`、reconcile 或 replay。r710 首次接入共享 fence，但 recovery harness 未注入 RemoteWorker Worker 容器句柄，在真实验收前以 `CLOUD_AGENTS_E2E_WORKER_CONTAINER` 未绑定变量退出；两轮均不计 Agent recovery PASS。命令/log SHA-256 分别为 r709 `0a114120e70c3b5b4ee942c634cfe7c2ceb2c1921dca2882ea0f59def1a5f3e` / `a07b558ccdfd30302b28a172cf9842b0dde1263a632b59bf7d7f39ff67bed6fa`，r710 同命令 SHA / `4aaad544f9a2058245dd692be9b0149b9db59e561d67f390f78ed64aa54c7b2`。

### 2026-09-27：Pi×RemoteWorker Agent fault 共享 PTY fence 后完整通过（r711）

r711 使用 candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，模型从受保护 `provider-credentials/tenant-local.pi.json` 动态读取，实际为 `gpt-6-luna`；模型探针 `/v1/models=200`、配置模型存在、`/v1/responses=200` 且有 response id，Worker image digest 为 `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e`。RemoteWorker Agent Runtime exit survival、共享 PTY deletion/claim fence、attempt `2` `process-restart/recovered`、capability-bound recovery marker 和 unknown-side-effect reconcile `not-applied` 通过；cross-node `cross-node-takeover/recovered/confirmed` 通过，attempt `2`、RPO `0`、RTO `9729 ms`、snapshot `78336` bytes、snapshot digest `sha256:d610274baf2b7e3f3c72d9f1d027602597ce08088abf2ac01584d9b4abc723ef`，恢复后 MCP/Skill binding、事件续读通过。

RemoteWorker MCP `14`、Skill `1`、Artifact `verified`、events `resumed`；revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，Compose smoke 通过，wrapper exit `0`、cleanup `0`。transport reconnect 沿用已完成的 Pi×RemoteWorker Provider cell；本 fault run 不重复该路径。命令、日志和脱敏 evidence JSON SHA-256 分别为 `0a114120e70c3b5b4ee942c634cfe7c2ceb2c1921dca2882ea0f59def1a5f3e`、`fc51fa975f9a2058245dd692be9b0149b9db59e561d67f390f78ed64aa54c7b2`、`abb5174b33effe1d0a649e9d98eeeaf88381bb4c8083198384b8e58ec9ac079d`。

该 fault cell 现为 `PASS`；r709–r710 仅保留为 repair history，不改变其它 fault cells 或 aggregate/release/feature Gate 的 `OPEN` 状态。

### 2026-09-27：Codex×RemoteWorker Agent fault r712 partial recovery BLOCKED

r712 在当前 dirty candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、受保护 `provider-credentials/tenant-local.codex.json` 模型 `gpt-6-luna` 和 Worker image digest `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e` 上进入真实 Agent fault。模型探针、Docker/RemoteWorker contract-negative、browser smoke、RemoteWorker MCP/Skill/Artifact/events `28/1/verified/resumed` 通过；Agent Runtime exit survival、PTY claim fence、attempt `2` `process-restart/recovered`、side-effect `not-applied`、cross-node `cross-node-takeover/recovered/confirmed`、RPO `0`、RTO `7571 ms`、snapshot `8644608` bytes、snapshot digest `sha256:4556919b9641763d7830d6e507ccc099c8210f79659907c769cfafcd15a26902`、恢复后 binding 与事件续读也通过。

r712 最终 Codex follow-up execution 停在 `pendingInteraction` 并在 10 分钟后超时：Provider 返回的 approval command 使用 `/bin/bash -c`，harness 只匹配 `/bin/bash -lc`，因此未 resolve approval；wrapper exit `1`，未计 Codex Agent fault PASS。该根因已在共享 approval 判定和 recovery checkpoint 判定处修复为归一化两种 shell 变体，待下一次独立真实重试。cleanup 仅完成 deterministic volume 检查，owned OpenSandbox runtime container 已提前不存在。

命令/log/evidence SHA-256 为 `242cc6cf0aab53b5462de2e5edf57f787dca414bb35288f7d96a02b7172380c3` / `677788a4e777fad7becfc4939e24e9e47d5dabc2ad74e3070bf3274b23677a10` / `cafd7e1f2972fd3f6f3dad5c9c36673b4862b0bc155d3119251d9b255778ab80`。该 fault cell 记录为 `BLOCKED`，不改变 `codex/remote-worker` Provider×Environment `PASS / supported=true`；aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：Codex×RemoteWorker Agent fault approval normalization 后通过（r714）

r714 在 r712 的 shell approval 变体根因修复后完成当前 Codex×RemoteWorker Agent fault cell。candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；模型从受保护 `provider-credentials/tenant-local.codex.json` 动态读取，实际为 `gpt-6-luna`；Worker image digest 为 `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e`。模型探针、Docker/RemoteWorker contract-negative、browser smoke 通过。

Agent Runtime exit survival、PTY claim fence、attempt `2` `process-restart/recovered`、side-effect `not-applied`、cross-node `cross-node-takeover/recovered/confirmed`、RPO `0`、RTO `9740 ms`、snapshot `8631808` bytes、snapshot digest `sha256:b39d09a2143c4e3bf6ea5c0fcd28d76b9bdfebe90fc61be4b133ff08af2c88a5`、恢复后 capability binding 与事件续读通过。RemoteWorker MCP `28`、Skill `1`、Artifact `verified`、events `resumed`；reconnect 沿用现有 Codex×RemoteWorker Provider cell，replayed `0`；revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，unknown-side-effect reconcile、Compose smoke、wrapper exit `0`、cleanup `0`。

命令/log/evidence SHA-256 为 `242cc6cf0aab53b5462de2e5edf57f787dca414bb35288f7d96a02b7172380c3` / `7148eed1bea3bd7d548c4a9e15b0543585e988d5a8f723041a57d051043eba85` / `0540dde7da20cc752224c3beadba19c4f8416ecc113b68ba6941a799149d265a`。r712 的 approval mismatch 与 r713 外层 wrapper 收尾问题仅保留为 repair history；当前 Codex×RemoteWorker Agent fault 为 `PASS`，不改变其它 fault cells 或 aggregate/release/feature Gate 的 `OPEN` 状态。

### 2026-09-27：ClaudeAgent×RemoteWorker Worker fault 完整通过（r715）

r715 在当前 dirty candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、受保护 `provider-credentials/tenant-local.claudeAgent.json` 模型 `gpt-6-luna` 和 Worker image digest `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e` 上完成真实 Worker fault。模型探针、Docker/RemoteWorker contract-negative、browser smoke 通过；Worker exit survival、PTY claim fence、attempt `2` `process-restart/recovered`、side-effect `confirmed`、cross-node `cross-node-takeover/recovered/confirmed`、RPO `0`、RTO `13003 ms`、snapshot `644096` bytes、snapshot digest `sha256:f2c3a4d5e5cfa0bc500d26b7e04895305612f6786dd6d40acae0fcf2b98aa4bb`、恢复后 binding 与事件续读通过。

RemoteWorker MCP `14`、Skill `1`、Artifact `verified`、events `resumed`；reconnect 沿用已有 ClaudeAgent×RemoteWorker Provider cell，replayed `0`；revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，unknown-side-effect reconcile、Compose smoke、wrapper exit `0`、cleanup `0`。命令/log/evidence SHA-256 为 `04a14e21f08208ccd78d077c3c60c992c9e382f94e744f46d0ddd42f95ffdcd2` / `3393d6eff76dd403bb65e4f74781fbf647a52d19cc07dc300f87400d20792ada` / `844826ea1650f406f3a1e1606931878bde7b32bb85f1ec5c2b7d6ea947c97eee`。该 fault cell 为 `PASS`；其它 fault cells 与 aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-27：ClaudeAgent×RemoteWorker Agent fault r716 marker 诊断 BLOCKED

r716 在 PTY fence 与 RemoteWorker Agent recovery 路径中完成前置和 capability acceptance，但最终 recovery execution 成功后没有包含预期的超长 run-specific marker，wrapper 未形成 PASS；日志保留了 recovery execution 409/claim-expiry、reconcile 后 200 的边界和 marker assertion failure。该结果不改变 `claudeAgent/remote-worker` Provider×Environment PASS。

Repair First 将 Agent recovery marker 缩为固定短 token `agent-exit-recovered`，session/run 仍独立标识本轮；`sh -n` 与 `git diff --check` 通过。r716 命令/log 为诊断历史，待 r717 真实重试。

### 2026-09-27：ClaudeAgent×RemoteWorker Agent fault 短 marker 修复后通过（r717）

r717 使用 dirty candidate `0.3.0-dev.694`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、受保护 `provider-credentials/tenant-local.claudeAgent.json` 模型 `gpt-6-luna` 和 Worker image digest `sha256:d2995f05a7a21a67344427fba4d4243e659670eb8ef2bd3e89602d1e5fc0f92e` 完成真实 Agent fault。Agent Runtime exit survival、PTY claim fence、attempt `2` `process-restart/recovered`、marker `agent-exit-recovered`、side-effect `confirmed`、cross-node `cross-node-takeover/recovered/confirmed`、RPO `0`、RTO `7591 ms`、snapshot `824320` bytes、snapshot digest `sha256:4d5d162d21ac65afc10ef6465f0f01ae8150be6ab1934c643e2c00a2d6e89a39`、恢复后 binding 与事件续读通过。

RemoteWorker MCP `14`、Skill `1`、Artifact `verified`、events `resumed`；reconnect 沿用已有 ClaudeAgent×RemoteWorker Provider cell，replayed `0`；revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，unknown-side-effect reconcile、Compose smoke、wrapper exit `0`、cleanup `0`。命令/log/evidence SHA-256 为 `c159932b8f2427903b906282743bf0eaee66d536642a36096deeb577374cad28` / `655bd37d204fd39973552b3a08409a03d843e335d8f3c394942ffc8e7702234c` / `6bfe54fd453ad9964eaf36ff50f2edc368851f623394e26c65fe0868f6a25fd6`。当前 ClaudeAgent×RemoteWorker Agent fault 为 `PASS`；其它 fault cells 与 aggregate/release/feature Gate 继续 `OPEN`。


### 2026-09-27：deepseek-harness×Docker Agent fault 修复后完整通过（r718）

r718 在 dirty candidate `0.3.0-dev.718`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true 上完成 DeepSeek×Docker Agent fault。模型由受保护 `provider-credentials/tenant-local.deepseek-harness.json` 动态读取，实际为 `gpt-6-luna`；模型探针 `/v1/models=200`、配置模型存在、`/v1/responses=200` 且有 response id。Worker image 为 `cloud-agents-worker:local-r718`，image ID `sha256:edadbd5cf07bd4b1201c71dd4c6170d7eb37a58c56613b4cb6c50c0f375406e3`，linux/arm64 worker SHA-256 `16fa04fdf8e1fa64fa868597e390f2ed1131c4e58587da11980b84d2185f4cba`；candidate manifest/checksums SHA-256 为 `9100d2c3049139de0e003c28283a198df5904504e380c313ca313378205d3e40` / `01c20da087c67013deb4da45725077242b9f223d2227382a91f2bbf1405ef050`。

Repair First 保留了 r675 的无 checkpoint 诊断；r718 先修复 linux/arm64 worker 构建、DeepSeek side-effect checkpoint、side-effect 等待器对非 Bash `str_replace_editor` 的判断、PID 已退出 guard 和无换行 file_text 的 reconcile digest。最终真实 fault 达到 `pendingSideEffect`、Agent Runtime exit survival、attempt `2` `process-restart/recovered`、marker `agent-exit-recovered`、side-effect `confirmed`、reconcile `confirmed`；Docker cross-node recovery 为 `N/A`。恢复后 MCP/Skill binding 与事件续读通过。

Docker MCP `8`、Skill `1`、Artifact `verified`、events `resumed`；transport reconnect 沿用已有 deepseek-harness×Docker Provider cell、replayed `0`；revoke `runtime_requests=0/side_effects=0/events=2`，cross-tenant rejected，version/digest rejected，stale generation `409`。Compose smoke、wrapper exit `0`、task-owned Compose/OpenSandbox cleanup `0`。命令/log/evidence JSON SHA-256 为 `e2b8f07ea171a9209b18ec55fe60668b2dca9271b4883e0f7c3f8a8e267b2db1` / `2c6b459c3c7294a9c644bca481fb0e5bf67ec037ecc87da1b03f16a41e738055` / `28b1f58ff5ad27d66838377455c39d8931cb6a413eb4ee7fb0083ce74dc12b87`。该 fault cell 现为 `PASS`；aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-28：Codex×Kubernetes Agent fault fixture 生命周期修复后通过（r722）

r721 只到 Kubernetes capability fixture 前置：同一 Runtime Pod 的固定 `48765` 端口已有旧 fixture 进程，第二次启动未先停止旧 PID，出现 `EADDRINUSE`，不计 Provider/Agent 结果。Repair First 在 `start_capability_mcp_fixture` 的共享 Kubernetes 分支加入旧 PID 终止，再以当前 dirty candidate 完成 r722；`sh -n` 与 `git diff --check` 通过。

r722 使用 candidate `0.3.0-dev.718`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，模型从受保护 `provider-credentials/tenant-local.codex.json` 读取为 `gpt-6-luna`；模型探针为 `/v1/models=200`、配置模型存在、`/v1/responses=200` 且有 response id。Worker image `cloud-agents-worker:local-r718`，image ID `sha256:edadbd5cf07bd4b1201c71dd4c6170d7eb37a58c56613b4cb6c50c0f375406e3`，linux/arm64 Worker SHA-256 `16fa04fdf8e1fa64fa868597e390f2ed1131c4e58587da11980b84d2185f4cba`；candidate manifest/checksums SHA-256 为 `9100d2c3049139de0e003c28283a198df5904504e380c313ca313378205d3e40` / `01c20da087c67013deb4da45725077242b9f223d2227382a91f2bbf1405ef050`。

OrbStack Kubernetes 单节点真实路径通过：Agent Runtime exit survival、PTY claim fence、attempt `2` `process-restart/recovered`、marker `agent-exit-recovered`、恢复后 binding 与事件续读；Kubernetes MCP `40`、Skill `1`、Artifact `verified`、events `resumed`。同一单元的 transport reconnect `side_effects=1/fault_requests=1/replayed=0`、撤销 `runtime_requests=0/side_effects=0/events=2`、跨租户拒绝、version/digest 拒绝、旧 generation `409`、reconciliation、Compose smoke、wrapper exit `0` 与 task-owned Compose/OpenSandbox cleanup `0` 均通过。本轮不重复 Kubernetes cross-node；既有 Codex×Kubernetes cross-node 证据保持独立，aggregate/release/feature Gate 继续 `OPEN`。

命令/log/evidence JSON SHA-256 为 `9ff2df865d9cefa17a12fff7b24a5fbd5b64efb3a51968b4d173dc17b59a2e5c` / `e34ffa904c9851518543363c9115fb29e7b47680166591956b174a0d0a475828` / `03b1f1a2d2e28c752306f73d12e0c1374fc6de9f1d2121e97ba3ca9e3264e3c4`。当前 Codex×Kubernetes Agent fault 为 `PASS`；r719/r720 DeepSeek×Docker Worker 仍保持独立 BLOCKED/preflight failure 边界。

### 2026-09-28：ClaudeAgent×Kubernetes Agent fault fixture 修复后通过（r723）

r723 使用 dirty candidate `0.3.0-dev.718`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、受保护 `provider-credentials/tenant-local.claudeAgent.json` 模型 `gpt-6-luna`，以及 `cloud-agents-worker:local-r718`（image ID `sha256:edadbd5cf07bd4b1201c71dd4c6170d7eb37a58c56613b4cb6c50c0f375406e3`）。共享 Kubernetes fixture 旧 PID 修复在 r723 重复验证，未再出现 `48765` 端口冲突；模型探针 `/v1/models=200`、配置模型存在、`/v1/responses=200` 且有 response id。

OrbStack Kubernetes Agent Runtime exit survival、PTY claim fence、attempt `2` `process-restart/recovered`、marker `agent-exit-recovered`、恢复后 binding 与事件续读通过；Kubernetes MCP `22`、Skill `1`、Artifact `verified`、events `resumed`。transport reconnect `side_effects=1/fault_requests=1/replayed=0`、撤销 `runtime_requests=0/side_effects=0/events=2`、跨租户拒绝、version/digest 拒绝、旧 generation `409`、reconciliation、Compose smoke、wrapper exit `0`、task-owned Compose/OpenSandbox cleanup `0` 均通过。本轮不重复 Kubernetes cross-node，既有 ClaudeAgent×Kubernetes cross-node 证据保持独立；aggregate/release/feature Gate 继续 `OPEN`。

命令/log/evidence JSON SHA-256 为 `a78752439e55d5940f5ee89e755764d022004f308e72b66f17281129aa753665` / `8c9162fc7486ffbc0e331b196da01040a477119b5bf283c0b8d28fb9b078f158` / `20662257340d06a3c05cade231b2d0b31480ba5c41976a45936994213b961fc3`。当前 ClaudeAgent×Kubernetes Agent fault 为 `PASS`。

### 2026-09-28：Pi×Kubernetes Agent fault sandbox probe 修复后通过（r726）

r724 已形成 `awaiting_reconciliation` 和 `pendingSideEffect`，但 Kubernetes Sandbox artifact probe 的 200 响应未通过 harness 对 `stdout` 行尾的过严匹配，导致 reconcile 未发出，后续 session close 返回 409；不计 PASS。r725 已验证 probe 修复后的 recovery markers，但外层 zsh 收尾使用只读变量 `status`，wrapper 不是 0，仍只保留为 repair history。

r726 使用 candidate `0.3.0-dev.718`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true，模型从受保护 `provider-credentials/tenant-local.pi.json` 读取为 `gpt-6-luna`；Worker image `cloud-agents-worker:local-r718`，image ID `sha256:edadbd5cf07bd4b1201c71dd4c6170d7eb37a58c56613b4cb6c50c0f375406e3`，linux/arm64 Worker SHA-256 `16fa04fdf8e1fa64fa868597e390f2ed1131c4e58587da11980b84d2185f4cba`。Repair First 将 Kubernetes probe 收敛为只接受单个 digest/`absent` 及可选 CR/LF 行尾，未放宽 exitCode 或 stderr 约束；`sh -n` 与 `git diff --check` 通过。

OrbStack Kubernetes Agent Runtime exit survival、PTY claim fence、attempt `2` `process-restart/recovered`、side-effect `confirmed`、reconciliation、恢复后 binding 与事件续读通过；Kubernetes MCP `24`、Skill `1`、Artifact `verified`、events `resumed`。transport reconnect `replayed=0`、撤销 `runtime_requests=0/side_effects=0/events=2`、跨租户/version-digest 拒绝、旧 generation `409`、Compose smoke、wrapper exit `0` 与 task-owned cleanup 验证通过。本轮不重复 Kubernetes cross-node；既有 Pi×Kubernetes cross-node 证据保持独立，aggregate/release/feature Gate 继续 `OPEN`。

命令/log/evidence JSON SHA-256 为 `089581fb608ec0e51be6e67cbc0582d72bccac46626fb2327e7b02f392f474e5` / `46afa36d01df816118575a4b379af3c4807f06c8c10de94dba2a509e09e0dfd0` / `5fcff889960ae2ba75e2aef124fbbfb14624f11669ba8cdf439f9ce059e32671`；wrapper 文件 SHA-256 `9a271f2a916b0b6ee6cecb2426f0b3206ef074578be55d9bc94f6f3fe3ab86aa`。当前 Pi×Kubernetes Agent fault 为 `PASS`。

### 2026-09-28：deepseek-harness×Docker Worker fault r732 PASS

r719/r720/r728/r729/r730/r731 保留为 bounded repair history；r732 在 dirty candidate `0.3.0-dev.732`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true 上完成 DeepSeek×Docker Worker fault。模型动态读取受保护 `provider-credentials/tenant-local.deepseek-harness.json`，实际 `gpt-6-luna`；Worker image `cloud-agents-worker:local-r732`，image ID `sha256:99b058e0d6e4d6bc1f840b99348885756e3c40b6bcf92fa9a4d78288a196e2fb`。

真实 Worker restart 形成 `pendingSideEffect`，`side_effect_reconciliation=confirmed`，attempt `2` `process-restart/recovered`，恢复后 capability binding 与事件续读通过。Docker MCP/Skill/Artifact/events 为 `8/1/verified/resumed`；transport reconnect `fault_requests=1/side_effects=1/replayed=0`，revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `1→2/409`，cross-tenant `401/401`，version/digest mismatch fail-closed，Compose smoke、wrapper exit `0`、cleanup `0`。Docker cross-node 与同节点 RPO/RTO 不适用/未单独发出 snapshot evidence。

命令/log/evidence JSON SHA-256 为 `288e54c5fd15b18c9c6d1e5a782328a5ebfcd51adcec1edf802ead96de65dbb9` / `2fb9f0f960cccce2b3be4e462939478a999218b46065e31a4141071937233688` / `810c3271f37ee58390e1eb8c07ec2308d20c8e4c022e3883abd881e1e81ea359`。当前 DeepSeek×Docker Worker fault 为 `PASS`；aggregate/release/feature Gate 仍为 `OPEN`，不改变 capability catalog supported 状态。

### 2026-09-28：deepseek-harness×RemoteWorker Worker fault r736 PASS

r733/r734 的 RemoteWorker PTY session 不适用于 DeepSeek 的 `str_replace_editor` adapter；r735 又确认 recovery snapshot 已 reconcile 后，adapter 仍把 retry 当作初次 recovery 并再次挂起 side effect。r736 在 `hasAuthoritativeResumeData` 识别 authoritative resume snapshot 后关闭该重复 hold，完成真实 Worker fault。candidate `0.3.0-dev.736`、sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、sourceDirty=true；模型由受保护 `provider-credentials/tenant-local.deepseek-harness.json` 动态读取，实际为 `gpt-6-luna`；Worker image `cloud-agents-worker:local-r736`，image ID `sha256:d5a14517251f83604d700bbaaee7a14129b57d8067ef600ca75f627d60972866`。

RemoteWorker Worker recovery、side-effect reconciliation `not-applied`、attempt `2`、cross-node `cross-node-takeover/recovered/confirmed`、RPO `0`、RTO `12863 ms`、snapshot digest `sha256:bacc26a8d65420d6fdfdefb8ae2ef36b00990d4621e3296c9e69a80c104c4e4e`、恢复后 binding 与事件续读通过。MCP/Skill/Artifact/events 为 `14/1/verified/resumed`；revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，cross-tenant `401/401`，version/digest mismatch fail-closed，Compose smoke、wrapper exit `0`、cleanup `0`。transport reconnect 沿用已有 DeepSeek×RemoteWorker Provider cell，本 fault run 未重复该路径。

命令/log/evidence JSON SHA-256 为 `269a3429c32d14942ee20d535e66117a0b6ca0e582856e92bd0767b63fdd6e48` / `e25944abbeaa04d78e5f82bd08a6b79955fd9bd58b70f9542fb6a32c16f6dd29` / `75eb62f984f2498a40fd1bfbb4e00a7176cdafcd084f4e44503acc8a7822fb4a`。当前 DeepSeek×RemoteWorker Worker fault 为 `PASS`；r733–r735 仅保留为 repair history，aggregate/release/feature Gate 继续 `OPEN`，不改变 capability catalog supported 状态。

### 2026-09-28：deepseek-harness×RemoteWorker Agent fault r740 PASS

r740 在 r736 candidate `0.3.0-dev.736`、dirty sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、受保护 tenant-local DeepSeek 模型 `gpt-6-luna` 和 `cloud-agents-worker:local-r736` 上完成 DeepSeek×RemoteWorker Agent fault。r737 暴露 DeepSeek `str_replace_editor` adapter 不适用 RemoteWorker PTY fence；r738 保留一次未形成 checkpoint 的运行时诊断，r739 保留一次 PostgreSQL serialization/heartbeat 外部并发失败；r740 使用 PTY fence 适配后完成真实重试。

Agent Runtime exit survival、side-effect reconcile `not-applied`、attempt `2` `process-restart/recovered`、cross-node `cross-node-takeover/recovered/confirmed`、side effect `confirmed`、RPO `0`、RTO `7617 ms`、snapshot digest `sha256:69685d36a6cfe494a0c1477ead0f7eb90c1d03942c72c0fe8dd3bc5fb0bf5134`、恢复后 MCP/Skill binding 与事件续读通过。RemoteWorker MCP/Skill/Artifact/events `14/1/verified/resumed`；revoke `runtime_requests=0/side_effects=0/events=2`，stale generation `409`，跨租户/version-digest negative、Compose smoke、wrapper exit `0`、cleanup `0`。transport reconnect 沿用既有 DeepSeek×RemoteWorker Provider cell，不把本轮未重复路径写成新 PASS。命令/log/evidence JSON SHA-256 为 `ce427dfd07ca44bb39d1376b26c9b495a7015e7056033d026d36e4cb1629c380` / `1db04f96b5a6f57c58d3e5eb37172f326cc252077f6f7f55cf4d6b0b5be24a09` / `b974d624fe728e0e9452a575f1dddf24494519563477e570f63bad1381cd3423`。

该 fault cell 现为 `PASS`；DeepSeek×Kubernetes Agent 仍开放，aggregate/release/feature Gate 继续 `OPEN`。

### 2026-09-28：deepseek-harness×Kubernetes Agent fault r741 PASS

r741 在 OrbStack Kubernetes 单节点完成 DeepSeek Agent fault recovery。candidate `0.3.0-dev.736`、dirty sourceCommit `29afe9b9103cac99088f637e02a8cd9bb3f48d58`、模型从受保护 tenant-local 配置读取为 `gpt-6-luna`、Worker image `cloud-agents-worker:local-r736`。Agent Runtime exit survival、attempt `2` `process-restart/recovered`、side-effect reconciliation `confirmed`、Kubernetes MCP/Skill/Artifact/events `22/1/verified/resumed`、transport reconnect `passed`、revoke `0/0/2`、stale `409`、cross-tenant/version-digest negative、Compose smoke、wrapper `0`、cleanup `0` 均通过；cross-node 沿用当前 tenant-local r659 evidence，direct Sandbox Worker 为 `NOT APPLICABLE`。

命令/log/evidence JSON SHA-256 为 `6ca6c8a4bfc71bf332d2f288b1243b488b58b63fa38b990a3e606c94afd04017` / `0be63530051551618b6c225c8b1630ea6b6789b358533149a9b29d3cb913e843` / `29a6a224a8a70b5fca209aeaf8196304f195fe31ae81bdda4f4cc00a351af5cd`。该 fault cell 现为 `PASS`；aggregate/release/feature Gate 与 capability catalog supported 状态保持原结论。

### 2026-09-28：MCP-SKILL-RUNTIME-V1 Gate approval and feature closeout

独立只读 reviewer 已明确 `APPROVE`。r741 后的 12/12 当前 tenant-local Provider×Environment、适用 fault/recovery matrix、cross-node evidence 与五份同步文档均满足收口条件；aggregate Gate、release Gate 与 feature closeout 现记为 `CLOSED / APPROVED`。capability catalog 保持原有 adapter capability 语义与真实 evidence 一致。
