# Platform P0 execution

- Status：VERIFIED
- Scope：freeze、inventory、provenance、baseline characterization、evidence
- Implementation/Release/Deployment：PAUSED
- Canonical plan：[`../README.md`](../README.md)

## Outputs

| Artifact                                                                                     | Purpose                                                     | Current state                                           |
| -------------------------------------------------------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------------- |
| [`frozen-baseline.json`](frozen-baseline.json)                                               | 三仓 refs/tree/worktree/dirty/toolchain 初始快照            | generated；live refs 由后续 verification 补齐           |
| [`synara-file-inventory.tsv`](synara-file-inventory.tsv)                                     | frozen Synara 全 tracked tree metadata/seed classification  | generated；8,625 rows，含完整 root build context        |
| [`synara-inventory-decisions.tsv`](synara-inventory-decisions.tsv)                           | owner/target/authority/license/secret/review final decision | generated；8,625 rows，exact secret triage bound        |
| [`synara-inventory-graph.json`](synara-inventory-graph.json)                                 | binary/image/deploy/external artifact nodes + edges         | generated；25 nodes/26 edges，no dangling/orphans       |
| [`synara-inventory-summary.md`](synara-inventory-summary.md)                                 | 数量、能力映射、混合包与生成链风险                          | R3 reviewed；`G-INVENTORY` verified                     |
| [`baseline-characterization.md`](baseline-characterization.md)                               | spec/negative/reference-host 与 fixed-source execution      | `G-BASELINE-P0` R4 verified；same behavior evidence     |
| [`baseline/synara-linux-amd64-execution.json`](baseline/synara-linux-amd64-execution.json)   | Synara fixed-source Linux characterization                  | executed；known precondition failures retained          |
| [`baseline/t3-linux-amd64-execution.json`](baseline/t3-linux-amd64-execution.json)           | T3 main + feature Linux characterization                    | install/tests/typecheck PASS；real Provider NOT RUN     |
| [`baseline/runtime-linux-amd64-execution.json`](baseline/runtime-linux-amd64-execution.json) | current Runtime/golden corpus Linux characterization        | focused tests/typecheck/audit/fmt/lint PASS             |
| [`provenance/runtime-supply-chain-audit.json`](provenance/runtime-supply-chain-audit.json)   | immutable rc.1 Runtime dependency/license/secret audit      | complete audit；7 blockers，release unauthorized        |
| [`provenance-summary.md`](provenance-summary.md)                                             | license/secret/dependency/build input provenance            | extraction PASS with restrictions；release unauthorized |

Gate records：

- [`G-INVENTORY` P0 R3](../cloud-agents-platform/evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R3.md)（VERIFIED；supersedes R2）；
- [`G-BASELINE-P0` R4](../cloud-agents-platform/evidence/G-BASELINE/CAG-G-BASELINE-P0-20260823-R4.md)（VERIFIED；supersedes R3）；
- [`G-BASELINE-P0` R3](../cloud-agents-platform/evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R3.md)（INVALIDATED；historical only）；
- [`G-INVENTORY` P0 R2](../cloud-agents-platform/evidence/G-INVENTORY/CAG-G-INVENTORY-P0-20260810-R2.md)（INVALIDATED；historical only）；
- [`G-BASELINE-P0` R2](../cloud-agents-platform/evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R2.md)（INVALIDATED；historical only）；
- [`G-BASELINE` P0 R1](../cloud-agents-platform/evidence/G-BASELINE/CAG-G-BASELINE-P0-20260810-R1.md)（INVALIDATED；historical only）。

## Frozen-source rule

P0 extraction source 固定为 clean
`hxp0618/synara:codex/cloud-agent-external-runtime@2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0`。
Synara 主 worktree、Stage 8、cloud-agents Codex fix 的脏改动只登记不吸收。remote 未实时验证时必须标
`unavailable`，不能拿本地 remote-tracking ref 冒充 live remote。

## Execution environment policy

采用 **local-first / cloud-final**：实现、focused tests、typecheck、format、lint 和证据脚本先在本机完成；只有
当一个 phase 的输入 SHA 已冻结并接近收口时，才在 `root@103.217.189.80` 做一次 Linux/amd64 重放。远端
重试、镜像切换或预热 tarball 必须作为环境过程单独记录，不能替代产品测试结果，也不得触发对已通过本地
Gate 的无差别重复执行。

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
