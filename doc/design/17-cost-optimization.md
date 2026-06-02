# 17 · 成本优化策略

本文定义平台各层的成本优化手段：模型侧（prompt caching、模型路由）、沙箱侧（预热池策略、Spot 实例）、存储侧（分层存储、过期清理）。目标：在不牺牲安全与隔离的前提下，把单次 Run 的信用成本压到最低。

> ⚠️ 本文属 P3+ 优化专题，基线设计已保证成本可控（配额/预算告警见 [08](./08-observability-and-sse.md)，计量见 [08 §4.2](./08-observability-and-sse.md)），本文在此之上做深度优化。

---

## 1. 模型侧优化

### 1.1 Prompt Caching

pi 与 Anthropic/OpenAI 等 provider 支持 prompt caching，可大幅降低重复上下文的 token 成本。

**缓存策略分层**：

| 缓存层 | 位置 | 命中场景 | 预期节省 |
|---|---|---|---|
| **System Prompt** | 模型 provider 侧 | 同一 Agent 定义的所有 Run 共享 system prompt（不变部分） | 30–50% 输入 token |
| **Skill 正文** | 模型 provider 侧 | 同一次 Run 中多次触发同一 Skill | 10–20% 输入 token |
| **工具定义** | 模型 provider 侧 | 同一 Agent 的所有 Run 共享工具 schema | 5–15% 输入 token |
| **会话上下文前缀** | 模型 provider 侧 | 长会话中，前面的 turn 不变 | 20–40% 输入 token（长会话） |
| **LLM Gateway 语义缓存** | Gateway 侧 | 相同/相似 prompt 重用上次响应（温度=0 场景） | 100% 该次调用（语义匹配） |

**实现要点**：

```yaml
# AgentDefinition 中的缓存配置
caching:
  systemPrompt:
    strategy: "static_prefix"       # 固定前缀标记为可缓存
    cacheBreakpoints: ["{{user_input}}"]  # 变量插入点标记缓存断点
  tools:
    strategy: "static"              # 工具定义固定不变 → 全缓存
  skills:
    strategy: "dynamic"             # 含变量，按需标记
```

**LLM Gateway 语义缓存**：

```
语义缓存流程:
  1. 请求到达 Gateway
  2. 计算 prompt embedding（轻量模型）
  3. 查向量索引 → 相似度 > 0.95 且温度=0 的历史响应
  4. 命中 → 直接返回缓存响应 + 计量标记 cache_hit
  5. 未命中 → 转发到 provider → 响应入库（含 embedding + TTL）
```

```yaml
# Gateway 语义缓存配置
semanticCache:
  enabled: true
  ttl: 3600                       # 缓存 1 小时
  similarityThreshold: 0.95
  maxEntries: 10000
  onlyForModels: ["claude-haiku-*"]  # 便宜模型不缓存（缓存本身有成本）
  excludePatterns:                  # 不含用户输入的 prompt 才缓存
    - "fix|repair|debug"            # 任务不同但 prompt 相似的场景
```

### 1.2 模型路由（Tiered Model Selection）

不是所有任务都需要最强的模型。根据任务复杂度自动选择模型层级：

```yaml
modelRouting:
  strategy: "tiered"
  tiers:
    - tier: "simple"
      examples: ["format code", "write README", "add comment"]
      model: "claude-haiku-4-5"     # 最便宜
      maxTokens: 4000

    - tier: "standard"
      default: true                 # 默认走这层
      model: "claude-sonnet-4-6"
      maxTokens: 16000

    - tier: "complex"
      examples: ["refactor across multiple files", "debug race condition"]
      model: "claude-opus-4-8"
      maxTokens: 32000

  # 自动降级: 子 Agent 默认走 simple/standard
  subAgentDefault: "standard"

  # 用户可显式覆盖
  userOverride: true               # 允许用户在 Run 请求中指定 tier
```

**自动路由判断**：

