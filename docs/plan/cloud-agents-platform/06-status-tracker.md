# 06. 当前状态与下一项

## 0. 当前主线（2026-09-06）

当前产品决定：[ADR-0032 / D-055](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)。第一阶段完整交付基础设施＋Admin Web，之后才做用户 CloudAgents 对话。唯一工作顺序是 [04](04-extraction-and-migration.md)，完成定义是 [05](05-gates-and-acceptance.md)，Admin 功能/交互要求是 [07](07-admin-web-requirements-and-design.md)。

### 当前实施任务

- 2026-09-05 用户已明确把本任务从原 ADMIN-M1～M4 迁移到 BASE-M0～M5，授权当前仓库实现、相关验证与本地提交；采用固定 BASE-READY / BASE-ADMIN-V1。文档基线 `cde5bdb07117526af4fc4595d94b43a6a8ab7880`；不修改其他任务，不把旧 ADMIN-WEB-V1 未完成项标为通过。
- BASE-M0 已开始。固定 OpenSandbox server `v0.2.2` / source `207d94c7`、execd `v1.0.21` 和镜像 digest；真实 OrbStack Docker 29.4.0 linux/arm64 PoC 通过无 Provider 的 create/exec/Files、删除计算保留卷与重建读回摘要、缺失卷拒绝及精确清理。见 [候选核对与领域映射](evidence/base-m0-opensandbox-candidate-20260905.md)。
- 候选缺口已实证：相同创建输入产生两个 Sandbox；不存在的入口程序初始被接受为 Running，随后变为 Failed/exit 127。不能用创建响应替代 CP 幂等、Workspace 单写和就绪判断。当前仅技术 PoC，不是产品后端或 BASE-M0 联合验收。
- Go 运行时回执接缝已新增：`internal/opensandbox` 按完整归属/generation/摘要发现已有物理对象，重复或冲突回执 fail closed，清理前复核回执；标准 HTTP 限制重定向、响应大小和错误内容。固定 Docker 实测 14 项含 Go 发现、重复拒绝、旧 generation 拒绝及清理重放，卷/Files 摘要保留；见 [Go 接缝证据](evidence/base-m0-go-receipts-20260905.md)。该接缝现已接入下述持久化 Controller；原 PoC 仍只证明其固定技术边界。
- 接入前修复公共 ID 兼容缺口：128 字符/含 `~` 的合法 Identifier 采用 v2 双半 SHA-256 标签，发现与清理仍核验完整归属，不缩短公共契约。真实候选 14 项通过，见 [ID 回执兼容证据](evidence/base-m0-public-id-receipts-20260905.md)。未接通产品持久化链；新编码不承担旧 PoC 对象迁移。
- 首条持久化内核已落地：新 foundation profile 生成 Go/SQL 身份，000053 用独立 Workspace/Volume/Sandbox 表与既有 Operation/outbox/finalizer/Audit 原子接受意图。PostgreSQL 17.6 实测并发重放、冲突回滚、RLS、重启持久化和独占认领；Go 绑定完整解析后摘要。见 [内核证据与未覆盖项](evidence/base-foundation-intents-20260905.md)。SQL/数据库 fixture 不代表产品链路完成。
- 000053 已纳入版本化产品安装链：真实 runner 从 product-000052 精确升级一条到 000053，当时产品 CLI 全新安装 53 条并在重放时 no-op；两条路径均核对账本和新表。该冻结包现作为 000054 的升级输入保留。见 [产品迁移证据](evidence/base-foundation-product-migration-20260905.md)。未构建/发布镜像或部署；本机 `uv` 与仓库 pin 不符，因此不声称 umbrella contract suite 通过。
- 独立 no-Agent RuntimeProfile 与首条公开 Sandbox admission 已接通：product-000054 增加不可变 draft/published/disabled authority，生成 SDK、Admin/User HTTP 与 project/product scope 校验；User 只提交 profile ID/version，公开投影不含 Target/image/release/endpoint/凭据引用。PostgreSQL 17.6 使用真实签名 token 和生成客户端验证生命周期、普通用户 Admin 403、disable/replay、RLS 和 durable Operation/outbox；见 [API 与 authority 证据](evidence/base-runtime-profile-api-20260905.md)。该初始证据使用 ready Target SQL fixture；物理执行与 Admin 页面由下一条补齐。
- product-000055 已把 claim/renew/settle/reaper 接入生产启动的 Controller：真实 OrbStack Docker 中创建长期卷和 OpenSandbox，在首进程结算前退出后由第二进程回收 claim、严格 adopt 同一 runtime，并保留相同 Workspace 摘要；真实 Failed runtime 精确补偿，最终无 test-owned Sandbox/volume。生成 Admin Sandbox list/detail、服务端普通用户 403 和敏感字段排除通过；Admin Web 已提供真实 RuntimeProfile 与 Sandbox/Workspace 列表、详情及 Profile 生命周期，八种双语/主题/视口组合无溢出。见 [Controller 与 Admin 联合证据](evidence/base-m1-controller-admin-20260906.md)。本轮是本地 Docker/一次性 PostgreSQL 与本地 Web，不是部署、Kubernetes 或客户节点证据。
- 手动 Stop/Rebuild 垂直切片已接通：product-000056 以 generation/resourceVersion/精确资源确认接受持久化 Operation/outbox/Audit；真实 OpenSandbox Stop 删除计算并释放 writer、同一物理卷 Rebuild 后文件摘要不变，旧 generation 和外来卷 owner 被拒绝，普通用户 Admin API 为 403。Admin Web 用生成 SDK 和显式影响确认提交真实请求，202 只显示“请求已接受”。见 [Sandbox 生命周期证据](evidence/base-m1-sandbox-lifecycle-20260906.md)。本轮仍是本地 Docker/一次性 PostgreSQL 与本地 Web，不是部署、Kubernetes 或客户节点证据。
- TTL 自动停止垂直切片已接通：product-000057 用数据库时钟计算/判断 `expiresAt`，Controller 原子接受标准 Stop Operation，不另建物理删除路径；旧记录不回填 TTL，Rebuild 刷新期限。真实等待 60 秒后 generation 4 自动 Stop 删除计算并保留卷，generation 5 Rebuild 读回同一文件摘要；Admin API/Web 展示 TTL、到期时间与 manual/ttl 原因，Audit 和精确清理通过。见 [TTL 与恢复证据](evidence/base-m1-sandbox-ttl-20260906.md)。结合既有 Controller/Admin 和手动生命周期证据，BASE-M1 已覆盖当前固定退出范围。
- BASE-M2 首个同步 Exec 垂直切片已接通：Product API 只接受 Sandbox generation、bounded command 和 timeout；Control Plane 经服务端 user scope、PostgreSQL project authority 和完整物理回执执行固定 `/workspace` 前台命令，生成 Go/TypeScript SDK 与 CLI 同步更新。真实 OpenSandbox 返回 Workspace 摘要、stderr 和 exit 7；Admin 403、旧 generation 409、超 1 MiB 413，响应不含 endpoint/runtime/credential。见 [同步 Exec 证据](evidence/base-m2-sandbox-exec-20260906.md)。这不是 PTY/Files/访问网关或 BASE-M2 阶段完成。
- BASE-M2 PTY 垂直切片已接通：product-000058 持久化短期 Grant、PTY session 映射和签发/撤销 activity；独立固定路由 Access Gateway 逐请求及活跃连接重验 generation/expiry/revoke authority。真实 PTY 在 `/workspace` 执行，Gateway 重启与 cursor 重连无丢失，超过 1.1 MiB 输出时回放固定为 1 MiB；错误 token、跨 tenant、过期/撤销均拒绝，撤销关闭活跃连接。Admin API/Web 只显示 Grant 元数据并带 fencing/确认撤销，不返回 token、终端/文件内容或凭据。见 [PTY 与 Gateway 证据](evidence/base-m2-sandbox-pty-20260906.md)。本切片未做 Admin 浏览器视觉矩阵，不表示 BASE-M2 完成。
- BASE-M2 Files 垂直切片已接通：product-000059 将同一短期 Grant 固定为 PTY+Files，Access Gateway 提供 `/workspace` 相对路径的列表、1 MiB 版本绑定分页读取、16 MiB 写入和普通文件删除；路径穿越、symlink 穿越、错误 token、跨 tenant、超限和删除后读取均真实拒绝。真实文件在 Gateway 重启后按 offset/version 续读成功。Admin API/Web 仅显示 7 次尝试、2 次失败及最近 `NOT_FOUND` 等诊断元数据，不返回路径、文件名或内容；最终测试资源为 0。见 [Files 与 Admin 诊断证据](evidence/base-m2-sandbox-files-20260906.md)。本切片未做 Admin 浏览器视觉矩阵，不表示 BASE-M2 完成。
- BASE-M2 私有 Preview 垂直切片已接通：product-000060 持久化 Grant 下的显式活动端口，生成 SDK/CLI 只返回相对 Gateway 路径；Gateway 仅代理固定 OpenSandbox runtime/port，剥离 Grant、Cookie、候选 key 和用户伪造 forwarding 值。真实端口 3000 经 POST 方法/路径/query 转发，Gateway 重启后恢复；错误 token、跨 tenant、未注册/内部端口、端口/Grant 吊销和过期均拒绝，端口吊销关闭活跃流。Admin API/Web 只显示活动端口数字且普通用户为 403，最终测试资源为 0。见 [私有 Preview 与 Admin 端口证据](evidence/base-m2-sandbox-preview-20260906.md)。本轮未把 Foundation 尚未绑定的 Network Policy 显示成已执行。
- BASE-M2 短期 SSH 垂直切片已接通：同一 generation-bound Grant 返回固定 tenant/project/Grant username，短期 token 作为 password；独立 Access Gateway 以部署 host key 提供真实 SSH 协议，只允许一个固定 Sandbox `/workspace` session，拒绝 direct-tcpip、environment 和任意客户主机 shell。真实 SSH 执行、Gateway 重启重连、错误 password、跨 tenant、旧 generation、过期/吊销和活跃 transport 吊销关闭均通过；Admin 只显示 PTY/SSH 会话计数且原始响应不含 username、token、命令或输出。见 [短期 SSH 与 Admin 诊断证据](evidence/base-m2-sandbox-ssh-20260906.md)。
- BASE-M2 网络策略执行垂直切片已接通：product-000061 将规范化 direct allow targets 绑定到 no-Agent RuntimeProfile，Controller 下发后通过认证 sidecar 核对 exact policy 与 `dns+nft` 才结算。真实受限 Sandbox 只连通指定 sink，其他 Sandbox、Docker host/Control Plane 与 metadata IP 均阻断；`previewEnabled=false` 时新端口注册返回 403。Admin API/Web 管理 direct targets 并显示 Profile binding 与 Sandbox enforcement，不向 User API 暴露基础设施 authority；普通用户 Admin API 仍为 403。见 [网络策略执行与 Admin 证据](evidence/base-m2-sandbox-network-policy-20260906.md)。结合 Exec、PTY、Files、Preview、SSH 与 Gateway 证据，BASE-M2 当前固定范围已覆盖。
- BASE-M3 enrollment 垂直切片已接通：product-000062 持久化短期 RemoteWorker 注册意图、一次性 Secret 摘要、resourceVersion、幂等和 Audit；Admin API/Web 只管理元数据和撤销，独立 `remote-worker-bootstrap.act` CLI 路由一次领取 Secret 后拒绝重放。PostgreSQL 17.6 实测普通用户 Admin 403、Admin 领取 403、bootstrap Admin 读取拒绝、`no-store`、数据库无 Secret 原文及 create/claim/revoke Audit；见 [enrollment authority 证据](evidence/base-m3-remote-worker-enrollment-20260906.md)。尚无 CSR、mTLS 节点身份或 outbound 客户节点连接，因此 BASE-M3 保持进行中。
- BASE-M3 首条节点身份签发已接通：product-000063 以独立 enrollment Secret 鉴权，校验节点 CSR 后签发 15 分钟、server-owned SPIFFE SAN 的 mTLS client certificate；PostgreSQL 17.6 实测 000062→000063、全新安装/no-op、证书与本地私钥匹配、原响应精确重放、错误 Secret 401、Audit 和 Admin 元数据脱敏。生产 Control Plane/Helm 已支持配对 CA Secret 与 trust domain，CLI 在请求前预留新 0600 identity file；见 [CSR 与短期节点身份证据](evidence/base-m3-remote-worker-certificate-20260906.md)。尚无轮换/活动证书吊销或 outbound 客户节点连接。
- BASE-M3 活动证书轮换/吊销已接通：product-000064 由 TLS 校验后的当前节点证书授权轮换，PostgreSQL 保存当前/上一指纹并只允许上一证书对同一幂等键和请求摘要精确重放；Admin 沿用带 resourceVersion、资源名确认、幂等和 Audit 的撤销动作。PostgreSQL 17.6 实测 000063→000064、全新安装/no-op、真实 mTLS 轮换、旧证书非重放 401、普通用户 Admin 403、活动证书撤销后节点轮换 401，以及 Admin 只返回状态/时间而不返回证书或 Secret bytes；见 [轮换与吊销证据](evidence/base-m3-remote-worker-certificate-rotation-20260906.md)。本切片没有运行 outbound 客户节点或命令通道。
- BASE-M3 outbound heartbeat/节点健康切片已接通：product-000065 以当前短期 mTLS 证书接受五秒 heartbeat，在 PostgreSQL 持久化节点版本、能力、容量和 30 秒租约，并由数据库时间向 Admin 计算 online/degraded/offline；独立 Linux `cloud-agents-remote-worker` 进程真实出站上报且具备 1–30 秒退避重连，Admin Web 双语展示安全元数据。PostgreSQL 17.6 实测 000064→000065、全新安装/no-op、真实进程 heartbeat、重连客户端、轮换清除旧 incarnation 状态、401/409 负向路径、健康过期/恢复和 Admin 脱敏；见 [heartbeat 与节点健康证据](evidence/base-m3-remote-worker-heartbeat-20260906.md)。真实 harness 使用 `--once`，未模拟定时网络中断恢复，也尚无 command/Drain/Resume。
- BASE-M3 节点 Drain/Resume 命令切片已接通：product-000066 原子推进 server-owned desired generation，经 mTLS heartbeat 下发 30 秒 deadline command；RemoteWorker 以 `0600` 原子状态文件跨重启保存 observed generation/state、去重和 receipt，Control Plane 以 receipt 结算 Operation/Audit。PostgreSQL 17.6 与真实短生命周期进程实测 Drain、重启、Resume、精确请求/回执重放、旧 generation/resourceVersion、错误回执、过期失败和普通用户 Admin 403；Admin Web 使用生成 SDK 完成服务端 preview、精确 enrollment 确认和状态反馈。见 [命令与 Drain/Resume 证据](evidence/base-m3-remote-worker-command-20260906.md)。本切片只改变节点调度状态，尚未在客户节点执行 Workspace/Sandbox workload。
- BASE-M3 RemoteWorker Target 投影切片已接通：product-000067 为每个 enrollment 生成 server-owned `remote-worker` DeploymentTarget，数据库时钟与 mTLS heartbeat/certificate authority 驱动 unprobed/ready/offline/reconnect/revoked 状态；普通用户 Admin Target 读取 403，通用 Target Probe/Cleanup/Drain/Resume 均 409，避免绕过 RemoteWorker 生命周期。PostgreSQL 17.6 实测 000066→000067、fresh/no-op、真实 outbound 进程与投影状态；Admin Web 用生成 SDK 展示 placement target 并禁用错误入口。见 [Target 投影证据](evidence/base-m3-remote-worker-target-20260906.md)。本切片尚未向客户节点下发 Workspace/Sandbox workload。
- BASE-M3 outbound Sandbox create 切片已接通：product-000068 将在线、active 且具备 Docker capability 的 RemoteWorker 纳入 no-Agent RuntimeProfile publish 与 User Sandbox admission；heartbeat 只下发无 endpoint/credential 的持久化 `sandbox.create` attempt，节点使用本地 Docker/OpenSandbox 配置复用 Foundation executor，并以当前 mTLS 指纹结算 Audit。PostgreSQL 17.6 实测 000067→000068、fresh/no-op；OrbStack 上真实独立 RemoteWorker 创建 Running OpenSandbox runtime、保留 Workspace volume、重复两次回执，并精准清理至零测试残留。见 [客户节点 Sandbox create 证据](evidence/base-m3-remote-worker-sandbox-create-20260906.md)。
- BASE-M3 outbound Sandbox Stop 切片已接通：product-000069 复用同一 durable Operation/outbox 和 Foundation executor，经当前 mTLS 节点下发绑定旧 runtime、generation、spec digest 与保留卷的 `sandbox.stop`；结算同时校验 action/command/target/incarnation，成功后释放 writer 并允许精确回执重放。OrbStack Docker 29.4.0、PostgreSQL 17.6 与真实独立 RemoteWorker 实测物理删除、Admin `stopped`、保留卷、四条节点 Audit 及零测试残留；已有 Admin Web Stop 确认/Operation 流程直接适用并通过生成 SDK、33 项测试和构建。见 [客户节点 Sandbox Stop 证据](evidence/base-m3-remote-worker-sandbox-stop-20260906.md)。
- BASE-M3 outbound Sandbox Rebuild 切片已接通：product-000070 让既有 Admin generation/resourceVersion 围栏接受在线 active RemoteWorker，并下发仅绑定保留卷、不携带旧 runtime 的 `sandbox.rebuild`；客户节点复用 Foundation executor 创建新 generation runtime，回执按 action/attempt/Target/incarnation 结算和重放。OrbStack Docker 29.4.0、PostgreSQL 17.6 与真实独立 mTLS RemoteWorker 实测 Stop 前写入、同卷 Rebuild 后 SHA-256 不变、再次 Stop 及零测试残留；已有 Admin Web Rebuild 确认/Operation 流程通过生成 SDK、33 项测试和构建。见 [客户节点 Sandbox Rebuild 证据](evidence/base-m3-remote-worker-sandbox-rebuild-20260907.md)。
- BASE-M3 reverse Sandbox Exec 切片已接通：product-000071 在既有 User Exec contract 后按 PostgreSQL project/generation authority 排队，经当前 mTLS incarnation 的 outbound heartbeat 下发，客户节点复用 node-local OpenSandbox 在 `/workspace` 执行并结算 bounded stdout/stderr/exit/duration；Admin Exec 403，命令/输出表对 runtime role 不可读。OrbStack Docker 29.4.0、PostgreSQL 17.6 与真实独立 RemoteWorker 实测 exit 7、保留 Workspace SHA-256、请求/回执精确重放与冲突拒绝、证书/incarnation 绑定及最终零容器/卷残留。见 [客户节点 Sandbox Exec 证据](evidence/base-m3-remote-worker-sandbox-exec-20260907.md)。
- BASE-M3 reverse Sandbox Files 切片已接通：product-000072 保持既有 User Files API，经 generation-bound Grant、PostgreSQL authority 和当前 mTLS heartbeat 在客户节点执行 list/read/write/delete；1.9 MiB 写入跨越原 heartbeat 2 MiB 响应上限，Gateway 重启后按 version/offset 分页读回。错误 token/跨租户为 403，symlink 与 authority-mismatched receipt 为 409，删除后读取为 404；Admin 仅见计数/稳定错误且 runtime role 不可读路径或内容，最终零容器/卷残留。见 [客户节点 Sandbox Files 证据](evidence/base-m3-remote-worker-sandbox-files-20260907.md)。
- BASE-M3 reverse Sandbox PTY 切片已接通：product-000073 保持既有 User PTY API 与 Access Gateway 路径，经 generation-bound Grant、PostgreSQL authority 和当前 mTLS heartbeat 在客户节点交换 bounded WebSocket frames。真实 `/workspace` PTY 在 Gateway 重启后按 cursor 回放并继续输入；错误 token/跨租户/删除后访问为 403，authority mismatch 与 changed receipt 为 409。7 条命令均绑定 incarnation/certificate 并结算，runtime role 不可读帧，Admin 只见 1 个 session 计数，最终零容器/卷残留。见 [客户节点 Sandbox PTY 证据](evidence/base-m3-remote-worker-sandbox-pty-20260907.md)。
- BASE-M3 reverse Sandbox Preview 切片已接通：product-000074 保持既有 User Preview API 与相对 Gateway 路径，经 generation-bound Grant、Network Policy、PostgreSQL authority 和当前 mTLS heartbeat 在客户节点代理 bounded HTTP。真实 POST method/path/query/body 通过，credential/cookie/forwarding/`Set-Cookie` 均剥离；Gateway 重启、撤销、403/404/409 负向路径及 receipt 重放通过。2 条命令均绑定 incarnation/certificate 并结算，runtime 与 Admin 不可读请求/响应内容，最终零容器/卷残留。见 [客户节点 Sandbox Preview 证据](evidence/base-m3-remote-worker-sandbox-preview-20260907.md)。
- BASE-M3 RemoteWorker SSH 切片已接通：product-000075 复用既有短期 Grant、SSH Gateway 与 RemoteWorker PTY command table，在首次 node-local OpenSandbox WebSocket attach 后以 bounded stdin frame 驱动 PTY shell 和非 PTY exec；浏览器和 Control Plane 均不直连客户节点。OrbStack Docker 29.4.0、PostgreSQL 17.6 与真实 SSH 协议实测 `/workspace`、stdout/stderr、exit 7、Gateway 重启、PTY/pipe 双模式；缺少 `ssh` capability、错误 password、跨 tenant、direct-tcpip、environment 均拒绝，命令绑定当前 incarnation/certificate 且 runtime/Admin 不可读内容，最终零容器/卷残留。见 [客户节点 Sandbox SSH 证据](evidence/base-m3-remote-worker-sandbox-ssh-20260907/evidence.json)。
- BASE-M3 长时 claim 与断线重连切片已接通：product-000076 让执行中的 lifecycle command 通过现有五秒 mTLS heartbeat 每 20 秒续租既有 PostgreSQL claim；本地 `0600` 原子状态只记录当前已开始 command，进程重启不盲目重放。OrbStack Docker 29.4.0 与 PostgreSQL 17.6 故障注入实测 OpenSandbox create 阻塞超过原 claim、错误 command ID 返回 409、第二次续租连接被切断、原 command 不结算，claim 自然过期后仅以 attempt 2 对账执行；Workspace 生命周期及 Exec/Files/PTY/Preview/SSH 全回归通过并精确清理至零测试残留。见 [长时 claim 与断线重连证据](evidence/base-m3-remote-worker-reconnect-20260907/evidence.json)。
- BASE-M4 首个 Kubernetes Foundation 切片已接通：product-000077 让既有 Target/Profile/User Sandbox authority 接受 direct Kubernetes，Control Plane 以专用 ServiceAccount 创建并核验 owner-bound 保留 PVC，经固定 OpenSandbox server/controller 创建真实 BatchSandbox；既有生成 SDK 完成 Target 注册/Probe、Profile 发布、Sandbox create/Exec/Stop/Rebuild/final Stop，普通用户 Admin 403、Admin Exec 403、旧 generation 409，最终测试 Pod/BatchSandbox/命名空间/RBAC/容器均为零且 Workspace PVC 仅在测试命名空间生命周期内保留。OrbStack Kubernetes 1.35.6 与 PostgreSQL 17.6 的固定镜像、摘要和边界见 [Kubernetes Foundation 证据](evidence/base-m4-kubernetes-foundation-20260907/evidence.json)。
- BASE-M4 RemoteWorker 能力准入切片已接通：product-000078 扩展 heartbeat contract/生成 SDK，以数据库 authority 在 Profile 发布、公开列表、Sandbox 创建/重建和 Controller claim 统一校验 Docker runtime、amd64/arm64、`workspace-volume`、`network-dns-nft` 及 Profile CPU/内存和固定 20 GiB Workspace 磁盘下限；能力漂移时 User API 隐藏 Profile 且不下发命令。Admin Web 在既有节点详情显示同一 Foundation 基线和明确缺失原因。PostgreSQL 17.6 实测 fresh、000077→000078、no-op；真实 OrbStack RemoteWorker 对七类不兼容声明返回 409，恢复后完成 Sandbox/Exec/Files/Preview/PTY/SSH/Stop/Rebuild/Cleanup，最终测试容器和卷为零；见 [RemoteWorker 能力准入证据](evidence/base-m4-remote-worker-capability-admission-20260907/evidence.json)。当前架构值仅代表 worker 进程，容量按单 Sandbox 判定，尚不代表镜像 manifest 架构或聚合资源预留。
- 下一项：继续 BASE-M4，建立最小 Region/ResourcePool/Node authority 与 capacity placement，再补强隔离矩阵。
- 本切片未改既有 Agent/Lease/User Web 请求行为；无关 `.gitignore`、`go.work.sum`、`docs/img.png` 保留。当前无需要用户立即补充的凭据/权限；历史完整 Admin/Provider 验收仍按原范围保持未通过。

