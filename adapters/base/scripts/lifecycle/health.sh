#!/usr/bin/env bash
# ============================================================
# lifecycle/health.sh —— 生命周期框架：输出健康状态 JSON
#
# 契约：{"healthy": true|false, "reason": "..."}
# 钩子：hook_health（默认：按 GAME_BIN_NAME 进程匹配）
# ============================================================
set -Eeuo pipefail

LOG_PHASE="health"
# shellcheck source=/dev/null
source /scripts/lib/env.sh
# shellcheck source=/dev/null
source /scripts/lib/log.sh
# shellcheck source=/dev/null
source /scripts/lib/output.sh

# shellcheck source=/dev/null
if [ -f "${HOOKS_FILE}" ]; then
  source "${HOOKS_FILE}"
fi

if declare -F hook_health >/dev/null 2>&1; then
  hook_health
else
  if [ -n "${GAME_BIN_NAME:-}" ]; then
    health_by_process_name "${GAME_BIN_NAME}"
  else
    emit_health false "GAME_BIN_NAME not set and no hook_health"
  fi
fi
