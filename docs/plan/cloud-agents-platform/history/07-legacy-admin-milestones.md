# 旧 Admin 里程碑定义

来源：下方 §14.1 原文来自 `ed7d3ac5:docs/plan/cloud-agents-platform/07-admin-web-requirements-and-design.md`；末尾 ADMIN-WEB-V1 依据用户本次贴出的原完整 M1～M4 提示词固定验收范围，不是新的实现或验收记录。本文不产生新的工作授权，不改变旧证据结论；仍有原范围授权的旧任务可按此验收。平台默认主线仍是 [04](../04-extraction-and-migration.md) 的基础设施＋Admin，当前验收路由见 [07](../07-admin-web-requirements-and-design.md#15-实现验收标准)。旧 Lease volume 清理语义不能套用为长期 Workspace 的默认删除。

### 14.1 历史 ADMIN-M1～ADMIN-M4 定义

下列旧里程碑保留用于解释现有报告，不再是当前平台推进顺序，也不要求按它们先完成用户 Agent 才推进底座。

本文使用 `ADMIN-M1`～`ADMIN-M4` 标识 Admin Web 里程碑；平台原有 `M1` 仍指 Portable Runtime。
两者不共享状态或授权，名称区分不解除任何暂停，也不构成新的实施或部署授权。

### ADMIN-M1：Admin Web 基础与 Target 运维

- 新增 `apps/admin-web`；
- 独立管理员鉴权和 Admin API；
- 1:1 复刻 Daytona `v0.190.0` Dashboard shell、Sidebar、页面布局和基础组件状态；
- 建立 `zh-CN` / `en-US` message catalog、语言选择、fallback 和持久化；
- Target 列表、注册、详情、Probe、Cleanup；
- Lease 运维列表；
- 操作确认和 Audit 最小闭环。

验收：Admin Web shell、Target 列表/详情和创建 Sheet 在 `zh-CN`、`en-US` 下通过 Daytona 视觉基线对比；
语言选择在刷新后恢复且不存在缺失翻译；管理员可完成 Docker/Kubernetes/SSH Target 注册到 Probe ready，
并能看到 operation/audit；普通用户 Token 调用相同 Admin API 返回 403。

### ADMIN-M2：Environment Profile 与 User Web 边界

- Profile 草稿、发布、禁用和不可变版本；
- User Web Profile selector；
- 按 Profile 服务端解析 Target、release、资源和凭据引用；
- 从 User Web 移除 Target/Lease 配置。

验收：用户请求和浏览器存储中均不存在 endpoint、`credentialRef`、`providerCredentialRef`；用户可以通过
已发布 Profile 创建真实 Codex/Claude Code Session/Turn。

### ADMIN-M3：Worker、升级和策略管理

- Cluster/Worker 运维视图；
- Drain/Resume、固定 digest Upgrade 和回滚；
- Images/Releases；
- Quota、Storage Policy、Network Policy；
- Maintenance Operation 统一页面。

验收：管理员能在至少一个 Docker、一个 Kubernetes 和一个 SSH 目标完成 Drain -> Upgrade -> Probe ->
Resume，并保留 generation、operation 和 audit 证据。

### ADMIN-M4：安全与真实 E2E

- User/Admin 独立部署和 OIDC audience/scope；
- 内容与 Secret 返回检查；
- Docker/Kubernetes/SSH 各一条真实部署、会话、终止和零残留清理路径；
- Codex 与 Claude Code 各至少一条真实 Turn；
- 失败、重试、并发冲突和刷新恢复验证。
- Daytona 基线页面在 `zh-CN` / `en-US`、light/dark 和桌面/移动 viewport 下完成视觉回归。

验收：Admin Web 全程只能看到运维元数据；User Web 只选择 Profile；终止后平台拥有的 Worker、容器/Pod、
Workspace volume 均按策略清理。

## ADMIN-WEB-V1

适用范围：原“完成 Cloud Agents User Web / Admin Web 边界拆分，并交付 Daytona v0.190.0、中英文 Admin Web”的 M1～M4 任务。这里的 M1～M4 统一称 ADMIN-M1～M4，不是 Portable Runtime M1，也不是 BASE-M1～M4。以下条件与上方里程碑共同组成固定验收，不追随新版 07 §15 的 BASE 增量变化。

1. User Web 与 Admin Web 独立构建、部署、路由、鉴权；User Web 保留 Conversation、Session/Turn/Execution、Approval、User Input、Cancel/Interrupt、Artifact 和 Environment Profile 选择。
2. Admin Web 完成 Docker/Kubernetes/SSH Target、凭据引用、Probe、Cluster/执行 Worker、Images/Releases、Environment Profile、Quota、Storage/Network、Lease、Upgrade/Rollback、Drain/Resume、Cleanup、Maintenance Operation 和 Audit 的真实管理闭环。
3. Daytona `daytonaio/daytona@v0.190.0/apps/dashboard` 是布局、视觉、组件、响应式和交互固定 1:1 基线；只替换 Cloud Agents 品牌、文案、图标语义、资源名称和业务字段，不复制 Daytona 品牌素材或前端源码，不以“相似即可”替代验收。
4. 保持 Vite + React + TypeScript、原生 CSS/CSS variables 和生成 SDK；不引入 Next.js、Ant Design、Tailwind、新状态管理框架或不必要的 i18n 依赖。
5. `zh-CN` 与 `en-US` 覆盖导航、标题、表单、按钮、状态、确认框、Toast、空/错误状态、Tooltip 和 ARIA；缺失翻译测试失败，生产不显示 message key 或未翻译界面文案。
6. 账户/设置区域切换语言立即生效、不刷新；首次按浏览器语言选择，中文使用 `zh-CN`，其他使用 `en-US`；显式选择作为非敏感偏好持久化、刷新恢复，未知/不支持 locale 回退 `en-US`。
7. 日期、时间、数字、相对时间使用原生 `Intl`；资源 ID、stable error code、API 字段名和日志原文不翻译。中文不得造成 Sidebar、Toolbar、Table、Sheet/Dialog、按钮或 Badge 溢出，也不得缩小字体破坏比例。
8. 两种语言各自通过 light/dark、桌面/移动 Daytona 视觉回归；键盘、焦点、ARIA、对比度与 `prefers-reduced-motion` 验证通过。
9. User Web 页面、请求和存储不暴露基础设施 endpoint、kubeconfig、SSH 配置或 credential reference；Admin API/Web 不读取或返回用户对话、Prompt、代码、Workspace/Artifact 内容或凭据原文；普通用户 Token 调用 Admin API 由服务端返回 403。
10. Environment Profile 草稿、发布、禁用、版本不可变及 User selector 可用；Target、release、资源与凭据引用由服务端解析，不由浏览器组装底层部署配置。
11. 危险操作具有 generation 校验、影响确认、Operation 和 Audit；Cleanup 保留资源名称/generation 确认和精确资源清单。旧 Lease 的卷按其已声明策略处理，不扩大为删除长期 Workspace、新资源或用户已有数据的权限。
12. Docker、Kubernetes、SSH 各有真实部署、维护、终止和精确清理证据；Codex 与 Claude Code 各有真实 Session/Turn 证据；失败、重试、并发冲突和刷新恢复经验证。上述 Agent 验收不能因新 BASE 的 no-Agent 要求而删除或挪到 APP-M1。
13. 构建与相关 contract、Control Plane、SDK、Admin/User Web 测试通过；单元测试、Mock、静态页面、空按钮或 Helm lint 均不能单独作为完成证据。复用已有证据须核对其 source/backend/版本和受影响范围，不能重命名报告冒充新验收。

本范围不要求新增独立长期 Workspace/Sandbox 生命周期、outbound RemoteWorker、Region/Pool、通用 Gateway 或新底座快照恢复；它们属于 BASE，不因同路径文档更新自动追加。既有 Workspace 内容隔离、Storage/Network 和旧 Lease 保留/清理要求仍须满足。ADMIN-WEB-V1 完成不代表 BASE-READY。

原任务工作目录为 `/Users/huang/devel/project/huang/business/cloud-agents`，使用当前 `codex/cloud-agents-platform-p0`，不新建分支，不覆盖或提交无关 dirty work；沿原授权按完整切片验证、只提交相关文件并继续。真实凭据、外部权限、破坏性操作和其他明确审批边界保留，同范围已有授权不重复确认。本文不自动启动或修改该任务，不把主线迁移或文档编辑当作部署授权。
不扩展到第三种语言、Synara、T3、Billing、Wallet、Marketplace；不 push、发布镜像或创建 Release，除非用户另行明确授权。固定验收范围变化使用新标识，不原地改写 ADMIN-WEB-V1。