### 已完成的文档整合状态（历史，不重复执行）

- 当前文档位置：主项目 `/Users/huang/devel/project/huang/business/cloud-agents`，分支 `codex/cloud-agents-platform-p0`；整理来源为独立 worktree 的 `codex/foundation-first-docs-20260905` / `10541d6f`，执行时不再依赖临时 worktree。
- DOC-1 / DOC-2：VERIFIED（文档范围）；联合交付边界已统一，入口已精简，三组旧执行清单已归档，一份重复整理报告已删除。
- DOC-3：VERIFIED（静态文档范围）；检查结果见下文。不是 BASE 或正式 Gate closure。
- 复核后的五项修正已完成：限定 Synara 旧 CP 指代，区分单项任务与阶段完成，标注旧 Agent 部署配置，补齐按变更范围选验证的入口，将安装实测明确为 BASE-M5 退出条件；未扩展授权、降低联合验收或开始产品实现。
- 旧提示词引用新版 §15 的范围冲突已在文档中修正：原 ADMIN-M1～M4 采用 ADMIN-WEB-V1，新 BASE 主线采用 BASE-READY 与 BASE-ADMIN-V1；固定验收和显式任务迁移输入已补齐，未发送到其他任务或更新 Goal。
- DOC-4：VERIFIED（文档集成）；依据用户“再次确认目标一致吗，一致的我们开始合并代码”的新授权，以 `1c4a422b` 恢复被撤销的文档基线，再合并 `10541d6f` 的最新修订，无合并冲突。上次合并 `1a7878a2` 及撤销 `ed95007c` 的历史保留；本次不是运行代码实施、push 或其他任务迁移。
- 集成前主分支基线为 `7bd3f4f3`。11 份重叠文档草稿已按原字节及暂存状态备份到 stash `d9cca97ca32f4732656e087f6ca05053269ee062`，不覆盖回新版文档；`.gitignore`、`go.work.sum`、`docs/img.png` 保持原内容与未提交状态，既有代码与其他证据保留。
- 当前文档清单内的安全清理已完成，没有需要靠批量删除历史证据解除的阻塞。下一项产品实施是 04 的 BASE-M0；收到明确实现任务后推进，不再确认第一阶段是否包含 Admin Web。若仅继续审阅文档，则保持文档范围，不自动开始代码、真实节点或部署动作。
- 上述下一项只用于 BASE 主计划，不覆盖尚未迁移的旧 ADMIN-M1～M4 任务。04 的任务迁移提示词用于文档集成后的项目目录；合并文档不自动发送提示词或修改原任务/Goal，是否应用由用户明确决定。

