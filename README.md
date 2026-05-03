# Task Driver

Agent-agnostic 的任务状态持久化服务。任何 AI Agent（Hermes、Claude Code、Codex）可以通过 HTTP API 记录和管理长任务的状态、步骤、检查点。

## 快速开始

```bash
# 构建
cd /Users/bill/go/src/github.com/BillZong/task-driver
go build -o task-driver .

# 运行（使用默认配置）
./task-driver

# 指定配置
./task-driver --config ~/.config/task-driver/config.yaml
```

## 架构

```
Adapter 层（agent 特定） → HTTP → Go Task Driver (:9876) → SQLite
```

## Agent 适配

| Agent | 适配方式 |
|---|---|
| **Hermes** | Shell Hook（心跳）+ MemoryProvider 插件（压缩感知）+ Hermes Skill（任务管理） |
| **Claude Code** | Hook 脚本（待实现） |
| **Codex** | Hook 脚本（待实现） |

### Hermes 适配

1. **Shell Hook**（`internal/hooks/task-driver-hook.sh`）：配置在 `~/.hermes/config.yaml` 的 `hooks:` 段，自动上报心跳
2. **Python 插件**（`internal/hooks/plugins/memory/task-state-hook/`）：放在 `~/.hermes/plugins/memory/`，捕获 `on_pre_compress` 和 `on_session_switch`
3. **Hermes Skill**（`skills/hermes-task-management/SKILL.md`）：放在 `~/.hermes/skills/`，Agent 通过此 skill 主动调用 API（创建任务、更新步骤、plan 调整、恢复）

### Hermes 配置参考

```yaml
# ~/.hermes/config.yaml
hooks:
  on_session_end:
    - command: "/path/to/task-driver-hook.sh on_session_end"
      timeout: 10
  on_session_finalize:
    - command: "/path/to/task-driver-hook.sh on_session_finalize"
      timeout: 10
  on_session_reset:
    - command: "/path/to/task-driver-hook.sh on_session_reset"
      timeout: 10

memory:
  provider: task-state-hook
```

### Claude Code 适配（待实现）

参考 Claude Code 的 PostToolUse / PreCompact / SessionStart 等 hook 事件。

## API 参考

| Method | Path | Description |
|---|---|---|
| POST | /task/create | 创建任务 |
| POST | /task/{id}/start | 开始执行 |
| POST | /task/{id}/step | 更新步骤 |
| POST | /task/{id}/plan-update | 调整计划 |
| POST | /task/{id}/step/reorder | 调整步骤顺序 |
| POST | /task/{id}/step/skip | 跳过步骤 |
| POST | /task/{id}/checkpoint | 写检查点 |
| POST | /task/{id}/finish | 完成任务 |
| POST | /task/{id}/session | 更新 session |
| POST | /task/{id}/heartbeat | 更新心跳 |
| POST | /task/{id}/resume | 获取恢复信息 |
| GET | /task/{id} | 获取完整状态 |
| GET | /task/unfinished | 获取未完成任务列表 |
| GET | /health | 健康检查 |

## 配置文件

默认路径：`~/.config/task-driver/config.yaml`

```yaml
listen: ":9876"
db: "~/.config/task-driver/tasks.db"
agents:
  hermes:
    resume_cmd: "hermes run --resume {session_id}"
    description: "Hermes Agent"
  claude-code:
    resume_cmd: "claude -r {session_id}"
    description: "Claude Code"
```

## 设计文档

详见 `/Users/bill/Library/Mobile Documents/com~apple~CloudDocs/AI/hermes/task-state-v3-arch.md`
