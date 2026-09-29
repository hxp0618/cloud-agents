# 06. 当前状态与最终汇总

> 最后更新：2026-09-29。本文是 Cloud Agents 当前实际状态的唯一活动记录；第一阶段能力的可复现证据见 [phase-1 验收](../../acceptance/phase-1.md)；旧运行明细已在开源整理前归档，不在这里重复。

## 1. 最终结论

历史 r741 candidate 的 tenant-local 验收记录为：四个 Provider 在 Docker、outbound RemoteWorker、Kubernetes 三类环境共 12 个 Provider×Environment 单元均为 `PASS`。适用的 Worker/Agent 故障、跨节点接管和恢复路径均有 `PASS` 或 `NOT APPLICABLE` 结论；独立只读 reviewer 已 `APPROVE`。该记录使用 `29afe9b9103cac99088f637e02a8cd9bb3f48d58` 的 dirty candidate。

aggregate Gate、release Gate 与 feature closeout 对 r741 candidate 的记录为 `CLOSED / APPROVED`。本结论不改变 capability catalog 的既有 adapter capability 语义，也不把历史 candidate 的中间状态重新解释为当前工作区状态。当前开源候选的边界和未复验项见下方 §1.1。

## 1.1 当前合并候选边界

当前分支为 `codex/cloud-agents-platform-p0`，HEAD 为 `d80d825fb622dd8b538751ef0f0f880b82701a59`；工作区存在未提交的生产代码、生成器、迁移、测试和文档改动。r741 之后的改动尚未在一个干净 candidate 上重新完成 Provider×环境、恢复、跨节点、故障和安全矩阵，因此当前合并候选各格均记为 `NOT RUN`，不继承历史 `PASS`。这不是对 r741 正式记录的重开，而是对当前 dirty source 的合并边界。

## 2. Provider × Environment 矩阵（r741 历史证据）

| Provider | Docker | outbound RemoteWorker | Kubernetes |
| --- | --- | --- | --- |
| Codex | `PASS` | `PASS` | `PASS` |
| Claude Code (`claudeAgent`) | `PASS` | `PASS` | `PASS` |
| Pi | `PASS` | `PASS` | `PASS` |
| deepseek-harness | `PASS` | `PASS` | `PASS` |

矩阵状态只代表 r741 candidate 的 tenant-local 真实证据。历史模型、未计数 preflight、provider availability 诊断和 repair history 不计入该矩阵。

## 3. 共同验收结果（r741 历史证据）

| 验收面 | 最终结果 |
| --- | --- |
| MCP/Skill/Artifact 与事件续读 | 适用单元均通过 |
| transport/reconnect 与 replay 约束 | 适用单元均通过；未确认结果不盲目重放 |
| capability revoke、stale generation、跨租户和版本/digest 不兼容 | fail-closed 负向路径均通过 |
| unknown side effect | 先形成 checkpoint、完成 outcome reconcile，再允许恢复 |
| Worker/Agent 进程故障与运行恢复 | 适用路径 `PASS`；不适用路径 `NOT APPLICABLE` |
| RemoteWorker/Kubernetes 跨节点接管 | 适用路径 `PASS`；Docker cross-node 按适用性记载 |
| Admin/Web 数据边界 | 只暴露 opaque 元数据、状态、稳定错误码和审计引用，不暴露用户内容或 Secret |

## 4. 安全与清理（r741 历史证据）

- 凭据、Token、Authorization、Prompt、源码、工具输入输出和 MCP 返回内容不进入页面、日志、事件、Artifact 或 Workspace snapshot。
- 失败、断连、旧 generation、能力撤销和未知副作用保持 fail-closed；没有通过终态证据的路径不升级为 `supported`。
- 本轮 task-owned Compose、RemoteWorker、Kubernetes、OpenSandbox、fixture、临时凭据和网络/卷资源均已精确清理。

## 5. 工程维护

- 2026-09-28：按用户指示退役 `vitest run scripts` 中稳定失败的 102 个用例（含整个 `scripts/generate-sbom.test.ts`），其余 57 个文件、462 个用例在本地通过；CI/release 改为运行完整 `vitest run scripts` 与 `platform:migrations:check`。退役用例包括当前 release 相关断言（`platform-release.test.ts` 2 个、SBOM 1 个），不是只有历史 Gate 证据；需要恢复时从 git 历史取回并按当前产物更新。本地验证未使用锁定工具链。
- 2026-09-29：MAINT-1 `DONE`。产品迁移内核为 `internal/migrationcore`，历史 `internal/migration`（约 66k 行非测试代码）、`cloud-agents-migrate`、`data-recovery-validator`、分片测试工具和 5 个 runner-ledger Go 生成器已退役；同时退役 8 个校验这些已删除字节的 scripts 用例。Go 1.26.6 容器内 `scripts/test-platform-go-products.sh` 通过（58s，含 race/vet），`platform:go:check`（锁定 Go 1.26.6）、`platform:migrations:check`、`vitest run scripts`（58 个文件、457 个用例）通过。`migrationcore` 由自身测试和 `localmigration` 测试合并覆盖约 40% 语句；列/约束/索引/策略/触发器与表达式节点校验、`SQLLedgerStore.Insert` 尚无直接测试。
- 2026-09-29：MAINT-2 `DONE`。产品迁移 catalog 改为生成器产出的逐版本补丁（`product/` 约 98M → 21M），manifest/schema-bundle 与所有冻结 digest 不变；`platform:migrations:check`、`localmigration` 与产品迁移命令 Go 测试及 `go vet` 通过，补丁/基线篡改、多余或缺失文件均被拒绝。
- 2026-09-29：MAINT-3 `DONE`（范围见 [04 §0.6](04-extraction-and-migration.md#engineering-maintenance-items)）。replay v2/v3 与 closure-profile v3/v4 各合并为共享内核；在各自历史检查为绿的提交（`6af45061`、`700bc72f`）上叠加新代码后，相关生成器检查的退出码与输出、全部 scripts 用例的逐例状态均与原代码一致，closure v3/v4 `--write-source`/`--write` 重新生成的已提交产物逐字节不变。当前工作树 `vitest run scripts` 58 个文件、457 个用例通过。
- 已知问题：MAINT-1 删除 `internal/migration` 后，`scripts/generate-platform-migration-bundle-successor.ts --check` 因仍固定 `internal/migration/bundle.go` 而失败（该检查不在 `platform:migrations:check`/CI 中）。

## 6. 证据入口

- [phase-1 验收](../../acceptance/phase-1.md)：能力 → 实现位置 → 检查命令 → 结果。
- [Evidence index](evidence/README.md)：冻结 Gate 记录。
- [Gate closure template](templates/gate-closure-record.md)：正式 closure record 格式。
- [数据迁移、删除与回滚安全要求](migration-and-rollback-safety.md)。

## 7. 后续文档规则

- `04` 只维护计划，`05` 只维护验收标准，`07` 只维护需求与设计；任何新的实际结果先形成证据，再在本页更新汇总。
- 不在活动入口追加逐轮命令、运行编号、digest、修复尝试或重复的 Admin 投影。
- 文档收口不等于代码、部署、生产写入、数据迁移或新的 Gate 授权；这些仍按项目既有边界处理。
