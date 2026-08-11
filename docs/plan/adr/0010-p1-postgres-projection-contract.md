# ADR-0010：P1 PostgreSQL Authority/Catalog Projection Contract

- Status：Accepted
- Date：2026-08-11
- Decision owner：hxp0618
- Implementation executor：Codex
- Applies to：P1-A2.1a（authority、schema/default-ACL、snapshot）与 P1-A2.1b（relation/expression）
- Extends：[ADR-0008](0008-p1-postgres-data-kernel.md)、[ADR-0009](0009-p1-migration-bundle-runner.md)
- Amends：ADR-0009 §8 的 projector 实现拆分，并细化 verified authority input 为 release profile + deployment
  binding；保持外部验签/no-loose-digest 原则，不改变 schema ledger 或 Gate 语义
- Approval basis：P1 计划已批准；本 ADR 只把已批准的 P1-A2.1 收窄为可实现、可审计的 projection contract，**不扩大授权、阶段、发布或部署权限**

## 1. 背景与边界

ADR-0008 固定了 PostgreSQL 15–17、数据库角色、tenant context、migration lineage 与 RLS 边界；ADR-0009
又固定了 signed migration bundle、catalog contract、statement postcondition、session advisory lock 和短事务。
当前代码已经有 `AuthorityProjection`、`CatalogProjection`、typed relation/function/ACL 字段及 fail-closed
validator seam，但仍没有 PG15–17 的真实 catalog adapter、signed expected projection 或 expression normalizer。

本 ADR 冻结下一步实现所必须遵守的公共形状和观察边界：

1. authority 与 catalog 都必须由**签名 expected contract**驱动，并以完整 typed projection 比较；
2. `schema_absent | schema_present` 是唯一初始 schema 前置条件 union；
3. direct membership 与 `MEMBER`/`USAGE`/`SET` reachability（递归可达性）是不同事实，不能混成一张边表；
4. idle read snapshot 与 migration transaction 复用同一 projector/query 代码，但事务模式不同；
5. 每个 statement 的中间控制面状态和 digest 与最终 cumulative catalog projection 分开；
6. PG15/16/17 的 catalog 差异只在 adapter 层处理，输出同一个 version-neutral projection；
7. P1-A2.1a 只实现 schema/default ACL 与 authority 最小闭包；P1-A2.1b 再加入 relation、依赖对象和
   `cloud-agents-sql-expression/v1` AST；
8. 所有查询都有资源上限、稳定错误码和日志脱敏。

本 ADR 不声称 projector、签名、数据库 Gate、P1、Platform RC、发布、部署或生产升级已经完成。直到
签名 verifier、PG adapters、实库矩阵和独立 closure record 落地，所有现有 contract 继续保持
`publication_status = UNPUBLISHED_BOOTSTRAP_MUTABLE`、`runtime_introspection_status = NOT_IMPLEMENTED`，
生产 CLI 继续在读取数据库前 fail closed。

## 2. 非目标与 trust boundary

- 不改变 schema、migration ID、ledger digest、bootstrap role 或 RLS policy；
- 不实现 Session/Turn/Worker/Lease/Workspace 等 P2+ writer；
- 不把 projection 当成 SDK/wire resource；projection 只属于 Control Plane `internal` store 和 runner；
- 不让调用方注入 SQL、tenant context、canonical JSON 或 digest；
- 不以 `information_schema`、`pg_dump`、`pg_get_*` pretty text 或 OID 作为完整 authority；
- 不把数据库 superuser、`BYPASSRLS` 或仓外 provisioning 纳入 Cloud Agents workload authority。

Projection 证明的是：在一个明确的 PostgreSQL transaction snapshot 中，当前连接看见的 catalog/role/database
状态是否等于签名 expected contract。它不是对整个 cluster 的不可伪造证明。

## 3. Signed expected contract

### 3.1 Release profile、deployment binding 与 catalog subject

authority profile、deployment authority binding 与 cumulative catalog 是三个独立 subject，均使用 ADR-0009 的 strict JSON、RFC 8785 和
`sha256:<64 位小写 hex>` profile：

| subject                      | 绑定内容                                                                                                                                                                              | 使用位置                                                                                         |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `authority_profile_contract` | 数据库 locale/profile、固定 group role、完整字段/不变量、projection/binding schema；不含部署者注入的 LOGIN/database owner 名称                                                        | manifest `execution_policy.authority_contract`、signed Platform release                          |
| `authority_binding`          | exact database/session/owner/grantor identity，以及三个 phase 的完整 resolved `expected_projections`；引用 exact authority profile digest                                             | deployment trust root 签名的独立 subject、verified Platform trust decision                       |
| `catalog_contract`           | schema/default ACL、声明的 relation/function 及其完整子对象、denied object/dependency closure、statement descriptors、完整 final projection 与逐 statement signed expected transition | 每个 schema head 的 `catalog_contract` artifact、schema bundle、verified Platform trust decision |

签名覆盖外层 subject 的 exact bytes、subject kind、schema head/authority epoch、发行者 key id、有效期和撤销状态。
内部重新计算的 digest 只能证明 bytes 一致，不能替代 detached signature、trust root、expiry/revocation 或
expected subject identity。签名 verifier 未实现前，不得把仓内 JSON 当成 trusted expected projection。

`session_user`、database name/owner、membership grantor 等 deployment-specific identity 必须进入
`authority_binding.expected_projections`；它们不得由 environment variable、CLI loose string 或 projector 自己
“补齐”。同一公共 release 通过不同、可撤销的 signed authority binding 支持不同部署 identity；verified trust
decision 同时绑定 release profile digest 与 deployment binding digest，再解析出完整 expected projection。binding
变化不能重打或修改 schema bundle，也不能作为 unsigned local overlay 混入 runtime tar。

### 3.2 Authority contract 输入

manifest 内 authority profile contract 的固定 top-level 字段为：

```json
{
  "format_version": "cloud-agents-platform-authority-contract/v1",
  "contract_kind": "database_role_authority",
  "publication_status": "UNPUBLISHED_BOOTSTRAP_MUTABLE",
  "runtime_introspection_status": "NOT_IMPLEMENTED",
  "database": {},
  "group_roles": [],
  "required_projection_fields": [],
  "required_binding_fields": []
}
```

