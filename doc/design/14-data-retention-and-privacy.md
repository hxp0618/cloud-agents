# 14 · 数据留存与隐私合规

企业对数据留存、隐私与合规高度敏感——Agent 运行会产生海量代码、对话、工具输出等需治理的数据。本文定义：哪些数据存多久、谁能访问、如何删除、如何防篡改、如何脱敏。

---

## 1. 数据分类与生命周期

### 1.1 数据分类

```
数据类别（按敏感度从低到高）:
  D1 — 公开配置: Agent/Skill/MCP 定义（不含密钥）、沙箱画像
  D2 — 运行数据: 会话 JSONL、事件日志、工具输出、代码 Diff
  D3 — 用户数据: 用户身份、角色、成员关系、操作记录
  D4 — 敏感数据: 密钥引用、PII、内部 IP/主机名、审计日志
  D5 — 合规关键: 不可变审计事件、安审记录
```

| 类别 | 数据项 | 存储位置 | 加密 at rest | 加密 in transit | 敏感度 |
|---|---|---|---|---|---|
| **D1** | Agent/Skill/MCP 定义、版本、元数据 | PG + S3（Skill 包） | ✅ | ✅ | 低 |
| **D1** | Sandbox 画像、模型白名单 | PG | ✅ | ✅ | 低 |
| **D2** | 会话 JSONL（含对话、工具调用、Diff） | S3（运行时）+ 事件日志 | ✅ | ✅ | **中高** |
| **D2** | 事件流（SSE/Kafka） | Kafka + S3 归档 | ✅ | ✅ | **中高** |
| **D2** | 运行产物（构建输出、报告） | S3 | ✅ | ✅ | 中 |
| **D3** | 用户身份、角色、成员关系 | PG | ✅（列级可加密） | ✅ | 中 |
| **D3** | 操作审计（管理面：创建/删除/授权） | PG + 不可变存储 | ✅ | ✅ | 高 |
| **D4** | 密钥引用（`secret://`） | PG（仅引用） + Vault（明文） | ✅ | ✅ | **极高** |
| **D4** | PII（邮箱、姓名、IP） | PG（标记） + 事件日志（脱敏后） | ✅ | ✅ | **极高** |
| **D5** | 安全事件（security.flagged） | 不可变存储 | ✅ | ✅ | **极高** |
| **D5** | Skill 安审记录 | PG | ✅ | ✅ | 高 |

### 1.2 保留期限与自动清理

```
保留策略 (Retention Policy) 分三级:
  R1 — 短期 (运行期): 沙箱运行期间
  R2 — 中期 (可配置): 默认 90d，按组织设置
  R3 — 长期 (合规): >= 1 年，可配到 7 年
  R∞ — 永久: 不可变版本 (Skill/Agent 定义)
```

| 数据 | 保留级别 | 默认保留期 | 可配置 | 清理机制 | 说明 |
|---|---|---|---|---|---|
| 会话 JSONL | R2 | 90d | ✅ (30d–365d) | 定时任务每日清理过期会话 | 含代码、Diff、工具输出；Run 结束后自动倒计时 |
| 事件流归档 (S3) | R2 | 90d | ✅ (30d–365d) | S3 lifecycle policy → 过期自动删除 | 原始事件归档 |
| 审计事件 (PG) | R3 | 1 年 | ✅ (90d–7 年) | 分区表按月分区 → 过期分区 DROP | 不可删除（仅过期清理） |
| 审计事件 (不可变存储) | R3 | 1 年 | ✅ (90d–7 年) | WORM 存储到期自动清理 | 防篡改副本 |
| 管理操作日志 | R3 | 1 年 | ✅ | 同审计事件 | — |
| 运行产物 | R2 | 90d | ✅ | 随会话删除；重要产物可标记保留 | Git PR/报告等 |
| Skill/Agent 版本 (已发布) | R∞ | 永久 | — | 不可变，不自动清理 | 撤销 ≠ 删除，保留安审记录 |
| Skill 草稿 | R2 | 90d 无编辑后清理 | ✅ | 定时任务 | — |
| 用户与角色 | R3 | 用户离职后 30d 删除 | ✅ (7d–90d) | SCIM 触发 + 人工复核 | 见 §3 |
| 密钥引用 | — | — | — | 密钥轮转时旧版本 7d 后过期 | — |
| 沙箱镜像缓存 | R1 | 7d 未使用清理 | ✅ | 镜像 GC | — |
| 服务日志 (Loki) | R2 | 30d | ✅ | Loki retention policy | — |
| 沙箱 stdout/stderr | R1 | 7d | ✅ | — | — |

