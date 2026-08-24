#!/usr/bin/env bash
# ============================================================
# hooks.sh —— 幻兽帕鲁（Palworld）差异钩子
#
# 由 base 生命周期框架 source（/scripts/hooks.sh）。
# 覆盖：hook_pre_start / hook_find_bin / hook_start_command
#       hook_process_pattern
# 继承默认：health（按 GAME_BIN_NAME 进程匹配）、stop 兜底（kill_remaining）
#           save 空操作（Palworld 每 30s 自动存档；REST API 可选未接入）
#
# 渲染模型（adapter.toml 草案 P-1 定案）：
#   PalWorldSettings.ini 是单行 OptionSettings=(k=v,...)，与 per-key
#   xml_property 渲染不匹配 → 由本 hook 全量渲染：
#     1) /server 只读 → ini/Logs/SaveGames 软链到 /data（P-2 解决）
#     2) config_env_export（game-config.json → env）+ config_env_defaults
#        （config-defaults.env 补齐默认值）
#     3) envsubst 从模板全量渲染 ini（平台配置即事实源，每次启动生效）
#   bool 值在配置中为 true/false（小写，UE FParse::Bool 大小写不敏感可解析）；
#   PublicPort 用 ${GAME_HOST_PORT}（平台注入）占位，bIsMultiplay 固定 True。
#
# Palworld 专用环境变量（覆盖默认路径）：
#   PAL_INSTALL  安装目录，默认 ${SERVER_ROOT}/PalServer
#   PAL_BIN      覆盖启动器路径（默认 find_bin_default PalServer.sh）
#   PAL_HOME     HOME 目录（UE 需要可写 HOME），默认 ${DATA_ROOT}
# ============================================================

PAL_INSTALL="${PAL_INSTALL:-${SERVER_ROOT}/PalServer}"
PAL_INI="${DATA_ROOT}/PalWorldSettings.ini"

# 自载公共库：config-env.sh 不在 base 生命周期默认 source 列表内
if ! declare -F config_env_export >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/lib/config-env.sh"
fi

# ---- 预启动：软链 + 配置 env + 全量渲染 ini + steamclient.so 校验 ----
hook_pre_start() {
  export HOME="${PAL_HOME:-${DATA_ROOT}}"

  # /server 只读：配置/日志/存档软链到 /data（P-2）
  if [ -d "${PAL_INSTALL}/Pal/Saved" ]; then
    ln -sfn "${PAL_INI}" "${PAL_INSTALL}/Pal/Saved/Config/LinuxServer/PalWorldSettings.ini" \
      || log_warn "ini 软链失败（目录缺失或只读？）"
    ln -sfn "${DATA_ROOT}/Logs" "${PAL_INSTALL}/Pal/Saved/Logs" \
      || log_warn "Logs 软链失败"
    ln -sfn "${DATA_ROOT}/SaveGames" "${PAL_INSTALL}/Pal/Saved/SaveGames" \
      || log_warn "SaveGames 软链失败"
  else
    log_warn "未找到 ${PAL_INSTALL}/Pal/Saved，跳过软链（请确认服务器文件已就位）"
  fi

  # 配置 env：game-config.json 优先，config-defaults.env 补齐缺失键
  config_env_export || true
  config_env_defaults || true

  # 全量渲染：平台配置 = 事实源，每次启动覆盖（手工编辑 ini 会被重置，请走平台配置）
  if [ -f "${TEMPLATE_DIR}/PalWorldSettings.ini" ]; then
    local tmp
    ensure_dir "${DATA_ROOT}"
    tmp="$(mktemp "${PAL_INI}.XXXXXX")" || exit 1
    envsubst < "${TEMPLATE_DIR}/PalWorldSettings.ini" > "${tmp}" && mv "${tmp}" "${PAL_INI}"
    log "rendered ${PAL_INI}"
  else
    log_warn "template PalWorldSettings.ini not found, skip render"
  fi

  # steamclient.so：UE Steam GameServer 初始化必需（P-3）
  if ! find "${SERVER_ROOT}" -name steamclient.so -type f 2>/dev/null | grep -q .; then
    log_warn "steamclient.so not found under ${SERVER_ROOT}; Steam GameServer init will fail."
  fi

  log "generated files:"
  list_generated_files "${DATA_ROOT}" 3
  log "prepare ok"
}

# ---- 定位启动器（支持 PAL_BIN 覆盖） ----------------------------------
hook_find_bin() {
  if [ -n "${PAL_BIN:-}" ]; then
    echo "${PAL_BIN}"
    return 0
  fi
  find_bin_default "PalServer.sh"
}

# ---- 启动：PalServer.sh（UE 环境由启动器装配） ------------------------
hook_start_command() {
  export HOME="${PAL_HOME:-${DATA_ROOT}}"
  cd "$(dirname "${BIN}")"

  log "starting Palworld dedicated server..."
  run_in_foreground "${BIN}" \
    -port="${GAME_HOST_PORT:-8211}" \
    -players="${ServerPlayerMaxNum:-32}" \
    -useperfthreads \
    -NoAsyncLoadingThread \
    -UseMultithreadForDS
}

# ---- stop 兜底匹配模式 ------------------------------------------------
hook_process_pattern() {
  echo "PalServer-Linux-Test"
}
