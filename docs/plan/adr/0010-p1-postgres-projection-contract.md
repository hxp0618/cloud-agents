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
ReachabilityPrivilegeProjection = { privilege_kind: "member" | "usage" | "set", reachable, min_depth: uint32 | null, canonical_witness: string[] | null, edge_count: uint32 }
IndexTermProjection = { ordinal, term_kind: "column" | "expression", column?, expression?, opclass?, opclass_options[], collation?, order, nulls, exclusion_operator? }
FunctionArgumentProjection = { ordinal, name?, mode, type, default? }
DeniedObjectProjection = { object: ObjectIdentityProjection, owner?, dependency_kind?, depended_on?, reason_code }
ExpressionNode    = { kind, type?, identity?, value?, fields?, children[] }
```

该 projection 的两个 branch 是 closed：

```text
{ privilege_kind, reachable: true,  min_depth: uint32, canonical_witness: string[], edge_count }
{ privilege_kind, reachable: false, min_depth: null,   canonical_witness: null,    edge_count }
```

不得用缺失字段、空数组或 `0` 代替上述 nullable distinction。

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
logical identity 排序选出的 canonical witness path；`edge_count` 定义为完整
`AuthorityProjection.direct_memberships` projection 的 closure 总行数（即该数组的完整行数），并且每一个 reachability record 重复相同的总数，
不是本次 traversal 实际访问的边数。`reachable=true` 时 `min_depth` 与 `canonical_witness` 必须非 null；
`reachable=false` 时二者必须同时为 null。witness 只用于 deterministic explanation，不能用来授予额外权限。
存在 cycle、超过深度/边数上限或无法确定闭包时，projector 返回稳定错误而不是截断后继续。

canonical witness 的选择算法是 closed 且不允许 caller 影响：先从 `member` 出发沿 direct membership edge
构造有序 path（path 的第一个节点固定为 member，最后一个节点固定为目标 role）；过滤不满足该 privilege 的路径，
取最小 edge count；仅在最短路径集合中，把每条 path 作为**当前节点顺序不变**的 RFC8785 JSON array，取其
UTF-8 bytes 的 unsigned lexicographic minimum。path 节点不得 sort、reverse、按显示名重新排列或用 OID 替换；
equal path key 是 duplicate error。validator 必须逐边核对 `(path[i], path[i+1])` 在 direct edge projection 中存在且
options/privilege 条件满足，核对首尾 endpoint、`len(path)-1 == min_depth`、`edge_count` 等于完整
`direct_memberships` projection 行数，并重新计算候选最短路径的 canonical choice；只提交 caller 自报 witness 或仅检查 endpoint 都返回
`MIGRATION_PROJECTION_NON_CANONICAL_WITNESS`。

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
  object_kind: "table" | "sequence" | "function" | "type" | "schema"
  acl: ACLSetProjection
}

DependencyProjection {
  depender: ObjectIdentityProjection
  depended_on: ObjectIdentityProjection
  dependency_kind: string
}
```

`DefaultACLProjection.schema = null` 是**显式 global default ACL**，不是“无 scope”或可省略值。它会影响目标
`cloud_agents` schema 未来创建的对象，因此即使当前 schema 没有对应 relation，也必须进入 1a 的
`CatalogStateProjection` 和 `default_acl_digest`（前提是 owner 在签名的合法集合中；无关角色按下述规则过滤）。
`schema = "cloud_agents"` 是 schema-scoped row；global 与
schema-scoped row 合法共存，必须作为两个独立 canonical rows 保留，不能在存储 projection 中互相覆盖或折叠。

合法的 default-ACL owner 集合不是从 `pg_default_acl` 反推，而是由签名的
`VerifiedSchemaBundleScope.default_acl_owners` 与同一 scope 签名的 `object_creator_closure` 冻结。前者是
允许出现 default-ACL row 的 closed owner allowlist，后者是能为目标 schema 创建对象的完整 principal/role
closure；投影前必须验证 allowlist 中的每个 owner 都在该 closure 中，二者不一致即
`MIGRATION_AUTHORITY_DRIFT`。当前初始 profile 的两个集合都精确为
`["cloud_agents_migration_owner"]`，不得由 caller、环境变量、数据库发现结果或 adapter 扩张。只有同时
出现在这两个签名集合中的 owner，才可投影其 `schema = null` global row 与 `schema = "cloud_agents"`
target-schema row。

对不在该合法集合中的 owner，先检查其对目标 schema 的 effective `CREATE`：存在则返回
`MIGRATION_AUTHORITY_DRIFT`（即使同时存在 target-schema row，也保持此优先级）；没有 effective `CREATE`
但存在 target-schema default-ACL row，则返回 `MIGRATION_PROJECTION_UNKNOWN_OBJECT`。既无 effective `CREATE`
又只有与目标 schema 无关的 global row 的角色，不进入 projection、任何 ACL 子 digest 或
`default_acl_digest`，且不构成 drift。该过滤规则不改变 allowlisted owner 的 global row 必须进入目标 schema
state digest 的要求。

对每个 allowlisted `(owner, object_kind)`，effective 计算公式是 closed 且不可重写的：
`base = global row if present else hardwired defaults`；`effective = base union schema additions`。因此
schema-scoped row 只能追加 privileges，不能 override 或删除 global/base 值；global 与 schema 两个 stored rows
仍按各自 canonical bytes 独立保留，effective 结果不得反写成单行。

二者按以下 closed object-kind profile 解码：

```text
object_kind: "table" | "sequence" | "function" | "type" | "schema"
```

约束是机械的：`schema == null` 才能表示 global；`schema == "cloud_agents"` 才能表示目标 schema；
`object_kind = "schema"` 只允许 `schema == null`。其他 schema name、object kind、owner 或 ACL field 直接返回
`MIGRATION_PROJECTION_INVALID_SCOPE`。`pg_default_acl.defaclobjtype` 的 PG 原始 code 只能由 major adapter
映射到上述 enum；unknown code、unknown grantee、unknown privilege、重复 logical row 均 fail closed。outer rows
按 `(owner, schema-null-before-string, object_kind)` 的 UTF-8 logical key 排序；其中 null schema 排在任意 string
schema 之前。每个 ACL entry 在 row 内再按 `(grantor, grantee, privileges[], grantable[])` 排序。allowlisted owner
的 global rows 必须被包含在每个目标 schema state 的 digest 中；不得只扫描
`defaclnamespace = target_namespace` 而漏掉这些 owner 的 `NULL` rows。无 effective `CREATE` 且无关角色的 global
row 按上面的过滤规则排除，不得因“global 必须扫描”而重新进入 projection/digest。

future effective-default 语义固定为：`hardwired defaults + global alteration + schema addition`。schema-scoped
row 是对目标 schema 的 addition，不是 global row 的 override；两行的 stored projection 与 digest 永远分开，
不把 effective 结果反写或折叠为一行。`DefaultACLProjection.acl.catalog_value` 必须为 `"explicit"`（包括
explicit empty ACL）；`null` 只允许表示没有该 catalog row，不能用于已投影的 default-ACL row。

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

### 4.5 Projection digest domains

三个 state/typed projection digest domain 必须严格区分；domain 是 RFC8785 输入的一部分，不是日志标签：

```text
AuthorityProjection.digest = SHA-256(RFC8785({
  "domain": "cloud-agents-platform-authority-projection/v1",
  "projection": <complete AuthorityProjection without a digest field>
}))

CatalogProjection.digest = SHA-256(RFC8785({
  "domain": "cloud-agents-platform-catalog-projection/v1",
  "projection": <complete CatalogProjection without a digest field>
}))

CatalogStateProjection.digest(schema_absent) = SHA-256(RFC8785({
  "domain": "cloud-agents-platform-catalog-state/v1",
  "state": "schema_absent", "scope": <exact ProjectionScope>, "schema": "cloud_agents"
}))

CatalogStateProjection.digest(schema_present) = SHA-256(RFC8785({
  "domain": "cloud-agents-platform-catalog-state/v1",
  "state": "schema_present", "scope": <exact ProjectionScope>, "body": <complete CatalogProjectionBody>
}))
```

`cloud-agents-platform-catalog-state/v1` 是 catalog state union 的既有 domain，不得改名或复用于最终
`CatalogProjection`；TS/Go same-bits 输入是 flat `{domain, state, scope, schema|body}` object，不包含额外
`projection` wrapper。Authority/Catalog final projection 仍使用上面的 `{domain, projection}` wrapper。任一 projection
digest 不包含 `SnapshotMetadata`、query timing、backend PID、OID、adapter-local capability flag 或 expected digest；
这些只在 bounded metadata/evidence 中出现。缺字段、unknown field、不同 scope 或同一 bytes 采用错误 domain 都必须
返回 stable digest/scope error，不能“兼容计算”。

schema/default-ACL 的 control-plane 子 digest 也各有独立 domain；三者都使用完整 canonical input，不能只 hash
已经展示的 privilege 数组：

```text
schema_explicit_acl_digest = SHA-256(RFC8785({
  "domain": "cloud-agents-platform-schema-explicit-acl/v1",
  "schema": "cloud_agents",
  "explicit_acl": <complete ACLSetProjection including catalog_value and sorted entries>
}))

schema_effective_acl_digest = SHA-256(RFC8785({
  "domain": "cloud-agents-platform-schema-effective-acl/v1",
  "schema": "cloud_agents",
  "owner": <schema owner>,
  "effective_acl": <complete sorted ACLProjection[]>
}))

default_acl_digest = SHA-256(RFC8785({
  "domain": "cloud-agents-platform-default-acl/v1",
  "target_schema": "cloud_agents",
  "rows": <all allowlisted-owner global schema=null and target-schema rows in outer canonical order>
}))
```

`schema_explicit_acl_digest` 排除 effective ACL、default ACL、comment、security labels、OID、timing 和 query metadata；
`schema_effective_acl_digest` 的 canonical input **严格只有**上面的 `domain`、`schema`、`owner` 和完整排序的
`effective_acl`；因此它排除 raw ACL text、OID、role traversal order、database/session identity、explicit ACL
digest、database ACL、default-ACL digest、timing 和 query metadata。default-ACL owner allowlist 与
`object_creator_closure` 是投影前必须验证的 signed precondition，不能偷偷作为额外 digest field。`default_acl_digest`
排除由这些 rows 推导的 future-effective folded result、当前 relation objects 和 OID。每个 row 内
`ACLSetProjection.catalog_value`、grantor/grantee、privileges/grantable 的 exact canonical bytes 都参与其
对应 digest；任何 exclusion 变更都必须新建 ADR，而不能由 adapter 自行省略。

## 5. Snapshot API 与事务复用

Projector 只依赖下列最小接口，不暴露 raw connection、pool、role switch 或任意 SQL 给 caller：

### 5.0 Exact metadata/result shape

Snapshot metadata 描述的是观察边界，不是 projection digest 输入；字段集合 closed：

```text
SnapshotMetadata {
  mode: "idle_read_repeatable_read_only" | "migration_serializable_read_write"
  ownership: "owned_idle" | "borrowed_migration"
  postgres_major: uint16
  server_version_num: uint32
  database_name: string
  authority_phase: "connected_session" | "migration_role" | "migration_transaction"
  session_user: string
  current_user: string
  isolation_level: "repeatable_read" | "serializable"
  access_mode: "read_only" | "read_write"
  deferrable: bool
  tx_status: "T"
  migration_id: string | null
  statement_index: uint32 | null
}

ProjectionMetadata {
  projection_kind: "authority" | "catalog" | "catalog_state"
  digest_domain: "cloud-agents-platform-authority-projection/v1" |
                 "cloud-agents-platform-catalog-projection/v1" |
                 "cloud-agents-platform-catalog-state/v1"
  adapter_profile: "postgresql-authority-v1" | "postgresql-catalog-v1"
  snapshot: SnapshotMetadata
  verified_subject_digest: Digest
  scope: ProjectionScope | null
  limits_profile: "cloud-agents-platform-projection-limits/v1"
  query_count: uint32
  row_count: uint64
  total_bytes: uint64
  redaction_profile: "cloud-agents-platform-projection-redaction/v1"
}

ProjectionResult<T> {
  projection: T
  digest: Digest
  metadata: ProjectionMetadata
}
```

`ProjectionResult` 的 `digest` 只能由 typed projection 按第 4.5 节 domain 重算；`ProjectionMetadata` 不得把
query duration、backend PID/OID、raw SQL、secret 或 caller-supplied fields 带入 result。`verified_subject_digest`
指向已验签的 authority/catalog/bundle subject，不是实际 projection 的替代物；expected/actual 比较仍由
validator 完成。

`ProjectionMetadata` 的 `projection_kind`、`digest_domain`、`adapter_profile`、`scope` 是一个 closed mapping：

```text
authority     => scope = null,
                 digest_domain = cloud-agents-platform-authority-projection/v1,
                 adapter_profile = postgresql-authority-v1
catalog       => scope != null && scope.scope_kind = final,
                 digest_domain = cloud-agents-platform-catalog-projection/v1,
                 adapter_profile = postgresql-catalog-v1
catalog_state => scope != null,
                 digest_domain = cloud-agents-platform-catalog-state/v1,
                 adapter_profile = postgresql-catalog-v1
```

authority 结果不得携带任何伪造的 catalog scope；catalog final 结果不得使用 predecessor/statement-prefix scope；
catalog-state 的 scope 必须与 union branch 一致。任意其他组合返回
`MIGRATION_PROJECTION_METADATA_MISMATCH`。