```ts
// Orchestrator 在 Run 创建时做初始判断
function routeModel(input: string, agentDef: AgentDefinition): ModelTier {
  // 1. 用户显式指定 > 一切
  if (input.tier) return input.tier;

  // 2. 任务分类器（轻量模型）判断复杂度
  const complexity = await classifyComplexity(input.prompt);
  if (complexity === "complex") return "complex";
  if (complexity === "simple") return "simple";

  // 3. Agent 定义中的默认值
  return agentDef.modelTier ?? "standard";
}
```

**成本影响**：simple 任务走 haiku 可省 80–90% token 成本（vs opus）；standard 任务走 sonnet 可省 50–60%（vs opus）。

### 1.3 Token 优化

| 优化 | 实现 | 节省 |
|---|---|---|
| **工具描述精简** | 每个工具描述 ≤ 200 token；去掉不必要的参数说明 | 1–5k token / turn |
| **渐进式 Skill 披露** | pi 原生只注入 name+description，按需 read 全文 | 5–50k token（取决于 Skill 数量） |
| **MCP 懒加载** | `pi-mcp-adapter` 单代理 `mcp` 工具 ≈200 token，而非一次性注册所有工具 | 10k+ token（MCP server 多时） |
| **上下文压缩** | pi 的 auto-compaction：自动压缩旧消息 | 可保持上下文在 50k token 内 |
| **Turn 合并** | 连续多个只读操作（read/ls/grep）合并为一个工具调用 | 减少 round-trip × token 开销 |
| **响应截断** | 非关键输出限制长度（如 `git log` 只取最后 50 行） | 1–10k token / turn |

---

## 2. 沙箱侧优化

### 2.1 预热池策略

```
预热池配置:
  pool:
    minSize: 3                      # 最少保持 3 个预热沙箱
    maxSize: 20                     # 最多 20 个（根据并发自动调整）
    targetUtilization: 0.7          # 池利用率 > 70% 时自动扩容
    idleTimeout: 600                # 空闲沙箱 10 分钟回收

  预热分级:
    hot:                            # < 1s 分配（预启动 pi 进程）
      size: 3
      image: "polaris/sandbox:latest"
    warm:                           # < 5s 分配（容器已就绪，pi 未启动）
      size: 5
      image: "polaris/sandbox:latest"
    cold:                           # < 30s（按需创建，含镜像拉取）
      onDemand: true
```

**自适应扩缩**：

```
- 过去 5 分钟平均 Run 创建速率 × 平均 Run 时长 = 预估并发数
- 池大小 = 预估并发数 × 1.3 (30% buffer)
- 夜间/周末自动缩减到 minSize
```

### 2.2 沙箱复用策略

同一用户的连续 Run 可复用沙箱（减少冷启动延迟 + 成本）：

```yaml
sandboxReuse:
  enabled: true
  maxReuseCount: 5                  # 同一沙箱最多复用 5 次
  maxReuseAge: 3600                 # 1 小时后不再复用
  cleanupBetweenRuns:               # 两次 Run 之间清理
    - resetWorkdir                  # 清空工作区（保留工具链）
    - verifyIntegrity               # 验证沙箱完整性
  prohibitedFor:                    # 以下场景禁止复用
    - sandboxProfile.isHighSecurity # 高安全画像
    - previousRun.hasSecurityFlag   # 上次 Run 有安全事件
```

### 2.3 Spot / 可抢占实例（云部署）

```
数据面 Worker 节点可使用 Spot 实例:
  - 控制面: 常规实例（不可抢占）
  - 数据面: Spot 实例 + 少量常规实例兜底
  - Spot 被回收前 2min 通知 → 优雅 drain 该节点上的沙箱
  - 受影响 Run 自动迁移到常规节点（由 Orchestrator 恢复）
  
预计节省: 60–70% 计算成本（vs 全常规实例）
```

---

## 3. 存储侧优化

