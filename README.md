# Task Driver

Agent-agnostic 的任务状态持久化服务。任何 AI Agent（Hermes、Claude Code、Codex）可以通过 HTTP API 记录和管理长任务的状态、步骤、检查点。

## 架构

```
Adapter 层（agent 特定） → HTTP → Go Task Driver (:9876) → SQLite
```

## Agent 适配

| Agent | 适配方式 |
|---|---|
| Hermes | Shell Hook + MemoryProvider 插件 |
| Claude Code | Hook 脚本（待实现） |
| Codex | Hook 脚本（待实现） |

## 快速开始

```bash
task-driver --config ~/.config/task-driver/config.yaml
```

## 设计文档

详见 `/Users/bill/Library/Mobile Documents/com~apple~CloudDocs/AI/hermes/task-state-v3-arch.md`
