#!/usr/bin/env bash
# ============================================================
# node_agent 一键更新前置部署（一次性，root 执行）
# 见 docs/node-agent-upgrade-design.md §3.2.1
#
# 做什么：
#   1. 建运行用户（可选，默认用当前调用用户）
#   2. 一次性创建全局目录 /server /data 并固定属主
#      → start.sh 不再需要 sudo（旧 4 条 sudo 命令删除）
#   3. 安装 systemd unit（node_agent.service），由 ExecStartPre 保证每次启动目录就绪
#   4. 提示后续：放二进制 + start.sh → systemctl enable --now node_agent
#
# 用法：
#   sudo ./install-node-agent.sh [run_user] [install_dir]
#     默认 run_user  = 当前 $USER
#     默认 install_dir = /opt/node-agent
# ============================================================
set -euo pipefail

RUN_USER="${1:-$USER}"
INSTALL_DIR="${2:-/opt/node-agent}"

if [ "$(id -u)" -ne 0 ]; then
    echo "[ERROR] 请用 root 执行（需创建 /server /data 并安装 systemd unit）" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# ---------- 1. 运行用户 ----------
if ! id "$RUN_USER" &>/dev/null; then
    echo "创建运行用户 $RUN_USER ..."
    useradd -r -m -s /usr/sbin/nologin "$RUN_USER"
fi

# ---------- 2. 一次性全局目录 ----------
echo "创建 /server /data 并固定属主为 $RUN_USER ..."
install -d -o "$RUN_USER" -g "$RUN_USER" /server /data
# 旧版本可能以 root 遗留内容，兜底修正属主（首次幂等）
chown -R "$RUN_USER":"$RUN_USER" /server /data 2>/dev/null || true

# ---------- 3. 安装目录 + 二进制 + 启动脚本 ----------
echo "安装目录 $INSTALL_DIR ..."
install -d -o "$RUN_USER" -g "$RUN_USER" "$INSTALL_DIR"

if [ -f "${SCRIPT_DIR}/../node_agent" ]; then
    echo "安装 node_agent 二进制 → $INSTALL_DIR/node_agent"
    install -m 0755 "${SCRIPT_DIR}/../node_agent" "$INSTALL_DIR/node_agent"
    chown "$RUN_USER":"$RUN_USER" "$INSTALL_DIR/node_agent"
else
    echo "[WARN] 未找到 node_agent 二进制（node_agent/ 下编译 cargo build --release 后重跑，或手动放置）"
fi

if [ -f "${SCRIPT_DIR}/../start.sh" ]; then
    echo "安装启动脚本 → $INSTALL_DIR/start.sh"
    install -m 0755 "${SCRIPT_DIR}/../start.sh" "$INSTALL_DIR/start.sh"
fi

# ---------- 4. systemd unit（含目录准备 ExecStartPre） ----------
UNIT_SRC="${SCRIPT_DIR}/node_agent.service"
UNIT_DST="/etc/systemd/system/node_agent.service"
echo "安装 systemd unit → $UNIT_DST"
sed -e "s|__RUN_USER__|$RUN_USER|g" -e "s|/opt/node-agent|$INSTALL_DIR|g" \
    "$UNIT_SRC" > "$UNIT_DST"

systemctl daemon-reload

echo
echo "============================================"
echo " 完成。启动服务："
echo "   systemctl enable --now node_agent"
echo " 检查："
echo "   systemctl status node_agent"
echo "   journalctl -u node_agent -f"
echo " 更新二进制后（admin 一键更新 / 手动替换），"
echo "   systemctl restart node_agent"
echo " 即可 —— 全程无需再输入 sudo 密码。"
echo "============================================"
