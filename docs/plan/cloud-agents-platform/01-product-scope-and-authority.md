# 01. 产品范围与 Authority

## 1. 决策

按 [ADR-0032 / D-055](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)，第一阶段完整交付 **长期云工作区＋通用 Sandbox＋客户节点接入＋Admin Web**，共同就绪后再继续用户侧 CloudAgents 对话。基础设施与管理界面按能力一起实现、一起验收，不分成纯后端先交付、Admin 后补的两条轨道。

### 1.1 两层产品边界

| 层 | 拥有的能力 | 不应依赖 |
| --- | --- | --- |
| 基础设施底座 | Tenant/Org/Project/RBAC；Workspace/Volume/Snapshot；Sandbox 生命周期、Exec/PTY/Files/Ports；节点注册、调度、策略、访问、计量和恢复 | Agent Provider、Prompt、Turn、Synara/T3 私有服务 |
| 用户 CloudAgents | AgentSession/Turn/Execution、Approval/User Input、会话历史、Provider cursor、Artifact 和 Agent Checkpoint | 底座私有 Go 包、Docker/Kubernetes API 或节点凭据 |
| 基础设施管理界面（Admin Web） | Workspace/Sandbox/节点/策略/Operation/Audit 运维元数据和授权操作 | 用户对话、源代码或 Secret bytes 的读取权限 |

底座管理长期 Workspace 身份、Volume 绑定、文件系统快照及保留规则；应用在授权的数据通道内修改文件，
自己管理对话与 Agent Checkpoint。物理快照不声称保存 Provider 会话状态。默认同一 Workspace 同时只允许
一个可写 Sandbox，防止多个执行实例无约束地写同一卷。

底座可在不安装 Codex/Claude、不提供 Provider 凭据的情况下完成创建、连接、停止、重建和恢复。
普通用户可通过 Workspace API/CLI 使用自己的工作区；完整 CloudAgents User Web 后续接入。
这不赋予管理员读取用户内容的权限。

### 1.2 兼容与最终范围

已有 Agent Runtime/API 保留，当前只做安全、兼容和必要适配，不以新增对话功能推动底座里程碑。
下列三种运行模式及其 owner 表保留为消费/兼容边界，不再是底座领域模型或当前阶段排序。
T3 自己的逻辑 Workspace/Git/Checkpoint authority 不等于底座新建的物理 Workspace/Volume authority，
二者只能显式引用映射，不能双写同一聚合。公共仓最终仍须独立部署，并提供：

- Tenant/Organization/Project/basic RBAC 与标准 OIDC/JWT 接入；
- Managed Agent 的 Session/Turn/Execution；
- Managed Host 的 Environment Lease/Generation；
- Worker/Supervisor 注册、心跳、调度、fencing 与回收；
- Workspace/volume、checkpoint/snapshot primitive 与 Artifact；
- Credential binding/broker；
- Runtime/Provider release、capability 和 attestation；
- event stream、idempotency、audit/metering facts；
- 公共 OpenAPI/JSON Schema、TS/Go SDK 和 conformance；
- Docker Compose 与 Kubernetes/Helm 部署 profile。

“全部公共”指所有 Cloud Agent 通用产品能力只有一个公开可编辑来源。它不等于机械公开 Synara 全部 SaaS
后台；与 Cloud Agent 无关的企业扩展仍可由 Synara 作为外部消费者/adapter 提供。

## 2. 三种运行模式

### 2.1 Embedded Runtime

- 不要求部署 Go Control Plane；
- 宿主直接启动公共 Runtime；
- 宿主持有 Thread/Turn/Workspace/VCS/Checkpoint；
- 适合本地 T3、单机工具和自管桌面应用。

### 2.2 Managed Agent Plane

- 公共 Control Plane 持有 Tenant/Organization/Project、CloudAgentSession、Turn、Execution 和幂等；
- 公共 Worker/Supervisor 持有 Workspace 物化、Provider 运行、Artifact 和 Credential Broker；
- Synara native 通过公共 API/SDK 使用该平面；
- 其他 GUI/CLI 也可以作为纯客户端直接接入。

