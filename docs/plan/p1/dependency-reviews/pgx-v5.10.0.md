# P1 dependency review：pgx v5.10.0

- Status：**BLOCKED**
- Blocking finding：`github.com/jackc/pgx/v5 v5.10.0` 的默认生产闭包选择
  `golang.org/x/text v0.29.0`，命中可从 `pgxpool.New` 实际到达的
  [`GO-2026-5970`](https://pkg.go.dev/vuln/GO-2026-5970)
- Scope：Control Plane 对 native `github.com/jackc/pgx/v5` 与
  `github.com/jackc/pgx/v5/pgxpool` 的直接使用；不批准 `database/sql`、`pgx/stdlib`、逻辑复制、代理或其他新增用途
- Accountable owner：hxp0618
- Independent evidence reviewer：Codex P1 supply-chain reviewer（未修改依赖或 Control Plane 实现）
- Review date：2026-08-11 Asia/Shanghai
- Toolchain：Go `1.26.5 darwin/arm64`，`GOWORK=off`，`GOTOOLCHAIN=local`

## Decision

**当前不得把 `pgx v5.10.0` 的默认闭包加入 Control Plane。** 目标模块本身的固定版本 OSV 查询为空，
但这不能代表其生产闭包安全。`pgx v5.10.0` 要求的 `x/text v0.29.0` 受
`GO-2026-5970` / `CVE-2026-56852` 影响：无效 UTF-8 可使 `norm.Iter` 进入无限循环。
`govulncheck v1.6.0` 不仅在 module scan 中报告该问题，还从代表性的 native pool 初始化调用得到真实
symbol trace：

```text
pgxpool.New
  -> pgxpool.NewWithConfig
  -> pgx.ConnectConfig
  -> pgconn.ConnectConfig
  -> PgConn.scramAuth
  -> precis.OpaqueString.String
  -> golang.org/x/text/unicode/norm
```

这条路径属于 PostgreSQL SCRAM 登录，不是未链接的测试代码，也不能因数据库密码“通常是合法 UTF-8”而豁免。
在依赖解析选择已审查的修复版 `x/text >= v0.39.0`，并对随之变化的完整闭包重新审查前，本记录 fail closed。

## Fixed upstream identity

| Evidence                      | Fixed value                                                        |
| ----------------------------- | ------------------------------------------------------------------ |
| Module                        | `github.com/jackc/pgx/v5`                                          |
| Version / tag                 | `v5.10.0` / `refs/tags/v5.10.0`                                    |
| Tag target / source commit    | `7293fb11125be0373a92f716683f2d494f6fd4b0`                         |
| Go proxy publication time     | `2026-06-03T00:41:58Z`                                             |
| Upstream minimum Go           | `go 1.25.0`                                                        |
| Module content sum            | `h1:VhSvgU2jSli8o3AqIEOTJr7rZwAEUVo4E4XhR94Zfr0=`                  |
| `go.mod` sum                  | `h1:mal1tBGAFfLHvZzaYh77YS/eC6IX9OWbRV1QIIM0Jn4=`                  |
| Exact-source `go.mod` SHA-256 | `5bb85082d4aef07908c0ea914a1603ab40454968a95bf569cd89eb0c4df03d74` |
| Exact-source `go.sum` SHA-256 | `e25fc918da29eb2fce9cc6e2d674fe42db9f40008e469f75b3b99e394dd9e3a4` |
| Go proxy module zip SHA-256   | `bfc29f3326851fb4fc12bfa5678ea804e09ef01810948688e49dae3c62d23359` |
| SumDB lookup response SHA-256 | `641dc49d64a19f54e347e85604225dda5f2027d0464cda29a4c996950a0ac00f` |
| Upstream `LICENSE` SHA-256    | `467f95e074fe23079a5623ed652619682692041b8551da27e3c2ddb9659a1507` |

Go proxy `.info` 的 `Origin`、GitHub tag ref 和 `git ls-remote` 都把 `v5.10.0` 绑定到同一 commit；
exact-source 与 proxy `go.mod` 字节一致。SumDB snapshot 中的固定记录是：

```text
github.com/jackc/pgx/v5 v5.10.0 h1:VhSvgU2jSli8o3AqIEOTJr7rZwAEUVo4E4XhR94Zfr0=
github.com/jackc/pgx/v5 v5.10.0/go.mod h1:mal1tBGAFfLHvZzaYh77YS/eC6IX9OWbRV1QIIM0Jn4=
```

这些 identity 证据固定“审查了哪些 bits”，不覆盖下面的漏洞阻塞。

## Direct and transitive graph

在只 import `pgx` / `pgxpool` 的全新 Go `1.26.5` toolchain module（`go` directive `1.26.0`）中执行
`go mod tidy`，默认生产 build closure
为下表六个第三方 module。`Sum` / `GoModSum` 均由公开 Go proxy + SumDB 重放，没有 private proxy 或
`replace`：

| Module                           | Selected version                     | Edge / production use                                                          | Module sum                                        | `go.mod` sum                                      | License / license SHA-256                                                         |
| -------------------------------- | ------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------- | ------------------------------------------------- | --------------------------------------------------------------------------------- |
| `github.com/jackc/pgx/v5`        | `v5.10.0`                            | Control Plane direct；`pgx`, `pgxpool`, `pgconn`, `pgtype`, protocol internals | `h1:VhSvgU2jSli8o3AqIEOTJr7rZwAEUVo4E4XhR94Zfr0=` | `h1:mal1tBGAFfLHvZzaYh77YS/eC6IX9OWbRV1QIIM0Jn4=` | MIT / `467f95e074fe23079a5623ed652619682692041b8551da27e3c2ddb9659a1507`          |
| `github.com/jackc/pgpassfile`    | `v1.0.0`                             | `pgx -> pgpassfile`                                                            | `h1:/6Hmqy13Ss2zCq62VdNG8tM1wchn8zjSGOBJ6icpsIM=` | `h1:CEx0iS5ambNFdcRtxPj5JhEz+xB6uRky5eyVu/W2HEg=` | MIT / `adb1663fda031df8f4344aa68f299fd87d80353e31339406742ded21dae65702`          |
| `github.com/jackc/pgservicefile` | `v0.0.0-20240606120523-5a60cdf6a761` | `pgx -> pgservicefile`                                                         | `h1:iCEnooe7UlwOQYpKFhBabPMi4aNAfoODPEFNiAnClxo=` | `h1:5TJZWKEWniPve33vlWYSoGYefn3gLQRzjfDlhSJ9ZKM=` | MIT / `fc505773403fe869ed64cc2235cdd13988a427bb7e3a7e7004a3f4b27420f8fc`          |
| `github.com/jackc/puddle/v2`     | `v2.2.2`                             | `pgxpool -> puddle`                                                            | `h1:PR8nw+E/1w0GLuRFSmiioY6UooMp6KJv0/61nB7icHo=` | `h1:vriiEXHvEE654aYKXXjOvZM39qJ0q+azkZFrfEOc3H4=` | MIT / `2d50e98a4900b4d6457a38d39c1432fdc156fc2f7b365f2e33ec9344acbb0057`          |
| `golang.org/x/sync`              | `v0.17.0`                            | `pgx`, `puddle`；MVS 选择 `v0.17.0`                                            | `h1:l60nONMj9l5drqw6jlhIELNv9I0A4OFgRsG9k2oT9Ug=` | `h1:9KTHXmSnoGruLpwFjVSX0lNNA75CykiMECbovNTZqGI=` | BSD-3-Clause / `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |
| `golang.org/x/text`              | `v0.29.0`                            | `pgx -> precis -> unicode/norm`                                                | `h1:1neNs90w9YzJ9BocxfsQNHKuAT4pkghyXc4nhZ6sJvk=` | `h1:7MhJOA9CD2qZyOKYazxdYMF85OwPdEr9jTtBpO7ydH4=` | BSD-3-Clause / `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad` |

`go list -deps` 在该 harness 中解析到 203 个 package；实际第三方 package 都来自上表六个 module。
目标平台的有效 closure 不含 Cgo file，可用 `CGO_ENABLED=0` 构建。

完整 **module graph** 还选择以下仅由上游 test/tool `go.mod` edge 带入、未链接到本次 Control Plane binary 的
module：`davecgh/go-spew@v1.1.1`、`kr/pretty@v0.3.0`、`pmezard/go-difflib@v1.0.0`、
`stretchr/objx@v0.1.0`、`stretchr/testify@v1.11.1`、`golang.org/x/mod@v0.27.0`、
`golang.org/x/tools@v0.36.0`、`gopkg.in/check.v1@v1.0.0-20201130134442-10cb98267c6c` 和
`gopkg.in/yaml.v3@v3.0.1`。它们不能被误报为已链接的 runtime closure；但若未来 vendor 整个 module graph、
运行上游测试或把其中任一包引入生产，则必须纳入新的 license/vulnerability closure。

Default-harness evidence digests：

- `go.mod` SHA-256：`1d1efe79658577db432a28843d0f52a4790e47dc897b50aeca947f6cdddc0857`
- `go.sum` SHA-256：`10f85f016b4bb4919ddc6453cc8c09962bc186a29d39ed064332a23ed5a58115`
- effective-module list SHA-256：`4921c352137cf68cd25ec093c8a535d8e6bbe09f771a0bd875d8c232a3de621a`
- complete selected-module list SHA-256：`74d4f5162cb85b7ec25f53d1b3a85a9709b1b3f78b07f8167fc8542fb1d0b48a`
- `go mod graph` SHA-256：`ed87248e296e3ce236305b73f871a8acefdf8eb945084f8739b847e452eb6434`
- `go mod verify` output SHA-256：`b4537ed75f533f993f371954de47e42a793b8e5b0587577de7e27fb3e50696bd`

上述 harness 文件位于权限 `0700` 的临时目录，review 后删除；digest 仅用于识别本次 snapshot，不替代仓内
`go.mod` / `go.sum` 和下列重放命令。

## Licenses and notices

- `pgx`、`pgpassfile`、`pgservicefile`、`puddle` 为 MIT；`x/sync`、`x/text` 为 BSD-3-Clause。
- 六个有效 production module 根目录均有 `LICENSE`；在精确 `NOTICE` / `NOTICE.*` 文件名下未发现额外 notice。
  `pgproto3/notice_response.go` 是 PostgreSQL protocol source file，不是法律 notice。
- MIT 与 BSD-3-Clause 均允许当前预期的源码/二进制分发，但 binary distribution 必须在仓内
  `THIRD_PARTY_NOTICES`/license inventory 中携带实际链接 closure 的版权和许可证文本；只写 SPDX 名称不够。
- 当前依赖尚未进入 Control Plane，所以本审查没有改写现有 notice。解除漏洞阻塞并正式加依赖时，缺失 notice
  或 license inventory 同样应 fail closed。

## Vulnerability snapshot

Snapshot 时间不是永久安全证明：

| Check                             | Snapshot / result                                                 | Evidence SHA-256                                                                                                                                   |
| --------------------------------- | ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| OSV exact query：`pgx v5.10.0`    | `2026-08-11 00:36:40 +08:00`；目标 module 直接结果为空            | response `{}`：`44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a`                                                                  |
| OSV six-module batch              | 仅 `x/text v0.29.0` 返回 `GO-2026-5970`                           | `66df5b033f02f6d7b2c5d0f528b61360757be776b985569b49e77ca3b4ba057f`                                                                                 |
| OSV advisory detail               | `GO-2026-5970`, alias `CVE-2026-56852`；fixed `v0.39.0`；reviewed | `47a146d78f2919274b211fdcc9168f90b175f01312b54d91c8a674a3987ab540`                                                                                 |
| Go vulnerability DB               | DB updated `2026-07-27T20:14:16Z`                                 | module-scan JSON：`439d2b3b98f64e0c51d808bcc8eaeb75118f9c7136191151e06aa4a00a67846a`                                                               |
| `govulncheck v1.6.0 -scan=module` | exit `3`；1 affected module                                       | text：`5d6bb72648a18718e7f05ca6417b6de4669474b39c27e4ccccab64b2873e1428`                                                                           |
| `govulncheck v1.6.0 -scan=symbol` | exit `3`；`pgxpool.New` 到 affected symbol 的 trace 可达          | JSON：`dc9536a376af296acda8a63b62d40db7afb40c3807bafbb8bd98f99decac7401`；text：`8cfae0529d3273c4863467d247b68fe3c66ca88eb276c1a4d6e3e82a39230281` |

`GO-2026-5970` 影响 `golang.org/x/text/unicode/norm` 多个 `Form`/`Iter` symbol，修复版本为
`v0.39.0`。即使 OSV 对 **pgx module 本身** 的查询为空，也不得忽略 closure 结果。

## Maintainer and release provenance

- GitHub authority 是公开、未 archived 的 [`jackc/pgx`](https://github.com/jackc/pgx)，owner 为 `jackc`，
  license metadata 为 MIT，default branch 为 `master`。
- [`7293fb11125be0373a92f716683f2d494f6fd4b0`](https://github.com/jackc/pgx/commit/7293fb11125be0373a92f716683f2d494f6fd4b0)
  的 author/committer 都记录为 Jack Christensen，时间分别为 `2026-06-03T00:41:34Z` / `00:41:58Z`。
- `refs/tags/v5.10.0` 的 Git object type 是 `commit`：这是 **lightweight tag**，不是 signed annotated tag；
  GitHub commit verification 是 `verified=false`, reason `unsigned`。
- GitHub `releases/tags/v5.10.0` 在 snapshot 时返回 `404`，因此没有可核验的 GitHub Release、asset digest、
  signature 或 release attestation。不能声称 upstream 提供 SLSA provenance。
- Go proxy 的 `@latest` 在 snapshot 时仍是 `v5.10.0`，其 `Origin.Hash` 与 tag target 相同；Go SumDB 提供了
  append-only module/go.mod checksum witness。这能防止无声 bits 漂移，但不能补成 maintainer signature，也不能
  消除 transitive vulnerability。
- `CHANGELOG.md` 确认 `5.10.0` 日期为 2026-06-03，并列出 malicious-server decoder bounds、SCRAM iteration
  cap、TLS cancellation 和 `require_auth` 等 hardening；这些是功能背景，不是安全证明。

因此 provenance 风险评为 **MEDIUM**：若漏洞闭包清除，可在 exact version + SumDB + commit + zip digest 全部
固定且 CI 禁止 `GONOSUMDB`/`replace` 的边界内接受；单靠可移动的 Git tag 不可接受。

## Risk register

| Risk                                                                         | Severity           | Disposition                                                                                                      |
| ---------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------- |
| `x/text v0.29.0` 的 `GO-2026-5970` 可从 SCRAM auth path 到达并造成无限循环   | **BLOCKER**        | 不接受 suppression；选择 `x/text >= v0.39.0` 或含等价修复的后续 pgx closure 后重审                               |
| Lightweight tag、unsigned commit、无 GitHub Release/attestation              | MEDIUM             | 同时固定 exact commit、proxy Origin、module/`go.mod` SumDB checksum 与 proxy zip SHA-256；任一不一致 fail closed |
| DSN 环境变量、宽松 TLS/auth 或租户可控 config 扩大 credential/downgrade 风险 | HIGH（使用条件）   | 配置仅来自受信 deployment authority，收窄 allowed keys，显式 TLS 与 `require_auth`                               |
| Pool/message/cache 默认值可能放大连接、内存与 shutdown 风险                  | MEDIUM（使用条件） | Control Plane 设置有界连接数、timeout、message/cache 和 drain，并用故障测试验证                                  |
| MVS、build tags、import scope 或 advisory DB 变化导致审查闭包漂移            | HIGH               | generation/dependency lock、`go mod verify`、source + binary vuln scan；变化即使本记录失效                       |

## Remediation experiment and alternatives

### Candidate remediation（尚未批准）

临时 harness 保持 `pgx v5.10.0`，额外 exact require `golang.org/x/text v0.39.0` 后：

- Go MVS 同时把 `x/sync` 从 `v0.17.0` 提升到 `v0.21.0`；
- `go mod tidy`、`go build ./...`、`go test ./...` 通过；
- 同一个 `govulncheck v1.6.0 -scan=symbol` 返回 0，结果为 `No vulnerabilities found.`；
- fixed scan text SHA-256：`34d60d422ca02c29049789c67ccba9e568005ca4a3a40b43dde70b1ffbb8873f`；
- `x/text v0.39.0` exact `go.mod` SHA-256：`40e9425e17dcc56faf496619fde6908631d57b2cce0f766c4dca6bea8fc93838`。

这只是最小可行性证据，**不是批准**。`x/text v0.39.0`、被提升的 `x/sync v0.21.0`、新的 `go.sum`、
license/vulnerability closure 和真实 Control Plane source trace 都必须单独复核。不得把 vulnerability suppression、
输入“理论上有效”、`replace` fork 或 vendor patch 当作等价修复。

### Other alternatives

1. 等待或采用后续 pgx patch/minor，使其上游 `go.mod` 自身选择已修复 `x/text`；仍需重新固定 tag/commit、
   module sum、API compatibility 和完整 closure。snapshot 时 Go proxy `@latest` 仍是 `v5.10.0`。
2. `database/sql` + `pgx/stdlib` 不解决问题：它仍使用同一个 pgx module/auth path，而且超出本审批用途。
3. 更换 PostgreSQL driver 会改变 native types、pool、transaction、COPY/listen-notify 与错误语义；没有现成的
   等价且已审查候选，本轮不批准。
4. 手写 PostgreSQL wire/pool 或维护私有 fork 会制造第二份协议和补丁 authority，provenance 与长期维护风险更高，
   本轮否决。

## Required operating boundary after unblock

即使未来把本记录更新为 APPROVED，也必须同时满足：

- Control Plane 只直接 import `pgx` / `pgxpool`；新增 `stdlib`、replication、proxy/tool package 必须重审；
- `services/control-plane/go.mod` 与 `go.sum` 使用 exact versions，不允许 `replace`、未审查 fork、branch 或 pseudo
  version 漂移；CI 使用公开 SumDB 并执行 `go mod verify`；
- DSN/config 只来自受信 deployment config，不接受租户或请求直接提交；使用 `ConnStringAllowedKeys` 收窄允许键；
- 明确 TLS mode、CA/hostname verification 与 `require_auth`，生产不得依赖 `sslmode=prefer` 或无约束 auth downgrade；
- pool 的 max/min conns、acquire/health/lifetime timeout、statement/cache、message/body 与 shutdown drain 必须有界；
- error/log/trace 不输出 DSN password、token、SQL 参数或租户 secret；
- 在真实 Control Plane source 上运行 `govulncheck -scan=symbol ./...`，并对 binary 再运行一次 scan；
- RC/SBOM/license inventory/THIRD_PARTY_NOTICES 记录最终 selected closure 与 checksum；
- 任何 version、Sum/GoModSum、tag target、license、module graph、import scope、scanner DB finding 或 build tags 变化
  都使本 review 失效。

本记录不批准数据库 migration、生产连接、deployment，也不关闭 `G-DATA`、`G-SUPPLY-CHAIN`、P1 Exit 或
任何 release Gate。

## Exact replay commands

以下命令从任意安全目录执行；它们创建权限受限的临时 module，不读生产凭据，也不连接 PostgreSQL：

```bash
set -euo pipefail
umask 077
review_dir=$(mktemp -d /tmp/pgx-v5.10.0-review.XXXXXX)
chmod 700 "$review_dir"
cleanup() {
  case "$review_dir" in
    /tmp/pgx-v5.10.0-review.*) rm -rf -- "$review_dir" ;;
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
go env GOOS GOARCH
git ls-remote https://github.com/jackc/pgx.git \
  refs/tags/v5.10.0 'refs/tags/v5.10.0^{}'
curl -fsSL https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.10.0.info -o proxy.info
curl -fsSL https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.10.0.mod -o proxy.go.mod
curl -fsSL https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.10.0.zip -o pgx.zip
curl -fsSL https://sum.golang.org/lookup/github.com/jackc/pgx/v5@v5.10.0 -o sumdb.lookup
curl -fsSL \
  https://raw.githubusercontent.com/jackc/pgx/7293fb11125be0373a92f716683f2d494f6fd4b0/go.mod \
  -o upstream.go.mod
curl -fsSL \
  https://raw.githubusercontent.com/jackc/pgx/7293fb11125be0373a92f716683f2d494f6fd4b0/go.sum \
  -o upstream.go.sum
cmp proxy.go.mod upstream.go.mod
shasum -a 256 proxy.info proxy.go.mod upstream.go.mod upstream.go.sum pgx.zip sumdb.lookup

go mod init review.local/pgx
go get github.com/jackc/pgx/v5@v5.10.0
tee main.go >/dev/null <<'GO'
package main

import (
    "context"

    "github.com/jackc/pgx/v5/pgxpool"
)

func main() {
    pool, err := pgxpool.New(
        context.Background(),
        "postgres://review:review@127.0.0.1/review",
    )
    if err == nil {
        pool.Close()
    }
}
GO
go mod tidy
go mod download -json github.com/jackc/pgx/v5@v5.10.0
go mod verify
go list -deps -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}' . | sort -u
go list -m all
go mod graph
CGO_ENABLED=0 go build ./...
go test ./...

