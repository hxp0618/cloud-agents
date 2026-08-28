# D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1 — independent read-only review

日期：2026-08-28（Asia/Shanghai）
审查方式：独立只读；reviewer 在 candidate 的 clean archive 副本中工作，未修改
candidate、未写入生产数据库、未执行 HTTP/P2/provider、部署、发布或 Gate 操作。

## 固定 candidate 与 verdict

本记录审查的 candidate 是一个已经固定的单 parent commit；本记录只作为其后续
review child 添加，不把 review commit/tree 写入自身，避免自引用：

| 项目 | 值 |
| --- | --- |
| candidate commit | `b79d01028c652d004e67a00fdcbdf204e04dc946` |
| candidate tree | `289c7c2ff7ab39b0af1ea0bac84a902d461de8dc` |
| direct parent | `4ee0e847a7c8e6d0c7313f0f359acc7002ec9d97` |
| authority | `D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1` |
| profile | `cloud-agents/managed-agent-worker-local-execution/localdev-v1alpha1` |

**Verdict：`APPROVE` — P0=0 / P1=0 / P2=0。**

该 verdict 只批准进入下一个已批准的 code-bearing P1 slice；不表示生产 Runner、
PostgreSQL、HTTP/P2/provider、部署、发布或任何 Gate 已就绪或关闭。

## Authority/profile evidence

candidate 中的 generated authority、strict source/profile schema、Go profile 和
generator 相互一致：

- source logical digest：`sha256:e87ebf7f8de39c6addbea4ef9ade99625589b62b882f6f8960e14844c8e7364a`；
- profile logical digest：`sha256:892a718cfd58e138cbb22e556da2f0088fdc8b73f43b47805b35e9c90f777e74`；
- 43-path input manifest digest：`sha256:bcb335668343b2682539f9ea20e9db0f4875f83636d5898fdde26a88e81d51de`；
- input set 为 43 个 regular files，排除集合为 13 个 exact path/root，generated 集合为
  5 个 path；`operation_builder_test.go` 已包含在 input set；
- schema 在 top-level 与 nested objects 均为 `additionalProperties=false`，identity、
  path set、archive/member-manifest labels、runner、receipt、lineage 和 review rules
  均为 exact const/array/map 约束；
- predecessor 为 `D-054-WORKER-DISPATCH-000001.r1`，profile
  `cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1`，digest
  `sha256:4ed83884e50cf2f55e9799a16afe28c97cf5756969ae47cdc082a1987b5ddbc1`；
  `D-053-MIG-000014.r2` 与 `D-053-EC-2.r3` 仅以 immutable historical reference 保留。

## Code and fail-closed checks

reviewer 逐项检查了 `local_execution.go`、`lifecycle.go`、`operation_builder.go` 及其
tests：

- `NewLocal` + `BindLocalDispatch` 是唯一允许的 generated D-054 binding；coordinator
  在 lifecycle mutation 前校验 D-054 `Valid()`、profile ID 与 exact digest；
- typed `BuildLocalOperationAttempt` 重算 canonical request，校验 identity、scope、
  deadline、fencing、command vocabulary 与 detached nested values；只允许 `Probe`、
  `ValidateBinding`；
- 完整 typed attempt、lifecycle identity、input digest、generation 与 D-055 profile
  digest 形成内部 dispatch-binding digest，并写入不可由 transport caller 设置的
  mutation binding；同一 idempotency key 改动 command、operation ID 或 attempt ID 会在
  `CreateTurn` 前返回 `ErrIdempotencyConflict`，不会到达 Worker；
- lifecycle 顺序为 queued → running → terminal，terminal pair、execution link、
  generation 和并发 reconciliation 均 fail closed；parallel replay 只执行 Worker 一次；
- receipt 仅为 `process-local://worker-service/receipts` 的 bounded detached 值，
  `persistence=no_write`、`ABSENT_PENDING`；volatile receipt fields 在 result digest
  前清除，稳定错误码超集归类为 `worker_failed`；
- 未发现 HTTP listener/client、PostgreSQL/durable receipt、provider/P2、workspace、
  credential、artifact、production runner、deployment、publication 或 Gate actuator。

## Independent runner evidence

审查在 candidate 的 clean archive 中使用冻结的 darwin-arm64 toolchain：Go
`go1.26.6`、Bun `1.3.14`、Node `v24.18.1`。Go 命令均设置
`GOWORK=off GOFLAGS=-mod=readonly`；Bun 依赖使用 frozen lockfile 安装。结果如下：

| 检查 | 结果 |
| --- | --- |
| `bun scripts/generate-managed-agent-local-execution-profile.ts --check` | PASS |
| D-055 Vitest focused test | PASS（3/3） |
| D-055 TypeScript `oxfmt --check` / `oxlint --deny-warnings` | PASS |
| Control Plane `go test ./internal/managedagent -count=1` | PASS |
| Control Plane `go test -race ./internal/managedagent -count=1` | PASS |
| Control Plane `go vet ./internal/managedagent` | PASS |
| Control Plane `go mod tidy -diff` | PASS（empty diff） |
| Control Plane `go mod verify` | PASS（all modules verified） |
| Worker `go test ./... -count=1` | PASS |
| Worker `go test -race ./... -count=1` | PASS |
| Worker `go vet ./...` | PASS |
| D-054 generated profile `--check` | PASS |
| forbidden-effect/source boundary scan | PASS；未发现 HTTP/DB/provider/exec actuator |

## Findings and repair fence

旧的 pre-repair review 曾指出 input manifest 漏列 `operation_builder_test.go` 与
runner evidence 不足；同一 D-055 r1 candidate repair 已补齐路径、重算全部 generated
digests，并使用 exact pinned Go/Bun/Node 重跑。随后本记录只审查上述固定 SHA，未发现
新的 P0、P1 或 P2。没有创建 r2/r3；任何后续变化必须新建 versioned successor。

reviewer 没有修改 candidate。review child 应为 candidate 的 single-parent append-only
child，并且只新增本文件；review record、authority/profile digest、完整输入/排除集合、
archive/member-manifest 算法、runner/toolchain/platform、receipt path、lineage fence
和本 verdict 均由当前 frozen profile 约束。`notGateClosure=true`，`gateEffect=NO_GATE_CLOSURE`。