### 2.3 Managed Host Plane

- 公共 Control Plane 持有 CloudEnvironmentLease、Generation、workload、endpoint 和 grant lifecycle；
- 完整 T3 server、Runtime、Workspace、Git、Terminal 和 checkpoint 位于同一 lease；
- T3 server 持有 Thread/Turn/Workspace authority；公共 CP 不写 T3 Turn；
- T3 客户端通过 proof-bound managed connection 接入。

## 3. 单一 owner 表

“状态 writer”“物理 actuator”“内容 writer”“receipt issuer”必须分开；actuator/Worker 不能因为执行了副作用就
自行推进 Control Plane durable state。

| Mode               | Resource                                                       | Durable state writer        | Physical/content actuator                                         | Receipt issuer                                                                                        |
| ------------------ | -------------------------------------------------------------- | --------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| global             | PlatformTenant/PlatformOrganization/PlatformProject/Membership | 公共 CP                     | 公共 API；上游 IdP 只做 provisioning/PDP                          | CP audit/outbox                                                                                       |
| managed-agent      | Session/Turn/Execution/command/event cursor                    | 公共 CP                     | Runtime/Worker 执行 Turn                                          | Runtime/Worker 发 event/receipt，CP 验证后落库                                                        |
| managed-agent      | Workspace binding/checkpoint metadata                          | 公共 CP                     | 公共 Worker 是 content/checkpoint writer；volume adapter 只持久化 | Worker 签发 snapshot/materialization receipt                                                          |
| managed-agent      | Artifact metadata/retention                                    | 公共 CP                     | Worker 上传；object-store adapter 存 bytes                        | Worker 签发包含 object-store ETag/digest/proof 的 receipt，CP 验证后转状态                            |
| managed-agent/host | CredentialGrant lifecycle                                      | 公共 CP                     | 公共 Broker 发放/撤销；secret-source adapter 只取 secret          | Broker receipt                                                                                        |
| managed-host       | CloudEnvironmentLease/Generation                               | 公共 CP                     | Supervisor/scheduler 执行 allocate/revoke/reap                    | Supervisor/adapter operation receipt                                                                  |
| managed-host       | Workload/Volume/Endpoint binding                               | 公共 CP                     | scheduler/volume/ingress adapter                                  | adapter receipt；CP 决定 phase                                                                        |
| managed-host       | pairing link/hash/session                                      | lease 内 T3 auth store      | T3 auth service mint/consume/revoke                               | T3 签发只含 pairingRef/generation/subject/scope/expiry/status 的 durable receipt；不含 URL/token/hash |
| managed-host       | T3 Thread/Turn/Workspace/Git/Checkpoint                        | lease 内 T3 server          | T3 server/filesystem                                              | T3 内部 receipt；公共 CP 不保存 Turn 终态                                                             |
| embedded           | Thread/Turn/Workspace/Checkpoint                               | embedded 宿主               | embedded 宿主                                                     | embedded 宿主                                                                                         |
| all                | Provider Runtime session                                       | 当前 host command authority | 公共 Runtime/Provider process                                     | Runtime terminal/event receipt                                                                        |
| extension          | invoice/commercial/compliance decision                         | Synara enterprise system    | Synara extension service                                          | versioned extension receipt；不能改公共资源状态                                                       |

外部资源必须带 `aggregateId + generation + operationId + releaseDigest` 可发现标签。CP crash 或 receipt 丢失
后，reconciler 通过标签重新发现 side effect；不能只依赖 Supervisor 进程内状态。

Pairing 是 durable operation 的特例：一次性 `pairingUrl`/token 只能通过 request-scoped、`no-store`、全链路
redacted 的 ephemeral response 返回，不能进入 operation receipt、outbox、audit detail、日志或 tracing。
durable operation 只保存 opaque `pairingRef`、generation、subject/scope、expiry 和状态。若响应在客户端确认前
丢失，平台必须先按 `pairingRef` 撤销旧 link，再 mint 新 link；不得从数据库、outbox 或日志重放 secret。