### 1.3 会话 JSONL 的分层保留

```mermaid
flowchart LR
    subgraph Active["活跃期 (Run 运行中)"]
        Hot["内存 buffer + 本地 JSONL + 对象存储"]
    end
    subgraph Cool["冷却期 (Run 结束后 0-90d)"]
        Cool1["对象存储: 完整会话 JSONL 可回放"]
        Cool2["事件日志: 归档到 S3"]
        Cool3["控制台可回放、审计可检索"]
    end
    subgraph Archive["归档期 (90d-1y, 可选)"]
        Arch1["仅保留摘要: runId, 开始/结束, 模型, 成本"]
        Arch2["会话 JSONL 可选归档到 Glacier"]
        Arch3["审计事件仍保留完整"]
    end
    subgraph Purge["清理期"]
        Purged["会话 JSONL 永久删除<br/>审计事件保留摘要"]
    end
    Active --> Cool --> Archive --> Purge
```

---

## 2. 跨作用域数据隔离

### 2.1 隔离矩阵

| 资源类型 | 组织内跨业务组 | 业务组内跨项目 | 同一项目内 | 说明 |
|---|---|---|---|---|
| Agent/Skill/MCP 定义 | ✅ 可见（若可见域允许） | ✅ 可见（若可见域允许） | ✅ 全部可见 | 由可见域控制（见 [05](./05-rbac-and-governance.md)） |
| Run 列表 | ❌ 不可见（默认） | ❌ 不可见（默认） | ✅ 可见（项目成员） | RBAC 过滤 |
| 会话内容 (JSONL) | ❌ 严格隔离 | ❌ 严格隔离 | ✅ 项目成员可查看 | RBAC + 存储分区 |
| 事件流 (SSE) | ❌ 不可订阅 | ❌ 不可订阅 | ✅ 项目成员可订阅 | RBAC 过滤订阅 |
| 审计事件 | ❌ 不可见（默认） | ❌ 不可见（默认） | ❌ 不可见（仅审计员可见） | 审计员跨域只读 |
| 用量/成本 | ❌ 不可见（默认） | ❌ 不可见（默认） | ✅ 项目成员可见 | 管理员可看所属域 |
| 用户列表 | ❌ 不可见 | ✅ 同业务组成员可见 | ✅ 同项目成员可见 | — |
| Skill 草稿 | ❌ 仅作者可见 | ❌ 仅作者可见 | ❌ 仅作者可见 | 私有作用域 |

### 2.2 存储层隔离实现

| 存储 | 隔离方式 |
|---|---|
| **PostgreSQL** | 表级别：行含 `org_id/group_id/project_id` 列；所有查询强制带 scope filter（中间件）；可选 Row-Level Security 兜底 |
| **对象存储 (S3/MinIO)** | 前缀分区：`s3://polaris/<org_id>/<project_id>/runs/<run_id>/`；IAM policy 按前缀授权 |
| **事件日志 (Kafka)** | 单 topic 或按 org 分 topic；消费时按 scope 过滤（Partition key = org_id） |
| **SSE 流** | Gateway 层按订阅者 RBAC scope 过滤事件下发（见 [08 §4.1](./08-observability-and-sse.md)） |

---

## 3. 用户数据删除（GDPR "被遗忘权"）

### 3.1 删除范围与流程

