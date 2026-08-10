# Platform P0 baseline characterization

- Status：IN PROGRESS
- Gate：`G-BASELINE`
- Real Provider execution：NOT RUN（M1 remains paused）
- Conclusion：existing evidence indexed；closure gaps remain

## Frozen reference profiles

| Profile                     | Ref                                                             | What it can prove                                         |
| --------------------------- | --------------------------------------------------------------- | --------------------------------------------------------- |
| Synara legacy managed-agent | `hxp0618/synara@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`       | Session/Turn/Execution/Worker/Workspace/Broker mechanisms |
| T3 embedded main            | `hxp0618/t3code@8101cd044911c7dc2a2adf7c7a9ba7962abf57b6`       | T3 Thread/Turn/Workspace/Git/Checkpoint authority         |
| T3 Cloud Agents consumer    | `feat/cloud-agent@9584a266e91fa94354e8c07f79af3a5e01755d16`     | thin bridge, trust/digest/drain/fail-close unit behavior  |
| Portable Runtime            | `hxp0618/cloud-agents@49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a` | Protocol 2.3 Describe and packed Runtime RC bits          |

Synara `main@ce8728c47c853a6420adca0ce0925bfed67a0d7c` 不含 managed cloud stack；其中
`managedAgentRunning/Observed` 只是本地 terminal subprocess 状态，不能作为公共 Control Plane baseline。

## Spec baseline

- Cloud Agent envelope schema、provider capability catalog、Distribution manifest；
- Provider Host Protocol 2.2/2.3 与 Runtime Event v2；
- T3 `docs/internals/environment-auth.md` pairing/session/proof/revoke model；
- 当前总架构的 embedded/managed-agent/managed-host 单一 authority；
- Synara legacy execution/worker/workspace/broker contracts；
- T3 Environment Lease 仅保留 greenfield spec/negative/reference-host baseline，不伪造 legacy happy path。

当前公共 fixture `fixtures/describe-command-v2.jsonl` 只有一条 Protocol 2.3 Describe command，不是完整 v2.2
golden corpus。

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

## Closure gaps

`G-BASELINE` remains open because all of the following are missing:

1. versioned Protocol 2.2 golden frame set；
2. same-input before/after characterization at frozen refs；
3. real Codex/Claude auth failure、429/unavailable、resume failure against the immutable candidate；
4. real SendTurn + file mutation + checkpoint/rollback + process/browser reconnect；
5. late terminal、sustained backpressure、secret/path containment and bounded soak；
6. Managed Host create→ready→terminate、generation revoke、multi-provider broker、pairing→DPoP direct/relay
   reference-host evidence。

另有状态漂移：ADR-0005 将部分 Runtime Gate 记为 closed，而总架构仍标 open。P0 不继承任何旧 closure；后续
必须以 canonical tracker 和新的 immutable record 重新裁决。

P1 不得以历史握手或 report 替代 `G-BASELINE` closure。
