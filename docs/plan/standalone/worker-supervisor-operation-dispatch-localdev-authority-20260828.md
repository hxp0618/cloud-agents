# D-054-WORKER-DISPATCH-000001.r1 — localdev Supervisor dispatch authority

状态：`AUTHORITY_FROZEN_REVIEW_PENDING`（只读审查候选；不关闭 Gate）
日期：2026-08-28（Asia/Shanghai）
Profile：`cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1`

## 1. 决策与冻结对象

本 authority 只批准 Standalone Platform v0.1 的一个真实 code-bearing slice：在同一进程
内，通过已绑定的 Supervisor client 将一个已经通过 Worker operation-admission、并由
operation-execution/localdev-v1alpha1 接受的操作交给 Worker 执行。绑定、dispatch、attempt
幂等和有界临时 receipt 都必须由 generated profile 固定；不得从 caller、HTTP、配置文件或
任意路径选择 profile。`--check` 是生成器默认模式，`--write` 只用于当前候选冻结。

生成器及其 checked-in outputs 为：

- `scripts/generate-worker-supervisor-local-dispatch-profile.ts`
- `scripts/lib/worker-supervisor-local-dispatch-profile.ts`
- `services/worker/supervisor/dispatch-profile/v1/authority-source.json`
- `services/worker/supervisor/dispatch-profile/v1/authority-source.schema.json`
- `services/worker/supervisor/dispatch-profile/v1/profile.json`
- `services/worker/supervisor/dispatch-profile/v1/profile.schema.json`
- `services/worker/supervisor/dispatch_profile_generated.go`

当前冻结 digest（由 generator 计算，格式为 `sha256:<64 lowercase hex>`）：

- authority source：`sha256:b97cb1e464e1cd01e4a42eae270834b45c8db92deddc964f7652fb68417565fa`
- generated profile：`sha256:4ed83884e50cf2f55e9799a16afe28c97cf5756969ae47cdc082a1987b5ddbc1`

## 2. 固定 profile contract

Profile 只允许 `mode=localdev_only`、`transport=in_process`；generic client dispatch、
caller-selected profile、foreign transport 和 network listener 均为 `forbidden`。父 profile
按以下顺序且不可替换：

1. `cloud-agents/worker-operation-admission/v1alpha1`
2. `cloud-agents/worker-operation-execution/localdev-v1alpha1`

固定 capabilities 为 `negotiation`、`health`、`operation_dispatch`；固定 command 只有
`Probe` 与 `ValidateBinding`。固定 Worker 限制为 protocol `1.0`、wire `1048576` bytes、
repeated items `64`、string `1024` bytes、payload `65536` bytes、deadline `300` seconds、
identifier `256` bytes、fencing token `65536` bytes、admission records `1024` 和 receipt
records `1024`。

所有 external side effects 均为 `false`：database、durable receipt、HTTP、P2、provider、
workspace、credential、artifact、deployment 和 publication。实现只能持有进程内有界状态，
不得把临时 receipt 当成生产 durable receipt。

## 3. D-053 lineage fence

本 r1 仅 append 到并引用 D-053，不重写、重算、替代或修改 D-053-MIG-000014.r2 的任何
source/profile/schema/manifest/SQL/catalog/archive/review bytes。前置 authority 固定为：

- authority：`D-053-MIG-000014.r2`
- profile：`cloud-agents-platform-migration-runner-binding/v1`
- profile logical digest：
  `sha256:7ffe830d854e5037994e2b5019da792a42d97928da456639bcdbfc4c3fa05489`
- mutation：`forbidden`

Lineage 是 `single-predecessor-append-only`，历史证据 `retain-and-never-rewrite`。任何
D-053 对象只可通过固定 authority/profile/digest 引用；不得以当前工作树中“自洽但身份不同”
的对象替换。

## 4. 实现边界

允许：进程内 Supervisor→Worker dispatch、固定 operation/attempt identity、严格幂等、
身份/能力/fencing 校验、cancel/deadline、以及 detached bounded process-local receipt replay。

禁止：HTTP/TLS/network listener、PostgreSQL 或任何 durable receipt、production Runner、
Codex/Claude/provider invocation、P2 surface、workspace/credential/artifact 访问、部署、
发布及 Gate 状态变化。任何不属于两个固定 command 的输入必须 fail closed；generic client
不能提交 dispatch profile 或能力集合。

## 5. 审查与验证

审查必须是一次独立只读 review，固定记录路径为
`docs/plan/standalone/worker-supervisor-operation-dispatch-localdev-independent-review-20260828.md`，
输出 `APPROVE` 或 `REQUEST_CHANGES`，并分别记录 P0/P1/P2。
候选不可由 reviewer 修改，review 不可转换 Gate。P0/P1 finding 允许在同一个 r1 candidate
内修复一次并重新审查；P2 记录后延期，不创建 r2/r3。审查应确认生成物 digest、strict
Draft 2020-12 schema、parent profile 顺序、固定 capabilities/commands/limits、D-053
immutability 和所有 external-effect false 边界。

本 slice 的 focused checks：

```text
bun scripts/generate-worker-supervisor-local-dispatch-profile.ts --check
bunx vitest run scripts/lib/worker-supervisor-local-dispatch-profile.test.ts --reporter=dot
```

这些检查只证明生成器/schema/unit evidence，不证明生产 HTTP、数据库、provider、部署、
发布或 Gate 状态。
