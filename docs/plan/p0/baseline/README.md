# P0 Protocol 与 baseline manifest

本目录把两个不同的 closure 明确拆开：

- `platformP0CharacterizationClosure=true/status=COMPLETE`：固定 ref 上的 Synara legacy、T3 embedded 机制与
  本地 greenfield spec/negative/reference-host fixture 已完成 Platform P0 characterization。这个字段只关闭
  `G-BASELINE-P0` 的输入范围；criterion 中保留的 `NOT_RUN`、`SPEC_ONLY` 与 retention boundary 不会被提升，
  也不决定 aggregate Gate；
- `m1BehaviorClosure=false`：真实 Provider、SendTurn、Workspace 修改、checkpoint/rollback、重连和 same-bits
  行为仍为 `NOT_RUN`，由 M1 单独关闭。

本目录不直接声明 aggregate `G-BASELINE` 为 `VERIFIED`。三份 manifest 的 criterion mapping 合并后仍有
未执行或未绑定项，最终 Gate 状态由 canonical tracker/closure record 的主流程裁决。

## 固定清单

- [`synara-legacy.json`](synara-legacy.json)：Synara managed-agent 的固定 commit/tree/blob/SHA-256、能力与缺口；
- [`t3-embedded.json`](t3-embedded.json)：T3 embedded authority 与 Cloud Agent consumer 的两个固定 ref；
- [`reference-host-negative.json`](reference-host-negative.json)：公共 Runtime/reference-host 负向测试来源及版本化 Protocol corpus digest。

Protocol corpus 位于
[`packages/cloud-agent-protocol/fixtures/p0`](../../../../packages/cloud-agent-protocol/fixtures/p0)，由真实公共
structural validator、当前 JSON Schema 和公共 testkit 测试读取。它覆盖：

- Protocol 2.2 的 14 个既有命令以及 2.3 的 15 个命令；
- 七种 message variant、`Result`/`Error` terminal；
- 非法帧、版本、payload、四个 correlation 字段、generation、ordering、late/duplicate/missing terminal；
- `quiesced`、`forced`、`timed-out`、`failed` 四种 Stop outcome，以及非法 outcome/矛盾布尔负例；
- 版本化 reference-host lifecycle spec trace：create→admit→provision→ready→terminate→reap、
  failed→cleanup、restart/replay、partial allocation/orphan reconciliation、workload/volume/endpoint/grant
  独立生命周期、stale generation、duplicate receipt/idempotency、pairing secret 不持久化，以及
  DPoP/revoke/fence 负向路径。

[`reference-host-lifecycle-v1.json`](../../../../packages/cloud-agent-protocol/fixtures/p0/reference-host-lifecycle-v1.json)
是 `GREENFIELD_SPEC_ORACLE`，不是生产实现、部署结果或真实 Managed Host 执行记录。

当前 characterization 明确保留三个开放缺口：

1. structural validator 接受 2.2，但当前 JSON Schema 要求 `minor >= 3`；
2. 当前 JSON Schema 接受 future minor，而 structural validator 拒绝 `minor > 3`；
3. capability-command 一致性和真实 Provider 生命周期不由 structural parser 证明，分别标记为
   `NOT_ENFORCED` 与 `NOT_RUN`。

## Fail-closed 审计

从仓库根目录、使用固定 Node 24.13.1 运行：

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-baseline-evidence.mjs
bun run --filter '@synara/cloud-agent-protocol' test
bun run --filter '@synara/cloud-agent-testkit' test
```

审计脚本逐项校验已固定 source 输入的 Git commit/tree/path/blob/内容 SHA-256/长度，以及本地 fixture digest、
覆盖集和 criterion mapping。
任一固定输入缺失或漂移都会非零退出。它不会 fetch、写入其他仓库或执行真实 Provider。

Phase A fixture 已固定到 `0a984d02bda3c64f7dca1e9da7bed60e4d3f02f6`，
`fixtureCorpus.sourceBinding.status=BOUND` 同时记录 tree 与十个 blob；审计从 Git object 重读并与 workspace
内容 SHA-256 对照。该绑定只证明 fixture immutable，不把未执行的 Synara/T3/真实 Provider criterion 升级为
PASS。

## 证据边界

- 三份 manifest 均固定 `platformP0CharacterizationClosure.complete=true/status=COMPLETE`、
  `platformP0CharacterizationClosure.doesNotDecideAggregateGate=true`、
  `m1BehaviorClosure.complete=false/status=NOT_RUN`、`aggregateGateDecision=NOT_CLAIMED`；
- 历史 source/test 只能证明已存在机制和负向 oracle，不能证明同一不可变 candidate 的真实行为；
- lifecycle trace 证明规范覆盖完整，但没有生产 Managed Host create→ready→terminate 执行；
- 没有真实 SendTurn/Workspace/checkpoint/reconnect 证据，这些属于 M1，不阻塞 Platform P0
  characterization closure；
- 这些文件不能单独把 aggregate `G-BASELINE` 改为 `VERIFIED`。
