# D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1 — implementation record

日期：2026-08-28（Asia/Shanghai）  
Profile：`cloud-agents/managed-agent-worker-local-execution/localdev-v1alpha1`  
状态：`REVIEW_APPROVED_NON_GATE`

## 决策与范围

本实现把既有 Managed Agent 的内存 Session → Turn → Execution state machine 接到已批准的
`D-054-WORKER-DISPATCH-000001.r1` localdev `Supervisor.NewLocal` → Worker seam。它只使用
进程内对象和 detached receipt；没有 listener、URL、HTTP、PostgreSQL、durable receipt、
provider、workspace、credential、artifact、部署、发布或 Gate 写入。`entryWriter`、
`recoveryWriter` 仍为 `NOT_IMPLEMENTED`，complete-ledger 不在本 slice 中实现。

实现文件：

- `services/control-plane/internal/managedagent/lifecycle.go`（为 coordinator mutation
  binding 提供内部 digest fence）
- `services/control-plane/internal/managedagent/local_execution.go`
- `services/control-plane/internal/managedagent/local_execution_test.go`
- `services/worker/operation_builder.go`
- `services/worker/operation_builder_test.go`
- D-055 generated authority/profile/schema/Go outputs 与其 generator
- 本记录和后续独立只读 review 记录

## 固定执行路径

`LocalExecutionCoordinator` 的 constructor 只接受显式 Store、Clock、Supervisor 和绑定的
fencing lease/generation/token，并复制 token。`Execute` 在任何 lifecycle mutation 前检查：

- context、clock、tenant/project、所有 identity、attempt number、输入大小/UTF-8、deadline、
  mutation 和 fencing proof；caller fencing proof 必须与 constructor authority 做逐字节
  constant-time 比较；
- D-055 generated profile 与 D-054 generated profile 的 `Valid()`、profile ID 和 D-054
  exact profile digest；Supervisor 必须已经由 `NewLocal` + `BindLocalDispatch` 建立有效 binding；
- command 只能是 `Probe` 或 `ValidateBinding`，不接受 extension payload、caller-selected
  transport/profile 或隐式 negotiation。

通过检查后，路径严格为：

```text
CreateTurn(queued)
  -> CreateExecution(queued)
  -> StartExecution(running)
  -> Supervisor.DispatchOperation
  -> detached Worker receipt
       SUCCEEDED            -> CompleteExecution(succeeded)
       FAILED              -> FailExecution(failed)
       DEADLINE_EXCEEDED   -> FailExecution(deadline_exceeded)
       FENCED              -> FailExecution(fenced)
       CANCELLED           -> CancelTurn(cancelled)
```

Worker operation attempt 由 typed `BuildLocalOperationAttempt` 构造；canonical request digest
由 builder 重算，raw fencing token 只存在于本次进程内调用，不进入 lifecycle event 或 receipt
result digest。coordinator 另外对完整的 typed attempt、lifecycle identity、input digest、
generation 和 D-055 profile digest 计算内部 `dispatch-binding` SHA-256，并把它放入不可由
transport caller 设置的 lifecycle mutation binding；因此同一 idempotency key 改动 command、
operation/attempt identity 或其它 dispatch 字段，会在首次 `CreateTurn` 前返回
`ErrIdempotencyConflict`，不会触发 Worker dispatch。所有 terminal mutation 使用 Store 的
append-only event/idempotency kernel；
重复调用复用同一 mutation/Worker attempt，Worker executor 只执行一次。终态竞争通过读取
coupled Turn/Execution 快照并要求期望的 terminal pair 进行 reconciliation；generation、
execution link 不匹配时 fail closed。

## 作用域与 receipt 绑定

Lifecycle `Scope{TenantID,ProjectID}` 不再用可碰撞的 `tenant~project` 拼接。D-055 冻结的
`sha256-length-prefixed-tenant-project-v1` frame 为：版本域 `cloud-agents/managed-agent-
worker-scope/v1` 与 NUL、tenant UTF-8 byte length（u32 big-endian）+ bytes、project UTF-8
byte length（u32 big-endian）+ bytes；其 SHA-256 hex 加 `scope-` 前缀形成 Worker
`NamespaceRef.id`。因此生命周期允许的 `~` 字符不会造成跨租户 NamespaceRef collision。

成功 receipt 的 lifecycle result digest 固定为
`sha256:deterministic-protobuf-receipt-result-v1`：对 detached receipt 做 proto3
deterministic marshal 前清除 `receipt_id`、`sequence`、`observed_at`、
`fencing.token_sha256`，再取 SHA-256。receipt runtime path 为
`process-local://worker-service/receipts`，persistence 为 `no_write`，状态为
`ABSENT_PENDING`。未声明的 Worker stable error code（包括 `receipt_missing`）统一归类为
冻结 allowlist 中的 `worker_failed`。

## 生成 authority 与输入完整性

D-055 generator 使用 strict Draft 2020-12 `additionalProperties=false` schema，固定完整
输入/排除路径集合，并以
`utf8-bytewise-sorted-path-regular-file-mode-size-sha256-nul-v1` 计算 input manifest digest。
生成器拒绝 symlink/special file、重复路径、输入/排除/生成集合重叠和生成物漂移；archive
与 member-manifest 算法只作为 successor-compatible labels 冻结，本 revision 的 emission
均为 `forbidden`。Go/Bun lockfiles、`.mise.toml`、package metadata 和 JSON canonicalizer
均已纳入 input set。

## 验证边界

本记录的验证只针对 localdev focused slice：生成器/schema、managedagent/worker focused
unit tests、race、vet、module readonly 和 negative checks。它不证明生产 Runner、真实硬断电、
HTTP/P2/provider、PostgreSQL、部署、发布或任何 Gate 状态；这些边界由 D-055 authority
的 `externalSideEffects=false` 和 `reviewRules.gateTransition=forbidden` 固定。

独立只读 review 已完成并记录于
`docs/plan/standalone/managed-agent-worker-local-execution-independent-review-20260828.md`：
固定 candidate `b79d01028c652d004e67a00fdcbdf204e04dc946`，verdict
`APPROVE`（P0=0 / P1=0 / P2=0）。review child 不修改 candidate；没有创建 r2/r3。
