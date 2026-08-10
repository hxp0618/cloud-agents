# Cloud Agents Runtime P0 供应链审计

- 总状态：**BLOCKED**
- 固定 source：`49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a`
- 固定 tag：`cloud-agent-m1-rc.1`（tag object `ac64d6f2fd29f3a1b9d2e514efe2c72eb6118d62`）
- 固定 candidate：`sha256:b9931233d46aeaf1392197095483c2e3409f628a47b2ba92c8e57bb38b444676`
- 权限边界：只读审计；未发布、未生成可信 attestation、未修改 Release 或远端

## 已验证

- source tree：`952996e6847f67c8ad9f5986f960c645d0b9c8f8`；SOURCE_PROVENANCE 对象：11/11。
- 本地不可变 bits：七个 tgz + standalone 全部匹配 manifest/checksums。
- 生产依赖闭包：107 个外部节点、175 条边；七个公共包另计。
- SPDX 2.3：114 个 package、194 条 relationship。
- Gitleaks：PASS；固定 source、解包制品、local `--all` history 均不使用目录 allowlist；原始报告未入库。
- OSV snapshot：PASS；已知漏洞数 0。
- 固定 source workflow action pinning：BLOCKED。

## 阻塞项（fail closed）

- git-signatures: Source commit and/or RC tag has no trusted verifiable signature.
- github-attestation-verification: 8/8 local artifact attestations are absent or unverifiable.
- provenance-coverage: Existing local unsigned provenance covers only seven tarballs, has resolvedDependencies=[], and omits standalone/metadata evidence.
- standalone-bundle-composition: The RC has no retained bundler metafile, so the exact file-level standalone bundle closure cannot be proven.
- third-party-license: Anthropic Claude Agent SDK uses external Legal Agreements/All rights reserved and no redistribution authorization is recorded; non-host native package license texts are not materialized.
- workflow-action-pinning: One or more GitHub Actions are not pinned to a full commit SHA.
- workflow-trusted-attestation: Fixed source workflow lacks the complete OIDC attest plus immediate verification chain.

## 结论边界

本记录可以支持“固定 source 与本地八个 Runtime bits 的哈希、生产依赖图和 secret scan 已复核”。它不能支持 license cleared、可信 provenance/attestation、已发布、已部署、Platform RC、Beta 或 GA。

生成文件：

- `runtime-supply-chain-audit.json`：机器可读总报告；
- `secret-scan-sanitized.json`：仅规则/路径/行号/commit/fingerprint hash；
- `license-inventory.json` 与 `THIRD_PARTY_NOTICES.md`：许可证清单、文本哈希和 fail-closed 项；
- `sbom-production.spdx.json`：生产闭包 SPDX 2.3；
- `toolchain-lock.json`：固定工具版本、下载源和 binary/archive SHA-256；
- `generated-evidence.sha256`：上述输出与生成脚本的 SHA-256。
