# P1 审计材料索引

本目录保存 P1 契约、数据内核、恢复、身份、供给链、实现和 independent review 记录，是历史/技术证据入口。详细文件中的 `IN PROGRESS`、`OPEN`、`BLOCKED`、候选和阶段结果保留为历史事实，不代表当前 Cloud Agents 状态；当前状态只认 [06](../cloud-agents-platform/06-status-tracker.md)。

## 规范来源

P1 决策集中在 [ADR 索引](../adr/README.md)，尤其是 ADR-0007–ADR-0030。契约原文见 [references/contracts](../references/README.md)。

## Canonical evidence

- [G-CONTRACT R4](../cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4.md) 与 [independent review](../cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4-independent-review.md)。
- [G-DATA R1](../cloud-agents-platform/evidence/G-DATA/CAG-G-DATA-P1-20260823-R1.md) 与 [independent review](../cloud-agents-platform/evidence/G-DATA/CAG-G-DATA-P1-20260823-R1-independent-review.md)。
- [G-AUTHORITY P1 R1](../cloud-agents-platform/evidence/G-AUTHORITY/P1/CAG-G-AUTHORITY-P1-20260823-R1.md) 与 [independent review](../cloud-agents-platform/evidence/G-AUTHORITY/P1/CAG-G-AUTHORITY-P1-20260823-R1-independent-review.md)。
- [G-SECURITY P1 R1](../cloud-agents-platform/evidence/G-SECURITY/P1/CAG-G-SECURITY-P1-20260823-R1.md) 与 [independent review](../cloud-agents-platform/evidence/G-SECURITY/P1/CAG-G-SECURITY-P1-20260823-R1-independent-review.md)。
- [Aggregate Gate gap audit](p1-aggregate-gate-gap-audit-20260822.md) 与 [independent review](p1-aggregate-gate-gap-audit-independent-review-20260822.md)。
- [Runtime closure authority](g-contract-runtime-closure-profile-v4-authority-20260828.md) 与 [independent review](g-contract-runtime-closure-profile-v4-independent-review-20260828.md)。

## 文件边界

- 本目录的实现、复核、日志和机器输入按需访问；不把逐轮命令、运行编号、digest 或修复过程复制到活动文档。
- independent review 只作为审计依据，不自动关闭 Gate；当前汇总和 Gate 状态回到 [06](../cloud-agents-platform/06-status-tracker.md)。
- 当前实施顺序见 [04](../cloud-agents-platform/04-extraction-and-migration.md)，验收规则见 [05](../cloud-agents-platform/05-gates-and-acceptance.md)。
