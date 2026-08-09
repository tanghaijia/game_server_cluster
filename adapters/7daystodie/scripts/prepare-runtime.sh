#!/usr/bin/env bash
set -Eeuo pipefail

DATA_ROOT="${SDTD_DATA_ROOT:-/data}"
SERVER_ROOT="${SDTD_SERVER_ROOT:-/server}"
USER_DATA="${SDTD_USER_DATA:-/data/7DaysToDie}"

TEMPLATE_DIR="${TEMPLATE_DIR:-/templates}"
CONFIG_FILE="${SDTD_CONFIG_FILE:-/data/serverconfig.xml}"

echo "[prepare-runtime] DATA_ROOT=${DATA_ROOT}"
echo "[prepare-runtime] SERVER_ROOT=${SERVER_ROOT}"
echo "[prepare-runtime] USER_DATA=${USER_DATA}"
echo "[prepare-runtime] CONFIG_FILE=${CONFIG_FILE}"

mkdir -p "${USER_DATA}"
mkdir -p "${USER_DATA}/Saves"
mkdir -p "${USER_DATA}/Mods"

copy_if_missing() {
  local src="$1"
  local dst="$2"

  if [ ! -f "${dst}" ]; then
    echo "[prepare-runtime] create ${dst}"
    cp "${src}" "${dst}"
  else
    echo "[prepare-runtime] keep existing ${dst}"
  fi
}

# serverconfig.xml 放在 /data 下，启动时用 -configfile 指向它，用户可直接编辑
copy_if_missing "${TEMPLATE_DIR}/serverconfig.xml" "${CONFIG_FILE}"

# serveradmin.xml 位于 UserDataFolder/Saves 下（AdminFileName 相对 Saves）
copy_if_missing "${TEMPLATE_DIR}/serveradmin.xml" "${USER_DATA}/Saves/serveradmin.xml"

# 7DTD 无必须手动提供的 token，但可检查 config 是否非空，避免启动即失败
if [ ! -s "${CONFIG_FILE}" ]; then
  echo "[prepare-runtime] ERROR: ${CONFIG_FILE} is empty."
  exit 20
fi

echo "[prepare-runtime] generated files:"
find "${DATA_ROOT}" -maxdepth 4 -type f | sort

echo "[prepare-runtime] ok"