`owned_idle` 只适用于 `IdleReadSnapshot`：snapshot owner 在 idle connection 上执行 RR/RO/**NOT DEFERRABLE** `BEGIN`，
读取结束后必须 rollback、验证 `TxStatus = I` 并清除 transaction-local state。普通调用方由 factory release 或销毁
pool connection；第 5.4 节的 runner-owned factory 则把同一 dedicated physical connection 交还给 runner，绝不 release
进 pool。两种 factory 都不能把 raw connection 交给 projector。`owned_idle` 的 `migration_id` 和
`statement_index` 必须均为 `null`，
`authority_phase` 只能是 `connected_session` 或 `migration_role`。`borrowed_migration` 只适用于 runner 已拥有的
SERIALIZABLE/RW transaction，`authority_phase` 必须是 `migration_transaction`，`migration_id` 必填；
`statement_index = null` 仅允许 transaction-wide preflight，statement-scoped projection 必须填写 0-based index。
projector 可以读和生成 `ProjectionResult`，但绝不能 `BEGIN`、改变 isolation/role、创建 nested savepoint、commit、
rollback 或 close；runner transaction owner 承担全部生命周期。metadata 中 mode/ownership/isolation/access/phase
或 nullable-field 约束的不一致映射为 `MIGRATION_PROJECTION_METADATA_MISMATCH`。

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

`VerifiedSchemaBundleScope` 的 owner 约束也是 signed closed input，而不是 projector 的可选参数：

```text
VerifiedSchemaBundleScope.default_acl_owners: string[]          // sorted, unique
VerifiedSchemaBundleScope.object_creator_closure: string[]     // sorted, unique principal/role closure
```

两数组是 schema-bundle signed subject 的 exact members，由既有 `schema_bundle_digest` 与 bundle signature 传递
覆盖；不新增独立 closure digest。字段缺失、unknown member 或未签名新增值返回 `MIGRATION_INVALID_MANIFEST`，
signed closure 与观察到的 authority 不一致返回 `MIGRATION_AUTHORITY_DRIFT`，不能由 projection query 自动补全。

### 5.1 Idle read snapshot

`BeginIdleReadSnapshot` 只能在 connection idle 时执行：`BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY
NOT DEFERRABLE`，立即 read back `transaction_isolation`、`transaction_read_only`、`transaction_deferrable` 和
`TxStatus = 'T'`，再运行 authority/catalog queries。所有查询必须在同一 snapshot；禁止 caller/projector 设置
session-level GUC、切 role、创建 nested transaction 或跨连接 join。snapshot factory 在 begin 前必须先
`DISCARD ALL` 并证明 clean session；仅当 closed phase 为 `migration_role` 时，factory 自身才可执行固定
`SET ROLE cloud_agents_migration_owner` 并 exact read back，caller 不能传入任意 role。`connected_session` 不切 role。
结束时 rollback + `DISCARD ALL`，确认 `TxStatus = 'I'` 且 `current_user = session_user` 后才把 connection 交还
pool；状态不明、rollback 失败、role/GUC/prepared state 未清除则 hijack + close。

### 5.2 Migration snapshot reuse

runner 已经打开的每条 migration 短事务使用 `SERIALIZABLE READ WRITE`。projector 接收该 transaction 的
`MigrationSnapshot` 视图并复用完全相同的 query/normalizer；它**不得再次 BEGIN、改变 isolation、切 role 或
创建 nested savepoint**。statement 前、statement 后和 ledger insert 前的 authority/catalog query 必须看见
同一个 migration transaction 的中间状态。最终 cumulative catalog 只有在全部 statement 完成后验证；中间状态
不能拿 final contract 误判为漂移。

snapshot metadata 必须记录 `postgres_major`、database name、session/current user、mode、statement/migration
identity；projection digest 只出现在 `ProjectionResult`。projector 禁止使用 volatile `now()`、随机顺序、
backend PID 或 OID 作为 digest 输入。

### 5.3 New adapter API 与旧 runner validator 并存

A2.1a-impl-2 只新增并验证上述 `ProjectionSnapshot`、`Projector`、metadata/result 和 PG major adapter。现有
`AuthorityValidator`、`CatalogValidator`、`IntermediateValidator` 及其 `FailClosed*` 实现继续保留，作为旧
runner 的 compatibility seam；它们不得被本切片删除、重命名或改成接受未验签 JSON。新 projector API 不自动接入
旧 runner，也不替换旧 phase/transaction wiring。

impl-2 的成功边界是显式的：`ProjectAuthority` 可以在 verified authority contract、strict shape、固定 limits 和
PG15/16/17 adapter 全部通过时返回成功；`ProjectPrecondition` 可以成功返回 `schema_absent|schema_present` 及
1a 的 schema/default-ACL state。`ProjectCatalog` 与 `ProjectTransitionState` 在 A2.1b relation/expression projector
落地前无论目标 schema 是否只有 namespace，都必须返回 `MIGRATION_PROJECTION_NOT_IMPLEMENTED`；不得用
namespace-only projection、空 relation list 或 catalog contract 的 partial body 冒充 full catalog/transition
success。该 boundary 不改变任何 `publication_status`/`runtime_introspection_status`，也不把 mutable catalog
提升为已发布 authority。

只有 A2.1a-impl-3 才允许新增 adapter-backed validator、在 runner statement 前/后调用新 API、替换旧 phase
contract、持久化 `ProjectionResult` digest/bounded metadata evidence 与 intermediate chain，并为旧 validator 增加等价
stable error mapping。impl-2
完成时旧 runner 的执行结果和 `NOT_IMPLEMENTED`/`UNPUBLISHED` 边界必须保持不变；禁止用“新 API 可构造”宣称
runner 已使用 projector 或 Gate 已关闭。当前 Go/TS implementation 可能已经存在本地未提交 adapter/API 草稿，
但它们仍按本 ADR 的 impl-2 seam 适配：Go draft 中的 `SnapshotMetadata.database_identity` 和非 nullable
`ProjectionMetadata.scope` 必须在提交前移除/改为本 ADR shape；TS 当前 subdigest 的 loose `migrationDigest` 调用也
必须在 impl-2 适配为本 ADR 的 explicit domains。TS/Go CatalogState same-bits flat digest 必须保留，Authority/Catalog
wrapper digest 与本 ADR domains 对齐。不得因本地文件存在而把 mutable catalog contract 标为
`IMPLEMENTED`/`PUBLISHED_IMMUTABLE`，也不得在 impl-2 提前改 runner wiring。

### 5.4 A2.1a-impl-3 runner wiring ABI

本节只冻结 runner 接入已完成 projector 的 ABI、证据 journal 与 fail-closed 顺序；不实现 production signature
verifier、A2.1b catalog/expression projector、crash 后自动继续写数据库或任何 Gate closure。

#### 5.4.1 唯一 trust decision 与 opaque projection bindings

runner 只能从第 3 节同一个 release trust verifier 的一次成功结果取得以下 opaque value；不得再调用第二 verifier，
不得从 environment、CLI、DSN、runtime tar 内的 loose JSON 或 caller 参数补齐任一字段：

```text
RunnerProjectionBindings {
  release_trust_decision_digest: Digest
  runner_projection_decision_digest: Digest
  execution_lineage_digest: Digest
  schema_bundle_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
  recovery_policy_subject_digest: Digest
  decision_recovery_artifact_profile_digest: Digest
  verified_authority: VerifiedAuthorityContract
  verified_recovery_policy: VerifiedRecoveryPolicySubject
  initial_schema_scope: VerifiedSchemaBundleScope
  executable_catalogs: ExecutableCatalogBinding[]
  release_expires_at: RFC3339-UTC
  release_security_epoch: uint64
  authority_expires_at: RFC3339-UTC
  authority_security_epoch: uint64
}

ExecutableCatalogBinding {
  schema_head: string
  catalog_contract_digest: Digest
  verified_catalog: VerifiedCatalogContract
  expires_at: RFC3339-UTC
  security_epoch: uint64
}
```

`executable_catalogs` 按 `schema_head` UTF-8 bytes 升序、unique，且只能包含
`PUBLISHED_IMMUTABLE/IMPLEMENTED`、具备完整 `expected_projection` 和每 statement exact
`expected_transition` 的 catalog subject。`verified_authority` 必须同时绑定 exact authority profile/binding digest、
三个 phase expected projection、authority expiry 和 authority security epoch；`initial_schema_scope` 必须绑定同一
`schema_bundle_digest`、inline predecessor、owner closure、release expiry 和 release epoch；每个
`verified_catalog` 必须绑定其 exact catalog digest/head/statement closure、per-head expiry 和 epoch。各 subject 的
expiry/epoch 可因独立签名生命周期而不同，不能强行取同一个值；runner 必须逐项验证并在任一 subject 过期或低于
minimum epoch 时 fail closed。任何 digest、expiry、epoch 或 subject identity 不一致都返回
`MIGRATION_UNTRUSTED`，不能降级成 mutable input。

`VerifiedSchemaBundleScope.default_acl_owners` 与 `object_creator_closure` 的唯一 production 来源固定为 schema
bundle exact signed member：

```text
schema_bundle.projection_scope_authority {
  default_acl_owners: string[]
  object_creator_closure: string[]
}
```

两数组必须 sorted/unique/closed，并受既有 schema bundle signature 和 `schema_bundle_digest` 覆盖；不能从 fixture、
authority actual projection、catalog rows、environment 或部署 binding 推导。当前 manifest/schema-bundle 尚无该 exact
member，因此补齐 strict Go/TS contract、negative fixture、digest regeneration 和 same-bits test 是 impl-3 之前的
contract pre-slice；缺失时不得构造 `RunnerProjectionBindings`，并在连接数据库前返回
`MIGRATION_INVALID_MANIFEST`。

本节新增 `release_trust_decision_digest` canonical formula；它只表达 release subject，不含 deployment-specific authority binding 或
per-deployment executable catalog 集合：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-release-trust-decision/v1",
  "repository_identity": ...,
  "release_identity": ...,
  "schema_bundle_digest": ...,
  "bootstrap_bundle_digest": ...,
  "manifest_digest": ...,
  "outer_artifact_digest": ...,
  "runner_release_digest": ...,
  "expires_at": ...,
  "security_epoch": ...
}))
```

`runner_projection_decision_digest` 才是本次执行的 combined total identity：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-runner-projection-decision/v1",
  "release_trust_decision_digest": ...,
  "schema_bundle_digest": ...,
  "authority_profile_digest": ...,
  "authority_binding_digest": ...,
  "authority_expires_at": ...,
  "authority_security_epoch": ...,
  "recovery_policy_subject_digest": ...,
  "decision_recovery_artifact_profile_digest": ...,
  "catalog_contracts": [{
    "schema_head": ...,
    "catalog_contract_digest": ...,
    "expires_at": ...,
    "security_epoch": ...
  }, ...]
}))
```

`VerifiedRecoveryPolicySubject` 是 current decision 内的 opaque verified signed subject，不是 caller/runtime wire。
`recovery_policy_subject_digest` 必须对以下 closed canonical signed subject body 做 RFC8785+SHA-256：

```text
{
  "domain": "cloud-agents-platform-recovery-policy-subject/v1",
  "issuer_key_identity_digest": ...,
  "expires_at": ...,
  "security_epoch": ...,
  "minimum_old_security_epoch": ...,
  "old_revocation_policy_digest": ...,
  "old_decision_authorizations": [{
    "old_runner_projection_decision_digest": ...,
    "allow_expired": true | false,
    "allow_revoked": true | false,
    "allow_compromised": true | false
  }, ...]
}
```

`old_decision_authorizations` 必须按 old decision digest bytes 升序、unique；issuer/key identity、expiry、epoch、
minimum-old-epoch 与 revocation policy 全部受 detached signature 覆盖。current policy 在每次 open/recovery 时必须
未过期、security epoch 有效且不低于 current minimum；不得由 env/CLI/DSN/GUC 替换、放宽或补齐。
`old_decision_authorizations` 绝对不得包含承载该 subject 的 current
`runner_projection_decision_digest`；它只能列举 strict historical digests，否则会形成 self-reference 并必须
strict reject。
old expiry 只能在该 exact signed subject 下授权；old revoked/compromised/below-minimum 默认拒绝，只有
subject 中 exact old decision entry 对对应状态明文为 true 才可进入 historical validation。缺 entry、wildcard、
range/prefix match 或从 caller 推导授权一律拒绝。具体 signed policy body 只作 verifier-owned input，不进入 C3
wire；C3 只持久其 digest 间接绑定的 decision/header identities。

`decision_recovery_artifact_profile_digest` 固定为
`SHA-256(RFC8785({"domain":"cloud-agents-platform-decision-recovery-artifact-profile/v1",
"format_version":"cloud-agents-platform-decision-recovery-artifact/v1","canonicalization":"RFC8785",
"base64url":"unpadded-canonical","identity_max_bytes":1024,"encoded_field_max_bytes":1048576,
"projection_inputs_max":4099,"catalog_inputs_max":4096,
"kind_rank":["release","authority_profile","authority_binding","catalog"],
"max_size_bytes":4194304}))`。它与 `recovery_policy_subject_digest` 一起进入 combined decision，因而 C2/
后续 contract 必须绑定两者；不得在 C3/runtime 临时补绑。

其中 catalog 数组与 `executable_catalogs` exact same order/members。release decision 内已经绑定 release expiry/epoch；
combined digest 不把 authority/catalog identity 反向塞进 release identity。`ControlPlaneStates.verified_authority_decision_digest`
必须 exact 等于 `runner_projection_decision_digest`；不得指向 release-only digest 或另造 projector-local decision digest。
`catalog_contract_digest` 使用 schema bundle 中 exact artifact descriptor SHA-256；
`authority_profile_digest` 使用 manifest authority descriptor SHA-256；`authority_binding_digest` 使用已验签 binding
subject 的 RFC8785+SHA-256。重连、普通 retry 和 ambiguous reconciliation 都必须重新验证 signature、expiry、
revocation/minimum epoch、runner/repository/release identity，并分别 exact-match 第一次的 release decision digest、
combined projection decision digest 和完整 opaque bindings；只比较 schema bundle digest 不足以继续。

authority binding expiry/epoch、binding digest 或 schema-bundle strict-prefix successor 的合法轮换会改变 combined
decision 与 journal generation identity，但不能改变同一 deployment/database/repository 的 execution lineage。
`execution_lineage_digest` 固定为：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-execution-lineage/v1",
  "deployment_id": <signed authority binding deployment_id>,
  "expected_database_identity": {
    "database_name": <signed connected_session expected projection database_name>
  },
  "repository_identity": <signed release subject repository_identity>
}))
```

lineage key 不得包含 DSN、host、port、credential、expiry、security epoch、authority binding digest、combined decision
digest、release decision digest、schema bundle digest 或随机值。上述 member 必须来自同一个 release verifier 已验证的 signed subjects，并与 opaque bindings 一起
返回；caller 不能自报。

lineage recovery 仍由**同一个** `TrustVerifier` 和 current unexpired verified decision 授权，不引入第二
verifier。为了不让“先需要 terminal 才能连 DB，但又必须连 DB 才能产生 terminal”形成循环，recovery
必须分成以下两级 opaque capability：

```text
TrustVerifier.RecoverHistoricalDecision(
  current OwnedVerifiedDecision,
  current same-verifier recovery capability,
  owned old GenerationDescriptor,
  owned VerifiedDecisionRecoveryArtifact,
) -> (old OwnedVerifiedDecision, old RunnerProjectionBindings,
      VerifiedHistoricalRecoveryPolicy)

BindRecoveryExecution(
  VerifiedHistoricalRecoveryPolicy,
  current OwnedVerifiedDecision,
  old OwnedVerifiedDecision,
  old RunnerProjectionBindings,
  owned old GenerationDescriptor,
  owned old RecoverySnapshot,
) -> VerifiedRecoveryExecutionBindings

BindLineageSupersession(
  VerifiedRecoveryExecutionBindings,
  OwnedSupersessionEvidence,
) -> VerifiedLineageSupersessionAuthority

TrustVerifier.RecoverHistoricalSupersession(
  current C OwnedVerifiedDecision,
  owned GenerationSuperseded(A -> B),
  owned A VerifiedDecisionRecoveryArtifact,
  owned A durable boundary,
  owned planned-B VerifiedRuntimeArtifact,
  owned planned-B VerifiedContentReceipt,
  owned planned-B VerifiedDecisionRecoveryArtifact,
  owned planned-B VerifiedDecisionRecoveryReceipt,
) -> VerifiedHistoricalSupersessionReceipt
```

`RecoverHistoricalSupersession` 也只能由 current C decision 内的 same-verifier recovery-only capability 调用。C 的
current signed recovery policy 必须以两个 distinct exact historical entries 同时授权 A 和 B decision；缺任一
都是 `MIGRATION_EVIDENCE_RECOVERY_REQUIRED`。recovery-only validator 分别对 A/B artifacts 重验原 signatures、
digests、artifact profiles 和 signed subjects；A/B old expiry 只能由 C policy 裁决，不能绕回普通
unexpired validator。然后 verifier 从 owned A replay/durable boundary 和 recovered B candidate deterministic 重建原
A→B `VerifiedHistoricalRecoveryPolicy`、`VerifiedRecoveryExecutionBindings` 和
`VerifiedLineageSupersessionAuthority` 的 canonical bodies，exact 比较 `GenerationSuperseded` 中的
`lineage_supersession_authority_digest` 和 nested planned B `GenerationReserved`。任一 byte/digest/constraint/quota/header/
continuation mismatch 都拒绝。

planned-B 的两个 artifacts/receipts 只能按 nested
`planned_segment0_header.outer_artifact_digest/outer_artifact_size_bytes` 和
`decision_recovery_artifact_sha256/decision_recovery_artifact_size_bytes` 从 object store no-follow 恢复。方法必须对两个
B final objects 分别完整验证 typed receipt kind、SHA-256、size、store identity、owner/mode/link-count/regular-file
与 artifact bytes，并 exact 等于 nested planned header 的同一 pair。artifact/receipt absent 或完整自洽 objects 无法由
current capability 形成 historical authorization，都在 DB 零调用时返回
`MIGRATION_EVIDENCE_RECOVERY_REQUIRED`；已 registered object/header/receipt 存在而 kind 互换、size、digest、canonical bytes、
closed shape 或 pair 不一致是 stored-chain contradiction，必须返回 `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。

`VerifiedHistoricalSupersessionReceipt` 是 non-serializable one-shot capability，只授权在
full-root `Store.AcquireAdmission` + ALL-history two-pass replay 临界区中，通过 recovery-only
`RegisteredPublication`/typed receipts 重建 `AdjacentReserveReady`，再按 nested planned B byte-exact 紧邻追加
reserved→header→activated；它不授权 B
`Connect`、DB read/write、migration SQL、ledger insert、`Commit` 或新 attempt。B durable activated 后若已过期或仍非
current C，必须再以 `activated_no_migration_progress` 的 header-only evidence 构造 B→C supersession，不得直接
让 C 使用 B journal。receipt 消费、append unknown 或任一 sync 失败后永久失效，只能重新 `Open`。
该 receipt 还 exact 绑定上述 B runtime/recovery 两枚 typed durable receipts，只授权写入 nested header 中的同一
digest/size pair，不得在 append 时替换或重取另一 object。

首次 decision 成功时，同一 `TrustVerifier` 必须同时生成 bounded、non-secret、package-private
`VerifiedDecisionRecoveryArtifact`。其 exact canonical bytes 只能是以下 closed JSON，不得持久化完整
`CandidateEnvelope`：

```text
DecisionRecoveryVerificationInputs {
  format_version: "cloud-agents-platform-decision-recovery-artifact/v1"
  profile_digest: Digest
  old_runner_projection_decision_digest: Digest
  repository_identity: string
  release_identity: string
  candidate_subject_base64url_no_padding: string
  candidate_detached_envelope_base64url_no_padding: string
  projection_subject_inputs: [{
    kind: "release" | "authority_profile" | "authority_binding" | "catalog"
    subject_digest: Digest
    subject_base64url_no_padding: string
    detached_envelope_base64url_no_padding: string
  }, ...]
}
```

all fields required/non-null；identity 必须 non-empty NFC UTF-8 且各自最多 1,024 bytes；每个 base64url string
必须 non-empty、无 `=` padding、严格 alphabet 且 canonical round-trip，单字段最多 1 MiB encoded bytes。
`projection_subject_inputs` 必须非空、最多 4,099 members，`release|authority_profile|authority_binding`
各 exact 一个，`catalog` 可 0..4,096；按 kind fixed rank `release=0,authority_profile=1,
authority_binding=2,catalog=3` 后再按 decoded digest bytes 升序、unique。每个 decoded subject bytes 必须重算
SHA-256 并 exact 等于 `subject_digest`，detached envelope 必须对该 exact subject 完成签名链验证。
candidate subject decoded bytes 也必须重算 old decision 已绑定的 candidate subject digest，其 detached
envelope 必须对 exact candidate bytes 验证；不得只因 base64url 可解码就接受。
`profile_digest` 必须 exact 等于 current decision 绑定的
`decision_recovery_artifact_profile_digest`，profile digest 覆盖上述 encoding、sort、cardinality 和全部 bounds；整个
RFC8785 canonical artifact inclusive maximum 仍为 4 MiB。

artifact 明确排除 `CandidateEnvelope.Now`、current clock、observed/verification time、path、random nonce、map
iteration order、credential、token、DSN、raw SQL 和其他 non-deterministic/secret member。recovery validator 的 current
clock 是 non-persisted invocation parameter，只用于验证 current policy expiry。同一 old decision + 同一
verifier/profile 的 artifact 必须 same-bits；profile/decision/digest/sort/bound 任一 mismatch 都在 DB 前返回
`MIGRATION_EVIDENCE_RECOVERY_REQUIRED`；但 artifact 一旦已被 registered header/receipt 引用，其 stored
profile/decision/digest/sort/bound mismatch 按下文优先映射为 `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。artifact 仍是
verifier-owned content object，不是 evidence frame/C3 DTO。

reopen ancestor 时，evidence package 按 old `JournalHeader.decision_recovery_artifact_sha256/size_bytes`
no-follow 打开 content-addressed object，验证 receipt、owner/mode/link-count/regular-file、exact size 和全量 SHA-256，
再把 owned bytes 交回**原同一 verifier 的 recovery-only validator**。该 validator 必须重验原签名、detached
binding、全部 digests 和签名链，但不能调用普通“current unexpired execution” validator 将旧 expiry 失败
伪装成 recovery 结果。old release/authority/catalog expiry 可以已过期，过期本身只能由 **current signed
recovery policy** 对 exact old decision/security epoch/revocation state 显式裁决；revoked、compromised、below-minimum
或 policy 未授权，以及 artifact absent 或完整自洽但无法获 current authorization，都在 DB 零调用时返回
`MIGRATION_EVIDENCE_RECOVERY_REQUIRED`。这里“缺失”只指 registered path/object/receipt absent，或完整自洽 bytes 无法由
current policy/signature/recovery capability 授权；若 registered stored object/header/receipt 已存在但 size/digest/
canonical/closed-shape 不 exact，evidence layer 必须先分类为 `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`，不得送 verifier 后降级
为 recovery-required。成功时必须返回完整 old `OwnedVerifiedDecision` 和
`RunnerProjectionBindings` 以及 current signed subject 产生的 outcome-independent opaque
`VerifiedHistoricalRecoveryPolicy`，不得只恢复
digest summary。

`VerifiedRecoveryExecutionBindings` 可在 old terminal 不存在时，由 current decision 内的 same-verifier capability、
恢复的 old decision/bindings、owned old `GenerationDescriptor` 和 owned old `RecoverySnapshot` 构造。它只授权
对**旧 attempt** 的 exact recovery：使用 current authority projection、只读 old artifact/schema/catalog/ledger、取 exact
advisory lock，以及向 old journal 追加 terminal/checkpoint/resolution。它明确禁止 migration SQL、ledger
insert、`Commit`、开启新 attempt、supersede/reserve/activate。普通 same-generation retry 仍必须重验并
exact-match 原 decision/bindings；cross-generation 不得走普通 retry，只能走这个 recovery capability。

`recovery_execution_bindings_digest` 不 hash opaque wrapper，固定为以下无环 flat canonical object：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-recovery-execution-bindings/v1",
  "historical_recovery_policy_digest": ...,
  "execution_lineage_digest": ...,
  "current_runner_projection_decision_digest": ...,
  "old_runner_projection_decision_digest": ...,
  "old_journal_identity_digest": ...,
  "old_schema_bundle_digest": ...,
  "old_decision_recovery_artifact_sha256": ...,
  "old_decision_recovery_artifact_size_bytes": ...,
  "old_journal_replay_tail_digest": ...,
  "old_recovery_state": ...,
  "actions_profile": "cloud-agents-platform-old-attempt-exact-recovery/v1"
}))
```

`old_journal_replay_tail_digest/recovery_state` 只能来自同一 owned replay snapshot；`actions_profile` 是上述允许/禁止
动作的 closed code constant，不是 caller 传入的 action 数组。该 digest 不包含 terminal、checkpoint、resolution、
supersession authority、quota、header 或 planned reservation，因此不会重建鸡蛋循环或 digest cycle。

current verifier 签发的 `VerifiedHistoricalRecoveryPolicy` 只绑定 signed/verified old+successor identities、
outcome 许可集合和每个 outcome 的 continuation identity constraint，不选择 observed outcome，也不绑定本地
quota、header、frame、checkpoint 或 reservation。`historical_recovery_policy_digest` 固定为以下 flat
canonical object：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-historical-recovery-policy/v1",
  "recovery_policy_subject_digest": ...,
  "execution_lineage_digest": ...,
  "old_journal_identity_digest": ...,
  "old_runner_projection_decision_digest": ...,
  "old_schema_bundle_digest": ...,
  "old_decision_recovery_artifact_sha256": ...,
  "old_decision_recovery_artifact_size_bytes": ...,
  "successor_runner_projection_decision_digest": ...,
  "successor_schema_bundle_digest": ...,
  "allowed_outcomes": [<sorted unique RecoveryOutcome>, ...],
  "outcome_constraints": [{
    "outcome": RecoveryOutcome,
    "continuation": {"kind":"must_be_null"} |
                    {"kind":"exact_identity","identity":LineageContinuationIdentity} |
                    {"kind":"exact_carry_old_generation"}
  }, ...]
}))
```

`RecoveryOutcome` 的 closed union 是 `exact_committed_bundle_complete|exact_committed_continue_successor|
precommit_aborted_retryable|exact_pending|resolved_pending|confirmed_abort_terminal|terminal_failure|
divergent_terminal|activated_no_migration_progress`。
`allowed_outcomes` 与 `outcome_constraints` 必须都按 outcome UTF-8 bytes 升序、unique，两者成员集合
exact 相同；空集合、重复、unknown outcome、缺 constraint 或多 constraint 都拒绝。前两个 terminal
outcome 分别要求 `must_be_null|exact_identity(next-entry)`，三个 pending/retry outcome 要求
`exact_identity(next-attempt)`，`divergent_terminal` 要求 `must_be_null`，
`confirmed_abort_terminal|terminal_failure` 也要求 `must_be_null`，
`activated_no_migration_progress` 只允许 `exact_carry_old_generation`。

`VerifiedRecoveryExecutionBindings` 消费完整 allowed set/constraints，不选择、删减或固定 outcome。只有
DB observation 与 owned durable terminal/checkpoint/resolution（或 header-only descriptor）都已形成后，
`BindLineageSupersession` 才选择唯一 `observed_outcome`，证明它属于 `allowed_outcomes`并 exact 验证
对应 continuation constraint。header-only descriptor-known 也不得在第一层预选 outcome。

ordinary old reconciliation 得到的 terminal/checkpoint/resolution 已 durable 后，package-private evidence binder 才能用
`VerifiedRecoveryExecutionBindings` 和 owned replay evidence 生成 `VerifiedLineageSupersessionAuthority`；唯一不需
terminal/checkpoint 的例外是下述 header-only `activated_no_migration_progress`。
`OwnedSupersessionEvidence` 是 outcome-discriminated closed union：普通 outcome 要求 owned checkpoint 与所需
terminal/resolution adjacency；`activated_no_migration_progress` 则要求 owned `GenerationActivated` +
`JournalHeader` + segment-0 initial tail，且 checkpoint/intent/intermediate/commit/terminal/resolution 全部不存在。
该 outcome 的机械边界只是 exact header-only、未越过 durable `StatementIntent`；允许旧 session 曾
`Connect` 并执行 read-only authority/catalog/ledger preflight，因为它从未获得 migration SQL、ledger insert 或
`Commit` authority。任一 intent 或其后 DB-progress witness 非零都拒绝。
`lineage_supersession_authority_digest` 固定为：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-lineage-supersession-authority/v1",
  "historical_recovery_policy_digest": ...,
  "recovery_execution_bindings_digest": ...,
  "execution_lineage_digest": ...,
  "old_journal_identity_digest": ...,
  "old_runner_projection_decision_digest": ...,
  "old_schema_bundle_digest": ...,
  "old_checkpoint_record_digest": ... | null,
  "old_activation_record_digest": ... | null,
  "old_initial_journal_tail_digest": ... | null,
  "old_terminal_digest": ... | null,
  "old_resolution_digest": ... | null,
  "observed_outcome": ...,
  "successor_runner_projection_decision_digest": ...,
  "successor_schema_bundle_digest": ...,
  "continuation": LineageContinuationContext | null
}))
```

policy/execution/supersession digest 都是 domain 与字段同层的 flat object，禁止额外 wrapper nesting。
supersession authority 不绑定 quota/header/reservation；evidence 层只能在取得 authority 之后、按第 5.4.4
节 lock protocol 生成 quota、header 和完整 planned `GenerationReserved`。三者的 production loose constructor 全部
禁止。

policy/authority/superseded/planned 的 closed matrix 固定为：

- `exact_committed_bundle_complete|confirmed_abort_terminal|terminal_failure|divergent_terminal`：continuation
  identity/context 与 planned reservation 全为
  null；
- `exact_committed_continue_successor`：`begin_first_attempt_next_entry` 且 planned reservation 必须非 null；
- `precommit_aborted_retryable|exact_pending|resolved_pending`：`begin_next_attempt` 且 planned reservation 必须非
  null；
- `activated_no_migration_progress`：planned reservation 必须非 null，并且 successor continuation 与 old activated
  generation continuation byte-canonical exact carry；old continuation 非 null 时 historical policy constraint 必须是
  `exact_carry_old_generation`，old continuation 为 null 时 successor planned continuation 也必须为 null，但
  replay 必须因 immediately preceding superseded 将其识别为 `brand_new_inherited`，不得降级为 true
  `brand_new`。

`confirmed_abort_terminal` 只能选择 owned `aborted_terminal` +
`MIGRATION_TRANSACTION_BOUNDARY` + non-null validated boundary proof，且必须是 attempt budget 已耗尽或协议层明确
不可 retry 的 confirmed PostgreSQL abort；`terminal_failure` 只匹配其余 nonretryable `aborted_terminal`。两者都
`return_failure`，禁止新 attempt，且 acknowledgment-unknown/ambiguous commit 永远不得归入任一者。

binder 只能把 selected `must_be_null|exact_identity|exact_carry_old_generation` constraint 分别展开为 null、
owned terminal exact digest 绑定的 context，或 old generation continuation 的 byte-exact copy，并补入 owned source
journal/durable boundary/terminal digests；start action、migration、attempt 必须不变。
`activated_no_migration_progress` 不得重猜 start action；successor 只能 exact carry old reserved/header 已绑定的
continuation。

当前 production verifier 仍是 rejecting implementation。测试可在 migration package 内由 test-only verifier 构造
opaque bindings，但 production API 不得导出接受 loose projection JSON 的 constructor。

#### 5.4.2 Same-connection snapshot factory

ADR-0009 runner 的 `connected_session` 与 `migration_role` projection 必须运行在 runner 已拥有的同一条 dedicated
physical connection 上。内部 closed factory 只接受该 connection 和 enum phase，不接受 pool、任意 role 或 SQL：

```text
BeginRunnerSessionProjectionSnapshot(connection, "connected_session" | "migration_role")
  -> RunnerSessionProjectionSnapshot

RunnerSessionProjectionSnapshot.RollbackAndReturnToRunner()
```

factory 在该 connection idle 时开启 RR/RO/NOT DEFERRABLE transaction，复用相同 fixed-query registry；rollback 后
必须证明 `TxStatus=I`，然后把原 connection 交还 runner。`connected_session` 只能在任何 `SET ROLE`、session setting
和 advisory lock 前调用；此时允许在 begin 前做一次 initial sanitation。`migration_role` 只能在 runner 已完成固定
`SET ROLE`/settings exact readback 并取得 signed advisory lock 后调用；factory 不得 `DISCARD ALL`、`RESET ROLE`、
`SET ROLE`、unlock、release 进 pool 或换 connection，rollback 后仍须证明 `current_user` 是 migration owner 且 exact
session lock held。unknown phase 或 lifecycle mismatch 返回 `MIGRATION_PROJECTION_METADATA_MISMATCH`。

进入 migration transaction 后只能使用 `BorrowMigrationProjectionSnapshot`；它必须复用 runner-owned
SERIALIZABLE/RW transaction，不 nested `BEGIN`、不 savepoint、不 commit/rollback/close。这样三 phase 都观察同一
physical session，且只有 runner 是 role、lock 和 transaction lifecycle owner。

#### 5.4.3 Exact phase、StatementPlan 与 pre-ledger 顺序

runtime artifact 完成 strict decode 后、连接数据库前，runner 必须先证明 schema bundle 中每个 entry 的 catalog
binding 可执行，且每个 statement descriptor 都有以下 exact closed plan：

```text
StatementPlan {
  migration_id: string
  statement_index: uint32
  sql_artifact_sha256: Digest
  sql_artifact_size_bytes: uint64
  start_offset: uint64
  end_offset: uint64
  statement_sha256: Digest
  classification: StatementClassification
  expected_transition: ExpectedStatementTransition
  expected_transition_digest: Digest
}
```

runner 必须从 signed catalog subject 取出 `expected_transition`，重算
`SHA-256(RFC8785(<exact ExpectedStatementTransition>))` 并与 plan digest 比较，再核对 migration ID、0-based index、
SQL artifact digest/size、statement exact bytes digest、offset 和 narrow classification。actual state、classifier output 或 generator evidence
都不能生成/覆盖 expected transition。任一 schema head 不在 `executable_catalogs`，任一 transition 缺失/unknown，或
catalog 是 `UNPUBLISHED_BOOTSTRAP_MUTABLE/NOT_IMPLEMENTED`，都必须在 `Connect`、projection query、
`BeginMigration` 和 `ExecuteStatement` 前返回 fail-closed；该 run 的 connection/statement/ledger insert/commit 调用数
全部为零。

可执行 binding 下的顺序固定：

1. 每一条新 physical connection（initial、retry、reconcile 均包括）都必须先在切 role 前用 runner-owned snapshot 投影并
   比较 `connected_session` authority；不得从旧 connection 继承该 phase；
2. 固定 role/settings、取得 lock 后，用同 connection snapshot 投影并比较 `migration_role`；随后在每个 entry 前、
   每次 reconnect/retry/reconcile 后重做该 phase、lock、ledger prefix 和 predecessor/final catalog preflight；
3. 每个 attempt 开启 SERIALIZABLE/RW transaction 后，使用 `statement_index=null` 的 borrowed snapshot 做
   transaction-wide authority/predecessor preflight；
4. statement `i` 执行前，以 `statement_index=i` 投影 authority 与 exact `catalog_before`，比较同 phase verified
   authority 和 signed state digest；全部通过并 durable append `statement_intent` 后才可执行 exact SQL bytes；
5. statement 返回后，在同 transaction、同 index 投影 authority 与 `catalog_after`；比较后生成 intermediate。
   非 final statement 必须先 durable append intermediate 才能进入下一条 SQL；
6. final statement 后使用 `statement_index=null` 重做 lock/current user/TxStatus、migration-transaction authority 和
   final cumulative `ProjectCatalog`；final CatalogProjection 必须等于 signed expected projection，且 body byte-equal
   final statement-after CatalogState。只有该 equality 成立后才 durable append final intermediate；
7. durable final intermediate 后才允许 ledger insert；ledger row exact readback 后 durable append `commit_intent`，
   然后且仅然后调用 transaction commit。

`ControlPlaneStates` 表示该 statement 的 **after state**。它的 authority/schema/default-ACL subdigest 全部来自同一个
after snapshot；final statement 则来自 pre-ledger snapshot，并且必须与 immediate after projection exact 相等。
pre-ledger 不生成第二个 intermediate。任何 before/after/pre-ledger mismatch 都发生在 ledger insert 前并 rollback。

#### 5.4.4 Crash-durable append-only EvidenceJournal

impl-3 的“持久化”固定为 runner-local crash-durable `EvidenceJournal`，不是 ledger 字段、tenant payload、日志或
best-effort observer。journal 在连接数据库前打开并 replay；没有成功 replay 和 single-writer ownership 就不能执行。
最小机械接口固定为：

```text
EvidenceSink.Open(context, current VerifiedEvidenceRun, current VerifiedRuntimeArtifact)
  -> (EvidenceSession | null, RecoverySnapshot | null, error)

EvidenceSession.CurrentCandidate() -> OwnedCurrentCandidate
EvidenceSession.ActiveGeneration() -> ActiveGeneration
EvidenceSession.Journal() -> EvidenceJournal
EvidenceSession.RecoverySnapshot() -> RecoverySnapshot | null
EvidenceSession.ReserveAndActivateSuccessor(
  context,
  sealed VerifiedLineageSupersessionAuthority,
)
  -> (ActiveGeneration, RecoverySnapshot | null, error)
EvidenceSession.Close(context) -> error

EvidenceJournal.Replay(context) -> (JournalCursor, RecoverySnapshot | null, error)
EvidenceJournal.AppendDurable(context, JournalCursor, sealed OwnedEvidenceRecord)
  -> (AppendResult, error)
EvidenceJournal.Close(context) -> error

VerifiedRuntimeArtifact = opaque runner-owned bounded bytes/reader
VerifiedContentReceipt = opaque internal durable content-object identity
VerifiedDecisionRecoveryArtifact = opaque verifier-owned bounded recovery bytes
VerifiedDecisionRecoveryReceipt = opaque internal durable recovery-object identity
OwnedCurrentCandidate = opaque {
  verified_run,
  verified_runtime_artifact,
  verified_decision_recovery_artifact
}
OwnedVerifiedDecision = opaque {
  verified_decision_evidence,
  same_trust_verifier_recovery_capability
}

ActiveGeneration = opaque {
  kind: "current" | "ancestor_recovery"
  owned_journal: EvidenceJournal
  owned_decision: OwnedVerifiedDecision
  owned_schema_bundle: OwnedVerifiedSchemaBundle
  content_receipt: VerifiedContentReceipt
  decision_recovery_receipt: VerifiedDecisionRecoveryReceipt
  recovery_execution_bindings: VerifiedRecoveryExecutionBindings | null
}

VerifiedEvidenceRun {
  current_decision: OwnedVerifiedDecision
  decision_recovery_artifact: VerifiedDecisionRecoveryArtifact
  release_trust_decision_digest: Digest
  runner_projection_decision_digest: Digest
  execution_lineage_digest: Digest
  outer_artifact_digest: Digest
  outer_artifact_size_bytes: uint64
  decision_recovery_artifact_sha256: Digest
  decision_recovery_artifact_size_bytes: uint64
  manifest_digest: Digest
  runner_release_digest: Digest
  schema_bundle_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
}

JournalCursor {
  segment_index: uint32
  next_sequence: uint64
  previous_record_digest: Digest | null
  lineage_index_next_sequence: uint64
  lineage_index_previous_record_digest: Digest
  latest_checkpoint_record_digest: Digest | null
}

AppendResult {
  outcome: "durable" | "unknown"
  durable_cursor: JournalCursor | null
  candidate_sequence: uint64
  candidate_previous_record_digest: Digest | null
  candidate_record_digest: Digest
  rotation_header_record_digest: Digest | null
  rotation_header_checkpoint_record_digest: Digest | null
  candidate_checkpoint_record_digest: Digest
}

OwnedEvidenceRecord = package-private sealed {
  wire_record: EvidenceRecord
  witness: OwnedEvidenceWitness
  active_generation_identity: opaque
  consumed: bool
}

OwnedEvidenceWitness = package-private non-serializable closed capability union {
  owned_statement_intent | owned_intermediate |
  owned_commit_intent | owned_attempt_terminal | owned_ambiguous_resolution
}

RecoverySnapshot = opaque {
  state: "brand_new" | "brand_new_inherited" | "completed" |
         "dangling_statement_intent" | "dangling_intermediate" |
         "dangling_commit_intent" | "ambiguous_unresolved" |
         "terminal" | "divergent"
  migration_id: string | null
  attempt_index: uint32 | null
  lineage_continuation: OwnedRecovered<LineageContinuationContext> | null
  last_statement_intent: OwnedRecovered<StatementIntent> | null
  last_intermediate_evidence: OwnedRecovered<StatementIntermediateEvidence> | null
  commit_intent: OwnedRecovered<CommitIntent> | null
  last_terminal: OwnedRecovered<AttemptTerminalState> | null
  last_resolution: OwnedRecovered<AmbiguousResolutionState> | null
  last_terminal_digest: Digest | null
  last_resolution_digest: Digest | null
  last_statement_intent_record_digest: Digest | null
  last_intermediate_evidence_record_digest: Digest | null
  last_commit_intent_record_digest: Digest | null
  previous_attempt_terminal_digest: Digest | null
  last_intermediate_state_digest: Digest | null
  next_permitted_action: "begin_first_attempt" | "append_aborted_retryable" |
                         "append_aborted_terminal" | "reconcile_commit" |
                         "begin_next_attempt" | "begin_first_attempt_next_entry" |
                         "return_success" | "return_failure"
}
```

`ActiveGeneration.kind=current` 时 `recovery_execution_bindings=null`；`kind=ancestor_recovery` 时必须持有由
recovery-only validation 得到的 old full decision/bindings 所构造的 non-null
`VerifiedRecoveryExecutionBindings`。ancestor 不得把 current candidate bindings 塞入 old active generation，也不得只
凭 `JournalHeader` digest 构造可连 DB 的 capability。

`VerifiedRuntimeArtifact` 只能由 runner 对已读入内存或受同一 bounded reader 控制的 outer artifact 在 size bound、
release descriptor digest 和全量 SHA-256 均验证后构造；它不接受 loose path、URL 或 caller 声称的 digest。
`VerifiedDecisionRecoveryArtifact` 只能由产生 `OwnedVerifiedDecision` 的同一 verifier 构造，并与该
decision 一起封入 `VerifiedEvidenceRun`；caller 不能单独替换它。runtime artifact receipt 与 decision-recovery
receipt 是两个独立 content object binding，各自锁定 kind、SHA-256、size 和 durable object identity，不得跨
kind 复用 receipt。
`EvidenceSink.Open` 必须先取得 root-wide/single-writer locks、验证 current decision/artifact identity 并 strict replay
LineageIndex；若存在旧未决 generation，不得先盲目 publish current object。此时 session 的 `ActiveGeneration.kind` 为
`ancestor_recovery`，其 old runtime 与 decision-recovery receipts 只能由 evidence package 按 durable journal header 的两组
digest/size + content-addressed object store 内部 no-follow 恢复并全量重验，caller 不能注入 path/receipt。session
必须先由 recovery-only validator 恢复 old full decision/bindings，再用两级 execution/supersession capability 收敛
ancestor；之后同一个 session 调用 `ReserveAndActivateSuccessor` 才能 publish current objects、reserve 并
activate successor，不得关闭后以 brand-new session 绕过旧 state。

`OwnedVerifiedDecision.same_trust_verifier_recovery_capability` 是第一次 trust verification 随 decision 封装的
package-private、non-serializable capability；`Open` 的 ancestor recovery 和 `ReserveAndActivateSuccessor` 必须通过
它回到原 verifier，再用 session-owned replay bodies 调 execution/supersession binder。方法不接受
caller-supplied policy/authority/verifier/artifact，也不能 lookup 另一个 verifier；因此无第二 verifier 或 loose
injection path。

impl-3 filesystem slice 必须直接使用第 5.4.1 节已冻结的 same-verifier recovery graph：current
`OwnedVerifiedDecision` 持有唯一 recovery capability；already-owned historical `VerifiedDecisionRecoveryArtifact` 是由
该 capability 送入同一个 `TrustVerifier.RecoverHistoricalDecision` 的输入，不是 recovery output，也不能在 recovery
时第二次构造。该方法的 outputs 严格只有 old `OwnedVerifiedDecision`、old `RunnerProjectionBindings` 与
`VerifiedHistoricalRecoveryPolicy`；package-private binder 随后才产出 `VerifiedRecoveryExecutionBindings` 和
`VerifiedLineageSupersessionAuthority`。`RecoverHistoricalSupersession` 严格只产出
`VerifiedHistoricalSupersessionReceipt`，不得重签发 artifact、decision、bindings、policy、execution bindings 或
authority。filesystem package 只消费这些 opaque values，不能实现签名、policy、
expiry/revocation 或 artifact verification 的第二套 verifier，也不能把 disk bytes、environment 或 test hook 升格成
capability。磁盘 strict decode 只恢复 C3 wire DTO 与 typed `RecoverySnapshot`；它**绝不能**恢复、反序列化、伪造或缓存
任一 verified/owned capability。重启后的 capability 必须再次由 current decision 的 same-verifier recovery-only path，
结合全部 registered historical artifacts 与 replay-owned bodies 重新签发。

`OwnedEvidenceRecord` 只能由 package-private binder 从 exact C3 `EvidenceRecord`、当前 active generation、signed
StatementPlan/catalog inputs 和对应 non-serialized `OwnedEvidenceWitness` 构造；binder 必须完成第 5.4.7 节 external
chain witness、record-kind/body、self/link digest、cursor/generation、limits 和 action authorization。它是 sealed、
single-use value，不能由 public caller、wire decoder、JSON fixture 或 filesystem 实现直接构造。`AppendDurable` 只能
检查并消费该 owned value，不得接受裸 `EvidenceRecord`；写盘时只序列化其中的 wire record/frame，witness、capability、
connection/rollback/commit receipt 与 owned identity 永不落盘。append `unknown`、消费后重用、generation/cursor swap 或
witness mismatch 都 fail closed；replay 可重建 typed facts，但不能重建 `OwnedEvidenceWitness` 或再次授权同一 DB action。
`JournalHeader` 不属于 caller `OwnedEvidenceWitness` union：segment-0 header 只能由 filesystem 内部 package-private sealed
activation-header binder 基于 exact generation/reservation/verified receipts 构造；rotation header 只能由内部 sealed
rotation-header binder 基于 active generation、durable cursor、previous segment tail 与 fixed limits 构造。两者都不接受
StatementPlan/caller record，不走 caller-facing `AppendDurable(OwnedEvidenceRecord)` seam；但 rotation header 的内部
durability仍与 caller append 组成下文 composite operation。header wire bytes/self-digests 保持第 5.4.7 节 same-bits，
filesystem 只拥有 construction authority，不能新增 header wire member。

successor API 精确选择“显式 sealed 参数”而非在 filesystem 内派生：runner/evidence binder 先用
`VerifiedRecoveryExecutionBindings + OwnedSupersessionEvidence` 选择唯一 outcome 并签发
`VerifiedLineageSupersessionAuthority`；binder 消费 owned evidence，随后只把 sealed authority 交给
`ReserveAndActivateSuccessor`。session 必须在 invalidation/reacquire 后从 durable bytes重建 old boundary，再与参数中的
policy/execution/authority digest、old journal/schema/decision、outcome、continuation 和 planned successor 做 byte-exact
cross-bind；authority 必须同 session、同 generation、同 replay tail 且 single-use，session-owned replay evidence 必须与
authority 已绑定的 evidence digests exact。filesystem 不得重新选择
outcome、调用 loose binder、接受 digest-only authority，或从 `GenerationSuperseded` 反向恢复 capability。

若 lineage 已 ready，`Open` 可直接激活 current generation。无论哪条路径，session 都把验证后的 continuation 构造成
immutable `LineageContinuationContext` 注入 active `EvidenceJournal` 后再完成 journal `Replay`，才暴露 journal 和
opaque recovery snapshot。caller 不能伪造 cursor/snapshot/context/active generation。
`EvidenceJournal.Replay` 必须合并 journal frames 与注入的 lineage context 后计算 snapshot，不能仅看
segment-0/header。`Replay` 可在 append
response unknown 后或 reopen 时幂等重跑。只有 `AppendResult.outcome=durable` 且 `error=nil` 时
`durable_cursor` 非 null，并成为下一次 append 的唯一 authority；`outcome=unknown` 时普通 cursor 立即失效，caller
只能保存 candidate identity 并重新 `Replay`，不能猜测 append 是否成功。

`OwnedRecovered<T>` 是 evidence 实现拥有、只读且由 journal+lineage replay cursor、record digest 与 typed closed
decoder 共同约束的 opaque body/accessor；caller 不能构造、mutate 或把另一个 cursor/generation 的 body 注入
snapshot。各 typed body 的
self/record/link digest 必须与并列 digest fields 及 journal tail exact。这样 crash reopen 后
`reconcile_commit`/`begin_next_attempt` 可直接取得实际 intent/terminal/resolution typed input，而不是只靠 digest 猜测。
这里的 `RecoverySnapshot` 是本节 closed state/action union 的 dedicated typed runtime value；不得复用、type alias、
type assert 或扩展现有 generic `evidenceJournalSummary`（包括同名 private summary）来承载它。summary 可作为纯内部统计，
但不能取得 owned typed bodies、cursor、next-action 或 recovery authority；所有 public/package seam 都只接收本节 typed
`RecoverySnapshot`/`OwnedRecovered<T>`。

需要 full-root inventory/quota/publish/reserve/activate 的 `Open`/`ReserveAndActivateSuccessor` 必须按下文
`Store.AcquireAdmission` 临界区对 objects、journals、全部
durable `GenerationReserved` 和 LineageIndexes 做 bounded no-follow scan，先计算 object+journal+index combined
admission，再把已验证 outer runtime artifact 与 `VerifiedDecisionRecoveryArtifact` 分别以
`<root>/objects/sha256/<digest hex>` publish 为 content-addressed objects 并 durable reserve；两者使用相同的
no-replace/no-follow publish protocol，但 receipt kind 不可互换。这样并发 session 不能分别通过检查后
overcommit。root usage 的 authority 只有 durable object files、strict LineageIndex records 和其中
`GenerationReserved`；不得新增另一份 mutable quota state 或 database。

实现可以先由 sibling package `services/control-plane/internal/evidencefs` 产出只覆盖 `objects/sha256` 的 opaque
`Scan`，但该 commit-1 `Scan` **不是** full root quota authority，不能单独授权 admission、publish 后的 reserve 或
`GenerationReserved`。full root quota binder 必须在同一 active root-wide lease 下，把该 object scan 与 strict replayer
内部派生的全部 registered lineage/index/journal accounting 合并；replayer 必须跨所有 registered schema-bundle digests
枚举并验证每个 registration，不能只按 current bundle 过滤，也不能接收 caller 提供的 count/bytes/facts。strict replayer
属于 migration trusted code：它只能消费 evidencefs 同 lease/epoch/revision 的 opaque bounded views，完成 C3/FSM/historical
validation 后 mint `VerifiedAdmissionPlan`；evidencefs 只产 filesystem views，不理解 replay DTO/result，也不反向 import
migration。本决议不要求新增 `evidenceindex` package，但 replay outputs 只能从本次 no-follow inventory + exact migration
replay 内部产生，不得由 wire、public API 或普通 caller 构造。

full-root admission 的下一实现切片在此进一步冻结为 `AdmissionLease → AdmissionInventory → AdmissionPermit` 三个
opaque、不可复制且不可由普通 caller 构造的 authority。三者必须 exact 绑定同一个 evidencefs `Store`
identity、同一个仍 active 的 root lease、该次 full inventory epoch 和从零开始单调递增的 revision；每次检查都必须同时
验证私有 self/seal、store identity、lease identity/active、epoch 与 expected revision。zero value、值复制、已消费或 stale
revision、cross-store/root/lease/epoch 替换一律拒绝，不能靠相同 path、fd、`st_dev/st_ino`、digest、counter 或
same-package literal 修复。`AdmissionInventory` 与 `AdmissionPermit` 只能沿这一 authority chain mint；object-only
`Scan`、raw usage DTO、caller counter 或 foreign fd 永远不能升格。

唯一 acquisition 是 evidencefs `Store.AcquireAdmission`：先取得 `<root>/lineages.lock` root lease，在该 lease 下按 closed
root grammar 枚举全部 registered lineage，再按 canonical lineage digest byte order 对每个 `writer.lock` 只做
nonblocking try-lock。任一 lineage busy 时，必须逆序释放本轮已取得的全部 lineage locks，再释放 root lease，执行
bounded、context-aware backoff 后从 root discovery 重新开始；不得持 root 等待 busy lineage，不得跳过 busy/current 以外的
lineage，也不得接受 migration 传入的 path/fd 或 dev-inode assertion。成功返回的 `AdmissionLease` 独占 root 与此次枚举的
全部 lineage locks；单-lineage `AcquireRootThenTryLineage` 仍服务普通 open/handoff，但不能冒充 full-root inventory authority。
第一阶段只锁定并 inventory **existing registered set**；若 requested target 不存在，inventory 只携带由 closed root scan
产生的 typed `target_absent` fact，不创建 directory/index/lock，也不把不存在的 target 伪装成 registered member。该 fact 必须
绑定 target canonical digest、root lease、epoch/revision 与 existing full-set digest，不能由 migration 自报。

双权威与 package dependency 固定为单向 `migration → evidencefs`，禁止 import cycle。evidencefs 只 mint
`AdmissionLease`、epoch/revision-bound opaque filesystem `AdmissionInventory`/read views 和 one-shot mutation token；它只理解
closed filesystem grammar、物理事实与 token state，绝不 import migration、decode C3、理解 verifier capability/
`GenerationReserved` DTO，或接受 raw quota counts。migration 用 C3 strict decode、FSM、same-verifier historical recovery
与 quota formula 独立 mint opaque `VerifiedAdmissionPlan`。migration package-private composite permit binder 必须同时 consume 当前
`AdmissionInventory`/mutation token 与 `VerifiedAdmissionPlan`，逐项 cross-bind store/lease/epoch/revision、全部 registered
read-view identity、candidate objects、target index tail、split/aggregate quota 和 planned bytes 后才 mint
`AdmissionPermit`；该 permit 是 migration-owned composite wrapper，但每个 filesystem mutation 仍只能消费其中不可拆出的
evidencefs token。任何一侧都不能单独授权 publish/reserve，migration same-package literal 也不能伪造 evidencefs token。
opaque filesystem views 只提供 bounded accessors：canonical ordered lineage IDs；每个 index 的 exact owned bytes + file
identity；每个 registered generation 的 canonical segment names、exact owned bytes + file identities；每个 registered object 的
controlled full-byte read handle + digest/size/file identity。每次 accessor 都重验 active lease/epoch/revision，返回 owned copy 或
由 lease 控制的 bounded read，不暴露可缓存 raw fd/path。`VerifiedAdmissionPlan` 必须 cross-bind 每个 view identity、每个完整
byte set 的 domain-separated full-set digest 与 exact cardinality；不能只绑定 subset、current target 或汇总 counters。

evidencefs 在该 lease 内以 component-wise no-follow walk 闭合 root/object/lineage/generation/journal/segment 的 entry grammar、
owner/mode/type/link/device、physical bytes/count 与 registration correspondence，并只向 migration 暴露绑定本 epoch/revision
的 opaque inventory/replay views。migration 仍是 C3 strict decoder、LineageIndex/journal FSM、external historical verifier
和 quota formula 的唯一 authority；filesystem entry 名或 inventory 事实不能自证 C3。strict replay 必须覆盖 **ALL**
registered lineages、indexes、generations、segments、journals 与 schema-bundle digests，不按 current lineage、current
generation、current decision 或 current schema bundle 过滤；unknown/unregistered generation、segment 或 journal、缺失
registration、torn/middle corruption 都按本节 closed mapping fail closed。

每个 historical `GenerationReserved` 的 quota facts 必须从其 planned header 引用的 runtime 与 decision-recovery objects
no-follow 全量重验，并经与当前 run **同一个 verifier instance** 的 historical-decision recovery-only path 恢复
`VerifiedRuntimeManifest`、`ExecutionPolicy.MaxAttempts`、完整 statement/object closure。migration 必须据此重跑同一
whole-bundle formula，得到 journal/index split reservation，并与 stored `GenerationReserved`、planned header 与 quota
digest byte-exact 比较；不能信任 stored counter 或由磁盘 decode mint capability。artifact/decision/capability 缺失或完整
自洽但未获 historical authorization 是 `MIGRATION_EVIDENCE_RECOVERY_REQUIRED`；已 registered stored object/header/index
存在而 bytes/digest/shape/linkage/reservation mismatch 是 `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`，corrupt 优先。
该优先级必须以 two-pass ALL-history 实现：第一遍对全部 registered lineages/generations/bundles 完成 deterministic
filesystem+C3 structural replay，收集任一 stored contradiction；发现任何 corrupt 就在完整有界 structural pass 后返回 corrupt，
不能因更早遇到 absent/unauthorized 就提前返回。只有全局 structural pass 无 corrupt，第二遍才运行全部 required historical
authorization/capability recovery，并把 absent 或 unauthorized 汇总为 recovery-required。不得用 traversal order 改变结果。

root aggregate 将 objects physical usage 只计一次；journal component 对所有 registered generations 聚合各自重算且验证过的
完整 `reserved_journal_bytes`，不因 current filtering、实际已写 records 或 terminal 而缩小。LineageIndex component exact
等于全部 index files 的 no-follow physical actual bytes，加上每个 generation 经 strict replay 证明尚未消费的 planned index
reservation；已 durable 的 index frame 只能从其 generation 的 planned index reservation exact debit 一次，不能把同一 frame
同时计入 physical actual 和未消费 reservation，也不能只计其一。实现不得把 combined `reserved_bytes` 整体再与上述
journal/index components 相加；必须先按 same verifier inputs 重算 split，checked-subtract 已消费部分，再 checked-add，
以同时拒绝 double count、under-count、overflow 与 two-process overcommit。

index reservation ownership 还必须按 generation/frame 固定：`GenerationSuperseded(A→B)` 只 debit A 已验证 reservation 中的
`possible-supersede` frame budget；紧邻的 `GenerationReserved(B)`、后续 `GenerationActivated(B)`、B checkpoints 与未来 B
supersession 分别只 debit B reservation 对应的 reserve/activate/checkpoint/possible-supersede budgets。nested planned B body
不把 B frame debit 给 A，物理 frame bytes 也不得同时 debit 两个 generation。strict replay 对每个 durable frame exact 选择一个
owner/budget slot，重复、缺 slot、顺序不符或 consumption 超过 planned split 一律 corrupt；尚未 durable 的 slot 才保留在该
generation unconsumed index reservation 中。

quota preflight 成功后，上述 composite binder 才可 mint sealed `AdmissionPermit`。permit exact 绑定 candidate runtime object、candidate
decision-recovery object、target LineageIndex replay state/tail、完整 aggregate quota reservation、planned
`GenerationReserved` canonical bytes/digest 与当前 epoch/revision；target absent 时，`VerifiedAdmissionPlan` 还必须与 inventory
的 typed `target_absent` fact exact cross-bind 同一 target digest。唯一允许的状态序列是：可选 create/reuse target → publish runtime → bind runtime
publication → publish recovery → bind recovery publication → seal reserve-ready authority；每一步必须 consume 前一 revision
和 permit。每个 publish transition 还必须 consume 旧 `AdmissionInventory`，完成后从仍持有的 lease 对实际 object set
重新闭合验证，返回包含更新 object count/bytes/digests 的 revision+1 `AdmissionInventory`，再与 revision+1 permit
cross-bind；第二次 publish 绝不能复用第一次 publish 前的 inventory/`Scan`。每个 bind/seal transition 同样 consume 旧
inventory/permit 并返回同 epoch、revision+1 的成对 next inventory/permit，跳步、倒序、重复、candidate/object/index swap
或跨 permit reuse 均拒绝。这里的
最后一步只产生 package-private、内存中的 one-shot reserve-ready authority，不 append index、不形成 durable reservation；它
exact 保留 planned bytes、target index tail、quota 与两个 bound publications，供后续 receipt/supersession slice 使用。bind
只把 verified candidate exact 绑定到 evidencefs `Publication`，不 mint typed receipt。任何 create/write/link/rename/
append/sync/response-lost 的 outcome 无法证明 complete+durable 时，整条 `AdmissionPermit`、`AdmissionInventory` 与
`AdmissionLease` admission authority 全部 unknown-invalidated，释放 locks 后只能重新 `AcquireAdmission` 并 full scan/replay；
不得沿用旧 revision。已 durable 的 content object 可在 reopen 后按 full verification 复用，但它本身不能自动 mint receipt、
permit 或 reservation。

可选首步只能是 permit 专用 `CreateTargetLineage` transition。它 consume 当前 evidencefs mutation token、inventory 与 permit：
target absent 时，在仍持有 root + 全部 existing lineage locks 的同一 `AdmissionLease` 内，按 durable registration protocol
创建 target lineage directory/index/lock，逐 barrier sync 后 nonblocking try-lock target；target 已存在时必须 no-follow 验证并
复用其 canonical lock，禁止创建第二份。成功后重扫/重验 target 和完整 registered set，返回包含 target file identities/bytes、
更新 cardinality/full-set digest 的 revision+1 inventory/permit；随后 publish transitions 只能消费这个 next pair。任何 create/
sync/try-lock outcome unknown 都使整个 epoch/lease admission authority 失效；busy 必须 release-all 后重新 acquisition。不得在
初始 scan 内创建、不得由 migration 在 scan 后另建、不得让 target 未锁或不在 updated full-set digest 中进入 publish/seal，
也不得用第二把 lineage handle 拼接 inventory。若 target 原本存在，该 verify/reuse transition仍 consume/advance revision，避免
present/absent TOCTOU。

所有 admission mutation API 只返回 closed `AdmissionTransitionResult`：

- `pre_mutation_failure`：尚未尝试任何 mutation，返回稳定错误；旧 authority 仅在 error contract 显式允许时仍可关闭，绝不
  自动 advance；
- `unknown`：只保留 bounded diagnosis
  `(candidate_kind, candidate_sequence, previous_revision, candidate_digest)`，authority 必须为 null；
- `durable`：只能返回同 lease/epoch、revision+1 且包含 mutation 后实际 object/index state 的 next inventory/permit authority。

一旦 create/write/link/rename/append/sync 已尝试，context cancellation、limit observation、close/response error 或无法证明
complete+durable 的任何结果都只能是 `unknown` 并 invalidate lease admission、inventory、permit 与 mutation token；不得
降格为 pre-mutation limit/context，也不得携带 next authority。已 registered stored bytes/shape/linkage mismatch 的 corrupt
优先级仍高于 recovery-required，但 mutation 后同样不复活 authority。`Open`/full `AcquireAdmission` 后的 strict full replay 是
唯一 reconcile path；caller 不能按 diagnosis 猜测或重试旧 candidate。

本 admission slice 到 sealed reserve-ready authority 即结束，typed receipt binder、index append、runner phase/order、
DB/ledger/`Connect`/SQL/`Commit` 与 cloud wiring 全部继续 `MIGRATION_PROJECTION_NOT_IMPLEMENTED`，测试调用计数必须为零；
receipt binder 之后也只能消费同 permit 链已 bind 的 publications 与 reserve-ready authority。trusted mount authority 未实现期间 production constructor 仍在
任何 probe/mutation 前拒绝。本切片不新增任何 C3 wire field/record/union，所需 epoch、revision、split accounting、
permit state 与 historical capabilities 全是 opaque runtime state；不得以此宣称 production enablement、Platform RC 或 Gate
closure。

新 object 先写入同目录 no-follow/O_EXCL
temporary regular file；temp basename 固定为 `.tmp-<128-bit CSPRNG lower-hex>`，只能在该 object final path 的同一
directory 以 `O_NOFOLLOW|O_CREAT|O_EXCL`、mode `0600` 创建。写完后重验 size/outer SHA、`fdatasync`，再用
`renameat2(RENAME_NOREPLACE)` 或同目录 `linkat` no-replace + `unlink` 的等价序列 publish，并 `fsync` objects
directory；不得用可覆盖已有 target 的 rename。写入 runtime object 时重验 outer size/SHA，写入
recovery object 时重验 recovery size/SHA 与 4 MiB 单件上限。并发 publish 发现 final 已存在时，必须
no-follow reopen 并完整重验
owner/mode/link-count/regular-file/size/digest 后才丢弃本调用拥有的 temp；验证失败保留 fail-closed error。crash temp
只能在持有 store-writer lock、证明 basename/owner/regular-file/link-count 且它不被任何 active writer 持有后清理；
不得自动删除 final object。existing final object 同样必须完整重验后复用。object
和 objects directory 至多 `0600/0700`，partial/temp 不能被当作 final。只有 object durable 后才能创建 header；这样
`sql_path + start/end offset + statement digest` 长期引用 verified bytes，而不是引用调用方之后可能消失或变化的
artifact source；decision-recovery receipt 也因 header digest/size 能在重启后恢复 exact verifier input。

`objects` tree 的 registration grammar 在首次 `Open` 前固定关闭：configured root 必须预置 `objects` directory，
并且只允许另有 `lineages` directory 与 `lineages.lock` regular file；后两者缺失时继续由下文既有 registration state
machine 按 file/directory durability protocol 创建，缺失本身不是 object-store admission failure。root 中出现其他 entry、
symlink/special/hardlink，统一在 root admission 返回 `MIGRATION_EVIDENCE_JOURNAL_FAILED`，不得忽略或递归清理。
`objects` 内只能有预置的 `sha256` directory；`sha256` 缺失或出现其他 entry 都是同一 admission failure。root、`objects`、`objects/sha256`
必须在 `Open` 中逐 component `O_NOFOLLOW` 打开，exact 验证 runner UID、directory mode 至多 `0700`，并证明三者
`st_dev` 相同；`objects` 或 `sha256` 缺失必须拒绝，不得由 runner 创建。root 中尚未初始化的 `lineages`/
`lineages.lock` 仍只按下文 registration state machine 创建；这不授权创建 content-store directories。

`objects/sha256` 的 entry grammar 仅允许两类 basename：final 为 exact `64lowerhex`；temporary 为 exact
`.tmp-<32lowerhex>`，其中 32 hex 必须来自每次 create 独立的 128-bit CSPRNG，不能使用时间、PID、计数器、digest
prefix 或 caller string。final 名必须与全量 SHA-256 完全相等；registered digest/size/kind 与 final bytes 不一致是
`MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。malformed/unknown basename、symlink、device/socket/FIFO、owner/mode/link-count
错误或跨 device object 是 `MIGRATION_EVIDENCE_JOURNAL_FAILED`；valid-name final 的 full digest/size mismatch、同 digest
却与 durable registration 不一致则是 corrupt。final hardlink 因 `st_nlink != 1` 拒绝；temp 也必须是 owner 匹配、
mode 至多 `0600`、link-count=1、same-device regular file。

每次 root admission 对 temp 做 bounded inclusive scan：最多 `64` 个 temp、每 temp physical size 最多 `64 MiB`、
全部 temp physical bytes 总计最多 `4 GiB`；exact maximum 合法，第 65 个、任一 `64 MiB+1` 或 cumulative
`4 GiB+1` 在读取 object bytes 或进入 DB 前返回 `MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`。计数/累计必须使用
overflow-safe `value > max-current` 比较，不能 wrap。超过 limits 不得先删除再通过 admission。能证明由当前持有
store-writer lock 的 active publication handle exact 引用的 temp 保留；无法证明 active/unreferenced 的 temp 同样保守
计入 count/bytes。这里的 store-writer lock **exactly 是** `<root>/lineages.lock` 上既有的 root-wide lock，不新增第二个
lock file、path 或 lock domain；object scan、full root quota bind、publish、reserve 和 activate 必须连续消费同一个仍 active
的 root-wide lease。只有在该 lease 下、完整证明 basename/owner/type/link-count/same-device 且不被
任何 active writer/reference 持有后，maintenance 才可清理 unreferenced temp；`Open` 不得猜测 crash ownership，
不得删除 final，也不得把 temp 作为 durable object/receipt authority。

`publish` 一旦尝试创建/write/link/rename/sync，任何无法证明 durable 成功的结果都进入 unknown：session/cursor/root
admission authority 失效，只能 close 后重新 `Open` 并重新 bounded scan/full verify。超限若在任何 mutation 前由同一
root-wide lock 内 exact 证明，才映射 limit-exceeded；mutation 后即使随后观察到 quota 超限也不得把 unknown 降格为
普通 limit。sealed durable-object authority 只能由 content store 在 write-complete、full digest/size verify、`fdatasync`、
atomic no-replace publish、objects-directory `fsync`、no-follow reopen 及再次 full digest/size/owner/mode/link-count/kind/
store-identity verify全部成功后，从私有 publication result mint。`EEXIST` reuse 也必须执行同一套 reopen/full verify；
wire decode、普通 caller、loose Digest 或自洽 same-package literal 均不能升格。durable `Publication` 的 concrete type、
constructor、store identity、active-lease binding 和 validity check 由 sibling
`services/control-plane/internal/evidencefs` package 私有持有；`migration` 只能消费其 opaque result 并与 owned runtime/
recovery artifact 的 kind/digest/size/owner 交叉绑定。`migration` 内的 private seal、registry 或 same-package literal 都不能
mint、repair 或替代该 authority，两个 typed receipt 也必须保留该 opaque `Publication`，不能降格为 digest summary。当前
impl-2 receipt binder 在 full-root `AdmissionPermit` + sealed reserve-ready authority 路径闭合前固定
`MIGRATION_PROJECTION_NOT_IMPLEMENTED`，不得生成伪
durable receipt。

crash/reopen 另有一个与新 admission 明确隔离的 recovery-only 双权威路径。migration strict replay 从 all-lineage
`AdmissionInventory` 验证 stored `GenerationSuperseded.planned_generation_reserved`（或 durable matching
`GenerationReserved`/planned header）后 mint opaque `VerifiedHistoricalRegistrationPlan`。migration recovery composite binder
必须 cross-bind 该 plan 与同 epoch/revision 的 evidencefs registration-view token，并生成不可拆的 recovery request；request
内部保留 evidencefs 自有 token 与 exact frame/object view identities，但不把 C3 proof、bool、digest 或 raw assertion交给
evidencefs 充当 authority。evidencefs 只 consume/验证自己的 one-shot registration token、lease/epoch/revision 与 request 中
不可替换的 frame/object view identities，再对 registered final object no-follow reopen，验证 owner/mode/type/link-count/store
identity、exact size 与全量 SHA-256，mint opaque `RegisteredPublication`；它绝不观察或认可 C3 plan，并且不能从 basename、
unreferenced/self-consistent object、caller bool/digest/path 或旧 `Publication` 恢复。migration 随后只能用同一
`VerifiedHistoricalRegistrationPlan`、stored registration 与同一个 historical verifier 恢复的 artifact authority，cross-bind
exact kind/digest/size/owner 后 mint recovery-only typed receipt；kind swap、frame swap、current-only authorization 或
registration mismatch 均拒绝。
`RegisteredPublication`/recovery receipt 只允许恢复 `superseded_pending_reservation` 或验证已 registered reserved/header，不能
进入新 admission、不能 publish candidate、不能自动 mint `AdmissionPermit`/reserve-ready authority，也不能替代 quota permit
或 durable reservation。这样 crash 落在 superseded durable、紧邻 reserved write/response 未确认的窗口时，`Open` 可以从
durable registration 重建所需 receipts，再按 exact candidate replay/reconcile reserved；unreferenced durable objects 仍只计 quota，
绝不自证 receipt。

journal wire record 是以下 **exact closed union**；不存在额外 record kind、unknown member 或开放扩展点：

```text
EvidenceRecord =
  JournalHeader |
  StatementIntent |
  StatementIntermediateEvidence |
  CommitIntent |
  AttemptTerminalState |
  AmbiguousResolutionState

JournalHeader {
  format_version: "cloud-agents-platform-evidence-journal/v1"
  journal_identity_digest: Digest
  release_trust_decision_digest: Digest
  runner_projection_decision_digest: Digest
  execution_lineage_digest: Digest
  outer_artifact_digest: Digest
  outer_artifact_size_bytes: uint64
  decision_recovery_artifact_sha256: Digest
  decision_recovery_artifact_size_bytes: uint64
  manifest_digest: Digest
  runner_release_digest: Digest
  schema_bundle_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
  segment_index: uint32
  previous_segment_record_digest: Digest | null
  limits_profile: "cloud-agents-platform-evidence-journal-limits/v1"
  quota_reservation_digest: Digest
  reserved_records: uint64
  reserved_bytes: uint64
  reserved_segments: uint32
}

StatementIntent {
  schema_bundle_digest: Digest
  catalog_contract_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
  migration_id: string
  attempt_index: uint32
  statement_index: uint32
  sql_path: string
  sql_artifact_sha256: Digest
  sql_artifact_size_bytes: uint64
  start_offset: uint64
  end_offset: uint64
  statement_sha256: Digest
  classification: StatementClassification
  previous_attempt_terminal_digest: Digest | null
  previous_intermediate_state_digest: Digest | null
  expected_transition_digest: Digest
  authority_before_digest: Digest
  catalog_before_digest: Digest
  authority_before_result: ProjectionResultEvidence
  catalog_before_result: ProjectionResultEvidence
}

ProjectionResultEvidence {
  digest: Digest
  metadata: ProjectionMetadata
}

StatementIntermediateEvidence {
  state: StatementIntermediateState
  authority_before_result: ProjectionResultEvidence
  catalog_before_result: ProjectionResultEvidence
  authority_after_result: ProjectionResultEvidence
  catalog_after_result: ProjectionResultEvidence
  preledger_authority_result: ProjectionResultEvidence | null
  preledger_catalog_result: ProjectionResultEvidence | null
}

CommitIntent {
  schema_bundle_digest: Digest
  catalog_contract_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
  migration_id: string
  attempt_index: uint32
  previous_attempt_terminal_digest: Digest | null
  attempt_predecessor_catalog_digest: Digest
  last_intermediate_state_digest: Digest
  expected_ledger_length: uint32
  expected_ledger_head: string
  ledger_row: {
    migration_id: string
    migration_name: string
    predecessor_id: string | null
    phase: string
    schema_from: string
    schema_to: string
    compatible_binary_min: string
    compatible_binary_max: string
    sql_path: string
    sql_size_bytes: uint64
    sql_sha256: Digest
    bundle_digest: Digest
    transaction_mode: string
    reentrancy: string
    rollback_boundary: string
    requires_live_instance_preflight: bool
    requires_pitr_preflight: bool
  }
}

AmbiguousResolutionState {
  schema_bundle_digest: Digest
  catalog_contract_digest: Digest
  authority_profile_digest: Digest
  authority_binding_digest: Digest
  migration_id: string
  attempt_index: uint32
  unresolved_terminal_digest: Digest
  outcome: "resolved_committed" | "resolved_pending" | "resolved_divergent"
  reconcile_result: "exact_committed" | "exact_pending" | "divergent"
  stable_error_code: "MIGRATION_AMBIGUOUS_COMMIT" |
                     "MIGRATION_UNTRUSTED" |
                     "MIGRATION_EVIDENCE_JOURNAL_FAILED" |
                     "MIGRATION_EVIDENCE_RECOVERY_REQUIRED" |
                     "MIGRATION_CONTEXT_CANCELED" |
                     "MIGRATION_DEADLINE_EXCEEDED"
  resolution_digest: Digest
}
```

`quota_reservation_digest` 不是对 `GenerationReserved` 或 header 的递归 hash；它只使用以下 flat object：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-evidence-quota-reservation/v1",
  "limits_profile": "cloud-agents-platform-evidence-journal-limits/v1",
  "execution_lineage_digest": ...,
  "journal_identity_digest": ...,
  "runner_projection_decision_digest": ...,
  "schema_bundle_digest": ...,
  "reserved_records": ...,
  "reserved_bytes": ...,
  "reserved_segments": ...,
  "continuation": LineageContinuationContext | null
}))
```

输入明确排除 `planned_segment0_header`、`expected_segment0_header_digest`、
LineageIndex/reserved/header frame digests 和 `generation_reserved_record_digest`。唯一计算顺序是：先算 quota
digest；用它、reservation counts、current verified runtime/recovery object digest+size 和全部 decision identities 构造
`planned_segment0_header: JournalHeader`；将其放入 exact
`EvidenceFrame{sequence=0,previous_record_digest=null,record_kind="header",record=planned_segment0_header}` 重算
segment-0 `record_digest`；把 planned header 和该 digest 一起写入 `GenerationReserved`；再算 reserved
`LineageIndexFrame.record_digest`；最后
`GenerationActivated` exact 引用 segment-0 与 reserved 两个 record digest。durable append 顺序仍是 reserved →
segment-0/header → activated；计算顺序不授权跳过任何 durability barrier。

本节及第 6 节凡写“domain 对 object hash”，一律表示把 `domain` 作为被 hash object 的同层 closed member，并移除
该 object 的 self-digest field；禁止把业务字段嵌套到额外 wrapper member 或使用 byte-prefix domain。
TS/Go same-bits fixtures 必须直接比较这些 flat RFC8785 bytes。

`journal_identity_digest` 是
`SHA-256(RFC8785({"domain":"cloud-agents-platform-evidence-journal-identity/v1",
"release_trust_decision_digest":...,"runner_projection_decision_digest":...,
"outer_artifact_digest":...,"outer_artifact_size_bytes":...,"decision_recovery_artifact_sha256":...,
"decision_recovery_artifact_size_bytes":...,"schema_bundle_digest":...,
"authority_profile_digest":...,
"authority_binding_digest":...}))`。它是一次 verified run 的跨-entry journal identity；同一 schema bundle 的所有
entry、segment 和 attempt 必须使用同一 identity，不得在连接/读取 ledger 前猜测 pending migration。identity 不得加入
timestamp、PID、path 或随机 nonce；它是随 combined decision 轮换的 **generation identity**，不是跨轮换 lineage。

`LineageIndex` 是 `EvidenceSink.Open` 内部 ABI，不新增 public caller surface。路径固定为
`<root>/lineages/<execution_lineage_digest hex>/index.caj`，单文件、永不 rotate；root-wide lock 固定为
`<root>/lineages.lock`，per-lineage writer lock 固定为同 lineage directory 下 `writer.lock`。on-disk closed frame/profile
固定为：

```text
LineageIndexFrame {
  format_version: "cloud-agents-platform-lineage-index-frame/v1"
  sequence: uint64
  previous_record_digest: Digest | null
  record_kind: "header" | "generation_reserved" | "generation_activated" |
               "generation_checkpoint" | "generation_superseded"
  record: LineageIndexRecord
  record_digest: Digest
}

LineageIndexRecord = LineageIndexHeader | GenerationReserved |
                     GenerationActivated | GenerationCheckpoint |
                     GenerationSuperseded

LineageIndexHeader {
  format_version: "cloud-agents-platform-lineage-index/v1"
  execution_lineage_digest: Digest
  deployment_id: string
  expected_database_identity: { database_name: string }
  repository_identity: string
  limits_profile: "cloud-agents-platform-lineage-index-limits/v1"
}

LineageContinuationIdentity {
  start_action: "begin_first_attempt_next_entry" | "begin_next_attempt"
  migration_id: string
  attempt_index: uint32
  previous_attempt: "null" | "owned_old_terminal"
}

LineageContinuationContext {
  start_action: "begin_first_attempt_next_entry" | "begin_next_attempt"
  migration_id: string
  attempt_index: uint32
  previous_attempt_terminal_digest: Digest | null
  source_journal_identity_digest: Digest
  source_checkpoint_record_digest: Digest
  source_terminal_digest: Digest
}

GenerationReserved {
  execution_lineage_digest: Digest
  journal_identity_digest: Digest
  runner_projection_decision_digest: Digest
  schema_bundle_digest: Digest
  quota_reservation_digest: Digest
  reserved_records: uint64
  reserved_bytes: uint64
  reserved_segments: uint32
  planned_segment0_header: JournalHeader
  expected_segment0_header_digest: Digest
  continuation: LineageContinuationContext | null
}

GenerationActivated {
  execution_lineage_digest: Digest
  journal_identity_digest: Digest
  runner_projection_decision_digest: Digest
  schema_bundle_digest: Digest
  quota_reservation_digest: Digest
  generation_reserved_record_digest: Digest
  segment0_header_digest: Digest
  initial_journal_tail_digest: Digest
}

GenerationCheckpoint {
  execution_lineage_digest: Digest
  journal_identity_digest: Digest
  runner_projection_decision_digest: Digest
  schema_bundle_digest: Digest
  journal_next_sequence: uint64
  journal_tail_digest: Digest
  recovery_state: "brand_new" | "brand_new_inherited" | "completed" |
                  "dangling_statement_intent" | "dangling_intermediate" |
                  "dangling_commit_intent" | "ambiguous_unresolved" |
                  "terminal" | "divergent"
  migration_id: string | null
  attempt_index: uint32 | null
  last_statement_intent_record_digest: Digest | null
  last_intermediate_evidence_record_digest: Digest | null
  last_commit_intent_record_digest: Digest | null
  last_terminal_digest: Digest | null
  last_resolution_digest: Digest | null
  previous_attempt_terminal_digest: Digest | null
  last_intermediate_state_digest: Digest | null
  previous_checkpoint_record_digest: Digest | null
}

GenerationSuperseded {
  execution_lineage_digest: Digest
  old_journal_identity_digest: Digest
  old_runner_projection_decision_digest: Digest
  old_schema_bundle_digest: Digest
  old_checkpoint_record_digest: Digest | null
  old_activation_record_digest: Digest | null
  old_initial_journal_tail_digest: Digest | null
  lineage_supersession_authority_digest: Digest
  outcome: "exact_committed_bundle_complete" |
           "exact_committed_continue_successor" |
           "precommit_aborted_retryable" | "exact_pending" |
           "resolved_pending" | "confirmed_abort_terminal" |
           "terminal_failure" | "divergent_terminal" |
           "activated_no_migration_progress"
  planned_generation_reserved: GenerationReserved | null
}
```

`GenerationReserved.planned_segment0_header` 必须是 segment 0 header：`segment_index=0`、
`previous_segment_record_digest=null`，其 lineage/journal/release+runner decision/schema/authority/quota/counts 必须与
reserved 的并列字段 exact，其 outer runtime 与 decision-recovery object digest/size 必须与 current verified
receipts exact。`expected_segment0_header_digest` 必须每次从上述 exact sequence-0 frame 重算，不接受
caller digest。实际写入 segment-0 的 `JournalHeader` 必须与 planned header byte-canonical exact，否则 index
corrupt。

`reserved_no_header` reopen 只能从 durable nested planned header 恢复 runtime/recovery object digest+size、no-follow 重验
两个 object receipts，再使用同一 planned bytes 创建 segment 0；不得依赖丢失的 caller/session state。
`GenerationSuperseded.planned_generation_reserved` 非 null 时也完整覆盖 nested planned header。quota digest 不含
planned header/frame/reserved，planned header 不含 reserved/index digest，因此保持 quota → planned header/frame digest →
reserved → activated 的单向无环链。

frame bytes 固定为 `uint64-big-endian length || RFC8785(canonical LineageIndexFrame)`，没有 padding。
`record_digest` 固定为以下 flat domain object，不含自身：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-lineage-index-record/v1",
  "format_version": LineageIndexFrame.format_version,
  "sequence": LineageIndexFrame.sequence,
  "previous_record_digest": LineageIndexFrame.previous_record_digest,
  "record_kind": LineageIndexFrame.record_kind,
  "record": LineageIndexFrame.record
}))
```

首 record 必须是 header、sequence=0、previous=null；
之后 sequence 连续且 previous exact 引用前一 record digest；`record_kind` 与 union body 必须一一对应。header constituent 必须与第 5.4.1 节 lineage formula
重算 same-bits；每个 generation record 都 exact 绑定 lineage、generation journal identity、combined decision、schema
bundle、reservation/header/tail/state，superseded 还绑定 verified authority 与 old/new generation/outcome。index 禁止
secret、path、DSN、host、credential、raw SQL、raw cause 或 driver message。
`GenerationSuperseded` 必须通过第 5.4.1 节的统一 closed matrix：
`exact_committed_bundle_complete|confirmed_abort_terminal|terminal_failure|divergent_terminal` 要求
`planned_generation_reserved=null`；
`exact_committed_continue_successor|precommit_aborted_retryable|exact_pending|resolved_pending|
activated_no_migration_progress` 要求完整 planned body 非 null。后续 `GenerationReserved` body 必须与该
planned body byte-canonical exact；不能只比较 journal/decision/schema 三个 digest，也不能重新猜
quota/header/limits。普通 outcome 只能设置 non-null old checkpoint 且 activation/initial-tail 两字段为
null；`activated_no_migration_progress` 只能设置 checkpoint=null 且 activation/initial-tail 两字段非 null。
其他组合拒绝。

`GenerationReserved.continuation` 是 closed cross-generation start authority：

- continuation=null 只允许两种互斥情形：没有 preceding superseded、且同 lineage 不存在待继承
  state 的 true brand-new generation；或 immediately preceding superseded outcome 为
  `activated_no_migration_progress`、old continuation 也为 null 的 byte-exact carry。后者必须是
  `brand_new_inherited`，不是 true brand-new；
- `exact_committed_continue_successor` 只能使用 `begin_first_attempt_next_entry`，migration 必须是 verified current
  candidate 中 old checkpoint exact ledger prefix 的下一 entry，`attempt_index=1`、
  `previous_attempt_terminal_digest=null`；
- `precommit_aborted_retryable|exact_pending|resolved_pending` 只能使用 `begin_next_attempt`，migration 必须与旧 attempt
  同 entry，`attempt_index=old attempt_index+1<=max_attempts`，previous digest 必须 exact 等于旧 generation terminal；
- `activated_no_migration_progress` 必须 byte-canonical exact 继承 old `GenerationReserved.continuation`；它既不能
  把 null 改成 non-null，也不能把 non-null 改成 null 或重算 migration/attempt/source digests；
- 除 `activated_no_migration_progress` 外，non-null continuation 的 `source_journal_identity_digest` 必须 exact 等于
  immediately preceding superseded 的 `old_journal_identity_digest`，`source_checkpoint_record_digest` 必须 exact
  等于其 `old_checkpoint_record_digest`，`source_terminal_digest` 必须 exact 等于 binder authority 与该 old
  checkpoint 共同引用的 terminal；`activated_no_migration_progress` 的 non-null continuation 仍指向创建 old
  activated generation 时已验证的更早 source checkpoint/terminal，必须与 old `GenerationReserved`
  byte-exact，不得改为当前为 null 的 checkpoint；superseded record 的
  `lineage_supersession_authority_digest` 必须 exact 等于 binder authority，authority 中的 continuation 又必须与 reserved
  continuation exact。stored linkage
  自相矛盾是 `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`；当前 binding/authority 无法验证是
  `MIGRATION_EVIDENCE_RECOVERY_REQUIRED`。不得把 attempt 重置为 1、跳过 entry 或盲重放旧 SQL。

`GenerationSuperseded.record_digest` 覆盖完整 nested planned reservation 及 continuation。planned path 的
`GenerationReserved` frame 必须是 superseded 后 **紧邻的下一 index frame**，且 body byte-canonical exact；正常
brand-new generation 是唯一可无 superseded 前驱的 reserved 例外。`GenerationActivated.generation_reserved_record_digest`
必须 exact 引用该 reserved frame digest，activated 的 generation/decision/schema/quota 也必须与 reserved exact，防止
activation 与 continuation 脱钩。continuation 只含 bounded migration identity/digest/attempt，不得包含 secret/path/raw
cause。

LineageIndex 固定 inclusive maxima：单 on-disk frame 256 KiB、header 32 KiB、reserved/activated 各 64 KiB、checkpoint
16 KiB、superseded 128 KiB、每 index 16,384 records、单 index file 16 MiB、每 root 64 lineage indexes 且 index bytes 总计
1 GiB；exact maximum 合法，不可由 env/CLI/DSN/GUC 覆盖。prefix、frame header 与 record bytes 全计入 limits；不
rotation、不自动 GC。index reservation/count/bytes 必须纳入 root quota/admission，任何超限都在 Connect 前返回
`MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`。

index append 与主 journal 共用第 5.4.4 节 `AppendResult` durability 语义：只要一次 append 已尝试，zero-byte 可统一按
unknown；任一首字节已触碰但 short/partial write、write error、sync/response error 且未证明 complete+sync，都返回
unknown、旧 cursor 失效。replay 发现 byte-identical complete candidate 后必须再次成功 `fdatasync` 及必要 directory
`fsync` 才承认 durable；仅 index 尾部 torn frame 可截断到 last durable boundary 并 `fdatasync`，不得补写 missing
bytes；middle corruption、同 sequence different complete frame 或 chain mismatch 一律
`MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。

registration 状态机固定为：configured root 必须预先存在并通过 owner/mode/no-follow/filesystem probe，runner 不得
创建它。single-lineage `AcquireRootThenTryLineage(context, lineage)` 只服务不需要 full-root authority 的 normal
open/heal/handoff：它先取 root-wide
lock，然后只对 per-lineage lock 做**nonblocking try**；若 lineage busy，必须立即释放 root-wide lock，再按
bounded/context-aware backoff 重试。任何代码都不得持有 root-wide lock 等待 lineage lock，也不得先持
lineage 后等 root。任何 full-root inventory/quota/publish/reserve/activate 必须改用 `Store.AcquireAdmission`；两种 helper
共享 root-first、canonical lineage order、nonblocking try 与 busy-release-all-retry 规则，但 single-lineage helper 不能 mint
full-root authority。root-wide lock 与上文 store-writer lock 是同一个 `<root>/lineages.lock` flock lease；不存在可与其并行取得的
第二把 content-store writer lock。任何 object `Scan` 或 `Publication` 若不是由当前 active lease 产生，或 lease 已释放/
unknown-invalidated，都不能进入 quota binder、receipt binder 或后续 reserve。

首次创建 `<root>/lineages` 后必须 `fsync(root)`，创建 lineage directory 后必须
`fsync(<root>/lineages)`，创建并同步 lineage `writer.lock` 后必须在同一 helper 中 try-acquire；busy 时仍按
上述规则释放 root 重试。brand-new lineage 先 durable
创建/同步 LineageIndex header，然后 durable append `GenerationReserved`；再创建 deterministic generation journal
directory 并 `fsync` 其 lineage parent。写 segment-0/header 时必须 `fdatasync` segment file、`fsync` journal directory，
且 generation directory entry 已由 lineage-parent `fsync` 覆盖；reserved expected digest、activated header digest 与
activated initial tail digest 三者都必须 exact 等于该 segment-0 `EvidenceFrame.record_digest`；activated
`generation_reserved_record_digest` 还必须 exact 等于前述 reserved index frame digest；再 durable append
`GenerationActivated`。只有上述 file/directory barriers 全成功且 activated durable 后才
允许 `Connect`。root-wide lock 覆盖全 root discovery、quota revalidation、object publish、reserve、journal/header
create 和 activate；该临界区结束后立即释放 root-wide lock，保留 per-lineage writer lock 进入 normal
run/reconcile。正常 run 永不持有 root-wide lock。所有 index append 在 per-lineage lock 下串行；
checkpoint 只需该 per-lineage lock。reopen 时：reserved 且无
header 是 pre-DB reservation，可按 expected digest 创建/activate；reserved + valid matching header 但无 activated 时，
append activated 后方可 DB；activated 但 generation directory、segment-0 或 header missing/mismatch 是 index corrupt。正常 activation protocol 已在 index
record 前完成 file 与两级 directory-entry barriers，因此不得把 activated-but-missing 解释为普通掉电可恢复状态。
no-follow root scan 必须枚举所有
journal-shaped directories；任何未登记 journal、未知 generation 或 index 未覆盖的 journal dir 都是 corrupt，不能忽略。

activation 已绑定 segment-0/header；此后每个主 journal record 必须先 durable，再 append 对应该 tail 的
`GenerationCheckpoint`，两步共同构成一次 `AppendDurable` composite operation。顺序只能是 journal frame
write-complete → journal `fdatasync`/必要 directory `fsync` → byte-exact checkpoint index frame write-complete → index
`fdatasync`；只有两步都 durable 才返回 `outcome=durable` 和新 cursor。journal 已 durable 但 checkpoint append/sync/
response 失败或 unknown 时，**整个** `AppendDurable` 返回 `outcome=unknown`、`durable_cursor=null`，旧 cursor 与本次
owned record 永久失效，caller 不得继续数据库进度、重试同 record 或猜测 checkpoint。checkpoint 落后但 journal 是其
exact linear extension 时，`Open` 可在 DB 前 replay 并 append healing checkpoint。checkpoint 领先、指向不同 branch、
tail/state/link mismatch 或 journal 比 checkpoint 短一律 corrupt。supersession 前必须先固定 old generation 的
latest durable boundary：普通 generation 是 exact terminal/resolution 后的 `GenerationCheckpoint`；header-only
`activated_no_migration_progress` 是 `GenerationActivated` + segment-0 `JournalHeader`/initial tail，它不存在也不得伪造
checkpoint。旧 boundary 必须被 `VerifiedLineageSupersessionAuthority` exact 绑定。

`ReserveAndActivateSuccessor` 在取得该 durable boundary 后，必须先永久 invalidate 旧 journal cursor、journal
handle、generation handle 和 session recovery handle，再释放 generation writer lock 与 per-lineage writer lock。之后只能用
前述唯一 full-root acquisition 重取 admission lease，并从 durable bytes strict replay old boundary、recovery artifact、current
decision/authorization、continuation 和 root quota；不得沿用 handoff 前的 cursor/snapshot/receipt assertion。全部重验通过后，
本方法内 admission permit 的唯一 pre-durability 顺序必须显式为：

1. consume revision N publish/reuse B runtime final object，全量重验 SHA-256/size/store identity 与 durability barriers，返回 revision N+1；
2. consume N+1，把 exact verified B runtime candidate bind 到该 `Publication`，不 mint receipt，返回 N+2；
3. consume N+2 publish/reuse B decision-recovery final object并做同样 full verification，返回 N+3；
4. consume N+3，把 exact verified B recovery candidate bind 到该 `Publication`，不 mint receipt，返回 N+4；
5. consume N+4 seal package-private one-shot reserve-ready authority，返回 N+5；此步不 append index、不持久化 reservation。

typed receipt binder 与 index append 在本切片仍是 `MIGRATION_PROJECTION_NOT_IMPLEMENTED`/零调用；后续切片的 closed
authority graph 只能是：

- N+5 `ReserveReady.BindReceiptPair` 一次性 consume `ReserveReady`，在一次 atomic cross-bind 中同时 mint runtime/recovery
  两枚 typed receipts 与新 `ReceiptBoundReady`；不能让两个 binder 各消费同一 one-shot，失败不能留下半对 authority；
- brand-new consume `ReceiptBoundReady` durable append `GenerationReserved(B)`，再写 segment-0/header 并 append
  `GenerationActivated(B)`；这是唯一没有 preceding superseded 的 reserved 路径；
- successor consume `ReceiptBoundReady` durable append `GenerationSuperseded(A→B)`，只返回 `AdjacentReserveReady`；随后必须
  consume `AdjacentReserveReady` 紧邻 durable append byte-exact `GenerationReserved(B)`，再写 segment-0/header 并 append
  `GenerationActivated(B)`。

每个 transition 都返回新 authority 并永久 invalidate 旧 authority；unknown 不返回新 authority。两条路径都不得在 receipts
前 reserve；successor 不得跳过 superseded、把 reserved 移到其前面或在两条 index frames 间插入其他 frame，brand-new 不得
伪造 superseded。objects durable 但 superseded/reserve 尚未 append 时 crash，只能留下计入 root quota 的 unreferenced final
objects，不能自证 receipt。superseded durable 后 nested planned header 必须足以按前述 registration-only recovery 路径恢复
两个 B objects。任一 reacquire/replay/reverify/reauthorize/quota/append/sync 失败，旧 session 永久 invalid，只能由新
`Open` 恢复；successor 仍必须完整 reserve/header/activate，不得直接 `Connect`。

`LineageOpenState` 是 internal closed union：`ready|reserved_no_header|reserved_header_unactivated|index_stale_prefix|
supersession_required|superseded_pending_reservation|orphan_generation|corrupt`。`ready` 才可进入 normal run；两个
reserved state 与 `index_stale_prefix` 可按上述规则在 DB 前 deterministic heal；`supersession_required`
表示旧 generation 尚未 durable supersede。若 current signed recovery policy 授权该 exact
ancestor，`Open` 必须返回具有 `VerifiedRecoveryExecutionBindings` 的 ancestor session + owned snapshot，等待
显式 `Runner.Run`；若 stored chain 完整自洽但 current policy 不授权、signature/recovery capability 不可形成或 artifact
absent，才返回
`MIGRATION_EVIDENCE_RECOVERY_REQUIRED`。`superseded_pending_reservation` 只表示 superseded 已 durable、其非 null
planned reservation 尚未出现；planned B 不再要求仍是 current/unexpired decision。reopen 必须通过上述
`RecoverHistoricalSupersession(current C, A→B, A artifact/boundary, B runtime+recovery artifacts/typed receipts)` 取得
`VerifiedHistoricalSupersessionReceipt`，再且只能通过 full-root `Store.AcquireAdmission` + ALL-history two-pass replay 重验
root quota、nested planned reservation/header digest、continuation/source linkage、`RegisteredPublication` recovery receipts 和
receipt one-shot state，然后幂等 append byte-identical planned
`GenerationReserved`，再走 header/activate。A/B artifact absent、C policy 未 exact 授权两者或完整自洽 inputs 无法重建
current signature/recovery capability 返回
`MIGRATION_EVIDENCE_RECOVERY_REQUIRED`，quota 不足返回 `MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`；全部在 DB 前，
不得重算或猜测 planned body。已 registered A/B object/header/receipt 的 size/digest/canonical/shape mismatch、
reconstructed binding/authority body 与 stored chain 不 exact、orphan/corrupt 返回
`MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。单 journal
`RecoverySnapshot` 不承担该 lineage-level state。

`Open` 必须先完成上述 root scan，再扫描/replay 同 lineage index 登记的 **全部 registered generations、跨全部
registered schema-bundle digests**，并逐 generation 按 header 指向的 historical runtime object 与
decision-recovery artifact 恢复其 exact schema bundle/catalog set；不能只扫描 current candidate 能识别的 heads，也不能
用 current manifest 的 entry 列表过滤历史 registration。每个已登记 generation 的 header、artifact、schema bundle、
catalog descriptors 和 index chain 都必须 strict decode/replay；unknown registration 是 corrupt，known registration 但
current same-verifier policy 无法重新授权则 recovery-required。因此 strict-prefix successor bundle、新 combined decision、expiry 或 epoch 都不能隐藏 ancestor bundle 的
dangling/ambiguous/unresolved state。若任一旧 generation 是 `dangling_statement_intent`、`dangling_intermediate`、
`dangling_commit_intent`、`ambiguous_unresolved`，或存在尚未 exact 收敛的 ambiguous/pending recovery，缺少第 5.4.1 节
exact current recovery policy、historical decision 或 `VerifiedRecoveryExecutionBindings` 时必须在 DB 前返回
`MIGRATION_EVIDENCE_RECOVERY_REQUIRED`，且不得 reserve
新 generation。具备 execution bindings 时只能先显式 `Runner.Run` 收敛旧 snapshot 并 checkpoint，再
生成 supersession authority 并 durable append
superseded。只有 DB ledger/head/catalog 已达到 **当前 verified candidate 的 final entry/final catalog**，才允许
`exact_committed_bundle_complete`、planned reservation null 并 return success。旧 target entry exact committed、但当前
strict-prefix successor candidate 仍有 remaining entries 时必须
`exact_committed_continue_successor`，不得重放旧 entry SQL；它与 precommit `aborted_retryable` + exact predecessor、
`exact_pending`、`resolved_pending` 一样必须携带 non-null full planned reservation，在 superseded durable 后按
`superseded_pending_reservation` 创建新 generation，从 exact ledger prefix 的下一 pending entry 继续。
`confirmed_abort_terminal|terminal_failure` 必须 planned null 并 `return_failure`，不得开启 successor attempt；
`divergent_terminal` 永远 fail closed 且 planned null。lineage/index records 不保存或派生 DSN、host、port、credential。

`EvidenceSink.Open` 的返回语义是 closed XOR：成功时
`session!=null,snapshot!=null,error=nil`，且 session accessor 返回同一 owned snapshot；失败时
`session=null,snapshot=null,error!=nil`。可被 current policy 授权且
完成 historical revalidation 的 ancestor 是成功返回，其 `ActiveGeneration.kind=ancestor_recovery` 并持有
`VerifiedRecoveryExecutionBindings`；artifact absent、current policy 拒绝，或完整自洽 inputs 无法形成 same-verifier
recovery capability 时是 `session=null,snapshot=null + MIGRATION_EVIDENCE_RECOVERY_REQUIRED`。已 registered
artifact/header/receipt 的 stored size/digest/canonical/closed-shape mismatch 则是
`session=null,snapshot=null + MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。任何路径都永不允许 non-null session
与 non-null error 同时出现，也不得在 error 中泄露 artifact bytes 或 secret。

`StatementIntent`/`CommitIntent` 是 closed object，frame
digest 已提供 record identity，因此不得再增加自报 intent digest。`expected_ledger_length` 固定为
`entry_index + 1`，`expected_ledger_head` 和 `ledger_row.migration_id` 固定为 target entry ID；ledger row 还必须与即将
insert 的 exact typed row byte-for-byte 对应。`ledger_row` 包含现有 `LedgerRow` 除 observational `applied_at/applied_by`
外的全部 ledger-backed identity；这两个数据库生成/观察字段不得进入 commit-intent digest，reconciliation 也不得用
它们把 identity mismatch 判成成功。`attempt_predecessor_catalog_digest` 必须在 attempt 第一条 SQL 前由该 attempt
first statement 的 actual `catalog_before_digest` 固定，并同时 exact 等于 signed expected-transition 选择出的具体
predecessor branch；后续 statement 或 accepted predecessor union 的其他 branch 不能覆盖它。
`StatementIntent.sql_artifact_sha256/sql_artifact_size_bytes` 是 **inner SQL member** identity，必须 exact 等于
`StatementPlan` 以及 signed manifest/schema-bundle artifact descriptor；它们不得等于或冒充 outer
`VerifiedContentReceipt` 的 digest/size。runner 必须从 receipt 绑定的 outer object bytes 按 strict tar/member profile
重新定位 exact `sql_path`，对完整 inner member 重算 size/SHA-256，再验证 start/end 在 inner size 内且 statement bytes
digest exact；不能只凭 path、offset 或 outer receipt 信任 inner SQL。
`StatementIntent.authority_before_result/catalog_before_result` 必须是第 5.0 节 bounded/redacted evidence，且各自
`digest` exact 等于同 intent 的 `authority_before_digest/catalog_before_digest`。同 statement 的
`StatementIntermediateEvidence.authority_before_result/catalog_before_result` 必须与 intent 中对应 evidence
byte-canonical exact；这样 intent durable 后即使 SQL/after projection 前 crash，before metadata 仍完整可恢复，不能由
intermediate 事后补写或改变。
`StatementIntermediateEvidence` 是第 5.3 节“持久化 ProjectionResult digest/bounded metadata evidence”的 impl-3
closed evidence shape：四个
before/after result evidence 必填，其 digest 必须分别等于 state 的 authority/catalog before/after digest，metadata
必须是第 5.0 节 exact bounded/redacted shape、phase/index/scope/kind mapping 完全一致。非 final statement 的两个
preledger field 必须为 null；final statement 两者必填，分别记录 `statement_index=null` 的 migration-transaction
authority result 与 final `ProjectCatalog` result，并与 final after equality check 对齐。journal 不持久化 raw query row、
SQL、driver cause 或最大可达 8 MiB 的 typed projection canonical body；只有 projection digest 与 bounded/redacted
metadata 进入 evidence。typed projection 的 canonical digest 已进入 state，观察边界、query/row/byte counts、adapter、
verified subject 和 scope 由上述 metadata 完整保存。任一 evidence/result mapping mismatch 返回
`MIGRATION_INTERMEDIATE_STATE_MISMATCH`。
`resolution_digest` 使用 domain `cloud-agents-platform-ambiguous-resolution/v1` 对移除自身字段后的完整
`AmbiguousResolutionState` 做 RFC8785+SHA-256；outcome/result 必须一一对应。它只能引用同 migration/attempt 的
durable `ambiguous_unresolved` terminal，不能替换、删除或重写该 terminal。`resolved_pending` 后的新 attempt 仍以该
unresolved terminal digest 作为 `previous_attempt_terminal_digest`，journal frame chain 同时证明 resolution 发生在
新 attempt 之前。`AmbiguousResolutionState.stable_error_code` 必须 byte-for-byte 等于被引用 unresolved terminal 的
`StableFailureEvidence.code`/`stable_error_code`；resolution 不得重分类或覆盖该 code。

每个 frame 是 RFC8785 canonical closed object：

```text
EvidenceFrame {
  format_version: "cloud-agents-platform-evidence-journal-frame/v1"
  sequence: uint64
  previous_record_digest: Digest | null
  record_kind: "header" | "statement_intent" | "intermediate" |
               "commit_intent" | "attempt_terminal" | "ambiguous_resolution"
  record: EvidenceRecord
  record_digest: Digest
}
```

`record_digest` 固定为以下 flat domain object，且不含 `record_digest` 自身：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-evidence-journal-record/v1",
  "format_version": EvidenceFrame.format_version,
  "sequence": EvidenceFrame.sequence,
  "previous_record_digest": EvidenceFrame.previous_record_digest,
  "record_kind": EvidenceFrame.record_kind,
  "record": EvidenceFrame.record
}))
```

整个 journal 的首 frame 必须是 segment 0
header 且 previous 为 null，每个后续 segment 的首 frame 也必须是 header、previous 引用前一 segment final record；
其余 sequence 在 segment 间仍严格递增并引用前一 record digest。header exact 绑定 journal format、
release/combined decision digest、execution lineage、outer artifact、manifest、runner release、schema bundle、authority profile/binding、
decision-recovery artifact digest/size、segment index 和固定 limits；outer runtime 与 recovery artifact 必须分别与
durable content-addressed object receipt 及 verified decision exact match。segment 0 的
header previous segment 为 null；后续 segment header 还必须引用前一 segment 的 final record
digest。每个 attempt 的 intent/intermediate/terminal linkage 还必须满足第 6 节 chain，journal chain 不能替代它。

`AppendDurable` 必须先验证 typed record、canonical bytes、sequence/previous digest 和 limits，再以
`uint64-big-endian length || canonical frame bytes` 完整追加。所有 frame/record/segment byte limits 都包含 8-byte
length prefix、完整 frame headers 与 canonical record bytes，不存在 alignment/padding 豁免。只有 file `fdatasync`
成功、新建/rotate segment 所需 parent directory `fsync` 成功，且对应 `GenerationCheckpoint` 也按上文 composite
protocol durable 后，才返回 `AppendResult{outcome=durable,durable_cursor!=null}`；rotate 只允许发生在两个完整
durable composite operations 之间。
一旦进入 append attempt，zero-byte write/error 也允许统一按 unknown；任一首字节已触碰但 short/partial write、write
error、`fdatasync`、必要 directory `fsync`、checkpoint append/sync 或 response 失败，且 caller 未取得 journal+
checkpoint complete+sync proof 时，都必须返回
`AppendResult{outcome=unknown,durable_cursor=null}` 和 exact candidate sequence/previous/record digest，runner 立即停止
数据库进度；旧 cursor 不再有 authority，只有下一次 strict replay 可决定 durable boundary。replay 若在该 sequence 找到 byte-identical complete candidate，仍必须对承载它的 segment 再次
成功 `fdatasync`（以及该 segment 尚未证明 directory durability 时再次 `fsync` parent），并找到或 durable append
byte-exact healing checkpoint 后，才把它认定为 durable 并
幂等返回对应 cursor；不能只因 bytes 可读就承认。若只看到 final torn candidate，replay 只能截断到前一 durable
boundary 并 `fdatasync`，然后由 `RecoverySnapshot` 重新计算 next action；不得声称缺失 bytes 已恢复、不得自动补写
candidate。若该 sequence 已有不同 complete frame、candidate 出现在中间或 chain 不同，则
`MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。同一 logical record 永远不能因 append response 丢失而获得第二 sequence。

rotation 采用 exact worst-case preflight，且可能写入的 rotation header 本身也是 journal record。若当前 segment 放不下
caller record，`AppendDurable` 必须先证明新 segment header frame + 其 checkpoint + caller frame + 其 checkpoint 全部在
本 generation reservation 与 per-segment/per-index limits 内；任一不足时 zero write 返回
`MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`。durability 顺序固定为：创建/同步新 segment 与 directory entry → durable
append continuation header frame → durable append该 header 的 checkpoint → durable append caller frame → durable
append caller checkpoint。rotation header 或其 checkpoint 一旦已尝试，而 caller frame 尚未取得完整 composite durable
proof，整个调用仍返回 `outcome=unknown,durable_cursor=null`；`rotation_header_record_digest`/
`rotation_header_checkpoint_record_digest` 只作 candidate diagnosis，不是 cursor/authority。replay 必须保留已 durable
header、healing/验证其 checkpoint，并从该 header 后的 next sequence 判定 caller record absent/torn/complete；caller
不得把“rotation header durable、caller record absent”解释成 caller record durable，也不得在原 cursor 上补写。
固定 inclusive maxima 不可由 env/CLI/DSN/GUC 覆盖：frame 1 MiB、segment 16 MiB、每 segment 4,096 records、每
journal 16 segments；exact 等于 maximum 合法，任何 append 将使值超过 maximum 时必须在下一条 SQL 前返回
`MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`。
record-kind on-disk framed-record maxima 进一步固定为 header 32 KiB、statement intent 64 KiB、intermediate evidence
256 KiB、commit intent 64 KiB、attempt terminal 64 KiB、ambiguous resolution 64 KiB；这些 maxima 同样包含 prefix 与
frame headers。content object store 固定 inclusive maxima 为 64 个 final objects、总计 4 GiB；runtime outer artifacts
和 decision-recovery artifacts 都计入同一 object count/bytes quota，后者还受单件 4 MiB inclusive maximum。
journal root 固定为
16 个 run journal、总 durable reservation 4 GiB，单 journal 仍受 16 × 16 MiB 上限。所有限额不可配置覆盖，store
不自动删除 final object/journal 以制造额度。

`Open` 在任何第一次 `Connect` 前尚不能信任 ledger，因此必须把 **整个 schema bundle 的所有 entry 都保守视为尚未
提交**。attempt budget 的唯一 authority 是 verified schema bundle 已有 `ExecutionPolicy.MaxAttempts`；它对每个 entry
统一适用，不在 entry 或 C3 wire 新增 `max_attempts` 字段。reservation 按该统一值计算完整 remaining bundle 的所有 statement intent/intermediate、commit intent、
terminal、possible resolution、segment headers 与 length prefixes，以及每个 journal frame 对应的 lineage checkpoint、
reserve/activate/possible supersede 的 record/bytes worst case；绝不能只为
单个 entry admission。后续 exact ledger read 即使证明 prefix 已提交，也不能缩小该 run 已取得的保守 reservation。它
必须在 root-wide lock 下从 bounded no-follow object files、journal directories 和 strict-replayed LineageIndex
generations 重算 root usage；该 replay 必须覆盖所有 registered lineage/index/journal 和全部 registered schema-bundle
digests，不能只覆盖 current lineage/current schema bundle，也不能把 commit-1 object `Scan` 冒充 aggregate authority，以
no-overcommit 方式 durable append `GenerationReserved`；该 record 就是 journal/index quota 的唯一 durable
reservation authority，然后才按 registration state machine 写 segment-0 header。header 的 reservation fields/digest
必须 exact 对应。任一 journal/object/index count、bytes、records 或
segments admission 超限，必须在 Connect/transaction/SQL 前返回
`MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`。reservation 只防 exhaustion，不替代每次 append actual-size validation
或任何 sync；run 未达到 terminal 前不得回收，terminal 后也只能由显式、审计过的 maintenance 操作回收，不得由
runner 自动 GC。

whole-bundle reservation 的 exact inclusive formula 冻结为下式；所有 `MaxFramed*Bytes` 都是其 closed DTO
record-kind maximum（已包含 8-byte prefix + full frame headers + canonical record），不使用 sample serialization 或
平均值：

```text
max_attempts = verified_schema_bundle.execution_policy.max_attempts
entry_count = count(verified_schema_bundle.entries)
attempts_total = entry_count * max_attempts
statements_total = sum(entry.statement_count * max_attempts for every entry)
terminal_records = attempts_total
resolution_records = attempts_total

WorstCaseCallerSizes = concatenate, in verified entry/attempt/statement order:
  for each entry in verified order:
   for attempt_index = 1..max_attempts:
    repeat entry.statement_count times:
      [MaxFramedStatementIntentBytes, MaxFramedIntermediateBytes]
    [MaxFramedCommitIntentBytes,
     MaxFramedAttemptTerminalBytes,
     MaxFramedAmbiguousResolutionBytes]

segments = 1
segment_records = 1
segment_bytes = MaxFramedHeaderBytes
reserved_records = 1
reserved_journal_bytes = MaxFramedHeaderBytes
for size in WorstCaseCallerSizes:
  if segment_records + 1 > 4096 or segment_bytes + size > 16 MiB:
    segments += 1
    segment_records = 1
    segment_bytes = MaxFramedHeaderBytes
    reserved_records += 1
    reserved_journal_bytes += MaxFramedHeaderBytes
  segment_records += 1
  segment_bytes += size
  reserved_records += 1
  reserved_journal_bytes += size
reserved_segments = segments
reserved_checkpoint_records = reserved_records - 1 /* segment-0 activation header excluded */
new_index_header_records = 1 if lineage index is brand-new else 0
new_index_header_bytes = MaxFramedLineageIndexHeaderBytes if lineage index is brand-new else 0
reserved_index_records = new_index_header_records
                       + 2 /* generation_reserved + generation_activated */
                       + reserved_checkpoint_records
                       + 1 /* possible generation_superseded */
