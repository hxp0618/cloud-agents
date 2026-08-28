# D-054-WORKER-DISPATCH-000001.r1 — independent read-only review

Verdict：`APPROVE`
审查日期：2026-08-28（Asia/Shanghai）
审查方式：独立只读；reviewer 未修改候选、未切换候选分支、未执行外部副作用

## 固定候选与一次修复

- 初始 implementation candidate：`7c2ae88e4392cef3f10d51e0f6261142335e6f4b`
- 唯一 P1 repair：`9196155fc0116753331e4cf18f607ff88dd9904f`
- reviewed candidate（固定 SHA）：`9196155fc0116753331e4cf18f607ff88dd9904f`
- repair 内容：仅将 authority 文档中的 source/profile digest 对齐到 checked-in
  generated authority/profile（同一 r1 candidate 内完成；未创建 r2/r3）

## P0/P1/P2 verdict

| 类别 | 数量 | 结论 |
| --- | ---: | --- |
| P0 | 0 | 未发现阻断性安全、身份、数据完整性或越界副作用问题 |
| P1 | 0 | 初始唯一 P1（authority digest 文档漂移）已按批准规则修复并复审 |
| P2 | 0 | 无延期项 |

## 审查证据

### Authority/profile

- profile：`cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1`
- authority：`D-054-WORKER-DISPATCH-000001.r1`
- source digest：`sha256:b97cb1e464e1cd01e4a42eae270834b45c8db92deddc964f7652fb68417565fa`
- generated profile digest：`sha256:4ed83884e50cf2f55e9799a16afe28c97cf5756969ae47cdc082a1987b5ddbc1`
- D-053 predecessor：`D-053-MIG-000014.r2`，其 source/profile/schema/manifest/SQL/catalog/archive/review
  bytes 未修改；lineage 仍为 `single-predecessor-append-only`
- strict source/profile schemas、exact parent profile 顺序、capabilities、commands、limits、
  reviewPath 和所有 external-side-effect `false` 项均与 generator 一致

### Code and boundary

- `LocalDispatchHandle` 只由真实 Worker `Service` 铸造，私有 marker 使 zero/forged handle
  与 generic `New(Config{Client: ...})` fail closed
- 只有 `NewLocal` + `BindLocalDispatch` 能选择 generated local dispatch profile；旧 `Bind` /
  `BindOperationAdmission` 和 generic dispatch/receipt no-op 兼容行为保持不变
- dispatch/receipt 在 RPC 前后校验 exact negotiation tuple、profile、Worker identity、
  capability、fencing lease/generation/token digest、unknown fields、wire bound、deadline/cancel
  和 ABA binding pointer；返回 receipt 为 detached clone，Worker map 仅进程内有界保存
- 未发现 HTTP/TLS/network listener、PostgreSQL/durable receipt、production Runner、Provider/P2、
  Workspace/Artifact/Credential、deployment、publication 或 Gate 状态变化

## 独立重跑命令与结果

```text
bun scripts/generate-worker-supervisor-local-dispatch-profile.ts --check   PASS
bunx vitest run scripts/lib/worker-supervisor-local-dispatch-profile.test.ts --reporter=dot   PASS (3/3)
GOWORK=off GOFLAGS=-mod=readonly go -C services/worker test ./... -count=1 -timeout=5m   PASS
GOWORK=off GOFLAGS=-mod=readonly go -C services/worker test -race ./... -count=1 -timeout=5m   PASS
GOWORK=off GOFLAGS=-mod=readonly go -C services/worker vet ./...   PASS
git diff --check   PASS
```

## 审查边界

该 `APPROVE` 只允许在既有批准范围内进入下一个 code-bearing P1 slice；不等同于
生产 Runner、数据库写入、HTTP/P2/provider、部署、发布或任何 Gate closure 证据。P2 若后续
出现，按 authority 的 `record-and-defer` 规则处理。
