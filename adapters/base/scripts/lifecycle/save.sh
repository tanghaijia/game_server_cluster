#!/usr/bin/env bash
# ============================================================
# lifecycle/save.sh —— 生命周期框架：触发游戏保存
#
# 钩子：hook_save（默认：空操作返回 0）
# 契约：成功返回 0，失败返回非 0（平台据此判定保存结果）
# ============================================================
set -Eeuo pipefail

LOG_PHASE="save"
# shellcheck source=/dev/null
source /scripts/lib/env.sh
# shellcheck source=/dev/null
source /scripts/lib/log.sh

# shellcheck source=/dev/null
if [ -f "${HOOKS_FILE}" ]; then
  source "${HOOKS_FILE}"
fi

if declare -F hook_save >/dev/null 2>&1; then
  hook_save
else
  log "no hook_save defined, no-op"
fi
exit 0
