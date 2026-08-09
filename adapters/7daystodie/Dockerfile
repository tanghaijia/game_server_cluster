FROM debian:bookworm-slim

# 安装运行 7 Days to Die 专用服务器所需的依赖
# 该服务器基于 Unity 引擎，需要 32/64 位运行库；实际所需包以首次启动报错为准，可在此微调
RUN dpkg --add-architecture i386 && \
    apt-get update && apt-get install -y \
      libstdc++6:i386 \
      libc6:i386 \
      libgcc-s1:i386 \
      lib32gcc-s1 \
      libcurl4 \
      libssl3 \
      ca-certificates \
      curl \
      bash \
      jq \
      procps \
      && rm -rf /var/lib/apt/lists/*

# 安装 steamcmd（与 DST 适配器保持一致；实际游戏下载由 node_agent 侧宿主机 steamcmd 完成）
RUN mkdir -p /opt/steamcmd && \
    cd /opt/steamcmd && \
    curl -L -o steamcmd_linux.tar.gz https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz && \
    tar -xvzf steamcmd_linux.tar.gz

# 拷贝脚本和模板
COPY scripts/ /scripts/
COPY templates/ /templates/
RUN chmod +x /scripts/*.sh

# /server: 只读游戏缓存（node_agent 挂载，存放 7DTD 服务器安装文件）
# /data:   可写实例数据（存档、世界、日志、Mods、serverconfig.xml）
RUN mkdir -p /server /data

WORKDIR /scripts
ENTRYPOINT ["/scripts/start.sh"]
