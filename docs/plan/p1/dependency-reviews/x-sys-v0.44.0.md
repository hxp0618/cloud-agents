# P1 dependency review：golang.org/x/sys v0.44.0

- Status：**APPROVED_FOR_IMPLEMENTATION（conditional；尚未引入）**
- Scope：impl-3 filesystem slice 计划使用的 `golang.org/x/sys/unix` Linux syscall/constants surface
- Reviewed version：`golang.org/x/sys v0.44.0`
- Prohibited：`replace`、fork、vendor patch、floating branch/pseudo-version、空白 import、Darwin production fallback
- Accountable owner：hxp0618
- Evidence owner：Codex P1 supply-chain worker
- Review snapshot：2026-08-12T03:18:56Z
- Review toolchain：Go `1.26.5 darwin/arm64`，`GOWORK=off`，`GOTOOLCHAIN=local`
- Source boundary：review 从 clean exact HEAD
  `2fe6995de52c8b27046573c5c69f61dfcfda4d0c` 开始；结束前出现其他 worker 拥有的 untracked Go source，
  本记录没有读取、修改或把它们纳入 dependency/source closure

## Decision

**条件批准 `golang.org/x/sys v0.44.0` 作为 impl-3 Linux filesystem runtime 的 ordinary exact direct
dependency。** 只有 production filesystem source 实际 import `golang.org/x/sys/unix` 时，实施者才可在同一受审
slice 中加入 exact `require golang.org/x/sys v0.44.0`，且必须同时刷新 `go.sum`、dependency lock、CycloneDX SBOM、
license/PATENTS notice 与 source-bound closure evidence。不得提前增加未被 source 使用、会被 `go mod tidy` 删除的
graph edge，也不得用空白 import 伪装 source-direct dependency。

当前 committed Control Plane graph **不包含** `x/sys`：它不是 selected module，不是 source-imported package，
不是 production-linked component，也不是当前 distribution 或 notice scope。因而本 review 没有修改：

| Current artifact                                | SHA-256（与 HEAD same-bits）                                       |
| ----------------------------------------------- | ------------------------------------------------------------------ |
| `services/control-plane/go.mod`                 | `3eaa75348e57ef4ea72e1fff572058dd496acb877be543600e5b1a57ed93de90` |
| `services/control-plane/go.sum`                 | `f65654519c652388e9f315cef1ef299e86fdc964d8d18332c05dbdb51a40c7fb` |
| `services/control-plane/dependency-lock.json`   | `4bae3bfce955cdc1df34235b64308eb8bb6b411dacb65b56144039b143aeef7b` |
| `services/control-plane/sbom.cdx.json`          | `d8ff0182bebbc3a4cedcba6534a2c2a7747ccbd642bcc0238251857d360b6fe5` |
| `services/control-plane/THIRD_PARTY_NOTICES.md` | `5fa0f0ebf9c81ea7b9d15714799c0c6b2b3f5be65a52e3eee0b0b96f450d0f5a` |

上述批准只关闭 impl-3 的 **dependency choice review** Entry；它不证明 runtime source、filesystem probe、锁、
no-replace publish、crash/power-loss durability、SBOM 集成、最终 binary same-bits 或任何 RC/release Gate 已完成。

## Fixed upstream identity

| Evidence                      | Exact value                                                        |
| ----------------------------- | ------------------------------------------------------------------ |
| Module/version                | `golang.org/x/sys v0.44.0`                                         |
| Canonical tag commit          | `fb1facd76f95fa87c151018200ea5e4892ff115d`                         |
| Tag time                      | `2026-04-23T15:37:02Z`                                             |
| Minimum Go                    | `1.25.0`                                                           |
| Module dependencies           | none (`go.mod` contains only module path and Go directive)         |
| Module sum                    | `h1:ildZl3J4uzeKP07r2F++Op7E9B29JRUy+a27EibtBTQ=`                  |
| `go.mod` sum                  | `h1:4GL1E5IUh+htKOUEOaiffhrAeqysfVGipDYzABqnCmw=`                  |
| Proxy zip SHA-256             | `f1fa1052808e6bd6eb9c5372c053b2370a582532fac5d6a4600e7a6fab190ff3` |
| Proxy `go.mod` SHA-256        | `57f4393ea18d5446a12363b35c23a616d843fa1669c7121a70a2bc3a9677d665` |
| Proxy `.info` SHA-256         | `db88d97c963506d830212c91e42f4bbd9076c18faccb127b6c096bb86bab0ae6` |
| SumDB lookup response SHA-256 | `86d37dd2005bce9919d3893114fe50fbeeb751898ff9009a5871ccf320c00337` |

