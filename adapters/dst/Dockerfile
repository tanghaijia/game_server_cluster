# ============================================================
# adapters/dst —— DST（Don't Starve Together）游戏适配器
#
# 基于公共基础镜像（adapters/base）：
#   - 公共运行环境（i386 运行库、steamcmd、工具链）来自 base
#   - 生命周期框架（start/save/stop/players/health）来自 base
#   - 本镜像只提供：差异钩子 hooks.sh + 配置模板 templates/
#
# 构建：
#   1) 先构建 base：  docker build -t registry/game/base:2026.08 adapters/base
#   2) 再构建本镜像： docker build -t registry/game/dst-adapter:0.3.0 adapters/dst
# ============================================================
FROM registry/game/base:2026.08

# DST 专用默认值（hook 与框架读取）
ENV GAME_BIN_NAME=dontstarve_dedicated_server_nullrenderer \
    DST_CONF_DIR=DoNotStarveTogether \
    DST_CLUSTER=Cluster \
    DST_SHARD=both

# 差异钩子 + 配置模板 + 渲染清单（M4：DST 暂无渲染入口，manifest 为空占位）
COPY hooks.sh /scripts/hooks.sh
COPY templates/ /templates/
COPY config-manifest.json /scripts/config-manifest.json
RUN chmod +x /scripts/hooks.sh && mkdir -p /server /data

# 容器契约入口由 base 提供（/scripts/start.sh → lifecycle/start.sh）
