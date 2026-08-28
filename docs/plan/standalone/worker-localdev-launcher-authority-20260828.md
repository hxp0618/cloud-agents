# D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1 — versioned authority

日期：2026-08-28（Asia/Shanghai）

Profile：`cloud-agents/worker-localdev-launcher/v1alpha1`

状态：`AUTHORITY_FROZEN_REVIEW_PENDING`（只读审查候选；不关闭 Gate）

## 1. 决策和边界

本 authority 只批准 Standalone Platform v0.1 的一个真实、可运行的
localdev Worker launcher：把已有的内存 `worker.NewService` 和 generated Connect
handler 放进一个显式 `localdev` build-tag 命令，并在 loopback HTTP 上提供认证的
Worker RPC 和只读 `/healthz`。它是后续 Control Plane transport bridge 的前置过程边界，
不是 production Runner、mTLS listener 或 provider/runtime adapter。

固定身份为：

- Worker：`spiffe://cloud-agents.local/worker/localdev`
- Supervisor：`spiffe://cloud-agents.local/supervisor/localdev`
- lease：`worker-localdev-lease-000001`
- generation：`1`

监听地址必须是 IPv4/IPv6 loopback；非 loopback、主机名、通配地址和 malformed address
均 fail closed。token 只能由 launcher 生成并以 `O_EXCL`、mode `0600` 写入 caller 指定的
文件；token 不进入 argv、日志、health 响应或 profile。 `expected_*` 请求字段只作为 peer
identity constraint，永远不是认证材料。当前 bearer-to-context 身份适配器只用于
localdev 测试/开发，不声称实现生产 mTLS 或 trust provisioning。

允许的 route 只有：

- `/healthz`（GET、无持久化的 process-local JSON）
- `/cloudagents.worker.v1alpha1.WorkerExecutionService/` 下的 generated Connect RPC

unknown route、非 loopback 请求、缺失/重复/格式错误/不匹配 token 都拒绝。Worker 默认不
注入 `OperationExecutor`，因此 complete-ledger 为 no-op，entry/recovery writer 继续
`NOT_IMPLEMENTED`，不会因为 launcher 存在而产生 operation、receipt 或 ledger 写入。

## 2. 冻结的生成物和 digest

以下五个文件只能由
`scripts/generate-worker-localdev-launcher-profile.ts` 生成；`--check` 是默认模式，
`--write` 只用于冻结当前 candidate：

```text
services/worker/localdev-launcher-profile/v1/authority-source.json
services/worker/localdev-launcher-profile/v1/authority-source.schema.json
services/worker/localdev-launcher-profile/v1/profile.json
services/worker/localdev-launcher-profile/v1/profile.schema.json
services/worker/localdev_launcher_profile_generated.go
```

本 candidate 的 logical digest 在生成器最后一次写入后记录如下（若 candidate 发生任何
修改，必须重新生成并重新审查，不得沿用旧 digest）：

| 对象 | digest |
| --- | --- |
| authority source | `sha256:7f6a9fc3b097d793d708c6c9ac4b2de16ac78fe8020408c3cec9fcdd5c94ff5c` |
| profile | `sha256:2490437ed60735fc0ebfcff0aaaa9adeb48f0850823db15666608ae4ca22ee4a` |
| input manifest | `sha256:dd373d37032bb4e31856498b5dc06a6a8d7df3f7b0d2f24aaac270ee232f034d` |

上述 digest 以 generator 为 authority；candidate commit/tree 和五个文件的 Git blob
digest 只在 fixed candidate commit 后追加到 implementation/review record，不在这里
预先伪造。

## 3. 完整输入、排除和生成集合

输入集合是完整、显式、UTF-8 bytewise sorted 的 39 个 regular files；没有 glob、目录
扫描或隐式 untracked inclusion：

```text
.mise.toml
bun.lock
bunfig.toml
contracts/worker/v1alpha1/README.md
contracts/worker/v1alpha1/kernel.proto
contracts/worker/v1alpha1/worker_supervisor.proto
go.work
package.json
scripts/generate-worker-localdev-launcher-profile.ts
scripts/lib/platform-json-semantics.ts
scripts/lib/worker-localdev-launcher-profile.test.ts
scripts/lib/worker-localdev-launcher-profile.ts
sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go
sdk/go/gen/cloudagents/worker/v1alpha1/worker_supervisor.pb.go
sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect/worker_supervisor.connect.go
sdk/go/gen/common/v1alpha1/identity_generated.go
sdk/go/go.mod
sdk/go/go.sum
services/worker/cmd/cloud-agents-worker/main.go
services/worker/cmd/cloud-agents-worker/main_test.go
services/worker/cmd/cloud-agents-worker/README.md
services/worker/doc.go
services/worker/execution.go
services/worker/execution_test.go
services/worker/go.mod
services/worker/go.sum
services/worker/local_dispatch_handle.go
services/worker/operation_admission.go
services/worker/operation_admission_test.go
services/worker/operation_builder.go
services/worker/operation_builder_test.go
services/worker/service.go
services/worker/service_test.go
services/worker/supervisor/client.go
services/worker/supervisor/client_test.go
services/worker/supervisor/dispatch_profile_generated.go
services/worker/supervisor/local_dispatch.go
services/worker/supervisor/local_dispatch_test.go
tsconfig.base.json
```

