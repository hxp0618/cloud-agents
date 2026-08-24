# ADR-0007：P1 Contract、Data 与 Toolchain Foundation 冻结

- Status：Accepted
- Date：2026-08-10
- Decision owner：hxp0618
- Implementation executor：Codex
- Gate reviewers：各 Gate 指派与 executor 独立的 reviewer
- Supersedes：无
- Extends：[ADR-0006](0006-public-cloud-agents-platform.md)

## 背景

ADR-0006 已固定 `hxp0618/cloud-agents` 是 Portable Runtime、公共 Contracts/SDK、Go Control Plane、
Worker/Supervisor 与可部署平台的唯一公共来源。P0 inventory 又确认：旧 Synara Go server helper 只能作为
Control Plane/Worker `internal` 的迁移候选，旧 `docs/contracts/*` 只能作为 legacy oracle；两者都不能进入
新的公共 SDK 或正式 wire authority。

P1 开始实现前仍需消除以下多义性：同一业务对象是否会同时由 OpenAPI、JSON Schema 与 Proto 定义，
Control Plane 与 Worker 是否会形成 Go 包级耦合，旧数据库模型是否会连同 GORM/迁移编号一起迁入，以及
跨协议 resource reference、RBAC、migration safety 与生成器供应链采用什么可复现规则。若这些问题留给各
实现自行选择，会重新产生多个可编辑来源，且无法给 `G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` 和
`G-SECURITY-P1` 提供稳定输入。

本 ADR 只冻结 P1 foundation 的设计输入和首个最小切片，不声称任何 contract、SDK、服务、migration、测试、
Gate、制品或部署已经完成。

## 决策

### 1. API surface 与 transport

按 authority plane 固定两类 wire：

| Surface                                 | 唯一 wire authority               | Transport                                                                 | 身份与边界                                                    |
| --------------------------------------- | --------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------- |
| Management、Managed Agent、Managed Host | OpenAPI 3.1 + JSON Schema 2020-12 | HTTP/JSON                                                                 | 用户、管理客户端及宿主消费的公共 API                          |
| Worker/Supervisor、Platform Adapter     | Proto3 message/service            | ConnectRPC；仓外连接必须使用 HTTP/2 + mTLS，并保持标准 gRPC compatibility | workload-to-workload、generation/operation fenced 的公共 wire |

Worker/Supervisor 和 Platform Adapter 的服务实现不得另建手写 HTTP/JSON DTO 或第二份 JSON wire。ConnectRPC
是选定的调用框架，不改变 Proto3 作为 message/service 唯一权威；生成物必须可由标准 gRPC client 在已声明的
compatibility profile 下调用。仓外生产通信禁止明文 h2c，开发期 loopback 测试例外必须显式配置且不能进入
发布 profile。

该决定关闭计划中的 Q-002。Management/Managed Agent/Managed Host 不因本决定改成 Proto/gRPC；它们保持
OpenAPI 3.1 HTTP/JSON。

### 2. Contract authority 与 legacy 边界

JSON surface 遵循以下单向生成/引用关系：

```text
JSON Schema 2020-12 model
        ├── OpenAPI 3.1 $ref + route/auth/header/status/operationId
        ├── TypeScript model/validator/client
        ├── Go SDK model/validator/client
        └── server validation + golden/negative fixtures
```

- JSON Schema 2020-12 是所有 JSON data model 的唯一可编辑来源，拥有字段、required、枚举、format、约束、
  union 与 compatibility 语义。
- OpenAPI 3.1 只拥有 route、HTTP method、auth requirement、header、status code、content type 与
  `operationId`，schema 必须 `$ref` 到同版本 JSON Schema，不得复制或放宽 model。
- Proto3 是 Worker/Supervisor 与 Platform Adapter message/service 的唯一可编辑来源；不得从 JSON Schema
  推断同名但语义不同的 workload wire，也不得在 OpenAPI 中再定义这些 RPC。
- generated TS/Go SDK、server validator 和 fixture 必须记录 contract digest、generator identity/version 与
  generation config digest；修改 generated output 不修改 source contract 的检查必须 fail closed。
