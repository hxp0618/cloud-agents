# Evidence 索引

本目录保存 Gate closure、阶段报告、应用 E2E 和独立复核的原始审计材料。它是证据来源，不是当前计划或状态入口；当前 Cloud Agents 实际状态只认 [`06-status-tracker.md`](../06-status-tracker.md)。

## Gate 分类

```text
evidence/
├── G-INVENTORY/       ├── G-BASELINE/
├── G-CONTRACT/        ├── G-DATA/
├── G-AUTHORITY/       ├── G-MANAGED-AGENT/
├── G-WORKER-FENCING/  ├── G-MANAGED-HOST/
├── G-ADAPTER/         ├── G-SECURITY/
├── G-OPS/             ├── G-STANDALONE/
├── G-SYNARA-CUTOVER/  ├── G-T3-INTEGRATION/
├── G-SUPPLY-CHAIN/    ├── G-PLATFORM-RELEASE/
└── G-EXPOSURE/
```

跨阶段 Gate 在对应目录下继续保存 phase record；phase record 即使被后续证据替代也保持原文并标记其当时状态。根目录只承担分类和链接，不复制 closure record。

## 当前 canonical links

- [最终状态汇总](../06-status-tracker.md)：r741 历史 candidate 的 12/12 Provider×Environment、适用故障结果、清理/安全汇总和 `CLOSED / APPROVED` Gate 状态；当前 dirty worktree 见下方审查摘要。
- [开源候选审查摘要](open-source-candidate-audit-20260929.md)：区分 r741 历史证据与当前 dirty worktree，并记录公开边界和合并前最小验证。
- [Gate closure 模板](../templates/gate-closure-record.md)：新 closure record 的字段约束。
- [专项文档导航](../README.md)：04、05、06、07 的职责边界。
- [P0 审计索引](../../p0/README.md)、[P1 审计索引](../../p1/README.md)、[standalone 审计索引](../../standalone/README.md)：跨目录的证据定位入口。

## 记录规则

- 应用 E2E 报告保留 commit、资源标识和实际 phase transition；它们不能单独关闭正式 Gate。
- 正式 closure record 按模板记录输入范围、结果、未覆盖项、审批和 canonical artifact 链接；不把原始日志、Secret、数据库 dump 或真实 pairing/auth material 放入本目录。
- evidence 中的 candidate、`OPEN`、`BLOCKED` 或历史失败记录保留为审计事实，不替代 06，也不提供默认下一步或新的授权。历史 candidate 的 PASS 不得外推到未复验的 dirty worktree。
- 新阶段先记录计划，再记录实际结果和证据索引；文档整理本身不产生新的 runtime 验收结论。