```mermaid
sequenceDiagram
    participant HR as HR/管理员
    participant IAM as IAM 服务
    participant Jobs as 清理 Job
    participant PG as PostgreSQL
    participant S3 as 对象存储
    participant Audit as 审计存储

    HR->>IAM: 发起用户删除请求 (GDPR)
    IAM->>IAM: 标记用户为 pending_deletion<br/>(宽限期 7d，可撤销)
    Note over IAM: 7d 宽限期后自动执行:
    IAM->>PG: 匿名化用户身份<br/>(姓名→deleted_user_xxx<br/>邮箱→deleted_xxx@anon.local)
    IAM->>PG: 删除成员关系 (Membership)
    IAM->>IAM: 吊销所有令牌 + 拒绝新登录
    IAM->>Jobs: 触发数据清理
    Jobs->>S3: 删除该用户的私有 Skill/会话?
    Note over Jobs,S3: 会话属于项目 (组织资产)<br/>不被删除；但用户身份匿名化
    Jobs->>Audit: 审计事件不可删除 (合规要求)<br/>但用户身份可匿名化
    Note over Audit: 审计事件中的 user_id<br/>→ 匿名化但保留事件完整性
```

### 3.2 删除 vs 匿名化

| 数据类型 | 操作 | 原因 |
|---|---|---|
| 用户身份（姓名、邮箱） | **匿名化**（`user_deleted_<hash>@anon.local`） | 审计事件需保留归因链；会话归属需保留一致性 |
| 成员关系 (Membership) | **硬删除** | 离职 = 不再有权限 |
| 用户创建的私有 Skill（未发布） | **硬删除**（宽限期后） | 用户个人数据 |
| 用户已发布的 Skill | **保留 + 匿名化作者** | 已发布 Skill 是组织资产；作者匿名化 |
| 用户发起的 Run（会话 JSONL） | **保留 + 匿名化发起者** | 会话是组织资产（含项目上下文） |
| 用户的操作审计记录 | **保留 + 匿名化 operator** | 合规要求；不可删除 |
| 用户 API 令牌 | **立即吊销** | 安全 |
| 用户关联的密钥引用 | **立即吊销 + 轮转** | 安全 |

### 3.3 删除 SLA

| 阶段 | SLA | 说明 |
|---|---|---|
| 删除请求 → 标记 pending_deletion | < 1h | 管理员手动或 SCIM 自动触发 |
| pending_deletion → 匿名化完成 | < 7d（宽限期后自动执行） | 宽限期内可撤销 |
| 私有数据硬删除 | < 30d | 从 S3/PG 中完全移除 |
| 删除完成确认 | 提供删除报告（删了哪些、匿名化了哪些） | — |

---

## 4. 审计日志防篡改

### 4.1 技术方案

```
方案: Append-only + 哈希链 (Merkle Tree)

结构:
  每审计事件:
    {
      id: event_id,
      prev_hash: SHA-256(前一个事件的序列化),
      data: { ...事件内容... },
      ts: timestamp
    }

  每 N 个事件 (如 1000):
    构建 Merkle Tree → root hash 写入:
      ① 区块存证 (如以太坊测试网 / 联盟链可选)
      ② 异地 WORM 存储

验证:
  给定 event_id, 可验证:
    1. 该事件存在 (Merkle proof → root hash 一致)
    2. 未被篡改 (prev_hash 链完整)
    3. 未被删除 (序列连续、无 gap)
```

### 4.2 实现阶段

| 阶段 | 实现 | 防篡改强度 |
|---|---|---|
| **P1 基础** | append-only 表 + 无 DELETE 权限（PG 权限控制） | 弱（DB 管理员可绕过） |
| **P3 增强** | 哈希链（每事件含 prev_hash）+ 定期 root hash 输出到 S3（WORM lock） | 中（需同时攻破 PG + S3） |
| **P5 完整** | 默克尔树 + 外部存证（区块链/信任锚） | 强 |

### 4.3 审计事件不可删除原则

