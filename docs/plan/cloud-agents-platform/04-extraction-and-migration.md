# 04. Go Control Plane 提取与宿主迁移

## 1. 迁移原则

- 先 inventory 和 characterization，后搬代码；
- 按能力迁移，不按目录整仓复制；
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

## 3. 阶段

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

公共 migration 使用全新 lineage，不复制 Synara migration 编号；baseline 分域至少覆盖：tenant/org/project/
membership、idempotency/outbox/leader/operation receipt、managed-agent session/turn/execution/event/interaction、
worker identity/generation/release/attestation、workspace/materialization/checkpoint/cleanup、artifact、credential/
broker、target/quota/usage、managed-host lease/workload/volume/endpoint、adapter registry/finalizer、platform
manifest/compatibility。

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
- 每个 migration 有 checksum、全局 lock、schema compatibility range 和重复执行语义；
- backfill 按 durable cursor/batch 可暂停恢复，并有 mismatch/reconciliation report；
- Release manifest 固定最低可回滚 binary/schema 版本；contract 前验证旧 binary 已退出支持窗口；
- irreversible migration 必须有 freeze/批准、PITR restore point 和 restore drill；
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
