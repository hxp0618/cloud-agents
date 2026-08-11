# P1 dependency implementation closure：pgx v5.10.0 + x/text v0.39.0

- Status：**APPROVED — dependency implementation closure only**
- Scope：固定提交 `99106e8a77b6318830c63b4a4c294fa7ff8f1de4` 的 `services/control-plane` source、
  `go.mod` / `go.sum` 与 non-test `go list -deps ./...` closure
- Accountable owner：hxp0618
- Implementation evidence owner：Codex P1 supply-chain worker
- Review date：2026-08-11 Asia/Shanghai
- Toolchain：Go `1.26.5 darwin/arm64`，`GOWORK=off`，`GOTOOLCHAIN=local`，`GOFLAGS=-mod=readonly`
- Source commit：`99106e8a77b6318830c63b4a4c294fa7ff8f1de4`

## Decision and supersession

本记录确认 Control Plane 已按
[`x/text v0.39.0 remediation review`](./x-text-v0.39.0.md) 实施审查后的闭包：

- `github.com/jackc/pgx/v5 v5.10.0` 是 exact direct requirement；
- `golang.org/x/text v0.39.0 // indirect` 是 ordinary exact MVS floor，不是 `replace` 或 fork；
- MVS 同时选择 production `golang.org/x/sync v0.21.0`，以及 graph-only
  `x/mod v0.37.0` / `x/tools v0.47.0`；
- `go mod tidy -diff` 为空，`go mod verify` 输出 `all modules verified`；
- `go test -count=1 ./...` 与 `go vet ./...` 在固定 source 上通过；
- `replace`、`exclude`、`retract` 数量均为 0。

因此，本记录在当前 exact bits、source import closure 和 advisory snapshot 的边界内，**取代**
[`pgx v5.10.0 BLOCKED review`](./pgx-v5.10.0.md) 的实施状态。原文不改写：它仍是默认
`x/text v0.29.0` 为什么被拒绝的历史安全证据。本记录不是对原先 vulnerable default closure 的豁免。

这里的 APPROVED 只表示依赖选择与仓内 supply-chain artifacts 已落实；不关闭 `G-SUPPLY-CHAIN`、`G-DATA`、
P1 Exit、RC、release、deployment 或 production Gate。

## Fixed module inputs

固定 source authority：

| Source identity                                                 | Value                                                              |
| --------------------------------------------------------------- | ------------------------------------------------------------------ |
| Commit                                                          | `99106e8a77b6318830c63b4a4c294fa7ff8f1de4`                         |
| Repository tree OID（Git SHA-1 object format）                  | `606ab156439e6b30129f7738c646f1b4a3c6d70a`                         |
| `services/control-plane` subtree OID（Git SHA-1 object format） | `ad47c98dba6bebdbaaa612d7e9a5500f833db8ee`                         |
| 54-file Control Plane `git ls-tree` manifest SHA-256            | `65ae44316bfc02d5bf4980d73c1a521518dc499dc2e5cd68f4b58d3c50a3e173` |
| 25-file tracked Go-source manifest SHA-256                      | `ae5178f70c74dcd75ee530d4e44325bb19ec1c5d1769ce7f08ac574d0293cc05` |

这里同时记录 Git object identity 与 SHA-256 manifest：前者绑定 repository/subtree DAG，后者为当前证据工具提供
算法独立的可重放文件清单摘要。生成时 tracked worktree clean，只有本记录管理的四个 untracked supply-chain
artifact；它们不是 source commit 的一部分，也没有参与 source/import digest。

| Input                                | SHA-256                                                            |
| ------------------------------------ | ------------------------------------------------------------------ |
| `services/control-plane/go.mod`      | `be3b52ada42aee131f5ab650ecd9d17aa65b779d52615cd3b607df83507b4f5e` |
| `services/control-plane/go.sum`      | `f65654519c652388e9f315cef1ef299e86fdc964d8d18332c05dbdb51a40c7fb` |
| sorted `go list -m all`              | `4c6ddbb19110b86687228f56f48de96e10eea997d6c35283329945d08d52db1a` |
| sorted `go mod graph`                | `8264f2a6181bda29e3ed7e13cf410fb6f6e91cf5b0c5a1306a15e310a48e675e` |
| sorted effective production modules  | `12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` |
| sorted effective production packages | `07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354` |

