# Platform P1 execution evidence

- Status：IN PROGRESS
- Fixed decisions：[`ADR-0007`](../adr/0007-p1-contract-data-toolchain-foundation.md)、
  [`ADR-0008`](../adr/0008-p1-postgres-data-kernel.md)
- Completed slice：P1-A1 Contract Kernel bootstrap (`e0562b280dbbc29604ea1faad9095103ce4548f4`)
- Current slice：P1-A2 Data Kernel decision freeze
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