排除集合是以下 12 个 exact path/root；它们和输入/生成集合不得重叠：

```text
.idea
deploy
helm
node_modules
packages/cloud-agent-provider-api
packages/cloud-agent-runtime
release
services/control-plane/internal/migration
services/control-plane/internal/store/postgres
services/control-plane/migrations
services/worker/cmd/cloud-agents-worker/provider
tmp
```

除上述输入外的任何 workspace member 都是 undeclared，不能因“当前内容自洽”而被加入
candidate。生成集合为第 2 节列出的五个路径；generated path 不能同时是 input。

manifest 算法固定为
`utf8-bytewise-sorted-path-regular-file-mode-size-sha256-nul-v1`：每个路径先 `lstat`，
拒绝 symlink、目录和 special file；读取 bytes 两次并比较，同时复核 inode、device、mode
和 size，随后按 `path\\0mode\\0size\\0raw_sha256\\0` 追加到 SHA-256。重复路径、集合交叉、
metadata/content drift、输出 symlink 或 output directory 都使 generator 失败。

## 4. archive、member-manifest 和 runner

为保持与后续 successor 的可验证 lineage，冻结下列算法标签，但本 revision 禁止发出
archive/member-manifest：

```text
archive.algorithm       = deterministic-ustar-v1
archive.emission        = forbidden
archive.compression     = none
archive.ordering        = utf8-bytewise-sorted-path
archive.metadata        = mode=100644,uid=0,gid=0,mtime=0
archive.duplicatePolicy = reject
archive.symlinkPolicy   = reject

memberManifest.algorithm       = utf8-bytewise-sorted-path-mode-size-sha256-nul-v1
memberManifest.emission        = forbidden
memberManifest.path            = <not-written>
memberManifest.fields          = path\\0mode\\0size\\0sha256\\0
memberManifest.regularFileOnly = true
memberManifest.duplicatePolicy = reject
```

审查 runner 固定为 Go `1.26.0` / toolchain `go1.26.6`、Bun `1.3.14`、Node
`24.18.1`，目标平台 `darwin-arm64` 与 `linux-amd64`。Go 检查必须使用
`GOWORK=off GOFLAGS=-mod=readonly go test -tags localdev ./...`；launcher 仅通过
`go run -tags localdev ./cmd/cloud-agents-worker` 启动。runner mode 为
`localdev_only`、network 为 `loopback_only`、database/provider 为 `deny`，超时只覆盖
focused tests。任何 compile/unit/race/vet/live evidence 都不能升级为 deployment/release
证据。

## 5. receipt、state machine 和 effect fence

health evidence 的 runtime path 固定为 `process-local://worker-service/health`，
`persistence=no_write`、state=`ABSENT_PENDING`；没有 durable receipt、archive 或 ledger
写入。生命周期仅为：

```text
starting --listen--> serving --signal--> stopping --closed--> stopped
```

所有 external-side-effect flags（database、durableReceipt、HTTP external、P2、provider、
runtime、workspace、artifact、credential、deployment、publication、gate）固定为
`false`。这里的 `HTTP=false` 指“不开放 external/public HTTP”；loopback HTTP 只是本地
进程边界。实现 boundary 进一步固定 production/public HTTP、P2、provider、runtime、
数据库、持久化、部署、发布和 Gate transition 为 `forbidden`。

## 6. lineage fence 和 review 规则

本 revision 是 single-predecessor append-only successor，前置对象固定为：

- authority/revision：`D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1`
- profile：`cloud-agents/managed-agent-worker-local-execution/localdev-v1alpha1`
- profile logical digest：`sha256:892a718cfd58e138cbb22e556da2f0088fdc8b73f43b47805b35e9c90f777e74`

`D-054-WORKER-DISPATCH-000001.r1`、`D-053-MIG-000014.r2` 和 `D-053-EC-2.r3` 是
immutable historical references。任何 predecessor source/profile/schema/manifest/SQL/
catalog/archive/review bytes 都不得修改、重算、替换或通过 caller-selected path 重新解释；
历史证据采用 `retain-and-never-rewrite`。这条 fence 与已独立复核的版本化 authority
要求一致。:codex-annotation{index="1"}

review 必须在 fixed candidate 的 clean archive/worktree 中独立只读执行，记录路径固定为
`docs/plan/standalone/worker-localdev-launcher-independent-review-20260828.md`，输出
`APPROVE` 或 `REQUEST_CHANGES`，并分别给出 P0/P1/P2 verdict。reviewer 不得修改
candidate、生成新 r3/r4、执行生产 DB/HTTP/P2/provider/deploy/release 或关闭 Gate；同一
r1 candidate 内最多允许一次 P0/P1 repair + re-review，P2 只记录并延期。review child 只
追加 review record，不把 review bytes 自引用进 candidate digest。

本 authority 目前只表示“实现候选已冻结、待独立审查”，不表示 Platform RC、生产 Runner、
Control Plane transport、PostgreSQL/OIDC/JWKS、Codex/Claude E2E 或任何 Gate 已完成。
