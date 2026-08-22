#!/usr/bin/env bash
# ============================================================
# lifecycle/stop.sh —— 生命周期框架：优雅停服
#
# 流程：save → 钩子优雅停止 → 兜底 TERM → KILL
# 钩子：hook_save / hook_graceful_stop / hook_process_pattern（进程匹配模式）
# ============================================================
set -Eeuo pipefail

LOG_PHASE="stop"
# shellcheck source=/dev/null
source /scripts/lib/env.sh
# shellcheck source=/dev/null
source /scripts/lib/log.sh
# shellcheck source=/dev/null
source /scripts/lib/process.sh

# shellcheck source=/dev/null
if [ -f "${HOOKS_FILE}" ]; then
  source "${HOOKS_FILE}"
fi

# ---- 1. 保存 -------------------------------------------------------
/scripts/save.sh || log_warn "save failed"

# ---- 2. 钩子优雅停止（telnet shutdown / RCON / 广播等） --------------
if declare -F hook_graceful_stop >/dev/null 2>&1; then
  hook_graceful_stop || true
  sleep 3
fi

# ---- 3. 兜底：残留进程分级终止 --------------------------------------
pattern=""
if declare -F hook_process_pattern >/dev/null 2>&1; then
  pattern="$(hook_process_pattern)"
else
  pattern="${GAME_BIN_NAME:-}"
fi
if [ -n "${pattern}" ]; then
  if pgrep -f "${pattern}" >/dev/null 2>&1; then
    kill_remaining "${pattern}"
  else
    log "no process matches ${pattern}, nothing to force-stop"
  fi
fi

exit 0