`go mod download -json` 的 `Origin.URL`/`Origin.Hash`、`proxy.golang.org` `.info`、canonical
`go.googlesource.com/sys` tag ref 与 GitHub mirror tag ref 均解析到上述 commit。transport mirror 不是 bits
authority；实施 lock 必须同时固定 exact version、module `h1`、proxy zip digest 和公开 SumDB witness。SumDB lookup
包含时点 tree head，因此 response digest 是 non-bit-safe snapshot，不能替代 stable module identity。

## License and additional IP rights

- License：BSD-3-Clause；root `LICENSE` SHA-256
  `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`。
- Additional grant：root `PATENTS`，标题 `Additional IP Rights Grant (Patents)`，SHA-256
  `96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc`。
- `PATENTS` 包含 Google 的 patent license 及 patent-litigation termination 条款；不能仅记录 SPDX
  `BSD-3-Clause` 而遗漏该文本。

当前 x/sys 未被链接或分发，所以现有 production-linked-only `THIRD_PARTY_NOTICES.md` 保持不变是准确分类。
当 filesystem source import 落地后，x/sys 将成为 production-linked/direct component；届时 notice 必须明确把已共享
展示的相同 `PATENTS` 文本也独立适用于 x/sys，dependency lock 与 SBOM component 必须分别记录 license/PATENTS
path、digest 和 distribution scope。共享展示可去重文本，不能合并法律适用关系。

## Reviewed syscall surface

本 review 只批准 ADR 0010 impl-3 所需的窄 Linux surface，不构成对整个 `x/sys` module 的开放授权：

| Required behavior                             | Reviewed `unix` API/constants                                           | Boundary                                                                                                      |
| --------------------------------------------- | ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| descriptor-relative no-follow open/create     | `Openat`；`O_CLOEXEC`、`O_NOFOLLOW`、`O_DIRECTORY`、`O_CREAT`、`O_EXCL` | 每个 path component 仍需逐级验证；一个末尾 `O_NOFOLLOW` 不能证明整个 path 无 symlink                          |
| stronger component resolution where available | `Openat2` / `OpenHow`                                                   | Linux/kernel dependent；`ENOSYS`/unsupported flags 必须 fail closed 或走已审等价 component-walk，不得静默降级 |
| owner/mode/type/link checks                   | `Fstatat`、`Fstat`、`AT_SYMLINK_NOFOLLOW`                               | 检查与使用必须保持 descriptor authority，避免 pathname TOCTOU                                                 |
| filesystem allowlist probe                    | `Fstatfs`、`Statfs_t.Type`、`EXT4_SUPER_MAGIC`、`XFS_SUPER_MAGIC`       | magic 只证明报告类型；仍需验证 mount source/options 并做在线语义 probe                                        |
| cross-process nonblocking writer lock         | `Flock`；`LOCK_EX`、`LOCK_NB`、`LOCK_UN`                                | 必须两进程验证；不得用进程内 mutex 替代，也不得假设所有 mount 的 flock 语义相同                               |
| bounded write at offset                       | `Write` / `Pwrite`                                                      | short write、`EINTR`、partial mutation 和 checked offset overflow 必须显式处理                                |
| file durability                               | `Fdatasync`                                                             | 成功只覆盖该 file 的承诺边界；不能替代新 directory entry 的 parent `Fsync`                                    |
| directory-entry durability                    | `Fsync` on verified directory fd                                        | unsupported/error 必须 fail closed；`Close` 不是 durability barrier                                           |
| atomic no-replace publish                     | `Renameat2(..., RENAME_NOREPLACE)`                                      | `ENOSYS`/unsupported filesystem 时只能使用已审同目录 `Linkat` + `Unlinkat` 等价协议；普通可覆盖 rename 禁止   |
| no-replace fallback/cleanup                   | `Linkat`、`Unlinkat`                                                    | 必须在同一 verified filesystem/directory，处理 crash window、existing target 和 unlink failure                |

`v0.44.0` 的 Linux generated wrappers/constants 对 amd64 与 arm64 均包含该 surface。API 的存在只证明可以表达
协议，不证明目标 kernel、seccomp profile、mount 或 storage controller 支持相同语义。

## CGO=0 API fixture

权限 `0700` 的独立临时 module 以唯一 dependency `golang.org/x/sys v0.44.0` 编译以下行为：`Openat` no-follow
exclusive create、`Pwrite`、`Fdatasync`、`Fstatat`、`Fstatfs`、nonblocking `Flock`、
`Renameat2(RENAME_NOREPLACE)`、`Linkat`/`Unlinkat` fallback、ext4/xfs magic 与 directory `Fsync`。Darwin 文件只
验证通用 `unix` import/build seam，不授权 production Open。

