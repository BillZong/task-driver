#!/usr/bin/env pwsh
# install-agents.ps1 — 检测并安装 AI agents（Claude Code / Codex CLI / Hermes）
# Windows 平台的 agent 安装与 task-driver hook 注入。
#
# 用法:
#   powershell -ExecutionPolicy Bypass -File install-agents.ps1
#   pwsh install-agents.ps1
#   pwsh install-agents.ps1 -DryRun
#   pwsh install-agents.ps1 -Force
#   pwsh install-agents.ps1 -Only claude-code,codex
#
# 环境变量:
#   TASK_DRIVER_VERSION   — 指定 task-driver 版本 (默认: latest)
#   TASK_DRIVER_URL       — task-driver HTTP 地址 (默认: http://127.0.0.1:9876)

param(
    [switch]$DryRun,
    [switch]$Force,
    [string]$Only = ""
)

$ErrorActionPreference = "Stop"

# ── 配置 ──────────────────────────────────────────────────
$TASK_DRIVER_VERSION  = if ($env:TASK_DRIVER_VERSION)  { $env:TASK_DRIVER_VERSION }  else { "latest" }
$TASK_DRIVER_URL      = if ($env:TASK_DRIVER_URL)      { $env:TASK_DRIVER_URL }      else { "http://127.0.0.1:9876" }
$HERMES_CONFIG        = if ($env:HERMES_CONFIG)        { $env:HERMES_CONFIG }        else { "$env:USERPROFILE\.hermes\config.yaml" }
$TASK_DRIVER_BIN      = if ($env:TASK_DRIVER_BIN)      { $env:TASK_DRIVER_BIN }      else { "$env:ProgramFiles\task-driver\task-driver.exe" }
$TASK_DRIVER_HOOK     = "$TASK_DRIVER_BIN-hook.cmd"
$TASK_DRIVER_SKILL_DIR= if ($env:TASK_DRIVER_SKILL_DIR) { $env:TASK_DRIVER_SKILL_DIR } else { "$env:USERPROFILE\.hermes\skills\hermes-task-management" }

# ── Helpers ───────────────────────────────────────────────
function Write-Info  { Write-Host "[INFO]" -ForegroundColor Cyan -NoNewline; Write-Host " $args" }
function Write-Ok    { Write-Host "[OK]" -ForegroundColor Green -NoNewline; Write-Host " $args" }
function Write-Warn  { Write-Host "[WARN]" -ForegroundColor Yellow -NoNewline; Write-Host " $args" }
function Write-Err   { Write-Host "[ERR]" -ForegroundColor Red -NoNewline; Write-Host " $args" }

function Should-Install {
    param([string]$Agent)
    if (-not $Only) { return $true }
    return $Only.Split(',').Contains($Agent)
}

function Has-Command {
    param([string]$Cmd)
    return (Get-Command $Cmd -ErrorAction SilentlyContinue) -ne $null
}

function Download-File {
    param([string]$Url, [string]$OutFile)
    $tmp = "$env:TEMP\task-driver-install-$(Get-Random)"
    try {
        Write-Info "Downloading $Url ..."
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -Uri $Url -OutFile $tmp -UseBasicParsing
        Move-Item $tmp $OutFile -Force
        return $true
    } catch {
        Remove-Item $tmp -ErrorAction SilentlyContinue
        return $false
    }
}

function Get-LatestRelease {
    $url = "https://api.github.com/repos/BillZong/task-driver/releases/latest"
    try {
        $resp = Invoke-RestMethod -Uri $url -UseBasicParsing
        return $resp.tag_name
    } catch {
        return "v0.1.0"
    }
}