`database` 的字段固定为 `encoding`、`locale_provider`、`datcollate`、`datctype`、`icu_locale`、
`icu_rules`、`collation_version`。`group_roles` 只能包含 ADR-0008 的三个 role；任何额外 writer 或 role
必须先有新的 ADR。`required_projection_fields`/`required_binding_fields` 都是 closed list，不允许用 wildcard
或“只验证当前 caller”。deployment authority binding 的 exact shape 为：

```json
{
  "format_version": "cloud-agents-platform-authority-binding/v1",
  "authority_profile_digest": "sha256:<64-lowercase-hex>",
  "deployment_id": "<opaque normalized identifier>",
  "issued_at": "<RFC3339 UTC>",
  "expires_at": "<RFC3339 UTC>",
  "security_epoch": 1,
  "expected_projections": {
    "connected_session": {},
    "migration_role": {},
    "migration_transaction": {}
  }
}
```

三个 `expected_projections` 都必须是第 4.2 节对应 phase 的完整 `AuthorityProjection`，不得使用 sparse patch、
默认值或仅列 drift 字段；它们分别冻结连接后/`SET ROLE` 前、切换 migration role 后、migration transaction 内的
`session_user/current_user` 和其他 authority。projector 的实际结果必须与相同 phase 的 expected 值
byte-canonical、field-complete 地比较。binding 的 detached signature、
issuer/trust root、expiry、revocation、minimum security epoch 与 profile digest 都必须在连接数据库前验证；
binding 本身不能自签或把 loose digest 提升为 trust decision。

### 3.3 Catalog contract 输入

catalog contract 固定包含 `format_version`、`contract_kind`、`schema_head`、`publication_status`、
`runtime_introspection_status`、`source_descriptors`、`projection_model`、`declared_object_identities`、
`expected_projection`。每个 `source_descriptors[].statements[]` 还必须包含与 statement bytes/classification 同一
signed subject 绑定的以下唯一 closed shape；禁止由执行时 actual state 反向生成 expected 值：

```text
ExpectedStatementTransition {
  profile: "cloud-agents-platform-statement-transition/v1"
  catalog_before: CatalogStateDigestRef
  catalog_after: CatalogStateDigestRef
  authority_relation: "unchanged_relative_to_verified_binding"
  control_plane_delta: ObjectTransitionProjection[]
}

CatalogStateDigestRef {
  scope: ProjectionScope
  state_kind: "schema_absent" | "schema_present"
  digest: Digest
}

ObjectTransitionProjection {
  change_kind: "create" | "alter" | "grant" | "revoke"
  object: ObjectIdentityProjection
  grantee: string | null
}
```

`control_plane_delta` 按 object identity/change/grantee 的 UTF-8 bytes 排序，可为空但不可缺失；before/after ref
只携带 closed scope、state kind 与 digest，避免为每个 statement 重复内嵌完整 prefix projection而形成 O(N²)
catalog。生成器/reviewer evidence 保留完整 expected state；runtime 用同一 scope 投影 actual typed state、重算 digest
并比较。exact state digest 才是 authority，delta 只提供 closed explanation，不能扩大 classifier。authority 的 exact 值
来自可独立升级的 verified deployment binding，不能把某次 deployment authority digest 固化进 schema identity。
catalog contract 仍受 ADR-0009 的单文件 1 MiB 上限；generator/reviewer full-state evidence 不是 runtime member、
不能由 runner 当作第二 authority。
`source_descriptors` 绑定 migration SQL 的 exact statement offset、bytes digest、narrow classification 和
statement index；它不允许通过改变分类器而扩大 SQL 权限。

`expected_projection` 是该 schema head 的完整 final `CatalogProjection`。`projection_model` 是字段 allowlist。
`declared_object_identities` 的每一项都使用第 4.1 节 `ObjectIdentityProjection` tagged union，不能用裸字符串
混淆 relation/type/function/internal dependency；duplicate logical identity 或 unknown kind 直接拒绝。
声明以外的 relation、function、policy、trigger、ACL、dependency 或
internal object 都进入 `denied_objects`，而不是被静默忽略。`schema_head` 必须等于正在验证的 cumulative head。
`000001` 第一条 statement 的 before digest 对应第 4.4 节 `CatalogPreconditionProjection`，不是伪造的空
`CatalogProjection`；后续 before/after 都使用相同的 typed `CatalogStateProjection` digest domain。

## 4. Version-neutral projection structs

下面的字段名和语义是 contract，不要求 JSON 与 Go struct 的字段顺序相同；canonicalizer 在 digest 前固定
member order。所有数组按 normalized logical identity 的 UTF-8 bytes 排序；公共 Cloud Agents 自有 identity 继续
使用现有 ASCII grammar，PostgreSQL catalog identity 则保留数据库中的 exact UTF-8 bytes，不做 locale/NFC 猜测。
所有 map 在 canonical serialization 时
按 key 排序；所有 optional value 使用显式 `null`/缺失约定，不能在 adapter 间自行推断。

### 4.1 Common scalar types

```text
TypeIdentity      = { schema, name }
SQLIdentity       = { schema, name, arguments: TypeIdentity[] }
ACLProjection     = { grantor, grantee, privileges[], grantable[], origin }
ACLSetProjection  = { catalog_value: "null" | "explicit", entries: ACLProjection[] }
DatabaseRoleSettingProjection = { database, role, settings[] }
ReachabilityPrivilegeProjection = { privilege_kind: "member" | "usage" | "set", reachable, min_depth?, canonical_witness?, edge_count }
IndexTermProjection = { ordinal, term_kind: "column" | "expression", column?, expression?, opclass?, opclass_options[], collation?, order, nulls, exclusion_operator? }
FunctionArgumentProjection = { ordinal, name?, mode, type, default? }
DeniedObjectProjection = { object: ObjectIdentityProjection, owner?, dependency_kind?, depended_on?, reason_code }
ExpressionNode    = { kind, type?, identity?, value?, fields?, children[] }
```

`ObjectIdentityProjection` 是以下 closed tagged union，不是 `{kind, any}`：

