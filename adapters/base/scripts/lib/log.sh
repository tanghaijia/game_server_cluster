# shellcheck shell=bash
# ============================================================
# lib/log.sh —— 统一日志输出
#
# 用法：
#   LOG_PHASE="start"            # 入口处设置阶段名（start/save/stop/health/players）
#   source .../lib/log.sh
#   log "message"                # -> [2026-08-20T10:00:00+08:00] [start] message
#   log_error "oops"             # -> [..] [start] ERROR: oops
# ============================================================

: "${LOG_PHASE:=lifecycle}"

log() {
  echo "[$(date -Iseconds)] [${LOG_PHASE}] $*" >&2
}

log_error() {
  echo "[$(date -Iseconds)] [${LOG_PHASE}] ERROR: $*" >&2
}

log_warn() {
  echo "[$(date -Iseconds)] [${LOG_PHASE}] WARN: $*" >&2
}