`go.sum` 是主模块实际需要的 checksum set；graph pruning 不保证它包含所有 graph-only module 的 content
sum。为避免把“未写入 `go.sum`”误解为“未固定”，相邻
[`dependency-lock.json`](../../../../services/control-plane/dependency-lock.json) 还记录 15 个 selected third-party
module 的 `module_sum`、`go_mod_sum`、proxy zip SHA-256、proxy `go.mod` SHA-256、SumDB lookup response
SHA-256、license text SHA-256 和 production/graph-only classification。

本地实际 `GOPROXY` 是 `https://goproxy.cn,direct`；`GOSUMDB=sum.golang.org` 且 `GONOSUMDB`、
`GOPRIVATE` 为空。因而下载 transport 不是 bits authority，Go module `h1` 与公开 SumDB witness 才是。RC
重放应优先使用 `https://proxy.golang.org`，但只要 exact module sums、zip digest 与 SumDB 一致，镜像本身不会
改变候选 bits。

## Production-linked versus graph-only closure

`go list -deps ./...` 的 non-test closure 包含 29 个第三方 package，来自以下 6 个实际链接 module：

| Module                           | Version                              | License      | License text SHA-256                                               |
| -------------------------------- | ------------------------------------ | ------------ | ------------------------------------------------------------------ |
| `github.com/jackc/pgpassfile`    | `v1.0.0`                             | MIT          | `adb1663fda031df8f4344aa68f299fd87d80353e31339406742ded21dae65702` |
| `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | MIT          | `fc505773403fe869ed64cc2235cdd13988a427bb7e3a7e7004a3f4b27420f8fc` |
| `github.com/jackc/pgx/v5`        | `v5.10.0`                            | MIT          | `467f95e074fe23079a5623ed652619682692041b8551da27e3c2ddb9659a1507` |
| `github.com/jackc/puddle/v2`     | `v2.2.2`                             | MIT          | `2d50e98a4900b4d6457a38d39c1432fdc156fc2f7b365f2e33ec9344acbb0057` |
| `golang.org/x/sync`              | `v0.21.0`                            | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `golang.org/x/text`              | `v0.39.0`                            | BSD-3-Clause | `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |

独立复核发现 `x/sync v0.21.0` 与 `x/text v0.39.0` 除 BSD-3-Clause `LICENSE` 外，还都分发相同的根
`PATENTS` 文件：标题为 `Additional IP Rights Grant (Patents)`，SHA-256 为
`96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc`。该文本包含 Google 授予的
patent license 及 patent litigation termination 条款，不能只用 SPDX `BSD-3-Clause` 代替。

修正后的 notice 完整复制该文本一次，并明确它独立适用于两个 module；dependency lock 对两个 module 分别记录
`PATENTS` path/digest，CycloneDX component 也分别携带机器可读的 path/title/SHA-256 properties。共享展示不合并
两个 module 各自的法律适用关系。此前只包含 BSD license 的 artifact snapshot 已被本记录的新 digest 作废。

以下 9 个 module 被 `go list -m all` 选择，但没有 package 出现在 non-test `go list -deps ./...` 中：

```text
github.com/davecgh/go-spew v1.1.1
github.com/kr/pretty v0.3.0
github.com/pmezard/go-difflib v1.0.0
github.com/stretchr/objx v0.1.0
github.com/stretchr/testify v1.11.1
golang.org/x/mod v0.37.0
golang.org/x/tools v0.47.0
gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c
gopkg.in/yaml.v3 v3.0.1
```

它们在 SBOM 中标为 `graph-only` / `not-linked-not-distributed`，不是当前 binary notice scope。如果未来
vendor 全 graph、分发 module cache、运行上游测试/工具，或 source/build tags 引入其中任一 package，必须重新
分类并补齐相应 notice。

## Durable artifacts

| Artifact                                                                              | Scope                                                                                 | SHA-256                                                            |
| ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| [`dependency-lock.json`](../../../../services/control-plane/dependency-lock.json)     | 15-module selected graph、6/9 分类、SumDB/module/proxy/license/vulnerability evidence | `6f4aa06790acec55fe2c88d83cdab113dac2cde9b742ac58a8239705f6ddacd2` |
| [`sbom.cdx.json`](../../../../services/control-plane/sbom.cdx.json)                   | CycloneDX 1.6；15 components；root only depends on 6 production-linked components     | `9e3b868e61198733cbdbdcb2cf19f3bc9eaff0930b7cdd368469e00ba96daf2c` |
| [`THIRD_PARTY_NOTICES.md`](../../../../services/control-plane/THIRD_PARTY_NOTICES.md) | 6 个实际链接 module 的完整 copyright/license 及适用的 PATENTS grant                   | `5fa0f0ebf9c81ea7b9d15714799c0c6b2b3f5be65a52e3eee0b0b96f450d0f5a` |

