# 05. Gate 与验收

## 1. Gate 总表

| Gate                 | 阻塞                | 退出证据                                                                                                                             |
| -------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `G-INVENTORY`        | P0                  | frozen ref 的全量 code/SQL/schema/build/deploy/generated manifest、分类、source/tree hash、authority、license/secret provenance 完整 |
| `G-BASELINE`         | P0/M1               | legacy Synara managed-agent、T3 embedded、可复用机制 characterization；Managed Host 用 spec/negative/reference-host fixtures         |
| `G-CONTRACT`         | P1                  | OpenAPI/JSON Schema、TS/Go SDK、server validator、API reliability、golden/negative fixture 同源                                      |
| `G-DATA`             | P1                  | Postgres expand/contract、idempotency、outbox、leader、backup/restore、N/N-1                                                         |
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
| `G-AUTHORITY`      | `G-AUTHORITY-P1` … `G-AUTHORITY-P6`            |
| `G-SECURITY`       | `G-SECURITY-P1` … `G-SECURITY-P6`              |
| `G-ADAPTER`        | `G-ADAPTER-P2`、`G-ADAPTER-P3`、`G-ADAPTER-P4` |
| `G-WORKER-FENCING` | `G-WORKER-FENCING-P2`、`G-WORKER-FENCING-P3`   |

phase record 状态只允许 `NOT STARTED`、`IN PROGRESS`、`VERIFIED`、`INVALIDATED`。每个 record 固定前置
record、authority scope、source/dirty/toolchain、contract/module/SDK/image/migration/manifest digest、原样命令、
逐项结果、DRI/独立 reviewer 与 downstream invalidation rule。阶段 Exit 只依赖该阶段 record，不声称未来阶段
已验证；Platform RC 才验证上述 aggregate Gate。

失效规则至少为：contract/core/store 改动使 P1–P6 record 失效；Worker/adapter 改动使相关 P2–P6 record
失效；Standalone/security/ops 改动使 P4–P6 record 失效；Synara adapter/cutover 改动使 P5–P6 record
失效；T3 descriptor/proof/connection 改动使 P6 record 失效；任何重打包或 digest 变化使 same-bits record、
`G-SUPPLY-CHAIN` 与 `G-PLATFORM-RELEASE` 失效。旧 record 保留，不覆盖历史。

安全状态变化即使 bits 不变也会失效：新 applicable KEV/reachable Critical/High、waiver 到期、scanner DB
超过 24 小时、signer/trust root/release identity 撤销、base image EOL/revoke，都会把
`G-SUPPLY-CHAIN` 与引用它的 `G-PLATFORM-RELEASE` 标为 `INVALIDATED`，暂停未批准 exposure，并要求按新
数据库/信任根重扫、重新签署与重跑受影响 same-bits Gate。不得因 artifact digest 未变沿用旧安全结论。

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
