# Platform P0 provenance, license, and secret baseline

- Status：BLOCKED
- Runtime audit：complete、fail closed
- Synara extraction audit：PASS WITH RESTRICTIONS
- Publication/attestation mutation：NOT AUTHORIZED

## Runtime fixed-input audit

机器记录：[`provenance/runtime-supply-chain-audit.json`](provenance/runtime-supply-chain-audit.json)；中文摘要：
[`provenance/runtime-supply-chain-audit.zh-CN.md`](provenance/runtime-supply-chain-audit.zh-CN.md)。

已验证：

- source `49e8cdc6...`、tree `952996e...`、annotated tag object `ac64d6f2...`；
- `SOURCE_PROVENANCE.md` 11/11 declared Git objects；
- 七个 tgz 与 standalone 的 candidate/checksum/same-bits；
- Node 24.13.1、Bun 1.3.14 与 scanner binary/archive digest；
- 107 个外部生产依赖节点、175 条边；SPDX 114 packages、194 relationships；
- Gitleaks 对 fixed source tree、解包制品、local Git refs 零发现；raw report 未入库；
- Syft 识别 7/7 公共包；OSV 2.5.0 snapshot 为零已知漏洞。

Runtime 结论仍为 **BLOCKED**，且明确 `releaseAuthorization=false`：

1. Anthropic SDK/native package 的外部 Legal Agreements/All rights reserved 没有记录重分发授权；
2. 8 项 license text 缺失、10 项 package license policy blocked；
3. standalone 没有保留 bundler metafile；
4. fixed rc.1 workflow 未完整 SHA pin/OIDC attest + immediate verify；
5. source commit/tag unsigned；
6. legacy provenance 只覆盖七个 tarball，遗漏 standalone/metadata，`resolvedDependencies=[]`；
7. 8/8 GitHub artifact attestation 不存在或不可验证。

这些是旧 immutable rc.1 的历史事实；当前 P0 分支的 workflow hardening 不会反向改变旧 candidate。

## Synara extraction source

全量 inventory 已扩为 frozen ref `2c50b1eb...` 的 8,625 个 tracked blob，并为 public candidate 记录同一
MIT source license object：`LICENSE` blob `960499...`、内容 SHA-256 `305724...`。这只证明 source
license provenance，不替代第三方 dependency/license owner 的批准。

机器记录：[`provenance/synara-extraction-source-audit.json`](provenance/synara-extraction-source-audit.json)；
中文摘要：[`provenance/synara-extraction-source-audit.zh-CN.md`](provenance/synara-extraction-source-audit.zh-CN.md)。

固定 Gitleaks 8.30.1 对 fixed source tree、reachable history 与 1,129-row selected extraction source 实际扫描，
得到 `17 + 33 + 6 = 56` 条 finding。独立只读复核按 canonical path/rule/line/commit identity 逐条裁决：

- 48 条为 exact context false positive；
- 6 条属于同一静态测试私钥来源，必须删除或改为运行时生成，禁止直接复制到公共提交；
- 2 条位于历史日志；即使当前命中上下文是枚举，也禁止把 Synara Git history graft 到公共仓；
- possible real secret 与 cannot-determine 均为 0；没有整目录、整规则或全测试文件 allowlist；
- triage SHA-256 `2936cb8e...`，finding set SHA-256 `5f74f2af...`；决策有 owner/expiry，任何漂移或到期均
  fail closed。

因此 source/secret provenance 已完整，但这是 inventory closure，不是直接复制或公开授权：
`sourceHistoryImportAuthorized=false`、`selectedSourceDirectCopyAuthorized=false`、
`publicationAuthorized=false`。P1 只能创建新的公共历史，并在重写后重新扫描 extracted tree 与 artifact。

## Claim boundary

当前证据可支持“Runtime 供应链清单已完整审计并如实发现 blocker”，以及“Synara extraction 的 source/license/
secret provenance 已完成并带强制 rewrite/history quarantine 限制”。它不能支持：third-party license cleared、
trusted provenance/attestation、直接复制、已发布、已部署、Platform RC、Beta 或 GA。`G-SUPPLY-CHAIN`、
Runtime release 与 Platform exposure 均保持未关闭。
