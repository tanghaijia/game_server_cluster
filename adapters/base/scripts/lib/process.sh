# shellcheck shell=bash
# ============================================================
# lib/process.sh —— 进程管理骨架
#
# 从各游戏 adapter 的 start.sh 提取的公共逻辑：
#   - find_bin_default       按可执行文件名在 SERVER_ROOT 下查找
#   - run_in_foreground      前台运行单进程（trap + wait）
#   - register_child / wait_all / stop_children
#                           多子进程管理（DST 双 shard 等场景由 hooks 组合）
#   - kill_remaining         分级兜底停止（TERM → KILL）
#
# 依赖：lib/log.sh（自动加载）
# ============================================================

if ! declare -F log >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/log.sh"
fi

# 已注册的子进程 pid 列表（由 register_child 维护）
declare -a _CHILD_PIDS=()

# register_child pid —— 注册一个子进程 pid（供 stop_children 统一停止）
register_child() {
  _CHILD_PIDS+=("$1")
}

# wait_all —— 等待全部子进程退出（永不因单个失败而中断）
wait_all() {
  local pid
  local rc=0
  for pid in "${_CHILD_PIDS[@]}"; do
    wait "${pid}" || rc=$?
  done
  return "${rc}"
}

# stop_children —— SIGTERM/SIGINT 处理：
#   定义了 hook_graceful_stop 则交给钩子（如 7dtd telnet shutdown）；
#   否则对每个子进程 kill -TERM。随后等待全部退出。
stop_children() {
  log "received termination signal, stopping children..."
  if declare -F hook_graceful_stop >/dev/null 2>&1; then
    hook_graceful_stop || true
  else
    local pid
    for pid in "${_CHILD_PIDS[@]}"; do
      if kill -0 "${pid}" 2>/dev/null; then
        kill -TERM "${pid}" 2>/dev/null || true
      fi
    done
  fi
  wait_all || true
  log "children stopped"
}

# find_bin_default exe_name —— 在 SERVER_ROOT 下按可执行文件名查找游戏主程序
# 用法：BIN="$(find_bin_default dontstarve_dedicated_server_nullrenderer)"
# 成功输出绝对路径；失败输出空并返回 10
find_bin_default() {
  local exe_name="$1"
  local found
  found="$(find "${SERVER_ROOT}" -name "${exe_name}" -type f | head -1 || true)"
  if [ -z "${found}" ]; then
    log_error "${exe_name} not found under ${SERVER_ROOT}"
    return 10
  fi
  echo "${found}"
}

# run_in_foreground cmd [args...] —— 前台运行单进程并接管信号处理
# 用法：run_in_foreground "${BIN}" -configfile=...
# 返回子进程退出码
run_in_foreground() {
  trap stop_children SIGTERM SIGINT
  "$@" &
  register_child "$!"
  log "main process pid=${_CHILD_PIDS[-1]}"
  wait_all
  local rc=$?
  log "main process exited rc=${rc}"
  return "${rc}"
}

# kill_remaining pattern —— 兜底：按进程模式分级终止残留进程（TERM → 3s → KILL）
# 用法：kill_remaining "7DaysToDieServer.x86_64"
kill_remaining() {
  local pattern="$1"
  local pids
  pids="$(pgrep -f "${pattern}" || true)"
  if [ -n "${pids}" ]; then
    log "force stopping remaining processes: ${pids}"
    # shellcheck disable=SC2086
    kill -TERM ${pids} 2>/dev/null || true
    sleep 3
    # shellcheck disable=SC2086
    pids="$(pgrep -f "${pattern}" || true)"
    if [ -n "${pids}" ]; then
      # shellcheck disable=SC2086
      kill -KILL ${pids} 2>/dev/null || true
    fi
  else
    log "no remaining process matches ${pattern}"
  fi
}
