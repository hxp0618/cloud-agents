# D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1 — independent read-only review

Verdict：`APPROVE`

审查日期：2026-08-28（Asia/Shanghai）

审查方式：独立只读；reviewer 在 fixed candidate 的 clean archive/worktree 中执行检查，
未修改 candidate、未写入生产数据库、未执行 production/public HTTP、P2/provider、部署、
发布或 Gate 操作。

## 1. 固定 candidate 与一次 repair

本记录审查的是单一 append-only candidate；review record 作为其后续 child 追加，不把本
文件纳入 candidate 自身的 generated input manifest，避免自引用：

| 项目 | 值 |
| --- | --- |
| initial candidate | `398120f57f6483754f92fa1641b029f0231e8001` |
| unique r1 P1 repair / reviewed candidate | `f46ecdbc163e7c66d2ee87fed245e86f56a4e293` |
| reviewed candidate tree | `98db614adb31d036a9e97252b05a688d6d2f11fa` |
| reviewed candidate direct parent | `398120f57f6483754f92fa1641b029f0231e8001` |

唯一 repair 修复 `validateLoopbackListen` 的端口解析并强制 `1..65535`，并将
`runLocalWorker` 的顺序固定为先成功 `net.Listen`、后创建 token 文件；新增 `0`、非数字、
越界端口和 occupied-listener negative tests。该 repair 仍属于同一 `D-056 ... .r1`，未
创建 r2/r3/r4；repair 后重新生成所有 generated profile bytes 并由本记录独立复审。

## 2. Authority、profile、schema 与完整集合

- authority：`D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1`
- profile：`cloud-agents/worker-localdev-launcher/v1alpha1`
- source logical digest：`sha256:9af247b7deffecd2188a19ad1c4859ca8fb7332c15ed6aa980f15eee99663699`
- profile logical digest：`sha256:dc83b89cad24104093e86e69d14743ca9bbc1b106113c90ca52bfa9bde04b72e`
- input manifest digest：`sha256:ca5849afe9f82446f1e9e1439e3cbd6303f668e292b0f924a99d9fd4d82fc9ea`
- input set：39 个显式 regular files；exclusion set：12 个 exact paths/roots；generated
  set：5 个 exact paths。三者均无交集，路径按 UTF-8 bytewise 顺序冻结；完整集合如下。

### 2.1 Exact input set（39）

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
services/worker/cmd/cloud-agents-worker/README.md
services/worker/cmd/cloud-agents-worker/main.go
services/worker/cmd/cloud-agents-worker/main_test.go
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

### 2.2 Exact exclusion set（12）与 generated set（5）

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

```text
services/worker/localdev-launcher-profile/v1/authority-source.json
services/worker/localdev-launcher-profile/v1/authority-source.schema.json
services/worker/localdev-launcher-profile/v1/profile.json
services/worker/localdev-launcher-profile/v1/profile.schema.json
services/worker/localdev_launcher_profile_generated.go
```

generated object 的 candidate blob IDs：

| object | Git blob |
| --- | --- |
| authority source | `f147b95fa2933fed55a160beb3be5abb42d63e80` |
| authority source schema | `7228642e2b75bd84552d4e1a7696e5c87af4a02b` |
| profile | `3a93cfe468645aee85438a6223c75af308cc4ccd` |
| profile schema | `daa8432bfd4394ad2eff74a08372a7010a0a764d` |
| generated Go | `a8e1846c82082a64679b7890a78cb9405b3f99e5` |

## 3. Archive、member-manifest、runner 与 receipt

冻结但不发出 archive/member-manifest：

- archive：`deterministic-ustar-v1`，无压缩，UTF-8 bytewise path order，固定
  `mode=100644,uid=0,gid=0,mtime=0`，duplicate/symlink 均 reject，`emission=forbidden`；
- member manifest：`utf8-bytewise-sorted-path-mode-size-sha256-nul-v1`，字段为
  `path\\0mode\\0size\\0sha256\\0`，regular-file-only，duplicate reject，
  `emission=forbidden`。

authority runner 声明 Go `1.26.0` / toolchain `go1.26.6`、Bun `1.3.14`、Node
`24.18.1`，目标 `darwin-arm64` 与 `linux-amd64`；Go entrypoint 是
`GOWORK=off GOFLAGS=-mod=readonly go test -tags localdev ./...`，launcher 是
`go run -tags localdev ./cmd/cloud-agents-worker`。审查机实际报告 Bun `1.4.0`、Node
`26.7.0`，因此 TS 结果是 generator compatibility evidence，不冒充声明版本的 pin
proof；Go 使用 pinned `go1.26.6`，并完成 `linux-amd64` compile check。

