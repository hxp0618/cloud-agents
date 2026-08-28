# D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1 — local execution authority

状态：`REVIEW_APPROVED_NON_GATE`（localdev、只读、非 Gate）
日期：2026-08-28（Asia/Shanghai）
Profile：`cloud-agents/managed-agent-worker-local-execution/localdev-v1alpha1`

## 1. 决策与生成物

本 authority 只批准一个 transport-neutral code-bearing slice：在同一进程内把已经存在的
Managed Agent Session → Turn → Execution 生命周期，连接到已批准的
`D-054-WORKER-DISPATCH-000001.r1` `Supervisor.NewLocal` → Worker 路径。它不新增 HTTP、
PostgreSQL、provider、workspace、artifact、credential、部署、发布或 Gate 行为。

source、strict schema、profile 和 Go profile 均由以下生成器产生；默认操作是 `--check`，
`--write` 只用于冻结当前候选：

- `scripts/generate-managed-agent-local-execution-profile.ts`
- `scripts/lib/managed-agent-local-execution-profile.ts`
- `services/control-plane/internal/managedagent/local-execution-profile/v1/authority-source.json`
- `services/control-plane/internal/managedagent/local-execution-profile/v1/authority-source.schema.json`
- `services/control-plane/internal/managedagent/local-execution-profile/v1/profile.json`
- `services/control-plane/internal/managedagent/local-execution-profile/v1/profile.schema.json`
- `services/control-plane/internal/managedagent/local_execution_profile_generated.go`

当前生成 digest（JSON logical digest；文件 raw SHA 由审查记录另行绑定）：

- source：`sha256:e87ebf7f8de39c6addbea4ef9ade99625589b62b882f6f8960e14844c8e7364a`
- profile：`sha256:892a718cfd58e138cbb22e556da2f0088fdc8b73f43b47805b35e9c90f777e74`
- input manifest：`sha256:bcb335668343b2682539f9ea20e9db0f4875f83636d5898fdde26a88e81d51de`

## 2. 完整输入集合与排除集合

输入集合是下列 43 个路径的完整、去重、UTF-8 bytewise 排序列表。生成器只接受 regular
file；不会扫描、追踪、解析或隐式加入其它路径。输入集合的内容绑定算法为
`utf8-bytewise-sorted-path-regular-file-mode-size-sha256-nul-v1`（path、mode、size、
raw SHA-256 以 NUL 分隔后按路径排序）。

```text
.mise.toml
bun.lock
bunfig.toml
contracts/worker/v1alpha1/README.md
contracts/worker/v1alpha1/kernel.proto
contracts/worker/v1alpha1/worker_supervisor.proto
go.work
package.json
scripts/generate-managed-agent-local-execution-profile.ts
scripts/lib/managed-agent-local-execution-profile.test.ts
scripts/lib/managed-agent-local-execution-profile.ts
scripts/lib/platform-json-semantics.ts
sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go
sdk/go/gen/cloudagents/worker/v1alpha1/worker_supervisor.pb.go
sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect/worker_supervisor.connect.go
sdk/go/gen/common/v1alpha1/identity_generated.go
sdk/go/go.mod
sdk/go/go.sum
services/control-plane/go.mod
services/control-plane/go.sum
services/control-plane/internal/managedagent/events.go
services/control-plane/internal/managedagent/lifecycle.go
services/control-plane/internal/managedagent/local_execution.go
services/control-plane/internal/managedagent/local_execution_test.go
services/control-plane/internal/managedagent/profile.go
services/worker/execution.go
services/worker/execution_test.go
services/worker/go.mod
services/worker/go.sum
services/worker/local_dispatch_handle.go
services/worker/operation_admission.go
services/worker/operation_admission_test.go
services/worker/operation_builder.go
services/worker/operation_builder_test.go
services/worker/service.go
services/worker/service_test.go
services/worker/supervisor/client.go
services/worker/supervisor/dispatch-profile/v1/authority-source.json
services/worker/supervisor/dispatch-profile/v1/profile.json
services/worker/supervisor/dispatch_profile_generated.go
services/worker/supervisor/local_dispatch.go
services/worker/supervisor/local_dispatch_test.go
tsconfig.base.json
```

