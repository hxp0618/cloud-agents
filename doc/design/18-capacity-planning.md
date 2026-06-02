# 18 · 容量规划指南

本文给出从并发 Agent 数反推所需基础设施资源的估算公式与配置参考。适用于自托管/VPC 部署的容量规划与采购决策。

> ⚠️ 本文属 P3+ 参考文档，基于设计假设给出数量级估算；实际值需在 P0 MVP 后通过基准测试校准。

---

## 1. 核心公式

```
单 Agent Run 资源消耗 = 模型消耗（外部的）+ 沙箱消耗（数据面的）+ 控制面摊销

其中:
  - 模型消耗由外部 provider 或自建 vLLM 承担（见 §4 模型容量）
  - 沙箱消耗由 K8s 数据面节点承担
  - 控制面按并发 Run 数线性摊销
```

---

## 2. 沙箱（数据面）容量

### 2.1 单沙箱资源画像

| 沙箱类型 | CPU (request/limit) | 内存 (request/limit) | 磁盘 | 典型用途 |
|---|---|---|---|---|
| **small** | 0.5 / 1 core | 512Mi / 1Gi | 5Gi | 简单代码编辑、文档生成 |
| **medium**（默认）| 1 / 2 core | 1Gi / 2Gi | 10Gi | 代码 Review、测试运行 |
| **large** | 2 / 4 core | 2Gi / 4Gi | 20Gi | 大型重构、全量测试 |
| **xlarge** | 4 / 8 core | 4Gi / 8Gi | 50Gi | 多仓库、构建、训练 |

### 2.2 数据面节点估算

```
公式:
  required_nodes = ceil(concurrent_runs × avg_cpu_per_sandbox / allocatable_cpu_per_node × buffer_factor)

其中:
  - concurrent_runs: 峰值并发 Run 数
  - avg_cpu_per_sandbox: 平均每沙箱 CPU request（默认 medium = 1 core）
  - allocatable_cpu_per_node: 每节点可分配 CPU（≈ 节点 CPU 核数 - 系统预留 1-2 core）
  - buffer_factor: 余量系数（含预热池 + 调度碎片 + 峰值缓冲），建议 1.5–2.0

示例（参考节点: 16c/32G）:
  并发 20 Run × 1 core × 1.5 / 14 allocatable = 2.1 → 3 节点
  并发 100 Run × 1 core × 1.5 / 14 allocatable = 10.7 → 11 节点
  并发 1000 Run × 1 core × 1.3 / 14 allocatable = 92.9 → 93 节点
```

### 2.3 参考配置表

| 场景 | 峰值并发 | 节点规格 | 节点数 | 月成本（参考 $0.05/core/hr） |
|---|---|---|---|---|
| **开发 / POC** | 10 | 8c/16G × 1 | 1 | ~$288 |
| **小团队 (5-10 人)** | 20 | 16c/32G × 3 | 3 | ~$1,728 |
| **中型团队 (20-50 人)** | 100 | 16c/32G × 11 | 11 | ~$6,336 |
| **大型团队 (100+ 人)** | 500 | 32c/64G × 22 | 22 | ~$25,344 |
| **企业 (>500 人)** | 1000+ | 32c/64G × 47 | 47 | ~$54,144 |

> 💡 上述为常规实例成本；使用 Spot/可抢占实例可节省 60–70%（见 [17 §2.3](./17-cost-optimization.md)）。

### 2.4 存储容量

| 数据类型 | 单 Run 估算 | 并发 × 留存 | 总容量（中型团队） |
|---|---|---|---|
| 会话 JSONL | 1–5 MB | 100 × 90d ≈ 9000 个会话 | 9–45 GB（热存储） |
| 事件日志 (raw) | 100–500 KB | 持续流式写入 | 100–500 GB（90d 热+温） |
| 事件归档 (parquet) | 压缩 10× | 1 年留存 | 30–150 GB（冷存储） |
| Skill 包 | 1–10 MB / 版本 | 100 Skill × 5 版本 | 0.5–5 GB |
| PG 元数据 | 0.1–1 MB / Run | 含索引 | 10–50 GB（含审计索引） |

---

## 3. 控制面容量

### 3.1 组件资源需求

| 组件 | 副本数（默认/最少） | CPU/副本 | 内存/副本 | 扩展维度 |
|---|---|---|---|---|
| API Gateway | 2 / 2 | 1–2 core | 512Mi–1Gi | 请求量 + SSE 连接数 |
| IAM | 2 / 1 | 0.5–1 core | 512Mi | 请求量 |
| Catalog | 2 / 1 | 0.5–1 core | 512Mi | 请求量 |
| Orchestrator | 2 / 1 | 1–2 core | 1–2Gi | **并发 Run 数**（关键） |
| Event Hub | 2 / 2 | 1–2 core | 1–2Gi | 事件吞吐量 |
| LLM Gateway | 2 / 2 | 1–2 core | 512Mi–1Gi | 模型调用速率 |
| Review | 1 / 1 | 0.5–1 core | 512Mi | Skill 提交频率 |

