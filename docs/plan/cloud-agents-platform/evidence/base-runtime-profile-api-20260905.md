# RuntimeProfile 与 Sandbox admission — 2026-09-05

Source base `5144a756d72be8db25dd457240d99a27b4329bda`，分支
`codex/cloud-agents-platform-p0`。本切片在既有 foundation intent、Operation/outbox、
project RBAC 和迁移生成链上增加独立的 no-Agent `RuntimeProfile` authority，未复用含
Provider 字段的 Agent `EnvironmentProfile`。无关 `.gitignore`、`go.work.sum` 与
`docs/img.png` 保持未提交。

Admin API 可创建不可变 draft 版本并执行 publish/disable；User API 只列出绑定当前
ready/active Docker Target 的 published 摘要，并以 profile ID/version 接受 Sandbox。
服务端解析 Target、固定 image/release digest 与 CPU/内存后，原子写入既有 Workspace、
Volume、Sandbox、Operation、outbox、finalizer 与 Audit authority。公开请求和响应不包含
Target、image、release、endpoint、`credentialRef` 或 `providerCredentialRef`。迁移
000054 撤销 runtime role 对旧可信输入函数的直接执行权限，只允许经 profile wrapper
进入。

## 实际验证

```sh
node scripts/test-foundation-product-migration.mjs
bun scripts/generate-platform-json-sdks.ts --check
bun scripts/generate-foundation-migration-package.ts --check
bun scripts/check-platform-migration-bundle.ts
bun scripts/generate-platform-migration-bundle.ts --check
bun run typecheck
bun test sdk/typescript/src/platform.test.ts scripts/lib/platform-release.test.ts
go test ./services/control-plane/internal/coordination \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/localmigration \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./services/control-plane/cmd/cloud-agents-product-migrate \
  ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1
```

OrbStack/aarch64 上的 harness 启动一个禁网、仓库只读挂载且带归属标签的
`postgres:17.6-bookworm`：

- product-000053 全新安装后精确升级一条到 product-000054；product-000054 全新安装
  54 条，重放为 no-op，账本和两个 bundle digest 一致；
- 使用真实 RS256 签名 token、生成 Go Admin/User 客户端、Control Plane HTTP handler
  与 PostgreSQL runtime/owner 角色；
- 验证 create → publish → public list → Sandbox accept → disable；disable 后相同
  idempotency key 重放得到同一 Operation，新请求被拒绝；
- 普通用户 token 调 Admin create 返回 403；原始 User 响应检查未出现基础设施字段；
- 数据库最终包含一条 pending Operation/outbox、一组 Workspace/Sandbox，并确认 runtime
  role 直接调用旧 `accept_foundation_intent_v1` 返回 PostgreSQL `42501`；
- harness 仅删除其归属标签匹配的临时 PostgreSQL 容器和匿名卷。

[机器可读结果](base-runtime-profile-api-20260905.json) 固定 backend、schema/digest 与检查
边界。生成物 currentness、TypeScript workspace typecheck、44 个 TypeScript 测试及上述
Go 包测试通过。

仓库 umbrella 检查未声称通过：`platform:contracts:check` 在执行前因本机 Bun `1.4.1`、
uv `0.6.0` 与 pin `1.3.14`、`0.12.5` 不符而停止；Go module umbrella 因本机
Go `1.27.1` 与 pin `1.26.6` 不符而停止。全仓 lint 仍停在未由本切片修改的 Daytona
视觉基线脚本 `capture-worker-health.mjs:141` 的 `no-console`。本切片未改 pin 或无关文件。

## 尚未覆盖

ready Docker Target 是 SQL fixture；没有启动 Controller 或物理 Sandbox，也未验证
OpenSandbox create/adopt/readiness、失败补偿、claim renewal、物理 writer fencing、
CP/Controller 重启恢复、长期卷实际挂载或 Admin 页面。因此这不是 BASE-M0/M1 或
BASE-ADMIN-V1 完成证据。未构建/发布镜像、部署环境、迁移生产数据或操作已有资源。