| Target         | Command shape                                          | Result |
| -------------- | ------------------------------------------------------ | ------ |
| `darwin/arm64` | `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go test ./...` | PASS   |
| `linux/amd64`  | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test ./...`  | PASS   |
| `linux/arm64`  | `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test ./...`  | PASS   |

Fixture identity：Linux source SHA-256
`cc20e56943806026e9fa5e9c597222005aff40b25f0f6b6054779db3cea345d2`，Darwin source SHA-256
`926a99dd69439c05b1e3a64eb4f70e015dd6e8705bffa3c334fcec4e05e869fd`。fixture 位于 review 临时目录，
不属于 repository source、SBOM 或 production closure；cross-compile success 也不是 syscall runtime/fault evidence。

## Vulnerability snapshot

漏洞结果是联网时点证据，不是永久安全证明：

| Check                                                       | Snapshot/result                                                                         | Evidence SHA-256                                                                                   |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Go vulnerability DB module index                            | `x/sys` 有 `GO-2022-0493`（早已修复）和 `GO-2026-5024`（first fixed `v0.44.0`）         | exact selected module-index row `0644e1e476122df4cbdcb2bebc7b739fa14c19f838e16558541ad0bd0cdd5e73` |
| `GO-2026-5024` official record                              | reviewed；影响 `x/sys/windows.NewNTUnicodeString`；first fixed `v0.44.0`                | `ec93b4e06338823aff63cebe8aa3bad0d46bc68e16fe226cffaed3846af8b95f`                                 |
| OSV exact query                                             | ecosystem `Go`、module `golang.org/x/sys`、version `v0.44.0`；response `{}`，0 findings | `44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a`                                 |
| `govulncheck v1.6.0 -scan=module` on isolated fixture       | DB updated `2026-08-11T23:21:33Z`；`No vulnerabilities found.`                          | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                 |
| `govulncheck v1.6.0 -scan=symbol ./...` on isolated fixture | same DB；`No vulnerabilities found.`                                                    | `3016e51e4eac0d421674d2128bbbdefb2924b4646e0c14a1ab034977ad73fae5`                                 |

`GO-2026-5024` 是 Windows package issue，而计划 surface 是 Linux `unix`；仍选择首个 fixed exact version，避免把
OS/build-tag 边界当成 suppression。实施时必须在真实 Control Plane source、最终 Linux binaries 和更新后的完整
selected graph 上刷新 module/symbol/OSV evidence；不得继承本临时 fixture 的 0 findings 作为 RC 证明。

## Alternatives considered

| Alternative                                            | Decision | Reason                                                                                                                        |
| ------------------------------------------------------ | -------- | ----------------------------------------------------------------------------------------------------------------------------- |
| standard `os` / `path/filepath` only                   | reject   | 无法完整表达 descriptor-relative component walk、`renameat2(RENAME_NOREPLACE)`、`fstatfs` magic 与明确 fd durability barriers |
| standard `syscall`                                     | reject   | frozen/deprecated surface，缺少或不稳定覆盖所需现代 Linux wrappers/constants；自行 raw syscall 更易产生 arch/ABI 错误         |
| cgo + libc                                             | reject   | 无必要地引入 C toolchain、libc/ABI、cross-build 与 provenance closure；本 surface 已用 CGO=0 验证可编译                       |
| hand-written `RawSyscall` numbers                      | reject   | 重复 x/sys 的 per-arch ABI work，增加 unsafe pointer/lifetime、errno 与 syscall-number drift 风险                             |
| `x/sys` latest/floating branch                         | reject   | 不可复现；批准只适用于 exact `v0.44.0` bits                                                                                   |
| older `x/sys`                                          | reject   | `v0.44.0` 是 `GO-2026-5024` first fixed version；无理由选择已知受影响 floor                                                   |
| silently fall back to ordinary rename/best effort sync | reject   | 会破坏 no-replace 与 durable-append协议；应返回 stable fail-closed error                                                      |

## Runtime risks and required negative evidence

dependency/API 批准不等于 filesystem correctness。impl-3 必须至少关闭以下风险：

1. **`ENOSYS` / `EOPNOTSUPP` / `EINVAL` 不等于可忽略。** `Openat2`、`renameat2`、directory `fsync`、lock 或
   mount semantics 不可用时，只有 ADR 已允许且经 fault test 的等价协议可继续；否则在 DB connect 前 fail closed。
2. **no-follow 不是单 syscall 结论。** 必须 descriptor-relative 逐 component walk，验证 owner/mode/type/link-count，
   并在 check/use 期间保持 fd authority；不得先 `Lstat` 再按原字符串 reopen。
3. **filesystem magic 不足以证明 durable semantics。** ext4/xfs allowlist 还需 mount source/options、bind/overlay
   detection、required syscall probe，以及真实 local mount 的 power-loss/restart matrix。
4. **写入结果可能 unknown。** short/partial write、`EINTR`、`fdatasync`/directory `fsync` error、response loss 均需
   遵守 ADR 的 cursor invalidation/replay contract，不能把 retry 当作未写入。
5. **no-replace fallback 有 crash window。** link 成功而 unlink 失败时 final 已存在；replay 必须 full verify final，
   temp cleanup 只能在锁、owner/type/link-count 与 active-writer证明后执行。
6. **锁和 quota 是跨进程语义。** 必须验证 `Flock` nonblocking contention、root→lineage lock order、两进程
   no-overcommit；不得用单进程测试或 race-clean 代替。
7. **整数与资源边界。** fd、offset、length、record/byte quota 的转换和加法必须 checked；exact inclusive maxima
   可接受，溢出或超过上限在 mutation/DB 前拒绝。
8. **平台边界。** Darwin fixture 只验证 build seam。production implementation 必须由 Linux build tags 隔离，
   non-Linux production Open 返回稳定 fail-closed error，不能在 APFS 上声称 ext4/xfs durability。

## Implementation acceptance checklist

当 filesystem source 实际落地时，必须在一个可审计 slice 内全部满足：

- production source 直接 import `golang.org/x/sys/unix`；`go.mod` 以 ordinary exact direct require 固定
  `v0.44.0`，`go mod tidy -diff` 为空；
- `go.sum` 含本记录的两个 exact `h1`，`go mod verify` 通过；无 `replace`、`exclude`、fork 或 vendor patch；
- `go list -m all`、`go mod graph` 和 non-test `go list -deps ./...` 明确把 x/sys 分类为 selected、direct、
  production-linked，且 production effective package 只按真实 imports记录；
- `dependency-lock.json` 固定 tag/proxy/SumDB/module/license/PATENTS evidence，`sbom.cdx.json` root dependency
  关系与真实 production closure一致；
- `THIRD_PARTY_NOTICES.md` 加入 BSD-3-Clause 并明确共享 PATENTS grant 独立适用于 x/sys；
- 刷新 source commit/tree、tracked/Go-source manifests、module/package closure digests；候选未 commit 时必须
  `source_tree_committed=false`，集成 commit 后再二阶段刷新为 exact committed evidence；
- 真实 source 上通过 tidy/verify/test/race/build/vet、CGO=0 Linux amd64/arm64 build、license/PATENTS、
  CycloneDX 1.6 schema、secret scan 与 dependency-diff gate；
- 在 Linux ext4/xfs 环境完成 syscall probe、两进程 lock/quota、partial write/sync/close/ENOSYS、
  no-replace crash window、torn tail/replay 及 power-loss/restart evidence；
- 完成后由独立 reviewer 复核 exact diff、closure classification 和 fail-closed fault mapping，再声明 impl-3
  dependency integration Done。

任一 version/sum/proxy bits/license/PATENTS/API import/build tag/toolchain/module graph/advisory DB/distribution scope 变化，
均使本批准的对应证据失效并要求重审。

## Durable upstream locators

- Module：[pkg.go.dev `golang.org/x/sys@v0.44.0`](https://pkg.go.dev/golang.org/x/sys@v0.44.0)
- Unix API：[pkg.go.dev `golang.org/x/sys/unix@v0.44.0`](https://pkg.go.dev/golang.org/x/sys/unix@v0.44.0)
- Canonical source tag：[go.googlesource.com/sys `v0.44.0`](https://go.googlesource.com/sys/+/refs/tags/v0.44.0)
- GitHub mirror tag：[golang/sys `v0.44.0`](https://github.com/golang/sys/tree/v0.44.0)
- Go proxy：[`.info`](https://proxy.golang.org/golang.org/x/sys/@v/v0.44.0.info)、
  [`go.mod`](https://proxy.golang.org/golang.org/x/sys/@v/v0.44.0.mod)
- SumDB：[x/sys@v0.44.0](https://sum.golang.org/lookup/golang.org/x/sys@v0.44.0)
- Fixed vulnerability：[GO-2026-5024](https://pkg.go.dev/vuln/GO-2026-5024)
- Runtime authority：[ADR 0010 §5.4](../../adr/0010-p1-postgres-projection-contract.md)

联网 raw responses 与临时 fixture 位于权限 `0700` 的临时目录，review 后删除；仓库只保留 sanitised decision、
stable bits identity 和 response digest。未来 RC 必须重新生成 sanitised scanner/proxy/SumDB/OSV evidence，不得把本
snapshot 的空查询或旧 signed tree head 当作永久安全证明。
