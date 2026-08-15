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
# 端口注入：平台分配宿主端口后通过 env 传入（SDTD_SERVER_PORT=<宿主端口>），
# 启动前改写 serverconfig.xml 的 ServerPort，使游戏通告端口 == 宿主映射端口（EOS/Steam 发现与直连一致）。
SERVER_PORT="${SDTD_SERVER_PORT:-26900}"
BIN="${SDTD_BIN:-}"

# Unity/7DTD 需要可写的 HOME：
# 容器以 node_agent 的非 root uid 运行，/etc/passwd 无对应条目时 Docker 不会设置 HOME，
# Unity PlayerPrefs 与 EOS DeviceId 凭据会落到只读的 /.config，导致 EOS 创建 DeviceId 失败 → NoLoginTicket。
# 指向 /data（可写、持久化），EOS DeviceId 也能跨重启保存。
export HOME="${SDTD_HOME:-${DATA_ROOT}}"

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

# 端口注入：改写 /data/serverconfig.xml 的 ServerPort（幂等；模板默认 26900）
if [ -f "${CONFIG_FILE}" ] && [ "${SERVER_PORT}" != "26900" ]; then
  log "SDTD_SERVER_PORT=${SERVER_PORT} -> rewrite ${CONFIG_FILE} ServerPort"
  sed -i "s|<property name=\"ServerPort\" value=\"[0-9]*\"/>|<property name=\"ServerPort\" value=\"${SERVER_PORT}\"/>|" "${CONFIG_FILE}"
fi

BIN="$(find_server_bin)"
BIN_DIR="$(dirname "${BIN}")"

log "SERVER_ROOT=${SERVER_ROOT}"
log "BIN=${BIN}"
log "BIN_DIR=${BIN_DIR}"
log "DATA_ROOT=${DATA_ROOT}"
log "CONFIG_FILE=${CONFIG_FILE}"
log "USER_DATA=${USER_DATA}"
log "SERVER_PORT=${SERVER_PORT}"

cd "${BIN_DIR}"

# 确保游戏目录进入库搜索路径（steamclient.so 等）
export LD_LIBRARY_PATH="${BIN_DIR}:${BIN_DIR}/linux64:${LD_LIBRARY_PATH:-}"

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