### 产品阶段状态

下列状态仅针对新的联合切片，不表示仓内没有可复用实现。只有该阶段的真实基础设施行为、对应 Admin 闭环和检查均完成，才标记 VERIFIED；技术 PoC 不冒充产品完成。

| 阶段       | 状态        | 尚需证明的完成范围                                                                                          |
| ---------- | ----------- | ----------------------------------------------------------------------------------------------------------- |
| BASE-M0    | VERIFIED    | 固定候选、no-Agent Docker PoC、产品执行接缝、幂等 adopt、失败补偿及对应 Admin 运维投影已有固定证据；不表示底座产品已就绪 |
| BASE-M1    | VERIFIED    | 真实长期卷、跨进程恢复、失败补偿、手动与 TTL Stop/Rebuild、单写 fencing、Operation/Audit 和 Admin 保留/到期反馈已实测 |
| BASE-M2    | VERIFIED    | bounded Exec、PTY/Files/private Preview/short-lived SSH、Grant/Gateway、实际网络隔离及对应 Admin 管理已有真实本地 Docker/PostgreSQL 证据 |
| BASE-M3    | VERIFIED    | outbound enrollment/mTLS/轮换吊销、健康与离线拒绝、Drain/Resume、Sandbox lifecycle/Exec/Files/PTY/Preview/SSH、长时续租、断线重连与新 attempt 对账均有真实 Docker/PostgreSQL 证据 |
| BASE-M4    | IN PROGRESS | direct Kubernetes 与 RemoteWorker 单 Sandbox 能力/容量准入已实测；仍需 Region/Pool/Node 聚合 capacity placement 及强隔离矩阵/限制界面 |
| BASE-M5    | NOT STARTED | 快照恢复、独立交付、升级/回滚、usage/运维和完整 Admin 视觉/双语/权限验收                                    |
| BASE-READY | NOT STARTED | [05](05-gates-and-acceptance.md) 十二项全部满足，包括完整 Admin Web；不以 CLI-only、截图或旧 Agent E2E 替代 |
| APP-M1     | PAUSED      | 第一阶段完成后推进用户对话；现有 Agent 路径保留兼容并可作为回归负载                                         |