- 审计事件表**不存在 DELETE SQL 权限**（PG 用户权限层控制）
- 设计上无"删除审计"API
- 物理过期通过**分区按月 DROP**（非 DELETE），且仅在保留期过后
- 保留期内的审计事件：绝不可删除（GDPR 例外——见 §3.2 匿名化替代删除）

---

## 5. 事件流脱敏规则

### 5.1 脱敏分层

```
脱敏时机:
  Level 0 — 原始数据: 沙箱内，未经处理
  Level 1 — 发射前脱敏: 平台扩展在写事件前执行 (见 [08 §1](./08-observability-and-sse.md))
  Level 2 — 审计存储: 审计事件中 PII 进一步脱敏
  Level 3 — 客户端下发: SSE 经 RBAC 过滤 + 脱敏后下发
```

### 5.2 脱敏规则全集

| 类别 | 模式 | 示例 | 脱敏方式 | 适用层 |
|---|---|---|---|---|
| **密钥** | API Key | `sk-abc123...`、`sk-ant-...`、`AIza...` | `sk-***[REDACTED]` | L1/L2/L3 |
| **密钥** | JWT Token | `eyJhbGciOi...` | `eyJ***[REDACTED]` | L1/L2/L3 |
| **密钥** | 密码参数 | `--password secret`、`password=secret` | `--password [REDACTED]` | L1/L2/L3 |
| **密钥** | 私钥 (PEM) | `-----BEGIN RSA PRIVATE KEY-----` | `[PRIVATE_KEY_REDACTED]` | L1/L2/L3 |
| **密钥** | 连接字符串 | `mysql://user:pass@host/db` | `mysql://user:[REDACTED]@host/db` | L1/L2/L3 |
| **PII** | 邮箱 | `user@corp.com` | `u***@corp.com`（保持域名） | L1/L2 |
| **PII** | 姓名 (中文) | `张三` | `张*` | L1/L2 |
| **PII** | 姓名 (英文) | `John Doe` | `J*** D**` | L1/L2 |
| **PII** | 手机号 | `13812345678` | `138****5678` | L1/L2 |
| **PII** | 身份证号 | `110101199001011234` | `110101********1234` | L1/L2 |
| **网络** | 内网 IPv4 | `10.x.x.x`、`172.16-31.x.x`、`192.168.x.x` | `10.X.X.X` | L1/L2 |
| **网络** | 公网 IPv4 | `203.0.113.5` | `203.0.113.X`（保留 /24） | L2 |
| **网络** | 内网主机名 | `db-prod.internal.corp` | `db-prod.***`（保留前缀） | L2 |
| **路径** | 敏感路径 | `~/.ssh/id_rsa`、`/etc/shadow` | 保留（本身就是安全警告） | — |
| **文件** | `.env` 内容 | `SECRET_KEY=value` | `[ENV_FILE_REDACTED]` | L1/L2 |
| **Token** | OAuth token | `ghp_...`、`gho_...` | `ghp_[REDACTED]` | L1/L2/L3 |
| **Token** | Session cookie | `session=abc123` | `session=[REDACTED]` | L1/L2 |

### 5.3 脱敏实现位置

```
脱敏责任链:
  平台扩展 (沙箱内, Level 1):
    - 在 tool_call.requested 事件中脱敏 bash/write 参数
    - 在 tool_call.result 事件中脱敏工具输出
    - 在 message_update.text_delta 中脱敏模型流式输出? ← 不实时脱敏 (性能)
      但 message.completed 时全量检查

  Event Hub (控制面, Level 2):
    - 二次验证：所有入库事件做 schema 校验 + 脱敏规则复核
    - 审计存储前做 Level 2 脱敏

  API Gateway (控制面, Level 3):
    - SSE 下发前做 Level 3 RBAC 过滤 + 脱敏
```

---

## 6. 合规框架映射

### 6.1 GDPR 合规清单

