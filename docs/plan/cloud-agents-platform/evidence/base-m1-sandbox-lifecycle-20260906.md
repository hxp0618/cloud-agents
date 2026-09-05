# Sandbox Stop/Rebuild 与 Workspace 保留 — 2026-09-06

Source base `5d7bff76b9ff82137a4a9f0898e0fd2844098cae`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty；无关 `.gitignore`、`go.work.sum` 和
`docs/img.png` 未改动、未暂存。本切片复用既有 foundation Operation/outbox、Controller、
OpenSandbox 回执接缝、生成 SDK 与 Daytona `v0.190.0` Admin 壳层，没有新增依赖或未来抽象。

product-000056 为 Sandbox 增加严格物理 runtime receipt、不可变 lifecycle activity 和
Stop/Rebuild transition/claim/settle authority。Admin 请求携带 expected generation、resourceVersion、
精确 Sandbox ID、compute disposition、Workspace `retain` 和幂等键；接受时原子持久化 Operation、
outbox、finalizer 与 Audit。Stop 只验证并删除旧 runtime，不会在卷缺失时创建空卷；Rebuild 只复用
同一归属的物理卷，旧 generation 和错误 owner 均 fail closed。普通用户 Token 由服务端返回 403。

Admin Web 只在 authority 已完全结算时提供 Stop 或 Rebuild。确认框显示资源、generation、
resourceVersion、计算影响、物理卷和 Workspace 保留范围，并要求显式勾选；提交使用生成 SDK，页面再
读取真实 Admin Sandbox authority。成功提示区分“请求已接受”和“操作已完成”，不把 202 当作完成。

## 实际验证

```sh
node scripts/test-foundation-product-migration.mjs
node scripts/test-foundation-controller-docker.mjs <new-output-directory>
bun scripts/generate-foundation-migration-package.ts --check
bun scripts/generate-platform-json-sdks.ts --check
go -C services/control-plane test ./internal/coordination ./internal/dockertarget \
  ./internal/store/postgres ./internal/foundationcontroller ./internal/server \
  ./internal/authn ./internal/localmigration ./cmd/cloud-agents-product-migrate
bun test sdk/typescript/src/platform.test.ts
go -C sdk/go test ./gen/openapi/v1alpha1 ./gen/platform/v1alpha1
bun test apps/admin-web/src
bun run --cwd apps/admin-web typecheck
bun run --cwd apps/admin-web build
```

PostgreSQL 17.6 实测 product-000055 精确升级到 000056、56 条全新安装与 no-op 重放；当前产品包
摘要为 `sha256:0911945f728680634e23968fae1ab8d365897a084106fd0dce087b86c8f547f7`。
受影响 Go 测试、TypeScript SDK 27 项、Admin Web 32 项测试、类型检查与 production build 通过。
build 仅保留既有 601.47 kB chunk warning。

真实 Controller harness 使用 OrbStack Docker 29.4.0/aarch64、PostgreSQL 17.6 和固定 digest 的
OpenSandbox server/execd/runtime。Stop 删除 runtime
`6fcf3221-36e1-4307-9b21-b9bfa4592e55`、把 generation 推进到 2 并释放 writer，但保留卷
`ca-ws-56af1bb2e0b01921e1a5966b8f69dcfe21e1790f824b32a20b63f247`。Rebuild 在 generation 3
创建新 runtime `07c590e3-3373-48dc-88b3-411f0cd965b3`；重建前后 Workspace 文件摘要均为
`69c204e27c471aa5cd8e06a074ebd08577ef5ddc2caf2c26800c4b8b95a8ebaa`。旧 generation 删除请求、
外来 owner 卷和过期 resourceVersion 均被拒绝；最终 test-owned Sandbox/volume 为 0。

Admin 视觉/API harness 使用一次性真实 PostgreSQL、Control Plane 与 Vite。UI 提交 Stop 后生成
`sandbox.stop` Operation；将该一次性 fixture 按 Controller 已验证的 stopped authority 结算后，UI
提交 Rebuild，数据库读回 generation 3、desired `running`、observed `stopped`、Operation `pending`，
且 Workspace 仍为 `available`/`ca-ws-ui-retained`。数据库同时存在两条 lifecycle activity、Operation、
outbox 和 Audit accept 事实；同一 Admin GET 的普通用户 Token 返回 403。

新增确认界面实际覆盖 `zh-CN` dark Stop、`zh-CN` dark Rebuild、`en-US` light Rebuild；1280×720
视口中测得确认框 512×576，无横纵溢出且未显示 message key。临时截图未复制进仓库。验证结束后
Control Plane/Vite 端口关闭，带该 run label 的 PostgreSQL 容器移除。

固定 Bun `1.3.14`、uv `0.12.5`、Python `3.14.7` 后，contract standards 通过 156 个 schema、
2 份 OpenAPI/101 个 operation、383 个官方 case/1299 个 assertion 与 14 个单元测试；状态仍为
`ALL_GATES_OPEN`、独立评审 `PENDING`，不是正式 Gate closure。机器默认 Bun/uv/Go 版本不符合仓库
pin，因此未把默认工具链失败冒充契约失败或完整 composite SDK Gate 通过。

[机器可读结果](base-m1-sandbox-lifecycle-20260906.json) 固定上述 runtime、摘要、检查和边界。

## 尚未覆盖

本证据完成 BASE-M1 的手动 Stop/Rebuild、物理单写 fencing、Workspace 保留和对应 Admin 生命周期
切片，但不关闭 BASE-M1 或 BASE-ADMIN-V1。最早未完成项是 TTL 到期自动接受并完成同一 Stop 语义，
以及其 Admin 到期/恢复反馈。未部署、发布镜像、操作现有资源，也未运行 Kubernetes 或 outbound
客户节点；新增确认界面只检查了三个代表状态，不代替完整双语/双主题/桌面/移动视觉与可访问性矩阵。