reserved_index_bytes = new_index_header_bytes
                     + MaxFramedGenerationReservedBytes
                     + MaxFramedGenerationActivatedBytes
                     + MaxFramedGenerationCheckpointBytes * reserved_checkpoint_records
                     + MaxFramedGenerationSupersededBytes
reserved_bytes = reserved_journal_bytes + reserved_index_bytes
```

这是按 record-kind inclusive maxima 与 deterministic execution order 做的 exact worst-case first-fit simulation；rotation
predicate 的 `>` 表示 exact 16 MiB/4,096 records 合法，只有下一 record 将超过时才 rotate。entry/statement/attempt
count、每个乘加与最终值都用 checked integer arithmetic，overflow 等同 limit exceeded；`segments > 16` 也立即拒绝。
`terminal_records` 为每个 attempt 保守一个，
`resolution_records` 也为每个 attempt 保守一个，不因当前 failure path 看似不会 ambiguous 而删减。rotation header 按
上述 simulation 插入；每个由 `AppendDurable` 写入的 journal record（含每个 rotation header）exact 对应一个
checkpoint。segment-0 activation header 不经过 `AppendDurable`，由紧邻的 `GenerationActivated` 证明，故 checkpoint
数 exact 等于 `reserved_records-1`。formula 的 index bytes 与 index count 必须计入 root
admission；object bytes/count 另按 current runtime + recovery objects 的 exact verified size 加入 combined root quota，
不得塞入 `reserved_bytes` 造成双计数。若该 worst case 本身超过 journal/index maxima，必须在 DB 前拒绝，不得依赖运行时
“大概不会发生”的路径。
brand-new lineage 的 `LineageIndexHeader` 还必须 exact 保守 `1` record 与
`MaxFramedLineageIndexHeaderBytes`；existing strict-replayed lineage 两者为零。它进入 root combined admission 与
`reserved_index_records/bytes` 计算，但不是 generation-owned journal capacity，所以不进入 header/
`GenerationReserved.reserved_records`，也不改变 C3 wire。
现有 C3/header/quota fields 的 same-bits 语义不变：`reserved_records` 是 journal records（含 rotation headers），
`reserved_segments` 是 journal segments，`reserved_bytes` 是 journal + checkpoint/reserve/activate/possible-supersede index
frames 的 combined durable byte reservation；`reserved_index_records/bytes` 只是计算中间量，不新增 wire member。
三重 inclusive limit 必须同时成立且分别检查：generation `reserved_bytes <= 256 MiB` combined reservation；其中
`reserved_journal_bytes <= 16 * 16 MiB = 256 MiB` journal component；写入目标 LineageIndex 后该独立 index file 的 actual
durable bytes `<= 16 MiB`。combined 等于 256 MiB、journal component 等于 256 MiB、index file 等于 16 MiB 分别合法；
任一超过即 pre-DB `MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`，不能以另一上限尚有空间抵消。

journal root 必须是预先配置、已存在的 absolute local path，缺失时拒绝而不是创建。root、`lineages/`、每个 lineage directory 和每个 generation journal
directory 的所有 path component 必须用 no-follow component walk 验证为真实 directory、owner 是 runner UID、且
group/world 均不可写，directory mode 至多 `0700`；directory 不适用 regular-file/link-count predicate。
object/temp/LineageIndex/root-wide lock/per-lineage writer lock/generation writer lock/segment 则必须分别
no-follow open，验证为 owner 匹配、mode 至多 `0600`、link-count=1 的 regular file；任何 symlink、device/socket/FIFO
或 hard link 都拒绝。
首次创建 root-wide/per-lineage/generation lock、LineageIndex 或 segment file 时，必须先 `fdatasync` 新 file，再
`fsync` containing directory 后才能把它视为存在/可用；file create 成功本身不是 durable directory-entry proof。
创建新 segment 使用 `O_NOFOLLOW|O_CREAT|O_EXCL`，replay 已存在 segment 使用
`O_NOFOLLOW` read/write open 并在读取任何 record 前验证 owner/mode/regular-file/link-count/header identity；不得用
`O_EXCL` 伪装 replay。single-writer lock 必须在整个 replay/run/reconcile 期间独占，不能用进程内 mutex 冒充。
journal/object root 首期 production allowlist **仅限 Linux 上 local `ext4` 与 `xfs`**，且启动时必须同时通过 OS、mount
source/type/options 与 required syscall probe；Linux 上其他 filesystem、bind 到 unknown backing mount、overlayfs、tmpfs、
NFS、FUSE、object-backed mount，以及任何非 Linux OS 都是 unsupported，必须在 DB 前 fail closed。Darwin 只允许使用
in-memory/fake filesystem 做 unit tests，不得运行 production `Open`，不得把 APFS 测试、普通重启或 fake fault 注入称为
crash/power-loss durability evidence。即使 OS/mount/syscall consistency checks 通过，缺少 trusted provisioner 提供的
non-forgeable mount authority 时 production constructor 仍必须在任何 probe 或 mutation 前拒绝，不能凭 kernel mountinfo
推断 direct local mount。当前 committed source 已将 `golang.org/x/sys/unix v0.44.0` 作为 direct dependency 链接到 Linux
production filesystem source，其 exact version、license/SBOM、source provenance 与实际 syscall surface 的 dependency
review/integration 已闭合。production constructor 继续 fail closed 的剩余原因只是 trusted mount authority 尚未实现；该闭合
不构成 real required-syscall probe/runtime validation、`ext4`/`xfs` 或 power-loss/restart durability evidence，也不授权绕过
下述 environment matrix。
journal/object root 只支持上述 allowlist，且启动时 required syscall probe 必须
成功：POSIX advisory lock 跨进程互斥、atomic no-replace publish
（`renameat2(RENAME_NOREPLACE)` 或同目录 link/unlink 等价）、regular-file `fdatasync` 与 parent-directory `fsync`。
probe 必须先持有既有 `<root>/lineages.lock` root-wide lease，且所有 probe files 只能位于已验证的
`<root>/objects/sha256`，basename 必须是 entry grammar 已允许的 exact `.tmp-<32lowerhex>`，其中 nonce 来自独立 128-bit
CSPRNG；若 `lineages.lock` 原先缺失，只能在 trusted mount authority 与 closed root grammar 已验证后，先按上述
registration durability protocol 创建、同步并取得该 lock，再开始 probe。probe 不得在 root、`objects` 或其他 directory
创建 `caj-probe-*` 或任何新增 allowlisted name。clean probe 必须
unlink 自己拥有的 temp 并 `fsync(objects/sha256)` 后才成功；crash/unknown 遗留仍只是普通 valid-name temp，下一次 bounded
scan 必须保守计入 temp count/bytes，不能靠 probe prefix 忽略、自动删除或把它升格为 final object。
unknown/unsupported mount type、NFS/FUSE/object-backed mount 或任一 syscall/lock/no-replace probe 失败都必须在 Connect
前拒绝，不得用“best effort”降级。online startup probe 只验证调用与可观察语义，不宣称能证明 storage/controller 的
真实 crash durability；后者必须由 impl-3 fault-injection/environment matrix 的 power-loss/restart evidence 验证。
journal path 固定为
`<root>/lineages/<execution_lineage_digest hex>/<journal_identity_digest 去掉 sha256:>/`，其下只有 `writer.lock` 与
`segment-%08d.caj`；不得用 migration ID
或 caller string 拼 path。permission、owner、lock、write、sync failure 一律 fail closed。`Close` 不是 durability 或
correctness barrier：此前已成功 `fdatasync` 的 records 与已证明的 database outcome 继续有效；但 lock release/close
failure 必须返回 `MIGRATION_EVIDENCE_JOURNAL_FAILED`，阻止 clean success、writer handoff 或同进程再次执行，下一次
owner 仍须 no-follow open、取得 lock 并完整 replay。

replay 使用相同 strict JSON/RFC8785/closed-union/bounds，验证所有 segment/header/sequence/digest/attempt chain。
只有最后一个 segment 最尾部、尚未形成完整 length-prefixed frame 的 torn tail 可截断到最后一个 valid durable
boundary，截断和 `fdatasync` 成功后只能重建 `RecoverySnapshot`，不能自动补写被截断 bytes；middle corruption、完整 frame digest mismatch、unknown key/kind、
duplicate sequence、跨 segment 断链或非 canonical bytes 一律以 `MIGRATION_EVIDENCE_JOURNAL_CORRUPT` 拒绝。
journal 只保存 allowlist identity、digest、offset、
classification、bounded counts 和 stable code；禁止 DSN/password/token、raw SQL/literal、raw catalog row、driver
message 或未 allowlist role/ACL payload。exact SQL bytes 由 journal 中 artifact digest + offset + statement digest 引用，
不重复写入 journal。

append/数据库边界固定如下：

- `statement_intent` 在 before projection/transition compare 后、SQL 前 durable；append 失败时 SQL 零次；
- intermediate 在 after compare 后 durable；失败时 rollback，禁止下一 statement/ledger/commit；
- final intermediate 在 pre-ledger final equality 后 durable；随后 ledger insert；
- `commit_intent` 在 ledger insert/readback 后、commit 前 durable；失败时 rollback；
- `Commit` 调用前发生 connection loss 时，只有 transaction owner 证明 `commit_called=false`、旧 handle
  irrevocably closed/discarded，再由 new distinct connection 取得 same advisory lock 并 exact 证明
  predecessor/ledger/authority，才能以 `precommit_connection_terminated_exact_predecessor` retry（无预算则
  terminal）；不得伪造 rollback receipt；
- durable `commit_intent` 后调用 `Commit`：明确成功时 append `committed`；明确返回 PostgreSQL SQLSTATE
  `40001`/`40P01` 是 confirmed abort，只有新 connection 证明 bindings、ledger 和
  `CommitIntent.attempt_predecessor_catalog_digest` 的 exact predecessor 且 attempt budget 尚有余额，才能 append
  `aborted_retryable` 并授权下一 attempt；无余额则 append `aborted_terminal`。其他完整 PostgreSQL
  `ErrorResponse + ReadyForQuery` 证明 confirmed abort 时，只能
  `other_confirmed_postgres_error + aborted_terminal + retryable=false + non-null commit_rejected proof`，永不 retry。一旦
  `Commit` 已被调用，connection loss、
  EOF、context cancel/deadline 或任意 timeout 都永远是 ambiguous，禁止按 rollback/retry 处理；
- commit 明确成功或 reconciliation 得到 exact outcome 后，必须 durable append 唯一 attempt terminal 后才能向 caller
  报告 terminal result。

以上规则是 ADR-0009 第 10 节宽泛 “serialization/deadlock/connection-loss retry” 对 impl-3 的精确收窄；尤其不能把
`Commit` 调用后的 connection loss 解释为该宽泛 retry。

`Replay` 必须从完整 chain 构造上述 opaque `RecoverySnapshot`，并 exact 返回最后 terminal/resolution、
statement/intermediate/commit intent 的 owned typed body、migration/attempt identity、record digest，以及
previous-terminal/last-intermediate linkage 与唯一 next action；typed body 与 digest/cursor 不一致、缺失必要 link、同
attempt 多 terminal、resolution 未引用 unresolved terminal 或 state/next-action 不一致均为
`divergent`，不得以默认值继续。activated generation 只有 header 时必须联合 injected continuation 判定：

- continuation=null 且没有 immediately preceding superseded 才是 true `brand_new/begin_first_attempt`；pre-DB
  snapshot 的 migration/attempt 可为 null，随后 exact ledger preflight 必须选择 current verified bundle 的第一
  pending entry、`attempt_index=1`、previous attempt terminal null；
- continuation=null 但 immediately preceding superseded outcome 为 `activated_no_migration_progress` 时，snapshot 必须为
  `brand_new_inherited/begin_first_attempt`，migration/attempt 同样由 current exact ledger preflight 选择；这一 state 只证明
  继承了“无 continuation 的旧 header-only generation”，不得被归类为 true brand-new；
- successor continuation 返回 `brand_new_inherited/begin_first_attempt_next_entry`，migration exact 为 context 的 next
  entry、`attempt_index=1`、previous null；
- retry/pending continuation 返回 `brand_new_inherited/begin_next_attempt`，migration exact 为旧 entry、attempt 是
  `N+1<=max_attempts`、previous exact 为旧 generation terminal digest。

因此不能把所有空 header 无条件解释成 brand new。最后一个
schema-bundle entry 已 committed 或 resolved_committed 才是 `completed/return_success`；非最终 entry committed 或
resolved_committed 必须是 `terminal/begin_first_attempt_next_entry`，下一 entry 固定 `attempt_index=1`、
`previous_attempt_terminal_digest=null`，但 EvidenceFrame `previous_record_digest` 仍引用上一 entry 的 terminal/resolution
tail，保持整个 journal chain 连续。dangling commit 是
`dangling_commit_intent/reconcile_commit`；durable unresolved terminal 是
`ambiguous_unresolved/reconcile_commit`；普通 terminal 根据其 exact outcome 只能
`begin_next_attempt|return_success|return_failure`。`resolved_pending` resolution 与 exact retry budget 可授权
`begin_next_attempt` 的充分条件只采用第 5.4.6 节 adjacent 二选一，并以 unresolved terminal digest 作为下一 attempt
predecessor。
replay 见 `aborted_terminal` 时，只有 transaction-boundary + non-null validated proof 可投影为
`confirmed_abort_terminal/return_failure`；其余 nonretryable terminal 投影为
`terminal_failure/return_failure`；两者都不得被重分类为 `divergent_terminal`。

新 generation 的第一条 `StatementIntent` 必须 exact 消费 immutable continuation：non-null 时 migration ID、attempt
index 与 previous-attempt-terminal 三字段逐字节相等，且 successor/retry 对应的 ledger prefix、entry identity、attempt
budget 仍通过 current verified bindings 检查；null 时只能使用 exact ledger preflight 选出的 first pending entry、attempt
1、previous null。append 前 mismatch 必须 `divergent/return_failure` 且 SQL 为零；replay 发现已
durable 的 first intent 不匹配 injected context 是 `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`。first intent durable 后 context
即视为 consumed，后续 intent 不能再次使用或改变它。continuation 的旧 terminal、old checkpoint、authority 或 index
frame linkage 任一 stored mismatch 都在 DB 前 corrupt；仅当前 binding/authority 不可验证则 recovery-required。两者都
不得重置 attempt budget、把 inherited state 降级为 brand_new 或盲重放旧 SQL。

dangling `statement_intent` 或 `intermediate` 且没有 `commit_intent` 时，旧 transaction 不可能被本 runner commit；
`RecoverySnapshot` 分别返回 `dangling_statement_intent`/`dangling_intermediate`。只有显式调用 `Runner.Run` 后，runner
用新 dedicated connection 重验 exact bindings/role/lock/ledger/catalog，证明 actual catalog exact 等于该 attempt 的
first-before predecessor、journal linkage exact 且仍有 attempt budget，才可 durable append `aborted_retryable` 并进入
下一 attempt；无预算或 predecessor 不 exact 时只能 append `aborted_terminal` 并 return failure；journal link/shape
本身不 exact 则是 `divergent/return_failure`，不能追加新 record。
`Replay`、`Open`、后台 goroutine 或独立 recovery daemon 都不得自行连接或写数据库，也不得自动 append terminal。
dangling `commit_intent` 必须按第 5.4.6 节视为 ambiguous commit，禁止先重放 SQL。journal append/sync 在 commit 后
失败时，数据库结果不回滚成“失败”；runner 返回
`MIGRATION_EVIDENCE_JOURNAL_FAILED` 和 committed-unknown evidence 状态，下一次打开必须从 durable commit intent
reconcile，绝不能因 terminal 缺失而重放。

#### 5.4.5 Terminal runner/evidence codes

第 10 节 stable projection error allowlist 保持不变。`AttemptTerminalState.stable_error_code` 使用独立 closed union：
第 10 节 projection runtime codes 中除 `MIGRATION_PROJECTION_NOT_IMPLEMENTED` 和
`MIGRATION_PROJECTION_LIMIT_OVERRIDE` 外的 codes，加上
`MIGRATION_INVALID_SQL`、`MIGRATION_INVALID_LEDGER`、`MIGRATION_UNTRUSTED`、`MIGRATION_LOCK_LOST`、
`MIGRATION_TRANSACTION_BOUNDARY`、`MIGRATION_AMBIGUOUS_COMMIT`、
`MIGRATION_EVIDENCE_JOURNAL_FAILED`、`MIGRATION_EVIDENCE_RECOVERY_REQUIRED`、
`MIGRATION_CONTEXT_CANCELED` 和 `MIGRATION_DEADLINE_EXCEEDED`。其中 journal code 的 closed mapping 为：

`NOT_IMPLEMENTED|LIMIT_OVERRIDE` 均必须在 preconnect/attempt 之前返回 run-level error，不生成 terminal；它们仍
留在第 10 节 public stable error allowlist，但不得出现在 `StableFailureEvidence`、terminal matrix 或伪造不可达 tuple
中。

- open/create/write/flush/fsync/fdatasync/permission/owner/lock/close failure => `MIGRATION_EVIDENCE_JOURNAL_FAILED`；
- replay strict-shape/canonical/digest/sequence/segment chain corruption => `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`；
- 任一 fixed inclusive maximum 将被超过 => `MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`；
- durable dangling intent 阻止普通执行、必须先 reconciliation => `MIGRATION_EVIDENCE_RECOVERY_REQUIRED`。

每个 non-committed terminal 还必须携带 closed、bounded、redacted failure object：

```text
StableFailureEvidence {
  code: <exact AttemptTerminalState stable code union>
  projection_kind: "authority" | "catalog" | "snapshot" | null
  phase: "preconnect" | "journal_open" | "journal_replay" |
         "connected_session" | "migration_role" | "migration_transaction" |
         "commit" | "reconcile" | "journal_close"
  path: "trust" | "journal" | "authority" | "catalog" |
        "sql" | "ledger" | "transaction" | "context"
  major: uint16 | null
  retryable: bool
}