mkdir -p "$review_dir/bin"
GOBIN="$review_dir/bin" go install golang.org/x/vuln/cmd/govulncheck@v1.6.0
go version -m "$review_dir/bin/govulncheck"
set +e
"$review_dir/bin/govulncheck" -scan=module -format=json > govuln-module.json
"$review_dir/bin/govulncheck" -scan=module -format=text > govuln-module.txt
module_status=$?
"$review_dir/bin/govulncheck" -scan=symbol -format=json . > govuln-symbol.json
"$review_dir/bin/govulncheck" -scan=symbol -format=text . > govuln-symbol.txt
symbol_status=$?
set -e
test "$module_status" -eq 3
test "$symbol_status" -eq 3
rg 'GO-2026-5970' govuln-module.json govuln-module.txt govuln-symbol.json govuln-symbol.txt

tee osv-batch.json >/dev/null <<'JSON'
{"queries":[
  {"package":{"ecosystem":"Go","name":"github.com/jackc/pgx/v5"},"version":"v5.10.0"},
  {"package":{"ecosystem":"Go","name":"github.com/jackc/pgpassfile"},"version":"v1.0.0"},
  {"package":{"ecosystem":"Go","name":"github.com/jackc/pgservicefile"},"version":"v0.0.0-20240606120523-5a60cdf6a761"},
  {"package":{"ecosystem":"Go","name":"github.com/jackc/puddle/v2"},"version":"v2.2.2"},
  {"package":{"ecosystem":"Go","name":"golang.org/x/sync"},"version":"v0.17.0"},
  {"package":{"ecosystem":"Go","name":"golang.org/x/text"},"version":"v0.29.0"}
]}
JSON
curl -fsS -H 'Content-Type: application/json' \
  --data-binary @osv-batch.json https://api.osv.dev/v1/querybatch -o osv-result.json
