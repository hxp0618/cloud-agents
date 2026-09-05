# Cloud Agents 文档与执行入口

当前第一阶段是“长期 Workspace＋通用 Sandbox＋客户节点接入＋完整 Admin Web”；第二阶段才是用户 CloudAgents 对话产品。依据 [ADR-0032 / D-055](adr/0032-infrastructure-admin-delivery-and-document-routing.md)，基础设施与对应管理界面一起实现、一起验收。

## 从这里开始

- [04：文档清理与执行计划](cloud-agents-platform/04-extraction-and-migration.md) 是唯一当前工作顺序，包含精确清理范围和 BASE 联合切片。
- [06：当前状态与下一项](cloud-agents-platform/06-status-tracker.md) 是唯一当前进度记录；不要从旧计划的未完成清单选择下一项。
- [专项文档导航](cloud-agents-platform/README.md) 按问题选择 01/02/03/05/07，不要求每次把整个 docs/plan 读完。
- 上次合并已撤销；用户现已明确要求目标核对一致后合并本轮文档。此次授权只覆盖文档集成与相关 Git 提交，不授权 push、基础设施实现、部署发布或修改其他任务；实际结果见 06。

## Source of truth

本节只维护解释规则，不复制阶段状态：

1. 核对当前用户请求、仍有效的同范围授权与后续明确决定；旧暂停摘要不能覆盖后续明确授权，也不能替代原批准范围。
2. 对具体设计/安全问题，采用相关已批准且未被明确取代的 ADR；当前产品/文档路由看 ADR-0032，其他决定通过 [06](cloud-agents-platform/06-status-tracker.md) 按需定位。后续决定只覆盖其明确取代范围。
3. 01/02/03/04/05/07 各自维护产品、架构、发布、顺序、验收和 Admin 要求；草案或技术候选不因被引用而获准，不因为标题含“方案”就要求每个已授权常规步骤再次确认。
4. 代码、当前配置和固定版本实测回答“已经实现/验证了什么”；目标文档不能证明功能存在，历史缺失记录不能否定后续源码。
5. Synara/T3 专题、history/、p0/、p1/、standalone/、legacy/、references/ 与旧 Gate/E2E 记录按需查询。它们不提供平台默认下一步或全局暂停指令，但仍有效的安全、兼容和固定制品约束继续适用。

明确的旧 ADMIN-M1～M4 任务使用 [ADMIN-WEB-V1](cloud-agents-platform/07-admin-web-requirements-and-design.md#admin-web-v1)；新底座任务使用 BASE-READY 与 [BASE-ADMIN-V1](cloud-agents-platform/07-admin-web-requirements-and-design.md#base-admin-v1)。旧提示词中的“第 15 节全部标准”按该任务原范围解析，不能因章节更新自动扩权或降低原验收。任务范围明确时无需再询问选哪套；只有实际迁移任务范围时才应用对应明确指令。

遇到冲突，指出涉及的具体对象、版本、环境和动作；只暂停必须新决策/权限才能继续的动作，继续独立且已授权的工作。不是所有历史 OPEN Gate 都是当前任务前置条件；也不得通过删除文档免除对应 Gate 的证据/签署。

## 明确授权边界

以下要求保留，不能由“继续”“完成计划”“清理文档”或历史 P0/P1 批准自动外推：

- 生产数据库写入、module/image/Release/npm/Registry 发布；
- 部署、Beta、GA；
- 删除任何脏 worktree。

真实客户节点接入、现有数据/卷迁移、跨仓改动、用户内容访问及正式 Gate closure 仍按相关明确授权、安全合同和签署要求办理；当前没有的权限不能从参考文档推导。
已明确授权的同范围常规开发、验证、幂等重试和状态记录不重复确认；权限、目标环境、费用、数据保留或范围改变时再处理该项所需决定。
Admin Cleanup 的资源名称/generation 与影响确认属于实际产品交互，完整保留在 [07](cloud-agents-platform/07-admin-web-requirements-and-design.md)，不泛化为每次开发都要确认。

## 文档维护规则

- 04 只维护计划，06 只维护实际状态；其他入口不再复制执行清单、PAUSED 段落或源码库存。
- 先记录验收计划，执行后记录真实结果；不能要求先有通过报告才开始已授权实现，也不能把写完文档算作功能完成。
- 内容过期优先改写或移出默认阅读路径。删除按 04 的精确清单执行，核对反向引用、生成/测试依赖、替代位置与 Git 恢复来源。
- 当前专题文档仍在原编号路径；现有 ADR、冻结证据、契约、SQL、许可与来源材料不批量删除。历史引用只导航，不自动加载其全部文本。
- 目录/解释规则变更通过 ADR 记录。本轮记录在 ADR-0032；不更新其他任务、Goal 或自动化。

## 按需历史入口

- [历史状态与决策 registry](cloud-agents-platform/history/06-status-tracker-20260905.md)：旧 P0/P1 固定候选、批准与 Gate 状态，不是当前代码库存。
- [旧迁移计划](cloud-agents-platform/history/04-legacy-migration-plan.md)：实际涉及数据迁移/消费者 cutover 时适用。
- [Gate 证据与模板](cloud-agents-platform/evidence/README.md)：正式 closure 的独立证据/签署要求。
- [Synara/T3 消费者专题](synara-t3-cloud-agent-integration-architecture.md)：后续集成与兼容问题。
- [2026-08 文档迁移 manifest](migration-manifest.json)：来源记录，不是当前执行计划或当前文件 digest 清单。
