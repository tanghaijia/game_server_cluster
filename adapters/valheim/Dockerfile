# ============================================================
# adapters/valheim —— 英灵神殿（Valheim）游戏适配器
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
#      docker build -t registry/game/valheim-adapter:0.1.0-draft adapters/valheim
#      （转正式后把 adapter.toml [image].tag 改为同值并重生成 metadata.json）
#   3) 推送：         docker push registry/game/valheim-adapter:0.1.0-draft
# ============================================================
FROM registry/game/base:2026.08

# Valheim（Unity IL2CPP）不额外装包：base 公共依赖（libstdc++6、libssl3、
# libcurl4、ca-certificates）预期已覆盖。若 ⑤ 验收 ldd 报缺库，按报错在此补齐。

# Valheim 专用默认值（GAME_BIN_NAME 用于默认 health 进程匹配：
# "valheim_server" 子串同时命中 wrapper 与子进程）
ENV GAME_BIN_NAME=valheim_server

# 差异钩子 + 配置模板（adminlist/bannedlist/permittedlist）+ 渲染清单
# （file 为空，配置走 CLI 参数）+ 默认值（② config-env.sh 读取）
COPY hooks.sh /scripts/hooks.sh
COPY templates/ /templates/
COPY config-manifest.json /scripts/config-manifest.json
COPY config-defaults.env /scripts/config-defaults.env
RUN chmod +x /scripts/hooks.sh && mkdir -p /server /data

# 容器契约入口由 base 提供（/scripts/start.sh → lifecycle/start.sh）
