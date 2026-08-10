# ADR-0008：P1 PostgreSQL Data Kernel 冻结

- Status：Accepted
- Date：2026-08-11
- Decision owner：hxp0618
- Implementation executor：Codex
- Gate reviewers：`G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` 分别使用未实现对应证据的独立 reviewer
- Supersedes：无
- Extends：[ADR-0007](0007-p1-contract-data-toolchain-foundation.md)
- Amends：[ADR-0007 §11](0007-p1-contract-data-toolchain-foundation.md#11-p1-a-最小实现切片) 的执行拆分；不改变其总范围或 Gate
- Approval basis：用户已批准 ADR-0006/完整计划并授权按 Gate 持续实施；本 ADR 只收窄已批准 P1 的实现细节，不扩大权限

## 背景

ADR-0007 已冻结 PostgreSQL `15`–`17`、`pgx/v5`、手写 SQL、composite tenant foreign key、
`FORCE ROW LEVEL SECURITY`、分离的 runtime/migration role 与 live-instance migration preflight。P1-A Contract
Kernel 已建立公共 contract authority，但尚未创建数据库 schema、migration lineage 或运行期 persistence code。

在写入第一条公共 migration 前，仍需消除以下实现多义性：哪些表允许全局存在、首个 Tenant 如何建立、
tenant context 如何进入连接池事务、opaque ID 如何落库、migration manifest/checksum/bundle digest 如何计算，以及
P1 数据实现如何分片而不提前进入 P2 Session/Turn/Worker side effect。

本 ADR 冻结这些 P1 实现细节。它不声称 migration 已执行、数据库 Gate 已关闭、生产数据库可升级，或任何
P2–P6 能力已实现。

## 决策

### 1. 执行切片与 Gate 语义

ADR-0007 的 `P1-A` 是完整 foundation program，不因首个 Contract Kernel commit 完成。它细分为：

- `P1-A1 Contract Kernel`：bootstrap contract/toolchain（已完成 `e0562b2`）；正式 generation/SDK/N/N-1 仍在 P1；
- `P1-A2 Data Kernel`：本 ADR 的 PostgreSQL/authority/recovery 实现；
- `P1-A3 SDK/Identity/Closure`：generated SDK/server mapping、local auth、service/workload identity validation、
  issuer/audience/subject/scope/tenant/version/expiry/rotation/revoke negatives 与四个 P1 Gate closure evidence。

这些名称只是 P1 内部执行标签，不是新的阶段或 Gate。P1 Exit 仍必须由独立 closure record 同时关闭
`G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` 与 `G-SECURITY-P1`；在 A1–A3 全部完成前不得声称 ADR-0007 的
`P1-A` 完成。

P1-A2 按以下顺序提交，每一项只证明自己的边界：

1. `P1-A2.1 Migration/Tenancy`：migration manifest/runner/ledger、数据库角色、Tenant/Organization/Project、
   tenant-local resource revision/change log、tenant transaction helper、RLS 与 PostgreSQL 15–17 focused matrix；
2. `P1-A2.2 Membership/RBAC`：Membership、内置 role catalog、RoleBinding、default-deny evaluator；
3. `P1-A2.3 Durable Coordination`：idempotency、outbox、leader、PlatformOperation/Attempt/Receipt/Finalizer、
   最小 audit fact；
4. `P1-A2.4 Compatibility/Recovery`：durable live-instance/retirement、migration preflight、resumable backfill、
   local logical backup/restore 与 N/N-1 data evidence。

P1-A2 不实现 Session、Turn、Execution、Worker claim、Workspace、Artifact、Credential Broker、Lease、pairing、
scheduler/volume/ingress actuator 或真实 Provider Turn；这些仍由 P2/P3 及其 Gate 拥有。

### 2. Schema、identifier 与时间类型

- 公共 schema 名固定为 `cloud_agents`；所有 SQL identifier 使用未加引号的 lowercase `snake_case`。
- public opaque resource ID 使用 `text`，并以与
  [`identifier.schema.json`](../../../contracts/common/v1alpha1/schemas/identifier.schema.json) 相同的长度和字符约束
  校验；不得擅自收窄为 UUID、bigint 或宿主私有 ID。
- `resource_version`、generation、writer epoch、attempt number 与 fencing counter 使用带非负约束的 `bigint`；
  不使用 `serial`。
- `resource_version` 是 tenant-local、跨资源单调递增的 mutation revision：每次权威资源变化必须在同一事务中从
  `tenant_resource_versions` 分配一个新值，并同时写入资源行与 `resource_changes`。它既是 CAS 输入，也是 watch
  cursor 的排序事实；普通更新不得复用或倒退。`generation` 是 recreate/takeover 的 fencing epoch，只在替换
  authority instance 时增长，绝不能用 `resource_version` 代替，反之亦然。
- 首个 Tenant 是唯一 bootstrap 特例：audited bootstrap function 在一个事务中先插入
  `platform_tenants(resource_version = 1, tenant_id = tenant_uid)`，再插入带 composite FK 的
  `tenant_resource_versions(current_revision = 1)` 与 revision `1` 的 `resource_changes`。提交前任何一步失败会整体
  rollback；后续分配用 `SELECT ... FOR UPDATE` 锁 counter row。已提交 mutation 的 revision 必须从 `1` 连续无
  间隙；被拒绝或 rollback 的事务不消耗 revision。Tenant root/counter/change 三者不允许部分存在。
- 所有时间使用 `timestamptz`；deadline/expiry 必须显式保存时区。
- 金额/计量值后续使用精确 `numeric`；不得使用 float 保存可计费事实。
- tenant-owned 表的 primary/unique key 和 foreign key 以 `tenant_id` 为最左等值列；每个 foreign key、RLS
  predicate 与主要 claim/watch 查询必须有相符索引。等值列在前、时间/cursor range 列在后；pending outbox/
  cleanup 可使用与查询谓词一致的 partial index。

数据库 row struct 只存在于 Control Plane `internal` store；它不是 SDK 或 wire contract。

### 3. Tenant-owned 表与固定 global-table allowlist

`platform_tenants` 也属于 tenant-owned 表：它显式保存 `tenant_id`，并以 `tenant_id = tenant_uid` 的 check
约束表示隔离根。Organization、Project、Membership、RoleBinding、`tenant_resource_versions`、
`resource_changes`、idempotency、outbox、operation 与 audit 表都必须保存 `tenant_id`，使用 composite tenant
foreign key，并同时 `ENABLE` 与 `FORCE ROW LEVEL SECURITY`。

P1 仅允许以下 global table；新增任何 global table 必须修改本 ADR/allowlist、给出唯一 writer，并补隔离负例：

| Table                          | 唯一 writer                 | 原因与限制                                                                 |
| ------------------------------ | --------------------------- | -------------------------------------------------------------------------- |
| `schema_migrations`            | migration job               | 全数据库 migration lineage/checksum；runtime 只读                          |
| `migration_backfills`          | migration/backfill job      | 全数据库 durable cursor/reconciliation；runtime 不写                       |
| `schema_restore_evidence`      | audited migration/admin job | contract/irreversible preflight 的 release/schema/PITR 证明；不保存 secret |
| `workload_database_principals` | audited bootstrap admin     | `session_user` 到 instance/incarnation/capability 的非秘密、可撤销映射     |
| `live_instances`               | instance self-registration  | CP/Worker schema compatibility registry；只允许绑定自身 identity 的写入    |
| `instance_retirement_receipts` | reconciler                  | 对同一 instance/incarnation/generation 的完整 retirement 证明              |
| `leader_leases`                | Control Plane coordination  | 固定 allowlisted system leader 名称；不保存 tenant payload                 |
| `builtin_roles`                | migration job               | versioned neutral role catalog；runtime 只读                               |
| `builtin_role_permissions`     | migration job               | role 的显式 permission 集合；禁止 wildcard                                 |

provider/release catalog 尚未进入此 allowlist；P1 后续若要新增，必须先冻结 authority 与隔离规则。global table 不因
没有 RLS 就获得宽泛权限：每张表仍使用最小 GRANT、typed writer 与独立测试。

`builtin_roles` / `builtin_role_permissions` 是 immutable internal catalog，不是公开 `Role` resource 的第二份
wire/schema authority。HTTP `GET /v1/tenants/{tenantId}/roles/{roleId}` 固定 `roleId = role name`，reader 为路径
tenant 投影 catalog：

- `metadata.tenantRef` 来自已验证的 path/transaction tenant；`metadata.name = role name`；
- `metadata.uid = builtin-role~<role-name>~v<version>`；`metadata.resourceVersion` 使用 catalog 的正整数 revision；
- `metadata.createdAt` 使用该 catalog version 的发布时刻；
- `spec.name/version/permissions/state` 全部来自 global catalog，tenant 不能单独改变 deprecated/revoked 状态。

该 `Role` 是只读 derived catalog projection，明确不参加 tenant-local watch/CAS，也不写 `resource_changes`；它的
`resourceVersion` 属于 catalog domain，只在 global catalog version 变化时增长。当前 HTTP contract 只有 GET，
不得从该 projection 推导 tenant-local mutation revision。RoleBinding 才是 tenant-owned authority resource，其
create/revoke 必须分配 tenant revision 并进入 watch/change log。

RoleBinding 只固定 role name/version，不引用或伪造 tenant-local role row。`platform.admin` binding 必须使用
platform scope，且只能由 audited bootstrap function 创建；产品 `platform.admin` permission 与 PostgreSQL
`cloud_agents_bootstrap_admin` role 是两个独立 authority，互不授予、继承或映射。自定义 role 不在 P1 范围；
若后续引入，必须使用 tenant-owned 表和新 contract 决策。

### 4. 数据库角色与 bootstrap authority

checked-in cluster bootstrap 只创建三个 `NOLOGIN` group role，不创建或保存 password：

- `cloud_agents_migration_owner`：拥有 `cloud_agents` schema/table/function，用于受控 migration job；
- `cloud_agents_runtime`：在线请求的最小 DML/function role，非 owner、非 superuser、无 `BYPASSRLS`、无 DDL；
- `cloud_agents_bootstrap_admin`：首个 Tenant、恢复及显式跨 tenant 管理路径；非 superuser、无 `BYPASSRLS`。

实际 LOGIN/workload identity 与 role membership 由部署者在仓外注入。在线 Control Plane 配置只能持有 runtime
credential；migration credential 不得进入 runtime 配置、日志、fixture、镜像或 generated SDK。bootstrap admin
使用独立连接池/命令路径。它不获得 tenant table 的直接 DML，只能执行 `SECURITY DEFINER` 的 audited mutation
function；function 在同一短事务写业务事实和 redacted audit fact。function owner 固定为 migration owner，SQL
正文使用 fully-qualified object，`search_path` 固定为 `pg_catalog, cloud_agents`；撤销 `PUBLIC`/runtime EXECUTE，
只授予具名 bootstrap role。它不通过 `BYPASSRLS` 或 table-owner bypass。

cluster bootstrap SQL 是 migration bundle 的受摘要输入，但不是 schema migration ledger entry；它只能创建/
核验上述 group role，不创建业务表、不写业务数据。首次 schema bootstrap 仍由 `000001` migration 完成。
若同名 role 已存在，bootstrap 必须验证 `NOLOGIN/NOSUPERUSER/NOBYPASSRLS/NOCREATEDB/NOCREATEROLE`，并拒绝
该 group role 继承任何其他 role；属性或 membership 不一致时 fail closed，不静默 ALTER 降权。

`workload_database_principals` 由 audited bootstrap function 写入，至少绑定 exact PostgreSQL `session_user`、
service kind、instance ID、incarnation、允许的 registration/heartbeat/retirement capability、state 与 expiry。每个
workload LOGIN 都是 `cloud_agents_runtime` 的独立 member；`session_user` 不得是共享 group role。live-instance
register/heartbeat/retirement 只能调用具名 `SECURITY DEFINER` function，function 以 `session_user` 查询映射并只
写匹配 instance/incarnation 的行；runtime 不获得这些 global table 的任意 INSERT/UPDATE/DELETE。principal
撤销后所有函数 fail closed。reconciler 也使用自己的 session_user/capability mapping，不能冒充任意 instance。

migration runner 使用独立、短生命周期 LOGIN。连接后必须验证：`session_user` 不是任一 group role、在
`pg_auth_members` 中是 `cloud_agents_migration_owner` 的 direct member、不是 runtime/bootstrap group 的直接或
间接 member，且自身 `NOSUPERUSER/NOBYPASSRLS/NOCREATEDB/NOCREATEROLE/NOREPLICATION`。随后显式
`SET ROLE cloud_agents_migration_owner` 并核对 `current_user`，再取得 advisory lock；所有 schema/table/function
都由该 group role 拥有。完成或失败后 runner `RESET ROLE` 并关闭 dedicated connection，不得把它放回 runtime
pool。任一 membership/owner/attribute 不匹配时不执行 DDL。

### 5. Tenant context ABI

唯一数据库 tenant context 固定为 transaction-local GUC `cloud_agents.tenant_id`：

1. store 只能在显式 `pgx` transaction 内调用 `set_config('cloud_agents.tenant_id', $1, true)`；禁止 session-level
   `SET`、拼接 SQL 或把 tenant context 放入连接创建参数；
2. `cloud_agents.require_tenant_id()` 读取 `current_setting('cloud_agents.tenant_id', true)`，按公共 opaque identifier
   规则校验并返回 text；缺失、空、超长或非法值抛出 SQLSTATE `22023`；
3. tenant RLS policy 只比较 row `tenant_id = cloud_agents.require_tenant_id()`；bootstrap/migration policy 必须按
   具名 role 单独声明；
4. tenant context 由 transaction commit/rollback 自动清除。连接池测试必须证明下一借用者看不到上一事务的值；
5. store API 必须显式接收 normalized tenant ID。它不自行 trim/lowercase/path-clean，也不能接受调用方自报的
   canonical JSON/digest。

### 6. Migration lineage、manifest 与 checksum

公共 migration 位于 `services/control-plane/migrations/`，使用全新六位连续 ID：`000001`、`000002`……；
不得复制 Synara migration 编号。每个 entry 固定：

- `id`、唯一 `name`、精确 predecessor；
- `phase = expand|backfill|contract`；
- `schema_from` / `schema_to`；
- 最小/最大 compatible binary 声明；
- SQL/runner path、exact byte size、`sha256:<64 lowercase hex>`；
- transaction mode、重入语义、rollback boundary 与是否需要 live-instance/PITR preflight。

SQL 文件必须是 UTF-8、LF、无 BOM、Git mode `100644`。checksum 对 Git blob 的 exact bytes 计算，不做换行、
空白或 SQL normalization。strict manifest 还包含：lineage/schema version、advisory lock、global allowlist digest、
bootstrap artifacts，以及 runner/manifest validator/preflight source closure 中每个文件的 path、Git mode、size 和
SHA-256。`bundle_digest` 字段自身不参与输入；其余字段一个都不能排除。

bundle digest 固定为：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-migration-bundle/v1",
  "manifest": manifest excluding bundle_digest
}))
```

因此 predecessor、schema/binary compatibility、transaction mode、reentrancy、rollback、preflight、path/mode/
size/checksum 与 bootstrap/runner source 都被同一 digest 绑定。manifest 使用 strict JSON decoder，未知字段、重复
ID/path、断裂 predecessor、非连续 ID、checksum/size 漂移、未知 ledger entry 或当前 schema 越过未完成 phase
都 fail closed。RFC 8785 实现必须共享 checked-in golden/negative fixture；Go runner 重算结果与 generation script
不一致时拒绝 migration。

migration runner 在 dedicated `pgx` connection 上取得 session advisory lock。锁 key 是
`SHA-256("cloud-agents-platform:migrations:v1")` 前 8 bytes 的 signed big-endian int64：
`-1047838957622507638`。锁覆盖 ledger validation 与 migration 序列，但每个 DDL migration 使用独立、短事务；
外部网络调用不得位于事务中。crash 时 PostgreSQL 自动释放 session lock。

`000001` 只允许在 `cloud_agents` schema 不存在或为空且 ledger 不存在时创建 ledger；发现未知对象即拒绝，不能
adopt。已应用 migration 不修改；修复只能追加新 forward migration。

backfill 每个 batch 使用独立短事务和 deterministic boundary，提交 durable cursor/count/digest；失败可从上一
已提交 cursor 恢复。contract migration 在取得锁和执行 DDL 前必须通过第 8 节 preflight。

### 6.1 Durable coordination identity

P1-A2.3 在写 SQL 前固定以下 authority key：

- idempotency primary authority 是
  `(tenant_id, subject_digest, operation_name, idempotency_key)`。但在本表/mutation writer 落地前，P1-A1 contract
  follow-up 必须把 SubjectRef 与每类 idempotent request 的 canonical projection、RFC 8785 bytes、SHA-256、
  Unicode/number/unknown-field 处理和 golden/negative fixtures 写入 contract/generation lock。OIDC issuer/subject 按
  已配置 authority 的 exact string 比较，不 trim/lowercase/URL rewrite。Go row/struct JSON 不得反向定义 digest。
  contract 尚未冻结或 digest 不匹配时 P1-A2.3 保持 blocked。同 key 不同 request digest 必须在任何 side effect 前
  返回稳定 conflict；replay 只能保存 typed、redacted、无 secret 的 result/receipt 引用，必须有 expiry；
- outbox claim 必须同时绑定 `claim_holder_id + claim_incarnation + claim_token + claim_expires_at`。ACK/retry/DLQ
  更新必须匹配 message ID 与完整 claim tuple，进程重启或 holder ID 复用不能确认旧 claim；
- outbox event 分成两个互斥 class，避免同步 tenancy mutation 伪造 operation/generation：
  - `resource_change`：必须带 `tenant_id + event_id + aggregate_kind/id + resource_version`，固定 `generation = 0`、
    `operation_id IS NULL`、`aggregate_sequence = resource_version`；
  - `operation_effect`：必须带真实 `operation_id`、`generation > 0` 与正数 `aggregate_sequence`；
    两类都要求 `(tenant_id, event_id)` 唯一；operation_effect 还要求同 aggregate/generation 的 sequence 唯一。
    跨实例顺序以持久化 sequence/resource revision 为准，不依赖单进程 lane/hash；
- leader takeover 只使用数据库时间判定 expiry，并递增 monotonic fencing token。任何 leader-owned authority write
  必须在同一短 `pgx.Tx` 中锁定 leader row 并核对 token；
- Operation terminal Receipt identity/outcome immutable。所有 required Finalizer 完成前不得把
  `cleanup_phase` 写成 `complete`。

pairing URL/token、Provider/Broker credential、raw auth material 不能进入 idempotency replay、outbox、Receipt、
audit、log、trace 或 backup。

### 7. Persistence 分层

Control Plane module 内固定四层：

```text
services/control-plane/
  migrations/                  # SQL + strict manifest，schema authority
  internal/domain/             # 中立 domain state，不含 pgx row tag
  internal/store/postgres/     # pgx pool、transaction、query、row mapping
  internal/migration/          # manifest/ledger/runner/backfill/preflight