curl -fsSL https://api.osv.dev/v1/vulns/GO-2026-5970 -o osv-GO-2026-5970.json
curl -fsSL https://vuln.go.dev/ID/GO-2026-5970.json -o go-GO-2026-5970.json
shasum -a 256 go.mod go.sum govuln-module.json govuln-module.txt \
  govuln-symbol.json govuln-symbol.txt \
  osv-result.json osv-GO-2026-5970.json go-GO-2026-5970.json
```

临时 remediation 只能作为候选验证，不能改变本记录的 BLOCKED 状态：

```bash
go get golang.org/x/text@v0.39.0
go mod tidy
go list -m all
go build ./...
go test ./...
"$review_dir/bin/govulncheck" -scan=symbol -format=text -show=version,traces .
```

## Durable evidence locators

- Upstream commit：[`7293fb11125be0373a92f716683f2d494f6fd4b0`](https://github.com/jackc/pgx/commit/7293fb11125be0373a92f716683f2d494f6fd4b0)
- Go proxy metadata：[`v5.10.0.info`](https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.10.0.info)、
  [`v5.10.0.mod`](https://proxy.golang.org/github.com/jackc/pgx/v5/@v/v5.10.0.mod)
- Go SumDB lookup：[`github.com/jackc/pgx/v5@v5.10.0`](https://sum.golang.org/lookup/github.com/jackc/pgx/v5@v5.10.0)
- Go vulnerability record：[`GO-2026-5970`](https://vuln.go.dev/ID/GO-2026-5970.json)
- OSV vulnerability record：[`GO-2026-5970`](https://api.osv.dev/v1/vulns/GO-2026-5970)
- 仓内长期 authority：正式实施后的 `services/control-plane/go.mod`、相邻 `go.sum`、generation/dependency lock、
  SBOM、license inventory 与 `THIRD_PARTY_NOTICES`。这些文件当前尚未固定 pgx，因此不能用本 review 的临时
  harness digest 替代。

联网 API response、advisory database 与 GitHub metadata 都会变化。每次解除阻塞或发 RC 前必须保存新的
sanitised snapshot、scanner/database timestamp、selected graph 和 digest；旧的空 direct-query 不能证明未来安全。
