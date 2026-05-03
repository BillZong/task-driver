---
name: task-management
description: "Multi-agent task state management: create, track, checkpoint, resume long-running tasks via Task Driver HTTP API"
---

# Task Management

管理跨会话、跨 agent 的长任务。通过 Go Task Driver HTTP API 实现任务状态持久化，支持断点恢复、plan 调整、压缩感知、token 统计。

## 架构

```
Hermes Skill ──curl──► Go Task Driver (:9876) ──► SQLite
                              ▲
Hermes Python 插件 ──HTTP────┘
  (on_pre_compress / on_session_switch)
```

## 前置条件

1. Go Task Driver 已作为后台进程运行
2. Hermes Shell Hook 已配置（`~/.hermes/config.yaml` 的 `hooks:` 段）
3. MemoryProvider 插件已配置（如果要用压缩感知）

## 可用 API（全部通过 curl 调用 :9876）

### 任务生命周期

```bash
# 创建任务
curl -s -X POST http://127.0.0.1:9876/task/create \
  -H 'Content-Type: application/json' \
  -d '{"goal":"重构订单模块","plan":["分析现有代码","设计并发方案","实现改动","测试验证"],"agent_id":"hermes","agent_session_id":"'${HERMES_SESSION_ID}'"}'

# 开始执行
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/start

# 完成一步（带 token 统计）
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/step \
  -H 'Content-Type: application/json' \
  -d '{"step_index":0,"status":"completed","actual_outcome":"分析完成，发现3个并发问题","input_tokens":12345,"output_tokens":6789}'

# 写检查点
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/checkpoint \
  -H 'Content-Type: application/json' \
  -d '{"snapshot_type":"normal","step_index":1,"monologue":"正在设计并发方案..."}'

# 完成任务
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/finish \
  -H 'Content-Type: application/json' \
  -d '{"end_reason":"completed"}'
```

### 计划调整

```bash
# 修改计划（不可修改已完成步骤）
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/plan-update \
  -H 'Content-Type: application/json' \
  -d '{"plan":["步骤1","修改后的步骤2","步骤3","步骤4"],"steps":[{"step_index":1,"description":"修改后的步骤2","expected_outcome":"使用乐观锁实现"}]}'

# 跳过某步
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/step/skip \
  -H 'Content-Type: application/json' \
  -d '{"step_index":2,"reason":"不需要了"}'

# 调整顺序
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/step/reorder \
  -H 'Content-Type: application/json' \
  -d '{"step_order":[2,0,1,3]}'
```

### 查询和恢复

```bash
# 查看未完成的任务
curl -s http://127.0.0.1:9876/task/unfinished

# 查看完整任务状态（含步骤和检查点）
curl -s http://127.0.0.1:9876/task/{task_id}

# 获取恢复信息（含 resume_cmd）
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/resume

# 健康检查
curl -s http://127.0.0.1:9876/health
```

## Token 统计（Prometheus 标准）

不再在 task_steps 表中存储 token 字段。所有 token 数据通过 **Prometheus 标准 `/metrics`** 暴露，支持 Prometheus Server + Grafana 直接抓取和可视化。

### 指标清单

| 指标名 | 类型 | 标签 | 含义 |
|--------|------|------|------|
| `task_token_input_total` | Counter | `task_id, step_index, agent_id` | 输入 token 累积量 |
| `task_token_output_total` | Counter | `task_id, step_index, agent_id` | 输出 token 累积量 |
| `task_token_cache_read_total` | Counter | `task_id, step_index, agent_id` | 缓存读取 token 累积量 |
| `task_token_reasoning_total` | Counter | `task_id, step_index, agent_id` | 推理 token 累积量 |
| `task_token_input_bucket` | Histogram | `task_id, step_index` | 单次上报输入 token 分布 |
| `task_token_output_bucket` | Histogram | `task_id, step_index` | 单次上报输出 token 分布 |
| `task_step_latency_seconds` | Histogram | `task_id, step_index, status` | 步骤执行耗时分布 |

### 上报方式

#### 方式 1：Shell Hook 自动上报（推荐）

Hermes 的 `on_session_end` hook 自动完成：从 `~/.hermes/state.db` 的 `sessions` 表读取当前 session token 统计，POST 到 Driver `/metrics/tokens`。

零 Agent 介入，零 Skill 改动。已集成在 `task-driver-hook.sh` 中。

#### 方式 2：Agent 主动上报（测试/手动场景）

