# ADR-0009：P1 Migration Bundle、Runner 与 Trust Anchor 冻结

- Status：Accepted
- Date：2026-08-11
- Decision owner：hxp0618
- Implementation executor：Codex
- Gate reviewers：`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` 使用未实现对应证据的独立 reviewer
- Supersedes：无
- Extends：[ADR-0008](0008-p1-postgres-data-kernel.md)
- Amends：ADR-0008 §6 中“一个 bundle digest 同时绑定 schema、bootstrap、runner source”的草案语义
- Implementation input：SQL-design commit `4f39b146cad8c47a241b9e0303661a94aa4b7426`
- Approval basis：用户已批准 ADR-0006/完整计划并授权按 Gate 持续实施；本 ADR 只收窄 P1-A2.1
  的 migration/runner 身份与执行边界，不扩大阶段、发布或部署权限

## 背景

ADR-0008 已固定 exact-byte SQL、六位 migration lineage、dedicated migration LOGIN、PostgreSQL session
advisory lock 和每条 migration 的短事务。`4f39b14` 已建立两条 expand SQL 及 cluster/database
bootstrap SQL，但尚无 strict manifest、可执行 runner、外部 trust anchor 和 crash/replay 实现。

“一个 digest 绑定全部东西”会产生不可接受的耦合：同一 schema head 上修复 runner、tenant helper、
dependency lock 或 bootstrap SQL，digest 会变，而 ledger 中的旧 digest 会被误判为篡改。反过来，如果
runner 只相信目录中自报的 digest，攻击者可以同时改 SQL 和 manifest 得到一套完全“自洽”的
恶意 bundle。

本 ADR 先分离 schema lineage、bootstrap、runtime artifact 和 runner release 四类身份，再冻结 strict
decoder、SQL executor、catalog postcondition、连接/锁/事务与 tenant helper。它不声称实现、matrix、Gate、
发布或部署已完成。

## 决策

### 1. 四层 identity，只有 schema identity 进 ledger

固定四个不可混用的 digest：

| Identity                  | 绑定内容                                                                                                                                           | 存储/验证位置                                               |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| `schema_bundle_digest`    | lineage/head、advisory identity、global authority snapshot、consumer ranges、migration entries/SQL、ancestor                                       | `schema_migrations.bundle_digest`、runner、signed candidate |
| `bootstrap_bundle_digest` | `bootstrap/roles.sql`、`bootstrap/database.sql` exact artifacts                                                                                    | signed candidate/部署证据；不进 ledger                      |
| `manifest_digest`         | runtime manifest 除自身 `manifest_digest` 外的所有字段                                                                                             | runtime artifact 与 signed candidate                        |
| `runner_release_digest`   | signed Platform release subject，后者列出 runner binary/image、source identity、Go module closure、SBOM/notice/provenance 与 checker/test evidence | signed Platform release subject                             |

tenant transaction helper 属于 Control Plane runtime release，只受 `runner_release_digest`/整体 Control Plane
release digest 绑定，不影响 `schema_bundle_digest`。Go/TS source、`go.mod/go.sum`、generator 与 fixtures
属于 build provenance，不要求生产 runner 在磁盘上重读源码。

本 ADR 中所有 digest-valued JSON field 均使用 `sha256:<64-lowercase-hex>` string；公式中的
`SHA-256(...)` 也输出该 profile。archive filename 才使用不带 `sha256:` prefix 的 64 位 lowercase hex。

因此：

- 同 schema head 的 runner/helper/dependency-only 修复只改变 `runner_release_digest`；bootstrap 修复改变
  `bootstrap_bundle_digest/manifest_digest/runner_release_digest`；runtime manifest、execution policy 或 tar closure
  改变 `manifest_digest/runner_release_digest`。三者都不改变 `schema_bundle_digest`；
- migration entry、SQL、schema head、advisory identity 或 global writer authority 变更必须通过新 forward
  migration 推进 `schema_bundle_digest`；
- ledger 行只记录实际执行时的 `schema_bundle_digest`，永不改写成最新 release digest。

### 2. Exact manifest schema 与 initial schema bundle

current manifest 唯一位置为：

```text
services/control-plane/migrations/manifest.json
```

它是 UTF-8、LF、无 BOM、Git mode `100644` 的 strict JSON object。generator 以下列顺序输出顶层字段，
便于人类 review 与 byte-for-byte 重放：

1. `format_version = "cloud-agents-platform-migration-manifest/v1"`；
2. `schema_bundle`；
3. `schema_bundle_digest`；
4. `bootstrap_bundle`；
5. `bootstrap_bundle_digest`；
6. `execution_policy`；
7. `runtime_artifacts`；
8. `manifest_digest`。

JSON object 的输入 member 顺序不参与语义；decoder 接受任意 member 顺序，但拒绝 duplicate/escaped-equivalent
key、default 字段和未知/缺失字段。RFC 8785 负责 canonical object-key order，signed outer artifact SHA-256
负责固定发布 bits，二者不得混为“按输入顺序计算 digest”。`schema_bundle` 的字段固定为：

