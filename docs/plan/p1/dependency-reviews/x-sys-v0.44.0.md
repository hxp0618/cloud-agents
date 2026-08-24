# P1 dependency implementation closure：golang.org/x/sys v0.44.0

- Status：**APPROVED — dependency implementation closure only**
- Scope：固定 source commit `f0f1aceec07d6e4d7813f1483f26e7ef9528c245` 的
  `services/control-plane` source、module graph，以及 Linux/Darwin non-test import closure
- Reviewed dependency：`golang.org/x/sys v0.44.0`
- Accountable owner：hxp0618
- Evidence owner：Codex P1 supply-chain worker
- Source refresh snapshot：2026-08-15T18:20:12Z
- Toolchain：Go `1.26.5 darwin/arm64`，`GOWORK=off`，`GOTOOLCHAIN=local`，
  `GOFLAGS=-mod=readonly`
- Prohibited：`replace`、fork、vendor patch、floating version、blank import、Darwin production fallback

## Decision and supersession

本记录取代本文件此前的 `APPROVED_FOR_IMPLEMENTATION（conditional；尚未引入）` 状态。exact committed source
已经在 Linux build-tagged production package 中直接 import `golang.org/x/sys/unix`；`go.mod` 将
`golang.org/x/sys v0.44.0` 固定为 ordinary exact direct requirement，`go.sum` 固定两个 exact `h1`。

因此 x/sys 现在是 16-module selected graph 的 direct module，也是 Linux production-linked、noticed、SBOM root
dependency。Darwin non-test closure 不含 x/sys package，这是 build-tag platform 差异，不是 graph-only 分类：同一主模块
的 direct selected requirement 仍存在，而 production filesystem 在 Darwin 明确 fail closed。

此批准仅关闭 **dependency implementation/supply evidence**。它不声称 production filesystem runtime 已启用：
`NewEvidenceSink()` 仍返回 `CodeProjectionNotImplemented`，Linux `newProductionEvidenceFSRoot` 也在任何 mutation 前
返回稳定 filesystem failure，因为 trusted mount authority 尚未实现。真实 ext4/XFS syscall probe、跨进程锁、
power-loss/restart、runner/DB/cloud、RC/release Gate 仍然开放。

## Exact source authority

| Evidence | Exact value |
| --- | --- |
| Source commit | `f0f1aceec07d6e4d7813f1483f26e7ef9528c245` |
| Repository tree OID（Git SHA-1） | `aef7a5bdb503c65e16065b50dda6bce51cf4535d` |
| `services/control-plane` subtree OID（Git SHA-1） | `0e336f00df4434e98d32dbc57c0b0b292ccb9f23` |
| 274-file tracked manifest SHA-256 | `0b13d2e414d95ce741ebe7df21739c700adf70e5179a8839ec8ed4cfaab52876` |
| 210-file tracked Go-source manifest SHA-256 | `a2fc88bc895a251644552067b8e646390933251de6ec00946c0fedc8e5bb1f7b` |
| `go.mod` SHA-256 | `ec30f2a2af4c9a80aeec1538f9aff7d78e1ad1fd5b323195c49d0826d7062bc7` |
| `go.sum` SHA-256 | `8d46b65698d18e97869fa31da700c24f3bfbc8b091afefd5584b3aaa1824d977` |
| sorted `go list -m all` SHA-256 | `0f98de7d6500cfc9bda9c5d76cb269b714e2cd31a18857b7433e33fe540e7793` |
| sorted `go mod graph` SHA-256 | `28d53b4b26eb41e956644aeced541d33df787590a2e91c9fc81cb15973bf6416` |

tracked/source manifests 直接从 exact commit 的 `git ls-tree` 机械计算。本记录、lock 与 SBOM 是该 source
commit 之后的三个派生 refresh，不反向成为 source/import authority；NOTICE 的法律位未变化，因此保持 same-bits。

## Platform-specific production closure

Linux amd64 与 arm64（均 `CGO_ENABLED=0`）产生相同 non-test closure：7 modules、30 packages。