# ═══════════════════════════════════════════════════════════
# 1. Task Driver 安装
# ═══════════════════════════════════════════════════════════
function Install-TaskDriver {
    Write-Info "=== Task Driver ==="

    if (Has-Command "task-driver") {
        if (-not $Force) {
            Write-Ok "task-driver already installed"
            return
        }
    }

    if ($DryRun) {
        Write-Info "[dry-run] Would install task-driver $TASK_DRIVER_VERSION (windows/amd64)"
        return
    }

    $version = if ($TASK_DRIVER_VERSION -eq "latest") { Get-LatestRelease } else { $TASK_DRIVER_VERSION }
    $dlUrl = "https://github.com/BillZong/task-driver/releases/download/${version}/task-driver-windows-amd64.exe"
    $destDir = Split-Path $TASK_DRIVER_BIN -Parent
    New-Item -ItemType Directory -Force -Path $destDir | Out-Null

    if (Download-File $dlUrl $TASK_DRIVER_BIN) {
        Write-Ok "task-driver installed at $TASK_DRIVER_BIN"
    } else {
        Write-Warn "Download failed. Install manually from https://github.com/BillZong/task-driver/releases"
    }
}

# ═══════════════════════════════════════════════════════════
# 2. Claude Code 安装
# ═══════════════════════════════════════════════════════════
function Install-ClaudeCode {
    Write-Info "=== Claude Code ==="

    if (Has-Command "claude") {
        if (-not $Force) {
            Write-Ok "Claude Code already installed"
            return
        }
    }

    if ($DryRun) {
        Write-Info "[dry-run] Would install Claude Code via npm"
        return
    }

    if (-not (Has-Command "npm")) {
        Write-Warn "npm not found. Install Node.js first: https://nodejs.org"
        return
    }

    Write-Info "Installing Claude Code..."
    npm install -g @anthropic-ai/claude-code 2>&1 | Select-Object -Last 3
    Write-Ok "Claude Code installed"
}

# ═══════════════════════════════════════════════════════════
# 3. Codex CLI 安装
# ═══════════════════════════════════════════════════════════
function Install-Codex {
    Write-Info "=== Codex CLI ==="

    if (Has-Command "codex") {
        if (-not $Force) {
            Write-Ok "Codex CLI already installed"
            return
        }
    }

    if ($DryRun) {
        Write-Info "[dry-run] Would install Codex CLI"
        return
    }

    $dlUrl = "https://github.com/openai/codex-cli/releases/latest/download/codex-Windows-x86_64.exe"
    $dest = [System.IO.Path]::Combine($env:LOCALAPPDATA, "codex", "codex.exe")
    $destDir = Split-Path $dest -Parent
    New-Item -ItemType Directory -Force -Path $destDir | Out-Null

    if (Download-File $dlUrl $dest) {
        # Add to PATH if not already
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$destDir*") {
            [Environment]::SetEnvironmentVariable("Path", "$userPath;$destDir", "User")
            $env:Path = "$env:Path;$destDir"
        }
        Write-Ok "Codex CLI installed at $dest"
    } else {
        Write-Warn "Codex CLI download failed. Install manually: https://github.com/openai/codex-cli"
    }
}

