# 06. 当前状态与下一项

## 0. 当前主线（2026-09-05）

当前产品决定：[ADR-0032 / D-055](../adr/0032-infrastructure-admin-delivery-and-document-routing.md)。第一阶段完整交付基础设施＋Admin Web，之后才做用户 CloudAgents 对话。唯一工作顺序是 [04](04-extraction-and-migration.md)，完成定义是 [05](05-gates-and-acceptance.md)，Admin 功能/交互要求是 [07](07-admin-web-requirements-and-design.md)。

### 本轮文档状态与下一项

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

| 阶段 | 状态 | 尚需证明的完成范围 |
| --- | --- | --- |
| BASE-M0 | NOT STARTED | 固定版本执行候选、领域/契约映射、no-Agent Docker PoC；核对既有 Admin 可复用能力 |
| BASE-M1 | NOT STARTED | 长期 Workspace/Volume、自动恢复的 Operation/reconcile，以及对应资源/保留/维护界面 |
| BASE-M2 | NOT STARTED | Exec/PTY/Files/Preview/SSH 和真实策略执行；Admin grant/策略/诊断管理，不读取用户内容 |
| BASE-M3 | NOT STARTED | outbound RemoteWorker 注册/证书/重连/fencing，以及节点归属、健康、Drain/Resume 管理 |
| BASE-M4 | NOT STARTED | Kubernetes/客户节点能力、容量、强隔离矩阵，以及真实 placement/资源池/限制界面 |
| BASE-M5 | NOT STARTED | 快照恢复、独立交付、升级/回滚、usage/运维和完整 Admin 视觉/双语/权限验收 |
| BASE-READY | NOT STARTED | [05](05-gates-and-acceptance.md) 十二项全部满足，包括完整 Admin Web；不以 CLI-only、截图或旧 Agent E2E 替代 |
| APP-M1 | PAUSED | 第一阶段完成后推进用户对话；现有 Agent 路径保留兼容并可作为回归负载 |

## 1. 实现与证据边界

整理时核对的历史源码基线 `40812b8945c8b53a8d71b6bc7ef7c757527bcaea` 已有生产 HTTP/JWT/JWKS 路径、租户/配额、直接 Docker/Kubernetes/SSH actuator、Worker health observer 和 User/Admin Web。该基线仍以 Agent/Lease 为核心：同步部署、随 Lease 清理 volume、入站 mTLS Worker 不等价于目标的独立 Workspace、生命周期 reconciler、outbound RemoteWorker。已有 [Agent M5 实测](../../../apps/user-web/M5-ORBSTACK-E2E.md) 与 [Network Policy 配套记录](evidence/admin-web-network-policy-20260905.md) 仅证明各自固定范围；本次未重跑运行时验收。

合并只核对相对主分支集成前 `7bd3f4f3` 的变更均为文档，不代表已重新验收该基线及并行改动的运行行为。后续进入实现任务时须重新固定该任务的 HEAD/dirty/backend/工具版本，只复用仍适用的结论；不用历史未验收推断当前代码不存在。

本轮只改变文档，不运行基础设施 PoC、生产服务、真实客户节点或 Provider E2E，不迁移旧卷、不部署发布、不关闭任何正式 Gate。

## 2. 文档收口问题与处置

| 风险来源 | 可能影响 | 本轮处置 |
| --- | --- | --- |
| 0031 / 01–07 / HTML 的“Admin 配套” | 后端先交付，把管理 UI 推迟到用户阶段 | 0032 明确基础设施＋Admin 是一个交付对象；每项管理能力需两端闭环 |
| 多个 README、旧 06 的 `PAUSED` / “HTTP absent” / 旧 checklist | 选错下一步、重复实现，或把历史范围当作全局停止指令 | 入口只导航，旧 06 归档；当前顺序在 04、状态在本页 |
| 04 的旧 P0～P6 和 07 的旧 ADMIN-M1～M4 链 | 与 BASE 竞争；要求先完成用户 Agent 才做基础设施管理 | 归档旧链；固定历史报告不变，适用迁移/安全条件继续可查 |
| 多处重复源码、工具版本和发布状态 | 局部过期时给出互相矛盾的操作建议 | CLAUDE 精简为入口与稳定约束；当前事实核对源码，工具版本依照可执行配置 |
| 上轮文档仍写“完成后合并” | 在用户已撤销后再次自动集成 | 撤销历史保留；本次集成只依据用户复核后的新明确授权，不外推实现、发布或其他任务权限 |
| 把旧评审文件当作无用文档删除 | 生成器的路径存在性或 review SHA-256 校验失败 | 保留冻结引用；仅删除不承担约束/证据/生成输入的重复报告，精确清单见 04 |
| 旧 M1～M4 提示词动态引用新版 07 §15 | 原任务被追加新底座范围，或真实 Provider 验收被误删，导致无法完成/错误完成 | 使用独立的 ADMIN-WEB-V1、BASE-ADMIN-V1 标识；只在明确任务迁移时切换，不改旧证据结论 |

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
