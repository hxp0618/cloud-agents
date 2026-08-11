# Platform P1 execution evidence

- Status：IN PROGRESS
- Fixed decisions：[`ADR-0007`](../adr/0007-p1-contract-data-toolchain-foundation.md)、
  [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)、
  [`ADR-0009`](../adr/0009-p1-migration-bundle-runner.md)
- Completed slices：P1-A1 Contract Kernel bootstrap (`e0562b280dbbc29604ea1faad9095103ce4548f4`)；SubjectRef/
  HTTP idempotency authority follow-up (`eeb22f26765d99eefcbe316af3ea63991bb5950b`)；SQL/bootstrap authority
  (`4f39b14`)；tenant-scoped pgx read helper (`4af2a66`)；strict migration bundle bootstrap (`363627e`)；
  PostgreSQL tenant-helper matrix (`99a1b54`)；fail-closed migration runner core (`99106e8`)
- Current slice：P1-A2.1 Migration/Tenancy（真实 PostgreSQL authority/catalog/intermediate projector、signed trust
  provider、runner 实库矩阵、crash/recovery 与 Gate closure 仍未完成）
- Remaining P1 slice：P1-A3 SDK/Identity/Closure
- Gate closure：none

本目录保存 P1 实现过程中的 dependency review、固定输入和可重放本地证据。只有
[`cloud-agents-platform/evidence`](../cloud-agents-platform/evidence/README.md) 下由独立 reviewer 签署的 immutable
closure record 才能把 `G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` 或 `G-SECURITY-P1` 标为 `VERIFIED`。

P1 采用 local-first：开发、focused tests、format/lint/typecheck 与 evidence script 先在本机执行；接近 phase
closure 后才把固定 SHA 在选定 Linux/amd64 主机重放。此目录中的 `PASS` 不等于发布、部署、真实 Provider、
Platform RC、Beta 或 GA。

## Dependency reviews

- [`ajv-8.20.0.md`](dependency-reviews/ajv-8.20.0.md)：P1 JSON Schema 2020-12 validator direct edge；APPROVED
- [`pgx-v5.10.0.md`](dependency-reviews/pgx-v5.10.0.md)：P1 PostgreSQL driver target；**BLOCKED**（default
  `x/text v0.29.0` 存在 reachable `GO-2026-5970`，等待安全闭包独立复核）
- [`x-text-v0.39.0.md`](dependency-reviews/x-text-v0.39.0.md)：pgx remediation exact direct pin 与最终 MVS
  delta；**APPROVED**（仍需在真实 Control Plane `go.mod/go.sum` 落盘并重放后 supersede pgx BLOCKED record）

## Data kernel decisions

- [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)：global-table allowlist、bootstrap authority、tenant
  context ABI、migration lineage/checksum 与 P1-A2.1～P1-A2.4 execution slices。
- [`ADR-0009`](../adr/0009-p1-migration-bundle-runner.md)：schema/bootstrap/manifest/runner 四类 digest、deterministic
  bundle、外部 trust anchor、strict SQL/catalog contract、ambiguous commit 与 tenant helper 生命周期。

## P1-A2.1 current boundary

`services/control-plane/migrations/` 已建立 database/role bootstrap、migration ledger schema、Tenant/Organization/
Project、tenant-local revision/change fact、`FORCE RLS` 与 audited Tenant bootstrap 的 SQL authority。当前还具有
exact-byte manifest/generator、deterministic runtime/bootstrap archive、strict TS/Go classifier、dedicated pgx runner
core、transaction-local typed tenant helper，以及 PostgreSQL 15.18/16.14/17.10 上 helper 的连接复用/泄漏负向矩阵。

这些结果仍是 `UNPUBLISHED_BOOTSTRAP_MUTABLE`：catalog/authority runtime introspection 与 detached signing 为
`NOT_IMPLEMENTED`，生产 CLI 默认在读取 artifact 或连接数据库前拒绝。必须补齐真实 projectors、signed trust、
runner 的三版本实库执行/锁/timeout/ambiguous-commit 证据、migration/recovery/N-1/PITR evidence 和 immutable closure
record，才能声称 P1-A2.1 或 `G-DATA` 关闭。
