# 06. 状态与决策追踪

- 最后更新：2026-08-11
- Plan status：APPROVED
- Implementation status：P0 VERIFIED；P1 IN PROGRESS（P1-A1 bootstrap complete / P1-A2 decision freeze）；M1/P2–P6 PAUSED

## 1. 决策表

| ID    | 决策                                                                                             | 状态     | 依据/待确认                                  |
| ----- | ------------------------------------------------------------------------------------------------ | -------- | -------------------------------------------- |
| D-001 | Cloud Agents 是 Runtime + 完整公共 Go Control Plane 平台                                         | APPROVED | 用户于 2026-08-10 批准 ADR-0006              |
| D-002 | Public CP 同时提供 managed-agent 与 managed-host 两平面                                          | APPROVED | 解决 Synara/T3 不同 authority                |
| D-003 | T3 embedded 不强依赖 Go CP                                                                       | APPROVED | 保留轻量本地路径                             |
| D-004 | Public CP 必须无 Synara 私有依赖直接 Compose/Helm 部署                                           | APPROVED | 用户要求直接部署                             |
| D-005 | Synara/T3 通过公共 API/SDK 接入，不编译私有 CP fork                                              | APPROVED | 单一公共 source/bits                         |
| D-006 | 旧 Go CP 按 move/rewrite/adapter/synara-only/retire 分类                                         | APPROVED | 禁止 994-file 机械复制                       |
| D-007 | Public CP owns production Postgres/outbox/reconciler                                             | APPROVED | 独立部署与原子 authority                     |
| D-008 | 新 T3 `ManagedConnectionTarget`，direct/relay proof-bound                                        | APPROVED | 当前 direct 仍是 Bearer                      |
| D-009 | Runtime 与 Platform 同仓但独立 module/release train                                              | APPROVED | contract/conformance 原子，release 解耦      |
| D-010 | Public tenancy 固定 Tenant → Organization → Project                                              | APPROVED | 中立隔离根；Synara 一对一映射                |
| D-011 | Go SDK/CP/Worker 三个 module；go.work 仅开发                                                     | APPROVED | 标准子模块 tag 与无 workspace 依赖           |
| D-012 | P3 用 reference host；P6 消费 T3 signed workload descriptor                                      | APPROVED | 避免 P3/P6 Gate 循环                         |
| D-013 | pairing token/link/session 由 lease 内 T3 auth 写入                                              | APPROVED | CP 只写 lease admission 与 opaque ref        |
| D-014 | Platform RC 必须 API+CLI；公共管理 Web UI deferred                                               | APPROVED | 直接部署不依赖 Synara/T3 UI                  |
| D-015 | Contracts、TS SDK、Go SDK 各自使用 immutable release train                                       | APPROVED | consumer exact pin；发布 channel 独立批准    |
| D-016 | Public CP 是 management PEP；T3 auth 是 lease data PEP                                           | APPROVED | membership/generation/scope 为上游约束       |
| D-017 | Pairing secret response 与 durable receipt/outbox 完全分离                                       | APPROVED | 丢失后 revoke + remint，禁止 secret replay   |
| D-018 | Host descriptor/artifact/provenance 使用固定签名 trust domain                                    | APPROVED | descriptor 不替代 image/bundle 验签          |
| D-019 | 供应链实施 CVE/VEX/waiver/base-image revocation policy                                           | APPROVED | Platform RC 不继承历史 RC 的安全结论         |
| D-020 | 跨阶段 Gate 使用 phase record，最终再关闭 aggregate Gate                                         | APPROVED | 消除 P1 提前证明 P2–P6 的循环                |
| D-021 | 跨 PEP 使用短 TTL signed auth snapshot + revocation epoch/fence                                  | APPROVED | 分区时最多 60 秒后 fail closed               |
| D-022 | Baseline 使用 P0/M1 phase records，最终再关闭 aggregate Gate                                     | APPROVED | 应用 D-020，避免暂停中的 M1 反向阻塞 P0      |
| D-023 | Go 提取只创建新公共历史，禁止 graft Synara Git history                                           | APPROVED | 历史日志隔离；静态测试私钥必须先重写         |
| D-024 | 开发与 focused Gate 本地优先；固定 SHA 接近收口后再做云端终验                                    | APPROVED | 避免开发循环反复占用云主机并混淆证据层级     |
| D-025 | Management/Agent/Host 用 OpenAPI HTTP/JSON；Worker/Adapter 用 Proto + ConnectRPC/mTLS            | APPROVED | ADR-0007；每个平面只有一个 wire authority    |
| D-026 | JSON Schema、OpenAPI、Proto 分别拥有 model、route、worker/adapter service authority              | APPROVED | ADR-0007；legacy contract 仅作 oracle        |
| D-027 | CP/Worker import SDK；SDK 不 import service；Go 1.26.5；发布禁 replace                           | APPROVED | ADR-0007；go.work 仅本地开发                 |
| D-028 | P1 支持 PostgreSQL 15–17；pgx/v5 + 手写 SQL；禁 GORM/AutoMigrate                                 | APPROVED | ADR-0007；新 migration lineage               |
| D-029 | tenant 表使用 composite FK + FORCE RLS；runtime/migration role 分离                              | APPROVED | ADR-0007；RLS 是 defense in depth            |
| D-030 | NamespaceRef 结构化并以 RFC 8785 canonical JSON + SHA-256 标识                                   | APPROVED | ADR-0007；拒绝调用方自报 canonical string    |
| D-031 | 中立 basic RBAC 固定 platform root + 三层 tenancy scope、role/permission/default deny            | APPROVED | ADR-0007；workload/service 不继承 admin      |
| D-032 | contract migration 前必须 live-instance/N-1/PITR preflight，未知实例 fail closed                 | APPROVED | ADR-0007；普通 force 不得绕过                |
| D-033 | contract/SDK 生成器版本、binary digest、输入/输出 digest 固定 generation lock                    | APPROVED | ADR-0007；生成物可重放                       |
| D-034 | P1 DRI=hxp0618、executor=Codex；依赖由独立 Codex supply-chain reviewer 复核                      | APPROVED | ADR-0007；疑难 license 请求 owner/legal 决定 |
| D-035 | P1 固定 global-table allowlist、三数据库角色、transaction-local tenant GUC 与 migration manifest | APPROVED | ADR-0008；首条公共 migration 前冻结          |