排除集合是下列 13 个 exact path/root；root 按递归 deny 解释。它们以及未列入输入集合的
文件、untracked 文件、symlink、special file、duplicate path 都不可作为本 authority 的
输入：

```text
.idea
contracts/managed-agent/v1alpha1/openapi.json
contracts/platform/v1alpha1
deploy
helm
packages/cloud-agent-provider-api
release
services/control-plane/internal/migration
services/control-plane/internal/server
services/control-plane/internal/store/postgres
services/control-plane/migrations
services/worker/cmd
tools/g-contract-external-consumer
```

`inputManifestDigest` 为 `sha256:bcb335668343b2682539f9ea20e9db0f4875f83636d5898fdde26a88e81d51de`；它按上述
算法对每个输入的 path、Git mode、byte size 与 raw SHA-256 做 NUL 分隔聚合。任何输入内容、mode、
size 或锁文件变化都会使 authority/profile 生成检查失败。上述集合不改变、替换或重算
D-053-MIG-000014.r2、D-053-EC-2.r3 或其任何 source/profile/
schema/manifest/SQL/catalog/archive/review bytes。

## 3. Archive 与 member-manifest 算法

本 slice 不产生 archive 或 member-manifest；其输出状态固定为 `forbidden`，避免把内存
receipt 或测试目录误报为可发布 artifact。为后续 successor 保持可复核的算法标签：

- archive：`deterministic-ustar-v1`，无压缩，UTF-8 bytewise path 排序，固定
  `mode=100644,uid=0,gid=0,mtime=0`，duplicate/symlink reject；本 revision 的 emission
  为 `forbidden`。
- member-manifest：`utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`，字段序列为
  `path\0mode\0size\0sha256\0`，regular-file-only，duplicate reject；path 固定
  `<not-written>`。

## 4. Runner、toolchain、platform 与 selector

唯一 runner 是 focused local test runner：
`GOWORK=off GOFLAGS=-mod=readonly go test`。冻结环境为 Go `1.26.0` / toolchain
`go1.26.6`、Node `24.18.1`、Bun `1.3.14`，声明平台为 `darwin-arm64` 与 `linux-amd64`。
network、database、provider 均为 `deny`，超时策略为 `focused-tests-only`。本 authority 不
声称任何远程主机、生产部署或真实 provider E2E 证据。

连接前必须已经通过 `NewLocal` + `BindLocalDispatch` 获得 exact generated D-054 binding；
coordinator 只接受 profile ID `cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1`
及其当前 generated digest。caller-selected profile、generic Supervisor、foreign path、
foreign transport、隐式 negotiation 和 fallback 均 `forbidden`。

作用域投影固定为 `sha256-length-prefixed-tenant-project-v1`：以版本域分隔符、tenant UTF-8
字节长度+字节、project UTF-8 字节长度+字节组成 canonical frame，取 SHA-256，并以
`scope-<64 lowercase hex>` 作为 Worker `NamespaceRef.id`。因此含 `~` 的合法生命周期 ID 仍保持
单射，不依赖下游 idempotency 冲突来隔离租户。

允许的 command 只有 `Probe`、`ValidateBinding`；extension payload、Codex/Claude provider
调用和任意外部执行均拒绝。fencing lease/generation/token 在 coordinator constructor 内
绑定并复制，调用者输入必须逐字节匹配；attempt number 固定为 `1`。

