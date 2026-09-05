# 通用 Sandbox 同步 Exec — 2026-09-06

Source base `2a3497f135e0341590b13da025e6b4bb0d64db69`，分支
`codex/cloud-agents-platform-p0`，执行时工作树为 dirty；无关 `.gitignore`、`go.work.sum` 和
`docs/img.png` 未修改、未暂存。本切片复用 product-000057、既有 Controller、长期 Workspace、
OpenSandbox 凭据目录和生成 SDK，不新增数据库迁移或依赖。

新增 Product API `POST /v1/tenants/{tenantId}/projects/{projectId}/sandbox-sessions/{sandboxId}:exec`。
请求只接受 `expectedGeneration`、UTF-8 command（1～8192 bytes）和 1～60 秒 timeout；工作目录固定
为 `/workspace`，仅同步前台执行，不接受 endpoint、credential、env、uid/gid 或 background 参数。
Control Plane 先校验 `sandboxes.update` scope，再由 PostgreSQL 用既有 `projects.act` 成员 authority
解析当前 Running generation、成功 Operation、有效 TTL、单写 Workspace、准确 runtime receipt 和
内部 credentialRef。浏览器/CLI 不接触 OpenSandbox endpoint 或凭据。

OpenSandbox adapter 在执行前再次核验完整 tenant/project/workspace/sandbox/operation/generation/spec
回执；只使用生命周期 API 返回的 execd endpoint 与临时 header。stdout/stderr 合计上限 1 MiB，
响应只返回 Sandbox ID/generation、exit code、stdout/stderr 与 execution time。固定候选把非零 shell
退出表示为 `error` SSE 后再结算 command status；adapter 因而查询同一 command ID 的终态并返回真实
exit code，不把普通命令失败误报为 Sandbox runtime 故障。

## 实际验证

```sh
bun scripts/generate-platform-json-sdks.ts --check
go test ./services/control-plane/internal/opensandbox \
  ./services/control-plane/internal/server \
  ./services/control-plane/internal/store/postgres \
  ./services/control-plane/cmd/cloud-agentsctl \
  ./services/control-plane/cmd/cloud-agents-control-plane
go test -tags localdev ./services/control-plane/internal/authn \
  ./services/control-plane/cmd/cloud-agents-control-plane
bun test sdk/typescript/src/platform.test.ts
bun run typecheck # cwd=sdk/typescript
bun run build     # cwd=sdk/typescript
bun test src && bun run typecheck && bun run build # cwd=apps/admin-web
bun test src && bun run typecheck && bun run build # cwd=apps/user-web
node scripts/test-foundation-controller-docker.mjs <new-output-directory>
```

契约标准检查使用仓库 pin 的 Bun 1.3.14、uv 0.12.5 和 Python 3.14.7：158 个 schema、2 份
OpenAPI/102 个 operation、当前 81 个 JSON Schema case、官方 383 个 case/1299 条 assertion、14 项
契约测试及 `ALL_GATES_OPEN` 通过；这是 non-Gate candidate，独立 review 仍为 PENDING。生成 SDK 检查、
Go focused packages、Go SDK、TypeScript SDK 的 28 项测试/类型检查/build、Admin Web 的 32 项测试/
类型检查/build，以及 User Web 的 23 项测试/类型检查/build 均通过。Admin build 保留既有 500 kB
chunk warning，本切片没有新增 Admin 页面代码或依赖。

真实 harness 使用 OrbStack Docker 29.4.0/aarch64、PostgreSQL 17.6、固定 source
`207d94c7dc7735c143856fe5c6538b743e478786` 及 digest-pinned OpenSandbox server/execd 和 Node runtime。
generation 3 上，Product API 从 `/workspace/controller-proof.txt` 读出的 SHA-256 与创建、Controller
重启、Stop/Rebuild 后的摘要
`69c204e27c471aa5cd8e06a074ebd08577ef5ddc2caf2c26800c4b8b95a8ebaa` 一致，并返回真实 exit code 7；
固定候选对该非零退出没有 `execution_complete` 事件，因此规范化 execution time 为 0 ms。Admin token
调用 Product Exec 为 403，旧 generation 为 409，stdout/stderr 合计超过 1 MiB 为 413。生成响应中未
出现 endpoint、runtime ID、credentialRef、providerCredentialRef 或 OpenSandbox header。

同一次 run 随后完整重放既有数据库 TTL Stop/Rebuild 回归，最终 test-owned runtime container、长期
Workspace volume 和 harness container 均为 0。[机器可读结果](base-m2-sandbox-exec-20260906.json)
固定 run、制品、状态码、摘要和证据边界。

## 证据边界

这是 BASE-M2 的第一个产品垂直切片：同步 bounded Exec 的 contract、Control Plane authority、生成
Go/TypeScript SDK、CLI、真实 Docker 执行和负向授权已接通。BASE-M2 仍为 IN PROGRESS；本证据不
覆盖 PTY/reconnect、Files、Access Grant/Gateway、私有 Preview、短期 SSH、实际网络策略或对应 Admin
Grant/Port/诊断页面。本轮未部署、发布镜像、操作现有资源，也未运行 Kubernetes 或 outbound 客户节点。
