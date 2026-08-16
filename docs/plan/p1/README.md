# Platform P1 execution evidence

- Status：IN PROGRESS
- Fixed decisions：[`ADR-0007`](../adr/0007-p1-contract-data-toolchain-foundation.md)、
  [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)、
  [`ADR-0009`](../adr/0009-p1-migration-bundle-runner.md)、
  [`ADR-0010`](../adr/0010-p1-postgres-projection-contract.md)
- Completed slices：P1-A1 Contract Kernel bootstrap (`e0562b280dbbc29604ea1faad9095103ce4548f4`)；SubjectRef/
  HTTP idempotency authority follow-up (`eeb22f26765d99eefcbe316af3ea63991bb5950b`)；SQL/bootstrap authority
  (`4f39b14`)；tenant-scoped pgx read helper (`4af2a66`)；strict migration bundle bootstrap (`363627e`)；
  PostgreSQL tenant-helper matrix (`99a1b54`)；fail-closed migration runner core (`99106e8`)；pgx v5.10.0 +
  x/text v0.39.0 dependency implementation closure artifacts (`93f742f`，不关闭 `G-SUPPLY-CHAIN`)；
  P1-A2.1a-impl-1 strict projection contract/fixture (`b36f45a`)；P1-A2.1a-impl-2 PG adapters
  (`e2541c5`) 与本地 PG15/16/17 fresh A/B matrix (`a0eac37`)；admission registration/publish/bind/reserve
  chain (`8c9a72b` through `f654aae`)，其中 brand-new generation 已完成 exact
  `GenerationReserved → segment-0 header → GenerationActivated` durability 与 root-wide lock release/opaque
  target+generation lock handoff，并推进到 compact evidencefs snapshot、strict replay 与 sealed same-verifier
  `GenerationRecoveryReady`、receipt-bound retained existing/rotated-segment `EvidenceJournal` composite append/checkpoint、
  unknown reconciliation，以及 sealed current `ActiveGeneration`/`EvidenceSession`；registered ancestor reopen 与 live
  successor 已完成 irreversible full-root reacquisition、adjacent
  `GenerationSuperseded → GenerationReserved`、successor activate/handoff/replay/recovery/journal 和同一 session current swap；
  crash-reopened historical header-only successor 已在 `f654aae` 接入 production `BindSession` 的 B → C 闭图；
  `7b35a3f` 新增并由 `e230c74` 补齐 Linux/macOS SHA-256 tool portability 的 opt-in real Linux ext4/xfs
  clean-restart matrix，在 test-only authority 下验证 actual publish/sync、cross-process flock、publisher kill、
  fresh-process reopen 与 clean unmount/remount；`180929a` 将同一矩阵扩展到 actual target registration、generation
  header/index durability、root-lock handoff、retained existing/rotated-segment composite append、generation-holder kill 与
  full-byte fresh-process/remount replay；`3d21e90` 再以隔离 QEMU guest 整机 `SIGKILL`/fresh boot 覆盖 ext4/xfs
  object 与 generation durable-return 后的 virtual guest-host power cycle；`b6cfa88` 又在 fresh raw disk/new guest
  上覆盖 object publish 的 11 个 real-syscall barrier、whole-QEMU kill 与 sealed fresh-mount recovery classification；
  `daa6b9f` 再覆盖 existing-segment journal+checkpoint composite append 的 10 个 FD-bound `pwrite/fdatasync`
  barrier 与五态 prefix classification；`0e242ee` 继续覆盖 retained segment rotation 的 26 个 FD-bound
  create/write/data-sync/directory-sync barrier 与十态 fresh-mount classification；`be7cae8` 再覆盖 generation
  activation index append 的 5 个 exact-FD write/data-sync barrier 与三态 fresh-mount classification；`139d53a`
  再覆盖 target registration create 的 25 个 mkdir/lock/index barrier 与 torn-prefix recovery 的 21 个
  parent/lock/truncate/rewrite/sync barrier，且每项 fresh boot 均闭合到 exact `registered_empty`；`f650fae`
  再覆盖 generation-header create 的 21 个 directory/lock/segment barrier 与从 durable 28-byte torn segment
  开始的 23 个 recovery barrier，全部 fresh boot 均闭合到 exact 57-byte segment-0/`Revalidate`；`70269e1`
  让 migration pass-1 消费 directory/lock/segment generation-prefix facts，`7b52509` 再以 same-verifier
  historical generation、exact durable `GenerationReserved`、registered receipts 与 fresh evidencefs mutation token
  交叉绑定 one-shot recovery，闭合到 sealed `RecoveredHeaderDurablePermit`；`cdedda7` 再追加 byte-exact adjacent
  `GenerationActivated`、重跑 fresh ALL-history 并闭合到既有 `RegisteredGenerationHandoffPermit`，复用已审
  handoff/replay/session 路径
