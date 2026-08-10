# Platform P0 provenance, license, and secret baseline

- Status：BLOCKED
- Scope：source/RC/consumer evidence only
- Publication/attestation mutation：NOT AUTHORIZED
- Conclusion：source lineage and artifact hashes are largely reproducible；license/provenance/secret Gate not closed

## Positive evidence

- `SOURCE_PROVENANCE.md` 的 7 个包、3 个 release helper 与 root config 共 11 个 Git object，和 Synara
  `f9fb3d695` 逐对象 11/11 一致；后续 protocol object 也与 `b86d30b1` 一致；
- source 与七个 tgz 内 MIT LICENSE 内容一致；
- `cloud-agent-m1-rc.1` annotated tag 指向 `49e8cdc6...`；本地七 tgz + standalone SHA-256 与 candidate
  lock 一致；六个已安装生产包 SHA-512 与 `bun.lock` 一致；
- Synara external runtime offline candidate verifier 当前通过；
- `bun run secret:scan` 已通过；补充文件名/计数扫描未发现 PEM、AWS、GitHub、Slack 或 Google token；
  bundle 中 generic `missing access_token` 是错误文本，不是 credential。

这些正项只证明来源/哈希链的一部分，不等于 trusted attestation、完整 license closure 或全历史 secret
closure。

## Blocking findings

### SBOM/license

- 当前 SBOM 只列 8 个自有 manifest、0 relationships，未覆盖 peer/optional/transitive；
- standalone 实际 bundle `@anthropic-ai/claude-agent-sdk@0.3.207`，但没有可审计 THIRD_PARTY_NOTICES/
  license text hash；
- 本机缺 Syft/license scanner，无法关闭 unknown/NOASSERTION/blocked license policy。

### Provenance/signature

- `provenance.json` 是未签名 local builder 声明，`resolvedDependencies=[]`；
- subjects 只覆盖七 tgz，不含 standalone、SBOM、checksums、candidate manifest；
- candidate digest 只绑定七 tgz，不绑定 standalone 和 evidence；
- main commits/tag 无受信 signature/attestation；CI 没有 OIDC attestation + immediate verify；
- mutable action tags/`ubuntu-latest` 仍在使用。

### Secret scanning

- 现有 scanner 规则较窄，且整类跳过 `**/*.test.ts` 与 `fixtures/**`；published fixtures 不能整目录豁免；
- 本机缺 Gitleaks，尚未完成 full-history、current tree 和安全解包 artifact scan；
- Synara external source 中 PEM/generic 命中只完成规则级初筛，必须用 redacted scanner + exact fingerprint
  allowlist 复核。

### Consumer closure

- offline verifier 跳过 testkit 安装核验，只对 Distribution stdio 做文件 SHA-256；
- frozen lock 可证明六个安装包 integrity，但不能替代八个 release artifact 的逐项验证；
- Synara external source 另含 Bun/npm/Go lock、GitHub/pkg.pr.new URL 与 tag-only image，没有综合 license report。

## Required P0 evidence before closure

1. 固定 scanner/SBOM/license tool version + binary/OCI digest；
2. Gitleaks full-history/tree/extracted-artifact `--redact`；只保存删除 Secret/Match/raw line 后的 sanitized report；
3. 对七 tgz 的生产 closure 生成 SPDX/CycloneDX；standalone 结合 Bun metafile 还原 bundled dependency；
4. 生成 THIRD_PARTY_NOTICES 和 license text SHA-256；unknown/NOASSERTION fail closed；
5. canonical `artifact-set.json` 覆盖七 tgz、standalone、SBOM、NOTICE、sanitized scan/license reports；
6. provenance subjects/materials 覆盖 artifact set、source commit/tree、locks、external dependency integrity、
   workflow/action SHA 和 tool digests；
7. GitHub OIDC attestation 后在同一 workflow 立即 verify；
8. consumer 对八个 release artifacts 逐项 digest verify；testkit 明确属于 release-set 或 install closure；
9. waiver 使用 exact finding/digest + justification/owner/expiry，禁止整目录跳过。

在这些 evidence 完成前，不得把当前 RC 称为 license/provenance/secret cleared，也不得关闭
`G-INVENTORY` 的 provenance 子项或 `G-SUPPLY-CHAIN`。
