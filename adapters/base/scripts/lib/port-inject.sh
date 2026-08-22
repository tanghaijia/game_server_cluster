# shellcheck shell=bash
# ============================================================
# lib/port-inject.sh —— 通用端口注入
#
# 平台注入 GAME_HOST_PORT=<宿主端口>（见 adapter.toml port_inject.env），
# 本库负责把该值写进游戏配置文件（游戏进程不认 env，认自己的配置文件）。
#
# 支持三种声明式规则（render 类型，manifest 驱动见 lib/config-render.sh）：
#   xml_property  <property name="X" value="..."/>  改写/插入属性（7dtd ServerPort）
#   envsubst      文件内 ${VAR} 占位符替换
#
# 依赖：lib/log.sh、lib/fs.sh（自动加载）
# ============================================================

if ! declare -F log >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/log.sh"
fi
if ! declare -F ensure_dir >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/fs.sh"
fi

# inject_xml_property file prop_name value
#   对形如 <property name="X" value="..."/> 的 XML 配置：
#   属性存在 → 只替换 value；不存在 → 在 <ServerSettings> 后插入一行。
#   文件不存在返回 1（不中断调用方）。
inject_xml_property() {
  local file="$1"
  local prop_name="$2"
  local value="$3"
  if [ ! -f "${file}" ]; then
    log_warn "inject_xml_property: file not found ${file}"
    return 1
  fi
  if grep -q "<property name=\"${prop_name}\"" "${file}"; then
    sed -i "s|<property name=\"${prop_name}\"[[:space:]]*value=\"[^\"]*\"/>|<property name=\"${prop_name}\" value=\"${value}\"/>|" "${file}"
    log "injected ${prop_name}=${value} into ${file}"
  else
    # 找不到 ServerSettings 根节点时跳过（避免破坏非标准文件）
    if grep -q "<ServerSettings>" "${file}"; then
      sed -i "s|<ServerSettings>|<ServerSettings>\n\t<property name=\"${prop_name}\" value=\"${value}\"/>|" "${file}"
      log "inserted ${prop_name}=${value} into ${file}"
    else
      log_warn "inject_xml_property: <ServerSettings> root not found in ${file}, skip"
      return 1
    fi
  fi
  return 0
}

# inject_envsubst file —— 对文件内 ${VAR} 占位符做环境变量替换（幂等）
inject_envsubst() {
  local file="$1"
  if [ ! -f "${file}" ]; then
    log_warn "inject_envsubst: file not found ${file}"
    return 1
  fi
  local tmp
  tmp="$(mktemp "${file}.XXXXXX")" || return 1
  envsubst < "${file}" > "${tmp}" && mv "${tmp}" "${file}"
  log "envsubst applied to ${file}"
}

# inject_envsubst_from_template src_file dst_file —— 模板渲染（占位符替换后落地）
inject_envsubst_from_template() {
  local src_file="$1"
  local dst_file="$2"
  ensure_dir "$(dirname "${dst_file}")"
  envsubst < "${src_file}" > "${dst_file}"
  log "rendered ${src_file} -> ${dst_file}"
}

# apply_port_inject —— 默认端口注入入口：
#   GAME_HOST_PORT 非空时按注入类型改写配置文件。
# 用法：apply_port_inject xml_property /data/serverconfig.xml ServerPort
#       apply_port_inject envsubst /data/some.ini
apply_port_inject() {
  local render_type="${1:-xml_property}"
  local file="$2"
  local prop_name="$3"
  if [ -z "${GAME_HOST_PORT}" ]; then
    log "GAME_HOST_PORT empty, skip port injection"
    return 0
  fi
  case "${render_type}" in
    xml_property)
      inject_xml_property "${file}" "${prop_name}" "${GAME_HOST_PORT}"
      ;;
    envsubst)
      inject_envsubst "${file}"
      ;;
    *)
      log_warn "unknown port inject type ${render_type}, skip"
      return 1
      ;;
  esac
}