### 3.2 Orchestrator 容量（瓶颈点）

Orchestrator 是**唯一有状态**的控制面组件，也是并发 Run 的主要瓶颈：

```
单 Orchestrator 副本容量:
  - 最大并发 RPC 子进程数: ~100（受 OS fd limit + Node 事件循环限制）
  - 推荐并发: ≤80 / 副本（留 20% 余量）
  - 内存: ~10MB / Run（RPC 连接状态 + 事件缓冲区）

扩容公式:
  orchestrator_replicas = ceil(peak_concurrent_runs / 80)

示例:
  并发 100 Run → 2 副本
  并发 500 Run → 7 副本
  并发 1000 Run → 13 副本
```

### 3.3 控制面总节点估算

中型团队（并发 100 Run）控制面总需求：
- API Gateway: 2 × 1 core = 2 core
- IAM + Catalog + Review: 3 × 2 × 0.5 core = 3 core
- Orchestrator: 2 × 2 core = 4 core
- Event Hub: 2 × 2 core = 4 core
- LLM Gateway: 2 × 1 core = 2 core
- Redis + PG: 托管服务或 2 × 4 core = 8 core
- **总计**: ~23 core → 2 节点（16c each）或 3 节点（8c each）

---

## 4. 模型容量（自建 vLLM 场景）

若不完全依赖外部 provider，自建 vLLM 推理服务的容量估算：

```
模型推理吞吐:
  - 单张 A100 (80GB): 可服务 1×70B 模型 或 2×13B 模型
  - 平均每请求: ~1000 prompt + 500 completion tokens
  - 平均生成速度: ~50 tok/s (13B) / ~30 tok/s (70B)
  - 每 GPU 并发请求: 5–10（受 KV cache 限制）

Agent 模型调用特点:
  - 不是持续流式——是 burst：每次 turn 一次调用
  - 平均每 Run 10–20 次模型调用
  - 平均每 turn 间隔 30s–2min

估算:
  并发 100 Run × 平均 0.1 次调用/s（每 10s 一次 turn）
  = 10 并发请求
  → 1–2 张 A100 (13B) 或 2–4 张 A100 (70B)

建议: 先用外部 provider（按量付费）验证用量，再决定是否自建 GPU 集群
```

---

## 5. 网络带宽

| 流量类型 | 每 Run 估算 | 中型团队（100 并发）|
|---|---|---|
| SSE 事件流（下行） | 100 KB/s | 10 MB/s (80 Mbps) |
| LLM API 调用 | 10–50 KB/次，10–20 次/Run | 5–10 MB/s |
| 对象存储上传（会话/产物） | 1–5 MB/Run | burst 50 MB/s |
| Git clone / npm install | 10–500 MB | burst 500 MB/s |

建议：数据面节点 ≥ 1 Gbps；控制面出口带宽 ≥ 200 Mbps。

---

## 6. 快速参考卡

```
场景速查:
  POC (10 并发):
    数据面: 1×8c/16G + Docker
    控制面: 1×8c/16G (所有组件单副本)
    模型: 外部 provider 按量
    PG: 本地或单节点 2c/8G

  小团队 (20 并发):
    数据面: 3×16c/32G + gVisor
    控制面: 2×8c/16G
    模型: 外部 provider
    PG: 托管 4c/16G HA
    Redis: 托管 2c/4G

  中型团队 (100 并发):
    数据面: 11×16c/32G + gVisor
    控制面: 3×16c/32G
    模型: 外部 provider + 可选自建 vLLM (1-2 GPU)
    PG: 托管 8c/32G HA
    Redis: 托管 4c/8G Cluster
    Kafka: 3×4c/16G

  大型 (500+ 并发):
    数据面: 22×32c/64G + gVisor/Kata
    控制面: 5×16c/32G
    模型: 自建 vLLM 集群 (4-8 GPU) + 外部兜底
    PG: 托管 16c/64G HA + 读副本
    Redis: 托管 8c/16G Cluster
    Kafka: 5×8c/32G
```

---

## 7. 基准测试校准

实施后需通过基准测试校准本文估算：

```
基准测试清单:
  □ 单沙箱冷/热启动延迟（p50/p95/p99）
  □ 单沙箱在典型工作负载下的平均 CPU/内存
  □ Orchestrator 在不同并发下的 CPU/内存/延迟
  □ SSE 事件吞吐量 vs 延迟（1/10/50/100 订阅者）
  □ LLM Gateway 代理延迟增加量 (overhead)
  □ 会话 JSONL 平均大小（按 Agent 类型）
  □ PG 查询延迟与连接池利用率

校准后更新本文表格 + 在部署文档中提供 sizing calculator
```

---

> 💡 **如何阅读**：Infra/SRE 看 §2–§3（沙箱+控制面容量）+ §6（快速参考）；FinOps 看 §4（模型成本）+ [17](./17-cost-optimization.md)；采购看 §2.3（参考配置表）+ §6。
