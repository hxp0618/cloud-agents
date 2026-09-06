# 短期 Sandbox SSH 与 Admin Grant 诊断 — 2026-09-06

Source base `4cc6b304bb22e099a0928b4d0a8d168eb663ffdb`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty；无关 `.gitignore`、`AGENTS.md`、`CLAUDE.md`、
`go.work.sum`、`docs/img.png` 和计划文档 hunks 未覆盖或暂存。本切片复用现有短期 Sandbox Grant、PostgreSQL
tenant transaction、PTY session 映射、OpenSandbox receipt 校验和 Access Gateway；没有新增产品 migration、
sshd 镜像、代理框架或前端依赖。

Product Grant 新增确定性的 `tenant:project:grant` SSH 用户名，现有 60～900 秒 `accessToken` 同时作为 SSH
password。User API/生成 Go 与 TypeScript SDK 不返回 Gateway、Target、OpenSandbox 或客户主机 endpoint。
Access Gateway 使用部署持有且权限受限的 host private key 启动真实 SSH listener；客户端必须 pin 公钥。

每次密码认证及连接存续期间每秒重新校验 tenant、project、Grant token digest、到期/吊销、Sandbox
generation、Running 状态和完整物理 receipt。每个 SSH transport 最多接受一个 `session` channel，并只桥接到该
Sandbox 的固定 `/workspace` PTY/pipe；`direct-tcpip`、remote forwarding、environment mutation、agent/X11、
subsystem 和第二个 channel 均不开放。SSH shell/exec 复用固定候选的 PTY WebSocket 协议与 1 MiB replay 上限，
退出或断开后精确删除候选 PTY。Admin 继续只统计 PTY/SSH session 元数据，不返回 username、token、命令、
终端输出或凭据。

## 实际验证

```sh
GOTOOLCHAIN=local GOFLAGS=-mod=readonly node scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m2-sandbox-ssh-20260906-runtime

# 固定 Bun 1.3.14 / Node 24.18.1 / Go 1.26.6 / Python 3.14.7 / uv 0.12.5
bun run platform:contracts:check
bun run platform:migrations:check
bun scripts/test-foundation-product-migration.mjs
go -C services/control-plane test <本切片相关包>
go -C services/control-plane test -race \
  ./internal/accessgrant ./internal/opensandbox ./internal/accessgateway ./internal/server \
  ./cmd/cloud-agents-access-gateway
go -C services/control-plane vet ./...
go -C sdk/go test ./...
bun run test && bun run typecheck && bun run build # sdk/typescript
bun run test && bun run typecheck && bun run build # apps/admin-web
bun run test && bun run typecheck && bun run build # apps/user-web
```

真实 OrbStack Docker 29.4.0/aarch64 使用固定 OpenSandbox source
`207d94c7dc7735c143856fe5c6538b743e478786` 和 digest-pinned server/execd/Node 镜像；PostgreSQL 为
17.6。真实 SSH 协议以临时 Ed25519 host key 和严格 host-key callback 建连，在 generation 3 Sandbox 中执行
`printf` 并得到 `CAG_SSH_DIR=/workspace`。Gateway 进程上下文停止并以同一 DB/host key 重建后，同一尚有效
Grant 再次认证和执行成功。

错误 password、跨 tenant username、`direct-tcpip` 和 environment mutation 均被拒绝。测试将第二个 Grant
暂时改为不匹配 generation，SSH 认证被拒绝；恢复 generation 后令其按数据库时钟到期，认证仍被拒绝。主
Grant 吊销在轮询窗口内关闭正在 `sleep 30` 的活跃 SSH transport，随后认证失败。Admin 原始响应不含
`sshUsername`、Grant token、`CAG_SSH` 输出、endpoint、proxy path 或 credential reference。机器可读结果见
[Docker E2E](base-m2-sandbox-ssh-20260906-runtime/evidence.json)；最终 test-owned runtime container、Workspace
volume 和 harness container 均为 0。

固定工具链完整 contract check 通过：169 个当前 schema、114 个 OpenAPI operation、14 个 Python standards
tests 和全部生成器均 current。检查同时修正了已有 Admin Grant revoke、Sandbox stop/rebuild 的 coordination
owner 标注，并增加未知 owner 拼写 fail-closed 测试。product-000060 digest 仍为
`sha256:682b9693e113ab3372a59fd0af0f1e37c356fef2fec9d4dc8036662ee3cd1573`，PostgreSQL
000059→000060、fresh、replay 和 60 行 ledger 通过。相关 Go tests/race/vet、SDK 46 项、Admin Web 32 项、
User Web 23 项和三者 typecheck/build 均通过；Admin 仍只有既有 500 kB chunk warning。全仓 lint 仅命中本切片
未改动的 `capture-worker-health.mjs` 既有 `no-console`，未将其写成通过。

源码稳定后另以 `go test -count=1 ./...` 执行完整 Control Plane Go suite：除 `internal/migration` 外全部包通过，
包括本切片的 Access Gateway、Grant、OpenSandbox、server、PostgreSQL 和既有 networkpolicy。migration 包仍将当前
13 个 schema migration 与测试硬编码的 11 个旧快照比较，4 个 quota/journal 测试失败；另有既有提交
`f85cce61` 引入的 `runner_ledger_recovery_abort_terminal.go` 被 sealed-session 边界测试拒绝，最终该包达到 10 分钟
timeout。以上未写成本切片通过，也未借 SSH 提交刷新或放宽 migration 安全断言。

## 证据边界

这是 BASE-M2 的短期 SSH/Access Gateway/Admin 脱敏诊断垂直切片，不是 BASE-M2 完成。Admin Web 仅把既有
计数文案明确为 PTY/SSH，会随 BASE-M5 统一做双语、双主题、桌面/移动视觉矩阵。本轮没有部署或发布，没有
运行 Kubernetes 或连接客户节点；Network Policy 仍只有保存/展示 authority，尚未下发到实际 Sandbox 网络
执行路径。
