# shellcheck shell=bash
# ============================================================
# lib/template.sh —— 模板展开与占位符校验
#
# 从各游戏 adapter 的 prepare-runtime.sh 提取：
#   expand_templates         模板目录 → 数据目录（skip-if-exists）
#   assert_no_placeholder    校验文件不含占位符（如 PASTE_YOUR_TOKEN_HERE）
#   assert_non_empty         校验文件非空
#
# 依赖：lib/log.sh、lib/fs.sh（自动加载）
# ============================================================

if ! declare -F log >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/log.sh"
fi
if ! declare -F render_template_dir >/dev/null 2>&1; then
  # shellcheck source=/dev/null
  source "$(dirname "${BASH_SOURCE[0]}")/fs.sh"
fi

# expand_templates template_dir data_root —— 模板展开（skip-if-exists，保留用户文件）
expand_templates() {
  local template_dir="$1"
  local data_root="$2"
  if [ -d "${template_dir}" ]; then
    render_template_dir "${template_dir}" "${data_root}"
  else
    log_warn "template dir not found: ${template_dir}"
  fi
}

# assert_no_placeholder file placeholder —— 校验文件不含占位符（缺失返回 20）
assert_no_placeholder() {
  local file="$1"
  local placeholder="$2"
  if grep -q "${placeholder}" "${file}" 2>/dev/null; then
    log_error "${file} still contains placeholder: ${placeholder}"
    return 20
  fi
  return 0
}

# assert_non_empty file —— 校验文件非空（为空返回 21）
assert_non_empty() {
  local file="$1"
  if [ ! -s "${file}" ]; then
    log_error "${file} is empty"
    return 21
  fi
  return 0
}
