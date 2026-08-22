# shellcheck shell=bash
# ============================================================
# lib/output.sh —— 生命周期脚本的标准 JSON 输出（平台契约）
#
# 契约（见 game_server_cluster_development_doc.md §8.1）：
#   health.sh  → {"healthy": true|false, "reason": "..."}
#   players.sh → {"players": N, "max_players": M}
# ============================================================

# emit_health healthy [reason] —— 输出健康状态 JSON
emit_health() {
  local healthy="$1"
  local reason="${2:-ok}"
  if [ "${healthy}" = "true" ]; then
    echo "{\"healthy\": true, \"reason\": \"${reason}\"}"
  else
    echo "{\"healthy\": false, \"reason\": \"${reason}\"}"
  fi
}

# emit_players players [max_players] —— 输出玩家数 JSON
emit_players() {
  local players="${1:-0}"
  local max_players="${2:-}"
  if [ -n "${max_players}" ]; then
    echo "{\"players\": ${players}, \"max_players\": ${max_players}}"
  else
    echo "{\"players\": ${players}}"
  fi
}

# health_by_pid pid —— 默认健康检查：按 pid 存活判断
health_by_pid() {
  local pid="$1"
  if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
    emit_health true "running"
  else
    emit_health false "process not running"
  fi
}

# health_by_process_name name —— 默认健康检查：按进程名匹配
health_by_process_name() {
  local name="$1"
  if pgrep -f "${name}" >/dev/null 2>&1; then
    emit_health true "running"
  else
    emit_health false "process not found: ${name}"
  fi
}