## 2. 阶段追踪

| Stage             | Status      | DRI                         | Entry                                 | Exit Gate                                                                                       | Evidence                                                                                                                                              |
| ----------------- | ----------- | --------------------------- | ------------------------------------- | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1 Runtime        | PAUSED      | TBD                         | 当前 rc.1/fresh branches              | 原 M1 gates                                                                                     | rc.1 + host refs；真实 Provider open                                                                                                                  |
| P0 Inventory      | VERIFIED    | hxp0618 / Codex P0 executor | ADR accepted 2026-08-10               | G-INVENTORY/G-BASELINE-P0                                                                       | [G-INVENTORY R3](evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R3.md) / [G-BASELINE-P0 R3](evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R3.md)   |
| P1 Foundation     | IN PROGRESS | hxp0618 / Codex executor    | P0 verified                           | G-CONTRACT/G-DATA/G-AUTHORITY-P1/G-SECURITY-P1                                                  | [ADR-0007](../adr/0007-p1-contract-data-toolchain-foundation.md)；[ADR-0008](../adr/0008-p1-postgres-data-kernel.md)；[P1 execution](../p1/README.md) |
| P2 Managed Agent  | NOT STARTED | TBD                         | P1 verified                           | G-MANAGED-AGENT/G-WORKER-FENCING-P2/G-AUTHORITY-P2/G-ADAPTER-P2/G-SECURITY-P2                   | none                                                                                                                                                  |
| P3 Managed Host   | NOT STARTED | TBD                         | P1 + Runtime digest                   | G-MANAGED-HOST/G-WORKER-FENCING-P3/G-AUTHORITY-P3/G-ADAPTER-P3/G-SECURITY-P3                    | none                                                                                                                                                  |
| P4 Standalone     | NOT STARTED | TBD                         | P2/P3 verified                        | G-STANDALONE/G-OPS/G-AUTHORITY-P4/G-ADAPTER-P4/G-SECURITY-P4                                    | none                                                                                                                                                  |
| P5 Synara Cutover | NOT STARTED | TBD                         | P2/P4 candidate                       | G-SYNARA-CUTOVER/G-AUTHORITY-P5/G-SECURITY-P5                                                   | none                                                                                                                                                  |
| P6 T3 Managed     | NOT STARTED | TBD                         | P3/P4 candidate                       | G-T3-INTEGRATION/G-AUTHORITY-P6/G-SECURITY-P6                                                   | none                                                                                                                                                  |
| Platform RC       | BLOCKED     | TBD                         | all phase records + engineering gates | aggregate G-AUTHORITY/G-WORKER-FENCING/G-ADAPTER/G-SECURITY + G-SUPPLY-CHAIN/G-PLATFORM-RELEASE | none                                                                                                                                                  |

