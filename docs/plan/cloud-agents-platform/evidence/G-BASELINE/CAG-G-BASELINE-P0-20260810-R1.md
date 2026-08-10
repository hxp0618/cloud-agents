# Gate record: `G-BASELINE` / P0 / R1

- Evidence ID：`CAG-G-BASELINE-P0-20260810-R1`
- Record type：PHASE
- Status：IN PROGRESS
- DRI：hxp0618（owner）；Codex P0 executor
- Independent reviewer：P0 baseline explorer
- Date：2026-08-10 Asia/Shanghai

## Fixed inputs

- Synara legacy managed-agent：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`
- T3 embedded main：`8101cd044911c7dc2a2adf7c7a9ba7962abf57b6`
- T3 Cloud Agents consumer：`9584a266e91fa94354e8c07f79af3a5e01755d16`
- Portable Runtime：`49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a`
- Characterization index：[`baseline-characterization.md`](../../../p0/baseline-characterization.md)
- Real Provider execution in this record：NOT RUN

## Exit criteria mapping

| Criterion                                                           | Result  | Evidence                                                               |
| ------------------------------------------------------------------- | ------- | ---------------------------------------------------------------------- |
| legacy Synara managed-agent mechanisms indexed                      | PASS    | execution/fencing/workspace/broker/release test paths indexed          |
| T3 embedded authority indexed                                       | PASS    | main + fresh bridge paths indexed                                      |
| Managed Host greenfield spec/negative/reference-host baseline       | PARTIAL | spec and negative paths exist; no create→ready→terminate reference run |
| complete Protocol 2.2 golden corpus                                 | FAIL    | only one Protocol 2.3 Describe fixture observed                        |
| same-input before/after characterization                            | NOT RUN | no versioned closure record                                            |
| real Codex/Claude failure/resume characterization on immutable bits | NOT RUN | M1 remains paused                                                      |
| real T3 SendTurn/workspace/checkpoint/restart                       | NOT RUN | current cross-repo test stops after Start/Stop                         |
| raw evidence retained and immutable                                 | FAIL    | historical `/tmp` outputs removed/ignored                              |

## Existing evidence boundary

- Synara mechanisms can characterize allocation/fencing/workspace/broker/release, but its Session/Turn/Execution FK
  model is not a reusable Environment Lease model.
- T3 pairing/DPoP/session revoke primitives are the authentication reference; they do not prove a managed sandbox.
- T3 fresh handshake proves only `Describe → ready → StartSession → StopSession`.
- Historical Stage 3/4 reports are case oracles, not reproducible same-bits closure evidence.

## Closure decision

`G-BASELINE` remains `IN PROGRESS`. P1 is blocked until the missing golden frames, same-input comparison, real
Provider failure/resume, real T3 workspace/restart, and Managed Host reference-host evidence are produced in separately
authorized execution windows.
