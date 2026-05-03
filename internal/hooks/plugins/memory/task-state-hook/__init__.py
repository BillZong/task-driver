"""Task State Hook — 薄 MemoryProvider 插件

仅为 Hermes 的 on_pre_compress 和 on_session_switch 两个 hook 而生。
通过 HTTP 转发到 Go Task Driver，写入 checkpoint / 更新 session。

安装方式：
  1. 将本目录放到 ~/.hermes/plugins/memory/task-state-hook/
  2. 在 ~/.hermes/config.yaml 中设置：
     memory:
       provider: task-state-hook
     hooks:
       on_session_end:
         - command: "/usr/local/bin/task-driver-hook on_session_end"
           timeout: 10
       on_session_finalize:
         - command: "/usr/local/bin/task-driver-hook on_session_finalize"
           timeout: 10

所有 Task Driver 专属的 MemoryProvider 方法都是 no-op。
"""

from __future__ import annotations

import json
import logging
import os
import urllib.request
import urllib.error
from typing import Any, Dict, List, Optional

from agent.memory_provider import MemoryProvider

logger = logging.getLogger(__name__)

_TASK_DRIVER_URL = os.environ.get(
    "TASK_DRIVER_URL", "http://127.0.0.1:9876"
)


def _post(path: str, body: dict) -> Optional[dict]:
    """发送 HTTP POST 请求到 Task Driver。静默失败。"""
    url = f"{_TASK_DRIVER_URL}{path}"
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except (urllib.error.URLError, urllib.error.HTTPError, OSError, json.JSONDecodeError) as e:
        logger.debug("task-state-hook HTTP error: %s", e)
        return None


def _get(path: str) -> Optional[dict]:
    """发送 HTTP GET 请求到 Task Driver。静默失败。"""
    url = f"{_TASK_DRIVER_URL}{path}"
    try:
        with urllib.request.urlopen(url, timeout=5) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except (urllib.error.URLError, urllib.error.HTTPError, OSError, json.JSONDecodeError) as e:
        logger.debug("task-state-hook HTTP error: %s", e)
        return None


class TaskStateHookProvider(MemoryProvider):
    """MemoryProvider 适配器 → HTTP 转发到 Go Task Driver。

    只处理 on_pre_compress 和 on_session_switch。
    所有其他 MemoryProvider 方法为 no-op。
    """

    @property
    def name(self) -> str:
        return "task-state-hook"

    def is_available(self) -> bool:
        """必须返回 True，否则 Hermes 不会激活此 provider。"""
        return True

    # ------------------------------------------------------------------
    # 核心 hook
    # ------------------------------------------------------------------

    def on_pre_compress(self, messages: List[Dict[str, Any]]) -> str:
        """压缩前通知 — 写 compression_boundary checkpoint。

        Hermes 在压缩发生前调用此方法。返回值注入到 summary prompt。
        我们在这里写 checkpoint，并返回简要提示帮助 agent 感知压缩。
        """
        # 查询当前 session 是否有活跃 task
        session_id = getattr(self, "_session_id", "")
        if not session_id:
            return ""

        unfinished = _get("/task/unfinished")
        if not unfinished or not isinstance(unfinished, list):
            return ""

        matched = [t for t in unfinished if t.get("agent_session_id") == session_id]
        if not matched:
            return ""

        task_id = matched[0]["task_id"]

        # 写 compression_boundary checkpoint
        _post(f"/task/{task_id}/checkpoint", {
            "snapshot_type": "compression_boundary",
            "step_index": matched[0].get("current_step", 0),
            "monologue": (
                "上下文即将被压缩。以下信息需要特别保留：\n"
                "- task_id: {}\n"
                "- goal: {}\n"
                "- 当前步骤: 第 {} 步\n"
                "- agent: {}\n"
                "压缩后请从 task_steps 恢复精确上下文，不要依赖 summary。"
            ).format(
                task_id,
                matched[0].get("goal", ""),
                matched[0].get("current_step", 0),
                matched[0].get("agent_id", ""),
            ),
            "agent_id": matched[0].get("agent_id", "hermes"),
        })

        return (
            "[Task State Hook] 已为当前活跃 task ({}) 写 compression_boundary checkpoint。"
            "压缩后请通过 GET /task/{} 从 task_steps 恢复精确上下文。"
        ).format(task_id, task_id)

    def on_session_switch(
        self,
        new_session_id: str,
        *,
        parent_session_id: str = "",
        reset: bool = False,
        reason: str = "",
        **kwargs,
    ) -> None:
        """session_id 因压缩旋转 — 更新 task 的 session 关联。

        Hermes 压缩后创建新 session_id。我们需要告诉 Task Driver
        这个 task 现在关联到新的 session_id。
        """
        if not parent_session_id:
            return

        # 通过旧 session_id 找到 task
        unfinished = _get("/task/unfinished")
        if not unfinished or not isinstance(unfinished, list):
            return

        matched = [t for t in unfinished if t.get("agent_session_id") == parent_session_id]
        if not matched:
            return

        task_id = matched[0]["task_id"]

        # 更新 session_id
        _post(f"/task/{task_id}/session", {
            "agent_session_id": new_session_id,
        })

        # 同时写一个 checkpoint 记录 session 切换
        _post(f"/task/{task_id}/checkpoint", {
            "snapshot_type": "compression_boundary",
            "step_index": matched[0].get("current_step", 0),
            "monologue": (
                "Hermes 上下文压缩导致 session_id 变更。\n"
                "旧 session: {}\n"
                "新 session: {}\n"
                "task 已关联到新 session_id。继续执行时请从 task_steps 恢复精确上下文。"
            ).format(parent_session_id, new_session_id),
            "agent_id": matched[0].get("agent_id", "hermes"),
        })

    # ------------------------------------------------------------------
    # 初始化/生命周期 — only track session_id
    # ------------------------------------------------------------------

    def initialize(self, **kwargs) -> None:
        """记录初始化时传入的 session_id。"""
        self._session_id = kwargs.get("session_id", "")
        session_title = kwargs.get("session_title", "")
        logger.info(
            "Task State Hook initialized: session=%s title=%s",
            self._session_id, session_title,
        )

    def on_session_start(self, session_id: str, **kwargs) -> None:
        """记录 session 开始。"""
        self._session_id = session_id

    def shutdown(self) -> None:
        """清理。"""
        pass

    # ------------------------------------------------------------------
    # 以下全部 no-op
    # ------------------------------------------------------------------

    def system_prompt_block(self) -> str:
        return ""

    def prefetch(self, query: str) -> str:
        return ""

    def sync_turn(self, user_message: str, assistant_response: str) -> None:
        pass

    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        return []

    def handle_tool_call(self, name: str, args: Dict[str, Any], **kwargs) -> str:
        import json as _json
        return _json.dumps({"error": f"Unknown tool: {name}"})


def register(ctx) -> None:
    """注册到 Hermes 插件系统。"""
    ctx.register_memory_provider(TaskStateHookProvider())