## 3. Progressive Gate record registry

| Gate / phase        | Current record ID              | Status      | Fixed input digest                                                    | Evidence                                                                                         | Last reviewed |
| ------------------- | ------------------------------ | ----------- | --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | ------------- |
| G-INVENTORY         | CAG-G-INVENTORY-P0-20260810-R3 | VERIFIED    | `5209ea0 / 2c50b1e / bee237d / 24a7f91`                               | [R3](evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R3.md)                                     | 2026-08-10    |
| G-BASELINE-P0       | CAG-G-BASELINE-P0-20260810-R3  | VERIFIED    | `5209ea0 / 66e2f12 / 2c50b1e / 8101cd0 / 9584a26 / c2c0358 / 0a984d0` | [R3](evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R3.md)                                       | 2026-08-10    |
| G-BASELINE-M1       | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-BASELINE          | none                           | IN PROGRESS | `CAG-G-BASELINE-P0-20260810-R3`                                       | P0 phase verified；M1 phase not started                                                          | 2026-08-10    |
| G-CONTRACT          | none                           | IN PROGRESS | `ADR-0007 / ADR-0008 / G-INVENTORY R3 / G-BASELINE-P0 R3`             | P1-A1 bootstrap + P1-A2 Role/idempotency mapping freeze；official generation/closure record open | 2026-08-11    |
| G-DATA              | none                           | IN PROGRESS | `ADR-0007 / ADR-0008 / G-INVENTORY R3 / G-BASELINE-P0 R3`             | P1-A2 Data Kernel decision freeze；implementation and closure record open                        | 2026-08-11    |
| G-AUTHORITY-P1      | none                           | IN PROGRESS | `ADR-0007 / ADR-0008 / G-INVENTORY R3 / G-BASELINE-P0 R3`             | Public wire/module authority bootstrap + P1-A2 writer/data authority freeze；closure record open | 2026-08-11    |
| G-AUTHORITY-P2      | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-AUTHORITY-P3      | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-AUTHORITY-P4      | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-AUTHORITY-P5      | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-AUTHORITY-P6      | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-SECURITY-P1       | none                           | IN PROGRESS | `ADR-0007 / ADR-0008 / G-INVENTORY R3 / G-BASELINE-P0 R3`             | Fail-closed contract fixtures + P1-A2 RLS/role/tenant-context freeze；closure record open        | 2026-08-11    |
| G-SECURITY-P2       | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-SECURITY-P3       | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-SECURITY-P4       | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-SECURITY-P5       | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-SECURITY-P6       | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-ADAPTER-P2        | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-ADAPTER-P3        | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-ADAPTER-P4        | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-WORKER-FENCING-P2 | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-WORKER-FENCING-P3 | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-AUTHORITY         | none                           | IN PROGRESS | `G-AUTHORITY-P1`                                                      | P1 phase underway；aggregate closure not claimed                                                 | 2026-08-10    |
| G-SECURITY          | none                           | IN PROGRESS | `G-SECURITY-P1`                                                       | P1 phase underway；aggregate closure not claimed                                                 | 2026-08-10    |
| G-ADAPTER           | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-WORKER-FENCING    | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-SUPPLY-CHAIN      | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |
| G-PLATFORM-RELEASE  | none                           | NOT STARTED | none                                                                  | none                                                                                             | none          |

