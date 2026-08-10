# Platform P0 execution

- Status：IN PROGRESS
- Scope：freeze、inventory、provenance、baseline characterization、evidence
- Implementation/Release/Deployment：PAUSED
- Canonical plan：[`../README.md`](../README.md)

## Outputs

| Artifact                                                                                   | Purpose                                                     | Current state                                           |
| ------------------------------------------------------------------------------------------ | ----------------------------------------------------------- | ------------------------------------------------------- |
| [`frozen-baseline.json`](frozen-baseline.json)                                             | 三仓 refs/tree/worktree/dirty/toolchain 与 live remote 查询 | generated；remote 可能因网络标 unavailable              |
| [`synara-file-inventory.tsv`](synara-file-inventory.tsv)                                   | frozen Synara 全 tracked tree metadata/seed classification  | generated；8,625 rows，含完整 root build context        |
| [`synara-inventory-decisions.tsv`](synara-inventory-decisions.tsv)                         | owner/target/authority/license/secret/review final decision | generated；8,625 rows，exact secret triage bound        |
| [`synara-inventory-graph.json`](synara-inventory-graph.json)                               | binary/image/deploy/external artifact nodes + edges         | generated；25 nodes/26 edges，no dangling/orphans       |
| [`synara-inventory-summary.md`](synara-inventory-summary.md)                               | 数量、能力映射、混合包与生成链风险                          | R1 generated；`G-INVENTORY` open                        |
| [`baseline-characterization.md`](baseline-characterization.md)                             | spec/negative/reference-host 已有与缺失证据                 | corpus Git-bound；`G-BASELINE-P0` open                  |
| [`provenance/runtime-supply-chain-audit.json`](provenance/runtime-supply-chain-audit.json) | immutable rc.1 Runtime dependency/license/secret audit      | complete audit；7 blockers，release unauthorized        |
| [`provenance-summary.md`](provenance-summary.md)                                           | license/secret/dependency/build input provenance            | extraction PASS with restrictions；release unauthorized |

Gate records：

- [`G-INVENTORY` P0 R1](../cloud-agents-platform/evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R1.md)；
- [`G-BASELINE` P0 R1](../cloud-agents-platform/evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R1.md)。

## Frozen-source rule

P0 extraction source 固定为 clean
`hxp0618/synara:codex/cloud-agent-external-runtime@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`。
Synara 主 worktree、Stage 8、cloud-agents Codex fix 的脏改动只登记不吸收。remote 未实时验证时必须标
`unavailable`，不能拿本地 remote-tracking ref 冒充 live remote。

## Reproduce

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/generate-frozen-baseline.mjs

/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/generate-synara-file-inventory.mjs

/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/finalize-synara-inventory.mjs

/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/finalize-synara-secret-triage.mjs

P0_GITLEAKS_BIN=/absolute/pinned/gitleaks \
  /Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-synara-extraction-source.mjs

/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-baseline-evidence.mjs
```

脚本只执行 Git/toolchain 只读命令并写入本目录；不会 fetch、checkout、commit、push、运行 Provider、连接
数据库或部署资源。
