# Synara extraction source P0 provenance 审计

## 结论

- 状态：**PASS**
- 固定 source：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0` / tree `ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- 全量 inventory：8625 rows，固定 SHA-256 bee237da890f4f3d62fd524fd11142a6b6c883e82790e5d455c415461ae7b4e5
- 决策表：8625 rows，运行时计算 SHA-256 24a7f918636b7f0baafaa6c99ff1c04c9b8ad10163cbb568fd311718bb9342ee
- Source license provenance：完整
- Secret provenance：完整

## Secret 扫描

- `fixed-source-tree-archive`：REVIEWED_WITH_RESTRICTIONS，finding=17
- `fixed-commit-reachable-git-history`：REVIEWED_WITH_RESTRICTIONS，finding=33
- `public-candidate-selected-tree`：REVIEWED_WITH_RESTRICTIONS，finding=6

raw Gitleaks report 已在临时目录删除；仓库内仅保留 rule、path、line、commit 与 fingerprint SHA-256。扫描使用 Gitleaks 默认规则，没有 repository config 或整目录 allowlist。

全部 56 条 finding 均以 exact fingerprint SHA-256 映射到独立 triage；没有整目录/整规则豁免。源历史禁止导入，静态测试私钥来源必须删除或改为运行时生成后才能公开。triage SHA-256：`2936cb8e883b296d13cd99f2ac4ba474c06e79dad4bce0cfdf9b4e76083b61b6`。

## License 与声明边界

固定 `LICENSE` blob `960499447d8ea8f6ce86017893f132f0c3885fef` 的 MIT 文本 SHA-256 为 `305724dd050ca7ded99c662de813d755bc4ec3887c4543a37159c6662ca36d1b`。该结论只证明固定 Synara extraction source 的 license provenance；没有证明第三方 dependency/license 已 cleared，也不授权 publication。

- `sourceLicenseProvenanceComplete=true`
- `secretProvenanceComplete=true`
- `sourceHistoryImportAuthorized=false`
- `selectedSourceDirectCopyAuthorized=false`
- `thirdPartyDependencyLicenseCleared=false`
- `publicationAuthorized=false`

## Blockers

- 无。

## Restrictions

- Never graft or publish the Synara source Git history.
- Rewrite or delete static test private-key bytes before any selected source is published.
- Re-run extracted-tree and artifact scans after P1 rewrites; exact decisions expire on their recorded dates.
