# task-driver 迭代路线图

> 基于 [Task-driver 迭代 Roadmap 参考文档] 和当前 v0.2.0 实现状态制定。
> 演进逻辑：**生存 → 确定性 → 效率 → 进化**

---

## 版本概览

| 版本 | 目标 | 状态 |
|------|------|------|
| v0.0.1 | MVP：状态持久化原型 | ✅ 已发布 |
| v0.2.0 | 生产化：daemon、多 agent、session、checkpoint、metrics | ✅ 已发布 |
| **v0.3.0** | **安全边界 + 人机协同 + 暂停恢复** | ⬅ 当前目标 |
| v0.4.0 | 确定性工具调用 + 内存架构 | 计划中 |
| v0.5.0 | 推理成本治理 + MCP 协议深度 | 计划中 |
| v1.0.0 | 多 Agent 协作 + 闭环反馈 | 远期目标 |

---

## 明确非目标（Non-Goals）

以下功能在各版本的规划中被**主动排除**，不予实现：

| 非目标 | 排除原因 | 替代方案 |
|--------|---------|---------|
| 分布式/多节点部署 | task-driver 定位为单机 Agent 驱动，SQLite 是刻意的架构选择 | 多机部署用多个独立实例 |
| Agent 代码生成 | task-driver 管理任务状态，不介入 Agent 如何写代码 | — |
| 模型训练/Fine-tuning | 推理成本治理只做路由和统计，不做训练 | — |
| GUI / Web Dashboard | 所有操作通过 API + CLI 完成 | 远期可考虑 read-only dashboard |
| 任务市场/模板库 | 跨用户的任务模板共享不是当前产品方向 | — |
| 实时协作（多人同时操作同一任务）| 单 agent 单 task 模型 | 通过审批流间接协作 |

---

## 各阶段详细规划

### 第一阶段：基础设施与生存（Foundation & Survival）→ v0.2.0 ✅

底层能力，Agent 可靠运行的前提。**已完成。**

| # | 功能 | 实现 | 状态 |
|---|------|------|------|
| P1 | 状态机与持久化 | task_state / task_steps / task_checkpoints 三表，SQLite WAL，热重启恢复 | ✅ |
| P2 | 安全沙盒 | — | ⬜ 延后至 v0.3.0 |
| P3 | 评价体系 | — | ⬜ 延后至 v0.3.0 |

**v0.2.0 已交付能力：**
- 任务 CRUD + 步骤粒度状态追踪
- 多 agent（hermes/claude/codex）配置 + resume_cmd 模板注入
- 4 种 checkpoint 类型（normal / compression_boundary / phase_boundary / task_modified）
- Session 管理 + mapped_session_id 用于压缩容灾
- Plan update + 步骤重排 + skip + heartbeat
- Daemon 模式（launchd/systemd）+ Prometheus metrics + 跨平台编译
- Agent 安装脚本（bash + powershell）

**已知缺口：**
- 代理端钩子未集成（Hermes MemoryProvider 插件尚未开发——端点已就绪，v0.3.0 补齐）
- `task-management` skill 恢复流程仅半自动（需手动 /resume，无法从 checkpoint 自动重建上下文）
- `current_step` 一致性问题已在 `81e2221` 修复

**v0.2.0 已知稳定性缺陷（纳入 v0.3.0 修复）：**
- 无回归测试套件 — current_step 一致性问题在 v0.2.0 中潜伏数周未被发现
- heartbeat 无人消费 — agent 崩溃后 task 永久卡在 `in_progress`，系统不自愈
- 无并发控制 — 多 agent 同时更新同一 task 可导致静默数据覆盖
- 步骤间无结构化 I/O 传递 — agent 无法程序化读取上一步输出

---

### 第二阶段：安全与协同（Safety & Collaboration）→ v0.3.0 ⬅️ 当前目标

**主题：让 Agent 安全地与人协同，不丢进度、不会失控。**

#### v0.3.0 基础设施补全

以下基础设施项穿插在各阶段中交付，优先级高于功能特性：

1. **并发控制**（伴随 alpha 交付）
   - task 表新增 `version` 列（乐观锁）
   - 所有写操作带 `expected_version` → 冲突返回 409
   - 无额外端点，改动在 DB 层

2. **幂等性**（伴随 alpha 交付）
   - step update / checkpoint 端点支持 `idempotency_key` header
   - 重复请求返回 200 + 原始结果，不重复写入

3. **数据库迁移框架**（alpha 前完成）
   - 引入 golang-migrate 或自研轻量方案
   - `db/migrations/` 目录，CI 自动运行

4. **死任务检测**（伴随 beta 交付）
   - 后台 goroutine 扫描 `heartbeat` 超时（可配置，默认 5min）
   - 超时 → 自动标记 task 为 `stale` + 写入 checkpoint（type=heartbeat_timeout）
   - `GET /task/unfinished` 返回中包含 stale 标记