- Synara legacy schema、prose、Go struct、handler 和测试只可放在 `conformance/**/legacy-oracles` 或 inventory
  指定的 `internal` 候选位置，用于语义比对和 characterization；它们不是新 wire authority，不可被正式
  schema、SDK 或 server import 为生产 contract。
- breaking change 必须遵守新 API major/新 schema identity。mutation request 直接按 strict schema 校验并拒绝未知字段；
  response/watch reader 则先把 raw object 中未知字段按原始 JSON value 保存到独立 sidecar，再对 known projection
  运行同一 strict schema，重新编码时合并未冲突的 sidecar。unknown-field preservation 是 generated reader seam，
  不是让 mutation schema 接受任意属性，也不得原地重解释已发布字段；P1 后续仍须用 N/N-1 generated-reader
  fixtures 证明该行为。

### 3. 三个 Go module 与 import DAG

Go source 固定为三个独立 module：

| Module                                                   | Public responsibility                                      |
| -------------------------------------------------------- | ---------------------------------------------------------- |
| `github.com/hxp0618/cloud-agents/sdk/go`                 | 仅 generated contracts、validator 与公共 client            |
| `github.com/hxp0618/cloud-agents/services/control-plane` | Control Plane 与 CLI；domain/service/store 全部 `internal` |
| `github.com/hxp0618/cloud-agents/services/worker`        | Worker/Supervisor；domain/service/actuator 全部 `internal` |

contract authority / generation flow 是：

```text
contracts --> sdk/go --> services/control-plane consumer
                   \--> services/worker consumer
```

这里的箭头表示 authority/生成/消费流，不表示 Go import 的方向。Go import edges 固定为
`services/control-plane -> sdk/go` 与 `services/worker -> sdk/go`；`sdk/go` 不 import 任一 service。Control Plane
与 Worker 不得互相 import module，更不得 import 对方 `internal`；二者只通过第 1 节冻结的 Proto/ConnectRPC
wire 协作。外部 consumer 只能依赖 `sdk/go` 或公共 service API，server domain、database model、store、handler
和 adapter implementation 都不是公共 Go ABI。

根 `go.work` 只用于仓内开发。每个 module 必须能在 `GOWORK=off` 下独立 build/test/package；release module
的 `go.mod`、tag、provenance 与发布命令禁止 `replace`，不得依赖相对路径或未发布 workspace state。

### 4. Go toolchain

P1 固定 Go `1.26.6`。三个 module、生成器容器/二进制、CI 与 evidence 必须记录该完整 patch version；不得只写
`1.26` 或使用浮动 `latest`。原 `1.26.5` pin 因 2026-08-13 发布的 `GO-2026-6090`、`GO-2026-6088`、
`GO-2026-5972` 在当前 Control Plane symbol scan 中可达而失效；`1.26.6` 是三项在 Go 1.26 line 的共同 first-fixed
版本。升级 Go 属于新的、独立评审的 toolchain decision，并使受影响的 build/test/generation evidence 失效。

### 5. PostgreSQL 与数据访问

- P1 公共数据层只支持 PostgreSQL major 15、16、17；Gate matrix 至少覆盖三个 major 的受支持最新 patch，
  evidence 必须记录实际 server version，不能用 SQLite 或 mock 替代。
- Go driver 固定为 `pgx/v5`；SQL、transaction boundary、lock、isolation、claim 与 retry 语义必须在仓内显式
  编写并评审。ORM model 不是 schema authority。
- 禁止 GORM、`AutoMigrate`、运行期隐式 DDL，以及复制 Synara 旧 migration 编号/ledger。公共 schema 建立
  全新、连续且带 checksum 的 migration lineage；旧 migration 只作 semantic oracle/provenance 输入。
- schema/migration、query 和 Go domain model 分层；数据库 row struct 不成为公共 SDK contract。

### 6. Tenant integrity、RLS 与数据库角色

所有 tenant-owned table 必须携带 `tenant_id`，跨表引用使用包含 `tenant_id` 的 composite foreign key；只靠
应用层 where clause 或单列 object ID 外键不构成隔离。tenant-owned natural/opaque ID 的唯一性也必须包含
tenant scope，除非该 ID 被明确证明为全局 authority。

