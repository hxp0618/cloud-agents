# P1 dependency review：x/text v0.39.0 remediation closure

- Status：**APPROVED**
- Scope：Control Plane 为解除 `pgx v5.10.0` 的 `GO-2026-5970` 阻塞而增加的 exact ordinary
  `require golang.org/x/text v0.39.0`；批准随 MVS 发生的 `x/sync v0.21.0` production upgrade，以及
  `x/mod v0.37.0` / `x/tools v0.47.0` graph-only upgrade
- Prohibited：`replace`、fork、vendor patch、vulnerability suppression、branch 或 floating pseudo-version
- Accountable owner：hxp0618
- Independent evidence reviewer：Codex P1 supply-chain reviewer（未修改依赖、Control Plane 实现或 tracker）
- Review date：2026-08-11 Asia/Shanghai
- Toolchain：Go `1.26.5 darwin/arm64`，`GOWORK=off`，`GOTOOLCHAIN=local`

## Decision

**批准以普通 exact `require` 把 `golang.org/x/text` 固定到 `v0.39.0`。** 在审查的
`github.com/jackc/pgx/v5 v5.10.0` 闭包中，这个约束会把实际链接的 `x/text` 从 `v0.29.0` 升到
`v0.39.0`，把实际链接的 `x/sync` 从 `v0.17.0` 升到 `v0.21.0`，并把仅存在于完整 module graph、不会被
Control Plane binary 链接的 `x/mod` / `x/tools` 分别提升到 `v0.37.0` / `v0.47.0`。没有新增 production
module，也没有 `replace`。

