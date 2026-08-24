# Gate record: `G-INVENTORY` / P0 / R3

- Evidence ID：`CAG-G-INVENTORY-P0-20260810-R3`
- Record type：PHASE
- Phase / aggregate Gate：`G-INVENTORY`
- Prerequisite record IDs：none
- Status：`VERIFIED`
- DRI：hxp0618（owner）；Codex P0 executor
- Independent reviewer：Codex P1 inventory R3 explorer（只读复核及三项故障注入）
- Date：2026-08-10 Asia/Shanghai
- Supersedes：`CAG-G-INVENTORY-P0-20260810-R2`（`INVALIDATED`）

## Fixed inputs

- `cloud-agents` evidence commit：`5209ea09da3f4f092886dc0a36d1bf2974958438`
- Evidence commit tree：`d13c78d61baf5dfa81491bd9d32c65295a242665`
- Remote feature ref：`hxp0618/cloud-agents:codex/cloud-agents-platform-p1@5209ea09da3f4f092886dc0a36d1bf2974958438`
- Runtime immutable source/tag：`49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a`
- Synara extraction source/tree：`2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0` /
  `ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc`
- Inventory / graph / decisions SHA-256：
  - `bee237da890f4f3d62fd524fd11142a6b6c883e82790e5d455c415461ae7b4e5`
  - `3c7484c296f56e86f6a523e122f234c0f3b4ce6516f9ed492b69ea8a64faae04`
  - `24a7f918636b7f0baafaa6c99ff1c04c9b8ad10163cbb568fd311718bb9342ee`
- Finalizer / generated summary SHA-256：
  - `af4eb4effb229dd1c814ead2db06db7e20f09154d9fb9ec44ed73724bcd44c7a`
  - `c614850325409eaa1dd3e9245ee11544e1211932d9b47bbe06e9d2072441a8d6`
- Source audit / generated evidence index SHA-256：
  - `b8548f38126c487991d04052c024fde322b29904c30e1afd685f6d441eb9b2d3`
  - `20e25d1b2238a22466ec0fe334b6b723cab398e459e342529ad7eea82d819cd3`
- Secret finding-set / triage SHA-256：`5f74f2af7a9468e56fcd65bf3f6cc2ee168bf19189cbec92d0f75832f0860724` /
  `2936cb8e883b296d13cd99f2ac4ba474c06e79dad4bce0cfdf9b4e76083b61b6`
- Source LICENSE blob / text SHA-256：`960499447d8ea8f6ce86017893f132f0c3885fef` /
  `305724dd050ca7ded99c662de813d755bc4ec3887c4543a37159c6662ca36d1b`
- Toolchain：Git 2.55.0；Node 24.13.1；Bun 1.3.14；Gitleaks 8.30.1
- Deployment profile：none；inventory/provenance only

## Why R3 supersedes R2

R2 的 8,625 行覆盖、source/license/secret provenance、classification、owner、capability 和 graph 都保持有效，
但其中两类 `target` 会把旧 Synara 实现错误提升为新的公开 ABI 或 wire authority：24 个旧服务 helper 指向
`sdk/go/*`，42 个 `docs/contracts/*` prose/schema 指向正式 `contracts/*`。该 target 语义与 P1 的边界冲突，
因此 R2 整体标记为 `INVALIDATED`，由绑定新 decision digest 的 R3 完整替代；R2 记录文件本身保持不可变。

R2 到 R3 共纠偏 66 行，且只改变迁移目标及相应说明：

- `target` 改变 66 行，`reason` 改变 44 行；
- `classification`、`owner`、`finalCapability`、source/license/secret provenance 改变 0 行；
- 24 个旧 helper 中，10 个进入 `services/control-plane/internal/*`，14 个进入
  `services/worker/internal/*`，公开 `sdk/go/*` 中保留 0 个；
- 42 个旧 contract reference 中，4 个进入 `conformance/runtime/legacy-oracles/*`，8 个进入
  `conformance/worker/legacy-oracles/*`，30 个进入
  `conformance/control-plane/legacy-oracles/managed-agent/*`，正式 `contracts/*` 中保留 0 个；
- selected candidate 仍为 1,129 行，8,625 个 target 仍全部唯一。

## Exit criteria mapping

| Criterion                                                  | Result | Evidence                                                                                       |
| ---------------------------------------------------------- | ------ | ---------------------------------------------------------------------------------------------- |
| exact extraction source, tree, clean/non-shallow identity  | PASS   | `synara-extraction-source-audit.json`                                                          |
| full code/SQL/schema/build/deploy/generated tracked input  | PASS   | 8,625 rows；inventory SHA `bee237da...`                                                        |
| per-file classification/owner/target/provenance complete   | PASS   | 8,625 unique paths/targets；decision SHA `24a7f918...`；unresolved 0                           |
| public Go SDK excludes legacy server implementation        | PASS   | 24/24 helper targets corrected；new negative invariant rejects any recurrence                  |
| formal contracts exclude legacy prose/schema authority     | PASS   | 42/42 references moved to legacy-oracle targets；new negative invariant rejects any recurrence |
| classification, owner, capability, and selection stability | PASS   | 66-line semantic diff；all four fields unchanged；selected candidate rows remain 1,129         |
| source license provenance complete                         | PASS   | fixed MIT LICENSE Git object/text digest                                                       |
| secret provenance complete                                 | PASS   | three Gitleaks scopes；56/56 exact triage；48 FP + 6 rewrite + 2 history quarantine            |
| deterministic clean-archive replay                         | PASS   | Git archive of `5209ea0` reproduced decisions/summary byte-for-byte and provenance audit PASS  |
| fail-closed regression behavior                            | PASS   | three independent fault injections each exited 1 on the intended invariant                     |
| independent semantic review                                | PASS   | R3 reviewer reported P0/P1/P2 finding count 0                                                  |

