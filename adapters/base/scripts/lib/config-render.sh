# shellcheck shell=bash
# ============================================================
# lib/config-render.sh —— 按 key 合并平台下发的配置进游戏配置文件
#
# 解决「游戏配置文件（serverconfig.xml 等）大量可配置项写死」问题
# （见 adapter-framework-design.md §3.4）。
#
# 输入：
#   PLATFORM_CONFIG_FILE  /data/.platform/game-config.json
#                         （platform 级 + player 级合并后的实例配置，平台下发）
#   config-manifest.json  镜像内渲染清单（由 adapter.toml 生成，M4）：
#                         {"settings":[{"key":"ServerName","render":"xml_property",
#                          "apply":"always"|"on_first_start"}, ...]}
#
# 关键性质：按 key 粒度合并，不整体重写文件 → 用户手工添加的项原样保留。
#
# 依赖：lib/log.sh、lib/fs.sh、lib/port-inject.sh（自动加载）
# ============================================================

if ! declare -F log >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/log.sh"
fi
if ! declare -F ensure_dir >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/fs.sh"
fi
if ! declare -F inject_xml_property >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/port-inject.sh"
fi

# render_config_from_manifest config_file manifest_file [platform_config]
#   按 manifest 把平台配置合并进游戏配置文件。
#   config 文件不存在时先由模板生成（调用方负责模板展开）。
#   返回 0 表示成功（跳过项不算失败）。
render_config_from_manifest() {  local config_file="$1"
  local manifest_file="$2"
  local platform_config="${3:-${PLATFORM_CONFIG_FILE}}"

  if [ ! -f "${manifest_file}" ]; then
    log_warn "config manifest not found: ${manifest_file}"
    return 0
  fi
  if [ ! -f "${platform_config}" ]; then
    log "no platform config at ${platform_config}, skip render"
    return 0
  fi
  if ! jq -e . "${platform_config}" >/dev/null 2>&1; then
    log_warn "platform config is not valid JSON: ${platform_config}"
    return 0
  fi

  local key render apply value
  local keys
  keys="$(jq -r '.settings[] | select(.apply != "locked") | .key' "${manifest_file}")"
  while IFS= read -r key; do
    [ -n "${key}" ] || continue
    render="$(jq -r --arg k "${key}" '.settings[] | select(.key==$k) | .render // "xml_property"' "${manifest_file}")"
    apply="$(jq -r --arg k "${key}" '.settings[] | select(.key==$k) | .apply // "always"' "${manifest_file}")"
    if [ "${apply}" = "on_first_start" ] && [ -f "${config_file}" ]; then
      log "skip on_first_start key ${key} (config file already exists)"
      continue
    fi
    value="$(jq -r --arg k "${key}" '.[$k] // empty' "${platform_config}")"
    if [ -z "${value}" ]; then
      log "no value for ${key}, skip"
      continue
    fi
    case "${render}" in
      xml_property)
        inject_xml_property "${config_file}" "${key}" "${value}"
        ;;
      envsubst)
        inject_envsubst "${config_file}"
        ;;
      *)
        log_warn "unknown render type ${render} for ${key}, skip"
        ;;
    esac
  done <<< "${keys}"

  log "config render done: ${config_file}"
  return 0
}

# render_config_auto manifest_file —— 从 manifest 读取渲染目标文件并执行
# （manifest 结构：{"file": "/data/serverconfig.xml", "settings": [...]}）
# 供生命周期框架在启动时统一调用；无 manifest 或无 file 时静默跳过。
render_config_auto() {
  local manifest_file="$1"
  if [ ! -f "${manifest_file}" ]; then
    log "config manifest not found: ${manifest_file}, skip render"
    return 0
  fi
  local config_file
  config_file="$(jq -r '.file // empty' "${manifest_file}")"
  if [ -z "${config_file}" ] || [ "${config_file}" = "null" ]; then
    log "config manifest has no render file, skip render"
    return 0
  fi
  render_config_from_manifest "${config_file}" "${manifest_file}"
}