```

SQL schema 是数据库 authority；Go row、query 与 domain model 不反向生成 SQL。Control Plane/Worker 不互相 import
module；数据库 package 不进入 `sdk/go`。

### 8. Live-instance 与 contract preflight

P1-A2.4 必须持久化 instance ID、incarnation、service kind、binary version、supported schema range、writer epoch、
rollout generation、heartbeat/TTL 与 drain state。contract/destructive migration 在 DDL 前必须证明：

- PostgreSQL major、ledger checksum、target bundle/phase 与 restore evidence 匹配；
- 所有未过期实例和 rollback target 可读 target schema，当前 writer 可安全写入；
- 不存在 unknown、range 不交、stale-but-not-expired 或未 drain 的旧 writer；
- expired registration 只有在同 instance/incarnation/generation 的 durable receipt 同时证明 process terminated、
  generation fenced、endpoint revoked、credential revoked、claim released、leader released 后才能排除；
- irreversible/contract step 还需单独批准及匹配 release/schema digest 的 restore point/drill record。

registry 不可用或无法证明时拒绝 DDL。空 registry 只允许全新数据库的显式 bootstrap mode；已有 ledger 不得复用
该例外。P1 只做 local logical backup/restore 与 preflight contract；部署级 PITR/HA/failover 保留给 P4。

### 9. PostgreSQL matrix 与依赖

- 当前 P1 matrix 固定 minor 为 PostgreSQL `15.18`、`16.14`、`17.10`（依据
  [PostgreSQL version policy](https://www.postgresql.org/support/versioning/)）；执行 evidence 前再固定 official image 的
  platform-specific digest 和实际 `server_version`，禁止浮动 `postgres:15|16|17` 作为 closure 输入。
- Go driver target 为 [`github.com/jackc/pgx/v5 v5.10.0`](https://github.com/jackc/pgx/blob/v5.10.0/CHANGELOG.md)，
  只使用 native `pgx`/`pgxpool`；不引入 ORM、migration
  framework 或 testcontainers dependency。其默认依赖闭包在独立 review 通过前不得进入 `go.mod/go.sum`；任何
  安全版本提升/override 必须使用 normal direct requirement（发布禁止 `replace`）并获得自己的 review。
- dependency 进入 `go.mod/go.sum` 前必须完成独立 license/security/provenance review，并在
  `docs/plan/p1/dependency-reviews/` 保存可重放证据。
- 本地 matrix 使用 ephemeral、digest-pinned container；测试数据库名、volume 与 network 必须 task-scoped，
  teardown 后不得残留。接近 P1 closure 时才对固定 commit 在选定 Linux/amd64 主机重放一次。

## 被否决的方案

### 把 `platform_tenants` 或全部 role/binding 表做成无 RLS global table

否决。Tenant 是隔离根，不是绕过隔离的理由；只有 immutable built-in role catalog 是 global，RoleBinding 始终是
tenant-owned。

### 用 runtime table owner、superuser 或 `BYPASSRLS` 解决 bootstrap

否决。这会使任何 API/SQL 注入绕过租户边界。bootstrap 必须是单独 credential、单独连接池、显式 policy 与
audit path。

### 使用 UUID PK、GORM model 或旧 Synara migration 作为数据库 authority

否决。公共 contract 的 ID 是 opaque text；ORM/legacy schema 只能作 oracle，不能反向定义公共 SQL。

### 在一次 migration 中同时加入 P1 与 P2/P3 全部聚合

否决。它扩大 rollback/审查面，并会提前冻结尚未进入阶段的 Session/Turn/Lease writer。

## 结果与未关闭边界

该决策使 P1-A2 可以从稳定输入开始实现，并把 tenant isolation、global authority、migration 重放与 live-instance
preflight 变为可测试 contract。`G-DATA`、`G-AUTHORITY-P1` 与 `G-SECURITY-P1` 仍为 `IN PROGRESS`；只有完整
PostgreSQL 15–17、RLS/role negative、migration/recovery/N/N-1 evidence 和独立 reviewer closure record 才能改变
Gate 状态。