### 3.1 分层存储

```
会话 JSONL 的生命周期存储:
  活跃期 (0-7d):    热存储 (S3 Standard / MinIO) — 频繁回放
  冷却期 (7-90d):   温存储 (S3 Standard-IA) — 偶尔审计
  归档期 (90d-1y):  冷存储 (S3 Glacier / MinIO 压缩) — 仅合规
  清理:             自动过期删除 (见 [14](./14-data-retention-and-privacy.md))
```

### 3.2 事件日志压缩

```
Kafka / NATTS 事件日志:
  - 原始事件 → 保留 7d（热）
  - 压缩事件（去重 + 聚合）→ 保留 90d（温）
  - 归档到 S3（parquet 格式，列存 + 压缩）→ 保留 >= 1 年
  - 压缩比: ~5-10×（parquet + snappy）
```

---

## 4. 综合成本模型

```
单次 Run 成本 = 模型成本 + 沙箱成本 + 存储成本

模型成本:
  = Σ (每模型调用 prompt_tokens × prompt_price + completion_tokens × completion_price)
  - 缓存命中 prompt_tokens: 按 cached_price (通常 10% 原价)
  - 语义缓存命中: 0（仅计 embedding 成本）

沙箱成本:
  = 沙箱时长 × 实例单价
  - 预热池分摊: 预热池闲置成本平均到每次 Run
  - Spot 折扣: Spot 实例为常规 30–40%

存储成本:
  = Σ (每 GB × 存储层级单价 × 留存时长)
  - Standard: $0.023/GB/mo
  - IA: $0.0125/GB/mo
  - Glacier: $0.004/GB/mo

示例（典型 Run）:
  模型: sonnet, 10K prompt + 2K completion, 缓存命中 60%
    → 10000×$3/M × 40% + 10000×$0.3/M × 60% + 2000×$15/M
    → $0.012 + $0.0018 + $0.03 = $0.0438
  沙箱: 5min, $0.01/min → $0.05
  存储: 会话 5MB, 保留 90d → ~$0.0001

  总计: ~$0.094 / Run

优化后（haiku 路由 + 全缓存命中 + Spot + 短会话）:
  模型: haiku, 缓存命中 80% → ~$0.005
  沙箱: Spot, 3min → ~$0.01
  总计: ~$0.015 / Run (节省 84%)
```

---

## 5. 成本优化配置（按作用域可锁定）

```yaml
costPolicy:
  modelTierDefault: "standard"       # 默认模型层级
  allowUserTierOverride: true        # 用户可否指定 tier
  semanticCacheEnabled: true
  sandboxReuseEnabled: true

  spendingLimits:
    perRun: 100                      # 单次 Run 信用上限
    perUserPerDay: 1000
    perProjectPerDay: 5000

  optimizationAggressiveness: "balanced"  # conservative | balanced | aggressive
```

---

## 6. 成本 Dashboard

```
Cost Optimization Dashboard:

Row 1 — 缓存效率:
  - Prompt cache 命中率 (%, 按模型)
  - 语义缓存命中率 (%)
  - 缓存节省的 token / 成本 ($)

Row 2 — 路由效率:
  - 模型使用分布 (haiku/sonnet/opus %)
  - 路由准确率 (用户未覆盖的 %)
  - 自动路由 vs 用户指定带来的节省

Row 3 — 沙箱效率:
  - 预热池命中率 (%)
  - 沙箱复用率 (%)
  - 平均沙箱时长

Row 4 — 浪费检测:
  - 闲置沙箱时长
  - 未命中的语义缓存查询（相似但不够 → 调阈值建议）
  - 过期未清理的会话/产物
```

---

> 💡 **如何阅读**：FinOps 看 §4（综合成本模型）；平台工程师看 §1（模型侧）+ §2（沙箱侧）；SRE 看 §3（存储侧）；所有指标在 §6 Dashboard 中可见。
