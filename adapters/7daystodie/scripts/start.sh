#!/usr/bin/env bash
set -Eeuo pipefail

if [ "${SDTD_DEBUG:-0}" = "1" ]; then
  set -x
fi

DATA_ROOT="${SDTD_DATA_ROOT:-/data}"
SERVER_ROOT="${SDTD_SERVER_ROOT:-/server}"
CONFIG_FILE="${SDTD_CONFIG_FILE:-/data/serverconfig.xml}"
USER_DATA="${SDTD_USER_DATA:-/data/7DaysToDie}"
TELNET_PORT="${SDTD_TELNET_PORT:-8081}"
BIN="${SDTD_BIN:-}"

SERVER_PID=""

log() {
  echo "[$(date -Iseconds)] [start] $*" >&2
}

find_server_bin() {
  if [ -n "${BIN}" ]; then
    echo "${BIN}"
    return 0
  fi

  local found
  found="$(find "${SERVER_ROOT}" -name 7DaysToDieServer.x86_64 -type f | head -1 || true)"

  if [ -z "${found}" ]; then
    echo "[start] ERROR: 7DaysToDieServer.x86_64 not found under ${SERVER_ROOT}" >&2
    exit 10
  fi

  echo "${found}"
}

stop_server() {
  log "received termination signal, stopping 7 Days to Die server..."

  if [ -n "${SERVER_PID}" ] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    log "stopping server pid=${SERVER_PID}"
    kill -TERM "${SERVER_PID}" 2>/dev/null || true
  fi

  wait || true
  log "server stopped"
}

trap stop_server SIGTERM SIGINT

/scripts/prepare-runtime.sh

BIN="$(find_server_bin)"
BIN_DIR="$(dirname "${BIN}")"

log "SERVER_ROOT=${SERVER_ROOT}"
log "BIN=${BIN}"
log "BIN_DIR=${BIN_DIR}"
log "DATA_ROOT=${DATA_ROOT}"
log "CONFIG_FILE=${CONFIG_FILE}"
log "USER_DATA=${USER_DATA}"

cd "${BIN_DIR}"

log "starting 7 Days to Die dedicated server..."

# 必须加 -logfile，否则 Unity 默认往安装目录(/server 只读)写 output_log
# -nographics -batchmode 去掉 GUI；-dedicated 以专用服务器模式运行
"${BIN}" \
  -configfile="${CONFIG_FILE}" \
  -logfile /dev/stdout \
  -quit \
  -batchmode \
  -nographics \
  -dedicated &

SERVER_PID="$!"
log "server pid=${SERVER_PID}"

wait "${SERVER_PID}"
exit $?
