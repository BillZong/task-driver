#!/bin/bash
# task-driver-hook — Hermes Shell Hook 适配器
#
# 安装：cp 到 /usr/local/bin/task-driver-hook && chmod +x
# 配置：在 ~/.hermes/config.yaml 中添加 hooks 段（见 README）
#
# 从 stdin 读取 Hook JSON payload，转发心跳到 Go Task Driver。
# Observer-only，不返回 block 决策。

set -euo pipefail

TASK_DRIVER_URL="${TASK_DRIVER_URL:-http://127.0.0.1:9876}"
EVENT_NAME="${1:-unknown}"

# 读取 stdin JSON
PAYLOAD=$(cat)

# 提取 session_id
SESSION_ID=$(echo "$PAYLOAD" | python3 -c "
import sys, json
d = json.load(sys.stdin)
sid = d.get('session_id', '') or d.get('extra', {}).get('session_id', '')
print(sid)
" 2>/dev/null || echo "")

[ -z "$SESSION_ID" ] && echo '{}' && exit 0

case "$EVENT_NAME" in
    on_session_end|on_session_finalize|on_session_reset)
        # 查 /task/unfinished，找匹配 session_id 的活跃 task
        TASKS=$(curl -s "${TASK_DRIVER_URL}/task/unfinished" 2>/dev/null || echo "[]")
        MATCHING=$(echo "$TASKS" | python3 -c "
import sys, json
try:
    tasks = json.load(sys.stdin)
    for t in tasks:
        if t.get('agent_session_id') == '$SESSION_ID':
            print(t['task_id'])
            sys.exit(0)
    sys.exit(1)
except:
    sys.exit(1)
" 2>/dev/null || echo "")

        if [ -n "$MATCHING" ]; then
            curl -s -X POST "${TASK_DRIVER_URL}/task/${MATCHING}/heartbeat" > /dev/null 2>&1 || true
        fi
        ;;
esac

echo '{}'
