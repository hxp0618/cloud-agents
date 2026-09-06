# 私有 Sandbox Preview 与 Admin 端口诊断 — 2026-09-06

Source base `74b882d0072e5b401edfcd2337e0f6ae15a0a409`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty；无关 `.gitignore`、`go.work.sum`、
`docs/img.png` 未修改或暂存。本切片复用现有短期 Sandbox Grant、PostgreSQL tenant transaction、独立
Access Gateway 和固定 OpenSandbox 候选，没有新增代理框架、前端依赖或状态管理层。

Preview 默认不可达。用户必须先用 generation-bound、60～900 秒的 Grant 显式注册一个 1024～65535
端口；内部 execd `44772` 永远不可注册，每个 Grant 最多 32 个活动端口。product-000060 保存端口、注册
时间和撤销时间。生成 Go/TypeScript SDK 和 CLI 只提供注册/撤销，并返回同一 Gateway 上的相对
`proxyPath`，不返回 OpenSandbox endpoint、runtime ID 或 credential reference。

Gateway 在每次请求和每秒一次的活跃响应检查中重新解析 tenant/project/Grant、当前 Sandbox generation、
Running 物理回执及端口注册。反向代理只能使用候选返回的精确
`/v1/sandboxes/{runtime}/proxy/{port}`；外部 host、query target、用户提交 endpoint 和内部端口均不能成为
上游。转发前删除 Grant Authorization、Proxy Authorization、Cookie 和用户注入的 forwarding header；
候选 API key 仅用于 Gateway→OpenSandbox，实测没有进入 Sandbox 应用。响应禁用缓存并删除 `Set-Cookie`。
Admin API/Web 仅显示当前活动端口数字，不读取 Preview 响应、proxy path、token 或凭据。

## 实际验证

```sh
bun scripts/test-foundation-product-migration.mjs
node scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m2-sandbox-preview-20260906-runtime
bun scripts/generate-platform-json-sdks.ts --check
bun scripts/generate-foundation-migration-package.ts --check
bun scripts/check-platform-migration-bundle.ts
bun scripts/generate-platform-migration-bundle.ts --check
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test <本切片相关 Control Plane/SDK 包>
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go test -race \
  ./services/control-plane/internal/accessgateway \
  ./services/control-plane/internal/opensandbox \
  ./services/control-plane/cmd/cloud-agentsctl
GOTOOLCHAIN=local GOFLAGS=-mod=readonly go vet <本切片相关 Control Plane 包>
bun run test && bun run typecheck && bun run build # cwd=sdk/typescript
bun run test && bun run typecheck && bun run build # cwd=apps/admin-web
bun run test && bun run typecheck && bun run build # cwd=apps/user-web
```

真实 migration harness 在 PostgreSQL 17.6 上通过 000059→000060 精确升级、000060 全新安装、no-op replay
和 60 行不可变 ledger；生成包 digest 为
`sha256:682b9693e113ab3372a59fd0af0f1e37c356fef2fec9d4dc8036662ee3cd1573`。
同一生成器按冻结的 000015～000060 manifest/schema bytes 生成有序 binding 闭集，selection、admission、
current-head 与历史 ledger 校验共用该闭集；未知 000061 selector 的负向测试通过。

真实 OrbStack Docker 29.4.0/aarch64 使用固定 source
`207d94c7dc7735c143856fe5c6538b743e478786` 和 digest-pinned server/execd/Node 镜像。在 generation 3
Sandbox 内启动真实 Node HTTP 服务后，未注册端口不可达；注册 3000 后经返回的相对 `proxyPath` 转发
POST `/hello?value=alpha`，方法、路径和 query 保持一致。Grant/Proxy Authorization、Cookie、候选 API key
和用户伪造的 forwarding 值均未到达应用。

错误 token 与跨 tenant 为 403，未注册端口和内部 44772 为 404。Gateway 重启后同一注册仍可用；端口撤销
在轮询窗口内关闭已有流式响应，随后访问为 404。重新注册后 Admin 只看到 `[3000]`；整个 Grant 撤销和另一
Grant 到期后 Preview 均为 403，Admin 原始响应不含 token、endpoint、proxyPath、Preview/终端/文件内容或
credential reference。机器可读结果见 [Docker E2E](base-m2-sandbox-preview-20260906-runtime/evidence.json)；
最终 test-owned runtime container、Workspace volume 和 harness container 均为 0。

相关 Go tests/race/vet、TypeScript SDK 46 项、Admin Web 32 项、User Web 23 项以及双 Web
typecheck/build 均通过；Admin build 仍只有既有 500 kB chunk warning。AJV 官方套件记录为仓库既有
`EXECUTED_NONCONFORMANT` 非 Gate audit。contract standards 被本机 Bun 1.4.1、uv 0.6.0 与固定 Bun
1.3.14、uv 0.12.5 的版本门禁停止，未记为通过。

## 证据边界

这是 BASE-M2 的私有 HTTP Preview/Access Gateway/Admin 活动端口垂直切片，不是 BASE-M2 完成。
WebSocket upgrade 与普通 GET 共享同一固定代理路由，但本轮真实 E2E 只执行 HTTP。Admin Web 未新增浏览器
视觉矩阵；该项留在 BASE-M5 统一双语/双主题/桌面移动验收。短期 SSH、Network Policy 实际执行及对应
Admin 状态仍未完成；当前 Foundation RuntimeProfile 尚未绑定 Network Policy authority，因此没有把已保存
策略宣称为已执行。未部署、发布镜像、运行 Kubernetes 或接入客户节点。