1. `lineage = "cloud-agents-platform"`；
2. `schema_head = "000002"`；
3. `advisory_lock`；
4. `global_table_authority`；
5. `predecessor_schema_bundle`（initial 为 `null`）；
6. `migrations`。

所有 artifact record 都是 exact object：

```json
{
  "path": "services/control-plane/migrations/000001_expand_migration_kernel.sql",
  "mode": "100644",
  "size_bytes": 1,
  "sha256": "sha256:<64-lowercase-hex>"
}
```

`size_bytes` 的示例值 `1` 只演示 schema，生成时必须写 exact byte count。`bootstrap_bundle` exact shape 为
`{"artifacts": [<ArtifactRecord>...]}`，按 path 升序；`global_table_authority` 与每个 entry 的
`catalog_contract` 都是指向 runtime catalog JSON 的 ArtifactRecord，不是随意 inline map。

initial `execution_policy` exact object 固定：

```json
{
  "statement_profile": "postgresql-ddl-v1",
  "catalog_profile": "cloud-agents-platform-catalog/v1",
  "authority_contract": {
    "path": "services/control-plane/migrations/catalog/authority-v1.json",
    "mode": "100644",
    "size_bytes": 1,
    "sha256": "sha256:<64-lowercase-hex>"
  },
  "isolation_level": "serializable",
  "access_mode": "read_write",
  "postgres_major_min": 15,
  "postgres_major_max": 17,
  "statement_timeout_ms": 300000,
  "lock_timeout_ms": 30000,
  "idle_in_transaction_session_timeout_ms": 60000,
  "max_attempts": 3
}
```

major 范围两端 inclusive；release Gate 仍必须固定并实跑当时受支持的 exact patch 版本，不能把 major admission
冒充 patch/security support。`authority_contract` 属于 manifest/runtime release identity，不进入
`schema_bundle_digest`；因此 role/member/database bootstrap hardening 可以在同 schema head 发布。

initial authority profile 还固定数据库必须由 `template0` 或等价过程创建为：server encoding `UTF8`、locale
provider `libc`、`datcollate = C`、`datctype = C`、无 ICU locale/rules、database/default collation version 为
`NULL`，不接受 `C.UTF-8`/`POSIX` alias、ICU 或宿主默认 locale。这样 text unique/index/regex 使用同一 bytewise
语义。PG15/16/17 adapter 用 `to_jsonb(pg_database)`/versioned field mapping 读取 `datlocprovider`、ICU 与 collation
version 字段；bootstrap CLI 和 runner 都必须在 migration 前拒绝不匹配数据库，不能只设置 `client_encoding`。