tenant-owned table 必须启用并 `FORCE ROW LEVEL SECURITY`。runtime connection role：

- 不是 table owner、不是 superuser、没有 `BYPASSRLS`；
- 通过 transaction-local、fail-closed tenant context 使用 policy；缺失、非法或跨 transaction 泄漏 context
  时请求失败；
- 只拥有运行所需的最小 DML/sequence/function 权限，不能执行 migration DDL。

migration owner 是独立、非运行期角色，只用于受控 migration job；应用服务不持有其 credential。migration
owner 不得成为日常 request path 的连接用户。bootstrap/admin 的跨 tenant 操作必须走单独、可审计的显式
路径，不能通过给 runtime role 授予 `BYPASSRLS` 实现。

composite FK 与 FORCE RLS 是并行约束：任何一个都不能替代另一个。

### 7. `NamespaceRef` 的唯一表示

跨 contract 的中立 resource reference 固定为：

```json
{
  "namespace": "cloud-agents",
  "kind": "project",
  "id": "opaque-id"
}
```

规则如下：

1. `namespace`、`kind`、`id` 都是必填非空 string；v1alpha1 不接受额外字段。
2. `namespace` 与 `kind` 使用 ASCII lowercase，格式固定为
   `^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`；输入不得静默 lowercase。
3. `id` 先校验为合法 UTF-8，再由唯一的 normalization API 转换为 Unicode NFC；`id` 大小写敏感，不做
   lowercase、trim、路径 clean 或 percent decode。签名、授权、持久化和 canonicalization 只能消费转换后的
   值，不允许各调用点自行处理。
4. canonical bytes 是该三字段对象按 RFC 8785（JCS）产生的 UTF-8 JSON；字段集合与值均以 normalized
   `NamespaceRef` 为输入。
5. canonical digest 固定为 `SHA-256(canonical bytes)`，文本形式为 `sha256:<64 lowercase hex>`。
6. derived URN 固定为 `urn:cloud-agents:ref:sha256:<64 lowercase hex>`。URN 只作稳定派生标识，不能替代
   原始三元组做授权、展示或 provenance。

JSON Schema、Proto、TS SDK、Go SDK 与数据库 helper 必须共享上述 golden/negative fixtures；任何语言生成
不同 canonical bytes、digest 或 URN 都必须 fail closed。

### 8. 中立 basic RBAC

公共平台的授权词汇不包含 Synara commercial plan、enterprise entitlement、T3 product role 或宿主内部 ID。
permission 采用 `<resource>.<verb>`，P1 verbs 固定为 `create|get|list|watch|update|delete|act|bind`；permission
必须显式列出，不支持 `*` 隐式扩张。

内置 basic roles：

| Role                 | Scope        | P1 meaning                                                                      |
| -------------------- | ------------ | ------------------------------------------------------------------------------- |
| `platform.admin`     | platform     | bootstrap/recovery 专用；显式授予全部已登记 permission                          |
| `tenant.admin`       | tenant       | tenant 内 organization/project/membership 与 policy 管理                        |
| `organization.admin` | organization | organization 及其 project/membership 管理                                       |
| `project.admin`      | project      | project resource、membership、policy 管理                                       |
| `project.operator`   | project      | 执行经登记的 lifecycle `act`，读取/watch operation；不能管理 membership         |
| `project.developer`  | project      | 创建/读取/更新允许的 project-scoped resource；不能删除 policy 或管理 membership |
| `project.viewer`     | project      | 只读已明确登记的 get、list、watch permission                                    |

role 只是一组 versioned permission 的集合；binding 必须固定 subject、role、scope `NamespaceRef`、tenant、
version 与 lifecycle。父级 scope 不自动产生未登记 permission，future resource/verb 不自动加入旧 role version。
多个 binding 取显式 allow 的并集，但任何缺失身份、无效 scope、未知 role/permission、过期/revoked binding 或
无法完成 tenant 约束的请求均 default deny。P1 不引入通配 allow、隐式 owner、宿主 superuser 映射或
“认证即授权”。外部 enterprise PDP 只能进一步收紧 public RBAC，不能扩大它。

