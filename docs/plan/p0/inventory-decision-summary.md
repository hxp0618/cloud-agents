# P0 Synara inventory 最终裁决摘要

## 固定输入

- Source：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- Source tree：`ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- Inventory：`docs/plan/p0/synara-file-inventory.tsv`
- Inventory SHA-256：`f3858852538ef67ec6879a6db101246f7b3bf65ba6301f9e5e9274200d716aa1`
- Inventory rows：1607
- Manual-review input rows：339
- Decision rows：1607
- Decision TSV SHA-256：`a259ddf7f01e72e0bf6577d2ed754a8ec2837c17b78d28c8baa5b78f78cd3a08`
- Unresolved/duplicate/missing decisions：**0**

## 最终分类

人工复核的 339 条：

| Classification              | Count |
| --------------------------- | ----: |
| `adapter`                   |    19 |
| `deferred-public-extension` |     6 |
| `move`                      |     2 |
| `rewrite-public`            |   127 |
| `synara-only`               |   185 |

完整 1,607 条 final manifest（seed 仅作输入提示，已逐行写入 final 字段）：

| Classification              | Count |
| --------------------------- | ----: |
| `adapter`                   |   230 |
| `deferred-public-extension` |    67 |
| `move`                      |    26 |
| `retire`                    |     5 |
| `rewrite-public`            |   820 |
| `synara-only`               |   459 |

## Owner

| Owner                     | Count |
| ------------------------- | ----: |
| `deferred`                |    67 |
| `public-core`             |   809 |
| `public-platform-adapter` |   216 |
| `runtime-release`         |    47 |
| `synara-host`             |   468 |

## 输入 scope

| Scope                  | Count |
| ---------------------- | ----: |
| `ci`                   |    10 |
| `contract-reference`   |    62 |
| `control-plane`        |  1174 |
| `deploy`               |   117 |
| `provider-host-compat` |     4 |
| `root-supply-chain`    |     5 |
| `scripts`              |   235 |

## Final capability

| Capability                              | Count |
| --------------------------------------- | ----: |
| `audit`                                 |     4 |
| `billing-commercial`                    |    46 |
| `cocoon-isolation`                      |    14 |
| `control-plane-bootstrap`               |     2 |
| `control-plane-config`                  |     2 |
| `control-plane-eventstream`             |     2 |
| `control-plane-fairqueue`               |     2 |
| `control-plane-foundation`              |     4 |
| `control-plane-gitpolicy`               |     2 |
| `control-plane-lifecyclepolicy`         |     3 |
| `control-plane-memories`                |     2 |
| `control-plane-platform`                |     2 |
| `control-plane-podlifecycle`            |     2 |
| `control-plane-problem`                 |     1 |
| `control-plane-providercapabilities`    |     2 |
| `control-plane-providerproxy`           |     2 |
| `control-plane-quotas`                  |     2 |
| `control-plane-runtimekeys`             |     1 |
| `control-plane-schedulingdecision`      |     3 |
| `control-plane-schedulingpolicy`        |     2 |
| `control-plane-tenancy`                 |     6 |
| `control-plane-testsupport`             |     2 |
| `control-plane-usage`                   |     4 |
| `control-plane-validation`              |     3 |
| `credential-broker`                     |    44 |
| `deployment-adapter`                    |     6 |
| `desktop-integration`                   |    10 |
| `disaster-recovery`                     |     3 |
| `durability`                            |    18 |
| `execution-authority`                   |   284 |
| `external-runtime-candidate`            |    17 |
| `governance-retention`                  |    17 |
| `gvisor-attestation`                    |     3 |
| `identity-tenancy`                      |    51 |
| `kms-secret-rotation`                   |    15 |
| `kubernetes-deployment`                 |    74 |
| `management-api`                        |   109 |
| `metadata-migration`                    |     4 |
| `module-supply-chain`                   |     5 |
| `observability`                         |    17 |
| `platform-contract`                     |     2 |
| `postgres-schema`                       |   301 |
| `product-composition`                   |    22 |
| `provider-catalog-generation`           |     5 |
| `release-supply-chain`                  |    28 |
| `repository-governance`                 |    10 |
| `routing-ingress`                       |    12 |
| `synara-build-release`                  |     1 |
| `synara-product-operations`             |    42 |
| `synara-release-governance`             |   155 |
| `worker-agentd-workspace-provider-host` |   108 |
| `worker-capacity`                       |    13 |
| `worker-conformance`                    |    44 |
| `worker-image-supply-chain`             |    21 |
| `worker-lifecycle`                      |    22 |
| `workspace-artifact`                    |    29 |

## 关键裁决

1. `internal/agentd` 没有整包搬迁：Worker authority、workspace、checkpoint、credential、Provider Host、process/containment 分入 `services/worker/internal/*`；Cocoon、gVisor、Kubernetes、SSH 分入内置 adapter。
2. `cmd/api -> agentd` 的现有耦合不作为公共边界继承；agentd client/daemon 必须改写为只依赖 generated Worker SDK/wire，Control Plane 和 Worker 不互相 import `internal`。
3. Provider catalog 三文件全部标记 `rewrite-public`：旧 `catalog_gen.go` 是 orphaned output，旧 generator 指向缺失的 Synara JSON；公共实现以 `contracts/managed-agent/v1alpha1/provider-capability-catalog.json` 为 source-of-truth 后重新生成。
4. Synara desktop/mac、Polaris SDK、Stage 6 产品治理和私有运维脚本留在 Synara。公共 Worker 生命周期、隔离、manifest/registry/supply-chain/security conformance 有独立公共目标，Vault oracle 延后到外部 adapter extension。
5. Synara 根 `package.json`/`bun.lock` 只作为旧镜像 provenance 留在 Synara；根 Dockerfile 不能复制，必须重写为最小、digest-pinned 的 `deploy/images/worker/Dockerfile`。
6. 每条 provenance 同时固定 `source ref:path`、Git blob OID 和内容 SHA-256；生成器会对 source HEAD/tree/dirty、inventory SHA/行数、blob、重复、缺项和空/unknown 决策 fail closed。

## 全量覆盖

- Deploy：117 条全部进入 final manifest；Kubernetes -> Helm/adapter、personal/remote -> public Compose rewrite、Worker -> independent image train、SaaS/billing -> Synara、Vault production policy -> deferred extension。
- Scripts：235 条全部进入 final manifest；公共 conformance/release 工具与 Synara desktop/Stage 6/Polaris/product release 工具分开 owner/target。
- Contracts：62 条按 Runtime/Worker/Managed Agent/Platform Adapter、Synara product 或 deferred enterprise extension 分域，不把旧 prose 当新 wire authority。
- Go/SQL：Control Plane/Worker/adapter/Synara/deferred/retire 逐文件写入 target；167 个旧 migration 只映射到新 lineage 的 semantic target 或保留面，不继承编号/table identity。
- Root supply chain：5 条全部进入 final manifest；candidate lock/schema 映射公共 release contract/tooling，Dockerfile 重写，Synara root manifests 留在宿主。
- CI：10 条全部标记 Synara-owned；公共仓 workflow 必须在 P1 重新设计，不能复制 Synara 权限和发布语义。
- Cross-package edge：固定验证 `cmd/api/main.go` 对 `agentd.RunGitAskPassHelperFromEnvironment`、`LocalSupervisor` 的直接依赖；迁移时必须用 command composition/SDK seam 消除此 internal import。

## 重放

```bash
node docs/plan/p0/scripts/finalize-synara-inventory.mjs
shasum -a 256 docs/plan/p0/synara-inventory-decisions.tsv
```