receipt/runtime evidence 固定为 `process-local://worker-service/health`，
`persistence=no_write`、state=`ABSENT_PENDING`、`canonicalProjection=none_for_health_only`；
没有 durable receipt/archive/ledger 写入。complete-ledger 保持 no-op，entry/recovery
writer 保持 `NOT_IMPLEMENTED`。

## 4. 独立 code 与 fail-closed review

- 只接受 generated profile 的 exact ID、revision、source/profile/input digest、route、
  固定 Worker/Supervisor SPIFFE、lease 和 generation；caller-selected profile/path、
  foreign transport 与非 loopback listener 均拒绝。
- listener host 必须是 IPv4/IPv6 loopback，port 必须是 `1..65535`；RemoteAddr 也执行
  loopback + 有效端口检查。Bearer header 必须唯一、格式正确且 constant-time 比较；认证后
  context 只注入固定 Supervisor identity，`expected_*` 不是认证材料。
- listener bind 在 token-file creation 之前；occupied bind negative test 确认失败不会留下
  token。token 文件使用 `O_EXCL`、mode `0600`，token 不进入 argv、日志或 health/profile。
- 只注册 `/healthz` GET 和 generated Connect Worker route；unknown route、未认证请求、
  malformed peer 和非 loopback 请求 fail closed。health JSON 的 authority/profile/revision/
  digest/identity/lease/generation/transport 均与 generated profile 对齐。
- `OperationExecutor` 未注入，因此 ExecuteOperation 与 GetOperationReceipt 保持
  `Unimplemented`；不会因 launcher 存在而写 operation、receipt 或 ledger。
- 未发现 production/public HTTP、TLS/mTLS、P2/provider、runtime、PostgreSQL/durable
  persistence、workspace/artifact/credential、deployment、publication 或 Gate actuator。

## 5. 独立重跑证据

| 检查 | 结果 |
| --- | --- |
| `bun scripts/generate-worker-localdev-launcher-profile.ts --check` | PASS |
| `bunx vitest run scripts/lib/worker-localdev-launcher-profile.test.ts --reporter=dot` | PASS（3/3） |
| `bunx oxfmt --check ...` / `bunx oxlint ... --deny-warnings` | PASS |
| pinned Go `go -C services/worker test ./... -count=1` | PASS |
| pinned Go `go -C services/worker test -tags localdev ./... -count=1` | PASS |
| pinned Go `go -C services/worker test -tags localdev -race ./... -count=1` | PASS |
| pinned Go `go -C services/worker vet -tags localdev ./...` | PASS |
| `go -C services/worker mod tidy -diff` / `mod verify` | PASS（empty diff / verified） |
| live localdev process on `127.0.0.1:18095` | PASS：health 200、unauth 401、unknown 404、token mode 0600、SIGTERM exit 0 |
| `git diff --check` | PASS |

live run 只使用本机 temporary binary/token 和 loopback，不连接远程主机，不产生仓库内
构建产物；结果不是 deployment、production HTTP、数据库、provider 或 Gate evidence。

## 6. P0/P1/P2 verdict、lineage 与后续边界

| 类别 | 数量 | 结论 |
| --- | ---: | --- |
| P0 | 0 | 未发现阻断性身份、数据完整性或越界副作用问题 |
| P1 | 0 | 端口校验与 bind-before-token 唯一 repair 已完成并复审 |
| P2 | 3 | 记录并延期：token 父目录 symlink/path race；generator lstat→write TOCTOU；正常/预取消退出时 token 文件由 caller 管理、不会自动删除 |

主 reviewer 对 fixed candidate 给出 P2=0；第二路补充只读审计将上表三个低风险 residual
列为 P2。为保守保存证据，本 consolidated record 采用 P2=3 并延期，不把它们升级为 P1，
也不在 reviewed candidate 之后继续修改 r1。

lineage 为 `single-predecessor-append-only`：前置是
`D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1`，profile digest
`sha256:892a718cfd58e138cbb22e556da2f0088fdc8b73f43b47805b35e9c90f777e74`；
`D-054-WORKER-DISPATCH-000001.r1`、`D-053-MIG-000014.r2`、`D-053-EC-2.r3` 仅为
immutable historical references。前置 source/profile/schema/manifest/SQL/catalog/
archive/review bytes 未修改，历史 evidence 采用 `retain-and-never-rewrite`。

该 `APPROVE` 只允许按既有批准进入下一个 code-bearing P1 slice；不表示生产 Runner、
Control Plane transport、PostgreSQL/OIDC/JWKS、provider/HTTP/P2、部署、发布或任何 Gate
已完成/关闭。review child 只追加本文件，不改变 reviewed candidate SHA/tree；P2 按
authority 的 `record-and-defer` 规则处理。
