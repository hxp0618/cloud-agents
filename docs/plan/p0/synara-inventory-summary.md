# Synara Control Plane P0 inventory summary

- Status：IN PROGRESS
- Frozen source：`hxp0618/synara@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- Root tree：`ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- Control Plane tree：`4d32d26bf32b8ebd62188961d0dffff92da4cf0c`
- Deploy tree：`1d0369f6029f85bf0d3d14ddc81c235205169b45`
- Scripts tree：`999769dde39fced4a1711fd2e42fcc5ca2b00029`
- Source worktree：clean before and after audit
- File inventory：[`synara-file-inventory.tsv`](synara-file-inventory.tsv)
- Dependency graph：[`synara-inventory-graph.json`](synara-inventory-graph.json)

## Inventory coverage

| Scope                    | Tracked inputs |
| ------------------------ | -------------: |
| `services/control-plane` |          1,174 |
| `deploy`                 |            117 |
| `scripts`                |            235 |
| `.github`                |             10 |
| `docs/contracts`         |             62 |
| `apps/provider-host`     |              4 |
| root supply-chain inputs |              5 |
| **Total**                |      **1,607** |

Control Plane 的 1,174 个文件包括 517 个非测试 Go、477 个 `_test.go`、167 个连续编号 SQL migration
（`000001`–`000167`，无 gap）和 13 个 Dockerfile/module/fixture 等输入。全部 migration 由
`migrations/embed.go` 嵌入。

逐文件 TSV 固定 Git blob OID、SHA-256、bytes、scope、capability、phase/relation、seed classification、
generated state、risk 与 manual-review 标志。它是 P0 初始分类，不是最终 authority decision。

## Go command closure

离线 `GOWORK=off GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly CGO_ENABLED=0 GOOS=linux GOARCH=amd64`
审计得到：8 个生产 command，114 个 module package；生产闭包覆盖 104 个本地 package、497 个编译源与
167 个嵌入 SQL。未进入生产 command 的 10 个 package 是 catalog generator 和 9 个
`internal/testsupport/*`。

| Command                     | All packages | Standard | External | Local | Local build/embed files |
| --------------------------- | -----------: | -------: | -------: | ----: | ----------------------: |
| `api`                       |        1,072 |      225 |      756 |    91 |                     644 |
| `metadata`                  |        1,037 |      225 |      755 |    57 |                     476 |
| `agentd`                    |          975 |      225 |      699 |    51 |                     302 |
| `kms-worker`                |          285 |      207 |       76 |     2 |                       7 |
| `routing-authority-sign`    |          190 |      163 |       18 |     9 |                      83 |
| `gvisor-node-attestor`      |          195 |      192 |        1 |     2 |                       2 |
| `cocoon-supervisor`         |          130 |      126 |        1 |     3 |                       8 |
| `cocoon-provider-transport` |           82 |       80 |        0 |     2 |                       3 |

Linux amd64/arm64 的本地源数量一致；Linux/Darwin/Windows 四组矩阵并集为 671 个编译/embed 输入，658
个四平台共有，13 个是 OS build-tag 变体。

## Capability map

| Capability                                | Primary source                                                       |
| ----------------------------------------- | -------------------------------------------------------------------- |
| API/health/router                         | `cmd/api`、`internal/httpapi`                                        |
| Session/Execution/Target/Worker authority | `internal/sessions`、`executions`、`executiontargets`、`persistence` |
| Worker/workspace/credential/Provider Host | `cmd/agentd`、`internal/agentd`                                      |
| Cocoon guest/host/supervisor              | `cocoontransport`、`cocoonsupervisor`                                |
| KMS and runtime secret rotation           | `kmsworker`、`kmsrotation`、`runtimesecretrotation`                  |
| Metadata migration                        | `cmd/metadata`、`metadatamigration`                                  |
| gVisor attestation                        | `cmd/gvisor-node-attestor`、`gvisorattestor`                         |
| Routing publication/signing               | `cmd/routing-authority-sign`、`internal/routing`                     |
| PostgreSQL schema                         | `migrations/*`、`internal/database`                                  |
| Provider capability projection            | `internal/providercatalog`                                           |
| Deployment                                | `deploy/personal`、`saas`、`kubernetes`、`worker`                    |
| External Runtime candidate                | candidate lock/schema、provider-host、root Dockerfile                |

## Seed classification

| Classification seed       | Files |
| ------------------------- | ----: |
| rewrite-public            | 1,017 |
| adapter                   |   287 |
| move                      |    22 |
| synara-only               |    43 |
| deferred-public-extension |     8 |
| retire                    |     1 |
| unclassified              |   229 |

339 个文件必须人工复核，包括 229 个未分类输入、`internal/agentd` mixed package，以及缺失生成源的
Provider catalog chain。只有人工复核完成、逐文件 owner/target/provenance 冻结后，seed 才能提升为最终
classification。

## P0 blockers and P1 risks

1. `internal/providercatalog/generate.go` 仍引用已不存在的
   `packages/contracts/src/providerCapabilityCatalog.json`；checked-in `catalog_gen.go` 掩盖了不可重生状态。
2. 旧 catalog source path 仍被 generator、worker manifest、acceptance、测试和设计文档引用；必须标
   `orphaned-source`/`missing-source-reference`。
3. Go inventory 不能独立代表 Runtime：`agentd → worker image → provider-host bundle → candidate lock → 8 external
artifacts` 是跨语言、跨镜像边。
4. `internal/agentd` 同时包含 CP client、workspace/git、credential、containment、gVisor、Cocoon、Provider
   Host v2 与 local supervisor，不能整包 move。
5. `cmd/api` 直接调用 `agentd.RunGitAskPassHelperFromEnvironment` 和 `agentd.LocalSupervisor`，API/agentd
   当前并非独立 capability island。
6. Control Plane 与 Worker Docker stage 使用宽 `COPY`，把测试和 migration 带入 cache/build context；必须
   区分 semantic compile/embed 与 build-context/copy relation。
7. migration 与 worker image cache 耦合，不能用“文件进入镜像层”推断 Runtime authority。

因此 `G-INVENTORY` 仍为 `IN PROGRESS`，P1 不得开始。

## Reproduce

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/generate-synara-file-inventory.mjs
```

生成器在 HEAD 不等于 frozen SHA 或 source worktree 非 clean 时 fail closed。运行前后均不修改 Synara source。
