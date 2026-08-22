#!/usr/bin/env bats
# ============================================================
# lib/template.sh 契约测试（占位符校验）
# ============================================================
setup() {
  TEST_DIR="$(mktemp -d)"
  export TEST_DIR
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/log.sh"
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/fs.sh"
  # shellcheck source=/dev/null
  source "${BATS_TEST_DIRNAME}/../scripts/lib/template.sh"
}

teardown() {
  rm -rf "${TEST_DIR}"
}

@test "assert_no_placeholder: 含占位符返回 20" {
  echo "token=PASTE_YOUR_TOKEN_HERE" > "${TEST_DIR}/f.txt"
  run assert_no_placeholder "${TEST_DIR}/f.txt" "PASTE_YOUR_TOKEN_HERE"
  [ "$status" -eq 20 ]
}

@test "assert_no_placeholder: 无占位符返回 0" {
  echo "token=abc123" > "${TEST_DIR}/f.txt"
  run assert_no_placeholder "${TEST_DIR}/f.txt" "PASTE_YOUR_TOKEN_HERE"
  [ "$status" -eq 0 ]
}

@test "assert_non_empty: 空文件返回 21" {
  : > "${TEST_DIR}/empty.txt"
  run assert_non_empty "${TEST_DIR}/empty.txt"
  [ "$status" -eq 21 ]
}

@test "assert_non_empty: 非空文件返回 0" {
  echo "x" > "${TEST_DIR}/full.txt"
  run assert_non_empty "${TEST_DIR}/full.txt"
  [ "$status" -eq 0 ]
}

@test "expand_templates: 模板展开到数据目录" {
  mkdir -p "${TEST_DIR}/tmpl"
  echo "t" > "${TEST_DIR}/tmpl/a.conf"
  expand_templates "${TEST_DIR}/tmpl" "${TEST_DIR}/data"
  [ -f "${TEST_DIR}/data/a.conf" ]
}