RetryProofEvidence {
  proof_kind: "projection_transient_exact_predecessor" |
              "precommit_rollback_exact_predecessor" |
              "precommit_connection_terminated_exact_predecessor" |
              "commit_rejected_exact_predecessor"
  attempt_predecessor_catalog_digest: Digest
  observed_catalog_digest: Digest
  ledger_prefix_digest: Digest
  authority_result_digest: Digest
  commit_rejected_reason: null | "serialization_failure" | "deadlock_detected" |
                          "other_confirmed_postgres_error"
}
```

`StableFailureEvidence` 是 exact tuple，必须同时通过 code→kind、kind→path/phase、non-projection
code→path/phase、major 和 outcome/retry/proof 五组 mechanical matrices；没有 fallback pair，也不得用 phase 为
带 `MIGRATION_PROJECTION_` prefix 的 code 在 `authority|catalog|transaction` 中自由选 path。

projection code 的 closed `projection_kind` 集合固定为：

| exact code                                                                           | legal `projection_kind`            |
| ------------------------------------------------------------------------------------ | ---------------------------------- |
| `MIGRATION_AUTHORITY_DRIFT`                                                          | `authority`                        |
| `MIGRATION_CATALOG_DRIFT`, `MIGRATION_INTERMEDIATE_STATE_MISMATCH`                   | `catalog`                          |
| `MIGRATION_PROJECTION_UNSUPPORTED_MAJOR`, `MIGRATION_PROJECTION_CAPABILITY_MISMATCH` | `snapshot`                         |
| `MIGRATION_PROJECTION_CATALOG_QUERY_FAILED`, `MIGRATION_PROJECTION_LIMIT_EXCEEDED`   | `authority`, `catalog`             |
| `MIGRATION_PROJECTION_NON_CANONICAL_WITNESS`                                         | `authority`, `catalog`             |
| `MIGRATION_PROJECTION_UNKNOWN_OBJECT`, `MIGRATION_PROJECTION_INVALID_SCOPE`          | `authority`, `catalog`             |
| `MIGRATION_PROJECTION_INVALID_EXPRESSION`                                            | `catalog`                          |
| `MIGRATION_PROJECTION_METADATA_MISMATCH`                                             | `authority`, `catalog`, `snapshot` |
| `MIGRATION_PROJECTION_SNAPSHOT_INVALID`                                              | `snapshot`                         |

kind 选定后 path 和 phase 没有第二种选择：

| `projection_kind` | exact `path`  | legal phases                                                                |
| ----------------- | ------------- | --------------------------------------------------------------------------- |
| `authority`       | `authority`   | `connected_session`, `migration_role`, `migration_transaction`, `reconcile` |
| `catalog`         | `catalog`     | `migration_role`, `migration_transaction`, `reconcile`                      |
| `snapshot`        | `transaction` | `connected_session`, `migration_role`, `migration_transaction`, `reconcile` |

code-specific phase 再与 kind phase 取交集：`UNSUPPORTED_MAJOR|CAPABILITY_MISMATCH` 只允许
`connected_session`；其他 projection terminal code 只允许其 kind 表中 phases。任何交集为空的 tuple
不存在，不得由 decoder 猜测。

non-projection code 必须 `projection_kind=null`，且只允许下表 path/phase 集合：

| exact code/category                                         | exact `path`  | legal phases                                                     |
| ----------------------------------------------------------- | ------------- | ---------------------------------------------------------------- |
| `MIGRATION_EVIDENCE_JOURNAL_FAILED`                         | `journal`     | `journal_open`, `journal_replay`, `reconcile`, `journal_close`   |
| `MIGRATION_EVIDENCE_RECOVERY_REQUIRED`                      | `journal`     | `journal_replay`, `reconcile`                                    |
| `MIGRATION_CONTEXT_CANCELED`, `MIGRATION_DEADLINE_EXCEEDED` | `context`     | any closed phase in the wire enum                                |
| `MIGRATION_INVALID_SQL`                                     | `sql`         | `preconnect`, `migration_transaction`                            |
| `MIGRATION_INVALID_LEDGER`                                  | `ledger`      | `migration_role`, `migration_transaction`, `reconcile`           |
| `MIGRATION_LOCK_LOST`                                       | `transaction` | `migration_role`, `migration_transaction`, `reconcile`           |
| `MIGRATION_TRANSACTION_BOUNDARY`                            | `transaction` | `migration_transaction`, `commit`, `reconcile`                   |
| `MIGRATION_AMBIGUOUS_COMMIT`                                | `transaction` | `commit`, `reconcile`                                            |
| `MIGRATION_UNTRUSTED`                                       | `trust`       | `preconnect`, `connected_session`, `migration_role`, `reconcile` |

`MIGRATION_EVIDENCE_JOURNAL_CORRUPT` 和 `MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED` 仍是 run-level codes，不在
terminal tuple matrix 中；不得为了填 terminal 把它们降级为 `JOURNAL_FAILED`。

`major` 先受 wire `uint16` 约束，再受 phase/code matrix 约束：
`preconnect|journal_open|journal_replay|journal_close` 必须 null；仅
`MIGRATION_PROJECTION_UNSUPPORTED_MAJOR + projection_kind=snapshot + connected_session` 允许 1..65535；
其他 connected-session terminal（包括 capability mismatch）必须是
15..17；`migration_role|migration_transaction|commit` 必须是 15..17；`reconcile` 在连接前的 trust/journal/context failure
必须 null，已连接的 evidence 必须是 15..17。code category、phase/path 或 major 任一不匹配都 strict-shape reject。

`stable_error_code` 必须 exact 等于 `failure_evidence.code`，且 `retryable` 必须 exact 等于本节 closed policy；
`terminal_digest` 对完整 terminal object 的 hashing 因而同时绑定
code/projection_kind/phase/path/major/retryable。不得加入 raw error、
driver text、SQL 或 caller path。committed terminal 的 stable error/failure/retry proof 全部为 null；所有 noncommitted
outcome 的 stable code 与 failure 都非 null且 code exact 相等。

`RetryProofEvidence` 是 exact predecessor/boundary proof，不等于“授权再 retry”。它允许
`aborted_retryable` 且 `failure_evidence.retryable=true`，也允许下述 attempt budget 已耗尽的 confirmed-abort
`aborted_terminal` 且 `failure_evidence.retryable=false`，并进入 terminal digest：

- `projection_transient_exact_predecessor` 只配
  `MIGRATION_PROJECTION_CATALOG_QUERY_FAILED`，`commit_rejected_reason=null`；
- `precommit_rollback_exact_predecessor` 只配 `MIGRATION_TRANSACTION_BOUNDARY` 且 Commit 尚未调用，reason=null；
- `precommit_connection_terminated_exact_predecessor` 只配 `MIGRATION_TRANSACTION_BOUNDARY`、
  `commit_called=false` 且旧 connection/transaction handle 已 irrevocably close+discard，reason=null；
- `commit_rejected_exact_predecessor` 只配 `MIGRATION_TRANSACTION_BOUNDARY`，reason 只能是
  `serialization_failure|deadlock_detected|other_confirmed_postgres_error`。

protocol wrapper 只在 runner 内把 confirmed ErrorResponse 映射为上述三个 bounded reasons；raw SQLSTATE/driver
error 不进入 wire/evidence/digest。四种 proof 的 predecessor/observed catalog 必须相等，ledger
prefix/authority result digest 必须由 replay witness exact
引用；wire object 不能凭自报 digest 自证 predecessor、rollback、commit rejection 或 adjacency。除下述
confirmed-abort `aborted_terminal` 外，所有其他 non-retry outcomes 的 `retry_proof=null`。特别是
`ambiguous_reconciled_pending|ambiguous_unresolved` 的 failure `retryable=false` 且无 retry
proof；后续 attempt 只能由 durable pending terminal/resolution adjacency 授权。

`ledger_prefix_digest` 不是 caller 自报 ledger head。它的 flat domain 固定为：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-ledger-prefix/v1",
  "rows": [<ordered exact LedgerRow identity without applied_at/applied_by>, ...]
}))
```

