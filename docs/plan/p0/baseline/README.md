# P0 Protocol 与 baseline manifest

本目录把两个不同的 closure 明确拆开：

- `platformP0CharacterizationClosure=true`：只表示固定 ref 上的 Synara legacy、T3 embedded 机制，以及
  greenfield spec/negative/reference-host fixture 已形成完整、可审计的 P0 characterization；
- `m1BehaviorClosure=false`：真实 Provider、SendTurn、Workspace 修改、checkpoint/rollback、重连和 same-bits
  行为仍为 `NOT_RUN`，由 M1 单独关闭。

P0 characterization 完整不反向要求先执行 M1；本目录也不直接声明 aggregate `G-BASELINE` 为
`VERIFIED`，最终 Gate 状态由 canonical tracker/closure record 的主流程裁决。

## 固定清单

- [`synara-legacy.json`](synara-legacy.json)：Synara managed-agent 的固定 commit/tree/blob/SHA-256、能力与缺口；
- [`t3-embedded.json`](t3-embedded.json)：T3 embedded authority 与 Cloud Agent consumer 的两个固定 ref；
- [`reference-host-negative.json`](reference-host-negative.json)：公共 Runtime/reference-host 负向测试来源及版本化 Protocol corpus digest。

Protocol corpus 位于
[`packages/cloud-agent-protocol/fixtures/p0`](../../../../packages/cloud-agent-protocol/fixtures/p0)，由真实公共
structural validator、当前 JSON Schema 和公共 testkit 测试读取。它覆盖：

- Protocol 2.2 的 14 个既有命令以及 2.3 的 15 个命令；
- 七种 message variant、`Result`/`Error` terminal；
- 非法帧、版本、payload、correlation、generation、ordering、late/duplicate/missing terminal；
- `quiesced`、`forced`、`timed-out`、`failed` 四种 Stop outcome。
- 版本化 reference-host lifecycle spec trace：create→admit→provision→ready→terminate→reap、
  failed→cleanup、stale generation、duplicate receipt/idempotency、pairing secret 不持久化，以及
  DPoP/revoke/fence 负向路径。

[`reference-host-lifecycle-v1.json`](../../../../packages/cloud-agent-protocol/fixtures/p0/reference-host-lifecycle-v1.json)
是 `GREENFIELD_SPEC_ORACLE`，不是生产实现、部署结果或真实 Managed Host 执行记录。

当前 characterization 明确保留三个开放缺口：

1. structural validator 接受 2.2，但当前 JSON Schema 要求 `minor >= 3`；
2. 当前 JSON Schema 接受 future minor，而 structural validator 拒绝 `minor > 3`；
3. capability-command 一致性和真实 Provider 生命周期不由 structural parser 证明，分别标记为
   `NOT_ENFORCED` 与 `NOT_RUN`。

## Fail-closed 审计

从仓库根目录运行：

```bash
node docs/plan/p0/scripts/audit-baseline-evidence.mjs
bun run --filter '@synara/cloud-agent-protocol' test
bun run --filter '@synara/cloud-agent-testkit' test
```

审计脚本逐项校验本地 Git object 中的 commit、tree、path、blob、内容 SHA-256、长度、fixture digest 和覆盖集。
任一固定输入缺失或漂移都会非零退出。它不会 fetch、写入其他仓库或执行真实 Provider。

## 证据边界

- 三份 manifest 均固定 `platformP0CharacterizationClosure.complete=true`、
  `m1BehaviorClosure.complete=false/status=NOT_RUN`、`aggregateGateDecision=NOT_CLAIMED`；
- 历史 source/test 只能证明已存在机制和负向 oracle，不能证明同一不可变 candidate 的真实行为；
- lifecycle trace 证明规范覆盖完整，但没有生产 Managed Host create→ready→terminate 执行；
- 没有真实 SendTurn/Workspace/checkpoint/reconnect 证据，这些属于 M1，不阻塞 Platform P0
  characterization closure；
- 这些文件不能单独把 aggregate `G-BASELINE` 改为 `VERIFIED`。