## 3.1 Tenant 与授权关系

公共 v0.1 数据层固定为：

```text
Tenant (security and isolation root)
└── Organization
    └── Project
        └── Membership / ServiceAccount / WorkloadIdentity
```

这里的 Tenant 是中立平台隔离根，不是 Synara 私有表。Synara 初始采用一对一 tenant mapping，映射关系带
source namespace/version。

本文用 `PlatformProject` 指公共 CP 的租户/授权项目，用 `T3WorkspaceProject` 指 lease 内 T3 自己的
workspace/project。两者使用 namespace-qualified ref 显式映射，但不是同一聚合，也不共享 writer。

- 公共 CP 是 management/admission plane 的 PEP，也是 public membership/RBAC 的 durable writer；
- lease 内 T3 auth service 是该环境 HTTP/RPC/WebSocket 的 data-plane PEP；它必须把 public membership
  version、lease generation、subject 和 scope 当作上游约束，并在 stale/revoke 状态 fail closed；
- OIDC/SCIM/provisioning 把 identity、membership、suspension、deprovision 单向同步到公共 CP；
- Synara enterprise entitlement 可作为版本化外部 PDP 约束，最终允许条件是 public RBAC 与 PDP constraint
  的交集；timeout/error fail closed；
- suspension/deprovision 在公共 CP 本地事务中禁用 membership、递增 `revocationEpoch` 并写入 revoke outbox；
  对 T3/ingress 的传播是有界收敛，不得误称跨服务原子；
- 外部 PDP/IdP 不直接写 Session/Turn/Lease 表，也不能签发 Worker/Supervisor/Broker credential。

跨 PEP freshness 固定使用短 TTL、签名的 `LeaseAuthorizationSnapshot`，至少绑定 tenant、membershipVersion、
revocationEpoch、leaseId、generation、subject、scope、audience、issuedAt/expiry 和 policy digest。T3 data-plane
PEP 每 15 秒刷新，snapshot TTL 不超过 60 秒；无法刷新时拒绝新 session/grant，并在 expiry 后关闭现有
HTTP/RPC/WebSocket。紧急 revoke/terminate 还要先 fence ingress，再通过 durable revoke operation 让 T3
撤销 link/session。正常网络下目标撤销收敛不超过 30 秒，任何分区下硬上限为 60 秒；超限即 endpoint
fenced/不可用，而不是继续使用 stale authorization。

### 3.2 跨 namespace 引用与中立授权

P1 的跨系统引用统一使用公共 `NamespaceRef`，而不是把 Synara/T3 的表主键或 slug 提升为平台 ID。其
wire identity 固定为必填 `{namespace, kind, id}`；generation、版本、显示名和 endpoint 不属于 identity，
必须作为独立字段传递。`namespace`/`kind` 的 lowercase grammar、`id` 的 UTF-8/NFC 规则、RFC 8785 canonical
bytes、SHA-256 digest、派生 URN 与跨语言 fixture 以
[ADR-0007](../adr/0007-p1-contract-data-toolchain-foundation.md) 为唯一规范。解析器不得静默 lowercase/trim、
猜测前缀，或把外部 ref 当成本地聚合。

公共 CP 的 neutral RBAC contract 只定义 `SubjectRef`、versioned `Role`、`RoleBinding`、scope 与显式
permission，不携带 Synara 套餐、T3 workspace role 或企业 entitlement 名称，也不允许 wildcard/implicit
owner 扩权。公共 CP 是这些 binding 的 durable writer 和 management/admission PEP；外部 IdP/PDP 只能提供
identity/provisioning 或收紧授权，不能扩大公共 RBAC 结果。跨 namespace 映射是显式、版本化且可审计的
mapping，不改变任一侧 writer。

