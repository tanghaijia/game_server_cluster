#!/usr/bin/env bash
set -Eeuo pipefail

if [ "${DST_DEBUG:-0}" = "1" ]; then
  set -x
fi

DATA_ROOT="${DATA_ROOT:-/data}"
CONF_DIR="${DST_CONF_DIR:-DoNotStarveTogether}"
CLUSTER_NAME="${DST_CLUSTER:-Cluster}"
SERVER_ROOT="${SERVER_ROOT:-/server}"
BIN="${DST_BIN:-}"

MASTER_PID=""
CAVES_PID=""
LAST_PID=""

log() {
  echo "[$(date -Iseconds)] [start] $*" >&2
}

find_dst_bin() {
  if [ -n "${BIN}" ]; then
    echo "${BIN}"
    return 0
  fi

  local found
  found="$(find "${SERVER_ROOT}" -name dontstarve_dedicated_server_nullrenderer -type f | head -1 || true)"

  if [ -z "${found}" ]; then
    echo "[start] ERROR: dontstarve_dedicated_server_nullrenderer not found under ${SERVER_ROOT}" >&2
    exit 10
  fi

  echo "${found}"
}

stop_children() {
  log "received termination signal, stopping shards..."

  if [ -n "${MASTER_PID}" ] && kill -0 "${MASTER_PID}" 2>/dev/null; then
    log "stopping Master pid=${MASTER_PID}"
    kill -TERM "${MASTER_PID}" 2>/dev/null || true
  fi

  if [ -n "${CAVES_PID}" ] && kill -0 "${CAVES_PID}" 2>/dev/null; then
    log "stopping Caves pid=${CAVES_PID}"
    kill -TERM "${CAVES_PID}" 2>/dev/null || true
  fi

  wait || true
  log "all shards stopped"
}

trap stop_children SIGTERM SIGINT

/scripts/prepare-runtime.sh

BIN="$(find_dst_bin)"
BIN_DIR="$(dirname "${BIN}")"

export LD_LIBRARY_PATH="${BIN_DIR}/lib32:${LD_LIBRARY_PATH:-}"

log "SERVER_ROOT=${SERVER_ROOT}"
log "BIN=${BIN}"
log "BIN_DIR=${BIN_DIR}"
log "DATA_ROOT=${DATA_ROOT}"
log "CONF_DIR=${CONF_DIR}"
log "CLUSTER_NAME=${CLUSTER_NAME}"
log "LD_LIBRARY_PATH=${LD_LIBRARY_PATH}"

cd "${BIN_DIR}"

start_shard() {
  local shard="$1"

  log "starting shard=${shard}"

  "${BIN}" \
    -persistent_storage_root "${DATA_ROOT}" \
    -conf_dir "${CONF_DIR}" \
    -cluster "${CLUSTER_NAME}" \
    -shard "${shard}" &

  LAST_PID="$!"
  log "${shard} pid=${LAST_PID}"
}

if [ "${DST_SHARD:-both}" = "Master" ]; then
  start_shard Master
  MASTER_PID="${LAST_PID}"
  wait "${MASTER_PID}"
  exit $?
fi

if [ "${DST_SHARD:-both}" = "Caves" ]; then
  start_shard Caves
  CAVES_PID="${LAST_PID}"
  wait "${CAVES_PID}"
  exit $?
fi

start_shard Master
MASTER_PID="${LAST_PID}"

sleep 8

start_shard Caves
CAVES_PID="${LAST_PID}"

log "both shards started: Master=${MASTER_PID}, Caves=${CAVES_PID}"

while true; do
  if ! kill -0 "${MASTER_PID}" 2>/dev/null; then
    log "Master exited"
    wait "${MASTER_PID}" || true
    stop_children
    exit 1
  fi

  if ! kill -0 "${CAVES_PID}" 2>/dev/null; then
    log "Caves exited"
    wait "${CAVES_PID}" || true
    stop_children
    exit 1
  fi

  sleep 5
done