- Current slice：固定 trusted-mount provisioner、production required-syscall probe、remaining repair per-barrier virtual
  matrix、physical controller/host power-loss harness 和 runner/DB `Connect`；production constructor 继续 fail closed，
  `f650fae` 的 test-only physical evidence 与 `70269e1`/`7b52509`/`cdedda7` 的 migration authority evidence 都不构成
  positive trusted-mount cross-package integration 或 filesystem slice Done
- Remaining P1 slices：P1-A2.1b-impl-1～3、P1-A2.2～P1-A2.4、
  P1-A3 SDK/Identity/Closure
- Gate closure：none

本目录保存 P1 实现过程中的 dependency review、固定输入和可重放本地证据。只有
[`cloud-agents-platform/evidence`](../cloud-agents-platform/evidence/README.md) 下由独立 reviewer 签署的 immutable
closure record 才能把 `G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` 或 `G-SECURITY-P1` 标为 `VERIFIED`。

P1 采用 local-first：开发、focused tests、format/lint/typecheck 与 evidence script 先在本机执行；接近 phase
closure 后才把固定 SHA 在选定 Linux/amd64 主机重放。此目录中的 `PASS` 不等于发布、部署、真实 Provider、
Platform RC、Beta 或 GA。

## Dependency reviews

- [`ajv-8.20.0.md`](dependency-reviews/ajv-8.20.0.md)：P1 JSON Schema 2020-12 validator direct edge；APPROVED
- [`pgx-v5.10.0.md`](dependency-reviews/pgx-v5.10.0.md)：**HISTORICAL BLOCKED**；保留 default
  `x/text v0.29.0` 命中 reachable `GO-2026-5970` 的拒绝依据，实施状态已被下述 closure supersede
- [`x-text-v0.39.0.md`](dependency-reviews/x-text-v0.39.0.md)：pgx remediation exact MVS floor；**APPROVED**，
  已在真实 `go.mod/go.sum` 落盘
- [`pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md`](dependency-reviews/pgx-v5.10.0-x-text-v0.39.0-implemented-closure.md)：
  **APPROVED — dependency implementation closure only**；artifacts 已由 `93f742f` 集成，但
  `G-SUPPLY-CHAIN` 仍为 `IN PROGRESS`

## Data kernel decisions

- [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)：global-table allowlist、bootstrap authority、tenant
  context ABI、migration lineage/checksum 与 P1-A2.1～P1-A2.4 execution slices。
- [`ADR-0009`](../adr/0009-p1-migration-bundle-runner.md)：schema/bootstrap/manifest/runner 四类 digest、deterministic
  bundle、外部 trust anchor、strict SQL/catalog contract、ambiguous commit 与 tenant helper 生命周期。
- [`ADR-0010`](../adr/0010-p1-postgres-projection-contract.md)：冻结 signed expected authority/catalog contract、
  version-neutral typed projection、snapshot/transaction 边界及 P1-A2.1a/P1-A2.1b 实施顺序；细化 verified
  authority binding，但不改变 schema ledger 或 Gate 语义。

## Current admission durability evidence

- [`admission-generation-durability-20260813.md`](admission-generation-durability-20260813.md) 固定 source
  `5e0065afededa163a186d4ee706bfb2cc437f63f`，记录 brand-new receipt-bound reservation、generation
  journal/segment-0 durability、exact activation append、root-wide lock release、retained normal-run journal、segment
  rotation、unknown reconciliation、current active/session sealing、fault gates 与未实现边界。