| Linux production module | Version | Requirement | x/sys effective package |
| --- | --- | --- | --- |
| `github.com/jackc/pgpassfile` | `v1.0.0` | transitive | — |
| `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | transitive | — |
| `github.com/jackc/pgx/v5` | `v5.10.0` | direct | — |
| `github.com/jackc/puddle/v2` | `v2.2.2` | transitive | — |
| `golang.org/x/sync` | `v0.21.0` | transitive | — |
| `golang.org/x/sys` | `v0.44.0` | **direct** | `golang.org/x/sys/unix` |
| `golang.org/x/text` | `v0.39.0` | direct | — |

| Closure | Modules SHA-256 | Packages SHA-256 | Count |
| --- | --- | --- | --- |
| Linux amd64/arm64 | `48ca0dbaba0f918d99091decd0520a70327c36badb7d74c7cbbe1e180cd66e5f` | `12a56c91f56460e9757560f00234c06cec462f248df6a770d41172168e9a8d08` | 7 / 30 |
| Darwin arm64 | `12203596417e4926a8292ad208df4d410ef0d6e89627320e2c4fe08858a5154b` | `07d05153aff50a4db408a9e4d34c4a298a21f5ccd5615b9940e4e8521e0de354` | 6 / 29 |

9 个 module 只在 selected graph 中出现，未进入 Linux non-test package closure，继续分类为 graph-only：
`go-spew`、`kr/pretty`、`go-difflib`、`objx`、`testify`、`x/mod`、`x/tools`、`check.v1`、`yaml.v3`。
SBOM 保留其 inventory，但 root `dependsOn` 仅含上述 7 个 Linux production module。

## Fixed upstream bits and provenance

| Evidence | Exact value |
| --- | --- |
| Module/version | `golang.org/x/sys v0.44.0` |
| Canonical tag commit | `fb1facd76f95fa87c151018200ea5e4892ff115d` |
| Tag time | `2026-04-23T15:37:02Z` |
| Minimum Go | `1.25.0` |
| Module dependencies | none |
| Module sum | `h1:ildZl3J4uzeKP07r2F++Op7E9B29JRUy+a27EibtBTQ=` |
| `go.mod` sum | `h1:4GL1E5IUh+htKOUEOaiffhrAeqysfVGipDYzABqnCmw=` |
| Official proxy zip SHA-256 | `f1fa1052808e6bd6eb9c5372c053b2370a582532fac5d6a4600e7a6fab190ff3` |
| Official proxy `go.mod` SHA-256 | `57f4393ea18d5446a12363b35c23a616d843fa1669c7121a70a2bc3a9677d665` |
| Official proxy `.info` SHA-256 | `db88d97c963506d830212c91e42f4bbd9076c18faccb127b6c096bb86bab0ae6` |
| SumDB lookup response SHA-256 | `d06fe1ff17b158574ead359c298c5cf5158c9a21072793c1c6b639185437d543` |

`go mod download -json` 的 `Origin.URL`、`Origin.Hash`、`Origin.Ref` 分别是 canonical
`https://go.googlesource.com/sys`、上述 commit、`refs/tags/v0.44.0`。transport mirror 不是 bits authority；stable
identity 是 exact version、module `h1` 与 proxy zip digest。SumDB response 含时点 signed tree head，故本次 digest
与 conditional review 的旧 digest 不同并不表示 module bits 漂移；它是必须注明时间边界的 non-bit-safe witness。

## License, PATENTS, NOTICE, and SBOM

- License：BSD-3-Clause；root `LICENSE` SHA-256
  `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`。
- Additional grant：root `PATENTS`；标题 `Additional IP Rights Grant (Patents)`；SHA-256
  `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc`。
- `x/sync`、`x/sys`、`x/text` 分发 byte-identical PATENTS。NOTICE 只复制文本一次，但明确该 grant 独立适用于
  三个 module；lock 与 SBOM 对三个组件各自记录 path/title/digest/scope，不能把共享展示误解为合并法律关系。
- CycloneDX root dependency 现有 7 项并包含 x/sys。x/sys component 标记 exact direct、Linux production-linked、
  `linux/amd64` + `linux/arm64`、BSD-3-Clause 与 PATENTS metadata。

| Derived artifact | SHA-256 |
| --- | --- |
| `services/control-plane/dependency-lock.json` | `f5764e7c0a3e28d1b7c16dfe9e0684bcbc788b955d36c48be184ca47a3e7c256` |
| `services/control-plane/sbom.cdx.json` | `10e97be62d601725ba0c81ac31d6e41e04bc22b1ed950464a3d984f88bedd75a` |
| `services/control-plane/THIRD_PARTY_NOTICES.md` | `1cadb7fc75886f9085a53d3b9cc174b4c024981f609e4d5951e4e3f877dcbb48` |