Go 官方 vulnerability record 把 [`GO-2026-5970`](https://pkg.go.dev/vuln/GO-2026-5970) 的首个修复版本
明确标为 `v0.39.0`。修复 change `5ae8e578e495731553eddba11b2d0e86c91a00ce` 已合入该 tag：Git ancestry
结果为 tag 比修复 commit ahead 1、behind 0。候选闭包上的 `govulncheck v1.6.0` module scan 与 symbol scan
都返回 0，OSV 对六个 production module 和两个 graph-only upgrade 的 exact-version 查询均为空。

因此，**这个 exact pin 足以解除 [pgx v5.10.0 review](./pgx-v5.10.0.md) 中唯一的 dependency
vulnerability blocker，并使该 pgx 选择在本记录的边界内可接受。** 但原 pgx 文档与项目状态不得因本记录自动
改写：只有实施者把审查后的版本与 checksum 落入 `services/control-plane/go.mod`、相邻 `go.sum`、dependency
lock、SBOM/license inventory/notice，并在真实 Control Plane source 上重放检查后，才能把 pgx review 从
`BLOCKED` 更新为 `APPROVED`。本记录本身不关闭 `G-DATA`、`G-SUPPLY-CHAIN`、P1 Exit、release 或 deployment
Gate。

## Requirement semantics

这里的 “ordinary direct require” 指 **Control Plane main module 中显式、可被 MVS 选择的版本约束**，而不是
`replace`、fork 或在 pgx 的 module cache 中打补丁。若 Control Plane source 不直接 import `x/text`，Go 1.26
的 `go mod tidy` 会把该行规范化为：

```go
require golang.org/x/text v0.39.0 // indirect
```

`// indirect` 只说明 main module 的源码没有直接 import 它，不会削弱 exact MVS floor；该行仍会覆盖 pgx 的
`v0.29.0` 要求。不得通过空白 import 伪装 code-direct dependency，也不得为了去掉注释而接受 non-tidy
`go.mod`。验收标准是 ordinary exact `require` 仍存在、`go mod tidy -diff` 为空、selected version 是
`v0.39.0` 且 replacement count 为 0。

## Reviewed closure delta

独立 harness 只 import native `pgxpool`，分别对默认 pgx 闭包和 remediation 闭包执行 `go mod tidy`、
`go list -m all`、`go mod graph`、`go list -deps`。唯一变化是：

| Module               | pgx default | Remediated | Effective classification                                      |
| -------------------- | ----------- | ---------- | ------------------------------------------------------------- |
| `golang.org/x/text`  | `v0.29.0`   | `v0.39.0`  | production；pgx SCRAM/Precis/Unicode normalization path       |
| `golang.org/x/sync`  | `v0.17.0`   | `v0.21.0`  | production；pgx/puddle，当前 effective package 为 `semaphore` |
| `golang.org/x/mod`   | `v0.27.0`   | `v0.37.0`  | graph-only；由 `x/text` 的 tagged `go.mod` 声明，未链接       |
| `golang.org/x/tools` | `v0.36.0`   | `v0.47.0`  | graph-only；由 `x/text` 的 tagged `go.mod` 声明，未链接       |

候选 binary closure 中实际解析到的升级 package 是：

```text
golang.org/x/sync/semaphore
golang.org/x/text/cases
golang.org/x/text/internal
golang.org/x/text/internal/language
golang.org/x/text/internal/language/compact
golang.org/x/text/internal/tag
golang.org/x/text/language
golang.org/x/text/runes
golang.org/x/text/secure/bidirule
golang.org/x/text/secure/precis
golang.org/x/text/transform
golang.org/x/text/unicode/bidi
golang.org/x/text/unicode/norm
golang.org/x/text/width
```

其余 pgx production module/version不变：`pgx v5.10.0`、`pgpassfile v1.0.0`、
`pgservicefile v0.0.0-20240606120523-5a60cdf6a761`、`puddle/v2 v2.2.2`。完整 graph 中已有的上游
test/tool modules 也没有新增；`x/mod` / `x/tools` 只是从旧 selected version 升级。若未来 vendor 完整 graph、
执行相关 tool、或把 `x/mod` / `x/tools` package 引入生产，它们必须从 graph-only 重新分类并重审实际 closure。

## Fixed upstream identity

| Module / version             | Tag source commit                          | Minimum Go | Module sum                                        | `go.mod` sum                                      |
| ---------------------------- | ------------------------------------------ | ---------- | ------------------------------------------------- | ------------------------------------------------- |
| `golang.org/x/text v0.39.0`  | `b326f3d3c814ab79b3c516f4ac03c2314d8df65f` | `1.25.0`   | `h1:UbZz4pLOvn600D6Oh6GGEI6VAmndrEBLv8/6BEXzyus=` | `h1:3UwRclnC2g0TU9x8PZiyfOajCd1zaUNHF9cvqcQZ+ZM=` |
| `golang.org/x/sync v0.21.0`  | `5071ed6a9f1617117556b66384f765c934de3698` | `1.25.0`   | `h1:HLII4xRRTtCRkxYp4HNFF0Js/Og6q2i++KXbg0gHCwM=` | `h1:9xrNwdLfx4jkKbNva9FpL6vEN7evnE43NNNJQ2LF3+0=` |
| `golang.org/x/mod v0.37.0`   | `deb1dfcdb7c7fd98fb5afddc3e95dd36d5880874` | `1.25.0`   | `h1:vF1DjpVEshcIqoEaauuHebaLk1O1forxjxBaVn884JQ=` | `h1:m8S8VeM9r4dzDwjrKO0a1sZP3YjeMamRRlD+fmR2Q/0=` |
| `golang.org/x/tools v0.47.0` | `fbf9f2e2c8124fbe1877f5ed2857111038d9fe12` | `1.25.0`   | `h1:7Kn5x/d1svx/PzryTsqeoZN4TZwqeH5pGWjefhLi/1Q=` | `h1:dFHnyTvFWY212G+h7ZY4Vsp/K3U4/7W9TyVaAul8uCA=` |

Go proxy `.info` 的 `Origin.Hash`、canonical `go.googlesource.com` tag ref、GitHub mirror tag ref 和
`go mod download -json` 对四项都给出相同 commit。SumDB 固定记录为：

```text
golang.org/x/text v0.39.0 h1:UbZz4pLOvn600D6Oh6GGEI6VAmndrEBLv8/6BEXzyus=
golang.org/x/text v0.39.0/go.mod h1:3UwRclnC2g0TU9x8PZiyfOajCd1zaUNHF9cvqcQZ+ZM=
golang.org/x/sync v0.21.0 h1:HLII4xRRTtCRkxYp4HNFF0Js/Og6q2i++KXbg0gHCwM=
golang.org/x/sync v0.21.0/go.mod h1:9xrNwdLfx4jkKbNva9FpL6vEN7evnE43NNNJQ2LF3+0=
golang.org/x/mod v0.37.0 h1:vF1DjpVEshcIqoEaauuHebaLk1O1forxjxBaVn884JQ=
golang.org/x/mod v0.37.0/go.mod h1:m8S8VeM9r4dzDwjrKO0a1sZP3YjeMamRRlD+fmR2Q/0=
golang.org/x/tools v0.47.0 h1:7Kn5x/d1svx/PzryTsqeoZN4TZwqeH5pGWjefhLi/1Q=
golang.org/x/tools v0.47.0/go.mod h1:dFHnyTvFWY212G+h7ZY4Vsp/K3U4/7W9TyVaAul8uCA=
```

Additional immutable evidence digests：

| Module            | Proxy zip SHA-256                                                  | Proxy `go.mod` SHA-256                                             | SumDB response SHA-256                                             |
| ----------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `x/text v0.39.0`  | `cbfa33111dfa6cbafef63103b82c544d35df425824ac94ea19629a12bdbf0523` | `40e9425e17dcc56faf496619fde6908631d57b2cce0f766c4dca6bea8fc93838` | `7b4e7ae9cd019cdd3138068153c8d51986825417d7cd00506185a27bdd1d0d44` |
| `x/sync v0.21.0`  | `ee65459023de7f24836f6e2123144b5329bd0a4d05a87c3c448509378e2e6be7` | `a3e29e76060bd561060454b1fa2bdcd66674f60c9ca93833b8106355e34c603c` | `d2f06badf19238f6780a381c378a255a121aaeade602277047e9b91245a0d696` |
| `x/mod v0.37.0`   | `91e8e4e9b74a8706dae808b66538d4ab22befd00c11f34134eb97ff572d52e85` | `538472fdf094dd5e49dc40e70468fa931a93c241eba07fb946a98747c94ab4df` | `68be8104fe681822387b1a297eeff44417ee9f65c3133f75034f8ff08677339b` |
| `x/tools v0.47.0` | `143d132b519da1454db967febb65241796805d7c9d4752034341c1376fd3d7f1` | `eb46e44850fb4dca48f7b680cac5177682cb0e302b307d4d3dbd7ed9df05fc0f` | `4e48a1ae56a26dda7a7aed13a9f48cdf3077f402df076f23f993238ae3e264f4` |

## Vulnerability and fix verification

Snapshot 时间为 2026-08-11 Asia/Shanghai；联网 advisory response 会变化，空结果不是永久安全证明。

| Check                                   | Result                                                                 | Evidence SHA-256                                                   |
| --------------------------------------- | ---------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Go vulnerability DB                     | updated `2026-07-27T20:14:16Z`                                         | `b53476e80f68bf8f55d1ec61304aa42add7f10b836a1b1809e74e9aaa93015d0` |
| OSV six production modules              | exact-version batch：六项均为空                                        | `e7e00f5759742ba9322ec6db4bd44d79d245d9831a06655a80cdb93e39c27928` |
| OSV upgraded graph-only modules         | `x/mod v0.37.0`、`x/tools v0.47.0` 均为空                              | `a695960dbd857a99b1fa5950f6b69013974aef100c8b9b4c2dbc4fe8c1fc42e9` |
| `govulncheck v1.6.0 -scan=module`       | exit 0；`No vulnerabilities found.`                                    | `34d60d422ca02c29049789c67ccba9e568005ca4a3a40b43dde70b1ffbb8873f` |
| `govulncheck v1.6.0 -scan=symbol ./...` | exit 0；`No vulnerabilities found.`                                    | `34d60d422ca02c29049789c67ccba9e568005ca4a3a40b43dde70b1ffbb8873f` |
| Go advisory `GO-2026-5970`              | affected `< v0.39.0`，fixed `v0.39.0`，reviewed                        | `303b204846025e42c576f7de216aac66f03d5017eb76be78f7bdadd51c3b44ae` |
| Gerrit fix change / tag ancestry        | merged fix `5ae8e578...`；tag ahead 1 / behind 0                       | `c4439f41680e1e48e22e21ee8c0f6d07cc25c02dfae776d23828dcd1d7720853` |
| Upstream fix regression tests           | `unicode/norm` `TestAppend` / `TestAppendString` pass with 30s timeout | `833cccb96663bb3ff593d040254131f3466192512c8a378710548c755f4e8534` |

官方 fix 把 invalid rune 表示改为 size 1 + explicit invalid flag，从而使 `nextComposed` 等路径不会在 invalid
UTF-8 上以 size 0 原地循环。tag 包含修复和相应 illegal-rune regression fixture；这比仅依赖“版本号看起来足够新”
更强，但仍不替代真实服务的 symbol scan 与 adversarial auth fixture。

## API and Go compatibility

- 四项升级的 `go.mod` 都声明最低 Go `1.25.0`；项目固定 Go `1.26.5`，没有 toolchain downgrade 或自动下载。
- Remediated harness 在 Go `1.26.5` 下通过 `go mod verify`、`CGO_ENABLED=0 go build ./...` 和
  `go test ./...`。candidate `go.mod` / `go.sum` SHA-256 分别为
  `c86ec14d25786dfee590897277fcd8e0b5a4984255516751cc0be2df3a83b634` /
  `f65654519c652388e9f315cef1ef299e86fdc964d8d18332c05dbdb51a40c7fb`；replacement count 为 0。
- 把 pgx upstream source 的 `x/text` 要求从 `v0.29.0` 改为 `v0.39.0` 并 tidy 后，pgx 全 package
  `go test -run '^$' ./...` 通过，证明所有 production/test packages 可编译；evidence SHA-256 为
  `7015ddf91cdaedd07b5211292bb9557ac6c72dc3745f7a12a74efc182fc68562`。
- `x/text v0.39.0` 全仓测试通过；`x/sync v0.21.0` 的 `errgroup`、`semaphore`、`singleflight`、`syncmap`
  测试通过；evidence SHA-256 分别为
  `2527ad528ef4ecf851ebbe04983bbbf40c99212c0947dba2d8f231c85c2e5aca` /
  `a1e9b525396e1e81a3b3afa3747c28b0819fac5af7acbc09cc8326ab58056104`。
- 本 review 没有连接 PostgreSQL，因此不声称 SCRAM、pool contention 或真实 query E2E 已通过。实现后必须用本地
  PostgreSQL fixture 覆盖 ASCII / non-ASCII credential、invalid UTF-8 rejection、concurrent acquire/cancel 和
  bounded shutdown；没有这部分证据时只能批准 dependency choice，不能关闭 `G-DATA`。

编译兼容不等于语义完全相同：跨十个 `x/text` minor 可能更新 Unicode/Precis 行为，`x/sync` 的 semaphore 行为也
可能改变 pool 调度边界。当前用途通过 pgx 编译和 upstream unit tests，没有发现 API break；真实 auth/pool 行为仍由
上述集成测试 gate 负责。

## Licenses and notices

- `x/text`、`x/sync`、`x/mod`、`x/tools` 四项均为 BSD-3-Clause；根目录 `LICENSE` 字节相同，SHA-256 为
  `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`。
- 四个 module 根目录都没有 exact `NOTICE` / `NOTICE.*` 文件。代码中的普通 notice、protocol 名称或注释不得误当
  法律 notice。
- Control Plane binary 的 `THIRD_PARTY_NOTICES` / license inventory 必须至少携带实际链接的 `x/text`、
  `x/sync` BSD-3-Clause copyright/license text；若源码、vendor、tool image 或 SBOM 包含 graph-only `x/mod` /
  `x/tools`，也必须按实际分发 closure 纳入。
- 本记录没有直接改写 P0 license evidence。实施时缺失相邻 `go.sum`、SBOM、license inventory 或 notice 都应
  fail closed，不能用本 review 的临时 digest 代替 release artifact evidence。

## Maintainer and release provenance

- canonical VCS 是 Go project 管理的 `https://go.googlesource.com/{text,sync,mod,tools}`；公开 GitHub
  `golang/*` mirror、Go proxy `Origin` 和 canonical tag ref 对四项 commit 一致。
- 四个 tag 都直接指向 commit（lightweight tag）。GitHub mirror 对四个 commit 都报告 `verified=false`、
  `reason=unsigned`；`x/text v0.39.0` 与 `x/sync v0.21.0` 没有可核验的 GitHub Release assets，因而不能声称
  upstream 提供签名 release 或 SLSA provenance。
- Go proxy module zip + `go.mod` checksum 由公开 SumDB 见证，能固定下载 bits 和防止无声漂移；它不等同
  maintainer signature。故 provenance residual risk 评为 **MEDIUM**，在 exact tag/commit + SumDB + proxy zip
  digest + no-replace + CI replay 同时成立时接受。

## Risk register

| Risk                                                                | Severity           | Disposition                                                                                     |
| ------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------- |
| 不固定时 MVS 回落到 vulnerable `x/text v0.29.0`                     | **BLOCKER**        | main module 保留 exact ordinary require；CI assert selected `v0.39.0` 且禁止 `replace`          |
| Unicode/Precis 行为跨多个 minor 变化，可能改变 non-ASCII SCRAM 输入 | HIGH（使用条件）   | 本地 PostgreSQL auth fixture 覆盖 non-ASCII 与 invalid UTF-8；日志不得输出 credential           |
| `x/sync/semaphore` upgrade 影响 puddle pool contention/cancel       | MEDIUM（使用条件） | acquire/cancel/timeout/drain 并发测试；连接、队列与 shutdown 全部有界                           |
| `x/mod` / `x/tools` 被误认为 production 或未来被实际引入            | MEDIUM             | SBOM 按实际 build closure 分类；vendor/tool/import scope 变化即重审                             |
| Lightweight tags、unsigned commits、无 release attestation          | MEDIUM             | 同时固定 canonical commit、proxy Origin、SumDB checksums、zip digest；任一不一致 fail closed    |
| advisory DB、build tags、module graph 或 scanner 变化               | HIGH               | RC 前重放 source + binary scan；保存 DB timestamp、selected graph、sanitised response 与 digest |

## Alternatives

1. **选择 `x/text v0.39.0`（批准）**：它是官方 advisory 指明的首个修复版本，减少相对默认 pgx closure 的无关
   版本漂移；被动选择的 `x/sync v0.21.0`、`x/mod v0.37.0`、`x/tools v0.47.0` 已纳入本记录。
2. **`x/text v0.40.0` / `x/sync v0.22.0`（本轮不选）**：snapshot 时 Go proxy `@latest` 分别为这两个版本，
   但修复并不要求继续升级；它们会引入另一组未在本记录固定的 source/API/graph bits。需要采用时重新独立审查。
3. **等待 pgx 后续版本自行提高 floor（可作为未来收敛）**：能减少 main module 的 remediation pin，但必须重新审查
   新 pgx tag/commit、API、完整 MVS closure 和真实 Control Plane behavior；不能让当前 vulnerable 闭包先落地。
4. **`replace` fork、vendor patch、advisory suppression 或“输入通常有效”（否决）**：这些方案制造第二份修复
   authority、绕过 SumDB/version identity，或保留可从 SCRAM path 到达的 DoS；不与官方 fixed tag 等价。
5. **更换 PostgreSQL driver（不在本轮 scope）**：会改变 pool、native type、transaction、COPY、listen/notify 与
   error semantics；不是解除单一 transitive finding 的最小安全变更。

## Required operating boundary

- `services/control-plane/go.mod` 必须同时 exact pin `pgx v5.10.0` 和 `x/text v0.39.0`；相邻 `go.sum` 必须包含
  tidy/build 实际要求的 checksum，至少固定 production `x/text` / `x/sync` 的 module 与 `go.mod` sums；不允许
  `replace`、fork 或漂移版本；
- `go mod tidy -diff` 必须为空，`go list -m all` 必须选择本记录四个升级版本，`go mod verify` 必须通过；
- dependency/generation lock 记录 main-module `go.mod`、`go.sum`、完整 selected graph、effective production
  closure、四项 proxy/SumDB identity 和 no-replace assertion；Go module graph pruning 未把 graph-only
  `x/mod` / `x/tools` content sum 写入 `go.sum` 时，也不能从 dependency evidence 中省略；只固定 pgx 版本不足以
  代表 Runtime bits；
- 真实 Control Plane source 和最终 binary 都执行 `govulncheck`，RC 前刷新 Go vulnerability DB/OSV snapshot；
- SBOM、license inventory、`THIRD_PARTY_NOTICES` 按真实 build/distribution closure记录四项分类和 checksum；
- PostgreSQL DSN 只来自受信 deployment authority；显式 TLS/auth policy、连接/queue/timeout/drain 上限；不记录
  DSN password、SCRAM material、SQL params 或 tenant secrets；
- 任一 version、Sum/GoModSum、tag target、license、module graph、import/build tags、scanner finding 或 Go toolchain
  变化都会使本 review 失效。

## Exact replay commands

以下命令不读取生产凭据，也不连接 PostgreSQL。identity 命令可从任意权限受限的临时目录执行；service 命令应在
依赖实施后的 repo root 执行：

```bash
set -euo pipefail
umask 077
review_dir=$(mktemp -d /tmp/x-text-v0.39.0-review.XXXXXX)
chmod 700 "$review_dir"
cleanup() {
  case "$review_dir" in
    /tmp/x-text-v0.39.0-review.*) rm -rf -- "$review_dir" ;;
    *) printf 'refusing unsafe cleanup target: %s\n' "$review_dir" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM
cd "$review_dir"

export GOWORK=off
export GOTOOLCHAIN=local
export GOPROXY=https://proxy.golang.org,direct
export GOSUMDB=sum.golang.org
export GOPRIVATE=
export GONOSUMDB=
export GONOPROXY=
export GOMODCACHE="$review_dir/modcache"
export GOCACHE="$review_dir/buildcache"

test "$(go env GOVERSION)" = "go1.26.5"
git ls-remote https://go.googlesource.com/text refs/tags/v0.39.0 'refs/tags/v0.39.0^{}'
git ls-remote https://go.googlesource.com/sync refs/tags/v0.21.0 'refs/tags/v0.21.0^{}'
git ls-remote https://go.googlesource.com/mod refs/tags/v0.37.0 'refs/tags/v0.37.0^{}'
git ls-remote https://go.googlesource.com/tools refs/tags/v0.47.0 'refs/tags/v0.47.0^{}'

go mod download -json golang.org/x/text@v0.39.0
go mod download -json golang.org/x/sync@v0.21.0
go mod download -json golang.org/x/mod@v0.37.0
go mod download -json golang.org/x/tools@v0.47.0
curl -fsSL https://sum.golang.org/lookup/golang.org/x/text@v0.39.0
curl -fsSL https://sum.golang.org/lookup/golang.org/x/sync@v0.21.0
curl -fsSL https://sum.golang.org/lookup/golang.org/x/mod@v0.37.0
curl -fsSL https://sum.golang.org/lookup/golang.org/x/tools@v0.47.0
curl -fsSL https://vuln.go.dev/ID/GO-2026-5970.json
```

在实施后的 repo root：

```bash
set -euo pipefail
export GOWORK=off
export GOTOOLCHAIN=local
export GOPROXY=https://proxy.golang.org,direct
export GOSUMDB=sum.golang.org
export GOPRIVATE=
export GONOSUMDB=
export GONOPROXY=

cd services/control-plane
test "$(go env GOVERSION)" = "go1.26.5"
go mod tidy -diff
go mod verify
go list -m all
go mod graph
go list -deps -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' ./... | sort -u
test "$(go list -m -f '{{.Version}}' golang.org/x/text)" = "v0.39.0"
test "$(go list -m -f '{{.Version}}' golang.org/x/sync)" = "v0.21.0"
test "$(go list -m -f '{{if .Replace}}replace{{end}}' all | sed '/^$/d' | wc -l | tr -d ' ')" = "0"
CGO_ENABLED=0 go build ./...
go test ./...

review_bin=$(mktemp -d /tmp/x-text-govulncheck.XXXXXX)
chmod 700 "$review_bin"
cleanup_review_bin() {
  case "$review_bin" in
    /tmp/x-text-govulncheck.*) rm -rf -- "$review_bin" ;;
    *) printf 'refusing unsafe cleanup target: %s\n' "$review_bin" >&2 ;;
  esac
}
trap cleanup_review_bin EXIT HUP INT TERM
GOBIN="$review_bin" go install golang.org/x/vuln/cmd/govulncheck@v1.6.0
"$review_bin/govulncheck" -scan=module -show=version,traces
"$review_bin/govulncheck" -scan=symbol -show=version,traces ./...
```

最终 binary scan、PostgreSQL auth/pool integration、SBOM/license/notice 与 dependency-lock 命令由相应 P1 gate
脚本执行；本 review 不伪造尚未存在的 durable locator。

## Durable evidence locators

- Upstream fix：[`go.dev/cl/794100`](https://go.dev/cl/794100)、
  [`5ae8e578e495731553eddba11b2d0e86c91a00ce`](https://github.com/golang/text/commit/5ae8e578e495731553eddba11b2d0e86c91a00ce)
- Fixed source tags：[`x/text v0.39.0`](https://github.com/golang/text/tree/v0.39.0)、
  [`x/sync v0.21.0`](https://github.com/golang/sync/tree/v0.21.0)、
  [`x/mod v0.37.0`](https://github.com/golang/mod/tree/v0.37.0)、
  [`x/tools v0.47.0`](https://github.com/golang/tools/tree/v0.47.0)
- Go SumDB：[`x/text@v0.39.0`](https://sum.golang.org/lookup/golang.org/x/text@v0.39.0)、
  [`x/sync@v0.21.0`](https://sum.golang.org/lookup/golang.org/x/sync@v0.21.0)、
  [`x/mod@v0.37.0`](https://sum.golang.org/lookup/golang.org/x/mod@v0.37.0)、
  [`x/tools@v0.47.0`](https://sum.golang.org/lookup/golang.org/x/tools@v0.47.0)
- Vulnerability authority：[`GO-2026-5970`](https://vuln.go.dev/ID/GO-2026-5970.json)
- Repository decision chain：[pgx review](./pgx-v5.10.0.md)、本记录；实施后的长期 authority 是
  `services/control-plane/go.mod`、相邻 `go.sum`、generation/dependency lock、SBOM、license inventory 与
  `THIRD_PARTY_NOTICES`

本次 raw proxy/GitHub/OSV/scanner response 和 harness 位于权限 `0700` 的临时目录，review 后删除；上面的 digest
只识别该 snapshot，不替代仓内 lock 或未来 scanner refresh。每次 RC 必须重新保存 sanitised response、DB
timestamp、selected graph、effective closure 与 digest；不得用 2026-08-11 的空查询证明未来仍无漏洞。