`sbom.cdx.json` 已使用官方 CycloneDX 1.6、SPDX 和 JSF schemas 校验。SBOM 明确保留 graph-only inventory，
但 root `dependsOn` 只列实际 production closure；不得把 SBOM 中“存在 component”自动解释为“已链接”。

## Vulnerability snapshot

Snapshot 生成于 `2026-08-11T07:04:31Z`；结果是时点证据，不是永久安全证明。

| Check               | Fixed tool / database                                   | Result                              | Evidence SHA-256                                                                                                                                      |
| ------------------- | ------------------------------------------------------- | ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| module scan         | `govulncheck v1.6.0`，DB updated `2026-08-10T22:42:36Z` | exit 0，`No vulnerabilities found.` | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                                                                    |
| symbol scan `./...` | 同上                                                    | exit 0，`No vulnerabilities found.` | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                                                                    |
| OSV querybatch      | Go ecosystem，15 个 exact selected module               | 15 responses，0 findings            | query `d2ef1d0cc93788e13a3e12a8df0f9ee8ee5c8df17851978df6b815d630957e05`；response `dfffd23768f912c4b64bd2c127cef7c089434b0aaf86855657e6b8c4cb95386c` |

原 BLOCKED review 的 `GO-2026-5970` 路径没有 suppression：当前 symbol scan 实际选择 advisory 指明的首个
fixed `x/text v0.39.0`。不过 scanner output 恰好相同只说明两次文本都是 `No vulnerabilities found.`，不代表
module scan 和 symbol scan 是同一个分析。

## Source-tree and non-bit safety boundary

P1 migration runner core 已进入固定提交，`dependency-lock.json` 与 SBOM 均记录 `source_tree_committed=true`、
source commit、repository tree、Control Plane subtree、tracked manifest 和 Go-source manifest。non-test import
closure、module/symbol scan、test 与 vet 都从该 exact source 重放；生成时 tracked worktree clean。本记录的四个
untracked evidence artifact 不属于 source tree，且没有参与 source/import digest。

这消除了上一 snapshot 的“runner source 尚未提交”边界，但不把 supply-chain artifact 自身升级为 release
provenance：四个 artifact 形成集成 commit 后仍需记录其新 commit/tree，并对最终 binary/artifact 再做 same-bits
scan，才能作为 RC closure evidence。

以下任何一项变化都会使本记录失效：version、module/go.mod sum、proxy bits、license/PATENTS text、Go toolchain、
module graph、source import、build tag、distribution scope、scanner finding 或 advisory database。尤其是
advisory DB 和 SumDB lookup response 的 signed tree head 都是 non-bit-safe snapshot；它们变化时必须刷新判断，
不能只比较旧 response digest。稳定的 bits identity 是 exact version + module `h1` + proxy zip digest，而不是
旧时点的空漏洞响应。

## Exact local replay

从 repository root 执行；命令不读取数据库 DSN 或生产凭据：

