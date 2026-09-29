# 数据迁移、删除与回滚安全要求

从旧迁移计划（旧 P0～P6）中保留的仍然有效的安全条件。实际涉及旧数据迁移、消费者切换、破坏性回收或回滚时必须遵守；它不是当前实施顺序，当前顺序见 [04](04-extraction-and-migration.md)。

## 迁移原则

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

## 数据切换

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

### Migration ledger 与历史读取

- 所有 legacy/public aggregate 使用 namespace-qualified ID，不假设旧 UUID 全局无冲突；
- migration ledger 记录 aggregate kind、legacy/public ID、source version、writer epoch、cutover/drain/EOL 状态；
- read router 按 ledger 读取 public 或 legacy 历史，禁止通过“读不到就双写补一份”；
- audit/retention/export 在兼容期能跨 public/legacy 汇总，并标明 source/writer；
- legacy writer 在最后活动聚合 drain 前继续接受安全修复，但不增加新功能；
- 每个 cohort 有 decommission deadline、延期 owner 和数据删除/保留批准。

### 数据库迁移与 rollback

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

## 迁移到公共仓后的删除规则

某能力只有满足以下条件才从 Synara 删除：

1. 公共源码/测试/SDK/Release 已固定；
2. Synara client/adapter 使用公共 API；
3. shadow/canary/cutover/failback 通过；
4. 活动 legacy 数据已 drain 或进入明确兼容期限；
5. data retention/DR/audit 已批准；
6. 删除后全仓搜索无第二个可编辑实现。

删除的是重复公共实现，不是 Synara 企业扩展和历史数据读取义务。

## 回滚

- Runtime 与 Platform 分开回滚；
- 公共 CP rollback 必须保持 schema N/N-1、outbox cursor 和 active generation 可读；
- 新 Session/Lease admission 可停止，活动聚合不得换 writer；
- endpoint/grant/workload 先 revoke/drain，再降级/卸载；
- failed cleanup 继续 reconciliation，不能因 rollback 丢 finalizer；
- 每个阶段维护明确 RPO/RTO、backup/restore 和数据修复 runbook。