```text
{ kind: "schema", name }
{ kind: "relation", identity: {schema, name} }
{ kind: "column", relation: {schema, name}, name }
{ kind: "index", identity: {schema, name}, relation: {schema, name} }
{ kind: "policy", relation: {schema, name}, name }
{ kind: "type", identity: TypeIdentity }
{ kind: "extension", name }
{ kind: "collation", identity: {schema, name} }
{ kind: "opclass", identity: {schema, name}, access_method }
{ kind: "function" | "operator", identity: SQLIdentity }
{ kind: "cast", source_type: TypeIdentity, target_type: TypeIdentity }
{ kind: "constraint", relation: {schema, name}, name }
{ kind: "trigger", relation: {schema, name}, name, owning_constraint? }
{ kind: "internal", semantic_kind, owning_object: ObjectIdentityProjection }
```

每个 variant 的 key set 都 closed；递归 internal owner 有 depth limit，不能嵌套另一个 `internal`。所有
`declared_object_identities`、dependency 两端和 denied object 都使用这一个 union。

`SQLIdentity` 的 `arguments` 是已解析的 schema/name/signature identity，不是 OID。`ACLProjection` 的 `grantor`
和 `grantee` 都来自 exact ACL expansion，不能因 effective privilege 相同而丢掉 provenance；
`origin` 只能是 `catalog_explicit`、`owner_implicit`、`public_default`、`default_acl_catalog`；
`default_acl_catalog` 只用于 `pg_default_acl` 行本身。对象当前 catalog 中由默认 ACL 与显式 `GRANT` 产生的相同
aclitem 无法区分，必须统一为 `catalog_explicit`，不得反推历史 provenance。`ACLSetProjection` 额外区分 catalog
原值为 `NULL` 与显式空/非空 ACL；`catalog_value = null` 时 entries 必须为空，`explicit` 时可为空或非空。
privileges/grantable 是去重后的 uppercase privilege name，按固定权限顺序输出。
`ExpressionNode` 使用
`cloud-agents-sql-expression/v1` typed AST：`kind`、resolved `type`/`identity`、typed logical `value`、closed
`fields` 和递归 `children`；未知 node kind、未知 field、OID、raw Datum、host-endian bytes 和 pretty SQL text
都拒绝。`fields` 不是任意扩展 map，具体 key 必须由该 expression adapter 的 versioned allowlist 声明。

ADR-0009 的普通 JSON 安全整数 profile 不足以表示 PostgreSQL 的 `connection_limit = -1`、`typmod = -1` 或
function `cost/rows`。projection numeric 使用以下机械 profile：

- signed integer：`0|-?[1-9][0-9]{0,18}`，禁止 `-0`，随后按字段验证 int16/int32/int64 range；
- exact `numeric`：`-?(0|[1-9][0-9]*)(\.[0-9]*[1-9])?`，另行拒绝 `-0`，`0.0` canonicalize 为 `0`，
  无 exponent、无 trailing fractional zero，
  最长 128 bytes；
- float4/float8：`cloud-agents-ryu-v1` shortest round-trip decimal；lowercase `e`、exponent 无 `+`/leading zero，
  禁止 `NaN`、`Infinity`、`-0`，最大 32 bytes。输入必须先按 PostgreSQL binary32/binary64 rounding，再运行
  同一固定 Ryu vectors；不能依赖 locale、`printf` 默认精度或 host architecture。

非负且不超过 `9007199254740991` 的 count/ordinal 可继续用 JSON integer。TS/Go/PG15–17 fixture 必须覆盖
Min/Max、0、fraction、trailing-zero、subnormal、rounding tie、exponent boundary，并证明 parse -> typed value ->
canonical string round-trip same-bits。
`reason_code` 同样是 versioned closed enum，不接受 adapter 自由文本。

### 4.2 AuthorityProjection

Authority projection 必须完整包含以下对象，不能用“只包含漂移字段”的 sparse projection：

```text
AuthorityProjection {
  phase: "connected_session" | "migration_role" | "migration_transaction"
  session_user: string
  current_user: string
  database_name: string
  database_owner: string
  database_encoding: string
  locale_provider: string
  datcollate: string
  datctype: string
  icu_locale: string | null
  icu_rules: string | null
  collation_version: string | null
  database_acl: ACLSetProjection
  roles: RoleProjection[]
  direct_memberships: DirectMembershipProjection[]
  membership_reachability: ReachabilityProjection[]
  database_role_settings: DatabaseRoleSettingProjection[]
  effective_create: map<string, bool>
  effective_temporary: map<string, bool>
}

RoleProjection {
  name: string
  login: bool
  inherit: bool
  superuser: bool
  create_role: bool
  create_db: bool
  replication: bool
  bypass_rls: bool
  connection_limit_int32_decimal: string
  valid_until: string | null
  config: string[]
}

DirectMembershipProjection {
  role: string
  member: string
  grantor: string
  admin_option: bool
  inherit_option: bool
  set_option: bool
}

ReachabilityProjection {
  role: string
  member: string
  privileges: ReachabilityPrivilegeProjection[]
}
```

`direct_memberships` 是 `pg_auth_members` 的 exact direct edge：一条 catalog edge 一行，保留 grantor、
`admin_option`、`inherit_option`、`set_option`。`membership_reachability` 是从 workload/login 到三个
Cloud Agents group 的递归闭包；`privileges` 必须按 member/usage/set 恰好三项排序，分别记录 PostgreSQL 的
`MEMBER`（仅关系可达）、`USAGE`（当前无需
`SET ROLE` 即可使用）和 `SET`（可沿 set-enabled chain 切换）语义，不能用一个 `reachable` 布尔值代替。
它不覆盖 direct array，也不能把 indirect edge 当成 direct authority。每种 privilege 只保留一个按 UTF-8
logical identity 排序选出的 canonical witness path 和完整 traversed edge count，避免枚举指数级全部路径；witness
只用于 deterministic explanation，不能用来授予额外权限。存在 cycle、超过深度/边数上限或无法确定
闭包时，projector 返回稳定错误而不是截断后继续。

`roles` 至少覆盖 authority contract 列出的 group role、session user、database owner、所有 direct/reachability
membership 相关 principal；未列出的 role 不得被隐式过滤。`database_role_settings` 读取
`pg_db_role_setting` 的 exact database/role/settings row，不允许用拼接 map key 抹去 database-vs-role scope；
`effective_create`/`effective_temporary` 是按 database ACL、
role membership 和 role attributes 计算出的 observed effective result，不等同于“某条 ACL 存在”。
role/database setting name 使用 closed safe allowlist；password、token、DSN、extension 自定义 secret 或未知 GUC
直接拒绝，不能为了“完整投影”把秘密写进 signed contract/evidence。初始 profile 预期 group/workload role settings 为空。