`rows` 必须按 verified schema-bundle entry order 从 0 连续开始，每行包含 `CommitIntent.ledger_row` 的全部
ledger-backed identity，并且明确排除 observational `applied_at/applied_by`；空 prefix 是 `rows=[]`。禁止只 hash
head/length、二次嵌套 digest 列表或改用 byte-prefix domain。

retry proof 的创建还必须消费 non-serializable external authority：

```text
OwnedRollbackReceipt
OwnedPrecommitConnectionTerminatedReceipt
OwnedCommitRejectedReceipt
OwnedRecoveryPredecessorReceipt
```

`OwnedRollbackReceipt` 只能由拥有 exact transaction/physical connection lifecycle 的 transaction owner 在 rollback 成功且
`commit_called=false` 后产生。`OwnedPrecommitConnectionTerminatedReceipt` 只能由 transaction owner 在
`commit_called=false`、旧 connection/transaction handle 已 irrevocably closed/discarded 且不可再发送任何 bytes 时
产生；它不是也不得冒充 `OwnedRollbackReceipt`。`OwnedCommitRejectedReceipt` 只能在 protocol
wrapper 明确解码 PostgreSQL `ErrorResponse`，且 `ReadyForQuery`/connection lifecycle 证明 transaction 未提交时
产生；它只保存 bounded reason enum
`serialization_failure|deadlock_detected|other_confirmed_postgres_error`，不保存 raw SQLSTATE/message。EOF、timeout、
connection loss、未完整/不确定 ErrorResponse 或缺 `ReadyForQuery` lifecycle proof 都是 ambiguous，不得产生 receipt。
`OwnedRecoveryPredecessorReceipt` 只能由新 connection 上的 DB recovery 流程在重验 authority、exact ledger prefix 与
predecessor catalog 后产生。四种 receipt 必须绑定 execution lineage/journal/migration/attempt、physical connection
lifecycle、`commit_called`、confirmed stable reason、attempt predecessor catalog、上述 flat ledger prefix 和 authority
result；不得从 wire JSON 反序列化或由 caller constructor 产生。

