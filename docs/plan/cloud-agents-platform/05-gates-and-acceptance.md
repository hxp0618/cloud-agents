# 05. Gate 与验收

## 0. 底座就绪验收 BASE-READY

依据 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)，第一阶段是基础设施＋完整 Admin Web，两者共同通过 BASE-READY 后再交付用户 CloudAgents 对话。
BASE-READY 是本次提出的底座产品就绪检查，不是新增审批工具、历史 Gate 重命名或发布批准。
实施顺序见 [04](04-extraction-and-migration.md#0-当前实施顺序底座先行)；实际状态在 [06](06-status-tracker.md) 登记。

no-Agent 指不依赖用户对话、Coding Agent Runtime 或 Codex/Claude 凭据，不指没有 Admin Web；RemoteWorker/SandboxAgent 等基础设施进程不在此处排除的 Coding Agent 范围。下面十二项必须同时满足；其中 11/12 与后端条目同为必需条件，CLI/SDK 实测不能替代管理页面验收：

1. 独立 API/SDK/CLI 创建 Workspace 和 Sandbox，真实 Exec/PTY/Files 可用；用户 CloudAgents、Synara/T3
   均不是底座安装或验收前提。
2. 停止、TTL 到期或替换 Sandbox 后 Workspace/Volume 保留；重新启动可读回测试文件及摘要。
   删除工作区是独立授权动作，按声明的快照/保留规则执行，不误删其他 Workspace 或用户已有资源。
3. API 接受后即使 HTTP 断连、CP/Controller 重启、receipt 丢失，持久化 Operation 仍能自动恢复到明确终态；
   相同幂等键不重复创建，部分分配被 adopt/补偿，失败清理保留 finalizer 并可观察。
4. Workspace 单写与 generation fencing、卷归属、挂载授权、旧节点重连和过期命令负向矩阵通过；
   不能只凭健康超时强制把未 fence 的卷交给新 writer。
5. Preview 私有默认、PTY/Files 可重连、SSH 短期授权；跨租户、路径/symlink 逃逸、任意目标代理、
   过期/吊销/旧 generation 访问均被拒绝，Gateway 重启恢复行为与缓冲上限有实测。
6. 客户节点只通过 outbound 接入，完成注册/轮换/Drain/断线/重连/对账；平台无客户公网入站 SSH 依赖。
   节点只接收 ownerScope 允许的任务，离线不误删数据。
7. Docker、Kubernetes、客户节点各有真实执行与恢复证据；能力矩阵包含 runtime/arch/存储/网络限制，
   不支持组合拒绝，不以 Probe、Mock 或单一路径替代全部矩阵。
8. 身份/RLS/RBAC、容量与配额准入、受控 DNS、metadata/宿主/控制面/跨租户网络隔离实际生效；
   共享不可信租户必须有对应强隔离实证，不能用可信 runc 路径冒充。Secret 不进页面、日志、receipt 或快照。
9. Workspace 文件系统快照恢复到新 Volume/Sandbox 并核对数据；数据库备份恢复、证书轮换、
   N/N-1 升级回滚、节点故障恢复和孤儿资源清理有可复现记录，不声称未验证的进程内存或跨 Region 恢复。
10. 基础用量事实、长运行 checkpoint、离线对账、修正审计可核对；健康、调谐积压、容量、失败与告警可观察，
    有实际延迟/soak/恢复测量和 runbook，不将工程目标直接写成 SLO 承诺。
11. Admin Web 对本阶段真实资源提供完整的配置、监控、维护、失败恢复及 Operation/Audit；管理员不读取用户内容，
    普通用户不能调用 Admin API；危险操作保留影响范围、资源名称和 generation 确认。
12. Admin 必交付页面完成 [07 的 BASE-ADMIN-V1](07-admin-web-requirements-and-design.md#base-admin-v1) 全部条件，包括双语、可访问性、视觉与身份部署隔离；
    用户侧 Agent 流程留在 APP-M1 验收，不把其尚未接入当作通用底座失败。

每项记录 source/dirty、backend/runtime/制品版本、输入、命令、实际结果和恢复边界。复用已有检查和证据路径，
仅重验被改动影响的结论，不为每个切片先建设新的证据生成器。证据在执行后如实记录，不能要求先有通过报告再开始实现。
BASE-M5 只有逐项满足以上条件才能标为就绪；未完成项保持开放，不降为隐藏按钮或文档占位。

### 0.1 与旧 Gate、Release 的关系

下文旧 P0～P6/Platform RC 的 record、签署、审批、安全和发布条件保持不变，不因底座优先而自动关闭或降低。
底座本地就绪不要求先完成 Synara/T3 或真实 Agent E2E，但也不代表通过仍包含这些条件的旧完整 Platform RC。
若以后发布独立底座 channel，其适用验收与曝光范围须显式记录并取得原有发布批准；本次不创建或批准该 channel。
APP-M1 再执行 Managed Agent 的真实 Codex/Claude 与交互/历史/Artifact 验收，后续消费者各自关闭适用 Gate。

原 Admin M1～M4 任务另按 [ADMIN-WEB-V1](07-admin-web-requirements-and-design.md#admin-web-v1) 验收；其中既有 Agent 的真实 Codex/Claude E2E 仍是原任务必需项，不因 BASE 的 no-Agent 范围而豁免。ADMIN-WEB-V1 与 BASE-READY 不互相替代，也不互为启动或完成的通用前置条件；只有明确迁移后的任务才改用 BASE 范围。

## 1. Gate 总表

以下保留旧完整 Platform/消费者的正式 Gate 定义与批准要求，仅在执行对应 Gate 或该范围变更时适用；不是所有 BASE 日常工作必须先关闭的清单。BASE 联合交付的完成条件由上节定义，不新增一轮泛化审批，也不降低这里任何正式 closure 条件。

| Gate                 | 阻塞                | 退出证据                                                                                                                             |
| -------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `G-INVENTORY`        | P0                  | frozen ref 的全量 code/SQL/schema/build/deploy/generated manifest、分类、source/tree hash、authority、license/secret provenance 完整 |
| `G-BASELINE`         | P0/M1 phase records | P0 characterization 与 M1 真实 Provider baseline 分别关闭；aggregate 只在两个 record 同时有效时关闭                                  |
| `G-CONTRACT`         | P1                  | JSON Schema/OpenAPI 与 Proto/Connect/gRPC authority、TS/Go SDK、server validator、mapping/golden/negative fixtures 同源              |
| `G-DATA`             | P1                  | PG15–17 fixed-patch/digest matrix、forward migration、tenant RLS、idempotency/outbox/leader、本地 logical backup/restore、N/N-1      |
| `G-AUTHORITY`        | P1–P6 phase records | 三种模式 owner 唯一；无 Session/Turn/Lease/Workspace 双写                                                                            |
| `G-MANAGED-AGENT`    | P2                  | Session/Turn/Execution/Worker/Workspace/Artifact/Credential real E2E                                                                 |
| `G-WORKER-FENCING`   | P2/P3 phase records | stale generation 无法 heartbeat/ready/取密/发 endpoint/提交终态；revoke/reap 证据完整                                                |
| `G-MANAGED-HOST`     | P3                  | reference host 的 Lease/Generation/workload/volume/endpoint/grant/cleanup 与 signed descriptor conformance                           |
| `G-ADAPTER`          | P2–P4 phase records | built-in 与外部 Platform Adapter protocol conformance、mTLS、幂等、deadline、receipt                                                 |
| `G-SECURITY`         | P1–P6 phase records | tenant isolation、五类身份、SSRF/DNS、secret/log/cache、path/symlink、rate limit、downgrade、host cutover                            |
| `G-OPS`              | P4                  | DB/leader/outbox/retry/orphan/partial create、HA、backup/restore、upgrade/rollback、SLO/runbook                                      |
| `G-STANDALONE`       | P4                  | fresh Compose 与 Helm 在无 Synara 私有依赖下完成真实 Codex/Claude Turn 与 cleanup                                                    |
| `G-SYNARA-CUTOVER`   | P5                  | shadow/canary/single-writer/failback、legacy drain、重复公共源码删除                                                                 |
| `G-T3-INTEGRATION`   | P6                  | embedded 不回归；managed direct/relay proof-bound，Bearer 拒绝，真实 T3 E2E/soak                                                     |
| `G-SUPPLY-CHAIN`     | Release             | module/tag/image/chart/SDK/host descriptor digest、SBOM/provenance/signature/license/secret/CVE/VEX/base-image gate                  |
| `G-PLATFORM-RELEASE` | RC                  | 同一 platform manifest 的 Synara/T3/standalone E2E、install/upgrade/rollback 全闭合                                                  |
| `G-EXPOSURE`         | Beta/GA             | 用户范围、支持等级、channel、回滚、事故响应与人工批准                                                                                |

## 1.1 Progressive Gate 语义

跨阶段 Gate 不允许在早期阶段被一次性关闭。阶段只产生 immutable phase record，聚合 Gate 只在全部必需
record 同时有效时标记 `VERIFIED`：

| Aggregate Gate     | Required phase records                         |
| ------------------ | ---------------------------------------------- |
| `G-BASELINE`       | `G-BASELINE-P0`、`G-BASELINE-M1`               |
| `G-AUTHORITY`      | `G-AUTHORITY-P1` … `G-AUTHORITY-P6`            |
| `G-SECURITY`       | `G-SECURITY-P1` … `G-SECURITY-P6`              |
| `G-ADAPTER`        | `G-ADAPTER-P2`、`G-ADAPTER-P3`、`G-ADAPTER-P4` |
| `G-WORKER-FENCING` | `G-WORKER-FENCING-P2`、`G-WORKER-FENCING-P3`   |

phase record 状态只允许 `NOT STARTED`、`IN PROGRESS`、`VERIFIED`、`INVALIDATED`。每个 record 固定前置
record、authority scope、source/dirty/toolchain、contract/module/SDK/image/migration/manifest digest、原样命令、
逐项结果、DRI/独立 reviewer 与 downstream invalidation rule。阶段 Exit 只依赖该阶段 record，不声称未来阶段
已验证；Platform RC 才验证上述 aggregate Gate。

`G-BASELINE-P0` 只验证固定 ref 的 Synara legacy managed-agent、T3 embedded、可复用机制实际
characterization，以及 greenfield Managed Host 的 immutable spec/negative/reference-host oracle；它不得要求或
声称真实 Codex/Claude M1 行为。`G-BASELINE-M1` 才验证 Protocol 2.2/2.3、真实 Codex/Claude
happy/auth/rate-limit/unavailable/resume、SendTurn/workspace/checkpoint/reconnect 的同输入基线。P0 Exit 只依赖
`G-BASELINE-P0`；M1/Platform RC 才要求 aggregate `G-BASELINE`。

P1 的 contract、data、authority 与 security 边界以
[ADR-0007](../adr/0007-p1-contract-data-toolchain-foundation.md) 为决策 authority；ADR digest 或其中冻结的 wire、
schema、database、tenant isolation、identity 边界变化时，四个 P1 record 一并失效并重新复核。

失效规则至少为：contract/core/store 改动使 P1–P6 record 失效；Worker/adapter 改动使相关 P2–P6 record
失效；Standalone/security/ops 改动使 P4–P6 record 失效；Synara adapter/cutover 改动使 P5–P6 record
失效；T3 descriptor/proof/connection 改动使 P6 record 失效；任何重打包或 digest 变化使 same-bits record、
`G-SUPPLY-CHAIN` 与 `G-PLATFORM-RELEASE` 失效。旧 record 保留，不覆盖历史。

安全状态变化即使 bits 不变也会失效：新 applicable KEV/reachable Critical/High、waiver 到期、scanner DB
超过 24 小时、signer/trust root/release identity 撤销、base image EOL/revoke，都会把
`G-SUPPLY-CHAIN` 与引用它的 `G-PLATFORM-RELEASE` 标为 `INVALIDATED`，暂停未批准 exposure，并要求按新
数据库/信任根重扫、重新签署与重跑受影响 same-bits Gate。不得因 artifact digest 未变沿用旧安全结论。

## 1.2 P1 精确退出标准

### `G-CONTRACT`

- JSON Schema 是 management/agent/host JSON 数据模型的唯一 authority；OpenAPI 只定义 route、status、header
  与 schema ref，OpenAPI bundle 不包含漂移的内联副本；
- Worker/Platform Adapter 的 wire model 以 versioned Proto 为唯一 authority，并生成 Connect/gRPC server/client
  mapping；descriptor set、OpenAPI bundle、JSON Schema bundle 与 SDK digest 均进入 closure record；
- TS SDK、Go SDK、Go server validator 和映射层通过同一套 golden、negative、N/N-1 compatibility fixtures；至少覆盖
  unknown field、enum/version downgrade、stable error、watch cursor、idempotency key、deadline/cancellation 和
  显式共享语义类型的 Proto↔domain↔JSON mapping round-trip；不要求或允许为所有 Proto RPC 另建 JSON wire；
- 从全新外部 consumer 安装 exact-pinned SDK 后可编译并调用 fixture server；仓库外不得依赖 workspace/file/git
  dependency，生成后 diff 必须为零；
- 任一 schema、Proto、OpenAPI、generated SDK 或 mapping fixture digest 改变都会使该 record 失效。

### `G-DATA`

- Postgres `15`、`16`、`17` compatibility matrix 全部通过；每个 major 固定 patch version 与 OCI image digest，
  closure record 保存实际 `server_version` 和 digest；
- persistence 仅使用 `pgx/v5`、`pgxpool` 和手写 SQL；自动检查证明没有 GORM、`AutoMigrate`、ORM schema
  generation、Synara migration 编号或 legacy schema authority；
- 新 migration ledger 完成 `expand -> resumable backfill -> code cutover -> contract`，逐项验证 immutable
  checksum、Postgres advisory lock、重入、并发执行、crash/resume、checksum drift/unknown migration fail closed；
- 所有 tenant-owned table 具有 composite tenant FK、`ENABLE/FORCE RLS`；runtime role 非 owner、无
  `BYPASSRLS`，事务使用 `SET LOCAL` tenant context。缺失 context、伪造 tenant、跨 tenant join/insert/update/
  delete 和 connection-pool context 泄漏测试均拒绝；global table 仅限固定 allowlist；
- durable live-instance registry 的 heartbeat、schema compatibility range 和 drain state 可阻断 contract；N/N-1
  rolling matrix 通过。unknown、stale-but-not-expired，以及 expired 但没有同时证明同 incarnation/generation
  process termination + fencing + endpoint/credential revoke + claim/leader release 的 durable retirement receipt
  的实例均 fail closed；只有完整 retirement receipt 才能从 live set 排除；
- P1 在本地固定输入完成 logical backup/restore、migration replay、outbox/idempotency/leader 恢复并核对数据 digest。
  P1 还须验证 preflight 会在缺少匹配 release/schema digest 的 PITR restore point 或有效 restore-drill record 时
  fail closed；部署级 PITR restore point/drill、HA 与 failover 实证明确保留给 P4，不能用 P1 record 冒充
  `G-OPS`。

### `G-AUTHORITY-P1`

- Tenant/Organization/Project/Membership/basic RBAC、provider catalog、contract、migration、idempotency/outbox/
  leader/operation receipt 各自只有一个声明 writer 与一个持久化 authority；legacy Synara 只作为行为 oracle，
  不作为公共 write path 或 schema authority；
- JSON Schema/OpenAPI、Proto/Connect/gRPC、SDK/generated code 与数据库 migration 的 authority/derived-from 关系
  可机读并固定 digest，禁止手写双 authority 或 server/SDK 双写；
- tenant transaction context、migration owner/runtime role、global-table allowlist 和 durable live-instance registry
  的 owner 明确；故障、重试和回滚测试证明不会切换活动 aggregate writer；
- `G-AUTHORITY-P1` 仅覆盖 P1 foundation；Session/Turn/Worker claim、Lease/workload/pairing/T3 session 等后续
  writer 不在本阶段被提前声明为已验证。

### `G-SECURITY-P1`

- composite tenant FK 与 FORCE RLS 的正反向隔离矩阵通过，runtime role 非 owner、无 `BYPASSRLS`，migration
  owner credential 不进入 runtime 配置、日志、fixture 或制品；连接池复用不会泄漏 `SET LOCAL` tenant context；
- management、service account 与 workload identity 的 issuer/audience/subject/scope/tenant/project/version/expiry
  validation fail closed；签名 key rotation、unknown `kid`/algorithm、revoke 与 clock-skew negative fixtures 通过；
- secret、credential、token、pairing material 不进入 durable receipt、outbox、audit、log、trace、fixture、backup 或
  generated SDK；错误响应、watch cursor 和 stable error 不泄漏跨 tenant 标识或内部 SQL；
- SQL parameterization、request/body/decompression limit、rate limit、deadline/cancellation、watch backpressure、
  dependency/license/secret scan 通过；waiver 必须有 owner、范围和到期时间；
- P1 record 只证明 contract/data/auth foundation，不冒充 P2 Worker secret access、P3 Managed Host pairing、P4
  deployment hardening 或 P5/P6 host cutover/proof-session 安全结论。

## 2. Managed Agent 必测

- Session/Turn create、idempotent retry、interrupt、approval/user-input；
- Runtime crash、Worker replacement、Control Plane restart/outbox replay；
- Workspace edit、checkpoint、Artifact、credential revoke；
- late terminal、sequence gap、backpressure、resume cursor；
- Codex 与 Claude 真实 Turn、rate limit/auth/unavailable；
- 双 Tenant/Project/Provider 并发隔离和有界 soak。

## 3. Managed Host 必测

- CloudEnvironmentLease create/ready/terminate；
- P3 reference host 的 workload/volume/endpoint/grant/cleanup；
- signed HostWorkloadDescriptor allowlist、compatibility、expiry/revoke/downgrade；
- P6 T3 server/Runtime/Terminal/filesystem 同 Workspace；
- P6 direct 与 relay proof challenge/exchange；
- pairing token single-use/no-store、Bearer downgrade 拒绝；
- pairing ephemeral response 不进入 DB/WAL/backup/outbox/audit/log/trace/watch/webhook；丢失响应先 revoke 再
  remint，并发 consume 只有一个成功；`issued/delivery-attempted/consumed` receipt 不冒充 secret 已送达或
  session ready；
- T3 hidden ref/diff/revert、server/browser reconnect；
- old generation endpoint/session/grant/heartbeat 全拒绝；
- partial allocation、failed cleanup、orphan reaper；
- lease 内双 Provider/双项目隔离与 soak。

## 4. Standalone 必测

- 全新机器仅使用 public source/artifact；
- Compose bootstrap、health、真实 Turn、升级、回滚、卸载；
- Helm external Postgres/S3/OIDC、rolling upgrade、backup/restore；
- public images 不依赖 Synara registry/config/secret；
- docs 示例无真实 credential/endpoint；
- default-deny network/identity、non-root、read-only rootfs 与最小 capability。
- initial admin/OIDC bootstrap、显式 Compose/Kubernetes Provider secret、master-key rotation；
- local container actuator 不向 CP/Worker 暴露 Docker socket；

## 4.1 Security/operations 细项

- OIDC issuer/audience/clock skew/JWKS rotation、membership suspend/deprovision；
- signed `LeaseAuthorizationSnapshot` 的 audience/epoch/generation/TTL/refresh；CP↔Supervisor/T3 分区、
  signer rotation/revoke、stale snapshot、现存 HTTP/RPC/WebSocket 在 60 秒硬上限内 fence/close；
- service account、Supervisor/Worker/actuator identity issuance/rotation/revoke；
- Kubernetes ServiceAccount/RBAC/PodSecurity/seccomp/network/egress policy；
- at-rest envelope key rotation、broker grant、webhook signature/replay；
- quota/fairness/noisy-neighbor、outbox DLQ/replay/retry storm；
- metrics/log cardinality与 redaction、alert/runbook、PITR/灾备演练；
- P5/P6 host adapter/client、cutover routing 与 proof session 攻击面。
- Go/TS/OCI/CLI/chart/workflow vulnerability scan、scanner DB freshness、KEV/reachable Critical/High policy、
  VEX 与 time-bounded waiver；base image digest/signature/provenance/support window；

## 5. Closure record

每个 Gate 必须记录：

- evidence ID、DRI、独立复核人、日期；
- public repo、Synara、T3 commit 与 dirty state；
- Go/Node/Bun/pnpm/Provider CLI/SDK/Postgres/Kubernetes 版本；
- contract/module/image/chart/platform manifest digest；
- reference-host/T3 HostWorkloadDescriptor、producer signature/trust identity、image/bundle digest；
- SBOM/provenance/signature/VEX/vulnerability report、scanner/DB timestamp、waiver/base-image digest；
- 原样命令或 CI job、脱敏日志、artifact 地址；
- 每条 exit criterion 的 pass/fail；
- failure、waiver、rerun 与 rollback evidence；
- 签署结论。

没有 closure record 不得把 `IN PROGRESS` 改成 `VERIFIED`。

## 6. Release 与 Exposure

顺序固定：

```text
source gates
  -> immutable candidate manifest
  -> same-bits standalone/Synara/T3 E2E
  -> digest recheck
  -> G-PLATFORM-RELEASE
  -> RC
  -> independent G-EXPOSURE
```

公开 GitHub/source 与当前 Runtime prerelease 是历史已发生状态。新的 Platform Go module、container、chart、
部署 channel、internal beta、public beta 和 GA 各自独立关闭 `G-EXPOSURE`；“不得公开”只约束尚未批准的
具体 Platform channel，不反向否认已公开的 Runtime source/prerelease。

Platform RC 必须同时关闭所有单阶段 Gate、上述四个 aggregate Gate、`G-SUPPLY-CHAIN` 与
`G-PLATFORM-RELEASE`；phase record 本身不能替代聚合 Gate 或 RC closure。
