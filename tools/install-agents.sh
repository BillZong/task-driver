#!/usr/bin/env bash
# install-agents.sh — 检测并安装 AI agents（Claude Code / Codex CLI / Hermes）
# 并为每个 agent 注入 task-driver hook 配置。
#
# 用法:
#   curl -fsSL https://raw.githubusercontent.com/BillZong/task-driver/main/tools/install-agents.sh | bash
#   bash install-agents.sh
#   bash install-agents.sh --dry-run     # 仅检测，不安装
#   bash install-agents.sh --force       # 覆盖已有安装
#   bash install-agents.sh --only claude-code,codex  # 仅安装指定 agent
#
# 环境变量:
#   TASK_DRIVER_VERSION  — 指定 task-driver 版本 (默认: latest)
#   TASK_DRIVER_URL      — task-driver HTTP 地址 (默认: http://127.0.0.1:9876)

set -euo pipefail

# ── 配置 ──────────────────────────────────────────────────
DRY_RUN=false
FORCE=false
ONLY_AGENTS=""

TASK_DRIVER_VERSION="${TASK_DRIVER_VERSION:-latest}"
TASK_DRIVER_URL="${TASK_DRIVER_URL:-http://127.0.0.1:9876}"
HERMES_CONFIG="${HERMES_CONFIG:-$HOME/.hermes/config.yaml}"
TASK_DRIVER_BIN="${TASK_DRIVER_BIN:-/usr/local/bin/task-driver}"
TASK_DRIVER_HOOK="${TASK_DRIVER_BIN}-hook"
TASK_DRIVER_SKILL_DIR="${TASK_DRIVER_SKILL_DIR:-$HOME/.hermes/skills/hermes-task-management}"

# ── Terminal colors ───────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()  { echo -e "${CYAN}[INFO]${NC} $*"; }
ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()   { echo -e "${RED}[ERR]${NC} $*"; }

# ── Help ──────────────────────────────────────────────────
usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Options:
  --dry-run      Only detect, don't install anything
  --force        Force reinstall even if already installed
  --only LIST    Comma-separated list: claude-code,codex,hermes
  --help         Show this help

Environment:
  TASK_DRIVER_VERSION  task-driver version (default: latest)
  TASK_DRIVER_URL      task-driver HTTP endpoint
EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) DRY_RUN=true; shift ;;
        --force)   FORCE=true; shift ;;
        --only)    ONLY_AGENTS="$2"; shift 2 ;;
        --help|-h) usage ;;
        *) err "Unknown option: $1"; usage ;;
    esac
done

# ── OS detection ──────────────────────────────────────────
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) err "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# ── Helper: check if agent is requested ──────────────────
should_install() {
    local agent="$1"
    [ -z "$ONLY_AGENTS" ] && return 0
    [[ ",$ONLY_AGENTS," == *",$agent,"* ]] && return 0
    return 1
}

# ── Helper: check command exists ──────────────────────────
has_cmd() { command -v "$1" &>/dev/null; }

# ── Helper: download file ─────────────────────────────────
download() {
    local url="$1" out="$2"
    if has_cmd curl; then
        curl -fsSL -o "$out" "$url"
    elif has_cmd wget; then
        wget -q -O "$out" "$url"
    else
        err "Neither curl nor wget found. Install one and retry."
        exit 1
    fi
}

