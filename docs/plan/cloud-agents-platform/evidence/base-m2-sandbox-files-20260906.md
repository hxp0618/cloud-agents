# Sandbox Files 与 Admin 诊断 — 2026-09-06

Source base `2cc3307c99095a5cafbc1306106ce73edb273e4f`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty；无关 `.gitignore`、`go.work.sum`、
`docs/img.png` 未修改或暂存。本切片复用同一短期 Access Grant、独立 Access Gateway、长期
Workspace 和固定 OpenSandbox 候选，没有新增框架或产品抽象。

Grant 的 `accessKind` 由只表示 PTY 的 `pty` 升级为固定 `sandbox`，同时授权 PTY 与 Files。Files API
仅接受 `/workspace` 相对路径，支持立即子项列表、最多 1 MiB 的版本绑定分页读取、最多 16 MiB 的
base64url 写入和普通文件删除。已有路径组件逐级通过固定候选 `/files/info` 检查；symlink 可列出但不可
遍历、读取、覆盖或删除。浏览器和 CLI 只连接 Access Gateway，不接收 OpenSandbox endpoint、runtime
receipt 或 credential reference。

product-000059 持久化每次文件操作的 action、started/succeeded/failed、稳定错误码、字节数和完成时间。
Admin API/Web 仅展示操作总数、失败数、最近动作/状态/时间和 stable error code；不保存或返回文件路径、
文件名、内容、token 或基础设施凭据。异常进程退出留下 `started` 记录作为诊断事实，不伪造成功。

## 实际验证

```sh
bun scripts/test-foundation-product-migration.mjs
bun scripts/test-foundation-controller-docker.mjs \
  docs/plan/cloud-agents-platform/evidence/base-m2-sandbox-files-20260906-runtime
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
bun run --cwd sdk/typescript test
bun run --cwd sdk/typescript typecheck
bun run --cwd apps/admin-web test
bun run --cwd apps/admin-web typecheck
bun run --cwd apps/admin-web build
bun run --cwd apps/user-web test
bun run --cwd apps/user-web typecheck
bun run --cwd apps/user-web build
```

真实 migration harness 在 PostgreSQL 17.6 上通过 000058→000059 精确升级、000059 全新安装、no-op replay
和 59 行不可变 ledger；生成包 digest 为
`sha256:9b4344c2ea4d9ac2525a1c761a2024dabcd528ef72c499c9ec6a7f67d8d1ddce`。

真实 OrbStack Docker 29.4.0/aarch64 使用固定 source `207d94c7dc7735c143856fe5c6538b743e478786`
和 digest-pinned server/execd/Node 镜像。生成 Go SDK 经 Control Plane 签发 generation 3 Grant 后，真实写入
29 bytes，列表同时返回普通文件和 symlink 元数据；首段读取后重启 Gateway，使用同一 `fileVersion` 与
绝对 offset 续读第二段并还原完整内容。错误 token、跨 tenant 为 403，路径穿越为 400，symlink 穿越为
409，超限请求为 413，删除后读取为 404。

数据库记录 7 次文件尝试、其中 2 次失败；Admin 最近诊断为 `read / failed / NOT_FOUND`。Admin 原始响应
未出现 Grant token、测试文件路径、symlink 名、`contentBase64Url`、endpoint、credentialRef 或
providerCredentialRef。机器可读运行证据见
[Docker E2E](base-m2-sandbox-files-20260906-runtime/evidence.json)；最终 test-owned runtime container、
Workspace volume 和 harness container 均为 0。

相关 Go tests/race/vet、TypeScript SDK 46 项、Admin Web 32 项、User Web 23 项以及双 Web
typecheck/build 均通过；Admin build 仍只有既有 500 kB chunk warning。contract standards 检查被本机
Bun 1.4.1、uv 0.6.0 与仓库固定 Bun 1.3.14、uv 0.12.5 的版本门禁停止，未记为通过。

## 证据边界

这是 BASE-M2 的 Files/Access Gateway/Admin 诊断垂直切片，不是 BASE-M2 完成。Admin Web 本轮未新增
浏览器视觉矩阵；该项留在 BASE-M5 统一双语/双主题/桌面移动验收。私有 Preview、短期 SSH、实际网络策略
执行及其 Admin 管理仍未完成；未部署、发布镜像、运行 Kubernetes 或接入客户节点。
