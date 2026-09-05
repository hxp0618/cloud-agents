# Sandbox TTL 自动停止与 Workspace 恢复 — 2026-09-06

Source base `3cc5002a14b1c1eb5ed39b1a1f070e6cbb80da7f`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty；无关 `.gitignore`、`go.work.sum` 和
`docs/img.png` 未修改、未暂存。本切片没有新增依赖，复用 product-000056 的 Stop/Rebuild、
Operation/outbox/finalizer/Audit、Controller、OpenSandbox 回执和 Admin Sandbox 页面。

product-000057 为新 Sandbox 明确要求 `ttlSeconds`（60～86400），由 PostgreSQL 操作创建时间计算
绝对 `expiresAt`。旧记录保持 NULL，不会因迁移被自动停止；Rebuild 使用同一 TTL 刷新到期时间。
Controller 在现有 outbox claim 前用数据库时钟原子接受到期项，仍创建标准 `sandbox.stop` Operation，
不增加第二条物理删除路径。到期 activity 标记 `lifecycleTrigger=ttl`，并追加
`sandbox.ttl.accept` Audit；幂等键按 tenant/project/sandbox/generation/expiresAt 确定生成。

User API 只返回 `expiresAt`，请求只新增 TTL，不返回 Target、endpoint、image/release 或凭据引用。
Admin API 投影增加 TTL、到期时间和 manual/ttl 触发原因；Admin Web 用生成 SDK、`Intl` 和中英文文案
显示这些运维字段。浏览器仍不直接接触 Docker/OpenSandbox，管理员也不能读取 Workspace 内容。

## 实际验证

```sh
bun scripts/test-foundation-product-migration.mjs
bun scripts/test-foundation-controller-docker.mjs <new-output-directory>
bun scripts/generate-foundation-migration-package.ts --check
bun scripts/generate-platform-json-sdks.ts --check
go test ./services/control-plane/internal/coordination \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/foundationcontroller \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/localmigration \
  ./services/control-plane/cmd/cloud-agents-product-migrate \
  ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1
bun test sdk/typescript/src/platform.test.ts
bun test apps/admin-web/src
bun run --cwd sdk/typescript build
bun run --cwd apps/admin-web typecheck
bun run --cwd apps/admin-web build
```

PostgreSQL 17.6 实测 product-000056 精确升级到 000057、57 条 fresh install 和 no-op replay；
产品 schema bundle digest 为
`sha256:563f055691a573fb9fc882c0bc56c5ea7d74b7b477d01cebdfe69fbc31c9fa25`。
真实签名 Admin/User token、生成客户端、普通用户 Admin 403、public/Admin TTL 投影及 Controller DB
claim/settle 测试通过。

契约标准检查使用仓库 pin 的 Bun 1.3.14、uv 0.12.5 和 Python 3.14.7：156 个 schema、2 份
OpenAPI/101 个 operation、官方 383 个 case/1299 条 assertion、14 项契约测试及 `ALL_GATES_OPEN`
通过；独立 review gate 尚未执行。

真实 harness 使用 OrbStack Docker 29.4.0/aarch64、PostgreSQL 17.6、固定 digest OpenSandbox
server/execd 和 Node runtime。generation 3 Rebuild 在同一卷
`ca-ws-56af1bb2e0b01921e1a5966b8f69dcfe21e1790f824b32a20b63f247` 上刷新
`expiresAt=2026-09-05T20:22:42.762284Z`；脚本没有改写到期时间，实际轮询数据库时钟直到 60 秒 TTL
到期。Controller 随后自动接受 generation 4 Stop，删除 runtime
`09b0dccf-e52b-4c90-aa64-b817f13590b3`、释放 writer、保留同一卷，并核对数据库生成的请求摘要
与 Go lifecycle authority 完全一致及一条 `sandbox.ttl.accept` Audit。generation 5 Rebuild 读回文件摘要
`69c204e27c471aa5cd8e06a074ebd08577ef5ddc2caf2c26800c4b8b95a8ebaa`，与创建、Controller
重启和手动 Rebuild 前一致。旧 generation 与外来卷 owner 继续被拒绝；最终 test-owned runtime、
container 和 Workspace volume 均为 0。

[机器可读结果](base-m1-sandbox-ttl-20260906.json) 固定本次 run、runtime、卷、摘要、到期时间和边界。

## 证据边界

本证据完成 BASE-M1 最后一项 TTL 自动 Stop/保留/恢复链路，结合前两份 BASE-M1 证据覆盖该阶段的
长期卷、跨进程自动恢复、失败补偿、手动生命周期、幂等/fencing 和真实 Admin authority。新增 Admin
字段已通过真实 API、生成 SDK、32 项 Admin 测试、类型检查和 production build；本次机器锁屏，未新增
TTL 字段的浏览器截图，完整双语/双主题/桌面移动视觉与可访问性矩阵仍由 BASE-M5 / BASE-ADMIN-V1
收口。本轮未部署、发布镜像、操作现有资源，也未运行 Kubernetes 或 outbound 客户节点。