## Vulnerability snapshot

漏洞证据是联网时点判断，不是永久安全声明。

| Check | Snapshot/result | Evidence SHA-256 |
| --- | --- | --- |
| `govulncheck v1.6.0 -scan=module` | DB updated `2026-08-11T23:21:33Z`；0 findings | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5` |
| Linux symbol scan | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0`；0 findings | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5` |
| OSV querybatch | 16 exact selected modules；16 responses；0 findings | query `937ac9fb9495cc4ea13990a2cdead2c17f85fbfb817f610057051097aeb8d720`；canonical response `597de9d918195ae56f69090341f0a38ab47139b85ab19c2a11ec52f916e5f861` |

本次 source-bound refresh 没有重新执行联网扫描。module graph、Linux/Darwin non-test closure、exact versions 与 sums
均同 `53b2463` byte-identical，因此继承其 2026-08-12 module/symbol/OSV 时点结果；这不构成 2026-08-16 的新安全
结论。`GO-2026-5024` 影响 Windows API，且 v0.44.0 是 first-fixed version；这里没有用 platform suppression 替代
fixed version。任一 advisory DB、scanner、module graph、source import、build tag、version、sum、license/PATENTS
或 distribution scope 变化，都要求刷新判断。

## Verification and fail-closed runtime boundary

本 closure 的本地 replay 必须至少执行：

```bash
cd services/control-plane
export GOWORK=off GOTOOLCHAIN=local GOFLAGS=-mod=readonly

go mod tidy -diff
go mod verify
go test -count=1 ./...
go test -race -count=1 ./internal/migration
go build ./...
go vet ./...

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -exec=/usr/bin/true ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -exec=/usr/bin/true ./...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go test -exec=/usr/bin/true ./...
```

`-exec=/usr/bin/true` 让 Darwin host 只编译异构 test binaries，不尝试执行 Linux/其他架构产物；直接执行会以
`exec format error` 失败，不能把 host-kernel incompatibility 误报为 source compile failure。

还应机械断言：16 selected components；7 Linux production / 9 graph-only；3 direct requirements；Linux closure与
lock production rows一致；SBOM metadata/root dependency ref 都绑定 exact source commit；root `dependsOn` 恰含7项；
x/sys component 的 direct/platform/PATENTS properties 完整；三个 PATENTS scope exact 一致；NOTICE digest 与 lock
互绑；CycloneDX 1.6、SPDX、JSF schemas 全部通过。

上述 build/test 只验证当前封装和 fail-closed seam。Linux production constructor 当前仍主动拒绝 trusted mount
authority，因而不会进行 probe mutation；这不是 runtime enablement。真实 ext4/XFS mount authority、跨进程/断电
matrix 必须在后续单独关闭，不能用 unit fake、cross-compile 或 supply-chain closure 代替。

## Gate boundary

- x/sys dependency choice + exact direct integration + supply artifacts：**CLOSED for this source commit**。
- production filesystem constructor/runtime：**NOT IMPLEMENTED / fail closed before mutation**。
- `G-SUPPLY-CHAIN`：**OPEN**；最终 RC binary/artifact same-bits 与集成 commit provenance 尚未关闭。
- `G-DATA`、runner/DB/cloud、RC/release/deployment：**OPEN / NOT AUTHORIZED**。

## Durable upstream locators

- [pkg.go.dev `golang.org/x/sys@v0.44.0`](https://pkg.go.dev/golang.org/x/sys@v0.44.0)
- [pkg.go.dev `golang.org/x/sys/unix@v0.44.0`](https://pkg.go.dev/golang.org/x/sys/unix@v0.44.0)
- [canonical tag `v0.44.0`](https://go.googlesource.com/sys/+/refs/tags/v0.44.0)
- [official proxy `.info`](https://proxy.golang.org/golang.org/x/sys/@v/v0.44.0.info)
- [SumDB lookup](https://sum.golang.org/lookup/golang.org/x/sys@v0.44.0)
- [GO-2026-5024](https://pkg.go.dev/vuln/GO-2026-5024)
- [ADR 0010 §5.4](../../adr/0010-p1-postgres-projection-contract.md)
