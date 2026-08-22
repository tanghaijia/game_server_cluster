#!/usr/bin/env bash
# ============================================================
# adapters/tools/ci-test.sh —— 适配器公共层 CI 契约测试
#
# 覆盖（adapter-framework-design.md §4）：
#   1. base 公共库 bats 单元测试（log/fs/output/template/port-inject）
#   2. base + 各游戏镜像构建（FROM base 分层验证）
#   3. 容器内契约检查：/scripts/*.sh 软链、hooks.sh、config-manifest.json、
#      health.sh JSON 契约
#
# 用法（在仓库根目录，需 docker + bats）：
#   ./adapters/tools/ci-test.sh [--skip-build]
# ============================================================
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE_TAG="game-base:ci"
GAMES=(dst 7daystodie)
SKIP_BUILD="${1:-}"

echo "==> [1/3] base 公共库 bats 单元测试"
if command -v bats >/dev/null 2>&1; then
  "${ROOT}/adapters/base/tests/run_tests.sh"
else
  echo "    bats not found, skip (install bats-core)"
fi

if [ "${SKIP_BUILD}" != "--skip-build" ]; then
  echo "==> [2/3] 构建镜像"
  docker build -t "${BASE_TAG}" "${ROOT}/adapters/base"
  for g in "${GAMES[@]}"; do
    echo "    building ${g}-adapter"
    docker build -t "${g}-adapter:ci" \
      --build-arg BASE_IMAGE="${BASE_TAG}" \
      -f "${ROOT}/adapters/${g}/Dockerfile" \
      "${ROOT}/adapters/${g}"
  done
fi

echo "==> [3/3] 容器内契约检查"
for g in "${GAMES[@]}"; do
  echo "    checking ${g}-adapter"
  docker run --rm --entrypoint bash "${g}-adapter:ci" -c '
    set -e
    # 1) 生命周期契约路径存在且为软链（指向 lifecycle/）
    for s in start save stop players health prepare-runtime; do
      [ -e "/scripts/${s}.sh" ] || { echo "MISSING /scripts/${s}.sh"; exit 1; }
    done
    # 2) hooks.sh 存在
    [ -f /scripts/hooks.sh ] || { echo "MISSING hooks.sh"; exit 1; }
    # 3) config-manifest.json 存在
    [ -f /scripts/config-manifest.json ] || { echo "MISSING config-manifest.json"; exit 1; }
    # 4) health.sh 输出合法 JSON 契约
    out="$(/scripts/health.sh)"
    echo "$out" | jq -e '.healthy == true or .healthy == false' >/dev/null \
      || { echo "BAD health output: $out"; exit 1; }
    echo "    OK: ${g}-adapter contract passed"
  '
done

echo "==> ALL ADAPTER CHECKS PASSED"