```bash
curl -s -X POST http://127.0.0.1:9876/metrics/tokens \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "5a3f350d-...",
    "step_index": 0,
    "agent_id": "hermes",
    "input_tokens": 50000,
    "output_tokens": 8000,
    "cache_read_tokens": 10000,
    "reasoning_tokens": 2000
  }'

# 上报步骤耗时
curl -s -X POST http://127.0.0.1:9876/metrics/latency \
  -H 'Content-Type: application/json' \
  -d '{
    "task_id": "5a3f350d-...",
    "step_index": 0,
    "status": "completed",
    "duration_seconds": 120.5
  }'
```

### Prometheus 抓取配置

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'task-driver'
    static_configs:
      - targets: ['127.0.0.1:9876']
    metrics_path: '/metrics'
```

### Grafana 面板示例

- **Token 消耗趋势图**：`rate(task_token_input_total[5m])` by task_id
- **步骤耗时 P99**：`histogram_quantile(0.99, rate(task_step_latency_seconds_bucket[5m]))`
- **输入输出比**：`task_token_output_total / task_token_input_total`
- **缓存命中率**：`rate(task_token_cache_read_total[5m]) / rate(task_token_input_total[5m])`

### 对比旧方案（已回滚）

| 维度 | 旧方案（已废弃） | 当前方案 |
|------|----------------|---------|
| 存储 | SQLite task_steps 表加字段 | Prometheus Counter + Histogram |
| 上报方式 | Agent Skill 在 POST /step 时附带 | Hook 自动上报 /metrics/tokens |
| 可观测性 | curl 查 JSON | Prometheus + Grafana |
| 聚合查询 | 自定义 Python 脚本 | PromQL `sum by(task_id)` |
| 告警 | 无 | Prometheus Alertmanager |
| 指标类型 | 原始值 | Counter（累积）+ Histogram（分布） |

## 工作流程

### 1. 开始新任务

```
用户："重构订单模块的并发逻辑"
  → 创建 task（如果在 CLI 中，当前 session_id 可通过 /status 获取）
  → 创建 plan
  → 逐步骤执行
  → 每步完成时调用 /step（附带 token 统计）
  → 关键决策点写 checkpoint
  → 全部完成时调用 /finish
```

### 2. 检测未完成任务（启动时）

每次 Hermes 启动或用户说"继续"：

```bash
curl -s http://127.0.0.1:9876/task/unfinished
```

如果列表不为空，向用户报告：
- 发现 N 个未完成的任务
- 列出每个任务的 goal、current_step、updated_at
- 询问是否继续

如果用户确认继续：

```bash
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/resume
```

返回中包含原 agent_id、agent_session_id、resume_cmd。使用该命令恢复。

### 3. 压缩感知

当检测到 messages 中包含 "[CONTEXT COMPACTION" 前缀的 summary 消息时：

立即执行：
1. 调用 `/task/{task_id}/checkpoint` 写 compression_boundary 检查点
2. 写入当前活跃的精确上下文（文件路径、关键变量、待决策点、git diff）
3. 读取 task_steps 确认 current_step 正确
4. 从 task_steps 的 description 和 expected_outcome 重建上下文
5. 继续下一步

注意：不要重复写入 task_steps 已有的内容。checkpoint 应补充
"为什么这么做、下一步打算怎么做"等上下文感知信息。

### 4. 任务调整

用户说"第二步的描述不对"或"加一个第4步"：

1. 检查完成状态：如果目标步骤已完成，提示用户不可修改
2. 调用 `/plan-update` 更新计划
3. Driver 自动写 task_modified checkpoint
4. 继续执行

### 5. 暂停和恢复

用户说"先放着"：
→ `/finish {end_reason: "paused"}`

用户说"继续上次的任务"：
→ `/task/unfinished` → 用户选择 → `/task/{task_id}/resume`

## 注意事项

- task_id 是 Driver 生成的 UUID，不是 Hermes session_id
- agent_session_id 是 Hermes/Claude Code/Codex 各自的 session ID
- Shell Hook 用于自动心跳，实际任务操作由本 Skill 主动调用
- 压缩感知有两种触发方式：
  - Python 插件自动触发 on_pre_compress（压缩前写 checkpoint）
  - Skill 检测 COMPACTION 标记（压缩后补充 checkpoint）
- 提交 step token 时传 input_tokens 和 output_tokens 即可，cache_read 和 reasoning 暂由 Hermes session 表记录
