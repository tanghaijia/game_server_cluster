#!/usr/bin/env bash
# ============================================================
# hooks.sh —— 英灵神殿（Valheim）差异钩子
#
# 由 base 生命周期框架 source（/scripts/hooks.sh）。
# 覆盖：hook_pre_start / hook_find_bin / hook_start_command / hook_process_pattern
# 继承默认：health（按 GAME_BIN_NAME 进程匹配）、stop 兜底（kill_remaining）
#           save 空操作（Valheim 每 30 分钟自动存档 + 停机时存档，无控制台命令）
#
# 配置模型（adapter.toml 草案 V 系定案）：
#   Valheim 无配置文件 → 平台配置项（ServerName/WorldName/Password/Public/
#   Crossplay/MemoryMb）经 config_env_export + config_env_defaults 注入 env，
#   hook_start_command 拼装 CLI 参数下发。
#
# Valheim 专用环境变量（覆盖默认路径）：
#   VALHEIM_BIN    覆盖主程序路径（默认 find_bin_default valheim_server.x86_64）
#   VALHEIM_SAVEDIR 存档目录，默认 ${DATA_ROOT}/valheim
#   VALHEIM_HOME   HOME 目录（Unity 需要可写 HOME），默认 ${DATA_ROOT}
#
# 已知注意（草案 V-2）：Valheim 在 Docker overlayfs 上偶发 ENOSPC 假崩溃，
# 本平台 /server 由宿主机目录 bind 挂载可规避；详见 adapter.toml 头注释。
# ============================================================

SAVEDIR="${VALHEIM_SAVEDIR:-${DATA_ROOT}/valheim}"

# 自载公共库：config-env.sh 不在 base 生命周期默认 source 列表内
if ! declare -F config_env_export >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/lib/config-env.sh"
fi

# ---- 预启动：存档目录 + 管理列表 + 配置 env ---------------------------
hook_pre_start() {
  export HOME="${VALHEIM_HOME:-${DATA_ROOT}}"
  ensure_dir "${SAVEDIR}"

  # 管理列表（savedir 下，玩家可自行编辑；permittedlist 为 crossplay 白名单）
  copy_if_missing "${TEMPLATE_DIR}/adminlist.txt"     "${SAVEDIR}/adminlist.txt"
  copy_if_missing "${TEMPLATE_DIR}/bannedlist.txt"    "${SAVEDIR}/bannedlist.txt"
  copy_if_missing "${TEMPLATE_DIR}/permittedlist.txt" "${SAVEDIR}/permittedlist.txt"

  # 配置 env：game-config.json 优先，config-defaults.env 补齐缺失键
  config_env_export || true
  config_env_defaults || true

  log "savedir=${SAVEDIR}"
  log "generated files:"
  list_generated_files "${SAVEDIR}" 2
  log "prepare ok"
}

# ---- 定位主程序（wrapper，支持 VALHEIM_BIN 覆盖） ----------------------
hook_find_bin() {
  if [ -n "${VALHEIM_BIN:-}" ]; then
    echo "${VALHEIM_BIN}"
    return 0
  fi
  find_bin_default "valheim_server.x86_64"
}

# ---- 启动：CLI 参数拼装（-savedir 指向 /data，世界文件入 S3 快照） -------
hook_start_command() {
  export HOME="${VALHEIM_HOME:-${DATA_ROOT}}"
  cd "${BIN_DIR}"

  local name="${ServerName:-My Valheim Server}"
  local world="${WorldName:-Dedicated}"
  local password="${Password:-}"
  local port="${GAME_HOST_PORT:-2456}"
  local public_flag="${Public:-true}"
  local crossplay_flag="${Crossplay:-false}"
  local memory="${MemoryMb:-2048}"

  # Valheim 要求 ≥5 字符的加入密码（缺失则服务器拒绝启动）
  if [ "${#password}" -lt 5 ]; then
    log_error "Valheim 需要 ≥5 字符的加入密码（配置项 Password），当前为空或过短"
    exit 23
  fi

  [ "${public_flag}" = "true" ] && public_flag=1 || public_flag=0

  local args=(
    -nographics
    -batchmode
    -name "${name}"
    -port "${port}"
    -world "${world}"
    -password "${password}"
    -public "${public_flag}"
    -savedir "${SAVEDIR}"
    -memory "${memory}"
  )
  if [ "${crossplay_flag}" = "true" ]; then
    args+=(-crossplay)
  fi

  log "starting Valheim dedicated server: -name=${name} -port=${port} -world=${world} -public=${public_flag} crossplay=${crossplay_flag} memory=${memory}"
  run_in_foreground "${BIN}" "${args[@]}"
}

# ---- stop 兜底匹配模式（wrapper 与子进程都命中） -----------------------
hook_process_pattern() {
  echo "valheim_server"
}
