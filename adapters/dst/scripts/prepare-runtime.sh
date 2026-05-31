#!/usr/bin/env bash
set -Eeuo pipefail

DATA_ROOT="${DATA_ROOT:-/data}"
CONF_DIR="${DST_CONF_DIR:-DoNotStarveTogether}"
CLUSTER_NAME="${DST_CLUSTER:-Cluster}"

TEMPLATE_DIR="${TEMPLATE_DIR:-/templates}"

DST_BASE="${DATA_ROOT}/${CONF_DIR}/${CLUSTER_NAME}"
MASTER_DIR="${DST_BASE}/Master"
CAVES_DIR="${DST_BASE}/Caves"

echo "[prepare-runtime] DATA_ROOT=${DATA_ROOT}"
echo "[prepare-runtime] CONF_DIR=${CONF_DIR}"
echo "[prepare-runtime] CLUSTER_NAME=${CLUSTER_NAME}"
echo "[prepare-runtime] DST_BASE=${DST_BASE}"

mkdir -p "${DST_BASE}"
mkdir -p "${MASTER_DIR}"
mkdir -p "${CAVES_DIR}"

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

copy_dir_files_if_missing() {
  local src_dir="$1"
  local dst_dir="$2"

  mkdir -p "${dst_dir}"

  for src in "${src_dir}"/*; do
    [ -f "${src}" ] || continue
    local filename
    filename="$(basename "${src}")"
    copy_if_missing "${src}" "${dst_dir}/${filename}"
  done
}

copy_if_missing "${TEMPLATE_DIR}/cluster.ini" "${DST_BASE}/cluster.ini"
copy_if_missing "${TEMPLATE_DIR}/adminlist.txt" "${DST_BASE}/adminlist.txt"
copy_if_missing "${TEMPLATE_DIR}/whitelist.txt" "${DST_BASE}/whitelist.txt"
copy_if_missing "${TEMPLATE_DIR}/blocklist.txt" "${DST_BASE}/blocklist.txt"
copy_if_missing "${TEMPLATE_DIR}/mod-manifest.json" "${DST_BASE}/mod-manifest.json"

copy_dir_files_if_missing "${TEMPLATE_DIR}/Master" "${MASTER_DIR}"
copy_dir_files_if_missing "${TEMPLATE_DIR}/Caves" "${CAVES_DIR}"

if [ ! -f "${DST_BASE}/cluster_token.txt" ]; then
  if [ -f "${TEMPLATE_DIR}/cluster_token.txt" ]; then
    cp "${TEMPLATE_DIR}/cluster_token.txt" "${DST_BASE}/cluster_token.txt"
  else
    touch "${DST_BASE}/cluster_token.txt"
  fi

  echo "[prepare-runtime] WARNING: cluster_token.txt is missing or placeholder."
  echo "[prepare-runtime] Please edit: ${DST_BASE}/cluster_token.txt"
fi

if grep -q "PASTE_YOUR_KLEI_CLUSTER_TOKEN_HERE" "${DST_BASE}/cluster_token.txt" 2>/dev/null; then
  echo "[prepare-runtime] ERROR: cluster_token.txt still contains placeholder token."
  echo "[prepare-runtime] Path: ${DST_BASE}/cluster_token.txt"
  exit 20
fi

if [ ! -s "${DST_BASE}/cluster_token.txt" ]; then
  echo "[prepare-runtime] ERROR: cluster_token.txt is empty."
  echo "[prepare-runtime] Path: ${DST_BASE}/cluster_token.txt"
  exit 21
fi

echo "[prepare-runtime] generated files:"
find "${DST_BASE}" -maxdepth 3 -type f | sort

echo "[prepare-runtime] ok"
