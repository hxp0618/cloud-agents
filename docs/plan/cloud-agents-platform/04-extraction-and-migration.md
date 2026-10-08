# 04. 基础设施、Admin Web 与 Anywhere Runtime 实施计划

本文件只维护实施顺序、切片范围、兼容迁移入口和 Goal 提示词。实际结果只记录在 [06 当前状态与最终汇总](06-status-tracker.md)，第一阶段验收证据见 [phase-1 验收](../../acceptance/phase-1.public.md)。

## 文档清理与执行计划

这是当前唯一的实施计划：文档职责收口 → 基础设施与 Admin Web 联合交付 → Anywhere Runtime/SDK → 后续用户 CloudAgents 对话。已完成切片不重复执行；重新开启任务时必须以新的明确范围和当前源码为准。

<a id="identity-v1-plan"></a>

### 账号登录、租户与角色：IDENTITY-V1（2026-10-08）

本节替换此前未分期的管理员登录草案。依据 [ADR-0033](../adr/0033-built-in-identity-service.md)、[05 验收](05-gates-and-acceptance.md#identity-v1) 和 [07 页面与范围](07-admin-web-requirements-and-design.md#identity-v1)，本次只交付 P0 文档；须用户明确批准 P0 才能实施运行时。既有 BASE/P1 授权、其他并行切片或文档写完均不跨越此边界。实际状态只写入 [06](06-status-tracker.md)。

| 阶段 | 完整切片范围 | 退出条件 |
| --- | --- | --- |
| P0 决策与文档 | 新 ADR 扩展内置身份范围和 ADR-0025 外围适配器职责；更新 01/02/05/07 与 SECURITY，保留冻结 ADR/profile 原字节 | 文档链接与一致性检查通过，用户批准具体决策；批准前不得开始 P1 |
| P1 契约、数据与身份核心 | 在 contracts 定义 login/logout/session/me 与 my tenants、tenant token、邀请、邮箱策略、账号关联和活性检查，生成 SDK；identity schema 受限函数、Argon2id、账号/IP 限流锁定、管理员重置、稳定账号 bootstrap、JWKS 自动刷新与 CP 在线撤销校验 | 小型核心失败回归转绿；错误密码/锁定、租户切换、新旧 token、跨租户拒绝、用户禁用/会话撤销、邮箱策略、审计脱敏及真实 DB 函数/RLS 拒绝通过；新迁移仅隔离库验证 |
| P2 Web 登录与范围切换 | 先为测试环境配置可信 HTTPS；扩展现有 server.mjs 为会话代理；Admin/User 分离 cookie 与用途，密码登录页、自动受权租户和项目下拉；同切片删除手填 ID/token 入口与调用方 | tester-army/e2e 完成登录落租户、平台/租户管理员可见性、User session 访问 Admin 拒绝、篡改路径/多 tab 切换拒绝串租户、浏览器无服务 token；TLS/CSRF/退出与后端状态核对通过 |
| P3 管理功能 | 成员、邀请及受限角色选择、邮箱域设置、账号密码重置/全局禁用（平台管理员）、租户成员停用（租户管理员）、个人关联登录/改密和审计页面 | 完整 Admin 流程与真实后端一致；跨租户账号操作拒绝，邀请过期/重放/并发消费、策略变化与权限撤销负例通过；关联登录的外部 provider 验收随 P4 完成 |
| P4.1 标准 OIDC | discovery、code + PKCE、state/nonce、ID token 校验；平台管理员配置 provider 与 secret reference | Keycloak E2E：登录、关联/解绑、未知账号拒绝、邀请接受及信任负例通过 |
| P4.2 GitHub / GitLab | 复用核心账号/会话流程，按各 provider 的 OAuth/OIDC 与邮箱验证语义适配 | 两个 provider 分别完成上述流程，不能用一个 provider 结果代替另一个 |
| P4.3 飞书 / 钉钉 / 企业微信 | 各自协议、稳定 subject、受限企业/组织与显式可信邮箱策略；缺失邮箱不能接受邀请 | 三个 provider 分别验证登录/关联/解绑/未知拒绝/邀请接受，覆盖邮箱缺失与不可信；未配置真实 provider 的单元保持 BLOCKED/NOT RUN |
| P5 CLI、自动化与部署 | cloud-agentsctl 浏览器登录及 loopback callback；独立 service-account token 用于自动化/E2E；Compose/Helm 身份容器、签名密钥引用、Web TLS；指定既有部署切换登录并停止人工 mint | CLI callback/state/PKCE 与服务账号最小权限、部署/密钥轮换/重启恢复通过，完成聚焦安全审查；既有部署写入及数据处理另经明确授权 |

token 默认 15 分钟，由 Web server 续签；空邮箱域列表允许任意已验证邮箱，平台管理员仅自身管理访问豁免；TOTP/WebAuthn、SCIM、会话管理页面延后。邀请创建、成员授权和撤销不能延后到页面阶段；P1 提供核心，P3 完成管理闭环。P2/P3 的密码验收不提前宣称 P4 联合登录已完成。

先按当前源码盘点 token 输入、存储、代理、CLI、E2E/fixture 与核心测试调用者，不把提案中的文件数当成固定清单。复用现有核心安全回归和生成链；Web 普通流程迁移至 tester-army/e2e，自动化走 service account。替代流程通过后再删旧覆盖，按相同范围统计测试维护文件和行数，搬移不计减少。实现当前契约，不保留粘贴 token 的历史兼容入口；不自动改写现有数据库、旧 subject 绑定或冻结证据。

### 当前代码与测试精简（2026-10-08，第三轮）

按 [CLAUDE.md](../../../CLAUDE.md) 的单一规则入口执行。前两轮已完成切片和结果只记录在 [06](06-status-tracker.md)，不重复执行；本轮从 510 个测试维护文件 / 141394 行继续，追踪 SDK 事件流、Runtime 进程生命周期和 User Web 消费链，只实施有调用证据的有限切片。

| 切片 | 已确认问题与最小处理 | 必要验证 |
| --- | --- | --- |
| Runtime 客户端 stdout 结束 | child 关闭 stdout 但进程继续运行时，完整帧结束路径没有关闭客户端，在途请求可永久等待。先按现有消费顺序处理已收到帧，再复用协议失败/进程回收路径拒绝未完成请求；不提前丢弃已收到的 terminal。 | 真实子进程最小失败回归，完整帧与异步确认顺序、相关 stdio client 用例及类型检查 |
| SDK 事件页路径身份 | TS User/Admin 与 Go events 入口验证事件结构后，未核对 metadata.projectId/sessionId 与请求路径，而消费端直接合并或输出事件。生成模板复用既有路径/body 身份拒绝规则，按完整页验证后才交给消费者。 | SDK 公开调用参数化验证匹配页与 project/session 错配；修改模板后重新生成，相关 TS/Go 测试、vet 和生成一致性 |
| User Web 未决提交恢复 | HTTP 等待取消/超时后，poll/refresh 已读取到服务器接受的 Execution，pendingSubmission 仍锁住表单与会话选择；初始化 effect 的清理分支不在实际恢复路径。共享精确 session/turn/execution 身份确认，成功恢复时收尾原提交，缺失/错配继续保留重试身份。 | 复用核心恢复夹具验证匹配、缺失与错配；检查 poll/refresh/原提交的顺序和取消边界，User Web 类型/构建/相关用例；无后端时不宣称浏览器恢复流程已验收 |

已确认的测试整理：`worker-oci-installed.test.ts` 的临时目录都由现有 `createFixture` 创建，却在每个用例重复清理。将 Vitest `onTestFinished` 登记放在该夹具创建处，删除纯清理的重复 try/finally；初始化抛错也能清理。保留 package/lock/架构/许可/篡改及符号链接断言与故意删除夹具成员的操作，仅执行对应测试文件，不运行 OCI 构建或旧供应链矩阵。

统一按原口径统计测试、E2E、fixture/helper 和生成测试，搬移不计减少；最终只在 06 更新简短结果。本轮不提交、合并、推送、发布、操作生产、删除现有数据或关闭 Gate。

### 进程与连接资源回收（2026-10-08）

代码审查确认的四个回收缺陷，与上方三轮及六项运行路径优化分开实施，不触碰其文件：

| 切片 | 已确认问题与最小处理 | 必要验证 |
| --- | --- | --- |
| Codex 强制终止 | `settleTurn` 清除 force-kill 计时器却保留字段，之后 `scheduleForceKill` 直接返回；中断后忽略 SIGTERM 的 codex 进程永不收到 SIGKILL。清除时同时置空字段。 | 忽略 SIGTERM 的真实子进程先红后绿，既有 interrupt 用例 |
| Runtime stdin 背压中止 | 等待 `drain` 时中止使 `terminal` reject，catch 删除刚建立的 abort tombstone，迟到帧随后触发全局协议失败；`drain` 监听不移除。等待只观察 terminal 结束，不把中止当写入失败，并在结束后移除监听。 | stdin 背压下中止后迟到帧不再拆除 Runtime 的最小回归，相关 stdio client 用例 |
| Runtime 管道错误 | child stdin/stdout 与 skill 归档 pipe 的 stdin 无 `error` 监听，子进程先退出时 EPIPE 成为未处理事件。复用既有协议失败/流销毁路径处理。 | 子进程提前退出后写入的最小回归，skill sandbox 用例 |
| Go HTTP 连接回收 | RemoteWorker 每个 OpenSandbox 命令新建 transport 且不关闭；三个 actuator 丢弃 Supervisor 前不调用现有 `CloseIdleConnections`；证书轮换替换客户端不关闭旧连接。在使用结束处关闭空闲连接，不缓存凭据读取结果。 | 受影响包 Go 测试与 vet |

同一审查的第二批，仍不触碰同期任务文件：

| 切片 | 已确认问题与最小处理 | 必要验证 |
| --- | --- | --- |
| MCP Broker deadline | undici 以 WeakRef 关联 fetch signal 与内部 Request 控制器，响应头返回后该控制器可被 GC，10s deadline 的 abort 不再到达 body，在途 tools/call 挂起至客户端超时。Broker 自身以 abort 竞速 fetch 与每次 body 读取并取消 reader；shutdown 直接中止同一控制器。 | 既有 deadline 用例即失败回归（强制 GC 可在 1s 复现），Broker 全部用例 |
| Provider 后台中止 | Pi `session.abort()` 与 DeepSeek `harness.close()` 在 interrupt/forceStop/未知结果路径以 `void` 丢弃 promise，拒绝成为未处理异常。忽略后台拒绝，结果仍由原 run 路径决定。 | 既有 stub 改为拒绝后先红后绿 |
| SDK Session/Execution 解码 | TS 导出解码器拒绝 `mcpServerRefs/skillBundleRefs`，客户端另用私有包装解码，包装的分页不校验 apiVersion/kind 与 200 上限。导出解码器直接解析能力引用（与 Go SDK 一致），客户端改用导出解码器并删除 4 个包装。修改模板后重新生成。 | 公开解码与客户端错 kind 分页的最小回归，SDK 测试、类型和生成一致性 |
| Admin RemoteWorker 幂等键 | 注册面板的创建、调度切换、吊销每次重试生成新幂等键，超时后重试可能重复执行。复用 `adminMutationKey`/`pendingIdempotencyKey`，按操作与完整 body 保留键，成功后清理。 | Admin 类型与现有用例；无组件测试设施，不新增依赖 |
| Go 未使用代码 | staticcheck U1000 报告的 7 处未引用函数/字段（含两个测试 fixture 与一个断言 helper）。直接删除。 | build、vet、U1000 与受影响包测试 |

第三批为行为保持的去重：OpenSandbox 客户端 10 个 execd/policy 请求重复构造请求、复制 header、发送并把非 context 错误脱敏为 `ErrUnavailable`，以及重复的有界 JSON 读取与响应体排空；合并为私有 helper，各调用保留原状态码、404 映射、尺寸上限、附加 header 与事后核验。RemoteWorker 四个 sandbox 执行器共用“凭据目录→客户端”构造。不改动 `ProxyPreview`（请求错误映射与 header 合并不同）和各 stable error 码表。runner-ledger 五个注册库的私有 helper 重复虽已确认，但这些文件是 generation lock 的哈希输入，修改会改变锁与冻结 review 绑定，另行决定；迁移版本集合已由生成器派生，仅剩的并行查找属安全选择路径，收益不足，不处理。验证：opensandbox 与 remote-worker 包测试、vet、gofmt。

### 执行范围

八项修复后的继续检查只处理相邻调用链上新复现的问题：为 claim heartbeat 的单次数据库续租设置短于租期的有限期限，超时按 claim failure 停止旧 Runtime，同时保留 heartbeat 总生命周期到终态结算的语义；Capabilities 刷新时 MCP/Skill 局部错误不再短路 Session/Execution 元数据，独立应用成功结果后报告错误；Sandbox reconcile 后复用当前 Session/Execution 页和选择；Provider 命令回执绑定完整命令身份，拒绝同 ID 异内容，内部 close/abort 不再派生可碰撞或超长的 ID；终态回执增加总字节预算，释放旧 payload 后保留有界的执行记录，重复旧命令必须拒绝或返回已有回执，不能再次执行；容量耗尽后为中断和关闭保留有界处理能力，Provider 关闭失败继续释放本地资源并向 Runtime 传播。沿用文档、最小失败回归、实现和受影响验证，不扩展到发布或真实环境写入。

本轮继续修复审查确认的八项：统一 Provider 会话历史写入与恢复边界；将长执行与普通 API 的并发容量分离；为 checkpoint、终态结算和取消设置有限数据库操作期限；Runtime Session 清理与事件泵共用有界清理期限并传播失败；Admin 后台轮询按资源状态加载并隔离错误；Capabilities 刷新同步读取绑定元数据；Admin Session 增加 Sandbox 服务端筛选并与 Execution 按需分页，调用方和生成 SDK 同步更新；复用 transcript 校验、编码和摘要，保留逐帧持久化。文档、失败回归、最小实现和清理依次进行，保持历史完整性、tenant/cursor scope、fencing、资源释放和恢复语义，不默认放宽 RPO；仅运行受影响检查，结果记录在 06。

当前授权的六项运行路径优化先沿用现有实现补最小失败回归：运行中 HTTP 消息只使用一份完整 transcript；claim 续租持续至终态原子落库完成；Provider 入口与持久化读取共用会话历史结构和尺寸规则；Admin 恢复/能力事件按需分页，正常历史增长不触发全页失败；Admin 按当前页面加载必要资源并隔离各资源读取错误；User Web 只保留界面展示所需的事件尾部，空批次复用原状态。保留 fencing、异常游标拒绝、服务端历史及现有授权边界，不引入框架或新依赖。验证按 Go、Provider、Admin/User Web 的受影响核心回归和类型/构建检查执行，真实环境与正式 Gate 分开报告。

| 顺序 | 工作与精确范围 | 完成条件 |
| --- | --- | --- |
| DOC-1 | 按 [ADR-0032](../adr/0032-infrastructure-admin-delivery-and-document-routing.md) 统一产品边界、授权识别与文档职责 | 所有活动入口都把基础设施＋Admin Web 作为第一阶段；文档集成依据用户最新明确合并授权，不外推代码实施或部署权限 |
| DOC-2 | 精简根/计划 README 与 CLAUDE；删除旧固定状态、旧迁移步骤、旧 ADMIN 里程碑和重复收口报告 | 默认入口没有第二套当前顺序、陈旧暂停指令或重复源码清单；历史约束可查但不自动加载 |
| DOC-3 | 核验本轮 diff、活动文档本地链接/锚点、HTML 结构、安全条款、被引用文件存在性及冻结输入字节 | 不修改契约/SQL/生成物/运行代码；只把实际通过的检查写入 06，不声称 runtime 或 Gate 验收 |
| DOC-4 | worktree 复核后按用户最新明确授权集成当前分支 | 重叠草稿先备份，无关未提交改动、运行代码和历史提交保留；核对完整文档 diff 与引用，只提交相关路径，不 push；实际结果只记录在 06 |
| BASE-M0～M5 | 下文的基础设施＋Admin Web 联合切片 | 对应后端、Admin 操作/状态/失败恢复、安全与真实验证同时达标；逐项完成 05 的 BASE-READY |
| APP-M1 | 先交付 Anywhere Runtime/SDK，再承接完整用户对话、任务、审批、历史和结果 | BASE-READY 后按 §0.4 推进；Runtime 子范围采用 ANYWHERE-RUNTIME-V1，不改写 BASE 完成条件 |

### 开源整理（2026-09-29）

第一阶段结束后，为开源合并到 `dev` 删除了过程材料：P0 基线与清单、旧计划/旧状态（history）、legacy 方案、逐轮 evidence 报告、原始日志与截图、应用下的 E2E 流水报告和未被引用的视觉对比脚本。仍被生成锁、closure profile 或 review digest 按字节绑定的 ADR、`p1/`、`standalone/` 与 `evidence/G-*` 记录原样保留。旧迁移计划中仍有效的安全条件移至 [数据迁移、删除与回滚安全要求](migration-and-rollback-safety.md)。

第二轮整理删除了未被 CI 使用的 evidencefs/mount authority 包、migration bundle successor 生成器、G-CONTRACT v3 successor/phase 工具链、closure-profile v4 与 Daytona 参考截图；Admin 视觉几何基线和截图脚本移至 `test/e2e/admin-visual/`。

以后删除任何文件，先列精确文件、替代入口和反向引用（生成锁、CI、生成器、测试 fixture）；被字节绑定的文件只能随生成器变更一起移除。

Worker OCI 声明必须区分直接包来源声明与构建实际消费的锁文件。先在现有供应生成器中拒绝非规范或长度不符的 SHA-512 SRI，不能因 authority 与锁文件包含同一个错误值就认定有效；错误输入保留为 `NOASSERTION` 与明确阻塞原因。独立 npm lock 的安装与 CLI 等价性验证通过后，再按完整生产者/消费者边界接入构建；未验证的 apt、传递依赖、许可证和镜像证明仍保持开放。

当前后继构建切片将已验证的 Worker 专用 npm lock 接入实际 `npm ci`，由同一锁输入生成供应声明，并核验镜像内安装版本、入口与声明一致性。execd 后继使用固定上游源码、许可、最小补丁及构建输入，在仓库内提供可复现入口；只有该入口产出的镜像通过真实 Foundation 恢复验收后才调整相应默认绑定，不能把机器上的临时镜像 ID 当成公开可获取的镜像。两项均保持构建与发布授权分离。

Worker 后续安装清单必须在真实镜像内按架构采集：物理 npm 包目录与 hidden lock 双向一致，并与仓库 lock 的版本、来源和完整性字段核对；记录实际 package.json、许可证文件、dpkg 已安装包与 copyright/common-license 字节。现有 OCI smoke 重新采集并比对镜像内清单，通过独立 receipt 绑定本地 immutable image ID。安装记录一致性、许可证文件存在性与完整供应链资格分别报告；缺少文本、apt 下载来源、基础镜像非 dpkg 内容、原生运行与最终发布 digest 的证据不从安装清单推定通过。

缺失的许可证材料在现有 Worker package authority 中按精确包路径、版本与 SRI 绑定；可核验的上游原文单次保存，来源限定为固定源码提交或完整性匹配的 npm tarball 成员。额外材料随 deployment 进入镜像文档目录，collector 单独记录，不能伪装成 npm 包原有文件或法律批准。已嵌入 README 的许可正文通过同一 authority 的精确成员路径和摘要识别，不用宽泛文本匹配放宽检查；来源、版本、路径或字节漂移均拒绝。

原生包的安装字节须沿用同一 authority，先验证精确 npm tarball SRI，再绑定其全部普通文件的相对路径、大小与摘要。安装声明 v2 使用 packageBindings 同时承载许可证与可选 packageFiles 闭集；collector 对已安装且声明闭集的包拒绝缺失、多余、软链接、大小或字节漂移，并单独报告包内容结果。Sharp 的平台 addon 与 libvips 包一起核对；组件版本表只作为发布者声明，与源码构建版本表一致也不能替代二进制到对应源码的构建证明。若 npm 发布物提供 SLSA/provenance，另行核验 trust subject、构建来源和签名关系；该证据不能替代 cargo update 后最终 Cargo 图、实际 link/static 成员、生产源码成员覆盖或二进制到对应源码的构建证明。不得把版本表、SLSA/provenance 或二进制计作许可证材料。

原生组件许可原文按实际 Linux 构建配方选择，保留源码中对应的版权和通知文件，不从包版本表猜测完整组件集合。对确需逐文件证明许可选择的 native 源码，复用已有完整 Apache-2.0 正文，并用既有 authority 将实际选择与生产源码成员（包括 archive_blake2* 成员）绑定，不新增重复正文或框架；材料范围只覆盖实际分发组件所需的许可、notice 和源码证据，已证明不适用或重复的材料移除，不把逐文件绑定扩展成强制框架。heif 的 LGPL-3.0/GPL-3.0 正文已经交付，剩余证据是组件映射与源码义务，不能由正文交付替代。非 npm 源码归档成员沿用同一 supplemental authority，以 HTTPS 来源、归档 SHA-256 和安全成员路径绑定；纳入前核对完整压缩流、成员类型和文本字节。归档观察摘要、发布者校验和、实际二进制构建证明分别记录，不能互相替代；材料随既有生成、归档和镜像链交付，完整 native/apt/base 资格仍按缺失证据保持开放。

完整 native successor 必须分别保留 npm SLSA/trust subject、npm lock/SRI、cargo update 后 Cargo 图、link/static 成员、生产源码成员绑定和工具链/base 输入记录；这些证据相互独立，不能合并为单一版本声明。已核实的固定包组件声明差异与缺失构建证据写入现有 package authority 的 blockedReasons，由同一生成器并入已有 manifest/notice 阻塞原因；保留上游原文与包字节，不把说明差异当作完整清单修复。当前 HEAD-only 在进入授权 commit 前，必须先形成经审查的完整源码、生成器与生成 authority/产物批次；批次稳定并获授权后再创建或复核 clone，不能以缺少候选输入的 HEAD-only 结果推定可复现。

受控 native 后继先核验固定 librsvg 归档在原配方 feature 修改与安全补丁后的 Cargo 锁是否仍有效；有效则复用原锁，否则只由 Cargo 生成必要变化。源码准备入口为 `python3 -B scripts/prepare-sharp-libvips-successor.py --sharp-archive <cached-archive> --librsvg-archive <cached-archive> --librsvg-security-patch <cached-patch> --librsvg-vendor-dir <cached-vendor> --sources-dir <cached-sources> --python-wheelhouse <cached-wheels> --rust-dist-dir <cached-rust-dist> --cargo-c-dir <cached-cargo-c> --builder-rpm-dir <cached-rpms> --output-dir <new-directory>`（Python 3.12+）；它读取 `tools/sharp-libvips-successor/v1/source.json` 的固定摘要，复用已有安全归档检查，仅从已核验的本地输入准备配方，不联网或覆盖已有目录。构建入口不得执行无约束依赖更新，真实 Cargo 消费须锁定依赖并拒绝漂移；配方随产物保留目标过滤的 workspace 解析图及同一 Cargo 锁，实际静态链接成员仍须另行捕获。该源码准备与锁解析仅证明后继输入，不补写既有 ELF 的最终构建图或关闭原生资格。 Linux glibc 后继还须将其余直接源码和补丁以同一 source.json 的 URL、文件名和 SHA-256 绑定；准备阶段验证本地文件并复制到上下文，由生成的离线读取入口在消费时再次校验摘要，拒绝未知 URL 和字节漂移。原配方管道必须传播读取失败，通知正文从已锁定 sharp 源码取用，禁止从 main 下载。该入口仅支持已审查的 linux-arm64v8/linux-x64；builder、包管理器和 Rust crate 的下载边界另行资格化，不能据此声明完整构建已离线。 基础镜像先在同一 source.json 绑定上游 OCI index 与两个平台的 child manifest；准备入口据此生成 builder 选择值，build.sh 必须显式传递 Docker platform 与 digest 引用，Dockerfile 缺少绑定时拒绝构建。保留可单独构建的 base stage，用真实容器核对实际架构、Rocky 发行版及 glibc；该证明不扩展为 dnf/Rust/Python 工具链固定或完整 base 来源、许可证与漏洞资格。 Python 构建工具沿用同一输入 authority，按所有真实调用方的最高版本下限选定 Meson，并固定 Ninja 的两架构 wheel；准备入口核验本地 wheel 后生成各平台 requirements，Dockerfile 使用 --no-index、--require-hashes 和本地 wheelhouse 安装。验证须在 Python 3.12 中离线安装并真实执行 Meson/Ninja 构建；该单元不替代 RPM 集合、Rust 或完整 native 编译验收。 Rust 工具链沿用该源码准备入口，绑定日期化 nightly manifest，并从 manifest 派生两架构 rustc、cargo、rust-std 的精确归档与摘要；本地归档预检后生成校验和及离线安装段，移除联网 rustup bootstrap 与浮动 nightly。只安装已验证组件，真实运行须覆盖现有两个 -Z 参数与 Cargo 禁网编译。cargo-c 沿用 librsvg 上游 CI 的精确 crate 与原始 Cargo.lock；使用标准 cargo vendor --locked 生成离线依赖，与 lock 摘要对应的原始 crate 成员核对后冻结 vendor 归档。版本、源码归档、vendor 归档和摘要统一绑定 source.json，准备器校验、复制并生成本地 source replacement 配置，Dockerfile 从固定源码执行 cargo install --path --locked --offline，禁止无版本 registry 安装。真实验收覆盖双架构编译、cargo-cbuild 消费和篡改拒绝；该证明不替代 librsvg 或完整 native 重建。cargo-c vendor 的公开生成入口使用已绑定源码归档及原始 Cargo.lock，从本地 registry cache/index 在隔离临时 Cargo home 运行标准 cargo vendor --frozen；只复制缓存，不读取外部 Cargo 配置或已解包源码。先按 lock 校验原始 crate，再独立比对生成 vendor 的成员、字节与 checksum metadata；拒绝未知来源、缺包、篡改及非预期遗漏。确定性归档按路径组件排序，使用 PAX、xz preset 3、零时间/owner/group、目录 0755 和原文件 mode 去除 group/other 写权限；打包必须匹配同一 source.json 的既有 vendor 摘要，才交付到新目录；不能覆盖已有文件或自动改写 authority。提供标准 Cargo/国内 registry 的缓存准备说明，使外部开发者可从公开源码重新生成，而不依赖本机临时归档。该入口不替代 librsvg、系统包或完整外部供应链资格。 librsvg 源码准备收敛到同一入口：原有三条 sed 的四项 feature 修改形成 source.json 绑定的本地补丁，按 feature 补丁、固定 Cargo.lock、安全补丁的原顺序生成源码树，并在补丁执行入口禁用偏移应用产生的备份；build/posix.sh 只复制该已准备树，禁止再次修改 feature 或锁。后续 crate vendor 必须复用此源码准备入口；先验证与既有双架构实际消费树的字节一致性及补丁漂移拒绝，再扩展 librsvg crate 闭包，实际引入 librsvg 第二消费者时，只抽取 cargo-c 已有的 lock/crate 校验、隔离 Cargo 运行和确定性归档逻辑；源码准备仍复用同一 helper，不复制补丁流程。librsvg vendor 须覆盖固定 workspace 锁的所有 registry 包，先补齐国内 registry 缓存并按锁验证，再离线运行标准 cargo vendor；首次归档经独立原 crate 对比审查后把摘要绑定到同一 source.json，公开复现命令必须拒绝不同摘要。prepare 验证并解包固定 vendor 到源码树，生成唯一 Cargo source replacement 和 offline 配置，真实双架构 metadata 必须在空 Cargo cache、禁网条件下消费该树；不以 metadata 通过替代完整 Meson 编译和链接验收。 RPM 构建输入沿用同一 source.json：从已固定基础镜像解析实际升级与安装事务，绑定每个 RPM 的国内 HTTPS 来源、文件名、SHA-256、NEVRA 和签名公钥；不手写第二份依赖图。prepare 新增本地 --builder-rpm-dir，校验输入闭集后生成逐平台安装上下文；Dockerfile 的独立 rpm stage 只使用系统 rpm/dnf，先校验固定公钥签名和包身份，再禁用所有仓库安装本地包，移除浮动更新、EPEL 配置和在线安装。双架构从相同固定 base 禁网构建，核对完整安装集合与真实工具运行，并拒绝缺失、额外、篡改、错误身份或签名输入；此项不替代完整 native 编译、最终链接清单及 base/RPM 许可证和漏洞资格。 完整 native 验收先在 ARM64 原生环境组合上述已固定输入构建完整 builder，再在禁网、独立源码/产物目录与受控并发下执行同一 build/posix.sh；未变化的 RPM 与输入验证证据复用。保留真实失败、编译目录和最终链接信息，修复生产模板或准备器后再生成；不得通过编辑生成树、增加未绑定在线回退或复用旧 ELF 使构建通过。共同问题修复后再扩至 AMD64；编译产物、实际组件归属、许可/源码义务与 Worker 安装分别验收。 公开 build.sh 入口先确认固定 base 已在本地，build 使用 --pull=false，build/run 均明确 --network=none；缺 base 时直接失败。最终 libvips-cpp 的既有 cpp_link_args 增加 GNU ld map，保留到 /target 顶层，成功后由公开入口复制到宿主 tar 旁，避免随 --rm 容器丢失，并与实际 Ninja 链接命令/执行日志及 ELF 摘要一起核对静态成员；不以候选 archive 成员全集代替实际吸收成员。 后续组件清单须区分源码/构建输入、链接候选和最终实际选入项：处理 GNU Linux 的 proxy-libintl 多列与 libxml2 零成员候选，并补齐扁平进入 libvips.a 的 libnsgif；其 vendored 源码未记录独立上游 tag/commit，只可使用源码中可证明的标识并绑定固定 libvips 来源与成员字节，不推造版本。 本次先在既有 versions.json 的 printf 生产段修正两个 GNU Linux 目标：去掉未构建的 proxy-libintl，补入 libnsgif，标识由同一 VERSION_VIPS 派生为 vendored-in-libvips-<version>；固定 libvips 归档摘要继续绑定其中全部源码，不新增版本 authority。Sharp 的版本表展示预构建依赖而非最终链接闭包，因此保留确实构建且作为候选的 xml2，由 map 单独记录其零选入成员。其余版本值、编译与链接输入不变，复用已通过的实际编译/PNG 证据；仅重新生成元数据和归档，核对除版本表外的成员字节与权限不变，并验证真实 Sharp 消费者。versions.json 不声称完整 Rust、内嵌源码或系统工具链 SBOM。 来源包沿原 prepare/recipe/npm 链交付：prepare 将与传入声明语义一致的原始 source.json 字节及其中三个已验证 repo-local 输入（recipe patch、feature patch、Cargo.lock），按原仓库相对路径保存到 cloud-agents/source-provenance；缺失、软链接、声明不一致或摘要漂移拒绝输出。构建只在编译完成后的打包段复制该目录，并放入该次 link map 与 librsvg Cargo metadata；两个受支持 Linux npm 模板的 files 显式保留 source-provenance。不新增组件 registry、receipt schema 或第二份 authority。用定点 RED/GREEN、真实 prepare、增量 native 打包和离线 npm pack 验证来源字节可追溯且不被筛除；未变化的编译、ELF 和 Sharp 消费直接复用。交付声明/图并不证明完整源码再分发、逐 archive member 唯一归属、签名 build attestation 或旧 npm 产物 provenance；Rust archive 还包含 C/native 与 compiler-builtins 成员，不能仅凭名称或整个 Cargo 图推定来源。

RemoteWorker 专项资格通过后，完整 Foundation 默认入口也应消费同一 execd 后继配方并运行原有完整验收，避免普通入口仍引用已知无法提供终态回执的旧镜像。单独的 gVisor、快照与故障模式按各自实际资格更新，不能从 RemoteWorker 一项自动推广。

完整 Foundation 恢复中的网络计量须覆盖 OpenSandbox 与 egress sidecar 共享网络命名空间的实际拓扑。当工作负载没有独立网络计数时，仅可读取其 `NetworkMode` 精确引用、正在运行且归属同一 runtime 的 egress sidecar；身份漂移、二次共享或无计数均拒绝。保持直接网络计量、聚合与溢出边界，并用真实恢复中的首轮及后续 checkpoint 验证。

恢复验收按目标记录的实际状态判断进展：全局 Controller 的一次调谐可能处理其他待办，首轮计量允许有界消费这些工作，但目标 `failed`、无进展或越界立即失败；不得重试目标失败来隐藏计量错误，generation、非零计数和恢复单调性断言保持不变。

`ANYWHERE_RUNTIME_R4_RECOVERY` 只在终态 execution、最终工件摘要复核、适用的 Kubernetes 证据写入及跨节点快照清理全部成功后发出。其 `rpoBytes=0` 仅表示该验收工件的最终内容与预期摘要完全一致，不扩展为未计量数据面的通用 RPO 承诺。

并发 Compose smoke 复用每轮已生成的唯一 `$project` 作为业务 Project 名称，使 API 身份、Worker、端口和卷保持同轮绑定。OpenSandbox 清理验收只轮询本轮记录的 runtime ID、对应 egress sidecar 与确定性 managed volume；其他并发轮次资源变化不得触发本轮失败，本轮任一已拥有资源残留仍须失败。只有成功的精确卷查询返回空结果才能证明 managed volume 不存在；Docker 查询失败须立即失败，不能冒充清理完成。

交互验收在 worker-exit 成功之后仍须独立验证 cancel/interrupt 的终态与清理，再进入 live SDK；前置 marker 不代表整轮通过。脚本非零退出只输出一个由固定阶段和原退出状态组成的稳定失败 marker，覆盖 approval、user-input、long-task、worker/agent recovery、cancel、interrupt 与 cleanup；不得包含 environment、参数、token、prompt 或响应内容。脚本在自身生命周期入口无条件初始化一次性 cleanup guard，不能由继承环境跳过清理；信号退出仍执行一次既有 cleanup，并分别保留 HUP/INT/TERM 的 129/130/143 状态。若现有日志不足以定位失败，仅用该边界确认根因，不重复完整 Provider 矩阵。

共享的恢复夹具寿命须覆盖前置 RemoteWorker 验收，避免把夹具在测试开始前已到期误判为恢复或计量失败。该夹具使用一小时 TTL；独立 TTL 阶段仍推进数据库 deadline 并验证真实到期回收，不改变生产时限或过期契约。

跨 stop/rebuild 阶段复用的 Admin 投影验收应精确对照数据库中的当前计量代际与计数，而非固定首轮代际；首轮与续测的固定代际断言保留在 Controller 恢复阶段。各已完成阶段的真实 receipt 单独保留，后续失败不抹去其范围证据，也不把部分通过算成整轮通过。

RemoteWorker 故障代理只在所属子测试内替换 OpenSandbox 连接配置；结束时须恢复原始文件字节与权限，再关闭代理，避免后续 Exec/Files 使用已失效的端点。保留输出上限、超时和认证错误映射，不通过改写产品错误码掩盖夹具泄漏。

OpenSandbox 共享 HTTP client 不得把一个 Sandbox 响应的 Cookie jar 状态带入另一个 Sandbox。固定服务镜像的后继补丁须在上游请求构建后，仅保留 endpoint authority 明确提供的 Cookie；没有该 authority 时删除共享 jar 注入值。先覆盖同一 client 的跨 Sandbox 污染和合法 endpoint Cookie 精确透传，再以固定来源和补丁构建服务镜像重跑现有完整 Foundation；保留原有 live 空 Cookie 断言和 `Set-Cookie` 测试源。

当前 dev、Compose 和 Foundation 的服务镜像生产入口须消费同一 Cookie 隔离后继配方，并记录实际构建架构、immutable image ID 与构建证据。Compose 从已验证的 deployment 归档读取配方，source/destination 共用同一构建结果；不回退到未修复的服务镜像。推广前分别验证固定上游 index 的 arm64/amd64 子镜像、原始源码字节和镜像内回归；模拟执行的 amd64 构建回归不能冒充原生 amd64 全链路验收。外部 operator 已提供的服务与历史证据仍按原绑定读取。

秘密扫描继续使用现有入口，当前树覆盖 tracked 与未忽略的 untracked 文件；全历史模式覆盖 refs 可达提交的文本匹配，先用相同大小写语义筛选变更提交，再检查候选 revision tree。回归须覆盖曾提交后删除的混合大小写敏感赋值，并输出不含内容的范围计数。忽略文件、二进制内容、提交/tag 消息、直接 tree refs 和不可达对象不属于该文本扫描范围，不能将成功结果表述为全部 Git 对象审计。

正式发布先生成并验证 Runtime candidate，平台生成器通过显式 `--runtime-candidate-dir` 校验并复用同一 standalone/notice 字节，避免两次独立 Bun 构建的输出顺序差异。保留最终 `cmp`、digest/source/路径绑定与篡改失败检查，不通过删掉比较或后处理 bundle 制造一致。该复用不宣称独立重建可复现；其他本地 platform-only 构建保持独立证据边界。

仅 deployment 脚本或声明变更时，复用现有部署归档生产者生成独立包，核对完整成员集合、实际变化和来源字节；未变化的 Web 与供应声明按字节复用已有证据。不手改既有候选的 manifest/checksums，不把独立部署包升级为完整 platform 候选；包内许可证等来源变化须保留，Runtime 包是否可复用须按其实际输入判断，不能仅凭 HEAD 与 dirty 标志。若 Runtime 包只有许可证输入变化，用原 npm pack 入口刷新受影响 tarball，并核对完整成员集合、权限与字节差异；未变化的执行代码沿用原验收，不重写旧 candidate 的摘要或 conformance 声明，不将独立 tarball 刷新算作新的完整 Runtime candidate。

共享项目中的 Grant 活动验收须通过 Grant 的 Sandbox 身份限定当前被测 Sandbox，保留 issued、revoked、文件访问和失败次数的精确断言，不能把其他 Sandbox 已验证的活动计入本单元或删除这些活动以制造通过。

### 每次如何继续

编码简化规则只维护在 [CLAUDE.md 的 Implementation simplicity](../../../CLAUDE.md#implementation-simplicity)；根 AGENTS.md 为 Codex 提供同一入口。按本切片处理重复来源，不另开全仓重构计划，也不以简化为由削弱迁移、权限或验收边界。

1. 先核对当前任务、验收标识、branch/worktree、dirty state 和相关源码；继续主计划时先读取 [06](06-status-tracker.md) 的最终状态，再按新的明确授权建立切片。明确的旧 ADMIN-M1～M4 任务按 ADMIN-WEB-V1，定点修复、审查或验证按其任务范围，不被默认下一项覆盖；不要从历史文档的一条未完成 checklist 重新启动旧项目。
2. 再读本文件对应切片及该切片所需的 01/02/03/05/07 段落；遇到具体契约/安全问题才查询相应 ADR、历史证据。搜索默认限制在当前规范与相关源码，不能把历史全文的指令当作当前任务。
3. 对已授权、目标和风险明确的常规修改、测试、幂等重试、状态记录继续执行，不重复确认同一事项。技术方案待验证不等于必须先人工批准每个字段。
4. 若缺少会改变权限、费用、数据保留或实施范围的选择，或触及明确批准要求，说明受影响动作与所需决定；保留状态并继续安全独立的已授权工作。不得默认生产写入、部署发布、迁移旧卷、读取用户内容或删除脏 worktree。
5. 单项任务按其明确范围、相关验证和结果记录判断完成；声明基础设施能力或 BASE 阶段完成时，才须同时满足对应后端与 Admin 闭环。单项任务完成不代表阶段通过，不能仅列待办、隐藏按钮或留下 Mock 就宣称能力已交付。无关旧 Gate 开放不反向阻塞当前切片，但对应正式 Gate 的证据/签署绝不免除。

文档收口的可验证目标是单一顺序、明确权限、真实状态、有效引用和无冻结输入破坏；它不能保证未知环境故障或所有未来任务绝不出错。新冲突按总入口规则定位到具体范围处理，不扩大为全项目停工。

## 0. 当前实施顺序：底座先行

[ADR-0032 / D-055](../adr/0032-infrastructure-admin-delivery-and-document-routing.md) 明确第一阶段完整交付基础设施＋Admin Web，第二阶段才是用户 CloudAgents 对话。下面 BASE 不是纯后端轨道：面向管理员的能力必须有对应可用管理页面与实测。以下是实施方案，不是已执行或所有生产动作已获准的声明。
阶段状态只维护在 [06](06-status-tracker.md)，整体底座就绪条件是 [05 的 BASE-READY](05-gates-and-acceptance.md#0-底座就绪验收-base-ready) 与 [07 的 BASE-ADMIN-V1](07-admin-web-requirements-and-design.md#base-admin-v1)，不再使用没有任务范围的“第 15 节全部标准”。

### 0.1 基础设施与 Admin 联合切片

| 顺序 | 底座切片 | 同阶段必交付 Admin Web | 退出证据 |
| --- | --- | --- | --- |
| BASE-M0 | 领域/API 映射与固定版本执行 PoC | 复用 Target、Operation/Audit、版本和能力事实视图；无数据的能力不建空页面 | 无 Agent 的真实 create → ready → exec/file → stop/delete；重复请求、失败补偿及卷不误删验证 |
| BASE-M1 | 长期 Workspace/Volume 与持久化生命周期调谐 | Workspace/Sandbox 分离列表；卷归属、保留规则、Operation/恢复状态 | 停止和重建保留代码；HTTP 断开、CP/Controller 重启后无需人工重发即可完成已接受操作 |
| BASE-M2 | 通用 Exec/PTY/Files、Preview/SSH 与访问隔离 | Port/Grant 元数据、到期/撤销、网络策略执行状态 | 无 Agent 客户端可连接和重连；跨租户/路径穿越/任意跳转被拒绝，网络规则实际生效 |
| BASE-M3 | 客户节点 RemoteWorker 接入 | 节点注册意图、owner、能力、心跳、Drain/Resume、版本与证书状态 | 仅 outbound/NAT 节点可承载同一 Workspace/Sandbox 流程；断连、重连、过期命令和旧 generation 正确处理 |
| BASE-M4 | Kubernetes 路径、资源池/容量调度与隔离等级 | Region/Pool/Node、RuntimeProfile、资源配额和调度失败原因 | Docker/Kubernetes/客户节点能力矩阵；容量不足、owner/runtime/arch 不匹配拒绝；强隔离实证 |
| BASE-M5 | 文件系统快照恢复、独立交付、计量与运维收口 | Snapshot/Restore、用量、失败积压、升级/回滚、恢复状态；管理页面整体回归 | 无 Agent 的完整底座矩阵、备份恢复/升级/故障演练与 Admin 验收；逐项通过 BASE-READY |
| APP-M1 | 四种 Agent 的 Anywhere Runtime/SDK，随后完整用户对话 | 相关 Provider、执行/恢复运维随切片交付；不加入对话/源码查看 | 按 §0.4 与 05 的 ANYWHERE-RUNTIME-V1，完成后再验收完整用户对话产品 |

BASE-M0～M5 共同组成第一阶段，不改名或重置旧 P0～P6、Portable Runtime M1、ADMIN-M* 的证据。M0 的执行候选 PoC 是技术验证，不宣告产品能力交付；从 M1 起每个面向管理员的能力都以真实后端＋Admin 流程作为同一个完成单元。
先 Docker 单 Region 验证产品语义，再扩展客户节点和 Kubernetes；不把只有 Docker 的结果声称为全部路径完成。
APP-M1 的新产品功能在 BASE-READY 后推进；已有 Agent 功能继续保留，必要的兼容/安全回归可以随底座进行。

### 0.2 各阶段的最小闭环

**BASE-M0：首先验证执行接缝。**

- 固定当前 source/dirty、既有 API/schema 和 OpenSandbox 候选版本、许可及能力差异；
  明确 Workspace、Sandbox、RemoteWorker 与旧 Lease/Worker/Profile 的映射，不按名字直接复用语义。
- 只定义首条链路必需的契约与迁移方案，复用现有生成链；不先生成全部未来资源的空 CRUD。
- 优先用 OpenSandbox adapter 验证真实 Docker Sandbox、Exec/Files、卷挂载和资源发现；
  验证不安装 Provider 也可执行，以及幂等重放、部分创建和异常清理。结果决定执行器复用/替换范围。
- 原 actuator 不删除，原 Lease 不迁移；新 PoC 只接触授权范围内的新测试资源。未通过则修复该接缝，
  或据实提出替代方案，不以此为由自动重建整套 sandbox engine。

**BASE-M1：先保证数据与操作不会随进程消失。**

- 新 Workspace/Volume 独立持久化；stop/TTL 释放计算、保留卷；删除工作区走独立授权与保留规则。
- 持久化 Operation/outbox 和 Controller 认领/重试真正接入部署路径；API 快速接受，状态可查询。
- 验证断开客户端、创建中重启、receipt 丢失、重复命令、失败回收及旧 generation；不依赖人工重发完成。
- 默认单写卷，测试旧写入者 fencing、同 Workspace 重建、越权挂载拒绝；升级复用卷不冒充任意跨节点迁移。
- Admin 必须区分 Workspace、Sandbox 与旧 Lease，清楚显示哪些数据会保留；策略值必须与执行值一致。

**BASE-M2：通用访问，而不是 Agent 工具的内部命令。**

- API/SDK/CLI 提供受限 Exec、PTY session、文件读写和端口发布；验证输出/缓冲/文件大小上限、
  reconnect cursor、路径/symlink 边界与内容访问所有权。
- 交付可独立运行的 Access Gateway；Preview 默认私有，SSH 短期凭据与固定 Sandbox 路由，
  expiry/revoke/generation rollover 后拒绝访问，不开放任意代理或客户主机 shell。
- 网络策略下发到实际执行路径，验证受控 DNS、metadata/宿主机/控制面/其他租户阻断和允许的外部访问。
- Admin 显示端点、Grant 和策略状态的脱敏元数据，不承载用户 Terminal/Files 内容；CLI/SDK 足以验证底座，
  不以完整用户 CloudAgents 页面作为本阶段前提。

**BASE-M3：主动连接的客户节点。**

- 实现 enrollment/CSR/mTLS 身份签发、轮换/吊销、节点能力/容量/版本上报、owner 约束和反向命令/访问通道。
- 至少一台仅允许 outbound 的真实测试节点完成创建、Exec/Files、连接、停止、重建；
  测试 NAT、断线恢复、幂等 command、deadline、incarnation/generation fencing。
- 离线停止新调度；重连先 reconcile，不盲目重放旧命令，也不因为离线删除 Workspace 或强挂卷。
- 客户节点默认只承载其所属租户；记录宿主管理员可读取本机数据的信任边界，不承诺对宿主管理员保密。

当前候选的 PTY 终态恢复修复先核验固定 execd 源码与镜像来源，再验证同一 execd 实例在连接断开或 RemoteWorker 重启后能重放原始权威 `exit` 控制帧。优先复用现有 frame receipt、输出游标和 Gateway 的 `exit-status` 路径；不得从 `running=false` 猜测退出码，也不得只延长读取窗口使单次验收通过。后继实现须保留输出与终态的顺序、takeover/身份边界，记录源版本、补丁和派生镜像摘要；真实 exit 0/7、断开后重连、Worker 重启、缺失权威退出码的拒绝以及完整 RemoteWorker 专项验收通过后才更新对应单元。同一 execd 实例内的重连证据不能扩展为 execd 自身崩溃后的进程恢复或正式 Gate 关闭。

**BASE-M4：调度和执行矩阵。**

当前 Kubernetes 验收先复用既有 OrbStack/k3s，动态核验 Pod/Service CIDR 与 VM 下一跳，在任务拥有的临时路由 helper 中快照、设置并恢复精确路由后运行 Foundation lifecycle。记录 wrapper 退出码、generation fencing 与资源清理；临时路由通过不表示宿主重启后的持久配置或多节点故障矩阵通过。

- 用同一基础 API 完成 Kubernetes 路径；声明每个 backend 支持的存储、访问、runtime 和恢复能力，
  不支持的组合在 admission 阶段拒绝，不退回另一安全等级。
- 建立最小 Region/ResourcePool/Node 模型与容量预留：硬过滤 owner、region、runtime、arch、卷可达性、
  节点健康、配额和 CPU/内存/存储；先用确定性选择，不先做成本预测和自动扩容。
- 区分共享不可信租户、可信单租户和专用节点；至少验证一个可用的强隔离 runtime 路径及其工具链/网络
  矩阵后，才能声称具备对应共享不可信租户能力。不能以设置 profile 名称代替实际隔离。
- 单 Region 多池足以退出；多 Region active-active、跨 Region 卷迁移、Warm Pool 和直接 MicroVM 不在当前必需项。

**BASE-M5：独立底座交付与恢复。**

- Workspace 文件系统快照、manifest、恢复到新卷/新 Sandbox、数据校验和保留清理；Secret 不进入快照。
  对无后端一致性快照能力的卷使用明确停写/离线快照，不冒充运行中一致性或内存恢复。
- 全新 Compose/Helm 安装与客户节点 bootstrap 文档；模板、runtime/worker/gateway 制品版本固定，
  支持声明的 N/N-1 升级、回滚、身份/证书轮换及可执行恢复 runbook。
- 持久化 CPU/内存分配时长、卷占用和支持的网络用量事实，含长任务 checkpoint、离线对账与可审计修正；
  不以 Prometheus 或 Provider token usage 作唯一事实源。完整价格、钱包和 invoice 后续实现。
- 测试 CP/Controller/Gateway/节点故障、备份恢复、限流/背压、容量和有界 soak；记录实际 P50/P95 与
  RPO/RTO，未经压测/演练不承诺 HTML 中的数字。根据适用风险执行既有安全/供应链检查。
- 收齐各阶段已交付的 Admin API 权限、危险操作确认、Operation/Audit、双语、可访问性和 Daytona 固定视觉验收；
  不能把历史截图或早期 Provider E2E 当作当前底座完整证明。

### 0.3 执行与完成约束

收到继续实施主计划的任务后，从 06 中最早未完成且在授权范围内的切片推进；BASE-READY 后进入 APP-M1，不重新打开已完成阶段。明确指定的后端、UI、契约、文档修复或审查/验证，按该任务范围完成，不自动扩成整个阶段；仍须验证其实际影响，不能据此豁免已受影响的 Admin 流程。
只有声明基础设施能力或 BASE 阶段完成时，才要求契约/后端、必要 SDK/CLI、相关 Admin 页面和真实验证共同满足；单项任务完成不代表阶段通过。后台可运行但管理闭环缺失，或页面可展示但依赖 Mock，均不能标记该能力完成。允许在同一联合切片内并行开发后端与页面，以及提前做安全独立的准备工作；不能把未完成的 Admin 工作整体移到下一阶段，也不能绕过依赖验收。

用户批准的产品边界已经记录，不重复要求确认同一边界；实现授权仍以当前任务和已有同范围授权为准。
需要新环境/凭据、改变数据保留、迁移现有卷或跨越单独批准要求时，只暂停受影响动作并说明需要什么，
继续可独立完成的在范围内工作。所有生产写入、部署/发布、脏 worktree 删除和正式 Gate 要求保持不变。

下文提供旧提取/cutover 方案的按需入口，供兼容与后续集成使用，不要求为新底座从头重做历史 inventory 或先完成真实 Agent/T3。

<a id="anywhere-runtime-plan"></a>

### 0.4 APP-M1：Anywhere Runtime 与 SDK

范围依据 [01 §1.3](01-product-scope-and-authority.md#13-anywhere-runtime-的产品目标)，接入和恢复语义见 [02 §0.6/0.7](02-target-architecture.md#06-agent-runtime-如何使用-workspacesandbox)。
`ANYWHERE-RUNTIME-V1` 覆盖下表 R1～R5；U1 是随后完整用户产品，不能把 Runtime 子范围完成当作整个 APP-M1 完成。
只定义当前切片必需的契约/迁移，复用现有生成链、Runtime、Provider、Controller、Worker、SDK 和 Admin 页面。

| 顺序 | 最小完整交付 | 退出条件 |
| --- | --- | --- |
| APP-M1-R1 | Codex/Claude Code 接通新 Workspace/Sandbox；统一 Agent 绑定、启动、事件、交互、结果及公共 SDK | Docker 上两个真实 Provider 经 SDK 完成持久 Workspace 的 Turn/文件/Artifact/后续 Turn；相关 Admin 状态、旧 Lease 与 no-Agent 回归。确认长任务超时策略，不能只复用旧 WorkerEndpoint 路径 |
| APP-M1-R2 | Pi 与 deepseek-harness Provider adapter、distribution 注册和固定版本制品 | 核验上游机器接口/许可/运行环境；四种 Provider 在同一公共契约下完成 Docker 真实流程，取消、交互与恢复能力据实声明；无 catalog-only 接入 |
| APP-M1-R3 | 可跨节点恢复的 Workspace 数据与状态引用 | 在原节点不可用时，从受验证的共享/复制存储或可达快照恢复到不同目标；校验归属、摘要、一致性点、旧 writer fencing、保留/清理与相关 Admin 操作；不得仅放宽 Target UID 校验 |
| APP-M1-R4 | 运行中 Turn 的持久认领、Agent Checkpoint、对账和跨节点接管 | CP/Agent/Worker/节点故障后，原执行可发现或新 attempt 安全接续；事件/交互/工具回执持久化，旧节点回归不能双写，副作用结果未知不盲重放；复用既有幂等/outbox/租约机制 |
| APP-M1-R5 | Docker、outbound RemoteWorker、Kubernetes 的完整 Provider/SDK/部署与运维验收 | 逐项通过 [05 的十二格及故障矩阵](05-gates-and-acceptance.md#anywhere-runtime-v1)，TS/Go 仓外消费、真实长任务、相关 Admin 双语/视觉/安全和 no-Agent 回归；记录实际 RTO/RPO 与限制 |
| APP-M1-U1 | 完整用户 CloudAgents 对话、任务、审批、历史、结果与恢复反馈 | 使用已验收的公开 Runtime/SDK 链路；另按用户产品任务验收，不混入本次 Runtime Goal 完成条件 |

R1 先贯通两个已有 Provider，R2 补新增 Provider，R3 为 R4 提供数据恢复前提；各切片同步补必要契约、SDK、Admin 与测试，
不将这些欠项统一推到 R5。环境/凭据暂缺时继续可独立实现和本地验证，缺失格保持开放，不删减 Provider 或故障条件。
R3 只以新测试 Workspace 验证，现有卷迁移另按授权办理；跨 Region 灾备和进程内存热迁移不属于 V1。

<a id="anywhere-runtime-goal"></a>

### 新 Goal 提示词：ANYWHERE-RUNTIME-V1

以下替换原 BASE 迁移提示词，供用户显式用于新的 Runtime 实施任务。保存或读取文档不创建/修改 Goal，不自动开始实现、部署或迁移其他任务；原 BASE 与 ADMIN-WEB-V1 的任务范围和证据保留。

```text
创建并持续推进 Goal：完成 Cloud Agents 的 ANYWHERE-RUNTIME-V1。

代码工作目录：当前仓库根目录
工作分支：codex/cloud-agents-platform-p0；先核对 cwd、branch、HEAD、dirty/staged 和现有改动归属，不覆盖、回滚或提交无关修改。
先读仓库根目录下的 `CLAUDE.md`，执行其 Implementation simplicity 规则；AGENTS.md 仅路由到同一规范。
文档目录：`docs/plan/cloud-agents-platform`
先读 06 当前状态，再按 04 §0.4 的 APP-M1-R1～R5 执行；01 产品范围、02 接入/恢复架构、03 制品、05 ANYWHERE-RUNTIME-V1 和 07 §8.13 为对应约束。
04 是唯一计划，06 是唯一状态；记录实际 source/dirty、证据、下一项和阻塞，不维护第二套进度表，不从历史清单重启 BASE。

目标：Codex、Claude Code、Pi、deepseek-harness 通过统一 Provider/Agent Runtime 和公共 TypeScript/Go SDK，
在 Docker、outbound RemoteWorker 远程机器、Kubernetes 中使用长期 Workspace/Sandbox，支持真实任务、交互、事件续读、结果和故障恢复/转移。
先完成 R1 的两个已有 Provider 在新底座上的 Docker + SDK 纵向闭环，再补新增 Provider、跨节点数据恢复、运行中 Turn 接管及完整矩阵。
deepseek-harness 来源：https://github.com/deepseek-ai/deepseek-harness；固定版本并核验机器接口，不把 WebUI 启动或模型 endpoint 配置当作 Harness 接入。
保留 BASE-READY、BASE-ADMIN-V1、旧 ADMIN-WEB-V1 的原结论和现有 Agent/Lease 兼容；默认 Compose/Helm 仍可无 Agent 运行。
完整用户对话 UI 属于后续 APP-M1-U1；本 Goal 不扩展到 Synara/T3、Billing、Wallet、Marketplace、跨 Region 灾备或内存热迁移。

运行恢复必须持久化执行认领、事件/交互、工具意图/回执与可用 checkpoint；CP 重启不能因内存 owner 丢失直接判失败。
跨节点先证明旧 writer 已 fence，再恢复 Workspace 和兼容 Agent 状态，以新 attempt 接续；旧节点回归不得双执行或双写。
副作用结果未知必须显式待处理，不盲目重放。历史读回、同节点重连和文件快照不等于运行中 Turn 跨节点恢复。
相关 Admin 配置、能力/状态、失败恢复及 Operation/Audit 随每个切片完成，沿用 Daytona v0.190.0、zh-CN/en-US、双主题和可访问性要求。
User/Admin 身份、API 与内容权限分离；普通用户调用 Admin API 返回 403；Admin 不读取对话、源码、原生 cursor 或 Secret。
危险操作保留权限、影响清单、资源名称/generation 确认和审计。

授权本仓范围内的必要代码、契约/迁移源、生成 SDK、相关 Admin、文档修改及测试；允许为本 Goal 创建和精确清理本机临时 Docker/kind 测试资源、打包未发布的本地候选，不操作既有业务资源。
复用现有模块、Provider 协议、Controller/RemoteWorker、幂等/outbox/fencing、生成器和测试，不另造调度器、任务内核或通用框架。
涉及版本或迁移时，修复生成源和共享消费入口，不继续手工复制逐版本分支、白名单、路径、数量或摘要。
保留必要历史绑定和独立完整性校验，复用相关生成检查及回归测试；不把同构重复改成另一份手写表，也不另建通用框架。
每个切片完成实现与必要真实验证后自动继续下一项，不停在审计、方案或文档待办；提交时只包含本 Goal 的相关修改。
以 05 全部十二格、运行中故障矩阵、TS/Go 仓外真实调用、长任务、相关 Admin 和受影响底座回归作为完成条件。
记录固定版本、命令、数据摘要、实际 RTO/RPO、限制和清理结果；不以 Mock、build/lint、CLI 手工启动或历史不同制品证据冒充完成。
沿用仍有效的同动作/对象/环境授权；外部客户节点测试若无同范围授权或缺少凭据，只暂停对应动作并说明缺少什么，继续独立的本地工作。
不从历史记录读取或复用未授权的客户凭据；改变费用、数据保留、旧卷迁移或破坏性范围时单独处理具体决定。
不 push、不发布镜像/npm/Release、不操作生产、不删除脏 worktree、不修改其他任务；额外部署和正式 Gate closure 仍遵守既有明确批准要求。
同范围常规开发、验证和幂等重试不重复确认；未完成或受阻矩阵保持开放，不把 Goal 或整个 APP-M1 提前标为完成。
```

<a id="mcp-skill-runtime-plan"></a>

### 0.5 MCP-SKILL-RUNTIME-V1 当前实施切片

该切片紧接 ANYWHERE-RUNTIME-V1，复用同一 Control Plane、Runtime、Worker、RemoteWorker、Kubernetes、fencing、幂等、Operation/Audit 和生成 SDK，不新增调度器或任务内核。执行顺序固定为：

1. JSON Schema/OpenAPI/Proto 正式化 MCP Server、Skill Bundle、引用、短期授权、撤销和 capability 事件，并生成 TS/Go SDK。
2. Control Plane catalog、tenant/project 绑定、RLS、版本/digest/幂等/撤销和 Session/Execution 显式引用；Runtime 打开前重新解析并 fail closed。
3. Worker/Foundation Runtime 统一注入 manifest；只允许 Host-managed MCP broker、短期凭据、网络 allowlist 和签名 digest 校验的只读 Skill Bundle，禁止主机任意路径和 Secret 持久化。
4. 逐个验证 Codex、Claude Code、Pi、deepseek-harness 的真实 native/emulated/unsupported 接口；仅凭真实运行结果更新能力目录。
5. Admin 展示 opaque 元数据、绑定和 Operation/Audit；完成 Docker、RemoteWorker、Kubernetes 十二格真实验收与故障矩阵后，才允许在 06 登记支持。

<a id="engineering-maintenance-items"></a>

### 0.6 工程维护独立项（2026-09-28 登记）

以下三项各自独立立项、独立授权和验收，不并入任何 BASE/APP 切片，也不因其他任务完成而隐式开始。执行时必须保留历史 ledger digest、冻结 SQL/bundle 字节、精确 selector 匹配和未知/篡改版本拒绝；只按 [CLAUDE.md 实现简化规则](../../../CLAUDE.md) 做等价重构。

| 编号 | 项目 | 现状 | 目标与完成条件 |
| --- | --- | --- | --- |
| MAINT-1 | `internal/migration` 拆分与退役 | 2026-09-29 完成：Go 1.26.6 `GOOS=linux` 可达性分析确认产品二进制只使用其中约 3k 行；这部分已抽成 `internal/migrationcore`（9 个源文件，无内部包依赖），`localmigration` 与产品命令改为依赖它，并由 `test/scripts/test-platform-go-products.sh` 完整覆盖。历史 `internal/migration`、`cmd/cloud-agents-migrate`、`scripts/data-recovery-validator`、分片测试工具及 5 个 runner-ledger Go 生成器已删除。 | 已达成；相关冻结证据与工具的退役记录随 MAINT-3 维护，当前结果以 [06 当前状态](06-status-tracker.md) 为准。 |
| MAINT-2 | 产品迁移 schema 快照存储方式 | 2026-09-29 完成："基线 + 增量"。`product/<head>/` 仍保存完整 `manifest.json` 与 `schema-bundle.json`（运行时与发布包只读这两者），累计 catalog 改为 `catalog/schema-*.patch`：相对 manifest 中 `predecessor_catalog_contract` 的零上下文行补丁，首个产品版本以冻结的 `catalog/schema-000014.json` 为基线。`scripts/generate-foundation-migration-package.ts` 写出补丁，`--check` 逐版本重放并要求每个重建的 catalog 与冻结 `catalog_contract` 的大小和 SHA-256 一致；目录多余/缺失文件、补丁篡改、基线篡改和头部换绑均拒绝。`product/` 由约 98M 降至约 21M，新版本补丁约 1–50KB。 | 已达成；生成器产出、冻结 digest 校验及历史选择/拒绝检查保持不变。 |
| MAINT-3 | v2/v3/v4 生成器合并 | 2026-09-29 完成：`platform-generator-supply-replay-v2/v3` 合并为共享内核 `platform-generator-supply-replay.ts`，`platform-contract-closure-profile-v3/v4` 合并为 `platform-contract-closure-profile-successor.ts`；版本模块只保留各自数据、错误类型，以及 v3 replay 固定的 authority/wrapper/核心输出和 v4 closure 新增的 authority、v3 fence 与 replay authority 绑定（两族共约 6.9k 行降至约 4.1k 行）。其余带版本后缀的族未合并：`successor-dag`、`successor-predecessor`、`generator-supply-profile`、`contract-lock` 各版本间共同代码仅 12–35%，属不同行为；`replay-platform-generators{,-v3}`、`-isolated{,-v3}.sh`、`g-contract-external-consumer{,-v2}` 和 durable lineage v2 相关脚本的字节被已提交 authority 以 digest 固定，改动会使历史 authority 失效。开源整理随后删除了 v3 replay、closure-profile v4 及 v3 successor/phase 工具链，共享内核现只服务 v2 replay 与 v3 closure。 | 已达成；共享内核只保留真实不同的行为与数据，确定性生成和历史版本拒绝检查保持不变。 |

## 1. 兼容、迁移与回滚的按需入口

已有 Agent/Lease 调用方继续兼容；Workspace 数据归属和旧卷采用不能由文档更名隐式改变。实际涉及旧数据迁移、消费者 cutover 或破坏性回收时，读取[数据迁移、删除与回滚安全要求](migration-and-rollback-safety.md)，核验授权、恢复点、N/N-1 与单一 writer，再执行该范围任务。不要为了新的 BASE 工作重新运行旧 P0 inventory 或提前实施 Synara/T3。

## 2. 当前文档规则

- `04` 只描述计划和执行约束；`05` 只描述验收标准与 Gate；`06` 只描述最终实际状态；`07` 只描述 Admin Web 需求与设计。
- 逐轮命令、候选版本、日志 digest、修复过程和旧阻塞记录不再复制到活动计划；需要审计时从 [phase-1 验收](../../acceptance/phase-1.public.md) 或冻结 `evidence/` 记录进入。
- 不因文档收口改变契约、迁移、安全、兼容、权限、数据保留或正式 Gate 规则。
