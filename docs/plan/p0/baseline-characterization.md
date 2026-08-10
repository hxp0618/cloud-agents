# Platform P0 baseline characterization

- Status：VERIFIED
- Phase Gate：`G-BASELINE-P0`
- Aggregate Gate：`G-BASELINE` OPEN（仍缺 `G-BASELINE-M1`）
- Real Provider execution：NOT RUN（M1 remains paused）
- Conclusion：`CAG-G-BASELINE-P0-20260810-R2` closes the P0 phase only

## Frozen reference profiles

| Profile                     | Ref                                                             | What it can prove                                         |
| --------------------------- | --------------------------------------------------------------- | --------------------------------------------------------- |
| Synara legacy managed-agent | `hxp0618/synara@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`       | Session/Turn/Execution/Worker/Workspace/Broker mechanisms |
| T3 embedded main            | `hxp0618/t3code@8101cd044911c7dc2a2adf7c7a9ba7962abf57b6`       | T3 Thread/Turn/Workspace/Git/Checkpoint authority         |
| T3 Cloud Agents consumer    | `feat/cloud-agent@9584a266e91fa94354e8c07f79af3a5e01755d16`     | thin bridge, trust/digest/drain/fail-close unit behavior  |
| Portable Runtime RC         | `hxp0618/cloud-agents@49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a` | immutable rc.1 source boundary                            |
| P0 Runtime corpus           | `hxp0618/cloud-agents@c2c03584656a0db04de2f6b84113ac932459eae6` | Git-bound golden corpus and current baseline audit        |

Synara `main@ce8728c47c853a6420adca0ce0925bfed67a0d7c` 不含 managed cloud stack；其中
`managedAgentRunning/Observed` 只是本地 terminal subprocess 状态，不能作为公共 Control Plane baseline。

## Spec baseline

- Cloud Agent envelope schema、provider capability catalog、Distribution manifest；
- Provider Host Protocol 2.2/2.3 与 Runtime Event v2；
- T3 `docs/internals/environment-auth.md` pairing/session/proof/revoke model；
- 当前总架构的 embedded/managed-agent/managed-host 单一 authority；
- Synara legacy execution/worker/workspace/broker contracts；
- T3 Environment Lease 仅保留 greenfield spec/negative/reference-host baseline，不伪造 legacy happy path。

`p0-protocol-golden-v3` 已覆盖 Protocol 2.2 的 14 个命令、2.3 的 15 个命令、七个 message variant、
Result/Error terminal、四 correlation 字段负例与 Stop 四 outcome；fixture 固定到 Git commit
`0a984d02...` 的 tree/blob map。Runtime stdio 测试通过真实 decoder 消费全部 29 个 command fixture。

## Negative baseline already present

### Runtime/public repo

- Runtime stdio parser/correlation/terminal/Stop tests；
- stdio client fail-close、environment replacement、listener behavior；
- provider protocol/descriptor validation；
- packed-bin Describe conformance。

### Synara legacy source

- allocation/fencing：`executiontargets/kubernetes_allocation_backend_test.go`、
  `executions/worker_claim_concurrency_test.go`、`leadership/write_fence_test.go`、
  `database/kubernetes_pod_deletion_fence_sqlite_test.go`；
- workspace：`agentd/workspace_test.go`、`workspace_restore_generation_test.go`；
- credential containment：`agentd/provider_credential_broker_test.go`；
- release identity：candidate lock tests、worker image manifest tests、agentd manifest tests。

### T3

- Environment auth/pairing grant single-use、race、TTL、thumbprint、revoke；
- DPoP proof replay/consume；
- relay managed endpoint allocation CAS/superseded deprovision；
- Cloud Agents adapter digest/trust、ACK/drain、approval/user-input/interrupt、resume、bounded Stop、rollback
  before workspace mutation。

## Reference-host evidence

- Synara checked-in Stage 3/4 product/failure reports provide historical Codex/Claude, allocation and isolation case
  oracles；原始 `/tmp` outputs 已不存在，不能当可重放 closure artifact；
- T3 fresh cross-repo integration only proves
  `Describe → ready → StartSession → StopSession`；没有 SendTurn、Workspace edit、checkpoint/rollback 或 restart；
- cloud-agents packed conformance currently proves Describe/descriptor/terminal transcript subset；没有完整
  Workspace/security/dual-host suite。

## Executed characterization

执行采用 D-024 的 **local-first / cloud-final** 口径：本机先完成 fixture、tests 与审计脚本收敛；输入 SHA
冻结后，才在 `root@103.217.189.80` 做一次 Linux/amd64 终验。远端网络重试、镜像尝试与 tarball 预热均
保留在 command history，但只有最终 exit-zero 的 frozen install/test/typecheck 决定 PASS。

| Profile         | Fixed source               | Linux result                                                                                                         | Machine record                                                                      |
| --------------- | -------------------------- | -------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Synara legacy   | `2c50b1e` / tree `ba41fc1` | 138 focused PASS；934 broad PASS 后遇 1 个前置失败；4 个并发用例均在断言前遇已记录 fixture/generated-source 前置失败 | [`synara-linux-amd64-execution.json`](baseline/synara-linux-amd64-execution.json)   |
| T3 main         | `8101cd0` / tree `e98f565` | frozen install；7 files / 30 tests；5 targeted typechecks PASS                                                       | [`t3-linux-amd64-execution.json`](baseline/t3-linux-amd64-execution.json)           |
| T3 Cloud bridge | `9584a26` / tree `171624a` | frozen install；5 files / 39 tests；server typecheck PASS                                                            | 同上                                                                                |
| Runtime corpus  | `c2c0358` / tree `2b3e3db` | Protocol 13、testkit 5、Runtime 33、三包 typecheck、audit/fmt/lint PASS                                              | [`runtime-linux-amd64-execution.json`](baseline/runtime-linux-amd64-execution.json) |

Synara 的 `FAIL_KNOWN_PRECONDITION` 是固定源码的实际 characterization：provider capability catalog source
缺失，以及 tenant fixture 未 seed `saas_plans(plan_code=test)`。这些结果没有进入目标并发语义断言，因此
既不能记成 PASS，也不会被补丁修改后伪装成重构前 baseline。P1 必须把它们作为已知输入缺口继承，并在
新公共实现的同一 criterion 上提供独立正向/负向 evidence。

## Closure boundary

`G-BASELINE-P0` 已由独立 reviewer 在固定 evidence commit `66e2f127...` 上签署 R2：

1. 三份 normalized machine record 与 Synara 21-entry evidence index 已 Git-bound；
2. 本地 audit v3 与远端 index verification 均通过；
3. greenfield Managed Host 在 P0 只要求 Git-bound spec/negative/reference-host oracle，生产实现与真实
   create→ready→terminate 属 P3，不得反向伪装成 P0 缺陷或 P0 已实现。

以下真实行为属于 `G-BASELINE-M1`，不会反向阻塞 P0，但 aggregate `G-BASELINE` 在它们完成前保持 open：

- Codex/Claude auth failure、429/unavailable、resume failure；
- real SendTurn + file mutation + checkpoint/rollback + process/browser reconnect；
- late terminal、sustained backpressure、secret/path containment 与 bounded soak。

另有状态漂移：ADR-0005 将部分 Runtime Gate 记为 closed，而总架构仍标 open。P0 不继承任何旧 closure；后续
必须以 canonical tracker 和新的 immutable record 重新裁决。

P1 不得以历史握手或 report 替代 `G-BASELINE-P0` closure；M1 不得用 P0 phase record替代 aggregate
`G-BASELINE`。
