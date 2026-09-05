#!/usr/bin/env bash
# ============================================================
# node-agent 启动脚本（一键更新友好版，见 docs/node-agent-upgrade-design.md §3.2）
#
# 与旧版的差异：删除 4 条 `sudo mkdir/chown /server /data`。
#   - systemd 托管路径：root 准备由 node_agent.service 的 ExecStartPre 完成（无需 sudo/密码）；
#   - 裸跑路径（无 systemd，仅开发/调试）：请先用 root 执行一次
#       install -d -o "$USER" -g "$USER" /server /data
#     （或运行 deploy/install-node-agent.sh）。
#
# 用法:
#   ./start.sh                    # 使用默认配置启动
#   NODE_ID=node-1 ./start.sh     # 指定节点 ID
#   REGISTRY_ADDR=192.168.1.100 ./start.sh
# ============================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# ============================================================
# 配置（可通过环境变量覆盖）
# ============================================================

# 监听地址
export NODE_AGENT_ADDR="${NODE_AGENT_ADDR:-0.0.0.0:50052}"

# 节点唯一标识
export NODE_ID="${NODE_ID:-node-$(hostname)}"

# Asset Service gRPC 地址
export ASSET_SERVICE_ADDR="${ASSET_SERVICE_ADDR:-http://127.0.0.1:50053}"

# Docker 镜像仓库
export REGISTRY_ADDR="${REGISTRY_ADDR:-127.0.0.1}"
export REGISTRY_PORT="${REGISTRY_PORT:-5000}"

# 镜像仓库认证（可选）
# export REGISTRY_USERNAME="user"
# export REGISTRY_PASSWORD="pass"
# export REGISTRY_SERVER_ADDRESS="https://"

# AWS S3 / S3 兼容对象存储配置
export AWS_REGION="${AWS_REGION:-us-east-1}"
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-}"

# 如果使用 MinIO 或其他 S3 兼容服务，需指定自定义 endpoint：
# export S3_ENDPOINT="http://192.168.1.100:9000"

# SQLite 数据文件路径（debug 模式下为内存，生产环境自动创建）
# 默认在 apalis-sqlite 的工作目录下创建 jobs.db

# ============================================================
# 前置检查
# ============================================================

echo "============================================"
echo " node-agent 启动"
echo "--------------------------------------------"
echo " NODE_AGENT_ADDR     = ${NODE_AGENT_ADDR}"
echo " NODE_ID             = ${NODE_ID}"
echo " ASSET_SERVICE_ADDR  = ${ASSET_SERVICE_ADDR}"
echo " REGISTRY            = ${REGISTRY_ADDR}:${REGISTRY_PORT}"
echo "============================================"

# 检查 Docker 是否可用
if ! command -v docker &>/dev/null; then
    echo "[WARN] docker 未安装或不在 PATH 中"
fi

# 检查配置目录（sqlite/apalis 数据目录）；agent 用户自有目录，无需 root
DATA_DIR="${DATA_DIR:-./data}"
mkdir -p "$DATA_DIR"

export RUST_LOG="${RUST_LOG:-info}"
# 一键更新日志目录（见 docs/node-agent-logging-design.md）
export NODE_AGENT_LOG_DIR="${NODE_AGENT_LOG_DIR:-./logs}"
mkdir -p "$NODE_AGENT_LOG_DIR"

# /server /data 由部署方预置（systemd ExecStartPre / install 脚本），此处不重复 sudo
for d in /server /data; do
    if [ ! -d "$d" ]; then
        echo "[WARN] 全局目录 $d 不存在（生产由 node_agent.service ExecStartPre 创建；裸跑请先 root 预置）"
    fi
done

# ============================================================
# 启动
# ============================================================

BINARY="${BINARY:-${SCRIPT_DIR}/node_agent}"

if [ ! -f "$BINARY" ]; then
    echo "[ERROR] 未找到二进制文件: ${BINARY}"
    echo "请先编译: cargo build --release"
    echo "或将二进制路径通过 BINARY 环境变量指定"
    exit 1
fi

echo "启动二进制: ${BINARY}"
exec "$BINARY"
