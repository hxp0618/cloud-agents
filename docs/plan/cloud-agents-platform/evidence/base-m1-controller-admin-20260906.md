# Foundation Controller 恢复与 Admin 运维投影 — 2026-09-06

Source base `d4d0efd3f744ea1c760d4bfa4093697a148de1ee`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty。本切片复用既有 foundation
Operation/outbox、OpenSandbox 回执接缝、生成 SDK 与 Daytona `v0.190.0` Admin 壳层；无关
`.gitignore`、`go.work.sum` 和 `docs/img.png` 保持未提交。

product-000055 把 Sandbox Operation 的 claim、renew、settle 和过期 claim reaper 纳入版本化
安装链。生产 Control Plane 启动独立 Controller，由 PostgreSQL authority 认领工作；Controller
创建或严格 adopt 唯一 OpenSandbox 物理对象，等待 execd 真正 ready 后结算，失败时按完整回执
补偿。Docker Workspace volume 独立创建并带归属标签，运行时清理不删除长期卷。

Admin API 新增只读 Sandbox/Workspace 运维投影的 list/detail scope、生成 Go/TypeScript SDK 和
服务端 project RBAC。响应只含 opaque 归属、desired/observed state、generation、placement、
Operation、失败与重试元数据；不返回 endpoint、凭据、Prompt、Files、Artifact、image 或 release。
Admin Web 新增 Runtime Profiles 与 Sandboxes & Workspaces 的真实列表、详情和
RuntimeProfile create/publish/disable 流程，未增加前端状态框架或占位 CRUD。

## 实际验证

锁定 Bun `1.3.14`、uv `0.12.5` 和 Python `3.14.7` 后执行生成 currentness、contract
standards、官方 JSON Schema 套件和单元检查：154 个 schema、2 份 OpenAPI/99 个 operation、
383 个 case/1299 个 assertion 及 14 个单元测试通过。结果为 `ALL_GATES_OPEN`，但独立评审仍为
`PENDING`，本记录不是正式 Gate closure。

```sh
bun scripts/generate-platform-json-sdks.ts --check
bun scripts/generate-foundation-migration-package.ts --check
go test ./services/control-plane/internal/coordination \
  ./services/control-plane/internal/dockertarget \
  ./services/control-plane/internal/opensandbox \
  ./services/control-plane/internal/foundationcontroller \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/localmigration \
  ./services/control-plane/cmd/cloud-agents-product-migrate \
  ./services/control-plane/cmd/cloud-agents-control-plane \
  ./sdk/go/gen/platform/v1alpha1 ./sdk/go/gen/openapi/v1alpha1
go test -tags=localdev ./services/control-plane/internal/authn \
  ./services/control-plane/cmd/cloud-agents-control-plane
node scripts/test-foundation-product-migration.mjs
node scripts/test-foundation-controller-docker.mjs
bun test apps/admin-web/src
bun run --cwd apps/admin-web typecheck
bun run --cwd apps/admin-web build
```

PostgreSQL 17.6 的迁移 harness 验证 product-000054 精确升级到 000055、55 条全新安装和重放
no-op；生成 Admin/User 客户端覆盖 RuntimeProfile 生命周期、Sandbox admission、claim
renew/retry/recovery/exhaustion/settlement、普通用户 Admin 403、RLS 与直接 authority 绕过拒绝。

真实 Controller harness 使用 OrbStack Docker 29.4.0/aarch64、固定 digest 的 OpenSandbox
server/execd/runtime：第一进程创建物理卷和 Sandbox 后在结算前退出；第二进程回收过期 claim，
严格 adopt 同一 runtime，没有重复创建，并读回相同 Workspace 摘要
`69c204e27c471aa5cd8e06a074ebd08577ef5ddc2caf2c26800c4b8b95a8ebaa`。另一路径使用真实
Failed runtime 验证精确补偿。最终 test-owned Sandbox 和 Workspace volume 均为 0。

Admin 视觉 harness 启动一次性的真实 PostgreSQL、Control Plane、Worker 和 Vite proxy；经
真实 API 注册 Docker/Kubernetes/SSH 三种 Target，创建并发布 RuntimeProfile，再经 User API
接受 Sandbox，Admin API 读回一条运维投影。`zh-CN`/`en-US` × light/dark × desktop/mobile
八种 foundation 组合都通过；整套 capture 产生 145 张临时截图和 145 项 layout 检查，溢出为
0，缺失 message key 为 false，Bearer 未持久化，普通用户得到 12 个服务端 403。capture 为验证
已有错误 UI 故意触发 36 次 quota 404 和 8 次 cleanup-preview 503；这些网络错误不表示控制台
零错误。截图只在 `/tmp`，未复制进仓库。

[机器可读结果](base-m1-controller-admin-20260906.json) 固定 runtime、digest、检查结果和边界。
Admin Web typecheck、31 个测试和 production build 通过；build 仅保留既有 589.37 kB chunk 大小
warning。临时开发栈、Controller Sandbox、归属卷均已精确清理，4174/18085/18095 端口关闭。

## 尚未覆盖

本证据关闭 BASE-M0 的执行接缝技术阶段，并证明 BASE-M1 的真实物理卷、持久化 Controller
恢复和 Admin 运维元数据切片；不等于 BASE-M1 或 BASE-ADMIN-V1 完成。尚未交付 stop/TTL、
同 Workspace rebuild、物理 writer fencing、越权挂载负向路径，以及带影响确认、Operation 和
Audit 的 Admin 生命周期/数据保留操作。视觉环境中的 Docker Target ready 状态是 SQL fixture，
不是本轮 Target Probe 证据。未部署、发布镜像、操作现有资源，也未运行 Kubernetes 或 outbound
客户节点路径。