| GDPR 条款 | 要求 | Polaris 实现 |
|---|---|---|
| **Art.5 数据最小化** | 只收集必要数据 | 脱敏规则减少 PII 入库；会话保留期自动清理 |
| **Art.17 被遗忘权** | 用户可要求删除数据 | §3 删除/匿名化流程 |
| **Art.20 数据可移植** | 用户可导出数据 | 会话 JSONL + 配置可导出（API `GET /v1/users/me/exports`） |
| **Art.25 隐私设计** | 默认隐私保护 | 脱敏在发射前（Level 1），非事后 |
| **Art.30 处理活动记录** | 记录所有处理活动 | 审计日志即处理活动记录 |
| **Art.32 安全** | 适当的技术安全措施 | 见 [11](./11-security-and-threat-model.md) |
| **Art.35 DPIA** | 数据保护影响评估 | 本文即 DPIA 的输入材料 |
| **Art.44-49 跨境传输** | 数据不出 EU | 自托管/VPC 部署；模型数据按合规路由 |

### 6.2 SOC 2 控制映射

| Trust Service Criteria | Polaris 控制 |
|---|---|
| **CC6.1 逻辑/物理访问控制** | RBAC + 四级作用域 + 工具策略闸门 |
| **CC6.3 数据 confidentiality** | 加密 at rest/in transit + 脱敏 + 密钥间接引用 |
| **CC6.6 外部通信保护** | TLS 1.3 + mTLS + egress 白名单 |
| **CC7.1 变更检测** | 审计事件不可篡改 |
| **CC7.2 系统监控** | 健康检查 + SLO 告警（见 [13](./13-operations-manual.md)） |
| **CC7.3 事件响应** | 安全事件响应流程（见 [11 §7](./11-security-and-threat-model.md)） |
| **CC7.5 恢复计划** | 备份恢复 + DR 演练（见 [13 §2](./13-operations-manual.md)） |

### 6.3 数据驻留 (Data Residency)

| 部署形态 | 数据驻留 | 模型数据 |
|---|---|---|
| **自托管/VPC (主)** | 所有数据在客户 VPC 内 | 模型调用可选路由到 ZDR/私有模型 |
| **SaaS 多租户 (扩展)** | 按 tenant 隔离 + 可选区域选择（US/EU/APAC） | LLM Gateway 合规路由到同区域 provider |
| **ZDR (Zero Data Retention)** | 模型 provider 不保留数据 | 配置 provider 的 opt-out；优先私有部署模型 |

---

## 7. 数据导出 API

```
用户数据导出:
  GET /v1/users/me/exports
  → 打包: 该用户的配置、私有 Skill、贡献记录
  → 格式: JSONL + YAML
  → SLA: 发起后 72h 内提供下载链接

项目数据导出:
  POST /v1/projects/{id}/exports (admin)
  → 打包: Agent/Skill/MCP 定义、Run 历史摘要（不含会话全文）
  → 格式: JSONL + YAML
  → SLA: 发起后 7d 内提供

组织配置导出:
  POST /v1/orgs/exports (super admin)
  → 打包: 组织配置（不含用户 PII、审计、会话）
  → 格式: YAML（可直接用于 Import 迁移）
```

---

## 8. 与需求对应

- 审计链路：[08](./08-observability-and-sse.md) 事件溯源 + 本文 §4
- 沙箱数据安全：[07](./07-sandbox-isolation.md) + 本文 §1.3、§2
- 密钥管理：[04](./04-pi-integration-and-multi-llm.md) LLM Gateway + 本文 §5.2
- 权限隔离：[05](./05-rbac-and-governance.md) + 本文 §2
- 合规：[10](./10-roadmap-and-open-questions.md) D9 + 本文 §6
- 备份恢复：[13](./13-operations-manual.md) §2

---

> 💡 **如何阅读**：DPO/合规官看 §1（数据分类+保留）+ §3（被遗忘权）+ §5（脱敏）+ §6（合规映射）；安全工程师看 §4（审计防篡改）+ §5.2（脱敏规则全集）；DevOps 看 §1.3（清理任务）+ §2.2（存储隔离）。