### 9. Live-instance migration preflight

任何可能改变 schema compatibility range 的 migration，在获取 migration lock 和执行 DDL 前必须运行
live-instance preflight：

- 校验数据库 major、当前 migration ledger/checksum、目标 bundle digest 与 expand/contract phase；
- 读取带 TTL 的 Control Plane/Worker live-instance registration/heartbeat，固定每个实例的 binary version、
  supported schema range、writer epoch 与 rollout generation；
- 证明当前所有未过期实例以及 rollback target 都能读取目标 schema，并且当前 writer 能安全写入；
- 若 registry 不可用、存在 unknown/stale-but-not-expired version、range 不相交、checksum drift、活动旧 writer
  或无法证明的实例，则 fail closed，不执行 DDL；
- registration 过期本身不能证明实例已退出。只有 reconciler 以同一 instance/incarnation/generation 同时验证
  workload/process 已终止、generation 已 fencing、endpoint/credential 已 revoke、leader/claim 已释放，并写入
  durable retirement tombstone/receipt 后，preflight 才能从 live set 排除该 registration；仍存活但仅被 fence、
  或已终止但缺少任一 revoke/release 证明，都不是完整 retirement。expired 但没有完整 receipt 的实例按 unknown
  处理并继续阻断；
- contract/destructive step 还必须证明旧 reader/writer 已 drain，并引用备份/PITR restore point 与单独批准。

仅依赖 deployment desired replica、人工口头确认或“Pod 看起来已重启”不满足 preflight。首次 bootstrap 可在
空 registry 下进入明确的 bootstrap mode；该例外不能用于已有 schema 的升级。

### 10. Dependency、generation 与责任锁

- 三个 Go module 和所有 contract/SDK generator 都必须使用 checked-in dependency/tool lock；direct
  dependency、generator binary/container、plugin、template/config 分别记录 version 与不可变 digest。
- generation 必须在 clean archive、固定 Go `1.26.6` 和固定 generator lock 下可复现；重新生成后 dirty diff、
  provenance 缺失、跨 module `replace` 或 contract digest 不匹配均 fail closed。
- 新增或升级第三方 dependency 前，必须记录用途、来源、version/digest、transitive license、notice、已知安全
  风险与替代项。repo owner hxp0618 是 accountable dependency/license 决策 owner；未参与该 dependency 实现的
  Codex supply-chain reviewer 负责独立 evidence review，并须在 dependency 进入已提交 module/generator lock 前
  签署逐项结论。proprietary/unknown/冲突或需法律解释的 license 一律阻塞并请求 hxp0618/具名 legal reviewer
  明确决定，自动化 reviewer 不得自行豁免。candidate/release 仍需独立复核 allowlist/exception。该流程关闭
  Q-008 的 owner/reviewer 多义性，但不代表任何依赖已获得 license approval。
- P1 DRI/decision owner 为 hxp0618；Codex 是 implementation/evidence executor。`G-CONTRACT`、`G-DATA`、
  `G-AUTHORITY-P1`、`G-SECURITY-P1` 分别由未实现该 Gate 证据的独立 reviewer 复核。executor 自检不能替代
  独立 closure record。

### 11. P1-A 最小实现切片

P1-A 只允许建立可纵向验证上述决定的最小 foundation：

1. 建立 JSON Schema/OpenAPI/Proto contract roots、版本与 generator lock；实现 common error、pagination、
   idempotency、watch/cursor 和 `NamespaceRef` fixtures。
2. 为 Tenant/Organization/Project/Membership/basic RBAC 提供最小 Management HTTP/JSON contract、generated
   TS/Go client/model/validator，以及对应 server validation seam。
3. 为 Worker/Supervisor 与 Platform Adapter 提供 version/capability/health 握手的最小 Proto3 service、
   ConnectRPC/gRPC compatibility 生成物和 mTLS negative fixtures；不执行真实 workload/adapter side effect。
