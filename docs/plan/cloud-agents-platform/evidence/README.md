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

当前没有已关闭的 Platform Gate；不要用 M1 Runtime 历史证据创建虚假的 Platform closure record。
