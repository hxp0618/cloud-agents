# Sandbox PTY Access Grant 与 Gateway — 2026-09-06

Source base `ff70a0d6d6f1563d7dc00e516c4444cdba08d97b`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty；无关 `.gitignore`、`go.work.sum` 和
`docs/img.png` 未修改、未暂存。本切片复用 product-000057、现有 Sandbox/Workspace authority 和固定
OpenSandbox 候选，新增 product-000058；未引入新的前端框架或状态管理依赖。

Product API 只向有 `sandboxes.update` scope 的普通用户签发 60～900 秒、绑定当前 Sandbox generation 的
PTY Grant；原始 `cag1_` token 仅在该响应返回，数据库只保存 SHA-256 digest。独立 Access Gateway 只接受
固定 PTY create/get/delete/WebSocket 路由，每次由 PostgreSQL 重新解析 tenant/project/Grant/generation、
当前 Running runtime、成功 Operation、长期卷单写状态及内部 credentialRef；浏览器不能提交或获得
OpenSandbox endpoint、runtime receipt 或基础设施凭据。execd 的 cwd 固定为 `/workspace`。

Grant、PTY session 映射、签发/撤销 activity 均持久化。Gateway 重启后通过绝对 `since` cursor 重连；
活跃连接每秒重验 authority，Grant 撤销、过期或 Sandbox generation 变化后关闭连接并拒绝新请求。Admin
API/Web 只显示 Grant ID、状态、generation、到期时间、resource version 和 PTY 数量；撤销要求精确 Grant
ID、generation/resource version、影响确认和幂等键，不读取 token、终端输入输出或 Workspace 内容。

## 实际验证

```sh
node scripts/test-foundation-controller-docker.mjs \
  /tmp/cloud-agents-base-m2-pty-20260906-8
node scripts/test-foundation-product-migration.mjs \
  /tmp/cloud-agents-base-m2-pty-migration-20260906
bun scripts/generate-platform-json-sdks.ts --check
bun run platform:migrations:check
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test \
  ./services/control-plane/internal/accessgrant \
  ./services/control-plane/internal/accessgateway \
  ./services/control-plane/internal/opensandbox \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/localmigration \
  ./services/control-plane/internal/foundationcontroller \
  ./services/control-plane/cmd/cloud-agents-access-gateway \
  ./services/control-plane/cmd/cloud-agentsctl \
  ./services/control-plane/cmd/cloud-agents-product-migrate \
  ./services/control-plane/cmd/cloud-agents-control-plane
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test -tags localdev \
  ./services/control-plane/cmd/cloud-agents-control-plane
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test -race \
  ./services/control-plane/internal/accessgrant \
  ./services/control-plane/internal/accessgateway \
  ./services/control-plane/cmd/cloud-agentsctl
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go vet \
  ./services/control-plane/internal/accessgrant \
  ./services/control-plane/internal/accessgateway \
  ./services/control-plane/internal/opensandbox \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/localmigration \
  ./services/control-plane/internal/foundationcontroller \
  ./services/control-plane/cmd/cloud-agents-access-gateway \
  ./services/control-plane/cmd/cloud-agentsctl \
  ./services/control-plane/cmd/cloud-agents-product-migrate \
  ./services/control-plane/cmd/cloud-agents-control-plane
bun run test && bun run typecheck && bun run build # cwd=sdk/typescript
bun run test && bun run typecheck && bun run build # cwd=apps/admin-web
bun run test && bun run typecheck && bun run build # cwd=apps/user-web
```

真实 harness 使用 OrbStack Docker 29.4.0/aarch64、PostgreSQL 17.6、固定 source
`207d94c7dc7735c143856fe5c6538b743e478786` 和 digest-pinned server/execd/Node 镜像。generation 3 上签发
Grant 后建立 PTY，真实 shell 回报 cwd `/workspace`；Gateway 重启后 session 映射和 output offset 78 保留，
从 cursor 0 重放无丢失。产生 1,100,165 bytes 输出后，候选的固定回放窗口恰为 1,048,576 bytes，起始
offset 为 51,589。

错误 token、跨 tenant、普通用户访问 Admin、过期 Grant 和撤销后重连均为 403。Admin 撤销使当时仍活跃
的 WebSocket 关闭；签发 activity 2 条、撤销 activity 1 条。Admin 原始响应未出现 access token、终端
输出、Workspace 文件内容、endpoint、credentialRef 或 providerCredentialRef。随后复跑 TTL、重建与
数据摘要回归，Workspace SHA-256
`69c204e27c471aa5cd8e06a074ebd08577ef5ddc2caf2c26800c4b8b95a8ebaa` 保持不变，最终 test-owned
runtime container、Workspace volume 和 harness container 均为 0。

product-000057→000058 精确升级、000058 全新安装和 58 条迁移 no-op 重放在独立 PostgreSQL 17.6 通过；
生成包 digest 为
`sha256:5bfc89969d49c4d5a50c4c300b5ffff58476560a79ab5bd5be1fe82804dafbb7`。相关 Go packages、TypeScript
SDK 46 项测试/typecheck/build、Admin Web 32 项测试/typecheck/build、User Web 23 项测试/typecheck/build、
相关 Go race/vet 均通过；Admin build 仍只有既有 500 kB chunk warning。[机器可读结果](base-m2-sandbox-pty-20260906.json)
固定本次输入和结果。

全量 `GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test ./...` 运行 10 分钟后在既有
`internal/migration` 冻结证据套件失败并超时。定向复现显示当前 HEAD 已提交的 migration manifest/schema
bundle 为 13 段、schema digest 为 `sha256:c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca`，
而未改动的 `TestCheckedInBundleQuotaReservationExact` 仍固定 11 段；另有既有 journal quota 和
`runner_ledger_recovery_abort_terminal.go` sealed-session spread 失败。本切片未修改该 manifest、bundle 或
`internal/migration` 测试，不把这组既有失败记为通过；本切片相关的 product migration、local migration、
生成包一致性和聚焦 Go 测试均已通过。

全量 `platform:sdk:check` 在 JSON SDK 一致性通过后，被本机 Go 1.27.1 与仓库 pin Go 1.26.6 的版本门禁
停止；contract standards 被本机 Bun 1.4.1/uv 0.6.0 与 pin Bun 1.3.14/uv 0.12.5 停止。本切片所改 JSON
契约、生成 Go/TypeScript SDK 和迁移生成均已用对应独立检查通过，不把未运行的 proto/标准套件记为通过。

## 证据边界

这是 BASE-M2 的 PTY/Access Grant/Gateway/Admin Grant 垂直切片，不是 BASE-M2 完成。Admin Web 本轮有
真实 API authority、测试、类型检查和生产构建，但未做浏览器视觉矩阵；后续 BASE-M5 统一补齐。仍缺 Files、
私有 Preview、短期 SSH、实际网络策略及其 Admin 管理；未部署、发布镜像或操作现有资源，也未运行
Kubernetes/outbound 客户节点。
