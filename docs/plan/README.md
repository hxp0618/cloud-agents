# Cloud Agents 计划与证据总入口

- Canonical root：`hxp0618/cloud-agents/docs/plan`
- Plan status：原计划与 ADR-0031 产品边界/优先级已批准；新技术拆解为实施方案，不宣称逐项已验收
- 当前主线：底座 BASE-M0～M5 → BASE-READY → 用户 CloudAgents APP-M1；Admin Web 随底座配套
- 当前决策与阶段状态：只在 [`06-status-tracker.md`](cloud-agents-platform/06-status-tracker.md) 维护；旧 P0～P6/Runtime/Gate 历史不代表新底座已实现
- Approved by user：2026-08-10
- Migration source：`hxp0618/synara@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- Source plan commit：`4433ebfcff882458822e90d9d79edb076c7ccc91`
- Migration manifest：[`migration-manifest.json`](migration-manifest.json)

## Source of truth

本目录是 Cloud Agents Runtime、公共 Go Control Plane、Worker/Supervisor、Synara/T3 consumer 集成计划与
Gate evidence 的唯一计划根。后续不再以 Synara 私有仓中的计划副本推进公共平台状态。

本节是解释顺序的唯一维护入口；其他文档引用本节，不另行复制 ADR 编号范围或优先级。

1. 与当前事项相关、已批准且未被取代的 ADR；从
   [`06-status-tracker.md`](cloud-agents-platform/06-status-tracker.md) 的决策记录定位并核对原始批准依据。
   后续决策只在明确取代的范围内覆盖前项，原有 ordered slices、local-only、non-Gate 等限制仍按各 ADR 执行；
2. [`cloud-agents-platform/01`–`07`](cloud-agents-platform/README.md) 中已批准的适用内容；
3. [`Synara × T3 总架构`](synara-t3-cloud-agent-integration-architecture.md)；
4. `legacy/` 历史计划；
5. `references/` 冻结参考合同；
6. 代码现状。

以上是规范与授权的解释顺序，不是实现事实的证据排序。判断“目前实现了什么”须检查当前源码与固定版本实测；目标文档不能证明功能存在，旧缺失记录不能否定后续已实现的代码。

草案不会因被索引、被其他文档引用或已有实现就变成已批准；代码与历史输入也不能反向授予实施权限。
本页及子计划的状态、执行边界摘要须与 `06-status-tracker.md` 和对应批准记录核对，不覆盖后续明确授权。
遇到暂停或未授权记录时，先核对当前任务、已有会话授权和后续批准记录；同一动作与范围已经明确获准且
仍有效时，不重复请求确认。范围、目标环境改变或存在单独审批检查点时，仍须满足相应批准要求；
不能把继续开发的授权外推为生产数据库写入、部署发布、删除脏 worktree 或 Gate closure 的授权。

## 当前产品与实施入口

[ADR-0031 / D-054](adr/0031-foundation-first-cloud-workspace-platform.md) 记录用户已明确的产品边界：
长期云工作区、通用 Sandbox、客户节点接入优先；Admin Web 配套；用户 CloudAgents 后续消费底座。
[04](cloud-agents-platform/04-extraction-and-migration.md) 维护唯一实施顺序，
[05](cloud-agents-platform/05-gates-and-acceptance.md) 定义无 Agent 的 BASE-READY 验收。
现有 Runtime、Managed Agent/Host API 和活动 Lease 保留兼容，不因文档调整改变数据或运行状态。

本次授权包括当前仓文档整理、独立 worktree 的文档提交与合并回当前分支；不含代码实现、跨仓改动、
生产数据、部署或发布。后续明确实现任务仍按上节识别同范围授权，不重复确认已经确定的产品边界。

## 当前计划

| 文档                                                                                                     | 作用                                             |
| -------------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| [`cloud-agents-platform/README.md`](cloud-agents-platform/README.md)                                     | 公共平台固定追踪入口                             |
| [`01-product-scope-and-authority.md`](cloud-agents-platform/01-product-scope-and-authority.md)           | 产品范围与单一 authority                         |
| [`02-target-architecture.md`](cloud-agents-platform/02-target-architecture.md)                           | Control Plane/Worker/Runtime 目标架构            |
| [`03-public-repository-and-release.md`](cloud-agents-platform/03-public-repository-and-release.md)       | 公共仓、module、制品与 release train             |
| [`04-extraction-and-migration.md`](cloud-agents-platform/04-extraction-and-migration.md)                 | BASE 实施顺序、既有 API/数据迁移与后续 cutover                  |
| [`05-gates-and-acceptance.md`](cloud-agents-platform/05-gates-and-acceptance.md)                         | Gate、same-bits、安全与验收                      |
| [`06-status-tracker.md`](cloud-agents-platform/06-status-tracker.md)                                     | 决策、阶段、record 与暂停现场                    |
| [`07-admin-web-requirements-and-design.md`](cloud-agents-platform/07-admin-web-requirements-and-design.md) | 底座配套 Admin Web、安全交互与旧 UI 兼容 |
| [`ADR-0031`](adr/0031-foundation-first-cloud-workspace-platform.md) | 底座先行决策及明确取代范围 |
| [`p0/README.md`](p0/README.md)                                                                           | P0 freeze、inventory、baseline 与 provenance     |
| [`p1/README.md`](p1/README.md)                                                                           | P1 foundation 与 dependency review 证据          |
| [`synara-t3-cloud-agent-integration-architecture.md`](synara-t3-cloud-agent-integration-architecture.md) | Runtime + Platform + 双宿主总设计                |
| [`ADR-0005`](adr/0005-cloud-agent-external-runtime-candidate.md)                                         | immutable external Runtime candidate 历史决定    |
| [`ADR-0006`](adr/0006-public-cloud-agents-platform.md)                                                   | 完整公共 Go Control Plane 平台决定               |
| [`ADR-0007`](adr/0007-p1-contract-data-toolchain-foundation.md)                                          | P1 contract/data/toolchain foundation 决定       |
| [`ADR-0008`](adr/0008-p1-postgres-data-kernel.md)                                                        | P1 PostgreSQL data kernel 决定                   |
| [`ADR-0009`](adr/0009-p1-migration-bundle-runner.md)                                                     | P1 migration bundle/runner/trust 决定            |
| [`ADR-0010`](adr/0010-p1-postgres-projection-contract.md)                                                | P1 PostgreSQL authority/catalog projection 决定  |
| [`ADR-0011`](adr/0011-p1-membership-rbac-contract.md)                                                    | P1 Membership/RBAC authority 决定                |
| [`ADR-0012`](adr/0012-p1-versioned-lineage-quota-profile.md)                                             | P1 versioned lineage/quota profile 决定          |
| [`ADR-0013`](adr/0013-p1-durable-coordination-contract.md)                                               | P1 durable coordination registry/state 决定      |
| [`ADR-0014`](adr/0014-p1-lineage-quota-profile-v3.md)                                                    | P1 lineage/quota profile v3 决定                 |
| [`ADR-0015`](adr/0015-p1-compatibility-recovery-contract.md)                                             | P1 compatibility/recovery contract 决定          |
| [`ADR-0016`](adr/0016-p1-compatibility-recovery-postgres-kernel.md)                                      | P1 compatibility/recovery PostgreSQL 决定        |
| [`ADR-0017`](adr/0017-p1-compatibility-recovery-v2-registry.md)                                          | P1 compatibility/recovery v2 registry 决定       |
| [`ADR-0018`](adr/0018-p1-compatibility-recovery-v2-writer-kernel.md)                                     | P1 compatibility/recovery v2 writer 决定         |
| [`ADR-0019`](adr/0019-p1-runner-ledger-preflight-contract.md)                                            | P1 runner ledger preflight 决定                  |
| [`ADR-0020`](adr/0020-p1-runner-ledger-consumer-contract.md)                                             | P1 runner ledger read-only consumer 决定         |
| [`ADR-0021`](adr/0021-p1-runner-ledger-entry-admission-contract.md)                                      | P1 runner ledger close-only entry admission 决定 |
| [`ADR-0022`](adr/0022-p1-runner-ledger-entry-success-writer-contract.md)                                 | P1 runner entry execution/success-writer 决定    |
| [`ADR-0023`](adr/0023-p1-runner-ledger-recovery-writer-contract.md)                                      | P1 runner recovery ordered writer 决定           |
| [`ADR-0024`](adr/0024-p1-software-crash-durability-acceptance.md)                                        | P1 software-crash durability acceptance 边界     |
| [`ADR-0025`](adr/0025-p1-offline-jwt-access-token-verifier-contract.md)                                  | P1 offline JWT access-token verifier 边界        |
| [`ADR-0026`](adr/0026-p1-json-schema-official-suite-evidence-closure.md)                                 | P1 JSON Schema official-suite evidence closure   |
| [`ADR-0028`](adr/0028-p1-generator-supply-profile.md)                                                    | P1 bounded generator-supply profile 边界         |
| [`ADR-0029`](adr/0029-p1-contract-closure-successor-supply-rebind.md)                                    | P1 closure/supply successor DAG 边界             |

## 历史与参考

- `legacy/` 保存迁移前的方向评估、快速供给、SDK、Go CP 和 sandbox 计划；它们是历史输入，不覆盖当前
  authority/Gate。
- `references/contracts/` 保存总架构直接依赖的 Synara Provider Host、Runtime Event 与 Worker protocol
  参考版本；公共 contract 完成后由生成/验证的公共 schema 取代。
- 迁移时未复制 Stage 4–8 报告、私有环境配置或其他 Synara 产品文档；历史计划中的此类引用固定到来源
  commit 的 GitHub URL。

## 既有 P0/P1 批准记录与明确授权边界

以下是 2026-08 各固定 slice 的历史批准与限制，不是当前功能清单或当前工作排序。
其批准不会被本次文档重排扩大；其旧“未实现/暂停”摘要也不能否认后续源码与明确同范围授权。
当前产品顺序及新底座未完成项以 D-054 和 06 的当前区块为准；原始验收文件保持不变。

P0 已由当前 `G-INVENTORY` R3 与 `G-BASELINE-P0` R4 两个 independently reviewed closure record 完成；
P1-A2.1b-impl-3 已由 `401206a` 完成；A2.2-impl-1 catalog contract 与 impl-2 data/read evaluator 已分别由
`f988e45`、`e36e1cf` 完成；A2.2-impl-3 mutation/service/matrix 已由 `de36ca3` 固定，`350b53c` 关闭
failed/unknown commit 的非零结果缺口，`afe6cb2 → 1ff7713 → 2dc443d` 再关闭 Membership/RoleBinding
admission authority、迁移闭包与当前 source-bound supply metadata。Independent implementation review 随后发现
Go 与 direct PostgreSQL 的 subject issuer language 不一致；append-only `000006` remediation candidate 已通过本地
PG15/16/17 matrix，但按冻结 ADR-0010 v1 whole-bundle 公式会使 lineage index 超出 16 MiB。用户已批准
[`ADR-0012`](adr/0012-p1-versioned-lineage-quota-profile.md) 的显式 v2 profile 方向；v1 历史兼容、v2
生成/绑定已由 `cd64dee` 提交并推送。首次独立复核发现 v1 显式空 profile 的降级边界，`f731c6b`
已 fail closed 修复并由 `610b1ab` 刷新 source-bound metadata；第二轮 `gpt-5.6-sol` 独立复核返回
`APPROVE, P0=0/P1=0/P2=0`，只关闭 A2.2-impl-3 remediation 的固定源码 implementation/review 层，详见
[`independent review`](p1/versioned-lineage-quota-profile-independent-review-20260818.md)。用户已于 2026-08-19
批准 A2.3 的 generated contract registry、closed state machines 与三切片顺序；`ff9ea33`/`a9826e4` 已固定前两
切片，`59ec260` 已固定 generated Go profile、append-only `000008` service/claim 与 PG15/16/17 normal/race/fault
matrix。用户又明确批准 ADR-0014 的 generated-manifest v3 profile 方向。独立审查对当时快照返回
`NOT APPROVE, P0=0/P1=1/P2=0`：quota/profile 与 append-only kernel 分项通过，但 generated Unicode
`organizationRef` 与 ASCII-only authorization `ScopeRef` 不一致。该 finding 已在后续候选中按 generated
operation-specific identity profile 收窄为 ASCII、最多 128 bytes、`exact_string_no_rewrite`，公共 Unicode
organization reference contract 保持不变；append-only `000009` 同时保留冻结 historical registry/profile pair，并以
versioned v2 service entry 使用 current pair。精确 remediation candidate 随后独立复核返回
`APPROVE, P0=0/P1=0/P2=0`，只关闭 A2.3 generated registry/profile、append-only kernel 和
service/claim/matrix 的 implementation/review slice，详见
[`A2.3 remediation independent review`](p1/durable-coordination-v3-remediation-independent-review-20260820.md)。
原 [`A2.3 v3 independent review`](p1/durable-coordination-v3-independent-review-20260819.md) 保留历史 verdict。随后
current-bundle local full `internal/migration` closure 已在 `67b8acb` 以 `-timeout=30m` 通过（`1012.165s`；见
[`closure record`](p1/durable-coordination-full-migration-closure-20260820.md)），仅为本地 full-suite evidence，HTTP/P2
side effect 不开放，且不关闭任何 Gate。原 entry audit 见
[`A2.3 blocker`](p1/durable-coordination-entry-blocker-20260818.md)。A2.4 的 versioned registry repair、append-only
writer kernel 与 typed service/claim/matrix 已由 `b639b07` 的 independent review 批准；A3 generated identity、JSON
SDK/server seam 与 Proto SDK/fresh consumers 已由 `c5d8cbf` 固定并完成 bounded independent review。随后 runner
ledger/catalog preflight 按 [`ADR-0019`](adr/0019-p1-runner-ledger-preflight-contract.md) 完成 generated profile、locked
read-only kernel 与 same-verifier one-shot claim/no-op dispatch；Slice C fixed `e64e0a2` 的 independent review
`9ed71b8` 返回 `APPROVE, P0=0/P1=0/P2=0`。该路径仍未进入 `Runner.Run` 或 writer。先前当前源码的五分钟
bounded run 正确记录为 **NOT PASS**；其后同一 control-plane subtree 已在 `b57acf2` 用 Go 1.26.6 完成 uncached
full normal `internal/migration` suite（`1108.208s`），见
[`current-source closure`](p1/runner-ledger-current-source-full-migration-closure-20260821.md)。该结果不继承
`67b8acb` 的旧源码结论，也不构成 full race、live PostgreSQL 或 Gate closure。
同一 700 项历史 run 的 reusable shard runner 随后在
[`8552c0c` closure repair](p1/migration-shard-runner-closure-repair-20260822.md) 中闭合严格 JSON run-set、
signal/异常退出 group cleanup、launch/retirement 窗口、stale PGID retirement 与 live-leader-bound residue
rejection；只复用固定历史 artifact 并运行短 fake fixture，未重跑 full migration。固定候选 `8e49501` 已由
[`1a98f72` independent review](p1/migration-shard-runner-closure-repair-independent-review-20260822.md) 返回
`APPROVE, P0=0/P1=0/P2=0`；该 verdict 只关闭 reusable-runner implementation/review slice，不构成 Gate closure。
当前 [`P1 aggregate Gate gap audit`](p1/p1-aggregate-gate-gap-audit-20260822.md) 把四个 P1 Exit Gate 的现有
evidence 与当时缺口分开：D-047/ADR-0023 仍须 owner 显式决定，physical controller/host power-loss 仍受 dedicated
DUT/storage/out-of-band controller 阻塞，最终 current-source immutable phase records 尚未形成。固定候选
`6274ad0` 的 [`d03d62b` independent review](p1/p1-aggregate-gate-gap-audit-independent-review-20260822.md)
返回 `APPROVE, P0=0/P1=0/P2=0`；审计与复核均未改变 Gate。后续 D-048/ADR-0024 只把物理硬断电从当前
P1/RC 必需证据改为可选 hardening；2026-08-24 owner 又明确普通 `poweroff`/`reboot` 可计为项目“掉电恢复”，
但必须记录其 exact mechanism 为 clean shutdown/restart，不能声称 abrupt crash、BMC hard-off、物理拔电或
SSD/controller cache-loss。closure 仍须结合既有 ext4/XFS/QEMU matrices、重启后精确核验、current-source
record 与独立 Gate review；原
[`independent review`](p1/software-crash-durability-acceptance-independent-review-20260823.md) 返回
`APPROVE, P0=0/P1=0/P2=0`，旧 gap audit 仍是固定 source 的准确历史记录。
其后 runner ledger consumer 已在 `dcb4b3a` 固定 complete-ledger `return_success` read-only no-op，并由
`4209e12` 独立复核批准；ADR-0021 的 generated five-pair entry-admission profile、same-verifier fresh-session
read-only revalidation 与 registry-backed `close_without_mutation` permit 又在修复 generation-lock 漂移后的
`88a5392` 固定，由 `dd5ea657` 独立复核返回 `APPROVE, P0=0/P1=0/P2=0`。该切片只在成功 cleanup 后返回稳定
`MIGRATION_PROJECTION_NOT_IMPLEMENTED`，不实现 entry/recovery writer、`BeginMigration`、SQL、ledger/evidence
append、生产数据库写入、HTTP/P2/provider 副作用或 Gate closure。
其后的只读 [`entry writer contract audit`](p1/runner-ledger-entry-writer-contract-audit-20260822.md) 已证明当前
signed bundle 的单 entry statement 数最高为 `161`，现有 brand-new 单语句 writer 与 ADR-0021 close-only permit
均不得扩权复用；因此 ADR-0022 将 fresh execution admission 与 one-entry multi-statement success writer
拆成两个新 generated profile，并将 retry/abort/reconcile/failure 保持为后续独立合同。D-046 已按 standing
automatic-execution approval 接受，只授权 ordered local implementation/review，不构成 production 实施 authority。
generated-contract Slice A 的
[`implementation record`](p1/runner-ledger-entry-writer-profile-implementation-20260822.md) 已生成两份 versioned
registry 与 ordinary Go profile；固定候选 `1f1b0c5` 已由独立 review commit `7615fe5` 返回
`APPROVE, P0=0/P1=0/P2=0`。随后 Slice B 仅把四个 generated first-attempt pair 接入 fresh locked
execution-admission revalidation 与 registry-backed one-shot permit；其临时生产 transition 仍只有
`close_without_mutation`，[本地实现/矩阵](p1/runner-ledger-entry-execution-admission-service-matrix-20260822.md)
的固定候选 `c375fac` 已由 `d49f89c` 独立复核批准。Slice C 的
[disconnected one-entry known-success kernel](p1/runner-ledger-entry-success-kernel-service-matrix-20260822.md)
已在 `9db5891` 固定，并由
[`818c4d5` independent review](p1/runner-ledger-entry-success-kernel-service-independent-review-20260822.md)
返回 `APPROVE, P0=0/P1=0/P2=0`。Slice D 的
[typed caller/first-attempt entry loop](p1/runner-ledger-entry-loop-service-matrix-20260822.md) 已在固定候选
`9fcdb73` 完成，并由
[`351e5ea` independent review](p1/runner-ledger-entry-loop-service-independent-review-20260822.md)
返回 `APPROVE, P0=0/P1=0/P2=0`。在该 ADR-0022 边界下，四个 generated first-attempt pair 之外的
retry/abort/reconcile/failure writer 当时仍为 `NOT_IMPLEMENTED`。后续
[`contract-only audit`](p1/runner-ledger-recovery-contract-audit-20260822.md) 已把 1 个 excluded retry pair 与
11 个 recovery/reconcile/failure pair 分型，并形成当时尚未批准的
[`ADR-0023 proposal`](adr/0023-p1-runner-ledger-recovery-writer-contract.md)；superseding candidate `deb3dc6` 的
[`6d4da5b` independent review](p1/runner-ledger-recovery-contract-audit-independent-review-r2-20260822.md) 返回
`APPROVE, P0=0/P1=0/P2=0`。该 verdict 仅批准 audit/proposal 的准确性；owner 随后在
[`D-047 decision record`](p1/runner-ledger-recovery-contract-decision-20260822.md) 接受 ADR-0023 的 Decision、Closed
pair mapping 与 ordered Slices A-G。Slice A 的
[`generated registry/profile implementation`](p1/runner-ledger-recovery-profile-implementation-20260822.md) 已在独立
工作树完成 8 个 versioned registry、ordinary Go profile 与 generation-lock closure；superseding candidate
`67210b7` 已由 independent review `88f1ecc` 返回 `APPROVE, P0=0/P1=0/P2=0`。Slice B 的
[`read-only recovery admission`](p1/runner-ledger-recovery-admission-service-matrix-20260823.md) 已在 code commit
`b7a9962` 完成 fixed implementation：exact 12 pairs 仅进入同 verifier full replay、fresh locked reread 与六类
action-specific `close_without_mutation` permit；fixed candidate `23c3083` 已由
[`4808d20` independent review](p1/runner-ledger-recovery-admission-service-independent-review-20260823.md)
返回 `APPROVE, P0=0/P1=0/P2=0`。Slice C/D/E/F 的 fixed candidates `6fd2873` / `7bbc391` / `f86e8ca` /
`e1cb598` 又分别由 reviews `be597de` / `cb94b53` / `48ba3cc` / `39d5d75` 批准。最终 Slice G
[`typed failure result / complete caller matrix`](p1/runner-ledger-recovery-result-service-matrix-20260823.md)
fixed candidate `2b01ede` 获得
[`40ad401` independent review](p1/runner-ledger-recovery-result-service-independent-review-20260823.md)
`APPROVE, P0=0/P1=0/P2=0`。ADR-0023 A–G 因此只在 ordered local implementation/review 边界上完成；这不构成
production database invocation、HTTP/P2/provider effect、deployment/release 或任何 immutable/aggregate Gate closure。
Inventory R2 因
66 个公开 target 的 ABI/authority 方向冲突被
R3 supersede，任何固定旧 decision digest 的下游证据不得继承。P1 仅允许在公共仓实施 contracts、Go/TS
SDK、数据模型、authority 与安全基础，以及 source modules/本地 ephemeral Postgres 验证；不得由 P0/P1
启动结论外推：

- P2–P6 Managed Agent/Host、Standalone 或 Synara/T3 cutover；
- M1 rc.2 或真实 Provider E2E；
- 生产数据库写入、module/image/Release/npm/Registry 发布；
- 部署、Beta、GA；
- 删除任何脏 worktree。

新范围和架构 decision 在实施前记录；验收计划在执行前记录；实际结果、状态、evidence index 与 closure
record 在对应实施或验证后如实记录，不预先宣称通过。已有实施项内的常规进展沿用其授权范围与记录，
不因补记状态或证据而重新申请同一授权。

应用 E2E 报告保留在对应应用目录，本计划通过链接索引，不复制原始报告。正式 Gate closure 按
[`evidence 规范`](cloud-agents-platform/evidence/README.md) 单独登记；应用报告不自动构成 Gate closure。
