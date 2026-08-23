# Evidence index

本目录在恢复实施后保存 Gate closure record 的索引；大日志和二进制只链接到 immutable CI/artifact store，
不直接提交 secret、真实 pairing/auth material、数据库 dump 或未脱敏日志。

建议结构：

```text
evidence/
├── G-INVENTORY/
├── G-BASELINE/
├── G-CONTRACT/
├── G-DATA/
├── G-AUTHORITY/
├── G-MANAGED-AGENT/
├── G-WORKER-FENCING/
├── G-MANAGED-HOST/
├── G-ADAPTER/
├── G-SECURITY/
├── G-OPS/
├── G-STANDALONE/
├── G-SYNARA-CUTOVER/
├── G-T3-INTEGRATION/
├── G-SUPPLY-CHAIN/
├── G-PLATFORM-RELEASE/
└── G-EXPOSURE/
```

跨阶段 Gate 在对应目录下保存 phase record，例如 `G-AUTHORITY/P1/`、`G-SECURITY/P6/`；目录根部只保存
aggregate closure。phase record 被新 bits 失效后保留并标记 `INVALIDATED`，不得覆盖或删除历史证据。

每条 evidence 使用 [`../templates/gate-closure-record.md`](../templates/gate-closure-record.md)，并在
[`../06-status-tracker.md`](../06-status-tracker.md) 登记状态与链接。

当前 Platform P0 phase 由 `G-INVENTORY` R3 与 `G-BASELINE-P0` R4 关闭；aggregate `G-BASELINE` 仍等待
`G-BASELINE-M1`，所有 P1-P6 aggregate/phase Gate 仍保持 open、in progress 或 not started。不得用 P0、M1
Runtime 历史证据或本地候选替代后续 immutable closure。

## Current candidates

- [`CAG-G-BASELINE-P0-20260823-R4`](G-BASELINE/CAG-G-BASELINE-P0-20260823-R4.md)：current verified P0
  phase record；supersedes audit-semantics-invalid R3 while retaining the unchanged behavior evidence and all M1/
  aggregate boundaries。

- [`CAG-G-CONTRACT-P1-20260823-R1`](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R1.md)：historical candidate；其固定
  source 已由 R2 supersede。R1 保留 contract/SDK/descriptor/fixture/lock identity 与 focused replay 证据，但不能作为
  current-source review 或 Gate closure。
- [`CAG-G-CONTRACT-P1-20260823-R2`](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R2.md)：historical blocked
  candidate；独立 review 的 quantitative-claim/bytecode-residue findings 由 R3 修复，但 R2 本身不能关闭 Gate。
- [`CAG-G-CONTRACT-P1-20260823-R3`](G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R3.md)：historical repaired
  candidate；其固定 prerequisite 仍命名 Baseline R3，因此在生成显式 R4 rebind candidate 前不能关闭
  `G-CONTRACT`。