## 1. 实现与证据边界

整理时核对的历史源码基线 `40812b8945c8b53a8d71b6bc7ef7c757527bcaea` 已有生产 HTTP/JWT/JWKS 路径、租户/配额、直接 Docker/Kubernetes/SSH actuator、Worker health observer 和 User/Admin Web。该基线仍以 Agent/Lease 为核心：同步部署、随 Lease 清理 volume、入站 mTLS Worker 不等价于目标的独立 Workspace、生命周期 reconciler、outbound RemoteWorker。已有 [Agent M5 实测](../../../apps/user-web/M5-ORBSTACK-E2E.md) 与 [Network Policy 配套记录](evidence/admin-web-network-policy-20260905.md) 仅证明各自固定范围；本次未重跑运行时验收。

合并只核对相对主分支集成前 `7bd3f4f3` 的变更均为文档，不代表已重新验收该基线及并行改动的运行行为。后续进入实现任务时须重新固定该任务的 HEAD/dirty/backend/工具版本，只复用仍适用的结论；不用历史未验收推断当前代码不存在。

上述文档整合阶段只改变文档，未运行基础设施 PoC、生产服务、真实客户节点或 Provider E2E，未迁移旧卷、部署发布或关闭正式 Gate。后续已授权实现结果以本页“当前实施任务”为准。