### 4.3 CatalogProjection

```text
CatalogProjection {
  schema_head: string
  body: CatalogProjectionBody
}

CatalogProjectionBody {
  schema: SchemaProjection
  default_acl: DefaultACLProjection[]
  relations: RelationProjection[]
  functions: FunctionProjection[]
  dependencies: DependencyProjection[]
  object_count: uint32
  declared_objects: ObjectIdentityProjection[]
  denied_objects: DeniedObjectProjection[]
}

SchemaProjection {
  name: string
  owner: string
  explicit_acl: ACLSetProjection
  effective_acl: ACLProjection[]
  comment: string | null
  security_labels: SecurityLabel[]
}

SecurityLabel {
  provider: string
  label: string
}

DefaultACLProjection {
  owner: string
  schema: string | null
  object_kind: string
  acl: ACLSetProjection
}

DependencyProjection {
  depender: ObjectIdentityProjection
  depended_on: ObjectIdentityProjection
  dependency_kind: string
}
```

`explicit_acl` 保留 `nspacl` 的 `NULL|explicit` 状态及 exact grantor/grantee provenance；`effective_acl` 是加入 owner/PUBLIC/default
语义后、按同一 principal closure 计算的 observed privilege。两者不能互相替代，因而不再在
`CatalogProjection` 顶层重复一个含义不明的 `schema_acl`。

`RelationProjection` 的完整字段为 `identity`、`relkind`、`persistence`、`access_method`、`owner`、`explicit_acl`、
`reloptions`、`replica_identity`、`rls_enabled`、`rls_forced`、`columns`、`constraints`、`indexes`、`policies`、
`triggers`。子项字段固定如下：

- `ColumnProjection`：`attnum`、`name`、`type`、`typmod`、`collation`、`nullable`、`identity`、`generated`、
  `default`、`storage`、`compression`、`explicit_acl: ACLSetProjection`；
- `ConstraintProjection`：`name`、`type`、`columns`、`referenced_relation`、`referenced_columns`、`match`、
  `update`、`delete`、`deferrable`、`deferred`、`validated`、`expression`；
- `IndexProjection`：`name`、`access_method`、`terms: IndexTermProjection[]`、`includes`、`unique`、`primary`、
  `valid`、`ready`、`live`、`immediate`、`clustered`、`check_xmin`、`nulls_not_distinct`、`exclusion`、
  `replica_identity`、`predicate`；每个 key/expression 的
  opclass/collation/order/nulls 必须与同一个 ordinal 同行，禁止用多个平行数组造成错位；
- `PolicyProjection`：`name`、`permissive`、`command`、`roles`、`using`、`with_check`；
- `TriggerProjection`：`identity`、`owning_relation`、`owning_constraint`、`function`、`enabled`、`type`、
  `columns`、`arguments`、`when`、`internal`；internal trigger 必须通过 owning constraint/relation 归一，不能
  用生成名或 OID；
- `FunctionProjection`：`identity`、`kind`、`language`、`arguments: FunctionArgumentProjection[]`、
  `variadic_type`、`returns`、`return_set`、`owner`、`explicit_acl: ACLSetProjection`、
  `security_definer`、`volatility`、`parallel`、`leakproof`、`strict`、`config`、`cost`、`rows`、
  `prosrc_sha256`、`probin`；default expression 必须绑定到对应 argument，不能只记录 default count。

OID-derived names 的 internal composite/array/TOAST relation 和 constraint trigger 只以 owning declared
relation/constraint + semantic kind 归一；未绑定 declared constraint 的 internal trigger、以及未在 contract 中
声明的 sequence/view/materialized view/foreign table/partition/type/operator/cast/extension 都进入
`denied_objects` 并使 validation fail closed。每个 denied row 必须保留 object kind、logical identity 与可解析的
owner/dependency，而不是把不同 kind 压成裸 `SQLIdentity`。对象是否允许由 signed catalog contract 决定，不由
adapter 猜测。

### 4.4 `schema_absent | schema_present` union

初始 predecessor 和 runtime catalog 的 schema state 只能是以下 discriminated union；禁止 `empty_schema`、
`null schema`、自由形状 map 或以 `object_count == 0` 猜测缺失：

```json
{
  "state": "schema_absent",
  "scope": {
    "scope_kind": "predecessor",
    "schema_head": null,
    "migration_id": "000001",
    "through_statement_index": null,
    "declared_objects": []
  },
  "schema": "cloud_agents"
}
```

或：

```json
{
  "state": "schema_present",
  "scope": {
    "scope_kind": "predecessor",
    "schema_head": null,
    "migration_id": "000001",
    "through_statement_index": null,
    "declared_objects": []
  },
  "body": {
    "schema": {
      "name": "cloud_agents",
      "owner": "cloud_agents_migration_owner",
      "explicit_acl": { "catalog_value": "null", "entries": [] },
      "effective_acl": [
        {
          "grantor": "cloud_agents_migration_owner",
          "grantee": "cloud_agents_migration_owner",
          "privileges": ["CREATE", "USAGE"],
          "grantable": ["CREATE", "USAGE"],
          "origin": "owner_implicit"
        }
      ],
      "comment": null,
      "security_labels": []
    },
    "default_acl": [],
    "relations": [],
    "functions": [],
    "dependencies": [],
    "object_count": 0,
    "declared_objects": [],
    "denied_objects": []
  }
}
```

`CatalogStateProjection` 正是上述 union；`scope.scope_kind` 只能是 `predecessor|statement_prefix|final`，其他字段
按 kind 使用 exact null/value。`statement_prefix` 必须给出 migration ID、0-based through-statement index 与截至
该边界的 typed declared object closure；`final` 必须给 schema head，且其 present `body` byte-equal
`expected_projection.body`、scope schema head 等于 `expected_projection.schema_head`。scope 不匹配时即使
projection digest 碰巧相同也拒绝。catalog state digest 的 domain 固定为
`cloud-agents-platform-catalog-state/v1`，输入为完整 `{state,scope,schema|body}` union。

