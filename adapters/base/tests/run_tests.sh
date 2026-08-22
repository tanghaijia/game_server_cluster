#!/usr/bin/env bash
# ============================================================
# tests/run_tests.sh —— 运行 base 公共层契约测试
#
# 依赖：bats-core（https://github.com/bats-core/bats-core）、jq
# 用法：
#   ./run_tests.sh              # 运行全部 bats 测试
#   ./run_tests.sh shellcheck   # 运行 shellcheck（可选）
# ============================================================
set -Eeuo pipefail

BASE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TESTS_DIR="${BASE_DIR}/tests"

if ! command -v bats >/dev/null 2>&1; then
  echo "ERROR: bats not found. Install bats-core first." >&2
  echo "  e.g. apt install bats (Debian) / brew install bats-core (macOS)" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "ERROR: jq not found (output 契约测试需要 jq 解析 JSON)." >&2
  exit 1
fi

echo "==> Running bats tests in ${TESTS_DIR}"
bats "${TESTS_DIR}"/*.bats

if [ "${1:-}" = "shellcheck" ]; then
  if ! command -v shellcheck >/dev/null 2>&1; then
    echo "ERROR: shellcheck not found, skip." >&2
    exit 1
  fi
  echo "==> Running shellcheck on lib/ and lifecycle/"
  shellcheck "${BASE_DIR}"/scripts/lib/*.sh "${BASE_DIR}"/scripts/lifecycle/*.sh
fi