`schema_bundle_digest` 固定为：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-schema-bundle/v1",
  "schema_bundle": <exact schema_bundle object>
}))
```

initial bundle 的 `migrations` 为按 ID 升序的 `000001`、`000002`。每个 entry 都包含：

- `id/name/predecessor_id/phase/schema_from/schema_to`；
- `compatible_control_plane_min/max`（min inclusive，max exclusive）；
- `compatible_worker_min/max`（min inclusive，max exclusive）；
- `sql_artifact`（runtime artifact path、mode、size、SHA-256）；
- `transaction_mode/reentrancy/rollback_boundary`；
- `requires_live_instance_preflight/requires_pitr_preflight`；
- `predecessor_catalog_contract`（执行前 cumulative state；`000001` 为 absent/empty bootstrap state）；
- `catalog_contract`（该 head 的 relation/function/policy/ACL postcondition）。

`000001.predecessor_catalog_contract` 的静态序列化形状固定为
`{ "accepted_states": [<schema_absent>, <empty_schema>] }`，数组顺序固定且恰好包含下面两个
exact discriminated-union branch：

- `{ "state": "schema_absent", "schema": "cloud_agents" }`；
- `{ "state": "empty_schema", "schema": "cloud_agents", "owner": "cloud_agents_migration_owner",
"effective_acl": [{"grantee":"cloud_agents_migration_owner","privileges":["CREATE","USAGE"],
"grantable":["CREATE","USAGE"]}], "object_count": 0, "comment": null, "security_labels": [] }`。

empty branch 只允许 namespace 的 owner dependency；无 PUBLIC/其他 grantee、extension membership 或任何 relation/
type/function/operator/collation/object dependency。两 branch 都是完整 object，禁止额外字段。

runner preflight 不在 signed migration artifact 之外 DROP/rebuild 或宽松 adopt empty schema；owner/ACL/object 任一
不符就拒绝。

initial CP/Worker ranges 均为 `[0.1.0-alpha.1, 0.2.0-0)`。现有 ledger 列
`compatible_binary_min/max` 在 manifest v1 中映射 CP range；Worker range 由 entry 与
`schema_bundle_digest` 共同绑定。runner 自身不使用 consumer SemVer 判断能力，只通过
`format_version`、`transaction_mode`、statement executor 和 preflight capability enum 谈判。

initial entry identity：

| Field               | `000001`                  | `000002`                 |
| ------------------- | ------------------------- | ------------------------ |
| name                | `expand_migration_kernel` | `expand_tenancy`         |
| predecessor         | `null`                    | `000001`                 |
| phase               | `expand`                  | `expand`                 |
| schema from/to      | `absent` / `000001`       | `000001` / `000002`      |
| transaction mode    | `transactional`           | `transactional`          |
| reentrancy          | `ledger_guarded`          | `ledger_guarded`         |
| rollback boundary   | `retain_expanded_schema`  | `retain_expanded_schema` |
| live/PITR preflight | `false` / `false`         | `false` / `false`        |

### 3. Advisory-lock key 是 signed decimal string

`advisory_lock` exact record：

```json
{
  "domain": "cloud-agents-platform:migrations:v1",
  "derivation": "sha256-first-8-bytes-signed-big-endian-int64",
  "key_int64_decimal": "-1047838957622507638"
}
```

key 必须是 grammar `^-?(0|[1-9][0-9]*)$` 的 JSON string，禁止 JSON number、`-0`、`+1`、前导零和
whitespace。TS 使用 `BigInt`，Go 使用 base-10 `ParseInt(..., 64)`，两者都必须重新计算 domain
SHA-256 首八字节 `f17554050d18478a` 并得到同一 signed int64。fixture 覆盖 MinInt64、MaxInt64、两侧
越界、`-0`、`+1`、前导零和错误 derivation。

### 4. Schema ancestor 只管 migration lineage

每个已发布 schema bundle 同时生成：

```text
services/control-plane/migrations/schema-bundle.json
```

文件只包含
`format_version = "cloud-agents-platform-schema-bundle/v1"`、`schema_bundle`、`schema_bundle_digest`。
创建 successor 前，必须把已发布的
`schema-bundle.json` byte-for-byte 复制为：

```text
services/control-plane/migrations/archive/<64-lowercase-hex-schema-digest>.schema-bundle.json
```

禁止重新 pretty-print、重排字段或根据当前 source 重建 archive。successor 的
`predecessor_schema_bundle` 是单链 descriptor：`schema_bundle_digest/path/mode/size/sha256`。runner 用
visited set 向后遍历，最多 `128` 代，拒绝环、重复 digest/path、缺文件、跳链或 raw checksum 不符。

每个 predecessor 的 migration list 必须是 successor 的 strict prefix，共享 entry 全字段完全一致；
global-table authority 变更也必须由新 forward migration 推进。runner/helper/bootstrap/release 在同 head 上的
变更不改 schema bundle，因此不需要伪造 no-op ancestor。

ledger 必须是 current migration list 从 `000001` 开始的连续 prefix。每行的 ledger-backed 列除
`applied_at/applied_by` 外，都要按 manifest v1 映射与其 `bundle_digest` 对应的 current/ancestor entry
完全一致；Worker range、catalog descriptor 等未落 ledger 的字段由该 digest 绑定，不得伪称逐列存储。
digest 只能来自已验证的单链。
chain index 固定 oldest published bundle 为 `0`，沿 successor 每次加 `1`，current 最大。按 migration ID 前进时
index 必须单调不降，不得用旧 bundle 认领它不包含的新 ID，
也不改写旧行 digest。

### 5. Bootstrap、manifest 与 release trust

`bootstrap_bundle` 只包含 `roles.sql`、`database.sql` 的 ordered artifact records。
`bootstrap_bundle_digest` 为：

```text
SHA-256(RFC8785({
  "domain": "cloud-agents-platform-bootstrap-bundle/v1",
  "bootstrap_bundle": <exact bootstrap_bundle object>
}))
```

这里的 ordered artifact record 只描述 checked-in source path/mode/size/SHA-256。build checker 必须读取
`roles.sql`/`database.sql` 的 exact bytes 生成这些 record；runtime runner 因 tar 中不携带 bootstrap SQL，
只能重算 record 的 canonical digest、与已验证的 signed candidate 比较，并核验数据库 resulting catalog state，
不能据此声称运行时重新验证或执行了 bootstrap source bytes。

Platform release 另行提供
`application/vnd.cloud-agents.platform-bootstrap-bundle.v1+tar`，只包含这两个 exact SQL artifact；signed
release subject 同时绑定其 outer SHA-256 和 `bootstrap_bundle_digest`。只有 provisioning/bootstrap CLI 消费它，
migration runner 不因方便而获得 cluster/database bootstrap authority。

initial SQL 没有写 durable bootstrap receipt；runner 只重新验证 role/membership/database ACL/schema owner 等 resulting
catalog state，不得声称证明 exact bootstrap bytes 曾执行。如需该证明，先以新 migration/ADR 建立
可认证 receipt schema。

`manifest_digest` 固定为：

```text
SHA-256(RFC8785(<manifest object with only top-level manifest_digest removed>))
```

`runner_release_digest` 是 signed Platform release subject 只移除 top-level `runner_release_digest` 后的
RFC 8785 SHA-256。signature/attestation 必须是 detached envelope，不得作为可任意修改的 subject field。
该 subject 以 artifact record 列出 runner binary/image 和 migration tar，并绑定
`schema_bundle_digest/bootstrap_bundle_digest/manifest_digest`、source commit/tree、build profile、module closure
及 supply-chain evidence。binary 只嵌入非自引用的 source/build/capability identity；不得把包含 binary SHA 的
`runner_release_digest` 再嵌入 binary 后重算。

subject 另有 signed positive integer `security_epoch`，initial 为 `1`。deployment trust policy 对
repository/release train 持久化 minimum accepted epoch；authority/trust hardening 必须提高 epoch 或显式 revoke
旧 subject。即使旧 candidate 尚未 expiry 且 signature 有效，低于 minimum 的 manifest/runtime/runner 也必须拒绝，
避免用旧 authority contract downgrade。

runner 在连接数据库前必须获得不由 bundle 自报的 verified trust decision。该 decision 必须由 runner 内置或
部署时显式配置的 trust root 验证 signed Platform candidate 后生成，不能把调用方传入的 loose digest string
直接提升为 trust。typed decision 至少包含：

- `expected_schema_bundle_digest`；
- `expected_manifest_digest`；
- signed outer migration artifact SHA-256、repository/release identity、signer/trust-root 和 expiry/revocation 决策；
- `expected_runner_release_digest`。

production/release mode 禁止“接受目录/文件自报的任意 digest”。local test 只能使用 checked-in、显式
`test_only=true` 的 trust fixture，该通路不可编译进 release CLI。Platform release subject 绑定
`schema_bundle_digest/bootstrap_bundle_digest/manifest_digest` 与 runner artifact，计算结果才是
`runner_release_digest`；detached signature/attestation 绑定该结果，不得把四者互相递归作为输入。
runner release identity 必须与 binary embedded build identity 或已验证的 OCI/image subject 一致；只有
`schema_bundle_digest` 写入 ledger。

bare binary 必须由 trusted installer/launcher 在 exec 前按 release subject 验证 executable SHA-256；OCI 路径由
admission/launcher 验证 exact image digest。runner 内嵌 source/build string 只用于 cross-check，不是可单独成立的
self-attestation。release evidence 必须覆盖 verified artifact 到实际启动 process 的 binding。

### 6. Runtime migration artifact layout 与资源上限

运行制品 media type 固定为：

```text
application/vnd.cloud-agents.platform-migration-bundle.v1+tar
```

tar 根下只允许：

```text
services/control-plane/migrations/manifest.json
services/control-plane/migrations/schema-bundle.json
services/control-plane/migrations/archive/*.schema-bundle.json
services/control-plane/migrations/*.sql
services/control-plane/migrations/catalog/*.json
```

manifest 顶层 `runtime_artifacts` 是按 path 升序的 ArtifactRecord array，覆盖
`schema-bundle.json`、authority contract、所有 ancestor archive、SQL 与 catalog JSON；每项固定
`path/mode/size/sha256`。
它明确不包含 `manifest.json` 自身或 outer tar，因此不存在 manifest/tar digest 自引用。schema bundle 中的
`sql_artifact`/`catalog_contract` 和 predecessor descriptor 必须与同 path 的 runtime record byte-for-byte 一致，
所有允许的 tar member 除 manifest 外必须在 array 中恰好出现一次，且不得有未被 schema/global/catalog
closure 使用的 SQL/catalog/archive；`schema-bundle.json` 是唯一允许只由 top-level record 直接绑定的 member。
outer tar 的 digest 只来自 signed trust decision。

`schema-bundle.json` decoded object 必须与 manifest 中
`format_version = "cloud-agents-platform-schema-bundle/v1"` 的 `schema_bundle/schema_bundle_digest` projection
完全一致。manifest 不可用一份 inline bundle 校验 ledger、再用另一份 file bundle 校验 ancestor。

bootstrap SQL、source、module lock、fixtures、SBOM/notice/provenance 属于 signed release 的其他 subject，不进 runtime
migration tar。runner 先校验 outer artifact digest，再以 no-follow 规则一次读入 immutable memory；禁止
symlink、hardlink、submodule、device/FIFO/socket、duplicate path、absolute/backslash/`.`/`..`、隐式解码和非
`100644` file。

outer migration/bootstrap tar 都使用 deterministic uncompressed POSIX ustar profile：member 按 ASCII path bytes
升序、regular file only、mode `0644`、uid/gid `0`、空 uname/gname、mtime `0`，禁止 PAX/GNU extension、sparse、
long-name side record、重复/trailing member；结尾恰为两个 zero block。generator 与 consumer 对该 profile
byte-for-byte 测试，并在 fixture 固定 typeflag、octal numeric/checksum field encoding、header/data padding 与 end
blocks；不能把“解包内容相同”冒充 same-bits。

固定上限：artifact `64 MiB`；manifest/schema bundle/catalog 单文件 `1 MiB`；SQL 单文件 `16 MiB`；
file count `8192`；migrations `4096`；ancestors `128`；JSON nesting `64`；object members/array entries 单层
`16384`；path ASCII bytes `256`；string UTF-8 bytes `1 MiB`。ustar path 必须能在最后一个合法 `/` 分割为
不超过 `155` bytes 的 prefix 与不超过 `100` bytes 的 name；无法按此唯一取最长合法 prefix、空 segment、
尾随 `/` 或超限都拒绝。越限在数据库连接前 fail closed。

### 7. Strict JSON、RFC 8785 与 fixture envelope

manifest/schema archive/catalog 在 typed decode 前拒绝：每层 duplicate/escaped-equivalent key、unknown/missing field、
trailing token/comment、BOM、非法 UTF-8/lone surrogate、非 JSON value。数值只允许 `0` 或无前导零的正十进制
integer token，不超过 `9007199254740991`；禁止负数、`-0`、小数和指数。signed int64 只使用第 3 节
的 decimal string profile。

RFC 8785 不做 NFC/trim/case-fold。字段 grammar、array order、path/mode/size/digest 都是 admission 输入。
TS/Go 共享 fixture 位于 `migrations/fixtures/bundle/`，但 fixtures 只进 build provenance，不进 runtime tar。

非法 JSON/UTF-8/duplicate/trailing 用两文件表示：合法 `case.json` 只包含 payload relative path、raw SHA-256、
expected accept/reject/stable error；另一 `.raw` 文件保存非法 bytes。禁止把非法 payload 本身伪装成可被
strict decoder 读取的 manifest。

fixture 至少覆盖 generator canonical byte order、object member reorder 后 JCS digest 不变、duplicate key、unknown/missing、
array reorder、large/exponent/fraction/negative-zero、
invalid UTF-8/lone surrogate、NFC non-equivalence、path traversal/link/mode/size/checksum、ancestor cycle/prefix mutation/ledger
unknown digest/回跳、SemVer/build metadata/服务类型边界，以及 `0.2.0-0.0`、`0.2.0-alpha`。

### 8. SQL statement contract：窄化分类后交给 PostgreSQL 真实 parser

禁止把整份 multi-statement SQL 交给 simple-query。checked-in `postgresql-lex-v1` 只负责识别 PostgreSQL
line/block comment、standard/escape/Unicode string、quoted identifier、dollar quote 和顶层分号，不改写 bytes。
同一组件还输出 token stream，由 checked-in、fail-closed 的 narrow DDL classifier 识别 command、object kind、
subcommand、qualified object、grantee 和 statement position；它不尝试接受完整 PostgreSQL grammar。每个分段
必须通过 pgx extended protocol 的单 statement Parse/Bind/Execute，由当前
PostgreSQL server parser 做最终 grammar admission；server 认为多 statement 则拒绝。

initial profile 只允许当前 signed SQL 实际需要的：

- `CREATE TABLE/INDEX/POLICY/FUNCTION`；
- `ALTER TABLE` 的 `ADD CONSTRAINT/OWNER TO/ENABLE ROW LEVEL SECURITY/FORCE ROW LEVEL SECURITY`；
- `ALTER FUNCTION ... OWNER TO`；
- `ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner IN SCHEMA cloud_agents` 的具名 revoke；
- `GRANT/REVOKE` 只能作用于 catalog contract 列出的 schema/table/function，且 grantee 只能是
  `PUBLIC/cloud_agents_runtime/cloud_agents_bootstrap_admin`；
- `000001` 第一个 `DO` 只能以 `migration_id + statement_index + statement_sha256` 的 exact special-case profile
  放行；未来新增 `DO` 必须新 profile/ADR，不能继承“首词为 DO 即允许”。

statement slice 是 splitter 从文件 offset `0` 或上一个 top-level semicolon 后第一 byte 开始，到本 statement
terminating semicolon（inclusive）的 raw bytes；leading whitespace/comment 与 semicolon 都参与 SHA-256，terminator
后的 bytes 属于下一 segment。所有 statement 必须有 terminator；final 仅允许 ASCII whitespace/comment，禁止 empty
statement。generator/runner 必须共享 offset/hash fixtures。

classifier 明确拒绝 `ROLE/DATABASE/TABLESPACE/SYSTEM/EXTENSION/PROCEDURE/LANGUAGE/CAST/OPERATOR/
PUBLICATION/SUBSCRIPTION/FOREIGN DATA WRAPPER/SERVER` authority，以及未列出的 object/subcommand/grantee。
这不是执行不可信 SQL 的 sandbox；migration 仍必须来自 signed expected artifact。它只防止受信 migration
意外破坏 runner 的事务、role 和 lock 边界。`BEGIN/START/COMMIT/END/
ROLLBACK/SAVEPOINT/RELEASE/PREPARE TRANSACTION`、`SET/RESET ROLE|SESSION AUTHORIZATION`、`LOCK`、advisory
lock/unlock call、`CALL`、`COPY`、`SELECT`、`INSERT/UPDATE/DELETE/MERGE`、`VACUUM`、`CLUSTER`、`REINDEX`
与未知类型均在执行前拒绝。字符串/comment/dollar body 中的关键字不得误报；真实顶层命令
不得靠字符串搜索放行。`DO`/function body 中的 transaction control 由 PostgreSQL 在显式事务中拒绝。

每个 statement 后、ledger insert 前必须在同一 `pgx.Tx` 内验证：

- `PgConn().TxStatus() = 'T'`；
- `current_user = cloud_agents_migration_owner`；
- 当前 backend 仍持有 exact session advisory lock；
- ADR-0008 完整 role/membership/role-config/database owner/ACL 与 statement 前快照一致；
- schema owner 与 `pg_default_acl` 只能发生 classifier 对该 exact statement 声明的窄化 transition；非
  `DO/ALTER DEFAULT PRIVILEGES` statement 必须保持不变，每个 transition 后立即核对 expected intermediate state。

这些 per-statement 检查不是 final cumulative catalog validation；多 statement migration 尚未完成时不得拿最终
contract 拒绝合法中间态。整条 SQL 完成后、ledger insert 之前才执行 post-DDL validation。每个 entry 的
`predecessor_catalog_contract` 与 `catalog_contract` 分别指向执行前、成功后完整 head 的 cumulative exact
contract，并使用 `cloud-agents-platform-catalog/v1` typed projection：

- schema owner/ACL、owner 在该 schema 的 default ACL；
- relation 的 schema/name/`relkind`/persistence/access method/owner/ACL/reloptions/replica identity、
  `ENABLE+FORCE RLS`；
- column 按 `attnum` 的 name、type schema/name/typmod、collation、nullability、identity/generated/default、storage/
  compression；
- constraint 的 name/type、columns、referenced relation/columns、match/update/delete action、deferrable/deferred/
  validated 与 expression AST；
- index 的 name/access method、key/include columns、opclass/collation/order/nulls、unique/primary/valid/ready/live、
  predicate/expression AST；
- policy 的 name、permissive、command、role identities、USING/WITH CHECK AST；
- trigger 的 name/function identity/enabled/type/columns/args/WHEN AST/internal flag；
- function/procedure 的 full identity、kind/language、argument/return types/modes/default count、owner/ACL、
  `SECURITY DEFINER`、volatility/parallel/leakproof/strict/config/cost/rows、`prosrc` exact SHA-256 与 `probin`；
- sequence/view/materialized view/foreign table/partition/type/operator/cast/extension 等 object 的显式 allow/deny；
- 未列 object、字段、ACL、policy、trigger、function 或额外 write principal 一律拒绝。

expression 不比较 `pg_get_*` pretty text。catalog contract 保存 version-neutral
`cloud-agents-sql-expression/v1` typed AST（column/function/operator 都用 schema/name/signature identity）；runner 的
PG15/16/17 adapter 把 `pg_node_tree`、catalog dependency 与 attribute mapping 投影到该 AST，拒绝未知 node type。
default/generated/check/policy/index predicate/expression/trigger WHEN 都走同一 normalizer；每个 major 的 raw catalog
fixtures 与共同 AST 必须在 matrix 中等价。普通 `is_valid_identifier()`、`require_tenant_id()` 与
`bootstrap_platform_tenant()` 都受上述 full function projection 保护，不能只检查 `SECURITY DEFINER` 函数。

user-created trigger 继续使用 exact name。`tgisinternal=true` 且绑定 declared constraint 的 trigger 禁止把生成名/
OID 纳入 identity，改用 owning constraint schema/name、owning relation、event/action、deferrability、resolved function
signature、column/args logical values；未绑定已声明 constraint 的 internal trigger 拒绝。自动生成的 table composite/
array type、TOAST relation/index 等同样以 owning declared relation + semantic kind 归一，排除 OID-derived name，
并验证 owner/ACL/options；其他 internal object 拒绝。fixture 必须证明同一 SQL 在两个 fresh DB 以及 PG15/16/17
得到相同 version-neutral projection。

`cloud-agents-sql-expression/v1` 的 Const 必须通过 server-deparsed expression 的 version-pinned PostgreSQL grammar
adapter 解码为 typed logical JSON value，再结合 catalog dependency 解析 type/operator/function identity；pretty text
只作 parser input，不作 comparison。禁止把 `nodeToString` 的 raw Datum bytes、OID 或 host endian 表示放入共同 AST。
linux-amd64 与 linux-arm64 都是 Platform release 前的必测 projection 架构。catalog 扫描范围只包含
`cloud_agents` namespace、自身 default ACL、declared object 的 internal dependency closure，
以及 authority contract 明列的 database/role records。`pg_catalog` built-in type/operator/cast/opclass/collation/function
仅作为 resolved identity/dependency anchor，不要求复制或枚举整个系统 catalog。

与 ledger schema identity 分开的 `authority_contract` 则完整绑定 ADR-0008 的 migration LOGIN/group role
attributes、direct/recursive membership、grantor/per-grant inherit/set/admin option、`rolconfig`、database owner/ACL、
`pg_db_role_setting`、CREATE/TEMP effective privilege 与 bootstrap/schema-adoption precondition。runner 在连接后、
持锁后、每个 statement 后及 ledger insert 前都重算该 projection；任何 role/database authority drift 都 rollback/
fail closed，但更新这个 contract 不要求伪造 schema migration。

initial schema 未列出 sequence/view/materialized/foreign/partition relation，因此出现即拒绝。validation 失败会 rollback
SQL effect；只有 catalog、transaction、role 和 lock 全部稳定后才插入 ledger 并 commit。启动时若 ledger
已有连续 prefix，runner 使用最后一条已应用 entry 的 cumulative contract 验证当前数据库，而不是拿早期 contract
错误地拒绝后续 migration 已声明的对象。

### 9. Runner 连接、锁和事务顺序

runner 只使用一个短生命 dedicated `pgx.Conn`，禁止 pool/runtime connection。顺序固定：

1. 先用配置的 trust root 验证 signed candidate、expiry/revocation 与 runner self identity，再读取 runtime tar；
   校验 outer SHA、limits、strict manifest/schema/archive/SQL/catalog、`manifest_digest`、
   `schema_bundle_digest`、bootstrap descriptor digest 和 runtime record closure，并将执行 closure 一次读入内存；
2. 确认 expected digests/release identity 与 artifact 一致后才连接 exact target DB；
3. 在未切 role 时验证 server `server_version_num`、database/session user 与完整 `authority_contract`；
4. `SET ROLE cloud_agents_migration_owner` 并核对 `current_user`；
5. 固定并 read back `client_encoding=UTF8`、`standard_conforming_strings=on`、`TimeZone=UTC`、
   `search_path=pg_catalog`、bounded `statement_timeout/lock_timeout/idle_in_transaction_session_timeout`；
6. 在 target DB 取得 `-1047838957622507638` session advisory lock，等待受 context deadline/cancel 限制；
7. 持锁后重做完整 authority contract/schema owner/ledger preflight；未持锁不读写 ledger；
8. 每个 pending entry 前重做完整 authority contract/schema catalog/lock/head，然后开启独立
   `SERIALIZABLE READ WRITE` 短事务，
   按第 8 节执行 statement、postcondition、ledger insert 并 commit；
9. commit 后重读 exact ledger/head；全部完成后 unlock、`RESET ROLE`、核对 user，关闭 connection。

official bootstrap/provisioning CLI 必须在同 target DB 使用同一 advisory key，或在进入现有 bare
bootstrap SQL 前取得等强的部署级互斥；bare SQL 不声称自带 concurrency receipt。即使有外部互斥，
runner 仍在 lock 后和每条 entry 前重验权限，不信任进程内缓存。

### 10. Crash、ambiguous commit 与 replay

每条 transactional migration 的全部 SQL effect、postcondition 与 ledger row 共享一个 commit boundary：

- `BEGIN` 前 crash：无 effect，重启后重做 artifact/role/lock/ledger validation；
- statement/postcondition/ledger insert 后、commit 前 error/crash：整条 rollback；
- commit acknowledgment 丢失：关闭旧 connection，用新 dedicated connection 重做 trust/role/lock/ledger 并取得
  lock。若 target exact row 已存在，完整 ledger 仍是合法 prefix，且数据库等于当前 ledger head 的 cumulative
  contract，则认定该 entry 已提交或已被另一 runner 合法继续推进；若 target row 不存在、ledger 仍停在它的
  predecessor，且数据库 exact 等于 `predecessor_catalog_contract`，才按 pending 重试；其他状态全部 fail closed。
  `000001` 的 predecessor contract 是上文 discriminated `schema_absent|empty_schema` exact state；
- commit 后、下一 entry 前 crash：从 exact ledger prefix 继续；
- unlock/reset/close 响应丢失不改 ledger，但 runner 重连验证 final head 后才返回成功。

retry 只覆盖 bounded connect/serialization/deadlock/connection-loss。permission、trust digest、manifest、schema、role、
ledger、catalog/postcondition 错误永不自动重试成成功。任何 error/cancel/panic 路径都 best-effort
unlock/reset，并无条件关闭 dedicated connection。

### 11. Tenant transaction helper 是 runtime boundary，不是 migration identity

helper 显式从 runtime `pgxpool` acquire 一条 physical connection，再以
`pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable}`
`BeginTx`。
第一条业务前置使用参数化
`set_config('cloud_agents.tenant_id', $1, true)`，随后调用 `require_tenant_id()` exact readback。callback 只获得
tenant-bound typed store capability；不得暴露任意 SQL string 的 `Exec/Query`、pool/raw conn/pgx Tx、role switch、
session SET 或 nested transaction。底层 SQL 只能存在于 store-owned typed method 内，不能由 HTTP/SDK/adapter caller
注入。

callback 正常返回时 commit；返回 error 时 rollback 并保留原 error；panic 时在 bounded cleanup context 中
rollback/处置连接，然后以原 panic value re-panic。只有 commit 或 rollback 明确成功、physical connection
`TxStatus = Idle`，且 transaction 外 exact readback `current_setting('cloud_agents.tenant_id', true)` 为 `NULL`
或空字符串时才 `Release` 回 pool；任何非空值都销毁连接。production helper 不故意调用一个必然抛错的
`require_tenant_id()` 作为每次 release 条件。
acknowledgment unknown、rollback failure、panic/cancel 后状态不明或 TxStatus 非 Idle 时，必须 `Hijack` 并
`Close`，不得返回 pool。Go type 不能阻止 callback 保存 interface；实现必须在 callback 返回后
invalidate handle，并测试之后的任何操作都稳定失败。

helper 不 trim/lowercase/path-clean tenant ID，不自动重放 mutation callback。conformance 在 PG15/16/17 分别覆盖
首次连接 `NULL` 与复用连接 empty-string，并使用单连接 pool 再次 borrow 同一 physical connection，第二次证明
GUC 仍为 cleared state；再调用 `require_tenant_id()` 证明两种 cleared state 都稳定返回 SQLSTATE `22023`，
且 error 后连接仍为 Idle。P1-A2.1
仍只有 tenant read，不提前实现 A2.2/A2.3 writer。

### 12. Build provenance、shared fixtures 与 release 边界

build checker 使用 fixed Node 24.13.1/Bun 1.3.14/Go 1.26.6 从 clean source 生成 schema/manifest/bootstrap
digests 与 runtime tar。`runner_release_digest` 的 signed provenance 必须包含：source commit/tree、runner binary/
image digest、selected Go dependency closure/完整 `go.mod/go.sum`、generator/checker/lexer/catalog validator/tenant helper
source closure、fixtures、SBOM、THIRD_PARTY_NOTICES、secret/vulnerability evidence。

source path 与 runtime artifact path 映射只在 build provenance 中；`schema_bundle` 绑定 runtime artifact path/mode/
size/digest。release 之前必须对 tar 安全解析、runner trust input、TS/Go canonical bytes、PG15–17 catalog 查询
和 packaged binary 做 same-bits replay。

## 被否决的方案

### 一个 digest 同时做 ledger、bootstrap、runner 与 tenant helper identity

否决。它使同 head 安全修复变成伪 migration，也使数据库 provenance 与编译产物寿命周期混在一起。

### 只重算 bundle 自报 digest，不要外部 trust anchor

否决。内部一致性不等于授权真实性。production runner 必须同时验证 signed expected identity。

### 整份 SQL 使用 simple query，执行后再检查

否决。嵌入 transaction-control 可以先提交 DDL。必须 grammar-aware 分段、server extended Parse 和
transaction 内 postcondition，再写 ledger。

### 用 pool 跑 migration 或把全部 lineage 包在一个长事务

否决。pool 会泄漏 role/session-lock authority；长事务会放大锁、rollback 与恢复边界。

### 当前 runner 声称已证明 exact bootstrap bytes 执行

否决。initial schema 无 durable bootstrap receipt，当前只能证明 resulting catalog state。

## 结果与未关闭边界

本 ADR 仅把 `4f39b14` 的 SQL-design 输入收窄为可实现的 schema identity、signed runtime artifact、
runner 和 tenant transaction contract。它未修改 `000001/000002` bytes，未创建 manifest/runner/store，也未将本地
SQL 验证升级为 migration evidence。

以下仍为 open：

- schema/manifest/bootstrap JSON、catalog contracts、raw fixtures、TS generator/checker、Go decoder/lexer/runner/helper；
- signed trust input、runtime tar producer/consumer、runner release provenance/SBOM/notice、完整 dependency review；
- digest-pinned PostgreSQL `15.18`、`16.14`、`17.10` role/RLS/migration/concurrency/crash/recovery matrix；
- P1-A2.2 Membership/RBAC、P1-A2.3 durable coordination、P1-A2.4 compatibility/recovery、P1-A3 SDK/Identity；
- `G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` 与全部 aggregate Gate closure。

因此 `Accepted` 仅表示已通过独立复审的实现输入被冻结，不代表 release、publication、Compose/Helm、云端/
生产部署、数据库写入、Platform RC、Beta/GA，也不授权开始 P2–P6。
