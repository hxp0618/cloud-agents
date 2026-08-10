# Platform P1 execution evidence

- Status：IN PROGRESS
- Fixed decision：[`ADR-0007`](../adr/0007-p1-contract-data-toolchain-foundation.md)
- Current slice：P1-A Contract Kernel
- Gate closure：none

本目录保存 P1 实现过程中的 dependency review、固定输入和可重放本地证据。只有
[`cloud-agents-platform/evidence`](../cloud-agents-platform/evidence/README.md) 下由独立 reviewer 签署的 immutable
closure record 才能把 `G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` 或 `G-SECURITY-P1` 标为 `VERIFIED`。

P1 采用 local-first：开发、focused tests、format/lint/typecheck 与 evidence script 先在本机执行；接近 phase
closure 后才把固定 SHA 在选定 Linux/amd64 主机重放。此目录中的 `PASS` 不等于发布、部署、真实 Provider、
Platform RC、Beta 或 GA。

## Dependency reviews

- [`ajv-8.20.0.md`](dependency-reviews/ajv-8.20.0.md)：P1 JSON Schema 2020-12 validator direct edge；APPROVED