5. **认证**（rc1 前完成）
   - 引入 API Token（Bearer 认证）
   - 本地 localhost 免认证，外部请求（webhook 回传）强制认证
   - `config.yaml` 中配置 `api_tokens`

#### P2: 执行审计与安全边界 — `v0.3.0-alpha`

**诚实声明**：task-driver 不执行命令，无法在内核层拦截 agent 行为。
v0.3.0 的目标是**可观测的安全**而非**可强制阻断的安全**。

**方案**：
1. **Command Audit Log**（v0.3.0-alpha）
   - 引入 `audit_log` 表：记录 agent 报告的每个 shell 命令、文件操作
   - 事后审计，非实时阻断
   - 高危模式（`rm -rf /`、`chmod 777`、`git push --force`）标记 WARNING 级别
2. **OS 级隔离**（v0.3.0 可选 — 需评估运维成本）
   - 方案 A：独立 launchd 用户（`_taskdriver`）运行 agent 子进程
   - 方案 B：Docker 容器封装（增加部署复杂度）
   - **不在 v0.3.0 强制交付**，但要求文档明确安全边界

**交付物：**
- `audit_log` 表 + `POST /task/{id}/audit` 写入端点
- WARNING 规则集（YAML 配置，可扩展）
- `docs/SECURITY.md`：明确当前安全边界与责任划分

#### P3: 回归测试与冒烟框架 — `v0.3.0-beta`

**诚实声明**：v0.3.0 交付的是冒烟测试框架，不是完整的 agent 行为评价体系。
后者需要确定性 agent 模拟器和跨版本基准线追踪，延后至 v0.4.0。

**方案**：
1. **冒烟测试**（v0.3.0-beta）
   - 3 个核心场景
     1. 正常流程：create → start → step×N → complete → finish
     2. 中断恢复：create → step×2 → 模拟崩溃 → resume → 继续 → finish
     3. Plan 变更：create → plan_update → 验证 steps 同步
   - 每个 PR 自动运行
2. **Mock Agent Harness**（v0.3.0-rc1 可选）
   - `test/harness/`：可脚本化的假 agent，按 YAML 剧本回放 API 调用
   - 用于 CI 中可重复的端到端验证
3. **Eval 基线**（v0.4.0）
   - 引入 `eval_runs` 表记录每次运行的指标
   - 跨版本对比成功率

**交付物：**
- `test/e2e/` 目录：3 个核心场景
- `make e2e` 命令
- Eval 结果纳入 CI

#### P5: 人机协同（HITL）— `v0.3.0`

**问题**：Agent 在高危操作、不确定决策时无法等待人工确认。

**方案**：
1. **Pause / Resume 信号系统**
   - `POST /task/{id}/pause`：暂停任务，记录暂停原因
   - `POST /task/{id}/resume`：恢复任务（已有，需增强）
   - 新增 `TaskPaused` 状态
2. **Notification Hook**
   - Pause 事件触发 webhook → Telegram/Feishu 通知
   - 用户回复后通过 webhook 回调 task-driver 恢复
3. **Approval Gateway**
   - `POST /task/{id}/approval-request`：发起审批
   - `POST /task/{id}/approval-respond`：审批回复
   - 审批超时自动拒绝（可配置）
4. **Timeout & Escalation**
   - 可配置超时：`approval_timeout`（默认 30min）
   - 超时策略（3 选 1）：自动拒绝 / 自动通过 / 升级通知（二次通知到备用渠道）
   - 超时记录到 `approval_log` 表
5. **Rejection Handling**
   - 拒绝原因必填 → 写入 step error
   - Agent 收到拒绝后可选：重试当前步骤 / 跳过 / 标记失败
   - `POST /task/{id}/step` 新增 `rejection_action` 字段
6. **Non-blocking Continuation**
   - `TaskPaused` 不等价于完全停止
   - 后续步骤如果不依赖当前步骤的输出 → 继续执行（需步骤 DAG，v0.4.0）

**交付物：**
- 3 个新端点 + `TaskPaused` 状态
- `approval_log` 表
- 超时/升级/回退逻辑
- Webhook 通知集成（至少 Telegram）
- `task-management` skill 更新：完整的 pause/resume/approval 流程

#### 代理端钩子补齐 — Hermes MemoryProvider 插件

**架构**：通过 Hermes 的 `MemoryProvider` 插件机制集成，零 Go 核心改动。
Hermes 提供了以下 Python 钩子：

| 钩子 | 触发时机 | task-driver 用途 |
|------|---------|-----------------|
| `on_pre_compress(messages)` | 上下文压缩前 | 写入 `compression_boundary` checkpoint |
| `on_session_switch(new_id, parent_id, reset, reason)` | session ID 变更（`reason="compression"\|"resume"\|"new_session"\|"branch"`） | 更新 mapped_session_id，检测恢复 |
| `on_session_end(messages)` | 会话结束 | 写入最终 checkpoint |
| `initialize(session_id, hermes_home, platform, ...)` | Agent 启动 | 扫描未完成任务，自动触发恢复 |