```bash
set -euo pipefail

test "$(git rev-parse HEAD)" = "99106e8a77b6318830c63b4a4c294fa7ff8f1de4"
test "$(git rev-parse 'HEAD^{tree}')" = "606ab156439e6b30129f7738c646f1b4a3c6d70a"
test "$(git rev-parse 'HEAD:services/control-plane')" = "ad47c98dba6bebdbaaa612d7e9a5500f833db8ee"
test -z "$(git status --porcelain --untracked-files=no)"

git ls-tree -r --full-tree HEAD -- services/control-plane \
  > /tmp/cloud-agents-control-plane-ls-tree.txt
test "$(sha256sum /tmp/cloud-agents-control-plane-ls-tree.txt | awk '{print $1}')" = \
  "65ae44316bfc02d5bf4980d73c1a521518dc499dc2e5cd68f4b58d3c50a3e173"
git ls-tree -r --full-tree HEAD -- services/control-plane |
  awk '$4 ~ /\.go$/ {print}' | LC_ALL=C sort \
  > /tmp/cloud-agents-control-plane-go-ls-tree.txt
test "$(sha256sum /tmp/cloud-agents-control-plane-go-ls-tree.txt | awk '{print $1}')" = \
  "ae5178f70c74dcd75ee530d4e44325bb19ec1c5d1769ce7f08ac574d0293cc05"

cd services/control-plane
export GOWORK=off
export GOTOOLCHAIN=local
export GOFLAGS=-mod=readonly

test "$(go version | awk '{print $3}')" = "go1.26.5"
test -z "$(go env GONOSUMDB)"
test -z "$(go env GOPRIVATE)"
! grep -nE '^(replace|exclude|retract)[ (]' go.mod
go mod tidy -diff
go mod verify
go test -count=1 ./...
go vet ./...

LC_ALL=C go list -m all | LC_ALL=C sort > /tmp/cloud-agents-selected-modules.txt
LC_ALL=C go mod graph | LC_ALL=C sort > /tmp/cloud-agents-module-graph.txt
go list -deps -json ./... > /tmp/cloud-agents-deps.json
jq -r 'select(.Module != null and .Standard != true and .Module.Main != true) |
  [.Module.Path,.Module.Version] | @tsv' /tmp/cloud-agents-deps.json |
  LC_ALL=C sort -u > /tmp/cloud-agents-production-modules.txt
jq -r 'select(.Module != null and .Standard != true and .Module.Main != true) |
  [.ImportPath,.Module.Path,.Module.Version] | @tsv' /tmp/cloud-agents-deps.json |
  LC_ALL=C sort -u > /tmp/cloud-agents-production-packages.txt

sha256sum go.mod go.sum \
  /tmp/cloud-agents-selected-modules.txt \
  /tmp/cloud-agents-module-graph.txt \
  /tmp/cloud-agents-production-modules.txt \
  /tmp/cloud-agents-production-packages.txt \
  dependency-lock.json sbom.cdx.json THIRD_PARTY_NOTICES.md

# Fail closed on classification drift and missing additional-IP-rights grants.
jq -e '
  (.modules | length) == 15 and
  ([.modules[] | select(.classification == "production-linked")] | length) == 6 and
  ([.modules[] | select(.classification == "graph-only")] | length) == 9 and
  ([.modules[] | select(
    (.path == "golang.org/x/sync" or .path == "golang.org/x/text") and
    .patents.path == "PATENTS" and
    .patents.sha256 == "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc"
  )] | length) == 2
' dependency-lock.json

jq -r '.modules[] | select(.classification == "production-linked") |
  [.path,.version] | @tsv' dependency-lock.json | LC_ALL=C sort |
  diff -u /tmp/cloud-agents-production-modules.txt -

jq -r '.modules[] | [.path,.version] | @tsv' dependency-lock.json |
  LC_ALL=C sort > /tmp/cloud-agents-lock-modules.txt
grep -v '^github.com/hxp0618/cloud-agents/services/control-plane$' \
  /tmp/cloud-agents-selected-modules.txt | awk '{print $1"\t"$2}' |
  LC_ALL=C sort | diff -u - /tmp/cloud-agents-lock-modules.txt

notice_sha=$(sha256sum THIRD_PARTY_NOTICES.md | awk '{print $1}')
test "$notice_sha" = "$(jq -r '.inputs.third_party_notices_sha256' dependency-lock.json)"

jq -e '[.components[] | select(
  ([.properties[]? | select(
    .name == "cloud-agents:additional-ip-rights-grant-sha256" and
    .value == "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc"
  )] | length) == 1
)] | length == 2' sbom.cdx.json

tool_dir=$(mktemp -d /tmp/cloud-agents-govulncheck-v1.6.0.XXXXXX)
chmod 700 "$tool_dir"
GOBIN="$tool_dir" go install golang.org/x/vuln/cmd/govulncheck@v1.6.0
"$tool_dir/govulncheck" -version
"$tool_dir/govulncheck" -scan=module
"$tool_dir/govulncheck" -scan=symbol ./...
```

OSV 是联网 refresh；必须由 `dependency-lock.json` 中 15 个 exact path/version 重新生成 querybatch，并保存
scanner/database timestamp、canonical response、digest 和 finding decision。不要把旧的 0 findings 复制成新
RC 证据。

## Gate boundary

本切片证明 dependency implementation、selected graph inventory、license text、notice、SBOM 和本地时点扫描一致。
它没有证明 PostgreSQL 15/16/17 matrix、真实 SCRAM/pool/tenant transaction 语义、migration runtime catalog、
fixed-SHA cloud replay、release artifact same-bits 或独立 Gate closure。因此：

- `G-SUPPLY-CHAIN`：**OPEN**；需集成 commit 后 source-bound refresh、最终 binary/artifact scan 与 closure record；
- `G-DATA`：**OPEN**；由数据库 matrix、migration runner 与 tenant isolation evidence 单独关闭；
- RC/release/deployment：**NOT AUTHORIZED**。
