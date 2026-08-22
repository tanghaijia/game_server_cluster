# ============================================================
# adapters/base —— 游戏适配器公共基础镜像
#
# 内容：
#   1. 游戏服务器公共运行环境（32/64 位运行库、steamcmd、工具链）
#   2. 公共脚本库 /scripts/lib/*.sh（日志/文件/进程/输出/端口注入/模板/配置渲染）
#   3. 生命周期框架 /scripts/lifecycle/*.sh（start/save/stop/players/health/prepare-runtime）
#   4. 兼容入口：/scripts/{start,save,stop,players,health,prepare-runtime}.sh
#      软链到 lifecycle，保持既有容器内契约路径
#
# 游戏适配器（如 adapters/dst/）以本镜像为基础：
#   FROM registry/game/base:<tag>
#   COPY hooks.sh /scripts/hooks.sh      # 游戏差异钩子
#   COPY templates/ /templates/          # 游戏配置模板
# ============================================================
FROM debian:bookworm-slim

# ---- 1. 公共运行环境 -------------------------------------------------
# 安装游戏专用服务器所需的公共依赖（32/64 位运行库 + 工具）
# 注：各游戏特有依赖（如有）由游戏适配器 Dockerfile 追加
RUN dpkg --add-architecture i386 && \
    apt-get update && apt-get install -y --no-install-recommends \
      libstdc++6:i386 \
      libc6-i386 \
      libgcc-s1:i386 \
      lib32gcc-s1 \
      libcurl4 \
      libssl3 \
      ca-certificates \
      curl \
      bash \
      jq \
      procps \
      coreutils \
      gettext-base \
      && rm -rf /var/lib/apt/lists/*

# steamcmd（游戏服务端下载工具；节点侧实际下载由宿主机 steamcmd 完成，
# 镜像内保留一份用于 prepare/调试场景）
RUN mkdir -p /opt/steamcmd && \
    cd /opt/steamcmd && \
    curl -L -o steamcmd_linux.tar.gz https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz && \
    tar -xvzf steamcmd_linux.tar.gz && \
    rm -f steamcmd_linux.tar.gz

# ---- 2. 公共脚本库 + 生命周期框架 ------------------------------------
COPY scripts/lib/ /scripts/lib/
COPY scripts/lifecycle/ /scripts/lifecycle/
RUN chmod +x /scripts/lib/*.sh /scripts/lifecycle/*.sh

# ---- 3. 兼容入口：保持容器内契约路径 /scripts/{start,...}.sh ---------
RUN ln -sf /scripts/lifecycle/start.sh          /scripts/start.sh && \
    ln -sf /scripts/lifecycle/save.sh           /scripts/save.sh && \
    ln -sf /scripts/lifecycle/stop.sh           /scripts/stop.sh && \
    ln -sf /scripts/lifecycle/players.sh        /scripts/players.sh && \
    ln -sf /scripts/lifecycle/health.sh         /scripts/health.sh && \
    ln -sf /scripts/lifecycle/prepare-runtime.sh /scripts/prepare-runtime.sh

# ---- 4. 统一数据边界（平台约定挂载点） --------------------------------
# /server     游戏服务端文件（节点 game-cache，只读挂载）
# /data       实例持久化数据（读写）
# /mod-cache  模组缓存（只读挂载，可选）
# /templates  游戏配置模板（由游戏适配器 COPY）
RUN mkdir -p /server /data /mod-cache /templates

WORKDIR /scripts
# 默认入口：生命周期框架 start.sh（游戏适配器通过 hooks.sh 定制，不必覆盖 ENTRYPOINT）
ENTRYPOINT ["/scripts/start.sh"]
