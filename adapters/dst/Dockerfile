FROM debian:bookworm-slim

# 安装依赖
RUN dpkg --add-architecture i386 && \
    apt-get update && apt-get install -y \
      libstdc++6:i386 \
      libc6-i386 \
      libgcc-s1:i386 \
      libcurl4-gnutls-dev:i386 \
      lib32gcc-s1 \
      ca-certificates \
      curl \
      bash \
      jq \
      procps \
      && rm -rf /var/lib/apt/lists/*

# 安装 steamcmd
RUN mkdir -p /opt/steamcmd && \
    cd /opt/steamcmd && \
    curl -L -o steamcmd_linux.tar.gz https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz && \
    tar -xvzf steamcmd_linux.tar.gz

# 拷贝脚本和模板
COPY scripts/ /scripts/
COPY templates/ /templates/
RUN chmod +x /scripts/*.sh

WORKDIR /scripts
ENTRYPOINT ["/scripts/start.sh"]