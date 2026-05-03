#!/bin/bash
# task-driver-hook — Hermes Shell Hook 适配器
#
# 安装：cp 到 /usr/local/bin/task-driver-hook && chmod +x
# 配置：在 ~/.hermes/config.yaml 中添加 hooks 段（见 README）
#
# 从 stdin 读取 Hook JSON payload，转发心跳到 Go Task Driver。
# Observer-only，不返回 block 决策。
#
# on_session_end 额外动作：上报当前 session 的 token 用量到 Driver /metrics/tokens，
# 以及记录步骤耗时。

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

# Hermes state.db 路径
HERMES_STATE="${HERMES_STATE:-$HOME/.hermes/state.db}"

# 上报 session token 到 Driver /metrics/tokens
report_session_tokens() {
    local task_id="$1"
    local step_idx="$2"
    local agent_id="$3"

    # 从 Hermes state.db 读取当前 session 的 token 统计
    local TOKEN_JSON
    TOKEN_JSON=$(sqlite3 "$HERMES_STATE" "
        SELECT json_object(
            'input_tokens', COALESCE(input_tokens,0),
            'output_tokens', COALESCE(output_tokens,0),
            'cache_read_tokens', COALESCE(cache_read_tokens,0),
            'reasoning_tokens', COALESCE(reasoning_tokens,0)
        ) FROM sessions WHERE id='$SESSION_ID'
    " 2>/dev/null || echo '{}')

    if [ "$TOKEN_JSON" != "{}" ]; then
        local BODY
        BODY=$(echo "$TOKEN_JSON" | python3 -c "
import sys, json
t = json.load(sys.stdin)
t['task_id'] = '$task_id'
t['step_index'] = $step_idx
t['agent_id'] = '$agent_id'
# 只上报有数据的字段
for k in ['input_tokens','output_tokens','cache_read_tokens','reasoning_tokens']:
    if t.get(k,0) == 0:
        t.pop(k, None)
print(json.dumps(t))
" 2>/dev/null)

        if [ -n "$BODY" ] && [ "$BODY" != "{}" ]; then
            curl -s -X POST "${TASK_DRIVER_URL}/metrics/tokens" \
                -H 'Content-Type: application/json' \
                -d "$BODY" > /dev/null 2>&1 || true
        fi
    fi
}

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
            print(json.dumps(t))
            sys.exit(0)
    sys.exit(1)
except:
    sys.exit(1)
" 2>/dev/null || echo "")

        if [ -n "$MATCHING" ]; then
            TASK_ID=$(echo "$MATCHING" | python3 -c "import sys,json; print(json.load(sys.stdin)['task_id'])" 2>/dev/null || echo "")
            STEP_IDX=$(echo "$MATCHING" | python3 -c "import sys,json; print(json.load(sys.stdin).get('current_step',0))" 2>/dev/null || echo "0")

            # 更新心跳
            curl -s -X POST "${TASK_DRIVER_URL}/task/${TASK_ID}/heartbeat" > /dev/null 2>&1 || true

            # 上报 token 统计
            report_session_tokens "$TASK_ID" "$STEP_IDX" "hermes"
        fi
        ;;
esac

echo '{}'