## 2. 文档收口问题与处置

| 风险来源                                                      | 可能影响                                                                  | 本轮处置                                                                            |
| ------------------------------------------------------------- | ------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| 0031 / 01–07 / HTML 的“Admin 配套”                            | 后端先交付，把管理 UI 推迟到用户阶段                                      | 0032 明确基础设施＋Admin 是一个交付对象；每项管理能力需两端闭环                     |
| 多个 README、旧 06 的 `PAUSED` / “HTTP absent” / 旧 checklist | 选错下一步、重复实现，或把历史范围当作全局停止指令                        | 入口只导航，旧 06 归档；当前顺序在 04、状态在本页                                   |
| 04 的旧 P0～P6 和 07 的旧 ADMIN-M1～M4 链                     | 与 BASE 竞争；要求先完成用户 Agent 才做基础设施管理                       | 归档旧链；固定历史报告不变，适用迁移/安全条件继续可查                               |
| 多处重复源码、工具版本和发布状态                              | 局部过期时给出互相矛盾的操作建议                                          | CLAUDE 精简为入口与稳定约束；当前事实核对源码，工具版本依照可执行配置               |
| 上轮文档仍写“完成后合并”                                      | 在用户已撤销后再次自动集成                                                | 撤销历史保留；本次集成只依据用户复核后的新明确授权，不外推实现、发布或其他任务权限  |
| 把旧评审文件当作无用文档删除                                  | 生成器的路径存在性或 review SHA-256 校验失败                              | 保留冻结引用；仅删除不承担约束/证据/生成输入的重复报告，精确清单见 04               |
| 旧 M1～M4 提示词动态引用新版 07 §15                           | 原任务被追加新底座范围，或真实 Provider 验收被误删，导致无法完成/错误完成 | 使用独立的 ADMIN-WEB-V1、BASE-ADMIN-V1 标识；只在明确任务迁移时切换，不改旧证据结论 |

