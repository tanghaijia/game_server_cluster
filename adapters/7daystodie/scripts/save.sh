#!/bin/bash
# 通过 Telnet 触发 7 Days to Die 世界保存（saveworld）
# Telnet 不可用（服务未启动或 TelnetEnabled=false）时降级为仅提示，不报错
TELNET_PORT="${SDTD_TELNET_PORT:-8081}"
HOST="127.0.0.1"

# 先探测 Telnet 是否可连接（子 shell 探测，失败不会中断主脚本）
if ! (echo > "/dev/tcp/${HOST}/${TELNET_PORT}") 2>/dev/null; then
  echo "Telnet on ${HOST}:${TELNET_PORT} unavailable, skip save (server not running or Telnet disabled)."
  exit 0
fi

echo "Saving game world via telnet..."
exec 3<>"/dev/tcp/${HOST}/${TELNET_PORT}"
printf 'saveworld\n' >&3
sleep 2
printf 'exit\n' >&3
exec 3<&-

echo "Save triggered."
