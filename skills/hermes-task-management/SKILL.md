---
name: task-management
description: "Multi-agent task state management: create, track, checkpoint, resume long-running tasks via Task Driver HTTP API"
---

# Task Management

管理跨会话、跨 agent 的长任务。通过 Go Task Driver HTTP API 实现任务状态持久化，支持断点恢复、plan 调整、压缩感知。

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

# 完成一步
curl -s -X POST http://127.0.0.1:9876/task/{task_id}/step \
  -H 'Content-Type: application/json' \
  -d '{"step_index":0,"status":"completed","actual_outcome":"分析完成，发现3个并发问题"}'

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

## 工作流程

### 1. 开始新任务

```
用户：\"重构订单模块的并发逻辑\"
  → 创建 task（如果在 CLI 中，当前 session_id 可通过 /status 获取）
  → 创建 plan
  → 逐步骤执行
  → 每步完成时调用 /step
  → 关键决策点写 checkpoint
  → 全部完成时调用 /finish
```

### 2. 检测未完成任务（启动时）

每次 Hermes 启动或用户说\"继续\"：

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
\"为什么这么做、下一步打算怎么做\"等上下文感知信息。

### 4. 任务调整

用户说\"第二步的描述不对\"或\"加一个第4步\"：

1. 检查完成状态：如果目标步骤已完成，提示用户不可修改
2. 调用 `/plan-update` 更新计划
3. Driver 自动写 task_modified checkpoint
4. 继续执行

### 5. 暂停和恢复

用户说\"先放着\"：
→ `/finish {end_reason: "paused"}`

用户说\"继续上次的任务\"：
→ `/task/unfinished` → 用户选择 → `/task/{task_id}/resume`

## 注意事项

- task_id 是 Driver 生成的 UUID，不是 Hermes session_id
- agent_session_id 是 Hermes/Claude Code/Codex 各自的 session ID
- Shell Hook 用于自动心跳，实际任务操作由本 Skill 主动调用
- 压缩感知有两种触发方式：
  - Python 插件自动触发 on_pre_compress（压缩前写 checkpoint）
  - Skill 检测 COMPACTION 标记（压缩后补充 checkpoint）