### 本轮实际检查

- 集成后的主项目文件盘点：387 份 Markdown/HTML，其中 2 份是 Vite 应用入口，不作为文档删除对象；未发现字节完全相同的文档副本。
- 主项目 385 份文档的 772 个本地链接、48 个锚点检查通过（排除代码块中的字面量链接）；应用入口的 `/src/main.tsx` 另按 Vite 项目根核对存在。
- 五项措辞修正及 CONTRIBUTING 中命令入口存在性检查通过；仅核对现有脚本定义，本次未运行 Go/Admin/数据库实测，不将文档校验当作运行验收。
- 两套验收标识与任务迁移入口检查通过；原 Admin 里程碑正文、新底座 13 项验收、Cleanup 交互、明确授权边界及正式 Gate 正文比对保持不变，旧真实 Provider 验收仍保留在 ADMIN-WEB-V1。
- `git diff --check`、架构 HTML 标签嵌套与 ID 唯一性检查通过。
- 已有 ADR、p0/p1/legacy/references/standalone、固定 Gate/E2E、契约、SQL、生成物、脚本和来源 manifest 未修改；生成锁直接列出的 3 份 Markdown 输入存在且原字节不变。
- Cleanup 资源名称/generation 确认，生产写入/部署发布/脏 worktree 批准条款，Daytona 固定基线、中英文与内容隔离要求保留。
- 仓内未发现 AGENTS.md 或 SKILL.md；本轮精简已有 CLAUDE 路由，没有修改全局 skills 或其他项目。
- 归档保留原正文并调整相对链接；旧 Admin 归档另追加来自用户原提示词的 ADMIN-WEB-V1 固定验收，不改原里程碑正文。删去的非 Gate 整理报告可从 `ed7d3ac5` 恢复；没有删除 Git 历史、stash 或 worktree。