# ── Helper: inject Hermes hook config ────────────────────
inject_hermes_hook() {
    local config_file="$1"
    if [ ! -f "$config_file" ]; then
        warn "Hermes config not found at $config_file; creating..."
        echo "hooks:" > "$config_file"
        echo "memory:" >> "$config_file"
    fi

    # Check if hook already injected
    if grep -q "task-driver-hook" "$config_file" 2>/dev/null; then
        if [ "$FORCE" = false ]; then
            ok "task-driver hook already in Hermes config"
            return 0
        fi
        warn "Replacing existing task-driver hook entries..."
    fi

    # Write hook config using python or sed
    if has_cmd python3; then
        python3 -c "
import yaml, sys
with open('$config_file') as f:
    cfg = yaml.safe_load(f) or {}
hooks = cfg.setdefault('hooks', {})
events = ['on_session_end', 'on_session_finalize', 'on_session_reset']
hook_cmd = '$TASK_DRIVER_HOOK {event}'
for ev in events:
    hooks[ev] = [{'command': hook_cmd.format(event=ev), 'timeout': 10}]
cfg['memory'] = cfg.get('memory', {}) or {}
cfg['memory']['provider'] = 'task-state-hook'
with open('$config_file', 'w') as f:
    yaml.dump(cfg, f, default_flow_style=False)
" 2>/dev/null || {
            # Fallback: append raw YAML
            cat >> "$config_file" << 'HOOK'

hooks:
  on_session_end:
    - command: "$TASK_DRIVER_HOOK on_session_end"
      timeout: 10
  on_session_finalize:
    - command: "$TASK_DRIVER_HOOK on_session_finalize"
      timeout: 10
  on_session_reset:
    - command: "$TASK_DRIVER_HOOK on_session_reset"
      timeout: 10
memory:
  provider: task-state-hook
HOOK
        }
    fi
    ok "Hermes hook injected into $config_file"
}

# ── Helper: install Hermes skill ──────────────────────────
install_hermes_skill() {
    local skill_dir="$1"
    local src_url="https://raw.githubusercontent.com/BillZong/task-driver/main/skills/hermes-task-management/SKILL.md"

    if [ -d "$skill_dir" ] && [ "$FORCE" = false ]; then
        ok "Hermes task-management skill already installed"
        return 0
    fi

    mkdir -p "$skill_dir"
    download "$src_url" "$skill_dir/SKILL.md"
    ok "Hermes task-management skill installed at $skill_dir"
}

