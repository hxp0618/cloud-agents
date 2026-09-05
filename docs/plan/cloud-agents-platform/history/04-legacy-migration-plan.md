# 旧 P0～P6 提取、数据迁移与消费者切换计划

来源：`ed7d3ac5:docs/plan/cloud-agents-platform/04-extraction-and-migration.md` §1–§6。只在当前任务确实涉及旧迁移、消费者切换或回滚时读取；不是当前实施顺序。当前执行计划见 [04](../04-extraction-and-migration.md)。数据迁移、破坏性删除与回滚安全要求保留；相对链接仅调整为新目录。

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

P1 的数据与传输 foundation 按 [ADR-0007](../../adr/0007-p1-contract-data-toolchain-foundation.md) 冻结：

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