`schema_present` 即使 object count 为零也必须读取 schema owner/effective ACL/default ACL；只有 namespace owner
dependency 允许存在。PUBLIC/未知 grantee、extension membership 或其他 object/dependency 会使该 branch 不匹配。
现有 ADR-0009 artifact 中的 `empty_schema` 必须在下一次 manifest/schema-bundle regeneration 中映射为
上述 `schema_present` exact branch；在兼容映射完成前，runner 继续拒绝混用两种 shape，不能静默接受两套语义。

## 5. Snapshot API 与事务复用

Projector 只依赖下列最小接口，不暴露 raw connection、pool、role switch 或任意 SQL 给 caller：

```go
type ProjectionSnapshot interface {
    catalogQueryer // internal, fixed query IDs only; no caller-supplied SQL
    Metadata() SnapshotMetadata
}

type IdleProjectionSnapshot interface {
    ProjectionSnapshot
    RollbackAndRelease(context.Context) error
}

type SnapshotMode string
const (
    IdleReadSnapshot SnapshotMode = "idle_read_repeatable_read_only"
    MigrationSnapshot SnapshotMode = "migration_serializable_read_write"
)

type AuthorityPhase string
const (
    ConnectedSession AuthorityPhase = "connected_session"
    MigrationRole AuthorityPhase = "migration_role"
    MigrationTransaction AuthorityPhase = "migration_transaction"
)

type Projector interface {
    ProjectAuthority(context.Context, ProjectionSnapshot, VerifiedAuthorityContract, AuthorityPhase) (ProjectionResult[AuthorityProjection], error)
    ProjectCatalog(context.Context, ProjectionSnapshot, VerifiedCatalogContract, ProjectionScope) (ProjectionResult[CatalogProjection], error)
    ProjectPrecondition(context.Context, ProjectionSnapshot, VerifiedSchemaBundleScope, CatalogPrecondition) (ProjectionResult[CatalogStateProjection], error)
    ProjectTransitionState(context.Context, ProjectionSnapshot, VerifiedCatalogContract, ProjectionScope) (ProjectionResult[CatalogStateProjection], error)
}
```

`CatalogStateProjection` 是第 4.4 节 union 的 typed Go 表达；`schema_absent` 不能伪装成零值
`CatalogProjection`。`ProjectionResult[T]` 固定返回 typed projection、canonical digest 和 bounded metadata；
`VerifiedAuthorityContract`/`VerifiedCatalogContract` 只能由 trust verifier 构造；初始 inline predecessor 则只
能由已验证 schema bundle 构造 `VerifiedSchemaBundleScope`，不能冒充一个不存在的 catalog contract。三者都包含
exact subject/bundle digest、declared object closure、phase/statement scope 和 expiry/epoch 决策，不能由 caller
loose JSON 构造。`ProjectionScope` 是 closed
`{scope_kind, schema_head, migration_id, through_statement_index, declared_objects}`，不得让 caller
扩大 verified catalog contract 的 object closure。
snapshot 自身不提供 projection digest，digest 只能由 canonical typed projection 计算。
idle snapshot 的 owner 负责 rollback/release；migration snapshot 的生命周期由 runner transaction owner 独占，
projector 无权 close/commit/rollback 它。

### 5.1 Idle read snapshot

`BeginIdleReadSnapshot` 只能在 connection idle 时执行：`BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY
NOT DEFERRABLE`，立即 read back `transaction_isolation`、`transaction_read_only`、`transaction_deferrable` 和
`TxStatus = 'T'`，再运行 authority/catalog queries。所有查询必须在同一 snapshot；禁止 session-level GUC、
role switch、nested transaction 或跨连接 join。结束时 rollback，确认 `TxStatus = 'I'` 后才把 connection 交还
pool；状态不明、rollback 失败或 transaction 外 GUC 未清除则 hijack + close。

### 5.2 Migration snapshot reuse

runner 已经打开的每条 migration 短事务使用 `SERIALIZABLE READ WRITE`。projector 接收该 transaction 的
`MigrationSnapshot` 视图并复用完全相同的 query/normalizer；它**不得再次 BEGIN、改变 isolation、切 role 或
创建 nested savepoint**。statement 前、statement 后和 ledger insert 前的 authority/catalog query 必须看见
同一个 migration transaction 的中间状态。最终 cumulative catalog 只有在全部 statement 完成后验证；中间状态
不能拿 final contract 误判为漂移。

snapshot metadata 必须记录 `postgres_major`、database identity、session/current user、mode、statement/migration
identity；projection digest 只出现在 `ProjectionResult`。projector 禁止使用 volatile `now()`、随机顺序、
backend PID 或 OID 作为 digest 输入。

## 6. Statement intermediate state

最终 `CatalogProjection` 不足以保护多 statement migration 的 transaction/authority 边界。每个 statement 都必须
产生以下 typed record：

signed `expected_transition` 携带 `catalog_before`/`catalog_after` 的 closed scope、state kind 与 digest；完整 expected
partial projection 留在 generator/reviewer evidence，避免 runtime catalog O(N²) 膨胀。runtime projector 按该
`ProjectionScope` 生成 actual typed state并重算 digest。scope 精确绑定 migration ID、through-statement index 与截至
该边界已声明的 object closure；它既不能提前要求后续 statement 才创建的对象，也不能忽略当前 prefix 已创建的
对象。首条 before 可为 `schema_absent|schema_present` predecessor union，final statement after 的 body 必须
byte-equal cumulative `expected_projection.body`。在 1b relation/expression projector 完成前，这一 full-prefix check
保持 fail closed，1a 不得只凭 schema/default ACL 把 runtime introspection 改成 IMPLEMENTED。

```text
StatementIntermediateState {
  schema_bundle_digest: Digest
  catalog_contract_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
  migration_id: string
  attempt_index: uint32
  statement_index: uint32
  statement_sha256: Digest
  previous_attempt_terminal_digest: Digest | null
  previous_intermediate_state_digest: Digest | null
  control_plane_states: ControlPlaneStates
  authority_before_digest: Digest
  authority_after_digest: Digest
  catalog_before_digest: Digest
  catalog_after_digest: Digest
  intermediate_state_digest: Digest
}

ControlPlaneStates {
  tx_status: "T"
  session_user: string
  current_user: string
  migration_role: string
  advisory_lock: { domain: string, key_int64_decimal: string, held: bool }
  verified_authority_decision_digest: Digest
  schema_owner: string
  schema_explicit_acl_digest: Digest
  schema_effective_acl_digest: Digest
  default_acl_digest: Digest
  expected_transition_digest: Digest
}
```