Postgres 行级隔离与 RBAC 是两道独立防线：所有 tenant-owned 表必须有 `tenant_id` 和 tenant-scoped
composite foreign key；在线 application role 对这些表启用并 `FORCE ROW LEVEL SECURITY`，每个事务先设置
并验证 tenant context。migration/maintenance owner 与在线 role 分离，不得服务请求；缺失、未知或不匹配的
tenant context 一律 fail closed。RLS 不能替代 API authorization，API RBAC 也不能绕过 RLS。字段、canonical
encoding、permission vocabulary 和 policy fixture 的正式冻结由 ADR-0007 与 P1 contract bundle 共同记录。

## 4. 公共与 Synara 专属的分类规则

### 必须进入公共仓

- Provider Host wire、Runtime Event、capability、stable errors；
- Session/Turn/Execution 的通用状态机与幂等；
- Environment Lease/Generation/fencing；
- Worker/Supervisor protocol；
- Workspace/Artifact/Credential 的通用 contract 与服务；
- Postgres schema/migration、outbox、leader/reconciler；
- OIDC/JWT、basic tenant/organization/project/RBAC；
- local/container/Kubernetes、filesystem/S3 等公开 deployment adapters；
- SDK、CLI、Compose、Helm、SBOM/provenance、conformance。

### 留在 Synara 的产品能力

- Synara 产品 UI、商业套餐、价格、invoice 和 entitlement；
- Synara 特有的 compliance/legal hold/incident/DR governance；
- Synara 专属云账号、私有 ingress、内部 KMS/HSM 或审计 sink；
- 现有 SaaS 数据迁移兼容层和过渡 projection。

### Deferred optional public extensions

- SAML/SCIM、enterprise identity federation；
- legal hold、privacy/export、advanced retention；
- Vault/AWS/Azure/GCP KMS 和多云 scheduler adapters。

这些通用企业能力可以后续公共化；在进入公共仓前由 Synara 通过版本化 extension surface 提供，不能被
永久误标为 Cloud Agents 无关能力。

### 不得进入任一公共权威

- T3 内部 Effect SPI、SQLite、Thread/Turn/Git/Checkpoint；
- Synara UI/Effect contract；
- 旧 Synara Go helper、server/domain/database struct 或旧 `docs/contracts` prose；它们只能作为迁移 oracle，
  不能成为 public wire/SDK ABI；
- 真实 tenant、credential、pairing token、内部 endpoint 或生产数据库 dump。

## 5. Synara 旧 Go Control Plane 的最终去向（按需迁移）

本节仅指 Synara 仓库中的旧 `services/control-plane`，不是当前 Cloud Agents 仓库的同名目录。
当前 Cloud Agents 的 `services/control-plane` 是公共实现，保留并演进；不得据本节将其整体迁出、重写或列为删除对象。
只有在获授权的 Synara 消费者迁移任务中，才核验并复用已有逐 package inventory，补充实际差异，按下表处理旧实现；这不是 BASE 工作的前置步骤：

| 分类           | 处理                                                            |
| -------------- | --------------------------------------------------------------- |
| Move           | 纯通用代码携来源 SHA/tree hash/许可证迁入公共仓                 |
| Rewrite public | 当前强耦合 Synara model，但能力本身通用；在公共 contract 下重写 |
| Adapter        | 平台 side effect；改为公共 plugin/adapter protocol 的实现       |
| Synara-only    | 企业/商业/合规能力留在 Synara                                   |
| Retire         | 被公共实现替代后删除，保留 migration/read compatibility 期限    |

迁移完成后，Move/Rewrite public 的修复只能进入 `hxp0618/cloud-agents`。Synara 不得保留可编辑 fork。

## 6. 明确否决

- 把 994 个 Go 文件不分类地整体复制；
- 让 Synara 和公共 CP 双写 Session/Turn/Execution；
- 让 T3 embedded 必须部署 Go；
- 让 managed-host 的 T3 Turn 同时进入公共 Managed Agent Plane；
- 用“共享数据库表”代替公共 API/SDK；
- 用源码公开、一次握手或镜像构建声称平台已可用。
