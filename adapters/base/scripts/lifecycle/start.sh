#!/usr/bin/env bash
# ============================================================
# lifecycle/start.sh —— 生命周期框架：启动游戏服务器
#
# 所有游戏共享同一份；游戏差异通过 /scripts/hooks.sh 提供：
#   hook_pre_start     预启动（默认：模板展开）
#   hook_find_bin      定位主程序（默认：GAME_BIN_NAME 查找）
#   hook_start_command 启动命令（默认：run_in_foreground "${BIN}"）
#   hook_graceful_stop 优雅停止（默认：kill -TERM，见 lib/process.sh）
#
# 容器契约入口：/scripts/start.sh（软链到本文件）
# ============================================================
set -Eeuo pipefail

LOG_PHASE="start"
# shellcheck source=/dev/null
source /scripts/lib/env.sh
# shellcheck source=/dev/null
source /scripts/lib/log.sh
# shellcheck source=/dev/null
source /scripts/lib/fs.sh
# shellcheck source=/dev/null
source /scripts/lib/template.sh
# shellcheck source=/dev/null
source /scripts/lib/process.sh
# shellcheck source=/dev/null
source /scripts/lib/port-inject.sh
# shellcheck source=/dev/null
source /scripts/lib/config-render.sh

# 游戏差异钩子（可选；不存在则全部走默认实现）
# shellcheck source=/dev/null
if [ -f "${HOOKS_FILE}" ]; then
  source "${HOOKS_FILE}"
fi

# 调试开关（适配器声明；GAME_DEBUG=1 或 {GAME}_DEBUG=1）
if [ "${GAME_DEBUG:-0}" = "1" ]; then
  set -x
fi

# ---- 1. 预启动（钩子优先，默认模板展开） ---------------------------
rc=0
if declare -F hook_pre_start >/dev/null 2>&1; then
  hook_pre_start || { rc=$?; log_error "hook_pre_start failed rc=${rc}"; exit "${rc}"; }
else
  expand_templates "${TEMPLATE_DIR}" "${DATA_ROOT}"
fi

# ---- 1.5 平台配置渲染（M4）：按 config-manifest.json 把平台下发的
#      /data/.platform/game-config.json 合并进游戏配置文件（如 serverconfig.xml）。
#      模板展开之后、游戏进程启动之前执行；无 manifest/无配置时静默跳过。
render_config_auto /scripts/config-manifest.json || log_warn "config render failed (non-fatal)"

# ---- 2. 定位游戏主程序 ----------------------------------------------
BIN=""
if declare -F hook_find_bin >/dev/null 2>&1; then
  BIN="$(hook_find_bin)" || { rc=$?; log_error "hook_find_bin failed rc=${rc}"; exit "${rc}"; }
elif [ -n "${GAME_BIN_NAME:-}" ]; then
  BIN="$(find_bin_default "${GAME_BIN_NAME}")" || exit $?
fi
if [ -z "${BIN}" ]; then
  log_error "game binary not resolved (provide GAME_BIN_NAME env or hook_find_bin)"
  exit 10
fi
export BIN
BIN_DIR="$(dirname "${BIN}")"
export BIN_DIR
# 游戏目录（含 linux64 子目录）加入库搜索路径（steamclient.so 等）
export LD_LIBRARY_PATH="${BIN_DIR}:${BIN_DIR}/linux64:${LD_LIBRARY_PATH:-}"

log "BIN=${BIN}"
log "BIN_DIR=${BIN_DIR}"
log "DATA_ROOT=${DATA_ROOT}"
log "SERVER_ROOT=${SERVER_ROOT}"

# ---- 3. 启动（钩子负责前台阻塞；默认单进程前台化） ------------------
# 统一注册信号处理：容器收到 SIGTERM（docker stop）时优雅停止子进程
trap stop_children SIGTERM SIGINT
cd "${BIN_DIR}"
if declare -F hook_start_command >/dev/null 2>&1; then
  hook_start_command
else
  run_in_foreground "${BIN}"
fi
