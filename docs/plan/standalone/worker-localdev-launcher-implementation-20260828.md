# D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1 — implementation record

日期：2026-08-28（Asia/Shanghai）

Profile：cloud-agents/worker-localdev-launcher/v1alpha1

状态：IMPLEMENTED_REVIEW_PENDING

## 1. 实现内容

本 slice 新增一个显式 localdev build-tag 的 Worker 进程入口：

- services/worker/cmd/cloud-agents-worker/main.go
- services/worker/cmd/cloud-agents-worker/main_test.go
- services/worker/cmd/cloud-agents-worker/README.md

它把已有 worker.NewService 和 worker.NewHandler 放入真实的 net.Listen/
graceful-shutdown 生命周期。默认监听 127.0.0.1:8091，并且 parser 先验证地址必须是合法
IPv4/IPv6 loopback（含 1..65535 port），再要求显式 --token-file。token 用 32-byte
CSPRNG、URL-safe base64 生成，通过 O_EXCL 和 mode 0600 一次写入；既有 token/文件不会被
覆盖，token 不打印、不放入 argv 或 health JSON。SIGINT/SIGTERM 只触发 bounded graceful
shutdown。

transport middleware 在进入 generated handler 前同时检查：

1. RemoteAddr 是 loopback 且端口合法；
2. 唯一、无空白、格式正确的 Authorization: Bearer <token>；
3. constant-time token 比较；
4. 将固定 generated Supervisor identity 放入 context。

Worker service 的 IdentityProvider 只读取该 context；请求中的 expected_* 字段仍是 peer
constraint。worker/supervisor identity、trust domain、leaf digest、lease、generation、
Connect route 和 health route 均必须与 GeneratedWorkerLocalDevLauncherProfile.Valid()
及其 generated constants 完全一致；caller 无法选择 profile、identity、foreign path 或
transport。

## 2. 固定执行路径和禁止项

    starting
      -- successful loopback listen --> serving
      -- SIGINT/SIGTERM ------------> stopping
      -- server close --------------> stopped

允许的 HTTP surface 只有 authenticated GET /healthz 和
/cloudagents.worker.v1alpha1.WorkerExecutionService/ 下的 generated Connect RPC。
healthz 返回 profile/revision/identity/lease/generation/status 的 process-local JSON；
不创建 durable receipt。Worker 不注入 OperationExecutor，所以 ExecuteOperation 和
GetOperationReceipt 继续由既有 service fail closed 为 NOT_IMPLEMENTED；complete-ledger
仍为 no-op，entry/recovery writer 仍为 NOT_IMPLEMENTED。

本实现没有 PostgreSQL、migration、filesystem archive、provider、Portable Runtime、
workspace、artifact、credential、P2、公开/生产 HTTP、TLS/mTLS、部署、发布或 Gate
actuator。profile 中 externalSideEffects.http=false 指不开放 external/public HTTP；
loopback listener 只是本地进程边界，不能作为生产 HTTP 证据。

## 3. generated authority/profile

生成器和五个输出由
scripts/generate-worker-localdev-launcher-profile.ts /
scripts/lib/worker-localdev-launcher-profile.ts 管理；source/profile 使用 strict
Draft 2020-12 schema 和 exact nested values。当前生成器检查：

- 39 个显式 regular-file inputs、12 个 exact exclusions、5 个 generated outputs；
- UTF-8 bytewise sorted path + mode/size/raw SHA-256/NUL manifest；
- 两次读取、inode/device/mode/size drift、symlink/special-file、重复/集合重叠和 output
  symlink fail closed；
- deterministic-ustar-v1 archive 与
  utf8-bytewise-sorted-path-mode-size-sha256-nul-v1 member manifest labels，
  两者 emission 均为 forbidden；
- runner 的 Go 1.26.0 / toolchain go1.26.6、Bun 1.3.14、Node 24.18.1、
  darwin-arm64/linux-amd64 和 GOWORK=off GOFLAGS=-mod=readonly go test -tags localdev ./...。

当前生成 digest（candidate 修改后必须重新生成，不能复用）：

| 对象 | digest |
| --- | --- |
| authority source | sha256:7f6a9fc3b097d793d708c6c9ac4b2de16ac78fe8020408c3cec9fcdd5c94ff5c |
| profile | sha256:2490437ed60735fc0ebfcff0aaaa9adeb48f0850823db15666608ae4ca22ee4a |
| input manifest | sha256:dd373d37032bb4e31856498b5dc06a6a8d7df3f7b0d2f24aaac270ee232f034d |

前置 lineage 为 D-055 profile
sha256:892a718cfd58e138cbb22e556da2f0088fdc8b73f43b47805b35e9c90f777e74；
D-054、D-053-MIG-000014.r2 和 D-053-EC-2.r3 只作为 immutable historical references。

## 4. focused verification

以下证据只覆盖 localdev code/contract，不覆盖 live deployment、生产 DB、provider 或
Gate：

    bun scripts/generate-worker-localdev-launcher-profile.ts --check
    bunx vitest run scripts/lib/worker-localdev-launcher-profile.test.ts --reporter=dot
    GOWORK=off GOFLAGS=-mod=readonly go -C services/worker test -tags localdev ./... -count=1 -timeout=30m
    GOWORK=off GOFLAGS=-mod=readonly go -C services/worker test -tags localdev -race ./... -count=1 -timeout=30m
    GOWORK=off GOFLAGS=-mod=readonly go -C services/worker vet -tags localdev ./...

launcher unit tests include non-loopback/malformed address rejection, token file mode and
exclusive-write checks, duplicate/missing/wrong token rejection, fixed identity context
binding, malformed peer address rejection, generated Connect Negotiate→CheckHealth over an
httptest loopback server, health JSON and unknown-route checks. A separate live evidence run
will start the command on loopback with a temporary token file, query /healthz, and send
SIGTERM; its result must remain classified as local process evidence.

本次 live process evidence 已完成（仅本机 temporary binary，未连接远程主机）：

- pinned Go 1.26.6 编译 localdev command，监听 127.0.0.1:18091；
- 读取 token 文件确认 mode=0600；带 token 的 GET /healthz 返回 200，且 authority、profile、
  revision、profile_digest、固定 SPIFFE pair、lease/generation 和 external_side_effects=false
  全部与 generated profile 一致；
- 无 token 返回 401，认证后的 unknown route 返回 404；
- 发送 SIGTERM 后进程以 status 0 graceful shutdown，监听端口释放；temporary binary/token
  不在仓库内。

这只是 local process/loopback evidence，不是部署、生产 HTTP、mTLS、数据库、provider 或
release 证据。

## 5. review and candidate fence

本记录不把 review bytes 写入 candidate digest。完成 focused checks 后固定单一 parent
candidate，再由独立 reviewer 在 clean archive/worktree 只读审查，记录路径为
docs/plan/standalone/worker-localdev-launcher-independent-review-20260828.md，输出
APPROVE 或 REQUEST_CHANGES 及 P0/P1/P2 verdict。reviewer 不得修改 candidate、执行生产
副作用或关闭 Gate；同一 r1 candidate 内最多一次 P0/P1 repair + re-review，P2 记录后延期。
只有 review child 追加后，feature branch 才可用 git merge --no-ff 进入
codex/cloud-agents-platform-p0；不 squash/rebase/force-push，不删除历史或执行
reflog/GC/prune。