### 3.1 Immutable record history

| Record ID                      | Gate / phase  | Status      | Fixed input digest                                                    | Evidence                                                         | Supersedes                     | Last reviewed |
| ------------------------------ | ------------- | ----------- | --------------------------------------------------------------------- | ---------------------------------------------------------------- | ------------------------------ | ------------- |
| CAG-G-INVENTORY-P0-20260810-R1 | G-INVENTORY   | INVALIDATED | `2c50b1e / f385885`                                                   | [record](evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R1.md) | none                           | 2026-08-10    |
| CAG-G-INVENTORY-P0-20260810-R2 | G-INVENTORY   | INVALIDATED | `2b2c5ed / 2c50b1e / bee237d / 4e8e92c`                               | [record](evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R2.md) | CAG-G-INVENTORY-P0-20260810-R1 | 2026-08-10    |
| CAG-G-INVENTORY-P0-20260810-R3 | G-INVENTORY   | VERIFIED    | `5209ea0 / 2c50b1e / bee237d / 24a7f91`                               | [record](evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R3.md) | CAG-G-INVENTORY-P0-20260810-R2 | 2026-08-10    |
| CAG-G-BASELINE-P0-20260810-R1  | G-BASELINE    | INVALIDATED | `2c50b1e / 9584a26 / 49e8cdc`                                         | [record](evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R1.md)   | none                           | 2026-08-10    |
| CAG-G-BASELINE-P0-20260810-R2  | G-BASELINE-P0 | INVALIDATED | `66e2f12 / 2c50b1e / 8101cd0 / 9584a26 / c2c0358 / 0a984d0`           | [record](evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R2.md)   | CAG-G-BASELINE-P0-20260810-R1  | 2026-08-10    |
| CAG-G-BASELINE-P0-20260810-R3  | G-BASELINE-P0 | VERIFIED    | `5209ea0 / 66e2f12 / 2c50b1e / 8101cd0 / 9584a26 / c2c0358 / 0a984d0` | [record](evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R3.md)   | CAG-G-BASELINE-P0-20260810-R2  | 2026-08-10    |

首次执行即向 history 追加 immutable record，并把上表 current pointer 指向它。record 失效时保留原 history 行并
标 `INVALIDATED`；新 revision 使用新 Record ID 追加一行，在 `Supersedes` 建链。不得覆盖或删除历史 evidence。

R1 失效原因：`G-INVENTORY` R1 只覆盖 1,607 个候选路径，遗漏旧根 Docker `COPY . .` 可见的完整 tracked
build context，且 classification 存在 durable authority / environment effect 混分；`G-BASELINE` R1 早于
D-022 的 P0/M1 phase-record 语义，也没有固定环境的实际 test-run evidence。两份 R1 只保留为历史输入，不能
作为当前 Gate closure。

`G-INVENTORY` R2 失效原因：P1 contract review 发现 24 个 legacy server helper 被错误映射到公开 `sdk/go/*`，
42 个 Synara legacy contract/oracle 被错误映射到正式 `contracts/*` authority。R3 不改变 frozen source、8,625-row
inventory、classification、owner、capability、license 或 secret triage；它只纠正 66 个 target，并新增
helper/legacy-contract/duplicate-target 三类 fail-closed invariant。任何固定旧 decision digest `4e8e92c...` 的
下游证据不得继承。`G-BASELINE-P0` R2 因固定前置 R2 同步失效；Baseline R3 不重跑或提升行为证据，只把
完全相同的 characterization 固定输入重绑定到 Inventory R3。

