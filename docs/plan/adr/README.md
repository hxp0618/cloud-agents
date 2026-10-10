# ADR 索引

本目录是架构决策和约束的规范来源。ADR 保留标题、状态、决策、范围、约束、后果与 authority；其中的证据段只用于说明决策依据。ADR 不提供当前 Cloud Agents 实际状态，当前状态统一回到 [06](../cloud-agents-platform/06-status-tracker.md)。

## 决策文件

- [0005-cloud-agent-external-runtime-candidate](0005-cloud-agent-external-runtime-candidate.md)
- [0006-public-cloud-agents-platform](0006-public-cloud-agents-platform.md)
- [0007-p1-contract-data-toolchain-foundation](0007-p1-contract-data-toolchain-foundation.md)
- [0008-p1-postgres-data-kernel](0008-p1-postgres-data-kernel.md)
- [0009-p1-migration-bundle-runner](0009-p1-migration-bundle-runner.md)
- [0010-p1-postgres-projection-contract](0010-p1-postgres-projection-contract.md)
- [0011-p1-membership-rbac-contract](0011-p1-membership-rbac-contract.md)
- [0012-p1-versioned-lineage-quota-profile](0012-p1-versioned-lineage-quota-profile.md)
- [0013-p1-durable-coordination-contract](0013-p1-durable-coordination-contract.md)
- [0014-p1-lineage-quota-profile-v3](0014-p1-lineage-quota-profile-v3.md)
- [0015-p1-compatibility-recovery-contract](0015-p1-compatibility-recovery-contract.md)
- [0016-p1-compatibility-recovery-postgres-kernel](0016-p1-compatibility-recovery-postgres-kernel.md)
- [0017-p1-compatibility-recovery-v2-registry](0017-p1-compatibility-recovery-v2-registry.md)
- [0018-p1-compatibility-recovery-v2-writer-kernel](0018-p1-compatibility-recovery-v2-writer-kernel.md)
- [0019-p1-runner-ledger-preflight-contract](0019-p1-runner-ledger-preflight-contract.md)
- [0020-p1-runner-ledger-consumer-contract](0020-p1-runner-ledger-consumer-contract.md)
- [0021-p1-runner-ledger-entry-admission-contract](0021-p1-runner-ledger-entry-admission-contract.md)
- [0022-p1-runner-ledger-entry-success-writer-contract](0022-p1-runner-ledger-entry-success-writer-contract.md)
- [0023-p1-runner-ledger-recovery-writer-contract](0023-p1-runner-ledger-recovery-writer-contract.md)
- [0024-p1-software-crash-durability-acceptance](0024-p1-software-crash-durability-acceptance.md)
- [0025-p1-offline-jwt-access-token-verifier-contract](0025-p1-offline-jwt-access-token-verifier-contract.md)
- [0026-p1-json-schema-official-suite-evidence-closure](0026-p1-json-schema-official-suite-evidence-closure.md)
- [0027-p1-runtime-server-path-tenant-authority](0027-p1-runtime-server-path-tenant-authority.md)
- [0028-p1-generator-supply-profile](0028-p1-generator-supply-profile.md)
- [0029-p1-contract-closure-successor-supply-rebind](0029-p1-contract-closure-successor-supply-rebind.md)
- [0030-p1-g-contract-current-source-phase-successor](0030-p1-g-contract-current-source-phase-successor.md)
- [0031-foundation-first-cloud-workspace-platform](0031-foundation-first-cloud-workspace-platform.md)
- [0032-infrastructure-admin-delivery-and-document-routing](0032-infrastructure-admin-delivery-and-document-routing.md)
- [0033-built-in-identity-service](0033-built-in-identity-service.md) — IDENTITY-V1；P0 已批准，按 Goal 推进运行时和验收。

## 阅读边界

- P1 数据、契约、恢复、身份和供给链决策集中在 ADR-0007–ADR-0030。
- 产品边界、底座优先和文档路由见 ADR-0005、ADR-0006、ADR-0031、ADR-0032。
- 内置账号、登录、邀请与租户发现的批准范围见 ADR-0033；它不改写 ADR-0025 冻结输入。
- 运行记录、independent review 和 Gate 结果只作为审计依据，按 [evidence 索引](../cloud-agents-platform/evidence/README.md)、[P1 索引](../p1/README.md) 或 [standalone 索引](../standalone/README.md) 访问，不复制到 ADR 索引。