`stateMachine` 是本 slice 的 execution-pair projection：profile 的 `queued/running/succeeded/
failed/cancelled` 分别对应 lifecycle 的 `TurnQueued/TurnRunning/TurnCompleted|TurnFailed/
TurnCancelled` 与 `ExecutionQueued/ExecutionRunning/ExecutionSucceeded|ExecutionFailed/
ExecutionCancelled`；`TurnInterrupted` 不属于本 coordinator command surface。完整 typed
dispatch attempt、lifecycle identity、input digest、generation 与 D-055 profile digest 还会
形成内部 dispatch-binding digest，并在第一次 lifecycle mutation 前绑定 idempotency key；
同 key 的 command、operation/attempt identity 或 dispatch 字段漂移会 fail closed。

## 5. Lifecycle、receipt 与 lineage fence

状态顺序固定为：

```text
CreateTurn(queued)
  -> CreateExecution(queued)
  -> StartExecution(running)
  -> Supervisor.DispatchOperation
  -> receipt outcome
       SUCCEEDED            -> CompleteExecution(succeeded)
       FAILED              -> FailExecution(failed)
       DEADLINE_EXCEEDED   -> FailExecution(error=deadline_exceeded)
       FENCED              -> FailExecution(error=fenced)
       CANCELLED           -> CancelTurn(cancelled)
```

`Store` 与 Worker receipt 都是进程内、有界、detached 的状态；receipt runtime path 固定为
`process-local://worker-service/receipts`，persistence 为 `no_write`，状态为
`ABSENT_PENDING`。独立 review/implementation 记录路径固定为：

- `docs/plan/standalone/managed-agent-worker-local-execution-implementation-20260828.md`
- `docs/plan/standalone/managed-agent-worker-local-execution-independent-review-20260828.md`

成功结果 digest 算法固定为
`sha256:deterministic-protobuf-receipt-result-v1`：对 detached `DurableReceipt` 做
`proto3` deterministic marshal 前清除 `receipt_id`、`sequence`、`observed_at`、
`fencing.token_sha256`，再 SHA-256。不得使用 receipt ID/sequence 作为结果 digest，也不得
把该 digest 当成 durable artifact。稳定失败码只允许：
`execution_failed`、`worker_dispatch_failed`、`deadline_exceeded`、`fenced`、`cancelled`、
`worker_failed`；其它 Worker 字符串统一归类为 `worker_failed`。

Lineage 是 `single-predecessor-append-only`：

- predecessor：`D-054-WORKER-DISPATCH-000001.r1`，profile
  `cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1`，digest
  `sha256:4ed83884e50cf2f55e9799a16afe28c97cf5756969ae47cdc082a1987b5ddbc1`；
- historical authorities：`D-053-MIG-000014.r2`、`D-053-EC-2.r3`，只允许 immutable
  reference；
- mutation：`forbidden`；历史证据 `retain-and-never-rewrite`。

## 6. Review 与非授权边界

必须执行一次独立只读 review，输出 `APPROVE` 或 `REQUEST_CHANGES`，分别计数 P0/P1/P2。
reviewer 不得修改 candidate、不得改变 profile/source/schema bytes、不得转换 Gate。P0/P1
最多在同一 r1 candidate 内修复一次并重新审查；P2 记录并延期，不创建 r2/r3。review 记录
必须绑定 candidate SHA、生成 digest、输入/排除集合、算法、runner/toolchain/platform、
receipt path 与 lineage fence。

本 revision 的独立只读 review 记录为
`docs/plan/standalone/managed-agent-worker-local-execution-independent-review-20260828.md`，
固定 candidate 为 `b79d01028c652d004e67a00fdcbdf204e04dc946`，verdict 为
`APPROVE`（P0=0 / P1=0 / P2=0）。该记录是 candidate 之后的 append-only review child，
不改变 candidate 或其 generated bytes。

所有 external side effects 均为 `false`：database、durableReceipt、HTTP、P2、provider、
workspace、artifact、credential、deployment、publication、Gate。entry/recovery writer
保持 `NOT_IMPLEMENTED`，complete-ledger 不在本 slice 实现。该 authority 只提供 localdev
focused evidence，不关闭任何 Gate，也不授权生产数据库写入、HTTP/P2/provider、部署或发布。
