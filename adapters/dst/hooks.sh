#!/usr/bin/env bash
# ============================================================
# hooks.sh —— DST（Don't Starve Together）差异钩子
#
# 由 base 生命周期框架 source（/scripts/hooks.sh）。
# 覆盖：hook_pre_start / hook_find_bin / hook_start_command / hook_save
# 继承默认：health（按 GAME_BIN_NAME）、stop 兜底（kill_remaining）
#
# DST 专用环境变量（M3 前由用户/平台设置，M3 后迁移到 adapter.toml config schema）：
#   DST_CONF_DIR  配置目录名，默认 DoNotStarveTogether
#   DST_CLUSTER   集群名，默认 Cluster
#   DST_SHARD     启动模式：Master / Caves / both（默认 both）
#   DST_BIN       覆盖主程序路径
# ============================================================

CONF_DIR="${DST_CONF_DIR:-DoNotStarveTogether}"
CLUSTER_NAME="${DST_CLUSTER:-Cluster}"
DST_BASE="${DATA_ROOT}/${CONF_DIR}/${CLUSTER_NAME}"

# ---- 预启动：模板展开（DST 布局）+ cluster_token 校验 ----------------
hook_pre_start() {
  log "dst_base=${DST_BASE}"
  ensure_dir "${DST_BASE}/Master" "${DST_BASE}/Caves"

  copy_if_missing "${TEMPLATE_DIR}/cluster.ini"      "${DST_BASE}/cluster.ini"
  copy_if_missing "${TEMPLATE_DIR}/adminlist.txt"    "${DST_BASE}/adminlist.txt"
  copy_if_missing "${TEMPLATE_DIR}/whitelist.txt"    "${DST_BASE}/whitelist.txt"
  copy_if_missing "${TEMPLATE_DIR}/blocklist.txt"    "${DST_BASE}/blocklist.txt"
  copy_if_missing "${TEMPLATE_DIR}/mod-manifest.json" "${DST_BASE}/mod-manifest.json"
  copy_dir_files_if_missing "${TEMPLATE_DIR}/Master" "${DST_BASE}/Master"
  copy_dir_files_if_missing "${TEMPLATE_DIR}/Caves"  "${DST_BASE}/Caves"

  # cluster_token.txt：平台注入的凭证（M8：凭证池分配 → /data/.platform/cluster_token）
  # 优先使用；无注入则回退模板（占位符，校验失败兜底）。
  if [ -f "${DATA_ROOT}/.platform/cluster_token" ] && [ -s "${DATA_ROOT}/.platform/cluster_token" ]; then
    cp "${DATA_ROOT}/.platform/cluster_token" "${DST_BASE}/cluster_token.txt"
    log "cluster_token.txt 使用平台注入凭证"
  else
    if [ ! -f "${DST_BASE}/cluster_token.txt" ]; then
      if [ -f "${TEMPLATE_DIR}/cluster_token.txt" ]; then
        cp "${TEMPLATE_DIR}/cluster_token.txt" "${DST_BASE}/cluster_token.txt"
      else
        touch "${DST_BASE}/cluster_token.txt"
      fi
      log_warn "cluster_token.txt 缺失或为占位，请编辑: ${DST_BASE}/cluster_token.txt"
    fi
  fi

  # 校验：占位符 / 空 token 直接失败（对齐原 prepare-runtime.sh 退出码）
  assert_no_placeholder "${DST_BASE}/cluster_token.txt" "PASTE_YOUR_KLEI_CLUSTER_TOKEN_HERE" || exit $?
  assert_non_empty "${DST_BASE}/cluster_token.txt" || exit $?
  # DST 要求 cluster_token.txt 首字符即 token，不允许注释行（真实 token 无注释前缀）
  if [ "$(head -c 1 "${DST_BASE}/cluster_token.txt")" = "#" ]; then
    log_error "cluster_token.txt 首字符为 #，DST 不允许注释行，token 必须是文件首字符"
    exit 22
  fi

  log "generated files:"
  list_generated_files "${DST_BASE}" 3
  log "prepare ok"
}

# ---- 定位主程序（支持 DST_BIN 覆盖） ----------------------------------
hook_find_bin() {
  if [ -n "${DST_BIN:-}" ]; then
    echo "${DST_BIN}"
    return 0
  fi
  find_bin_default "${GAME_BIN_NAME}"
}

# ---- 启动：Master + Caves 双 shard（或单 shard） ----------------------
hook_start_command() {
  # DST 需要 lib32 运行时库
  export LD_LIBRARY_PATH="${BIN_DIR}/lib32:${LD_LIBRARY_PATH:-}"
  cd "${BIN_DIR}"

  case "${DST_SHARD:-both}" in
    Master)
      start_one_shard "Master"
      wait_all
      ;;
    Caves)
      start_one_shard "Caves"
      wait_all
      ;;
    *)
      start_one_shard "Master"
      sleep 8
      start_one_shard "Caves"
      monitor_shards
      ;;
  esac
}

start_one_shard() {
  local shard="$1"
  log "starting shard=${shard}"
  "${BIN}" \
    -persistent_storage_root "${DATA_ROOT}" \
    -conf_dir "${CONF_DIR}" \
    -cluster "${CLUSTER_NAME}" \
    -shard "${shard}" &
  register_child "$!"
  log "${shard} pid=${_CHILD_PIDS[-1]}"
}

# 监控双 shard：任一退出则整体停止（对齐原 start.sh 行为）
monitor_shards() {
  local pid
  while true; do
    for pid in "${_CHILD_PIDS[@]}"; do
      if ! kill -0 "${pid}" 2>/dev/null; then
        log "shard pid=${pid} exited, stopping all shards"
        stop_children
        exit 1
      fi
    done
    sleep 5
  done
}

# ---- 保存：占位实现（对齐原 save.sh 的 sleep 模拟） --------------------
hook_save() {
  log "triggering save (placeholder, DST 暂无可靠 save 命令)"
  sleep 2
}