# ═══════════════════════════════════════════════════════════
# 4. Hermes Hook 注入
# ═══════════════════════════════════════════════════════════
function Setup-Hermes {
    Write-Info "=== Hermes Agent Hooks ==="

    if (-not (Has-Command "hermes")) {
        Write-Warn "Hermes not found. Skipping hook injection."
        Write-Warn "Install Hermes first, then re-run this script."
        return
    }

    if ($DryRun) {
        Write-Info "[dry-run] Would inject Hermes hooks and skill"
        return
    }

    # Create Hermes config directory if needed
    $hermesDir = Split-Path $HERMES_CONFIG -Parent
    New-Item -ItemType Directory -Force -Path $hermesDir | Out-Null

    # Inject hook into Hermes config
    if (Test-Path $HERMES_CONFIG) {
        $content = Get-Content $HERMES_CONFIG -Raw
        if ($content -match "task-driver-hook" -and -not $Force) {
            Write-Ok "task-driver hook already in Hermes config"
        } else {
            $newContent = @"
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

"@
            Set-Content $HERMES_CONFIG -Value ($content + "`n" + $newContent)
            Write-Ok "Hermes hook injected into $HERMES_CONFIG"
        }
    } else {
        $newConfig = @"
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

"@
        Set-Content $HERMES_CONFIG -Value $newConfig
        Write-Ok "Hermes config created at $HERMES_CONFIG"
    }

    # Install Hermes skill
    $skillPath = "$TASK_DRIVER_SKILL_DIR\SKILL.md"
    $skillUrl = "https://raw.githubusercontent.com/BillZong/task-driver/main/skills/hermes-task-management/SKILL.md"
    New-Item -ItemType Directory -Force -Path $TASK_DRIVER_SKILL_DIR | Out-Null
    if (Download-File $skillUrl $skillPath) {
        Write-Ok "Hermes task-management skill installed at $TASK_DRIVER_SKILL_DIR"
    } else {
        Write-Warn "Failed to download Hermes skill from GitHub"
    }

    # Install memory plugin
    $pluginDir = "$env:USERPROFILE\.hermes\plugins\memory\task-state-hook"
    New-Item -ItemType Directory -Force -Path $pluginDir | Out-Null

    @"
\"\"\"task-state-hook Hermes MemoryProvider plugin.\"\"\"
from .plugin import TaskStateHookProvider
__all__ = [\"TaskStateHookProvider\"]
"@ | Set-Content "$pluginDir\__init__.py"

    @"
import json
import urllib.request
import urllib.error

TASK_DRIVER_URL = \"$TASK_DRIVER_URL\"


class TaskStateHookProvider:

    def get_compression_context(self, session_id, context):
        task_id = context.get(\"task_id\", \"\")
        if not task_id:
            return {}
        try:
            body = json.dumps({
                \"snapshot_type\": \"compression_boundary\",
                \"step_index\": context.get(\"current_step\", 0),
                \"monologue\": context.get(\"monologue\", f\"Compression event for session {session_id}\"),
                \"agent_id\": context.get(\"agent_id\", \"hermes\"),
            }).encode()
            req = urllib.request.Request(
                f\"{TASK_DRIVER_URL}/task/{task_id}/checkpoint\",
                data=body,
                headers={\"Content-Type\": \"application/json\"},
                method=\"POST\",
            )
            urllib.request.urlopen(req, timeout=5)
        except (urllib.error.URLError, OSError):
            pass
        return {}

    def on_session_switch(self, session_id, context):
        task_id = context.get(\"task_id\", \"\")
        if not task_id:
            return {}
        try:
            body = json.dumps({
                \"agent_session_id\": session_id,
            }).encode()
            req = urllib.request.Request(
                f\"{TASK_DRIVER_URL}/task/{task_id}/session\",
                data=body,
                headers={\"Content-Type\": \"application/json\"},
                method=\"POST\",
            )
            urllib.request.urlopen(req, timeout=5)
        except (urllib.error.URLError, OSError):
            pass
        return {}
"@ | Set-Content "$pluginDir\plugin.py"
    Write-Ok "task-state-hook memory plugin installed at $pluginDir"
}

# ═══════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════
Write-Host ""
Write-Host "╔══════════════════════════════════════════════════╗"
Write-Host "║     Task Driver — Agent Installer (Windows)      ║"
Write-Host "╚══════════════════════════════════════════════════╝"
Write-Host ""

if ($DryRun) {
    Write-Warn "DRY RUN MODE — no changes will be made"
    Write-Host ""
}

if (Should-Install "task-driver")   { Install-TaskDriver }
if (Should-Install "claude-code")   { Install-ClaudeCode }
if (Should-Install "codex")         { Install-Codex }
if (Should-Install "hermes")        { Setup-Hermes }

Write-Host ""
Write-Ok "Done!"
Write-Host ""

$showNextSteps = (-not $Only) -or ($Only.Split(',').Contains("task-driver")) -or ($Only.Split(',').Contains("hermes"))
if ($showNextSteps) {
    Write-Host "Next steps:"
    Write-Host "  1. Start task-driver daemon:"
    Write-Host "     task-driver daemon"
    Write-Host "  2. Check status:"
    Write-Host "     task-driver status"
    Write-Host "  3. Verify API:"
    Write-Host "     curl http://127.0.0.1:9876/health"
    Write-Host ""
}