- [`successor-generation-session-20260814.md`](successor-generation-session-20260814.md) 固定 source `cebacea`，记录
  registered ancestor session、generation-lease→full-root reacquire、successor content/receipt/index/header/activation、
  retained replay/recovery/journal 和 same-session current swap。
- [`historical-successor-process-restart-20260816.md`](historical-successor-process-restart-20260816.md) 固定 source
  `f654aae`，记录 strict historical B replay 后的 one-way full-root reacquire、B → C materialization 与 current session wiring。
- [`evidencefs-linux-filesystem-matrix-20260816.md`](evidencefs-linux-filesystem-matrix-20260816.md) 固定 source
  `e230c74`，记录 OrbStack Linux/arm64 上真实 ext4/xfs loop mount、actual publish/sync、cross-process flock、
  killed publisher 后 fresh process reopen 与 clean unmount/remount；production `Open` 同场继续 fail closed。
- [`evidencefs-linux-generation-matrix-20260816.md`](evidencefs-linux-generation-matrix-20260816.md) 固定 source
  `180929a`，在相同真实 ext4/xfs harness 上增加 target/generation/index/segment transition、handoff 后分层锁验证、
  generation-holder `SIGKILL`、fresh-process exact-byte replay 与 clean remount replay。
- [`evidencefs-qemu-powerloss-matrix-20260816.md`](evidencefs-qemu-powerloss-matrix-20260816.md) 固定 source
  `3d21e90`，记录隔离 QEMU guest 的 ext4/xfs object/generation durable-ready→whole-guest `SIGKILL`→fresh-guest
  exact-byte recovery；不外推到 physical controller 或 per-barrier power loss。
- [`evidencefs-qemu-object-publish-barrier-matrix-20260816.md`](evidencefs-qemu-object-publish-barrier-matrix-20260816.md)
  固定 source `b6cfa88`，记录 ext4/xfs 上 object publish 11 个 syscall barrier 的 fresh-disk、whole-QEMU kill 与
  sealed fresh-mount state classification；不外推到 generation barriers 或 physical controller。
- [`evidencefs-qemu-generation-append-barrier-matrix-20260816.md`](evidencefs-qemu-generation-append-barrier-matrix-20260816.md)
  固定 source `daa6b9f`，记录 ext4/xfs 上 existing-segment composite append 10 个 FD-bound write/sync barrier 的
  whole-QEMU kill 与 sealed fresh-mount five-state classification；不外推到 generation create/rotation/repair barriers。
- [`evidencefs-qemu-generation-rotation-barrier-matrix-20260816.md`](evidencefs-qemu-generation-rotation-barrier-matrix-20260816.md)
  固定 source `0e242ee`，记录 ext4/xfs 上 retained segment rotation 26 个 FD-bound create/write/data-sync/
  directory-sync barrier 的 whole-QEMU kill 与 sealed fresh-mount ten-state classification；不外推到 generation
  registration/header/activation/repair barriers。
- [`evidencefs-qemu-generation-activation-barrier-matrix-20260816.md`](evidencefs-qemu-generation-activation-barrier-matrix-20260816.md)
  固定 source `be7cae8`，记录 ext4/xfs 上 generation activation index append 5 个 exact-FD write/data-sync
  barrier 的 whole-QEMU kill 与 sealed fresh-mount three-state classification；不外推到 header/repair
  barriers。
- [`evidencefs-qemu-target-registration-barrier-matrix-20260816.md`](evidencefs-qemu-target-registration-barrier-matrix-20260816.md)
  固定 source `139d53a`，记录 ext4/xfs 上 target registration create 25 个与 torn-prefix recovery 21 个
  exact-syscall barrier 的 whole-QEMU kill、sealed fresh-mount classification、fresh mutation token recovery 与最终
  `registered_empty`/`Revalidate`；不外推到 generation header/repair barriers。
- [`evidencefs-qemu-generation-header-barrier-matrix-20260816.md`](evidencefs-qemu-generation-header-barrier-matrix-20260816.md)
  固定 source `f650fae`，记录 ext4/xfs 上 generation-header create 21 个与 torn-prefix recovery 23 个
  exact-syscall barrier 的 whole-QEMU kill、sealed prefix/journal classification、fresh token completion 与最终 exact
  57-byte segment-0/`Revalidate`；不外推到 production verified reopen binder、remaining repair 或 physical controller。
