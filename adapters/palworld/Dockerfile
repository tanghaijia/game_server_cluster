# ============================================================
# adapters/palworld —— 幻兽帕鲁（Palworld）游戏适配器
#
# 基于公共基础镜像（adapters/base）：
#   - 公共运行环境（i386 运行库、steamcmd、工具链）来自 base
#   - 生命周期框架（start/save/stop/players/health）来自 base
#   - 本镜像只提供：差异钩子 hooks.sh + 配置模板 templates/
#     + 渲染清单 config-manifest.json + 默认值 config-defaults.env
#
# 构建（先重建 base：② 在 scripts/lib 新增了 config-env.sh）：
#   1) 先构建 base：  docker build -t registry/game/base:2026.08 adapters/base
#      （base 重打后如升级 tag，请同步下方 FROM）
#   2) 再构建本镜像：
#      docker build -t registry/game/palworld-adapter:0.1.0-draft adapters/palworld
#      （转正式后把 adapter.toml [image].tag 改为同值并重生成 metadata.json）
#   3) 推送：         docker push registry/game/palworld-adapter:0.1.0-draft
# ============================================================
FROM registry/game/base:2026.08

# Palworld 额外运行时依赖（UE5 服务器）：
#   base 已有 libcurl4（openssl 变体 64 位）、libssl3、libstdc++6、i386 运行库。
#   此处补齐 UE5 运行时常用库（依据：UE 在 Linux 依赖 ICU/curl/ssl；社区指南
#   libicu/libunwind/libcurl 均有涉及）。缺库以 ⑤ 验收 ldd 报错为准，可按需增删。
RUN apt-get update && apt-get install -y --no-install-recommends \
      libicu-dev \
      libunwind-dev \
      libcurl4-openssl-dev \
      libssl-dev \
      && rm -rf /var/lib/apt/lists/*

# Palworld 专用默认值（hook 与框架读取；GAME_BIN_NAME 用于默认 health 进程匹配）
ENV GAME_BIN_NAME=PalServer-Linux-Test

# 差异钩子 + 配置模板 + 渲染清单（file 为空，渲染由 hook 自管）+ 默认值（② config-env.sh 读取）
COPY hooks.sh /scripts/hooks.sh
COPY templates/ /templates/
COPY config-manifest.json /scripts/config-manifest.json
COPY config-defaults.env /scripts/config-defaults.env
RUN chmod +x /scripts/hooks.sh && mkdir -p /server /data

# 容器契约入口由 base 提供（/scripts/start.sh → lifecycle/start.sh）