## 4. 当前 open questions

| ID    | 问题                                        | 推荐默认                                                                        | 必须在何时关闭     |
| ----- | ------------------------------------------- | ------------------------------------------------------------------------------- | ------------------ |
| Q-004 | Local credential store 与 production broker | local encrypted store + Vault/KMS protocol                                      | P2 security design |
| Q-005 | Synara 现有活跃 Session 如何 drain          | 新 Session 分 cohort；旧 writer drain，不 live migrate                          | P5 cutover         |
| Q-006 | Managed Agent checkpoint primitive          | public Worker 写物理 snapshot；public CP 写 metadata/ref；managed-host 由 T3 写 | P2 contract        |
| Q-007 | Public Artifact store baseline              | filesystem + S3-compatible                                                      | P2 adapter freeze  |

## 5. 暂停现场问题

| ID    | 观察                                                                      | 状态                             | 恢复后动作                                            |
| ----- | ------------------------------------------------------------------------- | -------------------------------- | ----------------------------------------------------- |
| R-001 | Codex 0.145 null `exclude` 被 rc.1 attestor 误拒                          | OPEN / uncommitted fix preserved | 独立 rc.2 修复窗口                                    |
| R-002 | Claude SDK-managed SendTurn unsuccessful without details                  | OPEN / diagnosis interrupted     | 独立诊断，不与 CP 混合                                |
| R-003 | Full Worker image Alpine package lock drift                               | OPEN                             | 宿主供应链 refresh Gate                               |
| R-004 | app.asar/完整 cross-host/soak                                             | OPEN                             | M1 E2E closure                                        |
| R-005 | pgx v5.10.0 default closure 的 x/text v0.29.0 命中 reachable GO-2026-5970 | OPEN / dependency BLOCKED        | 审查 x/text >= v0.39.0 与最终 MVS 闭包后再进入 go.mod |

## 6. 恢复实施 checklist

- [x] 用户于 2026-08-10 批准 ADR-0006 与 D-001～D-021；
- [x] P0 DRI 暂定为 hxp0618（owner），Codex 为 evidence executor；P1 前重新确认长期 DRI；
- [x] 目标公共 repo 固定为 `hxp0618/cloud-agents`；CODEOWNERS/security advisory 在 P0 inventory 登记；
- [x] 先执行 P0 inventory，不直接搬代码；
- [x] M1 rc.2 与 Platform P0 保持两个独立执行窗口；本次不恢复 M1；
- [x] P0 允许只读外部 ref/metadata 查询与计划分支 push；不授权发布、部署或数据库写入；
- [x] 用户明确解除 P0 的 `PAUSED`；P0 closure 后 P1 Entry satisfied，M1/P2–P6 保持暂停。
- [x] 执行环境采用 local-first：本地完成实现、focused tests 与静态 Gate；云服务器只对接近收口的固定 SHA 做一次 Linux/amd64 终验。
- [x] `G-INVENTORY` R3 与 `G-BASELINE-P0` R3 均 supersede 各自 R2 并经独立复核为 `VERIFIED`；P0 Exit 已满足。
- [x] ADR-0007 冻结 P1 transport/contract/data/toolchain/security foundation；P1 状态进入 `IN PROGRESS`。
- [x] 首个新增第三方 dependency `ajv@8.20.0` / `ajv-formats@3.0.1` 已由未参与实现的 Codex
      supply-chain reviewer 完成[独立审查](../p1/dependency-reviews/ajv-8.20.0.md)；无疑难 license 豁免，后续新增
      dependency 仍须逐项重复该流程。