- [`generation-prefix-reopen-binder-20260816.md`](generation-prefix-reopen-binder-20260816.md) 固定 source
  `7b52509`，记录 migration-owned pass-1 generation-prefix transcript 与 same-verifier one-shot recovery binder，闭合到
  sealed `RecoveredHeaderDurablePermit`；不外推到 adjacent activation、handoff、trusted-mount integration 或 Gate closure。
- [`generation-prefix-activation-handoff-20260816.md`](generation-prefix-activation-handoff-20260816.md) 固定 source
  `cdedda7`，记录 recovered-header exact adjacent activation、fresh ALL-history rebind 与既有 retained-lock handoff bridge；
  不外推到 positive trusted-mount integration、runner/DB 或 Gate closure。
- 十四份记录都是 local implementation evidence，不是独立 reviewer 签署的 immutable Gate closure；concrete session
  仍没有 public `EvidenceSink` constructor、trusted mount、production process-restart/power-loss 或 DB authority。

## Projection runner boundary (still open)

`services/control-plane/migrations/` 已建立 database/role bootstrap、migration ledger schema、Tenant/Organization/
Project、tenant-local revision/change fact、`FORCE RLS` 与 audited Tenant bootstrap 的 SQL authority；runner 已具有
exact-byte bundle、strict classifier、transaction-local tenant helper 和 PostgreSQL 15/16/17 helper matrix。
`93f742f` 又集成了 pgx/x-text dependency lock、SBOM、notice 与本地时点扫描，但该记录只关闭依赖实施切片。

`b36f45a` 已完成 strict typed projection contracts、Go/TS fail-closed validators、共享 golden/fault fixtures、
generation-lock/source provenance，以及 scoped predecessor ABI。`e2541c5` 完成 verified wrapper、sealed snapshot、
PG15/16/17 capability/authority/namespace/default-ACL projector；`a0eac37` 在本地 `linux/arm64` 用固定 PG15.18、
16.14、17.10 镜像完成 fresh A/B、normal/race 两轮矩阵。该证据不外推到 x86_64、云环境或 Gate closure。

Projection runner 仍只进入 statement 前/后与 pre-ledger `ControlPlaneStates`/intermediate chain。
Signed expected subject 的 production verifier、deployment trust-root wiring、relation/expression projector、
crash/recovery、N-1/PITR 和 immutable Gate closure 均未实现。现有 catalog 继续保持
`UNPUBLISHED_BOOTSTRAP_MUTABLE` / `NOT_IMPLEMENTED`，生产 CLI 继续在读取数据库前拒绝；因此
`G-DATA`、`G-AUTHORITY-P1`、`G-SECURITY-P1` 与 `G-SUPPLY-CHAIN` 均不得标为 `VERIFIED`。

在上述 projection 工作之后，admission durability 已从 sealed non-runnable `GenerationRecoveryReady` 继续推进到
receipt-bound concrete journal 与 current `ActiveGeneration`/`EvidenceSession`：root-wide 与 non-target locks 已释放，只保留
exact target lineage + generation lock pair，并已完成 compact snapshot/strict replay、same-verifier facts、typed publication
receipt 自有化，以及当前 cursor/recovery snapshot 的 session accessor。
但这没有改变 Gate 结论：production trusted-mount constructor、public sink、runner/DB `Connect`、positive trusted-mount
cross-package activation/handoff integration、remaining repair per-barrier matrix，以及真实 physical controller power-loss
证据仍然开放。
`b6cfa88`、`daa6b9f`、`0e242ee`、`be7cae8`、`139d53a`、`f650fae` 只在 package-private test authority 下完成
isolated QEMU guest 的 object publish、existing-segment append、retained rotation、activation、target registration/recovery
与 generation header create/recovery barrier kill/recovery；
`70269e1`/`7b52509`/`cdedda7` 则只完成 migration ordinary transcript、composite recovery authority 与 existing-handoff
bridge；production constructor 仍 pre-mutation reject，这些记录都不是 positive trusted-mount cross-package restart
证据，也不允许越过 filesystem slice 进入 A2.1b。