4. 建立三个独立 Go module 和 `GOWORK=off` DAG checks。
5. 建立全新 PostgreSQL migration ledger，以及 tenancy/membership/RBAC、idempotency、outbox、leader 与
   PlatformOperation/Attempt/Receipt/Finalizer 的最小 schema；使用 `pgx/v5`、composite tenant FK、FORCE RLS、
   分离角色及 live-instance preflight seam。
6. 建立 local auth/service identity、default-deny authorization evaluator 与审计事实的 foundation fixtures；
   不伪造真实 OIDC、生产 workload identity 或跨 PEP revocation 结论。

P1-A 是 P1 的第一个 implementation slice，不等于 P1 Exit。它不得声称或授权：

- 完成 Managed Agent Session/Turn/Execution、真实 Provider Turn、Workspace/Artifact/Credential Broker；
- 完成 Managed Host Lease/Generation/pairing、T3 integration 或 HostWorkloadDescriptor；
- 执行 Worker claim、scheduler/volume/ingress/secret side effect，或完成 Platform Adapter implementation；
- 对生产数据库执行 migration、导入 Synara 数据、drain/cutover 任一 writer；
- 完成真实 OIDC、enterprise PDP、signed authorization snapshot、DPoP 或生产 mTLS issuance；
- 关闭任一 P1 Gate、aggregate Gate、M1 或 P2-P6 Gate；
- package/module/image/chart publication、GitHub visibility 变更、Compose/Helm 部署、云端部署、Beta 或 GA。

P1-A 之后仍需按 Gate record 模板补齐完整 reliability、N/N-1、PostgreSQL 15-17、RLS bypass negative、
migration/restore、auth/security、生成可复现与独立 reviewer evidence，才能逐项讨论 P1 Exit。

## 被否决的方案

### 所有 surface 统一成 OpenAPI/HTTP JSON

否决。Worker/Adapter 的双向 workload RPC、deadline、streaming 与 gRPC ecosystem 会另生私有协议；Proto3 +
ConnectRPC/gRPC compatibility 更适合作为唯一 workload wire。

### OpenAPI、JSON Schema、Proto 各自维护相同 model

否决。三份手写 data model 会漂移，server validator 与 SDK 也无法证明同源。JSON 与 workload RPC 按 surface
划分 authority，不复制同一 wire。

### Control Plane 直接 import Worker implementation

否决。这会把两个发布 train、部署边界和 server internals 绑定成第二个 in-process ABI。

### 搬入 Synara GORM model、AutoMigrate 和 migration 编号

否决。旧 schema 混有宿主历史与 authority，且运行期隐式 DDL、单层应用过滤不能满足公共 Postgres 多租户
隔离和可审计升级要求。

### 只使用 composite tenant FK 或只使用 RLS

否决。前者无法防止遗漏 tenant predicate，后者无法表达完整 referential integrity；公共基础层需要两者同时
fail closed，并分离 runtime/migration authority。

## 影响

- P1 contract 生成链和 Go module 边界在首行代码前可审计，legacy helper/schema 不会重新成为公共 ABI。
- PostgreSQL 采用全新 lineage，迁移工作需要显式 SQL、三 major matrix、RLS negative 与 live-instance
  compatibility evidence，初期实现量增加，但回滚/升级边界更清晰。
- `NamespaceRef`、RBAC 与 canonical digest 跨语言共享 fixtures，减少宿主 ID/role 泄漏；任何 canonicalization
  差异会被视为 protocol defect。
- ConnectRPC 不取消 gRPC compatibility；这会增加双协议 conformance 和 mTLS matrix，但避免锁定单一 client。
- P1-A 可以验证最小纵向链路，但所有运行时副作用、宿主接入、发布和部署继续等待后续阶段授权及 Gate。

## Status boundary

本 ADR 于 2026-08-10 冻结 P1 contract/data/toolchain foundation，并与 ADR-0006 的公共平台 source-of-truth、
authority plane、独立 release train 和阶段 Gate 边界一致。`Accepted` 只表示采用上述设计输入；当前仍未因此
产生实现、测试通过、Gate closure、release candidate、公开制品、部署或生产可用性结论。
