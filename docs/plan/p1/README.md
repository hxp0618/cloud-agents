# Platform P1 execution evidence

- Status：IN PROGRESS
- Fixed decisions：[`ADR-0007`](../adr/0007-p1-contract-data-toolchain-foundation.md)、
  [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)
- Completed slices：P1-A1 Contract Kernel bootstrap (`e0562b280dbbc29604ea1faad9095103ce4548f4`)；SubjectRef/
  HTTP idempotency authority follow-up (`eeb22f26765d99eefcbe316af3ea63991bb5950b`)
- Current slice：P1-A2.1 Migration/Tenancy（SQL/bootstrap authority 已实现并本地验证；strict manifest/runner、
  pgx tenant transaction helper 和 PostgreSQL 15–17 matrix 仍未完成）
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

## P1-A2.1 current boundary

`services/control-plane/migrations/` 已建立 database/role bootstrap、migration ledger schema、Tenant/Organization/
Project、tenant-local revision/change fact、`FORCE RLS` 与 audited Tenant bootstrap 的 SQL authority。该切片只是
P1-A2.1 的 SQL 输入；在 exact-byte manifest、dedicated pgx runner、ledger crash/replay、transaction-local tenant
helper、pool leakage negative 和 PostgreSQL 15.18/16.14/17.10 matrix 完成前，不得声称 P1-A2.1 或
`G-DATA` 关闭。
