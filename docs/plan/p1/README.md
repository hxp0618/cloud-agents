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
  chain (`8c9a72b` through local `fa7f8e1`)，其中 brand-new generation 已完成 exact
  `GenerationReserved → segment-0 header → GenerationActivated` durability 与 root-wide lock release/opaque
  target+generation lock handoff，并推进到 compact evidencefs snapshot + strict non-runnable `GenerationReplayReady`
- Current slice：从 sealed non-runnable `GenerationReplayReady` 建立 same-verifier recovery binding 与 normal-run
  `EvidenceJournal`/`JournalCursor` authority；production trusted mount 与 runner/DB `Connect` 仍 fail closed
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
  `c017c9573015d7e91099d71744459f9f7478594d`，记录 brand-new receipt-bound reservation、generation
  journal/segment-0 durability、exact activation append、root-wide lock release、fault gates 与未实现边界。
- 该记录是 local implementation evidence，不是独立 reviewer 签署的 immutable Gate closure；
  `GenerationReplayReady` 没有 production consumer，也没有 normal-run journal/cursor 或 DB authority。`fa7f8e1` 当前因
  GitHub remote 500 仅存在本地，远端分支仍停在 `3b15340`。

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

在上述 projection 工作之后，admission durability 已推进到 sealed non-runnable `GenerationReplayReady`：root-wide 与
non-target locks 已释放，只保留 exact target lineage + generation lock pair，并已完成 compact snapshot/strict replay。
但这没有改变 Gate 结论：production
trusted-mount constructor、journal cursor/checkpoint、runner/DB `Connect`、successor/reopen 与真实
ext4/XFS/power-loss 证据仍然开放。
