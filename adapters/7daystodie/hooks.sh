#!/usr/bin/env bash
# ============================================================
# hooks.sh —— 7 Days to Die 差异钩子
#
# 由 base 生命周期框架 source（/scripts/hooks.sh）。
# 覆盖：hook_pre_start / hook_find_bin / hook_start_command
#       hook_save / hook_graceful_stop / hook_process_pattern
# 继承默认：health（按 GAME_BIN_NAME）、players
#
# 7dtd 专用环境变量（M3 前由用户/平台设置，M3 后迁移到 adapter.toml config schema）：
#   SDTD_USER_DATA   用户数据目录，默认 /data/7DaysToDie
#   SDTD_CONFIG_FILE serverconfig.xml 位置，默认 /data/serverconfig.xml
#   SDTD_TELNET_PORT telnet 端口，默认 8081
#   SDTD_BIN         覆盖主程序路径
#   SDTD_HOME        HOME 目录（Unity/EOS 需要可写 HOME）
# ============================================================

CONFIG_FILE="${SDTD_CONFIG_FILE:-${DATA_ROOT}/serverconfig.xml}"
USER_DATA="${SDTD_USER_DATA:-${DATA_ROOT}/7DaysToDie}"

# M2 兼容：平台当前注入 SDTD_SERVER_PORT（M3 统一为 GAME_HOST_PORT 后此行为空）
: "${GAME_HOST_PORT:=${SDTD_SERVER_PORT:-}}"

# ---- 预启动：模板展开（7dtd 布局）+ steamclient.so 检查 + 端口注入 -----
hook_pre_start() {
  # Unity/7dtd 需要可写的 HOME（EOS DeviceId 落盘）
  export HOME="${SDTD_HOME:-${DATA_ROOT}}"

  log "USER_DATA=${USER_DATA}"
  log "CONFIG_FILE=${CONFIG_FILE}"
  ensure_dir "${USER_DATA}/Saves" "${USER_DATA}/Mods"

  # serverconfig.xml 在 /data 下（用户可直接编辑）；serveradmin.xml 在 UserDataFolder/Saves 下
  copy_if_missing "${TEMPLATE_DIR}/serverconfig.xml" "${CONFIG_FILE}"
  copy_if_missing "${TEMPLATE_DIR}/serveradmin.xml"  "${USER_DATA}/Saves/serveradmin.xml"

  # steamclient.so：Steam GameServer 初始化必需，属运行环境（/server），不写入 /data
  if ! find "${SERVER_ROOT}" -name steamclient.so -type f 2>/dev/null | grep -q .; then
    log_warn "steamclient.so not found under ${SERVER_ROOT}; Steam GameServer init will fail."
  fi

  # 配置非空校验（对齐原 prepare-runtime.sh）
  assert_non_empty "${CONFIG_FILE}" || exit $?

  # 端口注入：GAME_HOST_PORT（宿主端口）→ ServerPort 改写
  apply_port_inject xml_property "${CONFIG_FILE}" ServerPort

  log "generated files:"
  list_generated_files "${DATA_ROOT}" 4
  log "prepare ok"
}

# ---- 定位主程序（支持 SDTD_BIN 覆盖） ----------------------------------
hook_find_bin() {
  if [ -n "${SDTD_BIN:-}" ]; then
    echo "${SDTD_BIN}"
    return 0
  fi
  find_bin_default "${GAME_BIN_NAME}"
}

# ---- 启动（-logfile 必须指向 stdout，避免写 /server 只读目录） ----------
hook_start_command() {
  export HOME="${SDTD_HOME:-${DATA_ROOT}}"
  cd "${BIN_DIR}"
  export LD_LIBRARY_PATH="${BIN_DIR}:${BIN_DIR}/linux64:${LD_LIBRARY_PATH:-}"

  log "starting 7 Days to Die dedicated server..."
  run_in_foreground "${BIN}" \
    -configfile="${CONFIG_FILE}" \
    -logfile /dev/stdout \
    -quit \
    -batchmode \
    -nographics \
    -dedicated
}

# ---- 保存：telnet saveworld（服务未启动/Telnet 关闭时降级不报错） -------
hook_save() {
  local telnet_port="${SDTD_TELNET_PORT:-8081}"
  local host="127.0.0.1"

  if ! (echo > "/dev/tcp/${host}/${telnet_port}") 2>/dev/null; then
    log "telnet on ${host}:${telnet_port} unavailable, skip save"
    return 0
  fi

  exec 3<>"/dev/tcp/${host}/${telnet_port}"
  printf 'saveworld\n' >&3
  sleep 2
  printf 'exit\n' >&3
  exec 3<&-
  log "save triggered via telnet"
}

# ---- 优雅停止：telnet shutdown（失败时框架兜底 TERM→KILL） --------------
hook_graceful_stop() {
  local telnet_port="${SDTD_TELNET_PORT:-8081}"
  local host="127.0.0.1"

  if (echo > "/dev/tcp/${host}/${telnet_port}") 2>/dev/null; then
    log "sending graceful shutdown via telnet..."
    exec 3<>"/dev/tcp/${host}/${telnet_port}"
    printf 'shutdown\n' >&3
    sleep 3
    printf 'exit\n' >&3
    exec 3<&-
  else
    log "telnet unavailable, fallback to TERM"
  fi
}

# ---- stop 兜底匹配模式 ------------------------------------------------
hook_process_pattern() {
  echo "7DaysToDieServer.x86_64"
}
