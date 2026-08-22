# shellcheck shell=bash
# ============================================================
# lib/fs.sh —— 文件系统工具
#
# 从各游戏 adapter 的 prepare-runtime.sh 提取的公共逻辑：
#   copy_if_missing / copy_dir_files_if_missing / ensure_dir
#   render_template_dir / list_generated_files
#
# 依赖：lib/log.sh（本文件 source 时自动加载）
# ============================================================

# 相对 source 自身依赖，保证容器内与本机测试均可运行
if ! declare -F log >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/log.sh"
fi

# copy_if_missing src dst —— 目标不存在才复制（保留用户已有/已编辑文件）
copy_if_missing() {
  local src="$1"
  local dst="$2"
  if [ ! -f "${dst}" ]; then
    log "create ${dst}"
    cp "${src}" "${dst}"
  else
    log "keep existing ${dst}"
  fi
}

# copy_dir_files_if_missing src_dir dst_dir —— 目录内文件逐个按 copy_if_missing 复制
copy_dir_files_if_missing() {
  local src_dir="$1"
  local dst_dir="$2"
  ensure_dir "${dst_dir}"
  local src filename
  for src in "${src_dir}"/*; do
    [ -f "${src}" ] || continue
    filename="$(basename "${src}")"
    copy_if_missing "${src}" "${dst_dir}/${filename}"
  done
}

# ensure_dir dir —— 创建目录（含父目录）
ensure_dir() {
  mkdir -p "$1"
}

# render_template_dir src_dir dst_dir —— 将模板目录整体展开到目标（skip-if-exists）
render_template_dir() {
  local src_dir="$1"
  local dst_dir="$2"
  ensure_dir "${dst_dir}"
  copy_dir_files_if_missing "${src_dir}" "${dst_dir}"
}

# list_generated_files root [maxdepth] —— 列出已生成文件（调试输出，默认深度 3）
list_generated_files() {
  local root="$1"
  local maxdepth="${2:-3}"
  find "${root}" -maxdepth "${maxdepth}" -type f 2>/dev/null | sort
}
