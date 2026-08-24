# shellcheck shell=bash
# ============================================================
# lib/config-env.sh —— 平台配置 → 环境变量（hooks 用）
#
# 解决「无配置文件型游戏（Valheim CLI 参数）与单行 INI 型游戏
# （Palworld OptionSettings）无法走 per-key 渲染」问题：
# 把平台下发的 game-config.json（PLATFORM_CONFIG_FILE）导出为环境变量，
# 供 hook_start_command 拼装 CLI 参数 / hook_pre_start 做 envsubst 渲染。
#
# 两个函数：
#   config_env_export   导出 game-config.json 全部键（含空值）为 env
#   config_env_defaults 用 config-defaults.env（gen_manifest.py 生成，
#                       adapter.toml 默认值的单一事实源）补齐未设置键
#
# 约定：两者都【不覆盖已存在的同名变量】→ 平台注入变量（GAME_HOST_PORT 等）
# 与先导出的配置值优先，默认值只填空缺。
#
# 依赖：lib/log.sh（自动加载）；jq（base 镜像已装）
# ============================================================

if ! declare -F log >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/log.sh"
fi

# config_env_export [file] —— 导出平台配置为环境变量（默认 PLATFORM_CONFIG_FILE）
config_env_export() {
  local file="${1:-${PLATFORM_CONFIG_FILE}}"
  if [ ! -f "${file}" ]; then
    log "no platform config at ${file}, skip env export"
    return 0
  fi
  if ! jq -e . "${file}" >/dev/null 2>&1; then
    log_warn "platform config is not valid JSON: ${file}"
    return 0
  fi
  local key value
  while IFS=$'\t' read -r key value; do
    [ -n "${key}" ] || continue
    if ! declare -p "${key}" >/dev/null 2>&1; then
      export "${key}=${value}"
      log "config env: ${key}=${value}"
    else
      log "keep existing env: ${key}（平台注入优先，跳过配置值）"
    fi
  done < <(jq -r 'to_entries[] | [.key, (.value | tostring)] | @tsv' "${file}")
  return 0
}

# config_env_defaults [file] —— 用默认值文件补齐未设置的键（默认 /scripts/config-defaults.env）
# 文件格式：每行 KEY='value'（单引号包裹，gen_manifest.py 生成）
config_env_defaults() {
  local file="${1:-${CONFIG_DEFAULTS_FILE:-/scripts/config-defaults.env}}"
  if [ ! -f "${file}" ]; then
    log_warn "config defaults not found: ${file}（hook 需自行兜底或缺失键保持占位）"
    return 0
  fi
  local line key value
  while IFS= read -r line; do
    # 跳过注释与空行；解析 KEY='value' 单引号行（不 eval/source，安全）
    [[ "${line}" =~ ^[[:space:]]*# ]] && continue
    [[ "${line}" =~ ^([A-Za-z_][A-Za-z0-9_]*)='(.*)'[[:space:]]*$ ]] || continue
    key="${BASH_REMATCH[1]}"
    value="${BASH_REMATCH[2]}"
    if ! declare -p "${key}" >/dev/null 2>&1; then
      export "${key}=${value}"
      log "default env: ${key}=${value}"
    fi
  done < "${file}"
  return 0
}
