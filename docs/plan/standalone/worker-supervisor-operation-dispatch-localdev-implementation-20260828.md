# D-054-WORKER-DISPATCH-000001.r1 — implementation record

状态：`CANDIDATE_REVIEW_PENDING`（本记录不改变任何 Gate）
Profile：`cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1`

## 实现内容

本候选只把固定的 localdev Supervisor → Worker 进程内路径接通：

- `services/worker/local_dispatch_handle.go` 提供由真实 `worker.Service` 铸造的
  不可伪造 `LocalDispatchHandle`。零值、typed-invalid 或外部 generated Connect
  client 不会获得 dispatch authority；四个 generated client 方法直接转发到同一
  进程的 Service，不创建 URL、HTTP transport、listener 或网络调用。
- `services/worker/supervisor/local_dispatch.go` 提供 `NewLocal`/`NewInProcess`、
  `BindLocalDispatch`、exact negotiation tuple 校验、operation/attempt deadline
  与 context fence、identity/capability/fencing 校验、dispatch 和 detached receipt
  replay。私有 marker 使 `New(Config{Client: ...})` 永远保持 dispatch/receipt
  `Unimplemented`。
- `services/worker/supervisor/local_dispatch_test.go` 覆盖 generated profile、
  opaque handle、generic fail-closed、foreign binding/identity/capability/unknown
  fields、replay、receipt ownership、expiry、post-RPC cancellation/expiry 和响应
  fencing 校验。

## 不变式与边界

- Worker 的 `operation-admission` 与 `operation-execution/localdev-v1alpha1` 仍是
  父 profile；entry/recovery writer、complete-ledger/no-op、D-053 r1/r2/EC-2
  source/profile/schema/manifest/SQL/catalog/archive/review bytes 均未修改。
- receipt 仅存在于 Worker Service 的有界进程内 map，返回值始终 detached；没有
  PostgreSQL、durable receipt、生产 Runner、Provider/P2、Workspace、Artifact、
  Credential、HTTP/TLS、部署、发布或 Gate 状态副作用。
- binding 指针在 RPC 前后做 ABA/lineage fence；caller context、binding expiry 或
  operation deadline 失效时不返回成功 receipt。Worker 可能已在失效竞态前提交其
  进程内 receipt，但 Supervisor 会 fail closed，且不会把它宣称为 durable evidence。
- `Bind` 与 `BindOperationAdmission` 的既有兼容语义保持不变；只有
  `NewLocal` + `BindLocalDispatch` 能选择 generated local dispatch profile。

## 生成物与校验

Authority/profile source、strict Draft 2020-12 schemas、generated Go profile 和
lineage/review 规则见：

`docs/plan/standalone/worker-supervisor-operation-dispatch-localdev-authority-20260828.md`

固定 review 记录路径：

`docs/plan/standalone/worker-supervisor-operation-dispatch-localdev-independent-review-20260828.md`

候选验证命令：

```text
bun scripts/generate-worker-supervisor-local-dispatch-profile.ts --check
bunx vitest run scripts/lib/worker-supervisor-local-dispatch-profile.test.ts --reporter=dot
GOWORK=off GOFLAGS=-mod=readonly go -C services/worker test ./... -count=1 -timeout=5m
GOWORK=off GOFLAGS=-mod=readonly go -C services/worker test -race ./... -count=1 -timeout=5m
GOWORK=off GOFLAGS=-mod=readonly go -C services/worker vet ./...
```

这些结果只证明本地源码、生成物和 focused runtime checks；不证明生产 HTTP、
数据库、provider、部署、发布或 Gate 已关闭。