# ── Helper: install Python memory plugin ──────────────────
install_memory_plugin() {
    local plugin_dir="$HOME/.hermes/plugins/memory/task-state-hook"
    if [ -d "$plugin_dir" ] && [ "$FORCE" = false ]; then
        ok "task-state-hook memory plugin already installed"
        return 0
    fi

    mkdir -p "$plugin_dir"
    # Create minimal __init__.py and plugin.py
    # (In production, these would be downloaded from the repo)
    cat > "$plugin_dir/__init__.py" << 'PYEOF'
"""task-state-hook Hermes MemoryProvider plugin."""
from .plugin import TaskStateHookProvider

__all__ = ["TaskStateHookProvider"]
PYEOF

    cat > "$plugin_dir/plugin.py" << 'PYEOF'
"""Hermes MemoryProvider for task-driver integration.

Captures on_pre_compress and on_session_switch events
to write compression boundary checkpoints.
"""
import json
import urllib.request
import urllib.error

TASK_DRIVER_URL = "http://127.0.0.1:9876"


class TaskStateHookProvider:
    """Minimal MemoryProvider that forwards lifecycle events to task-driver."""

    def get_compression_context(self, session_id: str, context: dict) -> dict:
        """Called before compression — write a checkpoint."""
        task_id = context.get("task_id", "")
        if not task_id:
            return {}
        try:
            body = json.dumps({
                "snapshot_type": "compression_boundary",
                "step_index": context.get("current_step", 0),
                "monologue": context.get("monologue", f"Compression event for session {session_id}"),
                "agent_id": context.get("agent_id", "hermes"),
            }).encode()
            req = urllib.request.Request(
                f"{TASK_DRIVER_URL}/task/{task_id}/checkpoint",
                data=body,
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            urllib.request.urlopen(req, timeout=5)
        except (urllib.error.URLError, OSError):
            pass  # Non-blocking; don't crash Hermes
        return {}

    def on_session_switch(self, session_id: str, context: dict) -> dict:
        """Called when session switches — update session mapping."""
        task_id = context.get("task_id", "")
        if not task_id:
            return {}
        try:
            body = json.dumps({
                "agent_session_id": session_id,
            }).encode()
            req = urllib.request.Request(
                f"{TASK_DRIVER_URL}/task/{task_id}/session",
                data=body,
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            urllib.request.urlopen(req, timeout=5)
        except (urllib.error.URLError, OSError):
            pass
        return {}
PYEOF

    ok "task-state-hook memory plugin installed at $plugin_dir"
}

# ═══════════════════════════════════════════════════════════
# 1. Task Driver 安装
# ═══════════════════════════════════════════════════════════
install_task_driver() {
    info "=== Task Driver ==="

    if has_cmd task-driver && [ "$FORCE" = false ]; then
        ok "task-driver already installed ($(command -v task-driver))"
        return 0
    fi

    if $DRY_RUN; then
        info "[dry-run] Would install task-driver $TASK_DRIVER_VERSION ($OS/$ARCH)"
        return 0
    fi

    # Determine version
    local DL_VERSION
    if [ "$TASK_DRIVER_VERSION" = "latest" ]; then
        # Get latest release tag from GitHub API
        DL_VERSION=$(download "https://api.github.com/repos/BillZong/task-driver/releases/latest" - 2>/dev/null | \
            python3 -c "import sys,json; print(json.load(sys.stdin)['tag_name'])" 2>/dev/null || echo "v0.1.0")
    else
        DL_VERSION="$TASK_DRIVER_VERSION"
    fi

    # Map OS to download filename
    local DL_OS="$OS"
    [ "$DL_OS" = "darwin" ] && DL_OS="darwin"
    [ "$DL_OS" = "linux" ] && DL_OS="linux"

    local BINARY_NAME="task-driver-${DL_OS}-${ARCH}"
    [ "$OS" = "windows" ] && BINARY_NAME="${BINARY_NAME}.exe"

    local DL_URL="https://github.com/BillZong/task-driver/releases/download/${DL_VERSION}/${BINARY_NAME}"
    local TMPFILE=$(mktemp)

    info "Downloading task-driver $DL_VERSION from $DL_URL ..."
    if download "$DL_URL" "$TMPFILE"; then
        chmod +x "$TMPFILE"
        # Need sudo for /usr/local/bin
        if [ -w "$(dirname "$TASK_DRIVER_BIN")" ]; then
            mv "$TMPFILE" "$TASK_DRIVER_BIN"
        else
            sudo mv "$TMPFILE" "$TASK_DRIVER_BIN"
        fi
        ok "task-driver installed at $TASK_DRIVER_BIN"
    else
        rm -f "$TMPFILE"
        warn "Download failed; building from source..."
        if has_cmd go; then
            TMPDIR=$(mktemp -d)
            git clone --depth=1 https://github.com/BillZong/task-driver.git "$TMPDIR"
            cd "$TMPDIR"
            go build -o "$TASK_DRIVER_BIN" .
            rm -rf "$TMPDIR"
            ok "task-driver built from source at $TASK_DRIVER_BIN"
        else
            err "Cannot install task-driver: download failed and Go not found"
            return 1
        fi
    fi
}

# ═══════════════════════════════════════════════════════════
# 2. Claude Code 安装
# ═══════════════════════════════════════════════════════════
install_claude_code() {
    info "=== Claude Code ==="

    if has_cmd claude && [ "$FORCE" = false ]; then
        ok "Claude Code already installed ($(claude --version 2>/dev/null || command -v claude))"
        return 0
    fi

    if $DRY_RUN; then
        info "[dry-run] Would install Claude Code via npm"
        return 0
    fi

    if ! has_cmd npm; then
        warn "npm not found. Install Node.js first."
        return 1
    fi

    info "Installing Claude Code..."
    npm install -g @anthropic-ai/claude-code 2>&1 | tail -3
    ok "Claude Code installed"
}

# ═══════════════════════════════════════════════════════════
# 3. Codex CLI 安装
# ═══════════════════════════════════════════════════════════
install_codex() {
    info "=== Codex CLI ==="

    if has_cmd codex && [ "$FORCE" = false ]; then
        ok "Codex CLI already installed ($(command -v codex))"
        return 0
    fi

    if $DRY_RUN; then
        info "[dry-run] Would install Codex CLI ($OS/$ARCH)"
        return 0
    fi

    local DL_OS="$OS"
    local DL_ARCH="$ARCH"
    local EXT=""
    [ "$DL_OS" = "darwin" ] && DL_OS="Darwin"
    [ "$DL_OS" = "linux" ] && DL_OS="Linux"
    [ "$DL_ARCH" = "amd64" ] && DL_ARCH="x86_64"
    [ "$OS" = "windows" ] && EXT=".exe"

    # Codex CLI releases page
    local CODEX_VERSION="latest"
    local DL_URL="https://github.com/openai/codex-cli/releases/${CODEX_VERSION}/download/codex-${DL_OS}-${DL_ARCH}${EXT}"
    local TMPFILE=$(mktemp)

    info "Downloading Codex CLI from $DL_URL ..."
    if download "$DL_URL" "$TMPFILE"; then
        chmod +x "$TMPFILE"
        local DEST="${CODEX_BIN:-/usr/local/bin/codex}"
        if [ -w "$(dirname "$DEST")" ]; then
            mv "$TMPFILE" "$DEST"
        else
            sudo mv "$TMPFILE" "$DEST"
        fi
        ok "Codex CLI installed at $DEST"
    else
        rm -f "$TMPFILE"
        warn "Codex CLI download failed. Install manually: https://github.com/openai/codex-cli"
        return 1
    fi
}

# ═══════════════════════════════════════════════════════════
# 4. Hermes Hook 注入
# ═══════════════════════════════════════════════════════════
setup_hermes() {
    info "=== Hermes Agent Hooks ==="

    if ! has_cmd hermes; then
        warn "Hermes not found. Skipping hook injection."
        warn "Install Hermes first, then re-run this script."
        return 0
    fi

    if $DRY_RUN; then
        info "[dry-run] Would inject Hermes hooks and skill"
        return 0
    fi

    # Install hook script
    info "Installing task-driver hook script..."
    local HOOK_SRC="https://raw.githubusercontent.com/BillZong/task-driver/main/internal/hooks/task-driver-hook.sh"
    local TMPFILE=$(mktemp)
    if download "$HOOK_SRC" "$TMPFILE"; then
        chmod +x "$TMPFILE"
        if [ -w "$(dirname "$TASK_DRIVER_HOOK")" ]; then
            mv "$TMPFILE" "$TASK_DRIVER_HOOK"
        else
            sudo mv "$TMPFILE" "$TASK_DRIVER_HOOK"
        fi
        ok "Hook script installed at $TASK_DRIVER_HOOK"
    else
        rm -f "$TMPFILE"
        warn "Failed to download hook script from GitHub"
    fi

    # Inject Hermes config
    inject_hermes_hook "$HERMES_CONFIG"

    # Install Hermes skill
    install_hermes_skill "$TASK_DRIVER_SKILL_DIR"

    # Install memory plugin
    install_memory_plugin
}

# ═══════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════
main() {
    echo ""
    echo "╔══════════════════════════════════════════════════╗"
    echo "║     Task Driver — Agent Installer                ║"
    echo "╚══════════════════════════════════════════════════╝"
    echo ""
    $DRY_RUN && warn "DRY RUN MODE — no changes will be made" && echo ""

    should_install "task-driver" && install_task_driver
    should_install "claude-code" && install_claude_code
    should_install "codex" && install_codex
    should_install "hermes" && setup_hermes

    echo ""
    ok "Done!"
    echo ""

    if [ -z "$ONLY_AGENTS" ] || [[ ",$ONLY_AGENTS," == *",task-driver,"* ]] || [[ ",$ONLY_AGENTS," == *",hermes,"* ]]; then
        echo "Next steps:"
        echo "  1. Start task-driver daemon:"
        echo "     $ task-driver daemon"
        echo "  2. Check status:"
        echo "     $ task-driver status"
        echo "  3. Verify API:"
        echo "     $ curl http://127.0.0.1:9876/health"
        echo ""
    fi
}

main "$@"
