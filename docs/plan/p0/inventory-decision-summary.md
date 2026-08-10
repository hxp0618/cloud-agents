# P0 Synara inventory 最终裁决摘要

## 固定输入

- Source：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- Source tree：`ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- Inventory：`docs/plan/p0/synara-file-inventory.tsv`
- Inventory SHA-256：`bee237da890f4f3d62fd524fd11142a6b6c883e82790e5d455c415461ae7b4e5`
- Inventory rows：8625
- Manual-review input rows：355
- Decision rows：8625
- Decision TSV SHA-256：`24a7f918636b7f0baafaa6c99ff1c04c9b8ad10163cbb568fd311718bb9342ee`
- Unresolved/duplicate/missing decisions：**0**
- Source license provenance：`MIT@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0:LICENSE#blob=960499447d8ea8f6ce86017893f132f0c3885fef;sha256=305724dd050ca7ded99c662de813d755bc4ec3887c4543a37159c6662ca36d1b`
- Public-candidate secret provenance：**AUDITED exact-finding triage**；静态测试私钥来源文件为 **REWRITE REQUIRED**

## 最终分类

`manualReview=true` 的 355 条均由 executor explicit semantic rule 决策；该字段不代表人工 owner 已签署 Gate：

| Classification              | Count |
| --------------------------- | ----: |
| `adapter`                   |    19 |
| `deferred-public-extension` |     6 |
| `move`                      |     2 |
| `rewrite-public`            |   127 |
| `synara-only`               |   201 |

完整 8,625 条 final manifest（含 legacy root `COPY . .` 的完整 tracked build context；seed 仅作输入提示）：

| Classification              | Count |
| --------------------------- | ----: |
| `adapter`                   |    96 |
| `deferred-public-extension` |    67 |
| `move`                      |    26 |
| `retire`                    |     5 |
| `rewrite-public`            |   940 |
| `synara-only`               |  7491 |

## Owner

| Owner                     | Count |
| ------------------------- | ----: |
| `deferred`                |    67 |
| `public-core`             |   876 |
| `public-platform-adapter` |    92 |
| `runtime-release`         |    90 |
| `synara-host`             |  7500 |

## 输入 scope

| Scope                  | Count |
| ---------------------- | ----: |
| `ci`                   |    10 |
| `contract-reference`   |    62 |
| `control-plane`        |  1174 |
| `deploy`               |   117 |
| `legacy-build-context` |  7002 |
| `provider-host-compat` |     4 |
| `root-supply-chain`    |    21 |
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
| `legacy-build-context`                  |  7002 |
| `management-api`                        |   109 |
| `metadata-migration`                    |     4 |
| `module-supply-chain`                   |    16 |
| `observability`                         |    17 |
| `platform-contract`                     |     2 |
| `postgres-schema`                       |   301 |
| `product-composition`                   |    22 |
| `provider-catalog-generation`           |     5 |
| `release-supply-chain`                  |    28 |
| `repository-governance`                 |    10 |
| `routing-ingress`                       |    12 |
| `synara-build-release`                  |     1 |
| `synara-product-operations`             |    47 |
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
3. Provider catalog 的旧 `catalog.go`/测试只是 Control Plane internal 的低依赖 move 候选，不进入公开 Go SDK ABI；旧 `catalog_gen.go` 是 orphaned output，旧 generator 指向缺失的 Synara JSON，公共实现必须从独立定义的新 catalog contract 重新生成。
4. Synara desktop/mac、Polaris SDK、Stage 6 产品治理和私有运维脚本留在 Synara。公共 Worker 生命周期、隔离、manifest/registry/supply-chain/security conformance 有独立公共目标，Vault oracle 延后到外部 adapter extension。
5. Synara 根 `package.json`/`bun.lock` 只作为旧镜像 provenance 留在 Synara；根 Dockerfile 不能复制，必须重写为最小、digest-pinned 的 `deploy/images/worker/Dockerfile`。
6. 每条 provenance 同时固定 `source ref:path`、Git blob OID 和内容 SHA-256；生成器会对 source HEAD/tree/dirty、inventory SHA/行数、blob、重复、缺项和空/unknown 决策 fail closed；任何旧 Control Plane internal helper 指向 `sdk/go/*`、任何 `docs/contracts/*` 指向正式 `contracts/*` 也会直接失败。
7. Adapter 不得拥有 migration、durable model、routing/KMS lifecycle、HTTP authority 或 receipt truth；混合文件先标 `rewrite-public` 并要求 core/port 拆分，直接 Docker/filesystem/webhook/live-cluster effects 单独标 adapter。
8. 旧根 Dockerfile 使用 `COPY . .`，所以 inventory 冻结全部 8,625 个 tracked blob；不在批准 extraction surface 的 7,002 项统一 default-deny 留在 Synara，不能进入新的 public image context。

## 全量覆盖

- Deploy：117 条全部进入 final manifest；Synara Admin/Developer Docs/Stage 6 组合留在宿主，公共 CP/Worker Helm 重写，gVisor/Cocoon/Kubernetes actuator 独立 adapter，personal/remote -> public Compose rewrite，Vault production policy -> deferred extension。
- Scripts：235 条全部进入 final manifest；公共 conformance/release 工具与 Synara desktop/Stage 6/Polaris/product release 工具分开 owner/target。
- Contracts：62 条按 Runtime/Worker/Control Plane legacy oracle、Platform Adapter、Synara product 或 deferred enterprise extension 分域；旧 prose/schema 不进入正式 `contracts/*`，新 wire authority 独立定义。
- Go/SQL：Control Plane/Worker/adapter/Synara/deferred/retire 逐文件写入 target；167 个旧 migration 只映射到新 lineage 的 semantic target 或保留面，不继承编号/table identity。
- Root supply chain：21 条显式 root/workspace inputs 与全部 tracked build context 均进入 final manifest；candidate lock/schema 映射公共 release contract/tooling，Dockerfile 重写，Synara root manifests/patches/config 留在宿主。
- CI：10 条全部标记 Synara-owned；公共仓 workflow 必须在 P1 重新设计，不能复制 Synara 权限和发布语义。
- Cross-package edge：固定验证 `cmd/api/main.go` 对 `agentd.RunGitAskPassHelperFromEnvironment`、`LocalSupervisor` 的直接依赖；迁移时必须用 command composition/SDK seam 消除此 internal import。

## 重放

```bash
node docs/plan/p0/scripts/finalize-synara-inventory.mjs
shasum -a 256 docs/plan/p0/synara-inventory-decisions.tsv
```