## 3. 当前决定与按需历史

- D-055 / [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)：当前联合交付与文档路由，已接受用户明确边界。
- D-054 / [ADR-0031](../adr/0031-foundation-first-cloud-workspace-platform.md)：基础设施/应用分层仍适用；Admin 可后补的解释由 D-055 排除。旧文档合并授权不能覆盖用户撤销。
- [旧决策表与固定 Gate registry](history/06-status-tracker-20260905.md)：查询其他适用 ADR、原批准、固定候选和历史 Gate 状态时使用，禁止从其旧“下一步”选择当前工作。
- [旧迁移安全要求](history/04-legacy-migration-plan.md)、[旧 Admin 里程碑](history/07-legacy-admin-milestones.md)、[正式证据规则](evidence/README.md)：只在对应范围查询，不自动加载全文。

## 4. 更新规则

本页记录当前任务范围、阶段、已检查结论、下一项和真实阻塞；不复制长日志或逐提交历史。
BASE/APP 固定证据使用 source/ref、backend、输入、实际结果和未覆盖项；沿用既有报告位置。
正式 Gate 的 VERIFIED 仍需对应 closure、独立评审与签署；本轮文档、普通阶段报告和 BASE-READY 不自动关闭它们。
缺少新的实测证据时保持开放；不要求先有“通过”报告才能开始已授权的修改。