## Commands / evidence

```bash
/Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/finalize-synara-inventory.mjs
P0_GITLEAKS_BIN=/absolute/pinned/gitleaks \
  /Users/huang/devel/soft/nvm/versions/node/v24.13.1/bin/node \
  docs/plan/p0/scripts/audit-synara-extraction-source.mjs
shasum -a 256 \
  docs/plan/p0/synara-file-inventory.tsv \
  docs/plan/p0/synara-inventory-graph.json \
  docs/plan/p0/synara-inventory-decisions.tsv \
  docs/plan/p0/inventory-decision-summary.md
```

Clean-archive replay used `git archive 5209ea09da3f4f092886dc0a36d1bf2974958438` in a mode-0700 temporary
directory with fixed Node 24.13.1. It reproduced `synara-inventory-decisions.tsv` and
`inventory-decision-summary.md` byte-for-byte. The provenance audit exited 0 with `status=PASS`, decision SHA
`24a7f918...`, and `secretScanning.status=PASS_WITH_RESTRICTIONS`.

Independent review copied the fixed evidence into disposable workspaces and performed three negative tests：

1. 将一个旧 `services/control-plane/internal/*` helper 的 target 改回 `sdk/go/*`；finalizer exited 1；
2. 将一个旧 `docs/contracts/*` target 改回正式 `contracts/*`；finalizer exited 1；
3. 制造重复 target；finalizer exited 1。

三项都命中预期的 fail-closed invariant；未把故障输入或生成结果写回固定 evidence commit。

## Failures, retries, and restrictions

1. R2 并非遗漏文件，而是迁移 target 的 authority 语义错误；因此不能只在 P1 实现阶段口头忽略旧 target。
2. 旧 Go helper 可以作为固定来源的语义/实现候选进入服务 `internal`，但不得成为公开 Go SDK ABI；公开 Go SDK
   只允许生成的 contract/client surface。
3. 旧 prose/schema 仅是 conformance legacy oracle，不是新 JSON Schema、OpenAPI 或 Proto 的 wire authority；
   新正式 contract 必须独立定义和版本化。
4. 56 个 secret findings 的裁决未改变，仍非 blanket allowlist：6 个静态测试私钥必须 publication 前重写，
   2 个历史日志必须 source-history quarantine；禁止 graft Synara Git history。
5. Third-party dependency licenses、Runtime redistribution、签名/attestation、复制、公开发布、部署和真实 E2E
   仍是后续 Gate；本记录不授予这些权限。

## Rollback / cleanup evidence

- No runtime, database, endpoint, grant, workload, volume, or production state was created.
- Fault-injection and clean-archive replay used disposable directories only and did not mutate the fixed source or evidence commit.
- The pre-existing unrelated ADR worktree change was not included in evidence commit `5209ea09...`.

## Invalidation and propagation

This record becomes `INVALIDATED` if any of the following changes：

- fixed Synara source/tree, inventory selection rules, classification/owner/target/provenance, or graph semantics；
- either negative invariant stops rejecting legacy helper -> `sdk/go/*` or legacy contract -> formal `contracts/*`；
- target uniqueness, source LICENSE object/text, canonical secret finding identity, triage decision/expiry, or scanner
  version/digest changes；
- extraction imports Synara history, copies `REWRITE_REQUIRED` bytes, or promotes legacy-oracle content to formal wire
  authority without a separately reviewed contract lineage。

R2 decision SHA `4e8e92cfb48a2d272c4b025c815a8afb9f85ec7ab6be65a160f4893c85fc429d` 不得继续作为 P1
证据输入。任何已经或以后绑定该旧 digest 的 `G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1` 及 P1-P6 record
均不能继承 R3 的 `VERIFIED` 状态，必须以 R3 decision SHA `24a7f918...` 重新生成/复核。R3 失效时，同一组
下游 Gate 和所有消费该 decision set 的 P1-P6 record 一并失效。

## Sign-off

- DRI conclusion：R3 在不改变 inventory scope、classification、owner、capability 或 provenance 的前提下，纠正了
  66 个公开边界 target，并绑定可重放的固定 evidence commit。
- Reviewer conclusion：三项故障注入及固定 Node 24 clean-archive replay 均符合预期；R3 未发现 P0/P1/P2
  finding，可替代 R2 成为当前 `G-INVENTORY` authority。
- Closure decision：`G-INVENTORY = VERIFIED`。R2 同时变为 `INVALIDATED`；本记录不关闭
  `G-CONTRACT`、`G-DATA`、`G-AUTHORITY-P1`、aggregate `G-BASELINE`、M1、supply chain、release、deployment、
  Beta 或 GA。
