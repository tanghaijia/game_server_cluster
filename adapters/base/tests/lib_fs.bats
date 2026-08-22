#!/usr/bin/env bats
# ============================================================
# lib/fs.sh 契约测试
# ============================================================
setup() {
  TEST_DIR="$(mktemp -d)"
  export TEST_DIR
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/log.sh"
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/fs.sh"
}

teardown() {
  rm -rf "${TEST_DIR}"
}

@test "copy_if_missing: 目标不存在时复制" {
  echo "src-content" > "${TEST_DIR}/src.txt"
  copy_if_missing "${TEST_DIR}/src.txt" "${TEST_DIR}/dst.txt"
  [ -f "${TEST_DIR}/dst.txt" ]
  [ "$(cat "${TEST_DIR}/dst.txt")" = "src-content" ]
}

@test "copy_if_missing: 目标已存在时保留原文件" {
  echo "src-content" > "${TEST_DIR}/src.txt"
  echo "user-edit" > "${TEST_DIR}/dst.txt"
  copy_if_missing "${TEST_DIR}/src.txt" "${TEST_DIR}/dst.txt"
  [ "$(cat "${TEST_DIR}/dst.txt")" = "user-edit" ]
}

@test "copy_dir_files_if_missing: 目录内文件逐个复制" {
  mkdir -p "${TEST_DIR}/src" "${TEST_DIR}/dst"
  echo "a" > "${TEST_DIR}/src/a.txt"
  echo "b" > "${TEST_DIR}/src/b.txt"
  copy_dir_files_if_missing "${TEST_DIR}/src" "${TEST_DIR}/dst"
  [ -f "${TEST_DIR}/dst/a.txt" ]
  [ -f "${TEST_DIR}/dst/b.txt" ]
}

@test "copy_dir_files_if_missing: 目标已有同名文件不被覆盖" {
  mkdir -p "${TEST_DIR}/src" "${TEST_DIR}/dst"
  echo "new" > "${TEST_DIR}/src/a.txt"
  echo "old" > "${TEST_DIR}/dst/a.txt"
  copy_dir_files_if_missing "${TEST_DIR}/src" "${TEST_DIR}/dst"
  [ "$(cat "${TEST_DIR}/dst/a.txt")" = "old" ]
}

@test "ensure_dir: 创建多级目录" {
  ensure_dir "${TEST_DIR}/a/b/c"
  [ -d "${TEST_DIR}/a/b/c" ]
}

@test "render_template_dir: 模板目录展开" {
  mkdir -p "${TEST_DIR}/tmpl"
  echo "t" > "${TEST_DIR}/tmpl/x.conf"
  render_template_dir "${TEST_DIR}/tmpl" "${TEST_DIR}/out"
  [ -f "${TEST_DIR}/out/x.conf" ]
}

@test "list_generated_files: 列出文件并按字典序" {
  mkdir -p "${TEST_DIR}/root"
  touch "${TEST_DIR}/root/b.txt" "${TEST_DIR}/root/a.txt"
  run list_generated_files "${TEST_DIR}/root"
  [ "${lines[0]}" = "${TEST_DIR}/root/a.txt" ]
  [ "${lines[1]}" = "${TEST_DIR}/root/b.txt" ]
}
