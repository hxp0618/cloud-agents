# P1 dependency review：Ajv 8.20.0 / ajv-formats 3.0.1

- Status：APPROVED
- Scope：root exact `devDependencies`，仅用于仓内 P1 JSON Schema 2020-12 contract validation
- Accountable owner：hxp0618
- Independent evidence reviewer：Codex P1 supply-chain explorer（未参与本 dependency 实现）
- Review date：2026-08-10 Asia/Shanghai

## Fixed closure

| Package       | Version  | npm integrity                                                                                     | License |
| ------------- | -------- | ------------------------------------------------------------------------------------------------- | ------- |
| `ajv`         | `8.20.0` | `sha512-Thbli+OlOj+iMPYFBVBfJ3OmCAnaSyNn4M1vz9T6Gka5Jt9ba/HIR56joy65tY6kx/FCF5VXNB819Y7/GUrBGA==` | MIT     |
| `ajv-formats` | `3.0.1`  | `sha512-8iUql50EUR+uUcdRQ3HDqa6EVyo3docL8g5WJ3FNcWmu62IbkGUue/pEyLBW8VGKKucTPgqeks4fIU1DA4yowQ==` | MIT     |

Transitive closure 固定为 `fast-deep-equal@3.1.3`、`fast-uri@3.1.5`、
`json-schema-traverse@1.0.0`、`require-from-string@2.0.2`。六个 package 在原 `bun.lock` 已存在；提升根 direct
edge 后只允许 root importer 改变，版本与 SRI 不得漂移。

## Evidence digests

- pre-change `bun.lock` SHA-256：`f020af11710a943ca9516c70eff119ee25cd7335f8aff379d50ab9ed3f222f9f`
- six-package SRI manifest SHA-256：`bdef5614eef5b5a437d65119a1ac5878c1121ec88fddc9a7abead81b74b82343`
- official tarball SHA-256 manifest：`68489e7d90a3eb596d4d1554893adb2a5ea75e2b8ddb237e2bb67d36a83b469f`
- six-package LICENSE manifest：`bc73aefa10ed547582bd48460eeb1eadf4d7c2c10d9c8bacfb5d0fe6284ae908`
- OSV fixed-version response：`e7e00f5759742ba9322ec6db4bd44d79d245d9831a06655a80cdb93e39c27928`
- npm audit response：`4f12cbad0e7ecd65f1b70fa84eeed108609f55f6d549ed8eaa29c377105b98e5`
- P0 runtime supply-chain evidence：`237f0399c6dd8fcd21acd86c1cd6b1c01630f5bb60375cf61ec50dc051336f95`
- P0 license inventory：`5594817617302a4ea9c0a0100940dc9fdc528067f081b2edffe6b81b672c6683`
- P0 THIRD_PARTY_NOTICES：`12fb1e275ab6998ae2f345d43337c78327f208d0e0575db13f04b111682cf2a9`

Durable evidence locators：

- [`bun.lock`](../../../../bun.lock) 与 [`contracts/generation.lock.json`](../../../../contracts/generation.lock.json)
  固定 direct edge、完整 SRI 与 dependency-lock digest；
- [P0 runtime supply-chain evidence](../../p0/provenance/runtime-supply-chain-audit.json)、
  [license inventory](../../p0/provenance/license-inventory.json) 与
  [THIRD_PARTY_NOTICES](../../p0/provenance/THIRD_PARTY_NOTICES.md) 保存生产 closure、license decision 与 notice；
- 上述 package-specific SRI/tarball/license/OSV/npm-audit digest 是独立 reviewer 在权限为 `0700` 的临时目录
  生成的补充证据；raw response 在 review 后删除，不能单独作为长期 authority。长期重放必须以仓内 lock、P0
  evidence 与下列命令重新生成新 snapshot，不能仅信任裸 digest。

固定本地重放命令（从 repo root，先确认 Node `24.13.1` / Bun `1.3.14`）：

```bash
sha256sum bun.lock docs/plan/p0/provenance/runtime-supply-chain-audit.json \
  docs/plan/p0/provenance/license-inventory.json \
  docs/plan/p0/provenance/THIRD_PARTY_NOTICES.md
bun install --frozen-lockfile --ignore-scripts
bun scripts/generate-platform-contract-lock.ts --check
```

联网 advisory refresh 不是 immutable proof；每次 refresh 必须保存 sanitised response、scanner/database timestamp
和 digest，并按计划的 non-bit invalidation 规则重新判断。不得用旧的空 OSV/npm-audit response证明未来仍无漏洞。

六包均无 `preinstall`、`install`、`postinstall` 或 `prepare`。固定版本 OSV 查询为空，npm audit 返回 0
vulnerabilities；许可证为 MIT，`fast-uri` 为 BSD-3-Clause，均已进入 P0 notices。

## Alternatives

- 采用 Ajv：它在当前 lock closure 中已经存在，提供 Draft 2020-12、strict mode、离线 `$id/$ref` 注册和可检查
  的 error objects；本切片仅提升 root dev direct edge，没有引入新的 transitive version。
- 否决手写通用 JSON Schema validator：P1 只手写不可由 schema表达的 tenant/RBAC/JCS semantic seam；若手写
  Draft 2020-12 解释器，会产生第二份 schema authority并难以覆盖 `$ref`、conditional、format 与 error path。
- 其他第三方 validator 本轮不引入：它们会新增尚未完成 license/SRI/vulnerability review 的 dependency closure。
  这不是断言其他实现能力不足；如后续替换，必须用同一 30-case corpus、官方 JSON Schema suite、N/N-1 与资源
  上限重新做独立 decision。
- `ajv-formats` 仅用于已有审查的 `date-time`/`uri`；自行维护这两个 parser 会复制标准边界，注册全部 formats
  又会扩大未使用的 attack surface，因此两者都被否决。

## Required operating boundary

- 使用 `ajv/dist/2020.js` 的 `Ajv2020`；不得用默认 Draft-07 实例；
- `$data=false`，schema 只来自受信的 checked-in contract，禁止运行时编译远端或用户提交 schema；
- `ajv-formats` 只注册当前需要的 `date-time` 与 `uri`；
- schema object、pattern、format 和 input length 必须有上限；未来用于不可信生产 payload 前重新做 ReDoS、
  depth、`allErrors` 与资源耗尽评审；
- 任一 package version/SRI、transitive closure、license、install hook 或 applicable vulnerability 变化都会使本
  review 失效；
- 本批准不覆盖 Redocly、Buf、protobuf、Connect、gRPC、Go JSON Schema validator、`x/text` 或任何其他新增
  dependency，也不关闭 `G-CONTRACT`、`G-SUPPLY-CHAIN`、release、deployment 或 production Gate。
