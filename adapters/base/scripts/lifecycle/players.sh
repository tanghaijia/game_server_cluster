#!/usr/bin/env bash
# ============================================================
# lifecycle/players.sh —— 生命周期框架：输出玩家数 JSON
#
# 契约：{"players": N, "max_players": M}
# 钩子：hook_players（默认：{"players": 0}）
# ============================================================
set -Eeuo pipefail

LOG_PHASE="players"
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

if declare -F hook_players >/dev/null 2>&1; then
  hook_players
else
  emit_players 0
fi