receipt 组合也是 closed：`projection_transient_exact_predecessor|precommit_rollback_exact_predecessor`
必须同时消费 same-attempt `OwnedRollbackReceipt + OwnedRecoveryPredecessorReceipt`；
`precommit_connection_terminated_exact_predecessor` 必须同时消费
`OwnedPrecommitConnectionTerminatedReceipt + OwnedRecoveryPredecessorReceipt`；
`commit_rejected_exact_predecessor` 必须同时消费
`OwnedCommitRejectedReceipt + OwnedRecoveryPredecessorReceipt`。每组两个 receipt 必须是 same
lineage/journal/migration/attempt，但**不是 same physical connection**：旧 rollback/commit-rejected connection 必须先
closed/discarded，随后 new recovery connection 才可建立 predecessor receipt，两者 lifecycle ID 必须 distinct 且形成
exact old→new ordered relation。缺任一 receipt、cross-attempt、same/reversed/unordered connection lifecycle 或两个 receipt
的 ledger/authority/predecessor 不 exact 都拒绝。

`RetryProofEvidence` 只是上述 full witnesses 经 package-private binder 验证后持久化的 bounded summary。创建
terminal 时 external witness 必须验证完整 tuple（code/kind/phase/path/major/outcome/retry/proof）、receipt lifecycle、
journal adjacency 与所有 digest。crash replay 只能验证 durable summary 的 strict shape/self-digest/chain，不能凭 wire 反向
“证明”当时 driver 事件、rollback 或 commit rejection 真实发生。C3 negative fixtures 必须包含伪造
`serialization_failure`、伪 `commit_called=false`、伪 rollback/commit-rejected receipt serialization claim 和只改 summary
digest 的 case，并全部拒绝。