`control_plane_states` 是 closed object；未知 key、`tx_status != T`、`current_user != cloud_agents_migration_owner`、
lock 未持有或 expected transition digest 与 signed descriptor/classifier 不一致均 fail closed。所有 catalog-mutating
statement（包括 `DO`、CREATE、ALTER、GRANT、REVOKE）都只能执行 signed descriptor 声明的 exact transition；
不应改变 authority/default ACL 的 statement，before/after digest 必须相等。实际 catalog before/after state digest
必须与 signed `expected_transition` 比较；authority
before/after 必须分别等于相同 phase 的 verified deployment binding expected projection，且符合
`unchanged_relative_to_verified_binding`。不能把运行时 actual 反写成 expected。`intermediate_state_digest` 为：

`expected_transition_digest = SHA-256(RFC8785(<exact ExpectedStatementTransition>))`；它必须由 catalog contract
decoder 重算并与 `control_plane_states` 比较，不能由 projector 自报。

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-intermediate-state/v1",
  "schema_bundle_digest": ..., "catalog_contract_digest": ...,
  "authority_profile_digest": ..., "authority_binding_digest": ...,
  "migration_id": ..., "attempt_index": ..., "statement_index": ..., "statement_sha256": ...,
  "previous_attempt_terminal_digest": ...,
  "previous_intermediate_state_digest": ...,
  "control_plane_states": ...,
  "authority_before_digest": ..., "authority_after_digest": ...,
  "catalog_before_digest": ..., "catalog_after_digest": ...
}))
```

`attempt_index` 从 1 开始。每个 transaction attempt 的第一条 statement 都把
`previous_intermediate_state_digest = null`，后续引用同一 attempt 前一条 actual digest；retry 不把已 rollback
attempt 的 catalog state 当作新 predecessor。每个 attempt 结束时另生成 closed `AttemptTerminalState`，固定
`attempt_index`、最后 intermediate digest、stable error/reconcile result 与 terminal digest：

```text
AttemptTerminalState {
  schema_bundle_digest: Digest
  catalog_contract_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
  migration_id: string
  attempt_index: uint32
  previous_attempt_terminal_digest: Digest | null
  last_intermediate_state_digest: Digest | null
  outcome: "committed" | "aborted_retryable" | "aborted_terminal" |
           "ambiguous_reconciled_committed" | "ambiguous_reconciled_pending" | "ambiguous_divergent"
  stable_error_code: string | null
  reconcile_result: "not_run" | "exact_committed" | "exact_pending" | "divergent"
  terminal_digest: Digest
}
```

`terminal_digest` 使用 domain `cloud-agents-platform-attempt-terminal/v1` 对移除自身字段后的完整 object 做
RFC8785+SHA-256；statement 尚未产生 intermediate record 就失败时 `last_intermediate_state_digest = null`。
合法组合也是 closed：`committed => stable_error_code=null,reconcile_result=not_run`；
`aborted_retryable|aborted_terminal => stable_error_code!=null,reconcile_result=not_run`；
`ambiguous_reconciled_committed => stable_error_code!=null,reconcile_result=exact_committed`；
`ambiguous_reconciled_pending => stable_error_code!=null,reconcile_result=exact_pending`；
`ambiguous_divergent => stable_error_code!=null,reconcile_result=divergent`。其他组合、committed 但缺 final statement
digest、或 retryable outcome 超过 manifest `max_attempts` 都拒绝。
下一 attempt 的 `previous_attempt_terminal_digest` 必须引用它。
首个 attempt 为 null。这样审计链跨 attempt 连续，而 catalog state chain 在每个 rollback 边界重新开始。

该 digest 不是 ledger 的 schema identity 字段，也不替代最终 catalog contract；但 signed
`expected_transition` 属于 catalog artifact，因此必须经 artifact SHA 和 schema bundle**传递绑定**。authority
profile/binding 继续是可独立升级的 trust subject，只以 digest 进入 runtime evidence，不反向成为 schema identity。
运行时 actual digest 是 statement evidence 和 replay input，不得改变已签名 transition。
每个 statement 的 raw SQL bytes、offset、narrow classification、intermediate digest 和 stable error 都必须
进入本地 evidence/provenance，不能写入 tenant runtime payload。

## 7. P1-A2.1a / P1-A2.1b 拆分

| slice                         | 必须实现                                                                                                                                                                                                                                                                                                                                                              | 明确不实现                                                                                                                   |
| ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| **1a：authority + namespace** | 三 phase signed expected AuthorityProjection；PG15–17 role/database adapter；direct membership 与 `MEMBER`/`USAGE`/`SET` reachability；role settings、database owner/ACL/effective CREATE/TEMP；snapshot API；`schema_absent/schema_present`；schema owner/explicit+effective ACL/default ACL；bounds、errors、redaction；statement control-plane/intermediate digest | relation columns/constraints/indexes/policies/triggers 的完整 projection；expression AST；任意新 global writer；Gate closure |
| **1b：relation + expression** | signed cumulative CatalogProjection；relation/function 全字段；column/constraint/index/policy/trigger；internal object normalization；`cloud-agents-sql-expression/v1`；dependency closure；PG15–17 same-bits fixtures                                                                                                                                                | P1-A2.2 membership/RBAC writer、P1-A2.3 coordination、P2+ runtime                                                            |

1a 必须能够验证一个只有 `cloud_agents` schema 和 default ACL 的 fresh database；1b 才能验证 `000001/000002`
累积 catalog。1a/1b 的 source、fixture、adapter 和 runner binary 仍由 `runner_release_digest`/provenance 绑定，
不得把 helper 或 dependency 修复伪装成 schema migration。

## 8. PG15/16/17 adapter contract

每个 major 由独立 adapter 实现同一个 projector interface，并先做 capability probe；查询使用 version-specific
SQL 或 `to_jsonb` field mapping，不能把 PG16-only column 写进 PG15 parser。adapter 必须输出同一
version-neutral fields，差异保留在 evidence 的 `adapter_profile`，不进入业务 projection digest。

| surface                         | PG15                                                                                                                              | PG16                                                                                  | PG17                                                            | 统一规则                                                                                                       |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `pg_auth_members` grant options | `inherit_option`/`set_option` 不可读；按 PostgreSQL 15 legacy edge semantics 映射为 `true`，并记录 adapter capability             | 读取 `admin_option`、`inherit_option`、`set_option` 原值                              | 读取同一语义；若 future catalog 增字段，closed query 不自动吸收 | direct edge 三个 option 都进入 projection；synthetic legacy 值必须由 exact-major adapter 产生，caller 不得传入 |
| membership reachability         | `MEMBER` 按 direct/indirect membership；PG15 `SET = MEMBER`，与 `INHERIT` 无关；`USAGE` 才按路径上的 role-level `rolinherit` 计算 | 使用 direct edge options，并分别核对 `pg_has_role(..., 'MEMBER')`、`'USAGE'`、`'SET'` | 同 PG16，未知语义 fail closed                                   | 输出三个独立 privilege record；任何一个都不能从另一个推断或合并                                                |
| `pg_database` locale/collation  | 按可用字段读取 encoding、provider、C locale、ICU/collation nullable fields                                                        | 同一 version-neutral mapping                                                          | 同一 mapping，新增/变更字段必须 capability review               | 只接受 ADR-0009 的 UTF8/libc/C/null profile；缺字段或 alias 不得猜测                                           |
| role/database settings          | 读取 `pg_db_role_setting` 与 role attributes 的 PG15 catalog shape                                                                | 对新增 catalog field 使用 explicit allowlist                                          | 同一 allowlist；未知 field fail closed                          | `rolconfig`、database ACL、owner、effective CREATE/TEMP 必须完整验证                                           |
| relation/function/expression    | `pg_node_tree`/deparse 由 PG15 adapter 解析                                                                                       | PG16 adapter 解析同一 AST                                                             | PG17 adapter 解析同一 AST                                       | OID、raw node string、pretty text 不进入 digest；未知 node 返回 stable unsupported                             |

上述表是 adapter 行为合同，不是“任意版本都兼容”的声明。支持的 exact patch、image digest、locale 与 matrix
证据必须在 P1 closure 前另行固定；major range 不能代替 patch support。

## 9. Authority invariant 与 superuser 假设

每次连接后、取得 advisory lock 后、每个 statement 后和 ledger insert 前，都必须验证以下 invariant：

1. `session_user` 是独立 workload LOGIN，不属于任一 Cloud Agents group 以外的角色，不是 superuser、
   `BYPASSRLS`、`CREATEROLE` 或 database owner；
2. migration LOGIN 是 `cloud_agents_migration_owner` 的 direct member、`NOINHERIT`，只能显式 `SET ROLE`；
   runtime/bootstrap 不得与 migration authority 有 direct/indirect overlap；
3. 三个 group role 的 attributes、incoming membership、grantor、admin/inherit/set option 与 ADR-0008 完全一致；
4. database owner、database ACL、role settings、effective CREATE/TEMP、schema owner/effective ACL 与 signed
   expected authority 一致；
5. current user、transaction status、search path、timeouts、advisory lock 和 database identity 未漂移；
6. 任意未知 role/object/grantee、cycle、unbounded closure 或 projection query error 都拒绝，不以 best effort 继续。

本 invariant 有一个必须显式记录的 trust 假设：**不受信任的 cluster superuser 可以绕过 RLS、直接修改 role/
membership/ACL/catalog，甚至在 projector snapshot 外提交变化；这种变化可能在一个已建立的 MVCC snapshot 中不可见，
也不会因为 application advisory lock 而被阻止。** 因此 MVCC authority projection 不能发现或证明“没有 superuser
绕过协议”。部署 authority 必须由仓外 trust root、superuser provenance 和受控 provisioning 流程保证；若 caller
本身是 superuser/BYPASSRLS、authority provenance 缺失，runner 必须拒绝，但不能宣称 projector 能检测所有并发
superuser 攻击。该限制是边界条件，不是放宽 runtime role 的理由。

## 10. Query bounds、stable errors 与 redaction

所有 projector query 都在 bounded context 下运行，并在进入数据库前固定 limits：最大 result rows、单行/总 bytes、
递归深度、membership edges、catalog objects、expression nodes、ACL entries 和 statement timeout。递归 CTE 必须
有 cycle detection、depth/edge guard 和 deterministic order；超限即 rollback/fail closed。禁止把整个 `pg_catalog`
或 `information_schema` 扫描后再过滤。

stable error 至少包含以下 closed codes，并提供 `phase`、`path`、`postgres_major`、`retryable`；不得把数据库原始
message 当作对外 API：

| code                                        | 语义                                                | retryable        |
| ------------------------------------------- | --------------------------------------------------- | ---------------- |
| `MIGRATION_PROJECTION_UNSUPPORTED_MAJOR`    | adapter 未声明该 major/capability                   | no               |
| `MIGRATION_PROJECTION_CATALOG_QUERY_FAILED` | bounded query/scan/scan type 失败                   | 仅明确 transient |
| `MIGRATION_PROJECTION_LIMIT_EXCEEDED`       | rows/bytes/depth/nodes 超限                         | no               |
| `MIGRATION_PROJECTION_UNKNOWN_OBJECT`       | 未声明 object、ACL、node 或 dependency              | no               |
| `MIGRATION_PROJECTION_INVALID_EXPRESSION`   | expression AST 无法安全归一                         | no               |
| `MIGRATION_AUTHORITY_DRIFT`                 | authority/control-plane projection 不等于 expected  | no               |
| `MIGRATION_INTERMEDIATE_STATE_MISMATCH`     | statement 状态/digest 与 signed descriptor 不符     | no               |
| `MIGRATION_PROJECTION_SNAPSHOT_INVALID`     | isolation/read-only/TxStatus/snapshot 违反 contract | no               |

日志和 evidence 必须 redact DSN、password、token、SQL literal、expression value 中可能的 secret、完整 catalog
raw row 和未 allowlist 的 role/ACL payload。对外只保留 stable code、safe path、major、digest、statement index、
object kind 和 bounded counts；底层 error 作为本地审计字段保存前也要做 allowlist/length bound。

## 11. Conformance matrix（计划，不是 Gate closure）

实现完成后，以下矩阵必须以固定 patch/image digest 重放；当前仅是验收设计：

### 11.1 版本 × 实例 × snapshot

| 维度           | 组合                                                                   | 最低证明                                                                           |
| -------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| PostgreSQL     | 15、16、17 各一个 exact patch                                          | adapter capability、authority 1a、schema/default ACL、stable errors                |
| fresh instance | 两个独立 fresh database（A/B）× 每个 major                             | 同样输入得到相同 version-neutral projection/digest；无 instance-local OID/PID 泄漏 |
| snapshot       | idle RR/RO/`transaction_deferrable=false` 与 migration SERIALIZABLE/RW | 同一 projector query code；idle 不污染 pool；migration 不 nested BEGIN             |
| schema state   | absent、present-empty、present-drift、present-unknown-object           | union 精确匹配；所有 drift fail closed                                             |

### 11.2 Fault matrix

必须至少覆盖：

- role attribute、database owner/ACL、default ACL、direct membership、grantor、admin/inherit/set option 漂移；
- reachability cycle、深度/边上限、未知 role、`MEMBER`/`USAGE`/`SET` 漂移、重叠 Cloud Agents group authority；
- schema owner/ACL、未知 relation/function/extension、internal trigger/OID normalization 失败；
- expression unknown node/type/operator、invalid UTF-8、deparse/scan type 不匹配；
- query timeout、context cancel、rows/bytes limit、connection drop、rollback failure、ambiguous commit；
- advisory lock 丢失、`TxStatus` 非 `T`/`I`、current user 漂移、session GUC 未清除；
- 两实例并发 runner、statement 后 drift、ledger insert 前 drift；
- 受控 fixture 中 superuser 在 snapshot 外修改 authority：结果必须记录为“MVCC trust assumption”，不能误报为
  projector 已检测或安全通过。

每个 case 记录 input artifact digest、database image/patch、adapter profile、snapshot mode、expected stable error
或 projection digest、是否 rollback、是否销毁连接。通过测试不等于 Gate closure；只有独立 reviewer 签署 immutable
closure record 才能更新 `docs/plan/cloud-agents-platform/06-status-tracker.md`。

## 12. Alternatives considered

### A. 只比较最终 `pg_dump`/pretty SQL

拒绝。它丢失 transaction 中间态、grant options、effective privilege、OID-independent identity 和 version-neutral
expression semantics，无法保护 statement/ledger commit boundary。

### B. 只查 `information_schema`

拒绝。它不完整暴露 role/membership/default ACL/RLS/internal object/function security 属性，不能满足 authority
contract。

### C. 一张 membership 表同时表示 direct 与 recursive

拒绝。会把 indirect reachability 误当成授予边，无法保留 grantor 和 PG16 per-grant options，也无法解释 cycle。

### D. 所有 projector 都自行开启一个 `SERIALIZABLE` transaction

拒绝。会破坏 runner 的 statement 中间态和单一 commit boundary；idle read 与 migration transaction 必须共享
query/normalizer，而不是共享错误的事务生命周期。

### E. 相信 application advisory lock 能防 superuser drift

拒绝。superuser 可以绕过 lock/RLS/role protocol；必须把该事实写进 authority assumption，并由部署 trust root
承担不可观测的 cluster boundary。

### F. 用当前仓内 mutable JSON 作为 expected projection

拒绝。没有 detached signature、expiry/revocation 和 external trust anchor，内部自洽不等于授权真实性；在签名和
introspection 未实现前继续 `NOT_IMPLEMENTED`/`UNPUBLISHED`。

## 13. 精确下一切片与完成条件

下一切片只允许按以下顺序推进，不得跨入 P1-A2.2 或生产部署：

1. **A2.1a-impl-1：contract/fixture**：把本 ADR 的 union、AuthorityProjection、schema/default ACL 和
   `ControlPlaneStates` 固化为 strict fixture/schema；补齐 `empty_schema -> schema_present` 的 manifest
   regeneration 计划，保留 source SHA、tree/digest provenance。
2. **A2.1a-impl-2：PG adapters**：实现 PG15/16/17 capability probes、authority projector、schema/default ACL
   projector、idle/migration snapshot adapter；所有 unknown/missing field fail closed。
3. **A2.1a-impl-3：runner wiring**：在 statement 前/后和 ledger insert 前记录
   `intermediate_state_digest/control_plane_states`，验证 lock/current user/TxStatus，并补齐 redacted stable
   errors；signed verifier 仍为 fail closed。
4. **A2.1b-impl-1：catalog**：实现 relation/function/child object projection、internal dependency closure 和
   denied object set；不改变 migration SQL。
5. **A2.1b-impl-2：expression**：实现 PG15/16/17 `pg_node_tree`/deparse adapter 与
   `cloud-agents-sql-expression/v1` normalizer，先以 fixture same-bits 验证，再接 runner。
6. **A2.1b-impl-3：matrix/review**：执行三版本×双实例×两 snapshot mode 和 fault matrix；补齐 signed expected
   subject、dependency/provenance、SBOM/notice、reviewer closure record。完成前保持生产 CLI、Gate 和 release
   状态不变。

实现提交必须只触碰对应切片的源码、fixture、ADR/README 和 evidence；不得修改 main、合并宿主分支、发布公开
npm/Go channel、写生产数据库或把 `NOT_IMPLEMENTED` 改成 `IMPLEMENTED` 以制造进度。

## 14. 维护索引

- [ADR-0008：P1 PostgreSQL Data Kernel](0008-p1-postgres-data-kernel.md)
- [ADR-0009：P1 Migration Bundle、Runner 与 Trust Anchor](0009-p1-migration-bundle-runner.md)
- [P1 execution README](../p1/README.md)
- [typed projector seam](../../../services/control-plane/internal/migration/contracts.go)
- [catalog projection model](../../../services/control-plane/migrations/catalog/schema-000001.json)
- [authority contract placeholder](../../../services/control-plane/migrations/catalog/authority-v1.json)
- PostgreSQL official catalog semantics：[PG15 system information](https://www.postgresql.org/docs/15/functions-info.html)、[PG16 role membership](https://www.postgresql.org/docs/16/role-membership.html)、[PG17 `pg_auth_members`](https://www.postgresql.org/docs/17/catalog-pg-auth-members.html)、[PG17 `pg_attribute`](https://www.postgresql.org/docs/17/catalog-pg-attribute.html)、[PG17 `pg_index`](https://www.postgresql.org/docs/17/catalog-pg-index.html)、[PG17 `pg_proc`](https://www.postgresql.org/docs/17/catalog-pg-proc.html)
