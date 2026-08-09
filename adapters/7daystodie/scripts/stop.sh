#!/bin/bash
# 先保存世界，再通过 Telnet shutdown 干净退出；Telnet 不可用或进程残留时 killall 兜底
TELNET_PORT="${SDTD_TELNET_PORT:-8081}"
HOST="127.0.0.1"

/scripts/save.sh

SHUTDOWN_SENT=0
if (echo > "/dev/tcp/${HOST}/${TELNET_PORT}") 2>/dev/null; then
  echo "Sending graceful shutdown via telnet..."
  exec 3<>"/dev/tcp/${HOST}/${TELNET_PORT}"
  printf 'shutdown\n' >&3
  sleep 3
  exec 3<&-
  SHUTDOWN_SENT=1
fi

# 兜底：若进程仍在，强制结束
if pgrep -f 7DaysToDieServer.x86_64 >/dev/null; then
  if [ "${SHUTDOWN_SENT}" = "1" ]; then
    echo "Server still running after shutdown command, force killing..."
  else
    echo "Telnet unavailable, force killing server..."
  fi
  killall 7DaysToDieServer.x86_64 || true
else
  echo "Server already stopped."
fi
