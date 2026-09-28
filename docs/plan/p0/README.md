# P0 审计材料索引

本目录保存 P0 冻结、清单、基线、溯源和脚本输出，是历史/技术证据入口。目录中的 VERIFIED、PAUSED、NOT RUN 或其他阶段状态描述当时的 P0 证据边界，不代表当前 Cloud Agents 状态；当前状态只认 [06](../cloud-agents-platform/06-status-tracker.md)。

## Canonical material

- [Inventory summary](synara-inventory-summary.md)：文件清单、能力映射和生成链风险摘要。
- [Inventory decisions](inventory-decision-summary.md)：owner、target、authority、license、secret 和 review 分类。
- [Provenance summary](provenance-summary.md)：来源、依赖、许可、构建输入和审计边界。
- [Baseline characterization](baseline-characterization.md)：固定来源的基线观察与已知前置条件。
- [Baseline index](baseline/README.md)、[governance index](governance/README.md)：按主题访问机器输入与证据。
- [P0 Gate evidence](../cloud-agents-platform/evidence/README.md)：Gate closure 和独立复核的 canonical 链接。

## 边界

- JSON、TSV、脚本和运行输出是机器输入或原始证据，保留原文，不复制到活动入口。
- 不从本目录选择当前执行顺序；顺序见 [04](../cloud-agents-platform/04-extraction-and-migration.md)，最终结果见 [06](../cloud-agents-platform/06-status-tracker.md)。
- 本索引不记录具体运行编号、命令、digest 或临时路径。
