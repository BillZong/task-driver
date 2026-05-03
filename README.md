# Task Driver

Agent-agnostic 的任务状态持久化服务。任何 AI Agent（Hermes、Claude Code、Codex）可以通过 HTTP API 记录和管理长任务的状态、步骤、检查点。

## 快速安装

```bash
# 一行命令安装（自动下载 task-driver + 安装 agent + 注入 hook）
bash <(curl -fsSL https://raw.githubusercontent.com/BillZong/task-driver/main/tools/install-agents.sh)

# 或者手动下载
curl -fsSL https://github.com/BillZong/task-driver/releases/latest/download/task-driver-linux-amd64 -o /usr/local/bin/task-driver
chmod +x /usr/local/bin/task-driver
```

## 快速开始

```bash
# 前台运行
task-driver

# 后台守护进程
task-driver daemon

# 检查运行状态
task-driver status

# 停止守护进程
task-driver stop
```

## 架构

```
Adapter 层（agent 特定） → HTTP → Go Task Driver (:9876) → SQLite
```

## 子命令

| 命令 | 说明 |
|------|------|
| `task-driver` | 前台运行 HTTP 服务 |
| `task-driver daemon` | 后台守护进程（fork+setsid） |
| `task-driver start` | 同 `daemon` |
| `task-driver stop` | 停止守护进程 |
| `task-driver status` | 检查守护进程运行状态 |
| `task-driver install` | 安装系统服务（Linux: systemd / macOS: launchd） |
| `task-driver uninstall` | 卸载系统服务 |

### Daemon 选项

```
task-driver daemon --config ~/.config/task-driver/config.yaml --db /path/to/tasks.db
```

环境变量 / 标志控制：
- `--config`：配置文件路径（默认 `~/.config/task-driver/config.yaml`）
- `--db`：数据库路径覆盖
- `--pidfile`：PID 文件路径（默认 `~/.config/task-driver/task-driver.pid`）
- `--log-file`：日志文件路径（默认 `~/.config/task-driver/task-driver.log`）

## Agent 安装脚本

`tools/install-agents.sh`（[查看源码](tools/install-agents.sh)）提供一键式安装：

```bash
# 检测+安装所有 agent
bash install-agents.sh

# 仅检测，不安装
bash install-agents.sh --dry-run

# 仅安装指定 agent
bash install-agents.sh --only claude-code,codex

# 强制重新安装
bash install-agents.sh --force
```

自动完成以下操作：

| Agent | 安装方式 | Hook 注入 |
|-------|----------|-----------|
| **task-driver** | 从 GitHub Releases 下载或源码编译 | — |
| **Claude Code** | `npm install -g @anthropic-ai/claude-code` | - |
| **Codex CLI** | 下载预编译二进制 | - |
| **Hermes** | 检测已有安装 | Hook 配置 + Skill + Memory 插件 |

Windows 用户使用 `tools/install-agents.ps1`（[查看源码](tools/install-agents.ps1)）：

```powershell
powershell -ExecutionPolicy Bypass -File install-agents.ps1
```

## Agent 适配

| Agent | 适配方式 |
|-------|----------|
| **Hermes** | Shell Hook（心跳）+ MemoryProvider 插件（压缩感知）+ Hermes Skill（任务管理） |
| **Claude Code** | Hook 脚本（待实现） |
| **Codex** | Hook 脚本（待实现） |

### Hermes 适配

1. **Shell Hook**（`internal/hooks/task-driver-hook.sh`）：配置在 `~/.hermes/config.yaml` 的 `hooks:` 段，自动上报心跳
2. **Python 插件**（`internal/hooks/plugins/memory/task-state-hook/`）：放在 `~/.hermes/plugins/memory/`，捕获 `on_pre_compress` 和 `on_session_switch`
3. **Hermes Skill**（`skills/hermes-task-management/SKILL.md`）：放在 `~/.hermes/skills/`，Agent 通过此 skill 主动调用 API（创建任务、更新步骤、plan 调整、恢复）

安装脚本会自动执行以上三项配置。如需手动配置：

### Hermes 配置参考

```yaml
# ~/.hermes/config.yaml
hooks:
  on_session_end:
    - command: "/usr/local/bin/task-driver-hook on_session_end"
      timeout: 10
  on_session_finalize:
    - command: "/usr/local/bin/task-driver-hook on_session_finalize"
      timeout: 10
  on_session_reset:
    - command: "/usr/local/bin/task-driver-hook on_session_reset"
      timeout: 10

memory:
  provider: task-state-hook
```

### Claude Code / Codex 适配（待实现）

参考 Claude Code 的 PostToolUse / PreCompact / SessionStart 等 hook 事件。

## API 参考

| Method | Path | Description |
|--------|------|-------------|
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
| GET | /metrics | Prometheus 指标（OpenMetrics 格式） |
| POST | /metrics/tokens | 上报 token 用量 |
| POST | /metrics/latency | 上报步骤耗时 |

## 配置文件

默认路径：`~/.config/task-driver/config.yaml`

```yaml
listen: ":9876"
db: "~/.config/task-driver/tasks.db"

# Compression threshold settings (used for dynamic compression_boundary trigger)
compression:
  threshold: 0.50           # Ratio threshold (default: 50%)
  context_window: 1048576   # Model context window (default: 1M for DeepSeek V4 Pro)

agents:
  hermes:
    resume_cmd: "hermes run --resume {session_id}"
    description: "Hermes Agent"
  claude-code:
    resume_cmd: "claude -r {session_id}"
    description: "Claude Code"
```

## 全量测试

```bash
# 所有模块测试
go test ./... -v -count=1 -race

# 单模块
go test ./internal/daemon/ -v -count=1 -race
```

## 版本历史

- **v0.2.0** – 守护进程模式（daemon/stop/status/install） + Agent 安装脚本
- **v0.1.0** – 任务 CRUD + Prometheus 指标 + Compression 配置 + CI Release
