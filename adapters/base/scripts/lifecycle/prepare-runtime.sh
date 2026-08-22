#!/usr/bin/env bash
# ============================================================
# lifecycle/prepare-runtime.sh —— 默认预启动：模板展开 + 基础校验
#
# start.sh 框架在未定义 hook_pre_start 时调用本脚本。
# 游戏适配器可在 hooks.sh 中定义 hook_pre_start 覆盖（如 DST token 校验、
# 7dtd steamclient.so 检查），并在覆盖实现里复用本库函数。
# ============================================================
set -Eeuo pipefail

LOG_PHASE="prepare-runtime"
# shellcheck source=/dev/null
source /scripts/lib/env.sh
# shellcheck source=/dev/null
source /scripts/lib/log.sh
# shellcheck source=/dev/null
source /scripts/lib/fs.sh
# shellcheck source=/dev/null
source /scripts/lib/template.sh

log "DATA_ROOT=${DATA_ROOT}"
log "SERVER_ROOT=${SERVER_ROOT}"
log "TEMPLATE_DIR=${TEMPLATE_DIR}"

# 模板展开（skip-if-exists：用户已有文件/已编辑文件保留）
expand_templates "${TEMPLATE_DIR}" "${DATA_ROOT}"

log "generated files:"
list_generated_files "${DATA_ROOT}" 4
log "ok"