artifact/strict-shape 在 attempt 前失败不生成 attempt terminal；不得为了填 terminal 把 generic driver message 发明成
新 code。Go `context.Canceled` 必须映射 `MIGRATION_CONTEXT_CANCELED`，`context.DeadlineExceeded` 或 runner-owned
deadline expiry 必须映射 `MIGRATION_DEADLINE_EXCEEDED`，二者均 `retryable=false` 且不得把 raw context/driver error
写入 evidence。projector-owned bounded query timeout 仍严格按第 10 节映射
`MIGRATION_PROJECTION_CATALOG_QUERY_FAILED`；不能在两个 ABI 间任选。

唯一 terminal retryable cases 是：第 10 节明确标记 `retryable=true` 的
`MIGRATION_PROJECTION_CATALOG_QUERY_FAILED`；`Commit` 尚未调用且已证明 rollback/exact predecessor 的
serialization/deadlock `MIGRATION_TRANSACTION_BOUNDARY`；`Commit` 尚未调用且已证明 old handle terminated + exact
predecessor 的 connection-loss `MIGRATION_TRANSACTION_BOUNDARY`；以及 durable commit intent 后 `Commit`
明确返回 `40001`/`40P01`、再证明 exact predecessor 的 `MIGRATION_TRANSACTION_BOUNDARY`。`Commit` 调用后的
connection loss/EOF/context/timeout 不在 retryable union。其他 runner/evidence/context codes 全部 `retryable=false`。
上述 precommit rollback 或 confirmed commit rejection 若因 `attempt_index>=max_attempts` 无 retry budget，则是
`aborted_terminal + retryable=false`，但仍按上述 closed rule 保留 non-null boundary proof；proof 不会重新创造
attempt authority。

`MIGRATION_EVIDENCE_JOURNAL_CORRUPT` 与 `MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED` 是 run-level failure：不能尝试
把 terminal 写入同一个 corrupt/over-limit journal；只返回 stable run error，只有已验证 prefix 足以构造时才附 opaque
recovery snapshot，否则 snapshot 为 null。它们属于 runner stable error union，但不属于
`AttemptTerminalState.stable_error_code` union。journal
write/sync/close 失败时，同一不健康 journal 也不能追加 `MIGRATION_EVIDENCE_JOURNAL_FAILED` terminal；只有下一次
open/replay/sync 已证明 journal 健康且 reservation 足够时，显式 `Runner.Run` 才可按 snapshot append 该 terminal。

`AttemptTerminalState` 增加 outcome `ambiguous_unresolved` 和 reconcile result `unresolved`。closed 组合为：

- `committed => stable_error_code=null,failure_evidence=null,retry_proof=null,reconcile_result=not_run,last_intermediate!=null`；
- `aborted_retryable => failure_evidence.retryable=true,retry_proof!=null,reconcile_result=not_run,
attempt_index<max_attempts`，且只能是
  `MIGRATION_PROJECTION_CATALOG_QUERY_FAILED + projection_transient_exact_predecessor` 或
  `MIGRATION_TRANSACTION_BOUNDARY + precommit_rollback_exact_predecessor|
precommit_connection_terminated_exact_predecessor|commit_rejected_exact_predecessor`，且 commit-rejected reason 只能
  `serialization_failure|deadlock_detected`；
- `aborted_terminal => stable_error_code!=null,failure_evidence.retryable=false,reconcile_result=not_run`，并且 stable code
  明确禁止 `MIGRATION_AMBIGUOUS_COMMIT`。非 transaction-boundary terminal 必须 `retry_proof=null`；
  `MIGRATION_TRANSACTION_BOUNDARY` 只允许两种 confirmed-abort：
  `commit_called=false + OwnedRollbackReceipt|OwnedPrecommitConnectionTerminatedReceipt +
precommit_rollback_exact_predecessor|precommit_connection_terminated_exact_predecessor`，或 durable `CommitIntent` 后
  `OwnedCommitRejectedReceipt(serialization_failure|deadlock_detected|other_confirmed_postgres_error) +
commit_rejected_exact_predecessor`。两者都必须由
  new recovery connection 的 exact predecessor receipt 收敛，可因 attempt budget 无余额而保存 non-null proof 但
  `retryable=false`；acknowledgment unknown、connection loss/EOF/context/timeout after `Commit` 永远禁止归类为
  `aborted_terminal`，必须进入 ambiguous outcome；
- `ambiguous_reconciled_committed|ambiguous_reconciled_pending|ambiguous_divergent` 的 `stable_error_code` 必须是
  `MIGRATION_AMBIGUOUS_COMMIT|MIGRATION_EVIDENCE_JOURNAL_FAILED|MIGRATION_EVIDENCE_RECOVERY_REQUIRED` 之一，
  `reconcile_result` 分别为 `exact_committed|exact_pending|divergent`，且
  `failure_evidence.retryable=false,retry_proof=null,last_intermediate!=null`；
- `ambiguous_unresolved => stable_error_code=MIGRATION_AMBIGUOUS_COMMIT|MIGRATION_UNTRUSTED|
MIGRATION_EVIDENCE_JOURNAL_FAILED|MIGRATION_EVIDENCE_RECOVERY_REQUIRED|MIGRATION_CONTEXT_CANCELED|
MIGRATION_DEADLINE_EXCEEDED,failure_evidence.retryable=false,retry_proof=null,reconcile_result=unresolved,
last_intermediate!=null`。

所有 `ambiguous_reconciled_committed|ambiguous_reconciled_pending|ambiguous_divergent|ambiguous_unresolved`
除上述字段组合外，还必须由 external chain witness 证明同 migration/attempt 的 final 0-based statement
intermediate 已 durable，其后 exact adjacent 的 `CommitIntent` 也已 durable，且 commit-called lifecycle 已进入只能
reconcile 的边界。仅 `last_intermediate_state_digest!=null` 或 wire 中存在 `MIGRATION_AMBIGUOUS_COMMIT` code
不足以构造 ambiguous outcome。

`ambiguous_unresolved` 只能在 commit intent 已 durable、但 context/connection/trust/journal failure 使 exact database
outcome 尚不可证明时使用；它不可 retry SQL，也不可改写为 aborted/divergent。下一次 replay 必须继续 reconciliation，
若 unresolved terminal 尚未 durable，则 append exact committed/pending/divergent terminal；若它已经 durable，则保留
唯一 terminal 并 append 第 5.4.4 节 `AmbiguousResolutionState`，不得为同一 attempt 追加第二 terminal。

#### 5.4.6 Exact ambiguous reconciliation

commit acknowledgment 丢失、commit 后 terminal append failure 或 replay 发现 dangling commit intent 时，runner 关闭
旧 connection 并使用新 dedicated connection。current generation 必须 exact reverify 它原有、仍未过期的
`RunnerProjectionBindings`；ancestor generation 不得用 current bindings 冒充 old bindings，也不得调普通
unexpired validator 要求 old decision 仍未过期，只能消费 `Open` 已封入 session 的
`VerifiedRecoveryExecutionBindings`。该 bindings 只授权 current authority projection + old schema/catalog/ledger/artifact
read 与 exact advisory lock，继续禁止 migration SQL、ledger insert、`Commit` 和新 attempt。两条路径都要
重做 connected-session、migration-role、lock 和 database state projection，但 authority/bundle 来源不得互换。令
`entry_index` 为 target entry 的 0-based schema-bundle index：

- `len(ledger) == entry_index + 1`：只有 row `entry_index` 除 observational `applied_at/applied_by` 外的全部
  `LedgerRow` identity 与 durable `CommitIntent.ledger_row` exact，ledger 是合法 prefix，head exact 为 target，且
  actual catalog exact 等于 target cumulative contract，才是 `exact_committed`；
- `len(ledger) == entry_index`：只有 ledger 是 exact predecessor prefix、target row 不存在、head exact 为 predecessor，
  且 actual catalog digest exact 等于 durable `CommitIntent.attempt_predecessor_catalog_digest`，该 digest 又已与本
  attempt 第一条 statement 的 durable intent before digest 及 signed concrete predecessor branch exact 绑定，才是
  `exact_pending`；actual 仅匹配 accepted predecessor union 的任意其他 branch 不足以成功；
- 其他 ledger length，或上述任一 row/head/catalog/binding mismatch，都是 `divergent`。

reconnect/context/trust/journal 暂时无法得到上述 exact 结论是 `unresolved`，不是 `pending` 或 `divergent`。只有 durable
pending proof 满足以下二选一且 attempt budget 尚有余额时才能开启同 entry 下一 attempt：

1. durable `ambiguous_reconciled_pending` terminal；或
2. durable `ambiguous_unresolved` terminal 后紧邻同 migration/attempt 的 durable `resolved_pending` resolution；该模式
   不得写第二 terminal，下一 attempt 的 `previous_attempt_terminal_digest` 必须引用 unresolved terminal digest。

单独的 `exact_pending` database observation、未 durable record 或 resolution 与 terminal 不相邻都不足以 retry。
`exact_committed` 只证明 target entry 已提交，因此绝不重放该 target SQL；它不自动等于 current candidate success。只有
ledger length/head 与 actual catalog 同时达到 current verified candidate 的 final entry/final cumulative contract，才按
`exact_committed_bundle_complete` 返回成功。若 current strict-prefix successor 仍有 remaining entries，同 active
generation 按第 5.4.4 节跨-entry `begin_first_attempt_next_entry` 规则继续；从旧 generation supersede 到新 decision 时
必须 durable `exact_committed_continue_successor` + full planned reservation，创建/activate 新 generation 后从 exact
ledger prefix 的下一 entry 继续。`divergent` 永久 fail closed。

#### 5.4.7 C3 wire DTO、same-bits 与 external chain witness

C3 persisted wire DTO exact 清单固定为：`JournalHeader`、`StatementIntent`、`ProjectionResultEvidence`、
`StatementIntermediateEvidence`、`CommitIntent`、`StableFailureEvidence`、`RetryProofEvidence`、
`AttemptTerminalState`、`AmbiguousResolutionState`、`EvidenceFrame`、`LineageIndexHeader`、
`LineageContinuationContext`、`GenerationReserved`、`GenerationActivated`、`GenerationCheckpoint`、
`GenerationSuperseded`、`LineageIndexFrame` 及两个 exact record unions。`LineageContinuationIdentity` 只属于 recovery
policy digest input。`EvidenceSession`、`ActiveGeneration`、`RecoverySnapshot`、cursor、verified policy/authority/content
receipt、`VerifiedRecoveryPolicySubject`、`VerifiedHistoricalRecoveryPolicy`、
`VerifiedRecoveryExecutionBindings`、`VerifiedLineageSupersessionAuthority`、
`VerifiedHistoricalSupersessionReceipt`、`VerifiedDecisionRecoveryArtifact/Receipt`、`OwnedRollbackReceipt`、
`OwnedPrecommitConnectionTerminatedReceipt`、`OwnedCommitRejectedReceipt`、
`OwnedRecoveryPredecessorReceipt` 和所有其他 `Owned*` 是 opaque runtime types，不是 JSON wire。historical
decision recovery artifact 自身不是 persisted C3 DTO；`JournalHeader` 只持久化它的 SHA-256 和 size，exact bytes
只作为受 receipt 绑定的 content-addressed runtime object。
recovery policy/execution/supersession authority 与 quota 的 flat digest-input bodies（含
`LineageContinuationIdentity`）是 package-private exact
canonical structs，也必须有 TS↔Go same-bits vectors，但不得序列化成可由 caller 注入的 verified wrapper。

