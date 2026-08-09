#!/bin/bash
# 检查 7 Days to Die 服务器进程是否存活
pgrep -f 7DaysToDieServer.x86_64 >/dev/null
if [ $? -eq 0 ]; then
  echo '{"healthy": true}'
else
  echo '{"healthy": false}'
fi
