# 04. 底座实施顺序与既有平台迁移

## 0. 当前实施顺序：底座先行

[ADR-0031 / D-054](../adr/0031-foundation-first-cloud-workspace-platform.md) 确定底座优先、Admin Web 配套、
用户 CloudAgents 后续接入。以下是实施拆解方案，不是本次已执行或全部获准执行的声明。
阶段状态只维护在 [06](06-status-tracker.md)，整体底座就绪条件见 [05](05-gates-and-acceptance.md#0-底座就绪验收-base-ready)。

### 0.1 依赖与配套交付

| 顺序 | 底座切片 | 同阶段 Admin Web 配套 | 退出证据 |
| --- | --- | --- | --- |
| BASE-M0 | 领域/API 映射与固定版本执行 PoC | 复用 Target、Operation/Audit、版本和能力事实视图；无数据的能力不建空页面 | 无 Agent 的真实 create → ready → exec/file → stop/delete；重复请求、失败补偿及卷不误删验证 |
| BASE-M1 | 长期 Workspace/Volume 与持久化生命周期调谐 | Workspace/Sandbox 分离列表；卷归属、保留规则、Operation/恢复状态 | 停止和重建保留代码；HTTP 断开、CP/Controller 重启后无需人工重发即可完成已接受操作 |
| BASE-M2 | 通用 Exec/PTY/Files、Preview/SSH 与访问隔离 | Port/Grant 元数据、到期/撤销、网络策略执行状态 | 无 Agent 客户端可连接和重连；跨租户/路径穿越/任意跳转被拒绝，网络规则实际生效 |
| BASE-M3 | 客户节点 RemoteWorker 接入 | 节点注册意图、owner、能力、心跳、Drain/Resume、版本与证书状态 | 仅 outbound/NAT 节点可承载同一 Workspace/Sandbox 流程；断连、重连、过期命令和旧 generation 正确处理 |
| BASE-M4 | Kubernetes 路径、资源池/容量调度与隔离等级 | Region/Pool/Node、RuntimeProfile、资源配额和调度失败原因 | Docker/Kubernetes/客户节点能力矩阵；容量不足、owner/runtime/arch 不匹配拒绝；强隔离实证 |
| BASE-M5 | 文件系统快照恢复、独立交付、计量与运维收口 | Snapshot/Restore、用量、失败积压、升级/回滚、恢复状态；配套页面整体回归 | 无 Agent 的完整底座矩阵、备份恢复/升级/故障演练与 Admin 验收；逐项通过 BASE-READY |
| APP-M1 | 用户 CloudAgents 接入已就绪底座 | 沿用底座运维入口；不加入对话/源码查看 | Codex/Claude 真实 Turn、Approval、历史与 Artifact 在持久 Workspace 上完成，底座回归通过 |

BASE-M0～M5 是当前工作顺序，不改名或重置旧 P0～P6、Portable Runtime M1、ADMIN-M* 的证据。
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
- Admin 配套区分 Workspace、Sandbox 与旧 Lease，清楚显示哪些数据会保留；策略值必须与执行值一致。

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
- 收齐各阶段 Admin API 权限、危险操作确认、Operation/Audit、双语、可访问性和 Daytona 固定视觉验收；
  不能把历史截图或早期 Provider E2E 当作当前底座完整证明。

### 0.3 执行与完成约束

收到明确实施任务后，从最早未完成且在授权范围内的 BASE 切片推进；每次交付包含契约/后端、必要 SDK/CLI、
相关 Admin 页面和真实验证，不先铺完整后台页面再补行为。安全独立的准备工作可并行，不能绕过依赖验收。
单个 UI 视觉问题不阻止安全的后端工作，但对应配套页面和 BASE-READY 不能提前标为完成。

用户批准的产品边界已经记录，不重复要求确认同一边界；实现授权仍以当前任务和已有同范围授权为准。
需要新环境/凭据、改变数据保留、迁移现有卷或跨越单独批准要求时，只暂停受影响动作并说明需要什么，
继续可独立完成的在范围内工作。所有生产写入、部署/发布、脏 worktree 删除和正式 Gate 要求保持不变。

下文保留旧提取/cutover 方案供兼容与后续集成使用，不要求为新底座从头重做历史 inventory 或先完成真实 Agent/T3。

## 1. 迁移原则

- 先 inventory 和 characterization，后搬代码；
- 按能力迁移，不按目录整仓复制；
- 只把逐 blob 固定、复核并按目标语义重写后的内容提交到新的公共历史；禁止 subtree/filter-repo/cherry-pick
  等方式把 Synara Git 历史 graft 到公共仓；
- secret triage 标记为 `REWRITE_REQUIRED_BEFORE_PUBLICATION` 的来源只能作为行为 oracle，静态测试私钥必须
  删除或改为运行时生成后再进入公共提交；
- 公共 schema/API 先于数据库和实现；
- 新旧 authority 不双写；
- 活动资源由创建它的 writer drain 到终态；
- 每个公共能力切换后，Synara 中的重复可编辑实现必须删除或变为 client/projection；
- 所有阶段都可回滚，但回滚不能更换活动 Session/Lease 的 writer。

## 2. Inventory 输出

先固定 `source_ref/source_commit/tree_hash`，再对该 ref 的完整传递构建/部署输入建立 manifest。114 个 Go
package、994 个 Go 文件、168 个 migration 和 8 个 binary 只是 2026-08-10 的观测值，不是 Gate 的硬编码
范围；还必须覆盖 SQL/schema、Dockerfile、Helm/Compose、scripts、generated assets、fixtures 和 release
metadata：

```text
source_sha
source_tree_hash
path
tree_hash
package
capability
current_dependencies
data_tables
authority
classification = move | rewrite-public | adapter | synara-only | retire
target_path
license/provenance
characterization_tests
cutover_gate
```

分类粒度至少到文件/能力；`agentd`、execution targets、enterprise identity、usage、KMS、retention 等混合
package 不能整包贴标签。先重点审计：sessions、executions、agentd、worker protocol、workspace/materialization、artifacts、credentials/
broker、provider host、idempotency/outbox、worker releases/targets、projects/tenancy/auth。

Inventory seed（最终以逐文件 manifest 为准）：

| 分类                      | 候选能力                                                                                                                                                                                                                                   |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Move                      | problem/validation/secretguard、databasetime、fair queue、workertiming、limits、attestation、git policy、provider catalog 等低依赖机制                                                                                                     |
| Rewrite public            | database/persistence/httpapi、tenant/identity/auth/project/service account、session/execution/event/idempotency/outbox、artifact/credential、agentd/provider proxy、target/placement/quota/routing、worker release/observability/retention |
| Adapter                   | local/container/Kubernetes/SSH actuator、filesystem/PVC/S3、OIDC、local/Vault/KMS、OTLP、ingress/DNS/TLS、Cocoon/gVisor                                                                                                                    |
| Synara-only               | commercial plan/invoice/internal cost、support access、desktop product integration、Stage 6 approval/governance registry、Synara UI/projection/private infra config                                                                        |
| Deferred public extension | SAML/SCIM、legal hold/privacy export、advanced retention、多云 enterprise adapters                                                                                                                                                         |
| Retire                    | Provider Host v1、旧 metadata import、被公共 API/SDK 替代的 HTTP surface、cutover 后重复 CP 实现                                                                                                                                           |
| Greenfield public         | CloudEnvironmentLease/Generation、managed workload controller、volume/endpoint binding、pairing coordinator、lease grants/metering/cleanup                                                                                                 |

## 3. 旧 P0～P6 阶段定义（历史 Gate/兼容映射，不再是当前实施顺序）

### P0：Freeze、inventory 与 characterization

- 冻结源 SHA、remote、dirty/worktree；
- 生成完整分类 manifest；
- 保存 legacy Synara managed-agent、T3 embedded Runtime 和可复用机制（allocation/fencing/workspace/broker/
  release/pairing）的 happy/failure/restart characterization；
- Managed Host 是 greenfield：只建立 spec、negative fixtures 和 public reference-host baseline，不伪造
  “重构前 managed-host”证据；
- 决定公共/adapter/Synara-only owner；
- 完成 license/secret provenance。

### P1：Contracts、SDK 与公共 foundation

- Tenant/Organization/Project/Membership/basic RBAC；
- Managed Agent、Managed Host、Worker、Platform Adapter contracts；
- 独立 versioned Contracts、TS/Go SDK、stable errors、watch cursor；SDK exact pin contract digest；
- Postgres schema/migration、idempotency、outbox、leader/reconciler；
- durable PlatformOperation/Attempt/Receipt/Finalizer；
- provider catalog/capability/release/attestation、admission/fairness/quota/backpressure；
- service account/workload identity、OIDC/local auth、rate limit 与 audit/usage facts。

P1 的数据与传输 foundation 按 [ADR-0007](../adr/0007-p1-contract-data-toolchain-foundation.md) 冻结：

- Postgres CI compatibility matrix 固定覆盖 major `15`、`16`、`17`；每个 job 必须进一步固定 patch version
  与 OCI image digest，closure record 同时记录 server version、image digest 和 client/toolchain version，禁止仅使用
  `postgres:15` 等可漂移 tag 作为证据；
- Go persistence 固定使用 `pgx/v5`、`pgxpool` 与显式手写 SQL；禁止 GORM、ORM schema generation、
  `AutoMigrate`，也禁止沿用 Synara migration 编号或把 legacy migration 作为公共 schema authority；
- management/agent/host JSON API 以 JSON Schema 数据模型和 OpenAPI 路由映射为 authority；Worker/Platform
  Adapter wire 以 Proto + Connect/gRPC mapping 为 authority。两条传输面各自从唯一 authority 生成
  golden/negative fixtures；仅对显式共享语义类型增加跨面 mapping fixture，不允许 SDK 或 server 手写出第二套
  wire model；
- 数据隔离采用 composite tenant foreign key 加 `ENABLE ROW LEVEL SECURITY` / `FORCE ROW LEVEL SECURITY`。
  runtime role 必须是非表 owner、没有 `BYPASSRLS`，每个事务通过 `SET LOCAL` 写入并验证 tenant context；
  migration owner 与 runtime role 分离，只有经 ADR 与测试批准的 global table allowlist 可以不带 tenant RLS；
- live-instance compatibility 不能依赖临时进程列表：实例版本、schema range、最后 heartbeat 和 drain 状态进入
  durable registry。contract migration 前必须证明所有 live instances 位于 N/N-1 支持窗口；unknown、
  stale-but-not-expired，以及 expired 但尚无同 incarnation/generation fencing + termination + revoke +
  claim/leader release durable retirement receipt 的实例一律 fail closed。只有完整 retirement receipt 才能把过期
  registration 从 live set 排除。

公共 migration 使用全新 `expand -> backfill -> contract` lineage，不复制 Synara migration 编号；baseline 分域至少覆盖：tenant/org/project/
membership、idempotency/outbox/leader/operation receipt、managed-agent session/turn/execution/event/interaction、
worker identity/generation/release/attestation、workspace/materialization/checkpoint/cleanup、artifact、credential/
broker、target/quota/usage、managed-host lease/workload/volume/endpoint、adapter registry/finalizer、platform
manifest/compatibility。

每个 lineage entry 必须固定 migration ID、前置 migration、内容 checksum、阶段、最小/最大兼容 binary、重入语义
和 rollback 边界；执行器先取得 Postgres advisory lock，再核对数据库 ledger checksum，checksum 漂移、未知 migration
或越过未完成阶段均 fail closed。backfill 必须使用 durable cursor、固定 batch boundary 和 reconciliation report，
不能把一次性脚本或 CI 成功当成完成状态。

### P2：Managed Agent Plane

- Session/Turn/Execution；
- Worker claim/generation/fencing；
- Workspace/materialization/checkpoint primitive；
- Artifact 与 Credential Broker；
- Provider Runtime/Runtime Event projection；
- durable command/interaction/receipt/event sequence/resume cursor；
- Workspace source/repository revision/Git credential policy；
- Worker identity/incarnation/revocation/tombstone/storage scrub、retention/orphan cleanup；
- crash/replay/retry/stop/recovery。

### P3：Managed Host Plane（greenfield core）

- CloudEnvironmentLease/Generation；
- ProjectSource/WorkspaceSource、quota/admission/TTL 与 watch cursor；
- reference host workload/volume/state；
- signed `HostWorkloadDescriptor` contract、allowlist 与 compatibility；
- host bootstrap/admin contract：health/version/createPairingLink/revoke link/session/drain；
- workspace、host state、Runtime state 三类 volume binding；
- TLS/ingress/relay endpoint provision/revoke/drain；
- pairing issuance 的 durable record 只存 opaque ref/generation/scope/expiry/status，不存 URL/token/hash；
  一次性 secret 只走 no-store ephemeral claim response，丢失后 revoke + remint；
- lease-scoped BrokerPolicy 与多 Provider grant；
- Adapter registration/capability/trust root/workload identity；
- usage/metering facts、partial allocation/orphan discovery/compensating cleanup；
- durable operation/attempt/receipt/finalizer 和可发现资源标签；
- endpoint/pairing/proof session；
- broker/attestation/metering；
- create/ready/terminate；suspend/resume 延后。

P3 只关闭 generic `G-MANAGED-HOST`；真实 T3 artifact、pairing/revoke、checkpoint/reconnect 在 P6 关闭
`G-T3-INTEGRATION`，不得形成循环依赖。

### P4：Standalone deployment

- public CP/Worker images；
- Compose real Turn；
- Helm install/upgrade/rollback；
- local/Kubernetes/OIDC/S3 adapters；
- basic retention/cleanup、envelope encryption/key rotation、OTLP health/metrics/tracing/DLQ alert；
- backup/restore、HA、capacity/SLO、incident runbook。

P4 Standalone 默认包含 Managed Agent 与 reference Managed Host；真实 T3 profile 不是 Standalone 安装前提。

### P5：Synara cutover

- Synara 新增公共 SDK client/projection；
- shadow compare public CP 与 legacy CP；
- 按新 Tenant/Project 或 workload cohort 选择 single writer；
- 新 Session/Turn 路由公共 managed-agent；
- 活动 legacy Session 由 legacy writer drain；
- 删除已替代的公共能力源码，保留限期 read/migration compatibility；
- 企业 Billing/SAML/SCIM/compliance 通过公共 extension surface 接入。

### P6：T3Code 接入

- embedded 继续消费 Runtime，无 CP 强依赖；
- managed UI/management client 消费 Managed Host SDK；
- 新 `ManagedConnectionTarget`，direct/relay proof-bound exchange；
- 从 T3 repo 产出公开可获取、签名且 allowlist 的 T3 image/bundle + `HostWorkloadDescriptor`；
- T3 server 与 Runtime 同 Workspace；
- crash/reconnect/checkpoint/revert/soak E2E。

## 4. 数据切换

公共 CP 使用独立 schema/database namespace，不复用 Synara `agent_executions` 作为内部表。

| 数据                                                    | 单一 writer                         |
| ------------------------------------------------------- | ----------------------------------- |
| 公共 Tenant/Organization/Project/Session/Turn/Execution | Public CP                           |
| CloudEnvironmentLease/Generation/outbox                 | Public CP                           |
| actual workload/route/volume/grant                      | 对应 public/built-in adapter system |
| binding/receipt/accepted observation                    | Public CP                           |
| T3 Thread/Turn/SQLite/Git                               | T3 server                           |
| Synara enterprise billing/invoice/compliance            | Synara                              |

迁移规则：

- projection 使用 `source_event_id + resource_version` 去重；
- side effect 携带 `aggregateId + generation + operationId + fencingToken + releaseDigest`；
- adapter 只返回 observation/receipt，CP 决定状态转换；
- writer selector 对聚合生命周期 sticky；
- shadow 写隔离 schema/metrics，不拥有 side-effect credential；
- failback 只停止创建新聚合，活动聚合由原 writer drain；
- 禁止 reverse replication 写 authority 表；
- expand/contract migration 支持 N/N-1 rolling upgrade。

### 4.1 Migration ledger 与历史读取

- 所有 legacy/public aggregate 使用 namespace-qualified ID，不假设旧 UUID 全局无冲突；
- migration ledger 记录 aggregate kind、legacy/public ID、source version、writer epoch、cutover/drain/EOL 状态；
- read router 按 ledger 读取 public 或 legacy 历史，禁止通过“读不到就双写补一份”；
- audit/retention/export 在兼容期能跨 public/legacy 汇总，并标明 source/writer；
- legacy writer 在最后活动聚合 drain 前继续接受安全修复，但不增加新功能；
- 每个 cohort 有 decommission deadline、延期 owner 和数据删除/保留批准。

### 4.2 数据库迁移与 rollback

- 只采用 forward-only `expand -> resumable backfill -> code cutover -> contract`；
- 每个 migration 有 immutable checksum、Postgres advisory lock、schema compatibility range 和重复执行语义；
- backfill 按 durable cursor/batch 可暂停恢复，并有 mismatch/reconciliation report；
- tenant-owned tables 使用 composite tenant FK，并同时启用 `ENABLE ROW LEVEL SECURITY` 与
  `FORCE ROW LEVEL SECURITY`；runtime role 非 owner 且无 `BYPASSRLS`，事务必须以 `SET LOCAL` 设置 tenant
  context，缺失、非法或跨 tenant context 时 fail closed；
- migration owner 与 runtime role 分离；不受 tenant RLS 的 global tables 必须进入固定 allowlist，并有逐表 authority
  与隔离测试；
- durable live-instance registry 记录 binary、contract、schema range、heartbeat 与 drain state；contract preflight 要求
  所有 live instance 满足 N/N-1 compatibility；unknown、stale-but-not-expired，以及 expired 但没有同
  incarnation/generation retirement receipt 的实例阻止迁移。retirement receipt 必须证明 fencing/termination、
  endpoint/credential revoke 与 claim/leader release；
- Release manifest 固定最低可回滚 binary/schema 版本；contract 前验证旧 binary 已退出支持窗口；
- irreversible migration 必须有 freeze/批准、PITR restore point 和 restore drill；P1 只验收本地 logical
  backup/restore、checksum/advisory-lock 行为与 N/N-1 compatibility，部署级 PITR restore point、PITR drill、HA
  和故障切换到 P4 才能关闭；P1 仍须实现 fail-closed preflight contract，使部署执行在没有匹配 release/schema
  digest 的 restore point 与有效 restore-drill record 时拒绝进入 irreversible/contract 阶段；
- rollback 通常回滚 binary/traffic 并保留 expanded schema，不假装存在安全 down migration。

## 5. 迁移到公共仓后的删除规则

某能力只有满足以下条件才从 Synara 删除：

1. 公共源码/测试/SDK/Release 已固定；
2. Synara client/adapter 使用公共 API；
3. shadow/canary/cutover/failback 通过；
4. 活动 legacy 数据已 drain 或进入明确兼容期限；
5. data retention/DR/audit 已批准；
6. 删除后全仓搜索无第二个可编辑实现。

删除的是重复公共实现，不是 Synara 企业扩展和历史数据读取义务。

## 6. 回滚

- Runtime 与 Platform 分开回滚；
- 公共 CP rollback 必须保持 schema N/N-1、outbox cursor 和 active generation 可读；
- 新 Session/Lease admission 可停止，活动聚合不得换 writer；
- endpoint/grant/workload 先 revoke/drain，再降级/卸载；
- failed cleanup 继续 reconciliation，不能因 rollback 丢 finalizer；
- 每个阶段维护明确 RPO/RTO、backup/restore 和数据修复 runbook。