wire DTO 必须拥有独立 strict structs/decoders；不得直接复用 generic `ProjectionResult<T>`、database `LedgerRow`、
execution `StatementPlan` 或 opaque wrapper。需要相同字段时显式 copy + exact compare。所有 JSON integer 先满足
RFC8785/JSON safe integer，再与业务类型取交集：`uint32` 为 `0..2^32-1`，`uint64` wire 为
`0..2^53-1`，`uint16` 为 `0..65535`；负数、小数、指数 round-trip 不同 bits 或超过交集均 strict reject。

TS↔Go same-bits fixtures 必须覆盖每个 exact DTO/union、flat digest canonical bytes、safe-integer boundaries、unknown/
missing member 和 negative cross-field matrix。C3 wire decoder 自身最多证明 strict shape、RFC8785 canonical bytes 与
self-digest；**full validation 必须使用 external chain witness**。witness 从 ordered journal/index frames、signed
StatementPlan/catalog subject 和 owned replay state 构造，至少验证：final statement index ↔ durable final intermediate、
intent ↔ signed plan/transition、intent before evidence ↔ intermediate before evidence、resolution adjacency、terminal
retry-proof digest references、完整 StableFailure tuple↔owned retry receipts/connection lifecycle、flat ledger prefix、
checkpoint ↔ journal tail、planned header↔sequence-0 frame digest↔actual byte-exact header、
activated-no-migration-progress boundary、superseded/continuation/reserved/activated linkage。上述 witness
事实不得新增为 wire 自报字段；没有 witness 不得声称 chain conformance。
必须有负向 fixture 证明伪 serialization/rollback/commit-rejected claim 不能从 wire 自证 driver 事件。
还必须覆盖：伪 precommit terminated receipt、旧 handle 仍可用、old/new connection lifecycle 逆序、other
ErrorResponse 缺 `ReadyForQuery`、A/B artifact 缺失、C policy 只授权 A 或 B、重建 A→B authority/planned
body mismatch，以及 `confirmed_abort_terminal|terminal_failure` 与 terminal/proof 不匹配。

C3 Done 只要求 wire DTO、TS/Go same-bits、strict/self-digest validation 与 external witness fixtures。real filesystem
locking、append/fdatasync、crash/power-loss、object publish 和 recovery session fault matrix 属 impl-3 runtime validation，
不得用 C3 in-memory fixture 冒充。

#### 5.4.8 impl-3 filesystem slice Entry、stable mapping 与 Done

本 filesystem slice 是第 5.4.1–5.4.7 节 runtime substrate 的窄实现，不是 runner integration completion。
Entry 必须全部满足：C3 wire/flat digest same-bits fixtures 已固定且本 slice不得改其 canonical bytes/digests；同一个
`TrustVerifier` 的 historical recovery capability 与 binder API 已可由 package-private seam 消费；Linux `ext4`/`xfs`
测试环境的 mount identity、trusted mount authority 与 power-loss harness 已固定；`golang.org/x/sys/unix v0.44.0` 已作为
Linux production-linked direct dependency 完成 exact version、license/SBOM、source provenance、实际 syscall surface 与
module/lock integration review。dependency Entry 对当前 committed source 已闭合；trusted mount authority 和 real
required-syscall probe/runtime validation、`ext4`/`xfs` power-loss/restart evidence 尚未闭合，所以 production constructor
仍保持 pre-mutation rejecting，不能用 Darwin/APFS 或 fake 测试替代。

full-root admission 子切片的额外 Entry 是：object-only `Scan` 与 root-lock adapter 已各自通过当前 ref 的 focused/full/race/
vet/cross-build review；C3 wire fixtures 保持 byte-identical；historical verifier 能从两枚 registered objects 恢复 runtime
manifest、`MaxAttempts` 与 statement closure；所有 runner/DB/cloud/receipt-binder call sites 仍为零。Entry 不允许以 raw
counter/DTO/literal、foreign fd/path/dev-inode assertion、current-only replay 或 production mount bypass 补齐缺口。

filesystem API 的 stable error mapping 是 closed 且优先级固定：

| observation                                                                                                      | stable result                                                        | cursor/session                                  | DB effect                    |
| ---------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- | ----------------------------------------------- | ---------------------------- |
| non-Linux、非 `ext4`/`xfs`、unknown backing mount、syscall/probe unsupported                                     | `MIGRATION_EVIDENCE_JOURNAL_FAILED`                                  | null/closed                                     | zero                         |
| owner/mode/no-follow/link-count/lock/open/create/write/fdatasync/fsync/close failure，且未尝试 append            | `MIGRATION_EVIDENCE_JOURNAL_FAILED`                                  | null/closed                                     | zero                         |
| any journal/index append or sync outcome not proven complete+durable                                             | `MIGRATION_EVIDENCE_JOURNAL_FAILED` + `AppendResult.outcome=unknown` | cursor invalid                                  | stop all further DB progress |
| registered object/header/receipt 或 frame 的 size/digest/canonical/shape/sequence/chain/checkpoint mismatch      | `MIGRATION_EVIDENCE_JOURNAL_CORRUPT`                                 | null/closed                                     | zero or stop                 |
| fixed formula/runtime append would exceed any inclusive maximum                                                  | `MIGRATION_EVIDENCE_JOURNAL_LIMIT_EXCEEDED`                          | unchanged only if zero write; otherwise invalid | zero or stop                 |
| stored chain 完整自洽，但 artifact absent 或 current policy/authorization/signature/recovery capability 不可形成 | `MIGRATION_EVIDENCE_RECOVERY_REQUIRED`                               | null/closed                                     | zero                         |
| context cancellation before any mutation                                                                         | existing context stable code                                         | unchanged/closed                                | zero                         |
| context cancellation after any filesystem append/publish mutation begins                                         | journal failed/unknown semantics；不得降级为 plain cancel            | invalid/closed                                  | stop                         |

corrupt 优先于 recovery-required：已 registered stored object/header/receipt 存在却 size/digest/canonical/closed-shape
不匹配，或 chain 自相矛盾，就是 corrupt；stored chain 完整自洽但 artifact absent，或 current policy/authorization/
signature/recovery capability 无法形成，才是 recovery-required。limit 仅在 exact preflight 证明 zero write 时保留 cursor；一旦 write/publish/append 尝试，任何
不能证明完整 durable outcome 的错误都采用 unknown，不得借 limit/context 分类复用旧 cursor。

fault/Done matrix 至少必须逐 barrier 注入 before/short/after-write/before-sync/after-sync/response-lost/crash+reopen：

| boundary                                                            | required Done proof                                                                                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| runtime/recovery object temp write、publish、objects directory sync | no partial final accepted；typed receipts不可 swap；unknown reopen 全量重验                                                                 |
| root/index/generation directory 与 lock file creation               | no-follow、owner/mode、directory-entry barrier、cross-process lock order                                                                    |
| segment-0 header、reserved、activated                               | exact planned bytes/digests；reserved-no-header/header-unactivated deterministic heal；activated-missing corrupt                            |
| ordinary frame journal write/sync                                   | SQL-before intent zero；complete/torn/different-candidate replay exact                                                                      |
| matching checkpoint index write/sync                                | journal-durable/checkpoint-unknown 整体 unknown；cursor invalid；DB 前 healing checkpoint                                                   |
| rotation segment create/header/checkpoint/caller/checkpoint         | exact preflight；header-only progress 不误报 caller durable；reopen 唯一 next sequence                                                      |
| torn final vs middle corruption                                     | only final incomplete frame truncatable；middle/complete mismatch corrupt                                                                   |
| full-root acquire / busy locks                                      | canonical all-lineage nonblocking try-lock；任一 busy 释放全部 lineage+root 后 bounded retry；无 lock leak/deadlock                         |
| foreign authority / inode swap                                      | foreign fd/path/dev-inode、root/lineage/object inode swap、raw counter/DTO/literal 全拒绝；旧 epoch/revision 不可复活                       |
| quota/root concurrency                                              | ALL history whole-bundle exact formula、checked overflow、exact/+1 boundaries、two-process no-overcommit、no auto-GC                        |
| permit order / revision / reuse                                     | publish→bind→publish→bind→seal reserve-ready；每步 consume/advance revision；skip/swap/replay/copy/cross-root/reuse 全拒绝                  |
| transition result / inventory refresh                               | closed pre-mutation/unknown/durable；publish 后 object set 更新；第二次 publish 拒绝旧 inventory；unknown 无 authority、Open-only reconcile |
| brand-new / successor state graph                                   | receipts→reserved 与 receipts→superseded→adjacent reserved 两条 closed path；顺序、邻接、swap 全拒绝                                        |
| receipt pair one-shot                                               | `ReserveReady.BindReceiptPair` 一次 mint 两枚 receipts→`ReceiptBoundReady`；half-pair、双消费、旧 authority reuse 全拒绝                    |
| superseded→reserved crash/reopen                                    | stored registration 才可 mint recovery-only `RegisteredPublication`/receipt；unreferenced object、自证/new-admission reuse 全拒绝           |
| per-frame reservation debit                                         | A possible-supersede 与 B reserve/activate/checkpoint 各 debit 唯一 generation slot；无 double/missing debit 或超额消费                     |
| same-verifier historical generation                                 | 全 registered generations/bundles scan；stored mismatch→corrupt，absent/unauthorized→recovery-required；disk decode 无 capability           |
| successor handoff                                                   | binder先消费owned evidence；FS用sealed authority与session replay digests cross-bind；A→B exact；one-shot/swap rejection                     |
| platform matrix                                                     | Linux `ext4` 与 `xfs` 分别 power-loss/restart；其他 Linux FS/mount 与非 Linux production pre-DB reject；Darwin 仅 fake/unit                 |

filesystem slice Done 只允许声明：owned append/sealed successor seams、Linux `ext4`/`xfs` filesystem registration、object/
journal/index durability、typed replay/recovery snapshot、quota与上述 fault matrix 已本地验证并由 reviewer 签署。它明确
**不包含** runner phase/order wiring、任何 `Connect`/SQL/ledger/`Commit`、production signature/trust-root、cloud run、
PostgreSQL matrix、Gate closure、Beta/GA 或发布状态；这些 API 在本 slice test harness 中必须使用 fake/opaque inputs，
runner/DB/signature/cloud/Gate 调用计数均为零。只有后续 runner integration slice 才能消费本 filesystem package；不得因
filesystem Done 更新 catalog/runtime publication 或 Gate。

其中 full-root admission 子切片的 Done 还必须逐项证明：三种 opaque authority 的 zero/copy/stale/cross-root/lease/epoch/
revision rejection；全部 registered lineage/index/generation/segment/schema bundle 的 no-follow inventory 与 strict replay；
dual-authority cross-bind 与 dependency 单向性；每次 publish 后 inventory object set/revision 更新且 stale scan 被拒绝；closed
transition result 的 diagnosis/authority shape；brand-new 与 successor 两条 state graph；
`BindReceiptPair` one-shot/atomic pair 与 transition authority invalidation；superseded durable 后 reserved
absent/torn/durable-response-lost 各窗口只能由 stored registration 重建 recovery-only
`RegisteredPublication`/typed receipts，unreferenced object 不能自证；每个 index frame 对 A/B reservation slot 的唯一 debit；
history missing/unauthorized 与 stored corruption 的稳定分类；journal 全 reservation 和 index physical+unconsumed reservation
无 double count/under-count；exact maximum 与 +1；两个真实进程竞争下无 overcommit；每个 durability barrier 的
before/short/after-write/before-sync/after-sync/response-lost/crash+reopen 都只产生唯一可恢复 state。任一 barrier unknown 后
必须证明旧 lease/inventory/permit 均失效且 reopen full scan，durable object reuse 不 mint receipt/reservation。Done record
必须附 fixed source SHA、commands/results/failure evidence 与 reviewer；它仍不得声称 receipt、runner/DB/cloud、trusted-mount
production enablement 或 Gate closure，且 C3 wire fixture/digest diff 必须为空。

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
           "ambiguous_reconciled_committed" | "ambiguous_reconciled_pending" |
           "ambiguous_divergent" | "ambiguous_unresolved"
  stable_error_code: string | null
  failure_evidence: StableFailureEvidence | null
  retry_proof: RetryProofEvidence | null
  reconcile_result: "not_run" | "exact_committed" | "exact_pending" | "divergent" | "unresolved"
  terminal_digest: Digest
}
```

`terminal_digest` 使用 domain `cloud-agents-platform-attempt-terminal/v1` 对移除自身字段后的完整 object 做
RFC8785+SHA-256；statement 尚未产生 intermediate record 就失败时 `last_intermediate_state_digest = null`。
合法组合、runner/evidence stable code、`last_intermediate_state_digest` 的非空约束和 retryability 以第 5.4.5 节为
唯一 authority；本节不另建较宽的别名规则。其他组合或 retryable outcome 达到/超过 verified schema bundle
`ExecutionPolicy.MaxAttempts` 都拒绝。
只有 `committed` 与全部 ambiguous outcomes 必须接受该 entry 的 final 0-based statement index，并证明
`last_intermediate_state_digest` 对应 final durable intermediate；只检查 digest 非空不足以声称 final chain 已关闭。
`aborted_retryable`/`aborted_terminal` 可为 null（尚无 durable intermediate）或引用最后一个 durable statement prefix，
但非 null 时必须 exact 等于 replay 所见该 attempt journal tail 的 `last_intermediate_state_digest`，不得伪造 final-index
proof，也不得引用已 rollback attempt 中未 durable 的 state。
下一 attempt 的 `previous_attempt_terminal_digest` 必须引用它。
首个 attempt 为 null。这样审计链跨 attempt 连续，而 catalog state chain 在每个 rollback 边界重新开始。

该 digest 不是 ledger 的 schema identity 字段，也不替代最终 catalog contract；但 signed
`expected_transition` 属于 catalog artifact，因此必须经 artifact SHA 和 schema bundle**传递绑定**。authority
profile/binding 继续是可独立升级的 trust subject，只以 digest 进入 runtime evidence，不反向成为 schema identity。
运行时 actual digest 是 statement evidence 和 replay input，不得改变已签名 transition。
每个 statement 的 signed artifact digest、offset、exact bytes digest、narrow classification、intermediate digest 和
stable error 都必须进入本地 evidence/provenance；raw SQL bytes 由 signed artifact 引用，不重复写入 journal，也不能
写入 tenant runtime payload。

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

### 10.1 Projection limits v1（不可覆盖）

`cloud-agents-platform-projection-limits/v1` 的数值固定如下，适用于 idle 与 borrowed migration snapshot。它们是
代码/fixture/metadata 的常量；caller、environment variable、DSN option、manifest override 或数据库 GUC 都不能
提高或替换它们。实现若收到任何覆盖请求必须返回 `MIGRATION_PROJECTION_LIMIT_OVERRIDE`，而不是采用较宽值。

```text
max_query_rows                         = 8_192
max_row_bytes                          = 65_536
max_total_result_bytes                 = 8_388_608
max_principals                         = 256
max_membership_edges                   = 1_024
max_membership_depth                   = 32
max_canonical_witness_candidates       = 4_096
max_acl_entries                        = 4_096
max_default_acl_entries                = 512
max_catalog_objects                    = 4_096
max_dependency_edges                   = 8_192
max_expression_nodes                   = 4_096
max_security_labels_per_object         = 32
max_role_settings                      = 512
max_queries_per_projection             = 128
projection_query_timeout_ms            = 5_000
projection_lock_timeout_ms             = 1_000
projection_snapshot_lifetime_ms        = 30_000
projection_idle_in_transaction_timeout_ms = 60_000
```

`max_expression_nodes`、dependency 和 relation object limits 是 1b 的 reserved fixed values；1a 仍必须执行相同
上限 profile，不能因“不读取 expression”而变成无界。每个 query 都同时受 rows、row bytes、total bytes 和
`projection_query_timeout_ms` 约束；一个 aggregate query 不能通过单行较小绕过 total limit。对于所有
inclusive 的 cardinality（query rows、membership edges、principals、ACL/default-ACL entries、catalog objects、
dependency edges、expression nodes、witness candidates、role settings 和 query count），SQL 必须使用
`LIMIT max+1`，或使用额外的 `Rows.Next` sentinel 读取一行来证明是否超过上限；不得取前 N 行后截断。恰好
`max` 行并收到**正常 EOF**成功，只有观察到第 `max+1` 行（即 `observed > max`）才返回
`MIGRATION_PROJECTION_LIMIT_EXCEEDED`；非预期 EOF（包括 `Rows.Err`/流截断）或 scan/type 解码失败返回
`MIGRATION_PROJECTION_CATALOG_QUERY_FAILED`，不能被当作“达到上限”。

bytes 计数在完整 canonical serialization 后累计：单行或总结果只有在累计值实际 `> max` 时拒绝，恰好等于
上限成功；不得截断 bytes 来伪造边界证明。上述 inclusive 规则只适用于 count/bytes/cardinality，
`projection_query_timeout_ms`、`projection_lock_timeout_ms` 和 snapshot/idle TTL 是独立的取消/查询错误：到达
deadline 即取消并按 stable timeout/query-error mapping 返回，绝不套用 `LIMIT_EXCEEDED` 或“相等即成功”的
计数语义。

本次 A2.1a-impl-2 微冻结只约束 limits、authority、schema/default ACL、snapshot 和 API seam；不新增或替换
A2.1b relation/function/child-object query、dependency closure、internal-object normalization 或 expression AST
实现。1b 的 reserved limit 只保证未来实现不会绕过同一上限 profile。

stable error v1 allowlist **固定为以下 codes，不存在“至少”、别名或 adapter-specific 新 code**，并提供 `phase`、
`path`、`postgres_major`、`retryable`；不得把数据库原始 message 当作对外 API：

| code                                         | 语义                                                | retryable        |
| -------------------------------------------- | --------------------------------------------------- | ---------------- |
| `MIGRATION_PROJECTION_UNSUPPORTED_MAJOR`     | adapter 未声明该 major/capability                   | no               |
| `MIGRATION_PROJECTION_CAPABILITY_MISMATCH`   | adapter capability 与 verified contract 不一致      | no               |
| `MIGRATION_PROJECTION_CATALOG_QUERY_FAILED`  | bounded query/scan/scan type 失败                   | 仅明确 transient |
| `MIGRATION_PROJECTION_LIMIT_EXCEEDED`        | rows/bytes/depth/nodes 超限                         | no               |
| `MIGRATION_PROJECTION_UNKNOWN_OBJECT`        | 未声明 object、ACL、node 或 dependency              | no               |
| `MIGRATION_PROJECTION_INVALID_EXPRESSION`    | expression AST 无法安全归一                         | no（reserved）   |
| `MIGRATION_PROJECTION_INVALID_SCOPE`         | unknown default-ACL kind/scope 或 projection scope  | no               |
| `MIGRATION_PROJECTION_NON_CANONICAL_WITNESS` | path adjacency/endpoint/canonical choice 不成立     | no               |
| `MIGRATION_PROJECTION_LIMIT_OVERRIDE`        | caller/env/DSN/GUC 请求覆盖固定 v1 limit            | no               |
| `MIGRATION_PROJECTION_METADATA_MISMATCH`     | snapshot ownership/mode/isolation/phase 不一致      | no               |
| `MIGRATION_PROJECTION_SNAPSHOT_INVALID`      | isolation/read-only/TxStatus/snapshot 违反 contract | no               |
| `MIGRATION_PROJECTION_NOT_IMPLEMENTED`       | impl-2 尚未提供 1b catalog/transition projector     | no               |
| `MIGRATION_AUTHORITY_DRIFT`                  | authority/control-plane projection 不等于 expected  | no               |
| `MIGRATION_CATALOG_DRIFT`                    | cumulative catalog 与 verified expected 不一致      | no               |
| `MIGRATION_INTERMEDIATE_STATE_MISMATCH`      | statement 状态/digest 与 signed descriptor 不符     | no（reserved）   |

digest/strict-shape 映射沿用现有 Go error ABI，边界必须保持可观察且不可互换：strict JSON parser 的 raw
lexical/UTF-8、trailing token、duplicate key、unknown key 或 missing key 统一使用
`MIGRATION_INVALID_JSON`；JSON 已解析后的 typed semantic 校验、closed-union/strict-shape 和 signed manifest
校验使用 `MIGRATION_INVALID_MANIFEST`。`ParseDigest` 的 malformed input 使用 `MIGRATION_INVALID_DIGEST`；
typed field 的 `requireDigest` 失败，以及 digest recompute/mismatch，也使用 `MIGRATION_INVALID_MANIFEST`，不能
退化成 `MIGRATION_INVALID_DIGEST`。projection runtime failure 只能使用上面的 v1 runtime codes，不得重写成
generic ABI code 或另造 projection alias。

stable mapping 不泄露 SQLSTATE、query text 或 driver error。唯一允许 retry 的规则是：外部 transient context
deadline/statement timeout/connection interruption，且已经确认当前 snapshot/transaction 没有不明提交或 authority
漂移，并且尚未超过 verified retry budget；它映射为 `MIGRATION_PROJECTION_CATALOG_QUERY_FAILED(retryable=true)`。
cancel、scan/type/UTF-8 解码失败、所有 limits/scope/witness/metadata/authority/catalog drift、NOT_IMPLEMENTED 和
所有 reserved code 都是 `retryable=false`。row/byte/depth/principal/ACL/witness candidate 超限统一映射
`MIGRATION_PROJECTION_LIMIT_EXCEEDED`；unknown default-ACL kind/scope 和 invalid `ProjectionScope` 统一映射
`MIGRATION_PROJECTION_INVALID_SCOPE`；路径验证失败统一映射
`MIGRATION_PROJECTION_NON_CANONICAL_WITNESS`。原始数据库错误仅作为 bounded、redacted local cause 保存，不参与
digest、retry decision 或对外 message。

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
   regeneration 计划，保留 source SHA、tree/digest provenance。当前 schema-000001/000002 在 1b 前仍是
   `NOT_IMPLEMENTED/UNPUBLISHED` legacy bootstrap contract：decoder 可在且仅在这两个状态下接受 executable
   expected fields 缺失，但 validator 必须拒绝执行；禁止把 namespace-only 1a fixture 写成这两个 head 的完整
   `expected_projection`。任何 `IMPLEMENTED` 或 `PUBLISHED_IMMUTABLE` contract 必须包含本 ADR 的完整 fields。
2. **A2.1a-impl-2：PG adapters**：实现 PG15/16/17 capability probes、authority projector、schema/default ACL
   projector、idle/migration snapshot adapter；所有 unknown/missing field fail closed。
3. **A2.1a-impl-3：runner wiring**：只按第 5.4 节接入唯一 `RunnerProjectionBindings`、same-dedicated-connection
   session snapshots、statement 前/后与 pre-ledger projection、crash-durable EvidenceJournal、terminal/reconcile
   chain 和 redacted runner/evidence errors。Entry 是 impl-1/2 exact commits/matrix 已完成、本节 ABI 已冻结，且第
   5.4.1 节 `projection_scope_authority` contract pre-slice 已完成 strict Go/TS/fixture/digest same-bits。C3 contract
   pre-slice 的 exact Done 仅为第 5.4.7 节 wire DTO 独立实现、TS↔Go same-bits、safe-integer/negative fixtures、flat
   self-digests 与 external chain witness fixtures；不得把 generic runtime structs/opaque wrappers 直接当 wire，也不得用
   C3 fixture 声称 filesystem/power-loss 已验证。impl-3 runtime Done 必须证明
   executable synthetic binding 的 phase/order/journal/fault tests，以及当前
   `UNPUBLISHED_BOOTSTRAP_MUTABLE/NOT_IMPLEMENTED` catalog 在连接数据库前 deterministic fail closed，
   `Connect/ExecuteStatement/Ledger.Insert/Commit` 均为零。production signed verifier/deployment trust-root wiring仍是
   rejecting boundary；不得在本切片实现/生成 A2.1b relation/function/dependency/expression expected state，不得改变
   catalog publication/runtime status、生产 CLI、Gate、release 或部署状态。
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

`projection_api.go`、PG adapter、snapshot implementation 和 projection fixtures 若尚未形成已提交 source
closure，仅作为 planned A2.1a-impl-2 artifacts，不在本索引中伪造可点击 source link；现有 `contracts.go` 的旧
validator seam 继续作为当前实现索引。新 API 提交后必须补充 exact commit/tree link，再由 impl-3 评审 runner wiring。
