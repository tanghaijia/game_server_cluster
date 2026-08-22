# shellcheck shell=bash
# ============================================================
# lib/env.sh —— 公共环境变量解析（平台约定边界）
#
# 所有生命周期脚本与 hooks 在开头 source 本文件后，
# 即可直接使用以下变量（带默认值，平台/适配器可覆盖）。
#
# 变量分三类（见 adapter-framework-design.md §3.5）：
#   1. 平台约定边界：DATA_ROOT / SERVER_ROOT / TEMPLATE_DIR / MOD_CACHE
#   2. 平台注入（运行时）：GAME_HOST_PORT
#   3. 平台下发配置：PLATFORM_CONFIG_FILE
# ============================================================

# 平台约定边界（镜像挂载契约）
: "${DATA_ROOT:=/data}"            # 实例持久化数据根目录（可写）
: "${SERVER_ROOT:=/server}"        # 游戏服务端文件目录（只读）
: "${TEMPLATE_DIR:=/templates}"    # 游戏配置模板目录
: "${MOD_CACHE:=/mod-cache}"       # 模组缓存目录（可选）

# 平台注入：游戏端口对应的宿主端口（controller 按 port_inject.env 注入）
: "${GAME_HOST_PORT:=}"

# 平台下发的实例配置（配置渲染链路，见 lib/config-render.sh）
: "${PLATFORM_CONFIG_FILE:=/data/.platform/game-config.json}"

# 钩子文件位置（游戏适配器可覆盖为绝对路径）
: "${HOOKS_FILE:=/scripts/hooks.sh}"

export DATA_ROOT SERVER_ROOT TEMPLATE_DIR MOD_CACHE
export GAME_HOST_PORT PLATFORM_CONFIG_FILE HOOKS_FILE