**方案**：
1. **TaskDriverMemoryProvider**（Python 类）
   - 继承 `agent.memory_provider.MemoryProvider`
   - 放在 `plugins/memory/taskdriver/__init__.py`
   - 通过 `plugin.yaml` 注册
2. **`on_pre_compress` 钩子** → `POST /task/{id}/checkpoint`
   - 类型 `compression_boundary`
   - 写入 monologue（当前进度自述）+ environment（git diff、文件状态）
   - task_id 从 agent 启动参数或环境变量传入
3. **`on_session_switch` 钩子** → 更新 `mapped_session_id`
   - `reason="compression"` 时更新 task 的 session 映射
   - `reason="resume"` 时检测 task-driver 是否有未完成任务 → 自动注入恢复上下文
4. **`initialize` 钩子** → 启动时检查
   - 扫描 `GET /task/unfinished` → 如果 agent_id 匹配且 agent_context 为 primary，自动触发恢复流程
   - 将最新 checkpoint 上下文注入到 system prompt

**交付物：**
- `plugins/memory/taskdriver/__init__.py` + `plugin.yaml`
- `config.yaml` 示例：`memory.provider: taskdriver`
- task-driver 侧零改动（端点已就绪）

---

### 第三阶段：可靠性与交互（Reliability & Interaction）→ v0.4.0

| # | 功能 | 方案 |
|---|------|------|
| P4 | 工具调用确定性 | 强类型工具协议：Go 端定义 tool schema，agent 调用时类型校验 + 参数范围检查。引入 tool_call_log 表审计所有工具调用 |
| P6 | 内存架构 | Working Memory 滑窗：基于 token 预算的动态上下文裁剪。摘要生成器：当 step 数 > N 时自动生成步骤摘要替代全量 history |

---

### 第四阶段：生态与经济（Ecosystem & Economics）→ v0.5.0

| # | 功能 | 方案 |
|---|------|------|
| P7 | MCP 协议深度 | task-driver 作为 MCP Server 暴露 task 管理能力；作为 MCP Host 接入外部工具 |
| P8 | 推理成本治理 | 多模型路由器：简单任务→DeepSeek，复杂决策→Claude。cost_tracking 表统计每 task token 消耗 |

---

### 第五阶段：群体与进化（Intelligence Evolution）→ v1.0.0

| # | 功能 | 方案 |
|---|------|------|
| P9 | 多 Agent 协作 | 任务拆分→分配→结果合并。Agent 间通过 task-driver 共享状态 |
| P10 | 闭环反馈 | 失败日志分析→自动调整 prompt/few-shot。引入 feedback_loop 表和 prompt 版本管理 |

---

## v0.3.0 交付计划

```
v0.3.0-alpha  并发控制 + 幂等性 + 数据库迁移 + 执行审计（audit_log）
    ↓
v0.3.0-beta   死任务检测 + 回归测试框架（冒烟测试 + CI）
    ↓
v0.3.0-rc1    HITL（Pause/Resume/Approval + Timeout + Telegram webhook）+ 认证
    ↓
v0.3.0        Hermes MemoryProvider 插件 + 文档收尾
```

**时间线**：每个 alpha/beta/rc 预计 1-2 周，v0.3.0 正式版目标 6 周内。

---

## v0.3.0 成功标准

发布 v0.3.0 的条件（全部满足）：

- [ ] 冒烟测试 100% 通过（CI 强制执行）
- [ ] 并发冲突场景：2 个 agent 同时更新同一 task → 至少 1 个收到 409（不可静默覆盖）
- [ ] 死任务检测：agent 进程 kill 后 5min 内 task 状态变为 stale
- [ ] HITL 审批全链路：pause → Telegram 通知 → 用户回复 → task 恢复（手动 E2E 验证）
- [ ] 备份恢复：v0.2.0 的 state.db 可被 v0.3.0 正确迁移打开
- [ ] `task-management` skill：`/checkpoint` 命令可在 Hermes 中正常使用

---

## 技术选型记录

> 参考文档第 62 行的问题：同步阻塞式 vs 异步事件流？

**选型：同步 HTTP + 异步事件流混合**

- **HTTP API（同步）**：Agent 通过 REST 端点提交状态变更（step update、checkpoint）。这是当前实现，简单可靠，适合单 agent 场景。
- **Webhook 通知（异步）**：Pause/Approval 事件通过 webhook 推送到外部（Telegram），不阻塞 agent 主循环。
- **未来演进**：当需要多 agent 协同时（v1.0.0），引入事件总线（NATS）替代直连 HTTP，agent 订阅任务事件而非轮询。

---

_最后更新：2026-05-04 | 作者：Hermes Agent + Bill Stone_
_参考：[Task-driver 迭代 Roadmap 参考文档]_
